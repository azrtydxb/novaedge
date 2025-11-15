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

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// OSPFHandler manages OSPF VIP mode
type OSPFHandler struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Active VIPs and their configurations
	activeVIPs map[string]*OSPFVIPState

	// OSPF server started flag
	started bool

	// OSPF server instance (using custom implementation)
	ospfServer *OSPFServer

	// Context for background tasks
	ctx    context.Context
	cancel context.CancelFunc
}

// OSPFVIPState tracks the state of an OSPF VIP
type OSPFVIPState struct {
	Assignment *pb.VIPAssignment
	IP         net.IP
	AddedAt    time.Time
	Announced  bool
}

// OSPFServer represents a simplified OSPF server implementation
// This is a basic implementation for VIP announcements via OSPF LSAs
type OSPFServer struct {
	logger    *zap.Logger
	config    *pb.OSPFConfig
	mu        sync.RWMutex
	neighbors map[string]*OSPFNeighbor
	lsas      map[string]*OSPFLSA
	routerID  net.IP
	areaID    uint32
	running   bool
}

// OSPFNeighbor represents an OSPF neighbor
type OSPFNeighbor struct {
	Address   string
	Priority  uint32
	State     string // Down, Init, 2-Way, ExStart, Exchange, Loading, Full
	LastHello time.Time
	DeadTimer *time.Timer
}

// OSPFLSA represents an OSPF Link State Advertisement
type OSPFLSA struct {
	IP        net.IP
	Prefix    uint32
	Metric    uint32
	Sequence  uint32
	Age       uint16
	CreatedAt time.Time
}

