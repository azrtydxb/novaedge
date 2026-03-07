/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vip

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"time"

	api "github.com/osrg/gobgp/v3/api"
	"github.com/osrg/gobgp/v3/pkg/server"
	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/azrtydxb/novaedge/internal/agent/metrics"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

var (
	errBGPConfigIsRequiredForBGPModeVIPs = errors.New("BGP config is required for BGP mode VIPs")
)

// BGPHandler manages BGP VIP mode with IPv4/IPv6 and BFD support
type BGPHandler struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// BGP server instance
	bgpServer *server.BgpServer

	// Active VIPs and their configurations
	activeVIPs map[string]*BGPVIPState

	// BGP server started flag
	started bool

	// currentServerAS tracks the Local AS number the running BGP server was
	// started with. It is set by startBGPServer and used to detect cross-VIP
	// AS number mismatches when a new VIP with a different LocalAs is added.
	currentServerAS uint32

	// BFD manager for fast failure detection
	bfdManager *BFDManager
}

// BGPVIPState tracks the state of a BGP VIP
type BGPVIPState struct {
	Assignment *pb.VIPAssignment
	IP         net.IP
	AddedAt    time.Time
	Announced  bool
	IsIPv6     bool
	// bgpConfig stores the BGP config that was applied when this VIP was added,
	// so we can diff against new config during reconfiguration.
	bgpConfig *pb.BGPConfig
	// bfdConfig stores the BFD config that was applied when this VIP was added.
	bfdConfig *pb.BFDConfig
}

// NewBGPHandler creates a new BGP handler
func NewBGPHandler(logger *zap.Logger) (*BGPHandler, error) {
	handler := &BGPHandler{
		logger:     logger,
		activeVIPs: make(map[string]*BGPVIPState),
		started:    false,
	}

	// Create BFD manager with callbacks for neighbor failure and recovery
	handler.bfdManager = NewBFDManager(logger, handler.onBFDNeighborDown, handler.onBFDNeighborUp)

	return handler, nil
}

// Start starts the BGP handler
func (h *BGPHandler) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.started {
		return nil
	}

	h.logger.Info("Starting BGP handler")

	// Start BFD manager
	if err := h.bfdManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start BFD manager: %w", err)
	}

	h.started = true

	return nil
}

// Stop gracefully shuts down the GoBGP server and BFD manager.
func (h *BGPHandler) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.bfdManager != nil {
		h.bfdManager.Stop()
	}
	if h.bgpServer != nil {
		h.bgpServer.Stop()
	}
	h.started = false
	h.logger.Info("BGP handler stopped")
	return nil
}

