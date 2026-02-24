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

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	nrv1 "github.com/piwi3910/novaedge/api/novaroute/v1"
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

	conn   *grpc.ClientConn
	client nrv1.RouteControlClient

	// Track active VIPs for metrics and cleanup.
	activeVIPs map[string]*novaRouteVIPState
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
		logger:     logger,
		socketPath: socketPath,
		ownerName:  ownerName,
		ownerToken: ownerToken,
		activeVIPs: make(map[string]*novaRouteVIPState),
	}
}

// Start connects to the NovaRoute gRPC socket and registers as an owner.
func (h *NovaRouteBGPHandler) Start(ctx context.Context) error {
	h.logger.Info("Starting NovaRoute BGP backend",
		zap.String("socket", h.socketPath),
		zap.String("owner", h.ownerName),
	)

	conn, err := grpc.NewClient(
		"unix://"+h.socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("novaroute: dial %s: %w", h.socketPath, err)
	}

	h.conn = conn
	h.client = nrv1.NewRouteControlClient(conn)

	// Register as owner so NovaRoute knows our intents.
	_, err = h.client.Register(ctx, &nrv1.RegisterRequest{
		Owner:           h.ownerName,
		Token:           h.ownerToken,
		ReassertIntents: true,
	})
	if err != nil {
		h.conn.Close()
		return fmt.Errorf("novaroute: register owner %q: %w", h.ownerName, err)
	}

	h.logger.Info("Registered with NovaRoute", zap.String("owner", h.ownerName))
	return nil
}

// AddVIP announces a VIP via NovaRoute's BGP. It:
//  1. Configures BGP peers through NovaRoute
//  2. Binds the VIP address to loopback (same as GoBGP handler)
//  3. Advertises the prefix via NovaRoute
//  4. Configures BFD sessions if enabled
func (h *NovaRouteBGPHandler) AddVIP(ctx context.Context, assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

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

	// Remove VIP from loopback.
	if err := removeLoopbackAddr(state.address); err != nil {
		h.logger.Warn("Failed to remove VIP from loopback",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
	}

	delete(h.activeVIPs, assignment.VipName)
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