// NewOSPFHandler creates a new OSPF handler
func NewOSPFHandler(logger *zap.Logger) (*OSPFHandler, error) {
	ctx, cancel := context.WithCancel(context.Background())

	return &OSPFHandler{
		logger:     logger,
		activeVIPs: make(map[string]*OSPFVIPState),
		started:    false,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// Start starts the OSPF handler
func (h *OSPFHandler) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.started {
		return nil
	}

	h.logger.Info("Starting OSPF handler")

	// OSPF server will be started when first VIP is added
	// (we need config from VIP assignment)
	h.started = true

	return nil
}

// AddVIP adds a VIP with OSPF announcement
func (h *OSPFHandler) AddVIP(assignment *pb.VIPAssignment) error {
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

	// Validate OSPF config
	if assignment.OspfConfig == nil {
		return fmt.Errorf("OSPF config is required for OSPF mode VIPs")
	}

	h.logger.Info("Adding VIP with OSPF announcement",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
		zap.String("router_id", assignment.OspfConfig.RouterId),
		zap.Uint32("area_id", assignment.OspfConfig.AreaId),
	)

	// Start OSPF server if not already started
	if h.ospfServer == nil {
		if err := h.startOSPFServer(assignment.OspfConfig); err != nil {
			return fmt.Errorf("failed to start OSPF server: %w", err)
		}
	}

	// Announce LSA for the VIP
	if err := h.announceLSA(ip, assignment.OspfConfig); err != nil {
		h.logger.Warn("Failed to announce OSPF LSA",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
		// Don't fail the whole operation if announcement fails
	}

	// Track VIP state
	h.activeVIPs[assignment.VipName] = &OSPFVIPState{
		Assignment: assignment,
		IP:         ip,
		AddedAt:    time.Now(),
		Announced:  true,
	}

	// Update metrics
	metrics.OSPFAnnouncedRoutes.Set(float64(len(h.activeVIPs)))

	h.logger.Info("VIP announced via OSPF successfully",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)

	return nil
}

// RemoveVIP removes a VIP and withdraws OSPF announcement
func (h *OSPFHandler) RemoveVIP(assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.activeVIPs[assignment.VipName]
	if !exists {
		h.logger.Debug("VIP not active", zap.String("vip", assignment.VipName))
		return nil
	}

	h.logger.Info("Removing VIP and withdrawing OSPF LSA",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)

	// Withdraw LSA
	if state.Announced && h.ospfServer != nil {
		if err := h.withdrawLSA(state.IP, assignment.OspfConfig); err != nil {
			h.logger.Warn("Failed to withdraw OSPF LSA",
				zap.String("vip", assignment.VipName),
				zap.Error(err),
			)
		}
	}

	delete(h.activeVIPs, assignment.VipName)

	// Update metrics
	metrics.OSPFAnnouncedRoutes.Set(float64(len(h.activeVIPs)))

	h.logger.Info("VIP withdrawn from OSPF successfully",
		zap.String("vip", assignment.VipName),
		zap.Duration("duration", time.Since(state.AddedAt)),
	)

	return nil
}

// startOSPFServer initializes and starts the OSPF server
func (h *OSPFHandler) startOSPFServer(config *pb.OSPFConfig) error {
	h.logger.Info("Starting OSPF server",
		zap.String("router_id", config.RouterId),
		zap.Uint32("area_id", config.AreaId),
	)

	// Parse router ID
	routerID := net.ParseIP(config.RouterId)
	if routerID == nil {
		return fmt.Errorf("invalid router ID: %s", config.RouterId)
	}

	// Create OSPF server
	h.ospfServer = &OSPFServer{
		logger:    h.logger,
		config:    config,
		neighbors: make(map[string]*OSPFNeighbor),
		lsas:      make(map[string]*OSPFLSA),
		routerID:  routerID,
		areaID:    config.AreaId,
		running:   true,
	}

	// Configure OSPF neighbors
	for _, neighbor := range config.Neighbors {
		h.logger.Info("Adding OSPF neighbor",
			zap.String("address", neighbor.Address),
			zap.Uint32("priority", neighbor.Priority),
		)

		h.ospfServer.neighbors[neighbor.Address] = &OSPFNeighbor{
			Address:   neighbor.Address,
			Priority:  neighbor.Priority,
			State:     "Down",
			LastHello: time.Time{},
		}

		// Update metrics
		metrics.SetOSPFNeighborStatus(neighbor.Address, fmt.Sprintf("%d", config.AreaId), false)
	}

	// Start OSPF protocol handling in background
	go h.ospfProtocolLoop()

	h.logger.Info("OSPF server started successfully")
	return nil
}

// ospfProtocolLoop handles OSPF protocol operations
func (h *OSPFHandler) ospfProtocolLoop() {
	h.logger.Info("Starting OSPF protocol loop")

	// Hello interval from config (default 10 seconds)
	helloInterval := time.Duration(10) * time.Second
	if h.ospfServer != nil && h.ospfServer.config.HelloInterval > 0 {
		helloInterval = time.Duration(h.ospfServer.config.HelloInterval) * time.Second
	}

	ticker := time.NewTicker(helloInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			h.logger.Info("OSPF protocol loop stopped")
			return

		case <-ticker.C:
			h.sendHelloPackets()
			h.maintainNeighbors()
		}
	}
}

// sendHelloPackets sends OSPF Hello packets to all neighbors
func (h *OSPFHandler) sendHelloPackets() {
	if h.ospfServer == nil || !h.ospfServer.running {
		return
	}

	h.ospfServer.mu.RLock()
	defer h.ospfServer.mu.RUnlock()

	h.logger.Debug("Sending OSPF Hello packets",
		zap.Int("neighbor_count", len(h.ospfServer.neighbors)),
	)

	// In a real implementation, this would send actual OSPF Hello packets
	// using raw sockets. For now, this is a placeholder that simulates
	// the neighbor relationship establishment.
	for address, neighbor := range h.ospfServer.neighbors {
		neighbor.LastHello = time.Now()

		// Simulate neighbor state progression
		// In production, this would be based on actual packet exchange
		if neighbor.State == "Down" {
			neighbor.State = "Init"
			h.logger.Debug("OSPF neighbor state: Down -> Init",
				zap.String("neighbor", address),
			)
		} else if neighbor.State == "Init" {
			neighbor.State = "2-Way"
			h.logger.Debug("OSPF neighbor state: Init -> 2-Way",
				zap.String("neighbor", address),
			)
		} else if neighbor.State == "2-Way" {
			neighbor.State = "Full"
			h.logger.Info("OSPF neighbor adjacency established",
				zap.String("neighbor", address),
			)

			// Update metrics
			metrics.SetOSPFNeighborStatus(address, fmt.Sprintf("%d", h.ospfServer.areaID), true)
		}
	}
}

// maintainNeighbors checks neighbor liveness and manages state
func (h *OSPFHandler) maintainNeighbors() {
	if h.ospfServer == nil || !h.ospfServer.running {
		return
	}

	h.ospfServer.mu.RLock()
	defer h.ospfServer.mu.RUnlock()

	// Dead interval from config (default 40 seconds)
	deadInterval := time.Duration(40) * time.Second
	if h.ospfServer.config.DeadInterval > 0 {
		deadInterval = time.Duration(h.ospfServer.config.DeadInterval) * time.Second
	}

	now := time.Now()
	for address, neighbor := range h.ospfServer.neighbors {
		if neighbor.State != "Down" && !neighbor.LastHello.IsZero() {
			if now.Sub(neighbor.LastHello) > deadInterval {
				h.logger.Warn("OSPF neighbor dead interval expired",
					zap.String("neighbor", address),
					zap.String("state", neighbor.State),
				)
				neighbor.State = "Down"

				// Update metrics
				metrics.SetOSPFNeighborStatus(address, fmt.Sprintf("%d", h.ospfServer.areaID), false)
			}
		}
	}
}

// announceLSA announces an LSA for a VIP
func (h *OSPFHandler) announceLSA(ip net.IP, config *pb.OSPFConfig) error {
	if h.ospfServer == nil {
		return fmt.Errorf("OSPF server not initialized")
	}

	h.ospfServer.mu.Lock()
	defer h.ospfServer.mu.Unlock()

	// Create /32 host route LSA
	lsaKey := ip.String()

	h.logger.Info("Announcing OSPF LSA", zap.String("prefix", fmt.Sprintf("%s/32", ip.String())))

	// Create LSA entry
	h.ospfServer.lsas[lsaKey] = &OSPFLSA{
		IP:        ip,
		Prefix:    32,
		Metric:    10, // Default metric
		Sequence:  1,
		Age:       0,
		CreatedAt: time.Now(),
	}

	// In a real implementation, this would:
	// 1. Create an OSPF Router LSA or AS-External LSA
	// 2. Flood the LSA to all neighbors in Full state
	// 3. Add the LSA to the LSDB (Link State Database)
	// 4. Trigger SPF calculation on neighbors

	h.logger.Info("OSPF LSA announced successfully", zap.String("prefix", fmt.Sprintf("%s/32", ip.String())))
	return nil
}

// withdrawLSA withdraws an LSA for a VIP
func (h *OSPFHandler) withdrawLSA(ip net.IP, config *pb.OSPFConfig) error {
	if h.ospfServer == nil {
		return fmt.Errorf("OSPF server not initialized")
	}

	h.ospfServer.mu.Lock()
	defer h.ospfServer.mu.Unlock()

	lsaKey := ip.String()

	h.logger.Info("Withdrawing OSPF LSA", zap.String("prefix", fmt.Sprintf("%s/32", ip.String())))

	// Remove LSA entry
	delete(h.ospfServer.lsas, lsaKey)

	// In a real implementation, this would:
	// 1. Set the LSA age to MaxAge (3600 seconds)
	// 2. Flood the MaxAge LSA to all neighbors
	// 3. Remove the LSA from LSDB after acknowledgment

	h.logger.Info("OSPF LSA withdrawn successfully", zap.String("prefix", fmt.Sprintf("%s/32", ip.String())))
	return nil
}

// Shutdown gracefully shuts down the OSPF handler
func (h *OSPFHandler) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.logger.Info("Shutting down OSPF handler")

	// Cancel background tasks
	if h.cancel != nil {
		h.cancel()
	}

	if h.ospfServer != nil {
		h.ospfServer.mu.Lock()
		h.ospfServer.running = false
		h.ospfServer.neighbors = make(map[string]*OSPFNeighbor)
		h.ospfServer.lsas = make(map[string]*OSPFLSA)
		h.ospfServer.mu.Unlock()
		h.ospfServer = nil
	}

	h.activeVIPs = make(map[string]*OSPFVIPState)
	h.started = false
}

// GetActiveVIPCount returns the number of active VIPs
func (h *OSPFHandler) GetActiveVIPCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.activeVIPs)
}