// AddVIP adds a VIP with BGP announcement (IPv4 or IPv6).
// If the VIP already exists, it performs in-place reconfiguration of changed
// BGP/BFD parameters without releasing the VIP address.
func (h *BGPHandler) AddVIP(ctx context.Context, assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if already active — reconfigure in place
	if existingState, exists := h.activeVIPs[assignment.VipName]; exists {
		h.logger.Info("VIP already active, reconfiguring BGP",
			zap.String("vip", assignment.VipName),
		)
		return h.reconfigureVIP(ctx, existingState, assignment)
	}

	// Parse IP address
	ip, _, err := net.ParseCIDR(assignment.Address)
	if err != nil {
		return fmt.Errorf(errInvalidVIPAddressFmt, assignment.Address, err)
	}

	// Validate BGP config
	if assignment.BgpConfig == nil {
		return errBGPConfigIsRequiredForBGPModeVIPs
	}

	isIPv6 := ip.To4() == nil

	h.logger.Info("Adding VIP with BGP announcement",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
		zap.Uint32("local_as", assignment.BgpConfig.LocalAs),
		zap.Bool("ipv6", isIPv6),
	)

	// Start BGP server if not already started
	if h.bgpServer == nil {
		if err := h.startBGPServer(ctx, assignment.BgpConfig); err != nil {
			return fmt.Errorf("failed to start BGP server: %w", err)
		}
	} else {
		// BGP server already running — check whether the new VIP requests a
		// different Local AS number.  If so, the server must be restarted with
		// the new AS before we can announce this VIP.
		if assignment.BgpConfig.LocalAs != 0 && assignment.BgpConfig.LocalAs != h.currentServerAS {
			h.logger.Info("BGP server AS mismatch for new VIP, restarting",
				zap.Uint32("current_as", h.currentServerAS),
				zap.Uint32("requested_as", assignment.BgpConfig.LocalAs),
				zap.String("vip", assignment.VipName),
			)
			if err := h.restartBGPServer(ctx, assignment.BgpConfig); err != nil {
				return fmt.Errorf("restart BGP server for AS mismatch: %w", err)
			}
			// Re-announce all existing VIPs under the new AS.
			for _, existingState := range h.activeVIPs {
				if existingState.IP != nil {
					if err := h.announceRoute(ctx, existingState.IP, existingState.bgpConfig, existingState.IsIPv6); err != nil {
						h.logger.Error("Failed to re-announce VIP after AS change",
							zap.String("vip", existingState.Assignment.VipName),
							zap.Error(err),
						)
					} else {
						existingState.Announced = true
					}
				}
			}
		} else {
			// Ensure peers from this VIP config are added.  This handles the
			// case where multiple VIPs have different peer lists (e.g.,
			// auto-assigned-vip vs perf-vip).  Without this, peers are only
			// added for the first VIP that triggers startBGPServer.
			h.ensurePeersConfigured(ctx, assignment.BgpConfig)
		}
	}

	// Bind VIP address to loopback so the node can accept traffic
	if err := h.addLoopbackAddress(assignment.Address); err != nil {
		h.logger.Warn("Failed to bind VIP to loopback",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
	}

	// Announce route
	if err := h.announceRoute(ctx, ip, assignment.BgpConfig, isIPv6); err != nil {
		h.logger.Warn("Failed to announce BGP route",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
	}

	// Track VIP state
	h.activeVIPs[assignment.VipName] = &BGPVIPState{
		Assignment: assignment,
		IP:         ip,
		AddedAt:    time.Now(),
		Announced:  true,
		IsIPv6:     isIPv6,
		bgpConfig:  assignment.BgpConfig,
		bfdConfig:  assignment.BfdConfig,
	}

	// Setup BFD sessions for peers if BFD is configured
	if assignment.BfdConfig != nil && assignment.BfdConfig.Enabled {
		h.setupBFDSessions(assignment)
	}

	// Update metrics
	metrics.BGPAnnouncedRoutes.Set(float64(len(h.activeVIPs)))

	h.logger.Info("VIP announced via BGP successfully",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)

	return nil
}

// RemoveVIP removes a VIP and withdraws BGP announcement
func (h *BGPHandler) RemoveVIP(ctx context.Context, assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.activeVIPs[assignment.VipName]
	if !exists {
		h.logger.Debug(msgVIPNotActive, zap.String("vip", assignment.VipName))
		return nil
	}

	h.logger.Info("Removing VIP and withdrawing BGP route",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)

	// Withdraw route
	if state.Announced && h.bgpServer != nil {
		if err := h.withdrawRoute(ctx, state.IP, assignment.BgpConfig, state.IsIPv6); err != nil {
			h.logger.Warn("Failed to withdraw BGP route",
				zap.String("vip", assignment.VipName),
				zap.Error(err),
			)
		}
	}

	// Remove BFD sessions for peers
	if assignment.BgpConfig != nil {
		for _, peer := range assignment.BgpConfig.Peers {
			peerIP := net.ParseIP(peer.Address)
			if peerIP != nil {
				h.bfdManager.RemoveSession(peerIP)
			}
		}
	}

	// Remove VIP address from loopback
	if err := h.removeLoopbackAddress(assignment.Address); err != nil {
		h.logger.Warn("Failed to remove VIP from loopback",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
	}

	delete(h.activeVIPs, assignment.VipName)

	// Update metrics
	metrics.BGPAnnouncedRoutes.Set(float64(len(h.activeVIPs)))

	h.logger.Info("VIP withdrawn from BGP successfully",
		zap.String("vip", assignment.VipName),
		zap.Duration("duration", time.Since(state.AddedAt)),
	)

	return nil
}

// addLoopbackAddress binds a VIP address to the loopback interface so the
// node can receive traffic for announced BGP routes.
func (h *BGPHandler) addLoopbackAddress(cidr string) error {
	link, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("failed to get loopback interface: %w", err)
	}

	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("failed to parse address %s: %w", cidr, err)
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		if errors.Is(err, syscall.EEXIST) {
			h.logger.Debug("Loopback address already exists", zap.String("address", cidr))
			return nil
		}
		return fmt.Errorf("failed to add loopback address: %w", err)
	}

	h.logger.Info("Bound VIP address to loopback", zap.String("address", cidr))
	return nil
}

// removeLoopbackAddress removes a VIP address from the loopback interface.
func (h *BGPHandler) removeLoopbackAddress(cidr string) error {
	link, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("failed to get loopback interface: %w", err)
	}

	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("failed to parse address %s: %w", cidr, err)
	}

	if err := netlink.AddrDel(link, addr); err != nil {
		if errors.Is(err, syscall.EADDRNOTAVAIL) {
			h.logger.Debug("Loopback address doesn't exist", zap.String("address", cidr))
			return nil
		}
		return fmt.Errorf("failed to remove loopback address: %w", err)
	}

	h.logger.Info("Removed VIP address from loopback", zap.String("address", cidr))
	return nil
}

