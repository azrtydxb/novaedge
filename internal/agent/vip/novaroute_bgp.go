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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	nrv1 "github.com/piwi3910/novaedge/api/novaroute/v1"
	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// NovaRouteBGPHandler delegates BGP VIP announcements to a NovaRoute agent
// running as a sidecar or DaemonSet, communicating over a Unix gRPC socket.
type NovaRouteBGPHandler struct {
	logger *zap.Logger
	mu     sync.Mutex

	socketPath string
	ownerName  string
	ownerToken string

	conn      *grpc.ClientConn
	client    nrv1.RouteControlClient
	cancelCtx context.CancelFunc // cancels the connection loop goroutine

	// configuredAS tracks the local AS that was last configured via the
	// ConfigureBGP RPC so we only reconfigure when the AS actually changes.
	configuredAS uint32

	// Track active VIPs for metrics and cleanup.
	activeVIPs map[string]*novaRouteVIPState

	// lastAssignments stores the most recent VIPAssignment for each VIP so
	// we can replay them after a reconnect to NovaRoute.
	lastAssignments map[string]*pb.VIPAssignment
}

type novaRouteVIPState struct {
	address string
	addedAt time.Time
	isIPv6  bool
	// peerAddrs tracks which peers were applied for this VIP so we can
	// remove them when the VIP is withdrawn.
	peerAddrs []string
}

// NewNovaRouteBGPHandler creates a handler that talks to NovaRoute.
// socketPath is the Unix domain socket (e.g. /run/novaroute/novaroute.sock).
func NewNovaRouteBGPHandler(logger *zap.Logger, socketPath, ownerName, ownerToken string) *NovaRouteBGPHandler {
	return &NovaRouteBGPHandler{
		logger:          logger,
		socketPath:      socketPath,
		ownerName:       ownerName,
		ownerToken:      ownerToken,
		activeVIPs:      make(map[string]*novaRouteVIPState),
		lastAssignments: make(map[string]*pb.VIPAssignment),
	}
}

// Start connects to the NovaRoute gRPC socket and registers as an owner.
func (h *NovaRouteBGPHandler) Start(ctx context.Context) error {
	h.logger.Info("Starting NovaRoute BGP backend",
		zap.String("socket", h.socketPath),
		zap.String("owner", h.ownerName),
	)

	if err := h.dial(ctx); err != nil {
		return err
	}

	// Register as owner so NovaRoute knows our intents.
	if err := h.register(ctx); err != nil {
		h.conn.Close()
		h.conn = nil
		h.client = nil
		return err
	}

	h.logger.Info("Registered with NovaRoute", zap.String("owner", h.ownerName))

	// Start background connection loop for event monitoring and reconnection.
	loopCtx, loopCancel := context.WithCancel(ctx)
	h.cancelCtx = loopCancel
	go h.connectionLoop(loopCtx)

	return nil
}

// dial creates a new gRPC connection to the NovaRoute socket.
func (h *NovaRouteBGPHandler) dial(_ context.Context) error {
	conn, err := grpc.NewClient(
		"unix://"+h.socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return fmt.Errorf("novaroute: dial %s: %w", h.socketPath, err)
	}

	h.conn = conn
	h.client = nrv1.NewRouteControlClient(conn)
	return nil
}

// register sends a Register RPC to NovaRoute.
func (h *NovaRouteBGPHandler) register(ctx context.Context) error {
	_, err := h.client.Register(ctx, &nrv1.RegisterRequest{
		Owner:           h.ownerName,
		Token:           h.ownerToken,
		ReassertIntents: true,
	})
	if err != nil {
		return fmt.Errorf("novaroute: register owner %q: %w", h.ownerName, err)
	}
	return nil
}

// Stop deregisters from NovaRoute and closes the gRPC connection.
func (h *NovaRouteBGPHandler) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cancelCtx != nil {
		h.cancelCtx()
	}

	if h.client != nil {
		_, err := h.client.Deregister(ctx, &nrv1.DeregisterRequest{
			Owner:       h.ownerName,
			Token:       h.ownerToken,
			WithdrawAll: true,
		})
		if err != nil {
			h.logger.Warn("Failed to deregister from NovaRoute", zap.Error(err))
		}
	}

	if h.conn != nil {
		h.conn.Close()
	}

	h.logger.Info("NovaRoute BGP handler stopped")
	return nil
}

