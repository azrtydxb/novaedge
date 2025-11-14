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

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// BGPHandler manages BGP VIP mode
type BGPHandler struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// BGP server instance
	bgpServer *server.BgpServer

	// Active VIPs and their configurations
	activeVIPs map[string]*BGPVIPState

	// BGP server started flag
	started bool
}

// BGPVIPState tracks the state of a BGP VIP
type BGPVIPState struct {
	Assignment *pb.VIPAssignment
	IP         net.IP
	AddedAt    time.Time
	Announced  bool
}

// NewBGPHandler creates a new BGP handler
func NewBGPHandler(logger *zap.Logger) (*BGPHandler, error) {
	return &BGPHandler{
		logger:     logger,
		activeVIPs: make(map[string]*BGPVIPState),
		started:    false,
	}, nil
}

// Start starts the BGP handler
func (h *BGPHandler) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.started {
		return nil
	}

	h.logger.Info("Starting BGP handler")

	// BGP server will be started when first VIP is added
	// (we need config from VIP assignment)
	h.started = true

	return nil
}

// AddVIP adds a VIP with BGP announcement
func (h *BGPHandler) AddVIP(assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if already active
	if _, exists := h.activeVIPs[assignment.VipName]; exists {
		h.logger.Debug("VIP already active", zap.String("vip", assignment.VipName))
		return nil
	}

	// Parse IP address
	ip, _, err := net.ParseCIDR(assignment.Address)
	if err != nil {
		return fmt.Errorf("invalid VIP address %s: %w", assignment.Address, err)
	}

	// Validate BGP config
	if assignment.BgpConfig == nil {
		return fmt.Errorf("BGP config is required for BGP mode VIPs")
	}

	h.logger.Info("Adding VIP with BGP announcement",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
		zap.Uint32("local_as", assignment.BgpConfig.LocalAs),
	)

	// Start BGP server if not already started
	if h.bgpServer == nil {
		if err := h.startBGPServer(assignment.BgpConfig); err != nil {
			return fmt.Errorf("failed to start BGP server: %w", err)
		}
	}

	// Announce route
	if err := h.announceRoute(ip, assignment.BgpConfig); err != nil {
		h.logger.Warn("Failed to announce BGP route",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
		// Don't fail the whole operation if announcement fails
	}

	// Track VIP state
	h.activeVIPs[assignment.VipName] = &BGPVIPState{
		Assignment: assignment,
		IP:         ip,
		AddedAt:    time.Now(),
		Announced:  true,
	}

	h.logger.Info("VIP announced via BGP successfully",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)

	return nil
}

// RemoveVIP removes a VIP and withdraws BGP announcement
func (h *BGPHandler) RemoveVIP(assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.activeVIPs[assignment.VipName]
	if !exists {
		h.logger.Debug("VIP not active", zap.String("vip", assignment.VipName))
		return nil
	}

	h.logger.Info("Removing VIP and withdrawing BGP route",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)

	// Withdraw route
	if state.Announced && h.bgpServer != nil {
		if err := h.withdrawRoute(state.IP, assignment.BgpConfig); err != nil {
			h.logger.Warn("Failed to withdraw BGP route",
				zap.String("vip", assignment.VipName),
				zap.Error(err),
			)
		}
	}

	delete(h.activeVIPs, assignment.VipName)

	h.logger.Info("VIP withdrawn from BGP successfully",
		zap.String("vip", assignment.VipName),
		zap.Duration("duration", time.Since(state.AddedAt)),
	)

	return nil
}

// startBGPServer initializes and starts the BGP server
func (h *BGPHandler) startBGPServer(config *pb.BGPConfig) error {
	h.logger.Info("Starting BGP server",
		zap.Uint32("local_as", config.LocalAs),
		zap.String("router_id", config.RouterId),
	)

	// Create BGP server
	h.bgpServer = server.NewBgpServer()
	go h.bgpServer.Serve()

	// Global BGP configuration
	globalConfig := &api.StartBgpRequest{
		Global: &api.Global{
			Asn:      config.LocalAs,
			RouterId: config.RouterId,
			// Listen on all interfaces
			ListenAddresses: []string{"0.0.0.0"},
			ListenPort:      179,
		},
	}

	if err := h.bgpServer.StartBgp(context.Background(), globalConfig); err != nil {
		return fmt.Errorf("failed to start BGP server: %w", err)
	}

	// Configure BGP peers
	for _, peer := range config.Peers {
		port := peer.Port
		if port == 0 {
			port = 179
		}

		h.logger.Info("Adding BGP peer",
			zap.String("address", peer.Address),
			zap.Uint32("as", peer.As),
			zap.Uint32("port", port),
		)

		peerConfig := &api.AddPeerRequest{
			Peer: &api.Peer{
				Conf: &api.PeerConf{
					NeighborAddress: peer.Address,
					PeerAsn:         peer.As,
				},
				Transport: &api.Transport{
					RemotePort: port,
				},
			},
		}

		if err := h.bgpServer.AddPeer(context.Background(), peerConfig); err != nil {
			h.logger.Error("Failed to add BGP peer",
				zap.String("address", peer.Address),
				zap.Error(err),
			)
			// Continue with other peers
		}
	}

	h.logger.Info("BGP server started successfully")
	return nil
}

// announceRoute announces a route for a VIP
func (h *BGPHandler) announceRoute(ip net.IP, config *pb.BGPConfig) error {
	// Create /32 prefix for the VIP
	prefix := fmt.Sprintf("%s/32", ip.String())

	h.logger.Info("Announcing BGP route", zap.String("prefix", prefix))

	// Build path attributes as Any protobuf messages
	attrs := []*anypb.Any{}

	// Origin attribute
	originAttr, _ := anypb.New(&api.OriginAttribute{
		Origin: 0, // IGP
	})
	attrs = append(attrs, originAttr)

	// Next hop attribute
	nexthopAttr, _ := anypb.New(&api.NextHopAttribute{
		NextHop: config.RouterId,
	})
	attrs = append(attrs, nexthopAttr)

	// Add local preference for iBGP
	if config.LocalPreference > 0 {
		lpAttr, _ := anypb.New(&api.LocalPrefAttribute{
			LocalPref: config.LocalPreference,
		})
		attrs = append(attrs, lpAttr)
	}

	// Build NLRI for IPv4 /32 prefix
	nlri, _ := anypb.New(&api.IPAddressPrefix{
		PrefixLen: 32,
		Prefix:    ip.String(),
	})

	// Add path to global RIB
	_, err := h.bgpServer.AddPath(context.Background(), &api.AddPathRequest{
		TableType: api.TableType_GLOBAL,
		Path: &api.Path{
			Nlri:   nlri,
			Pattrs: attrs,
			Family: &api.Family{
				Afi:  api.Family_AFI_IP,
				Safi: api.Family_SAFI_UNICAST,
			},
		},
	})

	if err != nil {
		return fmt.Errorf("failed to add path: %w", err)
	}

	h.logger.Info("BGP route announced successfully", zap.String("prefix", prefix))
	return nil
}

// withdrawRoute withdraws a route for a VIP
func (h *BGPHandler) withdrawRoute(ip net.IP, config *pb.BGPConfig) error {
	prefix := fmt.Sprintf("%s/32", ip.String())

	h.logger.Info("Withdrawing BGP route", zap.String("prefix", prefix))

	// Build NLRI for IPv4 /32 prefix
	nlri, _ := anypb.New(&api.IPAddressPrefix{
		PrefixLen: 32,
		Prefix:    ip.String(),
	})

	// Delete path from global RIB
	err := h.bgpServer.DeletePath(context.Background(), &api.DeletePathRequest{
		TableType: api.TableType_GLOBAL,
		Path: &api.Path{
			Nlri: nlri,
			Family: &api.Family{
				Afi:  api.Family_AFI_IP,
				Safi: api.Family_SAFI_UNICAST,
			},
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