// setupBFDSessions creates BFD sessions for all BGP peers
func (h *BGPHandler) setupBFDSessions(assignment *pb.VIPAssignment) {
	if assignment.BfdConfig == nil || !assignment.BfdConfig.Enabled {
		return
	}

	bfdCfg := BFDConfig{
		DetectMultiplier: assignment.BfdConfig.DetectMultiplier,
		EchoMode:         assignment.BfdConfig.EchoMode,
	}

	// Parse duration strings
	if assignment.BfdConfig.DesiredMinTxInterval != "" {
		if d, err := time.ParseDuration(assignment.BfdConfig.DesiredMinTxInterval); err == nil {
			bfdCfg.DesiredMinTxInterval = d
		} else {
			h.logger.Warn("Invalid BFD DesiredMinTxInterval, using default",
				zap.String("value", assignment.BfdConfig.DesiredMinTxInterval),
				zap.Error(err),
			)
		}
	}
	if assignment.BfdConfig.RequiredMinRxInterval != "" {
		if d, err := time.ParseDuration(assignment.BfdConfig.RequiredMinRxInterval); err == nil {
			bfdCfg.RequiredMinRxInterval = d
		} else {
			h.logger.Warn("Invalid BFD RequiredMinRxInterval, using default",
				zap.String("value", assignment.BfdConfig.RequiredMinRxInterval),
				zap.Error(err),
			)
		}
	}

	for _, peer := range assignment.BgpConfig.Peers {
		peerIP := net.ParseIP(peer.Address)
		if peerIP == nil {
			h.logger.Warn("Invalid peer IP for BFD session", zap.String("peer", peer.Address))
			continue
		}

		if err := h.bfdManager.AddSession(peerIP, bfdCfg); err != nil {
			h.logger.Error("Failed to add BFD session",
				zap.String("peer", peer.Address),
				zap.Error(err),
			)
		}
	}
}

// reconfigureVIP handles in-place reconfiguration of an existing BGP VIP.
// It diffs the old and new BGP/BFD config and applies only the changes needed.
// Called with h.mu held.
func (h *BGPHandler) reconfigureVIP(ctx context.Context, state *BGPVIPState, assignment *pb.VIPAssignment) error {
	oldBGP := state.bgpConfig
	newBGP := assignment.BgpConfig

	if newBGP == nil {
		return errBGPConfigIsRequiredForBGPModeVIPs
	}

	// Check if ASN or RouterID changed — requires full BGP server restart
	if oldBGP != nil && (oldBGP.LocalAs != newBGP.LocalAs || oldBGP.RouterId != newBGP.RouterId) {
		h.logger.Info("BGP ASN or RouterID changed, restarting BGP server",
			zap.Uint32("old_as", oldBGP.LocalAs),
			zap.Uint32("new_as", newBGP.LocalAs),
			zap.String("old_router_id", oldBGP.RouterId),
			zap.String("new_router_id", newBGP.RouterId),
		)
		if err := h.restartBGPServer(ctx, newBGP); err != nil {
			return fmt.Errorf("failed to restart BGP server: %w", err)
		}
		// After server restart, re-announce all active VIPs.
		// We check vipState.IP != nil rather than vipState.Announced because
		// restartBGPServer sets Announced = false for every VIP, so checking
		// Announced would silently skip every route.
		for _, vipState := range h.activeVIPs {
			if vipState.IP != nil {
				if err := h.announceRoute(ctx, vipState.IP, newBGP, vipState.IsIPv6); err != nil {
					h.logger.Error("Failed to re-announce route after BGP restart",
						zap.String("vip", vipState.Assignment.VipName),
						zap.Error(err),
					)
				} else {
					vipState.Announced = true
				}
			}
		}
	} else if oldBGP != nil {
		// Diff peers and apply changes
		h.diffAndApplyPeers(ctx, oldBGP.Peers, newBGP.Peers)
	}

	// Check if route attributes changed (communities, local_preference)
	if oldBGP != nil && (oldBGP.LocalPreference != newBGP.LocalPreference || !communitiesEqual(oldBGP.Communities, newBGP.Communities)) {
		h.logger.Info("BGP route attributes changed, re-announcing route",
			zap.String("vip", assignment.VipName),
		)
		// Withdraw and re-announce with new attributes
		if state.Announced && h.bgpServer != nil {
			if err := h.withdrawRoute(ctx, state.IP, oldBGP, state.IsIPv6); err != nil {
				h.logger.Warn("Failed to withdraw route during reconfig",
					zap.String("vip", assignment.VipName),
					zap.Error(err),
				)
			}
			if err := h.announceRoute(ctx, state.IP, newBGP, state.IsIPv6); err != nil {
				h.logger.Error("Failed to re-announce route with new attributes",
					zap.String("vip", assignment.VipName),
					zap.Error(err),
				)
			}
		}
	}

	// Handle BFD config changes
	oldBFD := state.bfdConfig
	newBFD := assignment.BfdConfig
	if !proto.Equal(oldBFD, newBFD) {
		h.reconfigureBFD(oldBFD, newBFD, oldBGP, newBGP)
	}

	// Update stored state
	state.Assignment = assignment
	state.bgpConfig = newBGP
	state.bfdConfig = newBFD

	h.logger.Info("VIP reconfigured successfully",
		zap.String("vip", assignment.VipName),
	)

	return nil
}