// connectionLoop monitors the NovaRoute event stream and automatically
// reconnects when the connection is lost. On reconnect it re-registers
// and replays all active VIP assignments.
func (h *NovaRouteBGPHandler) connectionLoop(ctx context.Context) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		// Open the event stream — this serves as both event delivery and
		// connection health monitoring.
		stream, err := h.client.StreamEvents(ctx, &nrv1.StreamEventsRequest{
			OwnerFilter: h.ownerName,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			h.logger.Warn("NovaRoute event stream failed, reconnecting",
				zap.Error(err),
				zap.Duration("backoff", backoff),
			)
			if !h.reconnect(ctx) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
					backoff = min(backoff*2, maxBackoff)
					continue
				}
			}
			backoff = time.Second
			continue
		}

		backoff = time.Second
		h.logger.Info("NovaRoute event stream connected")

		// Read events until the stream breaks.
		for {
			ev, recvErr := stream.Recv()
			if recvErr != nil {
				if ctx.Err() != nil {
					return
				}
				h.logger.Warn("NovaRoute event stream error, will reconnect",
					zap.Error(recvErr),
				)
				break
			}
			h.handleEvent(ev)
		}

		// Stream broke — reconnect.
		h.logger.Info("NovaRoute connection lost, attempting reconnect")
		if h.reconnect(ctx) {
			backoff = time.Second
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			backoff = min(backoff*2, maxBackoff)
		}
	}
}

// reconnect closes the old gRPC connection, creates a new one, re-registers,
// and replays all active VIP assignments so that NovaRoute's FRR state is
// fully restored after a NovaRoute pod restart.
func (h *NovaRouteBGPHandler) reconnect(ctx context.Context) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Close old connection.
	if h.conn != nil {
		h.conn.Close()
		h.conn = nil
		h.client = nil
	}

	// Create new connection.
	if err := h.dial(ctx); err != nil {
		h.logger.Warn("NovaRoute reconnect: dial failed", zap.Error(err))
		return false
	}

	// Re-register as owner.
	if err := h.register(ctx); err != nil {
		h.logger.Warn("NovaRoute reconnect: register failed", zap.Error(err))
		h.conn.Close()
		h.conn = nil
		h.client = nil
		return false
	}

	h.logger.Info("NovaRoute reconnected and re-registered")

	// Reset configuredAS so ensureBGPGlobal will reconfigure on the new
	// NovaRoute instance (which has no state from the previous one).
	h.configuredAS = 0

	// Replay all active VIP assignments to restore BGP state in FRR.
	replayed := 0
	for vipName, assignment := range h.lastAssignments {
		// Clear the activeVIPs entry so AddVIP doesn't try to withdraw first.
		delete(h.activeVIPs, vipName)

		if err := h.addVIPLocked(ctx, assignment); err != nil {
			h.logger.Error("NovaRoute reconnect: failed to replay VIP",
				zap.String("vip", vipName),
				zap.Error(err),
			)
			continue
		}
		replayed++
	}

	h.logger.Info("NovaRoute VIP state restored after reconnect",
		zap.Int("replayed", replayed),
		zap.Int("total", len(h.lastAssignments)),
	)

	return true
}

// handleEvent processes a single event from the NovaRoute event stream.
func (h *NovaRouteBGPHandler) handleEvent(ev *nrv1.RouteEvent) {
	switch ev.Type {
	case nrv1.EventType_EVENT_TYPE_PEER_DOWN:
		h.logger.Warn("BGP peer down", zap.String("detail", ev.Detail))
	case nrv1.EventType_EVENT_TYPE_PEER_UP:
		h.logger.Info("BGP peer up", zap.String("detail", ev.Detail))
	case nrv1.EventType_EVENT_TYPE_BFD_DOWN:
		h.logger.Warn("BFD session down", zap.String("detail", ev.Detail))
	case nrv1.EventType_EVENT_TYPE_BFD_UP:
		h.logger.Info("BFD session up", zap.String("detail", ev.Detail))
	case nrv1.EventType_EVENT_TYPE_FRR_DISCONNECTED:
		h.logger.Error("NovaRoute lost FRR connection", zap.String("detail", ev.Detail))
	default:
		h.logger.Debug("NovaRoute event",
			zap.String("type", ev.Type.String()),
			zap.String("detail", ev.Detail),
		)
	}
}

