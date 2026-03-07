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

	nrv1 "github.com/azrtydxb/novaedge/api/novaroute/v1"
	"github.com/azrtydxb/novaedge/internal/agent/metrics"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

var (
	errInvalidAddress = errors.New("invalid address")
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
		_ = h.conn.Close()
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
		_ = h.conn.Close()
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
		_ = h.conn.Close()
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
		_ = h.conn.Close()
		h.conn = nil
		h.client = nil
		return false
	}

	h.logger.Info("NovaRoute reconnected and re-registered")

	// Replay all active VIP assignments to restore prefix announcements.
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
	case nrv1.EventType_EVENT_TYPE_UNSPECIFIED,
		nrv1.EventType_EVENT_TYPE_PREFIX_ADVERTISED,
		nrv1.EventType_EVENT_TYPE_PREFIX_WITHDRAWN,
		nrv1.EventType_EVENT_TYPE_OSPF_NEIGHBOR_UP,
		nrv1.EventType_EVENT_TYPE_OSPF_NEIGHBOR_DOWN,
		nrv1.EventType_EVENT_TYPE_FRR_CONNECTED,
		nrv1.EventType_EVENT_TYPE_OWNER_REGISTERED,
		nrv1.EventType_EVENT_TYPE_OWNER_DEREGISTERED,
		nrv1.EventType_EVENT_TYPE_POLICY_VIOLATION,
		nrv1.EventType_EVENT_TYPE_BGP_CONFIG_CHANGED:
		h.logger.Debug("NovaRoute event",
			zap.String("type", ev.Type.String()),
			zap.String("detail", ev.Detail),
		)
	default:
		h.logger.Debug("NovaRoute event",
			zap.String("type", ev.Type.String()),
			zap.String("detail", ev.Detail),
		)
	}
}

// AddVIP announces a VIP via NovaRoute's BGP by binding the address to
// loopback and advertising the prefix. BGP peers are managed by NovaRoute.
func (h *NovaRouteBGPHandler) AddVIP(ctx context.Context, assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Store the assignment for reconnect replay.
	h.lastAssignments[assignment.VipName] = assignment

	return h.addVIPLocked(ctx, assignment)
}

// addVIPLocked performs the actual AddVIP logic. Caller must hold h.mu.
//
// When using NovaRoute, BGP peering and global config (AS, router-id, peers,
// BFD) are already managed by NovaNET/NovaRoute. This handler only:
//   - Binds the VIP address to loopback
//   - Advertises the prefix via NovaRoute
func (h *NovaRouteBGPHandler) addVIPLocked(ctx context.Context, assignment *pb.VIPAssignment) error {
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
		zap.Bool("ipv6", isIPv6),
	)

	// Bind VIP to loopback so the node can receive traffic.
	if err := addLoopbackAddr(assignment.Address); err != nil {
		h.logger.Warn("Failed to bind VIP to loopback",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
	}

	// Build prefix attributes. Use BGP config for next-hop, communities,
	// and local-pref if available — these are per-prefix attributes, not
	// peering config.
	attrs := &nrv1.PrefixAttributes{}
	if assignment.BgpConfig != nil {
		attrs.NextHop = assignment.BgpConfig.RouterId
		attrs.LocalPreference = assignment.BgpConfig.LocalPreference
		attrs.Communities = assignment.BgpConfig.Communities
	}

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

	h.activeVIPs[assignment.VipName] = &novaRouteVIPState{
		address: assignment.Address,
		addedAt: time.Now(),
		isIPv6:  isIPv6,
	}

	metrics.BGPAnnouncedRoutes.Set(float64(len(h.activeVIPs)))

	h.logger.Info("VIP announced via NovaRoute BGP successfully",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)
	return nil
}

// RemoveVIP withdraws a VIP from NovaRoute BGP.
// Only withdraws the prefix and unbinds the loopback address — BGP peers
// and BFD sessions are managed by NovaNET/NovaRoute, not NovaEdge.
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
			return fmt.Errorf("%w: %s", errInvalidAddress, cidr)
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

// removeLoopbackAddr removes a VIP address from the loopback interface.
func removeLoopbackAddr(cidr string) error {
	if !strings.Contains(cidr, "/") {
		ip := net.ParseIP(cidr)
		if ip == nil {
			return fmt.Errorf("%w: %s", errInvalidAddress, cidr)
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