// restartBGPServer stops and restarts the BGP server with new global config.
// Called with h.mu held.
func (h *BGPHandler) restartBGPServer(ctx context.Context, config *pb.BGPConfig) error {
	if h.bgpServer != nil {
		// Withdraw all routes before stopping
		for _, state := range h.activeVIPs {
			if state.Announced {
				_ = h.withdrawRoute(ctx, state.IP, state.bgpConfig, state.IsIPv6)
				state.Announced = false
			}
		}

		h.bgpServer.Stop()
		h.bgpServer = nil
	}

	return h.startBGPServer(ctx, config)
}

// diffAndApplyPeers compares old and new peer lists and applies changes.
// Called with h.mu held.
func (h *BGPHandler) diffAndApplyPeers(ctx context.Context, oldPeers, newPeers []*pb.BGPPeer) {
	oldMap := make(map[string]*pb.BGPPeer, len(oldPeers))
	for _, p := range oldPeers {
		oldMap[p.Address] = p
	}
	newMap := make(map[string]*pb.BGPPeer, len(newPeers))
	for _, p := range newPeers {
		newMap[p.Address] = p
	}

	// Remove peers that are no longer in the config
	for addr := range oldMap {
		if _, exists := newMap[addr]; !exists {
			h.logger.Info("Removing BGP peer", zap.String("address", addr))
			if h.bgpServer != nil {
				if err := h.bgpServer.DeletePeer(ctx, &api.DeletePeerRequest{
					Address: addr,
				}); err != nil {
					h.logger.Error("Failed to delete BGP peer",
						zap.String("address", addr),
						zap.Error(err),
					)
				}
			}
		}
	}

	// Add new peers or update changed peers
	for addr, newPeer := range newMap {
		oldPeer, exists := oldMap[addr]
		if !exists {
			// New peer — add it
			h.addBGPPeer(ctx, newPeer)
		} else if oldPeer.As != newPeer.As || oldPeer.Port != newPeer.Port {
			// Peer changed — delete and re-add
			h.logger.Info("Updating BGP peer (AS or port changed)",
				zap.String("address", addr),
				zap.Uint32("old_as", oldPeer.As),
				zap.Uint32("new_as", newPeer.As),
			)
			if h.bgpServer != nil {
				if err := h.bgpServer.DeletePeer(ctx, &api.DeletePeerRequest{
					Address: addr,
				}); err != nil {
					h.logger.Error("Failed to delete BGP peer for update",
						zap.String("address", addr),
						zap.Error(err),
					)
				}
			}
			h.addBGPPeer(ctx, newPeer)
		}
	}
}

// addBGPPeer adds a single BGP peer to the running server.
// Called with h.mu held.
func (h *BGPHandler) addBGPPeer(ctx context.Context, peer *pb.BGPPeer) {
	if h.bgpServer == nil {
		return
	}

	port := peer.Port
	if port == 0 {
		port = 179
	}

	peerIP := net.ParseIP(peer.Address)
	isIPv6Peer := peerIP != nil && peerIP.To4() == nil

	h.logger.Info("Adding BGP peer",
		zap.String("address", peer.Address),
		zap.Uint32("as", peer.As),
		zap.Uint32("port", port),
		zap.Bool("ipv6", isIPv6Peer),
	)

	afiSafis := []*api.AfiSafi{}
	if isIPv6Peer {
		afiSafis = append(afiSafis, &api.AfiSafi{
			Config: &api.AfiSafiConfig{
				Family: &api.Family{
					Afi:  api.Family_AFI_IP6,
					Safi: api.Family_SAFI_UNICAST,
				},
				Enabled: true,
			},
		})
	} else {
		afiSafis = append(afiSafis, &api.AfiSafi{
			Config: &api.AfiSafiConfig{
				Family: &api.Family{
					Afi:  api.Family_AFI_IP,
					Safi: api.Family_SAFI_UNICAST,
				},
				Enabled: true,
			},
		})
	}

	peerConfig := &api.AddPeerRequest{
		Peer: &api.Peer{
			Conf: &api.PeerConf{
				NeighborAddress: peer.Address,
				PeerAsn:         peer.As,
			},
			Transport: &api.Transport{
				RemotePort:  port,
				PassiveMode: true,
			},
			AfiSafis: afiSafis,
		},
	}

	if err := h.bgpServer.AddPeer(ctx, peerConfig); err != nil {
		h.logger.Error("Failed to add BGP peer",
			zap.String("address", peer.Address),
			zap.Error(err),
		)
	}
}

