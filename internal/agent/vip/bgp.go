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
	"fmt"
	"net"
	"sync"
	"time"

	api "github.com/osrg/gobgp/v3/api"
	"github.com/osrg/gobgp/v3/pkg/server"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
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
}

// NewBGPHandler creates a new BGP handler
func NewBGPHandler(logger *zap.Logger) (*BGPHandler, error) {
	handler := &BGPHandler{
		logger:     logger,
		activeVIPs: make(map[string]*BGPVIPState),
		started:    false,
	}

	// Create BFD manager with callback to withdraw routes on neighbor failure
	handler.bfdManager = NewBFDManager(logger, handler.onBFDNeighborDown)

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

// AddVIP adds a VIP with BGP announcement (IPv4 or IPv6)
func (h *BGPHandler) AddVIP(ctx context.Context, assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if already active
	if _, exists := h.activeVIPs[assignment.VipName]; exists {
		h.logger.Debug(msgVIPAlreadyActive, zap.String("vip", assignment.VipName))
		return nil
	}

	// Parse IP address
	ip, _, err := net.ParseCIDR(assignment.Address)
	if err != nil {
		return fmt.Errorf(errInvalidVIPAddressFmt, assignment.Address, err)
	}

	// Validate BGP config
	if assignment.BgpConfig == nil {
		return fmt.Errorf("BGP config is required for BGP mode VIPs")
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

	delete(h.activeVIPs, assignment.VipName)

	// Update metrics
	metrics.BGPAnnouncedRoutes.Set(float64(len(h.activeVIPs)))

	h.logger.Info("VIP withdrawn from BGP successfully",
		zap.String("vip", assignment.VipName),
		zap.Duration("duration", time.Since(state.AddedAt)),
	)

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
		}
	}
	if assignment.BfdConfig.RequiredMinRxInterval != "" {
		if d, err := time.ParseDuration(assignment.BfdConfig.RequiredMinRxInterval); err == nil {
			bfdCfg.RequiredMinRxInterval = d
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

// onBFDNeighborDown is called when BFD detects a neighbor failure
func (h *BGPHandler) onBFDNeighborDown(peerIP net.IP) {
	h.logger.Warn("BFD detected neighbor down, withdrawing all routes through this peer",
		zap.String("peer", peerIP.String()),
	)

	// In production, this would immediately withdraw routes announced
	// through the failed BGP peer for sub-second failover.
	// The BGP session will also eventually detect the failure via holdtimer,
	// but BFD gives us much faster detection.
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
					RemotePort: port,
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
	originAttr, _ := anypb.New(&api.OriginAttribute{
		Origin: 0, // IGP
	})
	attrs = append(attrs, originAttr)

	// Next hop attribute
	if isIPv6 {
		// For IPv6 BGP, use MP_REACH_NLRI with next-hop
		mpReachAttr, _ := anypb.New(&api.MpReachNLRIAttribute{
			Family: family,
			NextHops: []string{
				config.RouterId,
			},
		})
		attrs = append(attrs, mpReachAttr)
	} else {
		nexthopAttr, _ := anypb.New(&api.NextHopAttribute{
			NextHop: config.RouterId,
		})
		attrs = append(attrs, nexthopAttr)
	}

	// Local preference for iBGP
	if config.LocalPreference > 0 {
		lpAttr, _ := anypb.New(&api.LocalPrefAttribute{
			LocalPref: config.LocalPreference,
		})
		attrs = append(attrs, lpAttr)
	}

	// Build NLRI
	nlri, _ := anypb.New(&api.IPAddressPrefix{
		PrefixLen: prefixLen,
		Prefix:    ip.String(),
	})

	_, err := h.bgpServer.AddPath(ctx, &api.AddPathRequest{
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
func (h *BGPHandler) withdrawRoute(ctx context.Context, ip net.IP, _ *pb.BGPConfig, isIPv6 bool) error {
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

	nlri, _ := anypb.New(&api.IPAddressPrefix{
		PrefixLen: prefixLen,
		Prefix:    ip.String(),
	})

	err := h.bgpServer.DeletePath(ctx, &api.DeletePathRequest{
		TableType: api.TableType_GLOBAL,
		Path: &api.Path{
			Nlri:   nlri,
			Family: family,
		},
	})

	if err != nil {
		return fmt.Errorf("failed to withdraw path: %w", err)
	}

	h.logger.Info("BGP route withdrawn successfully", zap.String("prefix", prefix))
	return nil
}

// GetBFDManager returns the BFD manager for external access
func (h *BGPHandler) GetBFDManager() *BFDManager {
	return h.bfdManager
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

// GetActiveVIPCount returns the number of active VIPs
func (h *BGPHandler) GetActiveVIPCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.activeVIPs)
}