// AddVIP announces a VIP via NovaRoute's BGP. It:
//  1. Configures BGP peers through NovaRoute
//  2. Binds the VIP address to loopback (same as GoBGP handler)
//  3. Advertises the prefix via NovaRoute
//  4. Configures BFD sessions if enabled
func (h *NovaRouteBGPHandler) AddVIP(ctx context.Context, assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Store the assignment for reconnect replay.
	h.lastAssignments[assignment.VipName] = assignment

	return h.addVIPLocked(ctx, assignment)
}

// addVIPLocked performs the actual AddVIP logic. Caller must hold h.mu.
func (h *NovaRouteBGPHandler) addVIPLocked(ctx context.Context, assignment *pb.VIPAssignment) error {
	if assignment.BgpConfig == nil {
		return fmt.Errorf("BGP config is required for BGP mode VIPs")
	}

	ip, _, err := net.ParseCIDR(assignment.Address)
	if err != nil {
		return fmt.Errorf("invalid VIP address %q: %w", assignment.Address, err)
	}
	isIPv6 := ip.To4() == nil

	// Check for reconfiguration.
	if _, exists := h.activeVIPs[assignment.VipName]; exists {
		h.logger.Info("VIP already active via NovaRoute, reconfiguring",
			zap.String("vip", assignment.VipName),
		)
		// Withdraw and re-announce (NovaRoute handles idempotent updates).
		if err := h.withdrawPrefix(ctx, assignment.Address); err != nil {
			h.logger.Warn("Failed to withdraw prefix during reconfiguration",
				zap.String("vip", assignment.VipName),
				zap.Error(err),
			)
		}
	}

	h.logger.Info("Adding VIP via NovaRoute BGP",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
		zap.Uint32("local_as", assignment.BgpConfig.LocalAs),
		zap.Bool("ipv6", isIPv6),
	)

	// Configure BGP global settings (AS number + router-id) via NovaRoute
	// if the controller-provided AS differs from what we last configured.
	// This replaces the DaemonSet env var workaround with proper code support.
	if err := h.ensureBGPGlobal(ctx, assignment.BgpConfig); err != nil {
		return fmt.Errorf("novaroute: configure BGP global: %w", err)
	}

	// Configure BGP peers.
	var peerAddrs []string
	for _, peer := range assignment.BgpConfig.Peers {
		afs := []nrv1.AddressFamily{nrv1.AddressFamily_ADDRESS_FAMILY_IPV4_UNICAST}
		peerIP := net.ParseIP(peer.Address)
		if peerIP != nil && peerIP.To4() == nil {
			afs = []nrv1.AddressFamily{nrv1.AddressFamily_ADDRESS_FAMILY_IPV6_UNICAST}
		}

		peerType := nrv1.PeerType_PEER_TYPE_EXTERNAL
		if peer.As == assignment.BgpConfig.LocalAs {
			peerType = nrv1.PeerType_PEER_TYPE_INTERNAL
		}

		_, err := h.client.ApplyPeer(ctx, &nrv1.ApplyPeerRequest{
			Owner: h.ownerName,
			Token: h.ownerToken,
			Peer: &nrv1.BGPPeer{
				NeighborAddress: peer.Address,
				RemoteAs:        peer.As,
				PeerType:        peerType,
				AddressFamilies: afs,
			},
		})
		if err != nil {
			return fmt.Errorf("novaroute: apply peer %s: %w", peer.Address, err)
		}
		peerAddrs = append(peerAddrs, peer.Address)
	}

	// Bind VIP to loopback so the node can receive traffic.
	if err := addLoopbackAddr(assignment.Address); err != nil {
		h.logger.Warn("Failed to bind VIP to loopback",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
	}

	// Build prefix attributes.
	attrs := &nrv1.PrefixAttributes{
		NextHop: assignment.BgpConfig.RouterId,
	}
	attrs.LocalPreference = assignment.BgpConfig.LocalPreference
	attrs.Communities = assignment.BgpConfig.Communities

	// Advertise the prefix.
	_, err = h.client.AdvertisePrefix(ctx, &nrv1.AdvertisePrefixRequest{
		Owner:      h.ownerName,
		Token:      h.ownerToken,
		Prefix:     assignment.Address,
		Protocol:   nrv1.Protocol_PROTOCOL_BGP,
		Attributes: attrs,
	})
	if err != nil {
		return fmt.Errorf("novaroute: advertise prefix %s: %w", assignment.Address, err)
	}

	// Configure BFD sessions if enabled.
	if assignment.BfdConfig != nil && assignment.BfdConfig.Enabled {
		minRxMs := parseDurationMs(assignment.BfdConfig.RequiredMinRxInterval, 300)
		minTxMs := parseDurationMs(assignment.BfdConfig.DesiredMinTxInterval, 300)
		detectMult := uint32(assignment.BfdConfig.DetectMultiplier)

		for _, peer := range assignment.BgpConfig.Peers {
			_, bfdErr := h.client.EnableBFD(ctx, &nrv1.EnableBFDRequest{
				Owner:            h.ownerName,
				Token:            h.ownerToken,
				PeerAddress:      peer.Address,
				MinRxMs:          minRxMs,
				MinTxMs:          minTxMs,
				DetectMultiplier: detectMult,
			})
			if bfdErr != nil {
				h.logger.Warn("Failed to enable BFD via NovaRoute",
					zap.String("peer", peer.Address),
					zap.Error(bfdErr),
				)
			}
		}
	}

	h.activeVIPs[assignment.VipName] = &novaRouteVIPState{
		address:   assignment.Address,
		addedAt:   time.Now(),
		isIPv6:    isIPv6,
		peerAddrs: peerAddrs,
	}

	metrics.BGPAnnouncedRoutes.Set(float64(len(h.activeVIPs)))

	h.logger.Info("VIP announced via NovaRoute BGP successfully",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)
	return nil
}

// RemoveVIP withdraws a VIP from NovaRoute BGP.
func (h *NovaRouteBGPHandler) RemoveVIP(ctx context.Context, assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.activeVIPs[assignment.VipName]
	if !exists {
		h.logger.Debug("VIP not active in NovaRoute", zap.String("vip", assignment.VipName))
		return nil
	}

	h.logger.Info("Removing VIP via NovaRoute BGP",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)

	// Withdraw the prefix.
	if err := h.withdrawPrefix(ctx, state.address); err != nil {
		h.logger.Warn("Failed to withdraw prefix from NovaRoute",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
	}

	// Disable BFD sessions.
	if assignment.BfdConfig != nil && assignment.BfdConfig.Enabled {
		for _, peerAddr := range state.peerAddrs {
			_, bfdErr := h.client.DisableBFD(ctx, &nrv1.DisableBFDRequest{
				Owner:       h.ownerName,
				Token:       h.ownerToken,
				PeerAddress: peerAddr,
			})
			if bfdErr != nil {
				h.logger.Warn("Failed to disable BFD via NovaRoute",
					zap.String("peer", peerAddr),
					zap.Error(bfdErr),
				)
			}
		}
	}

	// Remove peers for this VIP.
	for _, peerAddr := range state.peerAddrs {
		_, peerErr := h.client.RemovePeer(ctx, &nrv1.RemovePeerRequest{
			Owner:           h.ownerName,
			Token:           h.ownerToken,
			NeighborAddress: peerAddr,
		})
		if peerErr != nil {
			h.logger.Warn("Failed to remove peer via NovaRoute",
				zap.String("peer", peerAddr),
				zap.Error(peerErr),
			)
		}
	}

	// Remove VIP from loopback.
	if err := removeLoopbackAddr(state.address); err != nil {
		h.logger.Warn("Failed to remove VIP from loopback",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
	}

	delete(h.activeVIPs, assignment.VipName)
	delete(h.lastAssignments, assignment.VipName)
	metrics.BGPAnnouncedRoutes.Set(float64(len(h.activeVIPs)))

	h.logger.Info("VIP withdrawn via NovaRoute BGP",
		zap.String("vip", assignment.VipName),
		zap.Duration("duration", time.Since(state.addedAt)),
	)
	return nil
}

// ensureBGPGlobal calls the ConfigureBGP RPC to set the local AS and router-id
// in NovaRoute. It only makes the call when the AS has changed, avoiding
// unnecessary reconfiguration. This is the code-level equivalent of the
// GoBGP handler's startBGPServer() — it uses the controller-provided BgpConfig
// to dynamically configure the local BGP AS per node.
func (h *NovaRouteBGPHandler) ensureBGPGlobal(ctx context.Context, bgpCfg *pb.BGPConfig) error {
	desiredAS := bgpCfg.LocalAs
	routerID := bgpCfg.RouterId

	if desiredAS == 0 {
		return nil // No AS configured, nothing to do.
	}

	if h.configuredAS == desiredAS {
		return nil // Already configured with this AS.
	}

	h.logger.Info("Configuring BGP global via NovaRoute",
		zap.Uint32("desired_as", desiredAS),
		zap.String("router_id", routerID),
		zap.Uint32("previous_as", h.configuredAS),
	)

	resp, err := h.client.ConfigureBGP(ctx, &nrv1.ConfigureBGPRequest{
		Owner:    h.ownerName,
		Token:    h.ownerToken,
		LocalAs:  desiredAS,
		RouterId: routerID,
	})
	if err != nil {
		return fmt.Errorf("ConfigureBGP RPC (AS=%d, router_id=%s): %w", desiredAS, routerID, err)
	}

	h.configuredAS = desiredAS

	h.logger.Info("BGP global configured via NovaRoute",
		zap.Uint32("local_as", desiredAS),
		zap.String("router_id", routerID),
		zap.Uint32("previous_as", resp.PreviousAs),
		zap.String("previous_router_id", resp.PreviousRouterId),
	)

	return nil
}

// withdrawPrefix sends a WithdrawPrefix RPC to NovaRoute.
func (h *NovaRouteBGPHandler) withdrawPrefix(ctx context.Context, prefix string) error {
	_, err := h.client.WithdrawPrefix(ctx, &nrv1.WithdrawPrefixRequest{
		Owner:    h.ownerName,
		Token:    h.ownerToken,
		Prefix:   prefix,
		Protocol: nrv1.Protocol_PROTOCOL_BGP,
	})
	return err
}

// addLoopbackAddr binds a VIP address to the loopback interface.
func addLoopbackAddr(cidr string) error {
	// Ensure CIDR format.
	if !strings.Contains(cidr, "/") {
		ip := net.ParseIP(cidr)
		if ip == nil {
			return fmt.Errorf("invalid address: %s", cidr)
		}
		if ip.To4() != nil {
			cidr += "/32"
		} else {
			cidr += "/128"
		}
	}

	link, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("get loopback: %w", err)
	}
	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("parse address %s: %w", cidr, err)
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		if errors.Is(err, syscall.EEXIST) {
			return nil
		}
		return err
	}
	return nil
}

// parseDurationMs parses a Go-style duration string (e.g. "300ms") and returns
// the value in milliseconds. If parsing fails it returns the provided default.
func parseDurationMs(s string, defaultMs uint32) uint32 {
	if s == "" {
		return defaultMs
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultMs
	}
	return uint32(d.Milliseconds())
}

// removeLoopbackAddr removes a VIP address from the loopback interface.
func removeLoopbackAddr(cidr string) error {
	if !strings.Contains(cidr, "/") {
		ip := net.ParseIP(cidr)
		if ip == nil {
			return fmt.Errorf("invalid address: %s", cidr)
		}
		if ip.To4() != nil {
			cidr += "/32"
		} else {
			cidr += "/128"
		}
	}

	link, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("get loopback: %w", err)
	}
	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("parse address %s: %w", cidr, err)
	}
	if err := netlink.AddrDel(link, addr); err != nil {
		if errors.Is(err, syscall.EADDRNOTAVAIL) {
			return nil
		}
		return err
	}
	return nil
}