// reconfigureBFD handles BFD configuration changes for BGP peers.
// Called with h.mu held.
func (h *BGPHandler) reconfigureBFD(oldBFD, newBFD *pb.BFDConfig, oldBGP, newBGP *pb.BGPConfig) {
	oldEnabled := oldBFD != nil && oldBFD.Enabled
	newEnabled := newBFD != nil && newBFD.Enabled

	if !oldEnabled && newEnabled {
		// BFD newly enabled — create sessions for all current peers
		h.logger.Info("BFD enabled, creating sessions for BGP peers")
		tempAssignment := &pb.VIPAssignment{
			BgpConfig: newBGP,
			BfdConfig: newBFD,
		}
		h.setupBFDSessions(tempAssignment)
		return
	}

	if oldEnabled && !newEnabled {
		// BFD disabled — remove all sessions
		h.logger.Info("BFD disabled, removing sessions for BGP peers")
		if oldBGP != nil {
			for _, peer := range oldBGP.Peers {
				peerIP := net.ParseIP(peer.Address)
				if peerIP != nil {
					h.bfdManager.RemoveSession(peerIP)
				}
			}
		}
		return
	}

	if !newEnabled {
		return
	}

	// BFD params changed — update existing sessions
	h.logger.Info("BFD parameters changed, updating sessions")
	bfdCfg := BFDConfig{
		DetectMultiplier: newBFD.DetectMultiplier,
		EchoMode:         newBFD.EchoMode,
	}
	if newBFD.DesiredMinTxInterval != "" {
		if d, err := time.ParseDuration(newBFD.DesiredMinTxInterval); err == nil {
			bfdCfg.DesiredMinTxInterval = d
		}
	}
	if newBFD.RequiredMinRxInterval != "" {
		if d, err := time.ParseDuration(newBFD.RequiredMinRxInterval); err == nil {
			bfdCfg.RequiredMinRxInterval = d
		}
	}

	if newBGP != nil {
		for _, peer := range newBGP.Peers {
			peerIP := net.ParseIP(peer.Address)
			if peerIP != nil {
				if err := h.bfdManager.UpdateSession(peerIP, bfdCfg); err != nil {
					h.logger.Error("Failed to update BFD session",
						zap.String("peer", peer.Address),
						zap.Error(err),
					)
				}
			}
		}
	}
}

// communitiesEqual checks if two community string slices are equal.
func communitiesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// onBFDNeighborDown is called when BFD detects a neighbor failure.
// It withdraws all routes that were announced through the failed BGP peer
// for sub-second failover. The BGP session will also eventually detect the
// failure via holdtimer, but BFD gives much faster detection.
func (h *BGPHandler) onBFDNeighborDown(peerIP net.IP) {
	h.mu.Lock()
	defer h.mu.Unlock()

	peerStr := peerIP.String()
	h.logger.Warn("BFD detected neighbor down, withdrawing routes through failed peer",
		zap.String("peer", peerStr),
	)

	if h.bgpServer == nil {
		h.logger.Warn("BGP server not running, cannot withdraw routes",
			zap.String("peer", peerStr),
		)
		return
	}

	ctx := context.Background()
	withdrawn := 0

	for vipName, state := range h.activeVIPs {
		if state.Assignment.BgpConfig == nil {
			continue
		}

		// Check if this VIP has the failed peer in its BGP config
		peerFound := false
		for _, peer := range state.Assignment.BgpConfig.Peers {
			if peer.Address == peerStr {
				peerFound = true
				break
			}
		}

		if !peerFound {
			continue
		}

		if !state.Announced {
			continue
		}

		h.logger.Warn("Withdrawing route for VIP due to BFD neighbor failure",
			zap.String("vip", vipName),
			zap.String("address", state.Assignment.Address),
			zap.String("failed_peer", peerStr),
		)

		if err := h.withdrawRoute(ctx, state.IP, state.Assignment.BgpConfig, state.IsIPv6); err != nil {
			h.logger.Error("Failed to withdraw route on BFD neighbor down",
				zap.String("vip", vipName),
				zap.String("peer", peerStr),
				zap.Error(err),
			)
			continue
		}

		state.Announced = false
		withdrawn++
	}

	if withdrawn > 0 {
		metrics.BGPAnnouncedRoutes.Set(float64(h.countAnnouncedVIPs()))
		h.logger.Warn("Routes withdrawn due to BFD neighbor failure",
			zap.String("peer", peerStr),
			zap.Int("withdrawn_count", withdrawn),
		)
	}
}

// onBFDNeighborUp is called when BFD detects a neighbor has recovered.
// It re-announces all routes that were previously withdrawn due to BFD failure,
// providing automatic self-healing of BGP sessions.
func (h *BGPHandler) onBFDNeighborUp(peerIP net.IP) {
	h.mu.Lock()
	defer h.mu.Unlock()

	peerStr := peerIP.String()
	h.logger.Info("BFD detected neighbor recovery, re-announcing withdrawn routes",
		zap.String("peer", peerStr),
	)

	if h.bgpServer == nil {
		h.logger.Warn("BGP server not running, cannot re-announce routes",
			zap.String("peer", peerStr),
		)
		return
	}

	ctx := context.Background()
	reannounced := 0

	for vipName, state := range h.activeVIPs {
		if state.Assignment.BgpConfig == nil {
			continue
		}

		// Check if this VIP has the recovered peer in its BGP config
		peerFound := false
		for _, peer := range state.Assignment.BgpConfig.Peers {
			if peer.Address == peerStr {
				peerFound = true
				break
			}
		}

		if !peerFound {
			continue
		}

		// Only re-announce routes that were previously withdrawn
		if state.Announced {
			continue
		}

		h.logger.Info("Re-announcing route for VIP after BFD neighbor recovery",
			zap.String("vip", vipName),
			zap.String("address", state.Assignment.Address),
			zap.String("recovered_peer", peerStr),
		)

		if err := h.announceRoute(ctx, state.IP, state.Assignment.BgpConfig, state.IsIPv6); err != nil {
			h.logger.Error("Failed to re-announce route on BFD neighbor recovery",
				zap.String("vip", vipName),
				zap.String("peer", peerStr),
				zap.Error(err),
			)
			continue
		}

		state.Announced = true
		reannounced++
	}

	if reannounced > 0 {
		metrics.BGPAnnouncedRoutes.Set(float64(h.countAnnouncedVIPs()))
		h.logger.Info("Routes re-announced after BFD neighbor recovery",
			zap.String("peer", peerStr),
			zap.Int("reannounced_count", reannounced),
		)
	}
}

// countAnnouncedVIPs returns the number of VIPs with Announced=true.
// Called with h.mu held.
func (h *BGPHandler) countAnnouncedVIPs() int {
	count := 0
	for _, state := range h.activeVIPs {
		if state.Announced {
			count++
		}
	}
	return count
}

// ensurePeersConfigured adds any BGP peers from config that aren't already
// configured on the running BGP server. This is needed because multiple VIPs
// may have different peer lists, and only the first VIP's peers are added
// during startBGPServer. Called with h.mu held.
func (h *BGPHandler) ensurePeersConfigured(ctx context.Context, config *pb.BGPConfig) {
	if config == nil || len(config.Peers) == 0 {
		return
	}

	// Collect already-configured peer addresses
	existingPeers := make(map[string]bool)
	if err := h.bgpServer.ListPeer(ctx, &api.ListPeerRequest{}, func(p *api.Peer) {
		if p.Conf != nil {
			existingPeers[p.Conf.NeighborAddress] = true
		}
	}); err != nil {
		h.logger.Warn("Failed to list BGP peers", zap.Error(err))
		return
	}

	for _, peer := range config.Peers {
		if existingPeers[peer.Address] {
			continue
		}

		port := peer.Port
		if port == 0 {
			port = 179
		}

		peerIP := net.ParseIP(peer.Address)
		isIPv6Peer := peerIP != nil && peerIP.To4() == nil

		h.logger.Info("Adding missing BGP peer from VIP config",
			zap.String("address", peer.Address),
			zap.Uint32("as", peer.As),
			zap.Uint32("port", port),
			zap.Bool("ipv6", isIPv6Peer),
		)

		afiSafis := []*api.AfiSafi{}
		if isIPv6Peer {
			afiSafis = append(afiSafis, &api.AfiSafi{
				Config: &api.AfiSafiConfig{
					Family: &api.Family{
						Afi:  api.Family_AFI_IP6,
						Safi: api.Family_SAFI_UNICAST,
					},
					Enabled: true,
				},
			})
		} else {
			afiSafis = append(afiSafis, &api.AfiSafi{
				Config: &api.AfiSafiConfig{
					Family: &api.Family{
						Afi:  api.Family_AFI_IP,
						Safi: api.Family_SAFI_UNICAST,
					},
					Enabled: true,
				},
			})
		}

		peerConfig := &api.AddPeerRequest{
			Peer: &api.Peer{
				Conf: &api.PeerConf{
					NeighborAddress: peer.Address,
					PeerAsn:         peer.As,
				},
				Transport: &api.Transport{
					RemotePort:  port,
					PassiveMode: true,
				},
				AfiSafis: afiSafis,
			},
		}

		if err := h.bgpServer.AddPeer(ctx, peerConfig); err != nil {
			h.logger.Error("Failed to add BGP peer",
				zap.String("address", peer.Address),
				zap.Error(err),
			)
		}
	}
}

// startBGPServer initializes and starts the BGP server
func (h *BGPHandler) startBGPServer(ctx context.Context, config *pb.BGPConfig) error {
	h.logger.Info("Starting BGP server",
		zap.Uint32("local_as", config.LocalAs),
		zap.String("router_id", config.RouterId),
	)

	h.bgpServer = server.NewBgpServer()
	go h.bgpServer.Serve()

	globalConfig := &api.StartBgpRequest{
		Global: &api.Global{
			Asn:             config.LocalAs,
			RouterId:        config.RouterId,
			ListenAddresses: []string{"0.0.0.0", "::"},
			ListenPort:      179,
		},
	}

	if err := h.bgpServer.StartBgp(ctx, globalConfig); err != nil {
		return fmt.Errorf("failed to start BGP server: %w", err)
	}

	// Record the AS number that this server instance is running with so that
	// AddVIP can detect when a new VIP requests a different AS number.
	h.currentServerAS = config.LocalAs

	// Configure BGP peers
	for _, peer := range config.Peers {
		port := peer.Port
		if port == 0 {
			port = 179
		}

		peerIP := net.ParseIP(peer.Address)
		isIPv6Peer := peerIP != nil && peerIP.To4() == nil

		h.logger.Info("Adding BGP peer",
			zap.String("address", peer.Address),
			zap.Uint32("as", peer.As),
			zap.Uint32("port", port),
			zap.Bool("ipv6", isIPv6Peer),
		)

		peerConf := &api.PeerConf{
			NeighborAddress: peer.Address,
			PeerAsn:         peer.As,
		}

		// Configure address families based on peer IP version
		afiSafis := []*api.AfiSafi{}
		if isIPv6Peer {
			afiSafis = append(afiSafis, &api.AfiSafi{
				Config: &api.AfiSafiConfig{
					Family: &api.Family{
						Afi:  api.Family_AFI_IP6,
						Safi: api.Family_SAFI_UNICAST,
					},
					Enabled: true,
				},
			})
		} else {
			afiSafis = append(afiSafis, &api.AfiSafi{
				Config: &api.AfiSafiConfig{
					Family: &api.Family{
						Afi:  api.Family_AFI_IP,
						Safi: api.Family_SAFI_UNICAST,
					},
					Enabled: true,
				},
			})
		}

		peerConfig := &api.AddPeerRequest{
			Peer: &api.Peer{
				Conf: peerConf,
				Transport: &api.Transport{
					RemotePort:  port,
					PassiveMode: true,
				},
				AfiSafis: afiSafis,
			},
		}

		if err := h.bgpServer.AddPeer(ctx, peerConfig); err != nil {
			h.logger.Error("Failed to add BGP peer",
				zap.String("address", peer.Address),
				zap.Error(err),
			)
		}
	}

	h.logger.Info("BGP server started successfully")
	return nil
}

// announceRoute announces a route for a VIP (IPv4 or IPv6)
func (h *BGPHandler) announceRoute(ctx context.Context, ip net.IP, config *pb.BGPConfig, isIPv6 bool) error {
	var prefixLen uint32
	var family *api.Family

	if isIPv6 {
		prefixLen = 128
		family = &api.Family{
			Afi:  api.Family_AFI_IP6,
			Safi: api.Family_SAFI_UNICAST,
		}
	} else {
		prefixLen = 32
		family = &api.Family{
			Afi:  api.Family_AFI_IP,
			Safi: api.Family_SAFI_UNICAST,
		}
	}

	prefix := fmt.Sprintf("%s/%d", ip.String(), prefixLen)
	h.logger.Info("Announcing BGP route", zap.String("prefix", prefix), zap.Bool("ipv6", isIPv6))

	attrs := []*anypb.Any{}

	// Origin attribute
	originAttr, err := anypb.New(&api.OriginAttribute{
		Origin: 0, // IGP
	})
	if err != nil {
		return fmt.Errorf("failed to marshal origin attribute: %w", err)
	}
	attrs = append(attrs, originAttr)

	// Next hop attribute
	if isIPv6 {
		// For IPv6 BGP, use MP_REACH_NLRI with next-hop
		mpReachAttr, err := anypb.New(&api.MpReachNLRIAttribute{
			Family: family,
			NextHops: []string{
				config.RouterId,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to marshal MP_REACH_NLRI attribute: %w", err)
		}
		attrs = append(attrs, mpReachAttr)
	} else {
		nexthopAttr, err := anypb.New(&api.NextHopAttribute{
			NextHop: config.RouterId,
		})
		if err != nil {
			return fmt.Errorf("failed to marshal next-hop attribute: %w", err)
		}
		attrs = append(attrs, nexthopAttr)
	}

	// Local preference for iBGP
	if config.LocalPreference > 0 {
		lpAttr, err := anypb.New(&api.LocalPrefAttribute{
			LocalPref: config.LocalPreference,
		})
		if err != nil {
			return fmt.Errorf("failed to marshal local-pref attribute: %w", err)
		}
		attrs = append(attrs, lpAttr)
	}

	// Build NLRI
	nlri, err := anypb.New(&api.IPAddressPrefix{
		PrefixLen: prefixLen,
		Prefix:    ip.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to marshal NLRI: %w", err)
	}

	_, err = h.bgpServer.AddPath(ctx, &api.AddPathRequest{
		TableType: api.TableType_GLOBAL,
		Path: &api.Path{
			Nlri:   nlri,
			Pattrs: attrs,
			Family: family,
		},
	})

	if err != nil {
		return fmt.Errorf("failed to add path: %w", err)
	}

	h.logger.Info("BGP route announced successfully", zap.String("prefix", prefix))
	return nil
}

// withdrawRoute withdraws a route for a VIP (IPv4 or IPv6)
func (h *BGPHandler) withdrawRoute(ctx context.Context, ip net.IP, config *pb.BGPConfig, isIPv6 bool) error {
	var prefixLen uint32
	var family *api.Family

	if isIPv6 {
		prefixLen = 128
		family = &api.Family{
			Afi:  api.Family_AFI_IP6,
			Safi: api.Family_SAFI_UNICAST,
		}
	} else {
		prefixLen = 32
		family = &api.Family{
			Afi:  api.Family_AFI_IP,
			Safi: api.Family_SAFI_UNICAST,
		}
	}

	prefix := fmt.Sprintf("%s/%d", ip.String(), prefixLen)
	h.logger.Info("Withdrawing BGP route", zap.String("prefix", prefix))

	nlri, err := anypb.New(&api.IPAddressPrefix{
		PrefixLen: prefixLen,
		Prefix:    ip.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to marshal NLRI: %w", err)
	}

	// Build path attributes matching what was announced, so GoBGP can
	// identify the exact path to delete. Without these, DeletePath fails
	// with "nexthop not found".
	attrs := []*anypb.Any{}

	originAttr, err := anypb.New(&api.OriginAttribute{
		Origin: 0, // IGP
	})
	if err != nil {
		return fmt.Errorf("failed to marshal origin attribute for withdrawal: %w", err)
	}
	attrs = append(attrs, originAttr)

	if config != nil {
		if isIPv6 {
			mpReachAttr, err := anypb.New(&api.MpReachNLRIAttribute{
				Family:   family,
				NextHops: []string{config.RouterId},
			})
			if err != nil {
				return fmt.Errorf("failed to marshal MP_REACH_NLRI for withdrawal: %w", err)
			}
			attrs = append(attrs, mpReachAttr)
		} else {
			nexthopAttr, err := anypb.New(&api.NextHopAttribute{
				NextHop: config.RouterId,
			})
			if err != nil {
				return fmt.Errorf("failed to marshal nexthop for withdrawal: %w", err)
			}
			attrs = append(attrs, nexthopAttr)
		}
	}

	err = h.bgpServer.DeletePath(ctx, &api.DeletePathRequest{
		TableType: api.TableType_GLOBAL,
		Path: &api.Path{
			Nlri:   nlri,
			Pattrs: attrs,
			Family: family,
		},
	})

	if err != nil {
		return fmt.Errorf("failed to withdraw path: %w", err)
	}

	h.logger.Info("BGP route withdrawn successfully", zap.String("prefix", prefix))
	return nil
}

// Shutdown gracefully shuts down the BGP handler
func (h *BGPHandler) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.bfdManager != nil {
		h.bfdManager.Stop()
	}

	if h.bgpServer != nil {
		h.logger.Info("Shutting down BGP server")
		h.bgpServer.Stop()
		h.bgpServer = nil
	}

	h.activeVIPs = make(map[string]*BGPVIPState)
	h.started = false
}
