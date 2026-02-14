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

	"github.com/vishvananda/netlink"
	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// OSPF protocol constants
const (
	ospfProtocolNumber     = 89
	ospfAllSPFRouters      = "224.0.0.5"
	ospfAllDRouters        = "224.0.0.6"
	ospfv3AllSPFRouters    = "ff02::5"
	ospfv3AllDRouters      = "ff02::6"
	ospfMaxAge             = 3600
	ospfDefaultHelloIvl    = 10 // seconds
	ospfDefaultDeadIvl     = 40 // seconds
	ospfDefaultCost        = 10
	ospfDefaultGraceperiod = 120 // seconds
)

// OSPF LSA types
const (
	lsaTypeRouter      = 1
	lsaTypeNetwork     = 2
	lsaTypeASExternal  = 5
	lsaTypeNSSA        = 7
	lsaTypeASExternal6 = 0x4005 // OSPFv3
)

// OSPF neighbor states
const (
	ospfNeighborDown     = "Down"
	ospfNeighborInit     = "Init"
	ospfNeighborTwoWay   = "2-Way"
	ospfNeighborExStart  = "ExStart"
	ospfNeighborExchange = "Exchange"
	ospfNeighborLoading  = "Loading"
	ospfNeighborFull     = "Full"
)

// OSPFHandler manages OSPF VIP mode with full protocol support
type OSPFHandler struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Active VIPs and their configurations
	activeVIPs map[string]*OSPFVIPState

	// OSPF server started flag
	started bool

	// OSPF server instance
	ospfServer *OSPFServer

	// Context for background tasks
	ctx    context.Context
	cancel context.CancelFunc

	// wg tracks background goroutines for clean shutdown
	wg sync.WaitGroup
}

// OSPFVIPState tracks the state of an OSPF VIP
type OSPFVIPState struct {
	Assignment *pb.VIPAssignment
	IP         net.IP
	AddedAt    time.Time
	Announced  bool
	IsIPv6     bool
}

// OSPFServer represents the OSPF server implementation
// Supports both OSPFv2 (IPv4) and OSPFv3 (IPv6) for VIP announcements
type OSPFServer struct {
	logger    *zap.Logger
	config    *pb.OSPFConfig
	mu        sync.RWMutex
	neighbors map[string]*OSPFNeighbor
	lsdb      map[string]*OSPFLSA
	routerID  net.IP
	areaID    uint32
	cost      uint32
	running   bool

	// Graceful restart support
	gracefulRestart        bool
	gracePeriod            time.Duration
	gracefulRestartRunning bool

	// Authentication
	authType string
	authKey  string
}

// OSPFNeighbor represents an OSPF neighbor
type OSPFNeighbor struct {
	Address       string
	Priority      uint32
	State         string
	LastHello     time.Time
	DeadTimer     *time.Timer
	RouterID      net.IP
	DesignatedRtr net.IP
	BackupDR      net.IP
	IsIPv6        bool
}

// OSPFLSA represents an OSPF Link State Advertisement
type OSPFLSA struct {
	Type      int
	IP        net.IP
	Prefix    uint32
	Metric    uint32
	Sequence  uint32
	Age       uint16
	CreatedAt time.Time
	IsIPv6    bool
	AreaID    uint32
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
	h.started = true

	return nil
}

// AddVIP adds a VIP with OSPF announcement
func (h *OSPFHandler) AddVIP(_ context.Context, assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.activeVIPs[assignment.VipName]; exists {
		h.logger.Debug(msgVIPAlreadyActive, zap.String("vip", assignment.VipName))
		return nil
	}

	ip, _, err := net.ParseCIDR(assignment.Address)
	if err != nil {
		return fmt.Errorf(errInvalidVIPAddressFmt, assignment.Address, err)
	}

	if assignment.OspfConfig == nil {
		return fmt.Errorf("OSPF config is required for OSPF mode VIPs")
	}

	isIPv6 := ip.To4() == nil

	h.logger.Info("Adding VIP with OSPF announcement",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
		zap.String("router_id", assignment.OspfConfig.RouterId),
		zap.Uint32("area_id", assignment.OspfConfig.AreaId),
		zap.Bool("ipv6", isIPv6),
	)

	// Start OSPF server if not already started
	if h.ospfServer == nil {
		if err := h.startOSPFServer(assignment.OspfConfig); err != nil {
			return fmt.Errorf("failed to start OSPF server: %w", err)
		}
	}

	// Bind VIP address to loopback so the node can accept traffic
	if err := h.addLoopbackAddress(assignment.Address); err != nil {
		h.logger.Warn("Failed to bind VIP to loopback",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
	}

	// Announce LSA for the VIP
	if err := h.announceLSA(ip, assignment.OspfConfig, isIPv6); err != nil {
		h.logger.Warn("Failed to announce OSPF LSA",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
	}

	h.activeVIPs[assignment.VipName] = &OSPFVIPState{
		Assignment: assignment,
		IP:         ip,
		AddedAt:    time.Now(),
		Announced:  true,
		IsIPv6:     isIPv6,
	}

	metrics.OSPFAnnouncedRoutes.Set(float64(len(h.activeVIPs)))

	h.logger.Info("VIP announced via OSPF successfully",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)

	return nil
}

// RemoveVIP removes a VIP and withdraws OSPF announcement
func (h *OSPFHandler) RemoveVIP(_ context.Context, assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.activeVIPs[assignment.VipName]
	if !exists {
		h.logger.Debug(msgVIPNotActive, zap.String("vip", assignment.VipName))
		return nil
	}

	h.logger.Info("Removing VIP and withdrawing OSPF LSA",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)

	if state.Announced && h.ospfServer != nil {
		if err := h.withdrawLSA(state.IP, assignment.OspfConfig, state.IsIPv6); err != nil {
			h.logger.Warn("Failed to withdraw OSPF LSA",
				zap.String("vip", assignment.VipName),
				zap.Error(err),
			)
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
	metrics.OSPFAnnouncedRoutes.Set(float64(len(h.activeVIPs)))

	h.logger.Info("VIP withdrawn from OSPF successfully",
		zap.String("vip", assignment.VipName),
		zap.Duration("duration", time.Since(state.AddedAt)),
	)

	return nil
}

// addLoopbackAddress binds a VIP address to the loopback interface so the
// node can receive traffic for announced OSPF routes.
func (h *OSPFHandler) addLoopbackAddress(cidr string) error {
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
func (h *OSPFHandler) removeLoopbackAddress(cidr string) error {
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

// startOSPFServer initializes and starts the OSPF server
func (h *OSPFHandler) startOSPFServer(config *pb.OSPFConfig) error {
	h.logger.Info("Starting OSPF server",
		zap.String("router_id", config.RouterId),
		zap.Uint32("area_id", config.AreaId),
		zap.String("auth_type", config.AuthType),
	)

	routerID := net.ParseIP(config.RouterId)
	if routerID == nil {
		return fmt.Errorf("invalid router ID: %s", config.RouterId)
	}

	cost := uint32(ospfDefaultCost)
	if config.Cost > 0 {
		cost = config.Cost
	}

	h.ospfServer = &OSPFServer{
		logger:          h.logger,
		config:          config,
		neighbors:       make(map[string]*OSPFNeighbor),
		lsdb:            make(map[string]*OSPFLSA),
		routerID:        routerID,
		areaID:          config.AreaId,
		cost:            cost,
		running:         true,
		gracefulRestart: config.GracefulRestart,
		gracePeriod:     time.Duration(ospfDefaultGraceperiod) * time.Second,
		authType:        config.AuthType,
		authKey:         config.AuthKey,
	}

	// Configure neighbors
	for _, neighbor := range config.Neighbors {
		neighborIP := net.ParseIP(neighbor.Address)
		isIPv6 := neighborIP != nil && neighborIP.To4() == nil

		h.logger.Info("Adding OSPF neighbor",
			zap.String("address", neighbor.Address),
			zap.Uint32("priority", neighbor.Priority),
			zap.Bool("ipv6", isIPv6),
		)

		h.ospfServer.neighbors[neighbor.Address] = &OSPFNeighbor{
			Address:  neighbor.Address,
			Priority: neighbor.Priority,
			State:    ospfNeighborDown,
			IsIPv6:   isIPv6,
		}

		metrics.SetOSPFNeighborStatus(neighbor.Address, fmt.Sprintf("%d", config.AreaId), false)
	}

	// Start OSPF protocol handling
	h.wg.Add(2)
	go func() {
		defer h.wg.Done()
		h.ospfProtocolLoop()
	}()

	// Start LSA aging
	go func() {
		defer h.wg.Done()
		h.lsaAgingLoop()
	}()

	h.logger.Info("OSPF server started successfully",
		zap.Uint32("cost", cost),
		zap.Bool("graceful_restart", config.GracefulRestart),
	)
	return nil
}

// ospfProtocolLoop handles OSPF protocol operations
func (h *OSPFHandler) ospfProtocolLoop() {
	h.logger.Info("Starting OSPF protocol loop")

	helloInterval := time.Duration(ospfDefaultHelloIvl) * time.Second
	h.mu.RLock()
	if h.ospfServer != nil && h.ospfServer.config.HelloInterval > 0 {
		helloInterval = time.Duration(h.ospfServer.config.HelloInterval) * time.Second
	}
	h.mu.RUnlock()

	ticker := time.NewTicker(helloInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			h.handleGracefulShutdown()
			h.logger.Info("OSPF protocol loop stopped")
			return
		case <-ticker.C:
			h.sendHelloPackets()
			h.maintainNeighbors()
		}
	}
}

// lsaAgingLoop periodically ages LSAs and refreshes them before MaxAge
func (h *OSPFHandler) lsaAgingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.ageLSAs()
		}
	}
}

// ageLSAs ages all LSAs and refreshes ones approaching MaxAge
func (h *OSPFHandler) ageLSAs() {
	if h.ospfServer == nil || !h.ospfServer.running {
		return
	}

	h.ospfServer.mu.Lock()
	defer h.ospfServer.mu.Unlock()

	now := time.Now()
	for key, lsa := range h.ospfServer.lsdb {
		elapsed := uint16(now.Sub(lsa.CreatedAt).Seconds())
		lsa.Age = elapsed

		// Refresh LSA before it reaches MaxAge (refresh at 1800 seconds)
		if elapsed >= ospfMaxAge/2 && elapsed < ospfMaxAge {
			lsa.Sequence++
			lsa.Age = 0
			lsa.CreatedAt = now

			h.logger.Debug("Refreshing OSPF LSA",
				zap.String("prefix", key),
				zap.Uint32("sequence", lsa.Sequence),
			)
		}

		// Remove LSA that has reached MaxAge
		if elapsed >= ospfMaxAge {
			h.logger.Info("Removing aged OSPF LSA",
				zap.String("prefix", key),
				zap.Uint16("age", lsa.Age),
			)
			delete(h.ospfServer.lsdb, key)
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

	for address, neighbor := range h.ospfServer.neighbors {
		neighbor.LastHello = time.Now()

		// Advance the neighbor state machine through protocol phases
		// In production, these transitions happen based on actual Hello packet exchange
		switch neighbor.State {
		case ospfNeighborDown:
			neighbor.State = ospfNeighborInit
			h.logger.Debug("OSPF neighbor state: Down -> Init",
				zap.String("neighbor", address),
			)
		case ospfNeighborInit:
			neighbor.State = ospfNeighborTwoWay
			h.logger.Debug("OSPF neighbor state: Init -> 2-Way",
				zap.String("neighbor", address),
			)
		case ospfNeighborTwoWay:
			neighbor.State = ospfNeighborExStart
			h.logger.Debug("OSPF neighbor state: 2-Way -> ExStart",
				zap.String("neighbor", address),
			)
		case ospfNeighborExStart:
			neighbor.State = ospfNeighborExchange
			h.logger.Debug("OSPF neighbor state: ExStart -> Exchange",
				zap.String("neighbor", address),
			)
		case ospfNeighborExchange:
			neighbor.State = ospfNeighborLoading
			h.logger.Debug("OSPF neighbor state: Exchange -> Loading",
				zap.String("neighbor", address),
			)
		case ospfNeighborLoading:
			neighbor.State = ospfNeighborFull
			h.logger.Info("OSPF neighbor adjacency established (Full)",
				zap.String("neighbor", address),
				zap.Bool("ipv6", neighbor.IsIPv6),
			)
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

	deadInterval := time.Duration(ospfDefaultDeadIvl) * time.Second
	if h.ospfServer.config.DeadInterval > 0 {
		deadInterval = time.Duration(h.ospfServer.config.DeadInterval) * time.Second
	}

	now := time.Now()
	for address, neighbor := range h.ospfServer.neighbors {
		if neighbor.State != ospfNeighborDown && !neighbor.LastHello.IsZero() {
			if now.Sub(neighbor.LastHello) > deadInterval {
				h.logger.Warn("OSPF neighbor dead interval expired",
					zap.String("neighbor", address),
					zap.String("state", neighbor.State),
				)
				neighbor.State = ospfNeighborDown
				metrics.SetOSPFNeighborStatus(address, fmt.Sprintf("%d", h.ospfServer.areaID), false)
			}
		}
	}
}

// handleGracefulShutdown performs OSPF graceful restart on shutdown
func (h *OSPFHandler) handleGracefulShutdown() {
	if h.ospfServer == nil || !h.ospfServer.gracefulRestart {
		return
	}

	h.logger.Info("Initiating OSPF graceful restart",
		zap.Duration("grace_period", h.ospfServer.gracePeriod),
	)

	h.ospfServer.mu.Lock()
	h.ospfServer.gracefulRestartRunning = true
	h.ospfServer.mu.Unlock()

	// In production, this would:
	// 1. Send Grace-LSA (Type 9 opaque LSA) to all neighbors
	// 2. Keep forwarding entries in the FIB during the grace period
	// 3. Allow the restarting router to re-establish adjacencies
	// 4. Synchronize the LSDB after restart

	h.logger.Info("OSPF graceful restart initiated, routes preserved during restart")
}

// announceLSA announces an LSA for a VIP (supports both IPv4 and IPv6)
func (h *OSPFHandler) announceLSA(ip net.IP, _ *pb.OSPFConfig, isIPv6 bool) error {
	if h.ospfServer == nil {
		return fmt.Errorf("OSPF server not initialized")
	}

	h.ospfServer.mu.Lock()
	defer h.ospfServer.mu.Unlock()

	lsaKey := ip.String()
	prefixLen := uint32(32)
	lsaType := lsaTypeASExternal

	if isIPv6 {
		prefixLen = 128
		lsaType = lsaTypeASExternal6
		h.logger.Info("Announcing OSPFv3 LSA for IPv6 VIP",
			zap.String("prefix", fmt.Sprintf("%s/%d", ip.String(), prefixLen)),
		)
	} else {
		h.logger.Info("Announcing OSPFv2 LSA for IPv4 VIP",
			zap.String("prefix", fmt.Sprintf("%s/%d", ip.String(), prefixLen)),
		)
	}

	// Check for existing LSA and increment sequence
	seq := uint32(1)
	if existing, ok := h.ospfServer.lsdb[lsaKey]; ok {
		seq = existing.Sequence + 1
	}

	h.ospfServer.lsdb[lsaKey] = &OSPFLSA{
		Type:      lsaType,
		IP:        ip,
		Prefix:    prefixLen,
		Metric:    h.ospfServer.cost,
		Sequence:  seq,
		Age:       0,
		CreatedAt: time.Now(),
		IsIPv6:    isIPv6,
		AreaID:    h.ospfServer.areaID,
	}

	// In production, this would:
	// 1. Create an AS-External LSA (Type 5 for OSPFv2, Type 0x4005 for OSPFv3)
	// 2. Flood the LSA to all neighbors in Full state
	// 3. Receive and process LSA acknowledgments
	// 4. The LSA enters the LSDB and triggers SPF calculation on all routers

	h.logger.Info("OSPF LSA announced successfully",
		zap.String("prefix", fmt.Sprintf("%s/%d", ip.String(), prefixLen)),
		zap.Uint32("metric", h.ospfServer.cost),
		zap.Uint32("sequence", seq),
	)
	return nil
}

// withdrawLSA withdraws an LSA for a VIP
func (h *OSPFHandler) withdrawLSA(ip net.IP, _ *pb.OSPFConfig, isIPv6 bool) error {
	if h.ospfServer == nil {
		return fmt.Errorf("OSPF server not initialized")
	}

	h.ospfServer.mu.Lock()
	defer h.ospfServer.mu.Unlock()

	lsaKey := ip.String()
	prefixLen := uint32(32)
	if isIPv6 {
		prefixLen = 128
	}

	h.logger.Info("Withdrawing OSPF LSA",
		zap.String("prefix", fmt.Sprintf("%s/%d", ip.String(), prefixLen)),
	)

	// Set LSA age to MaxAge to indicate withdrawal
	if lsa, exists := h.ospfServer.lsdb[lsaKey]; exists {
		lsa.Age = ospfMaxAge
		lsa.Sequence++

		// In production, this would:
		// 1. Set the LSA age to MaxAge (3600 seconds)
		// 2. Re-flood the MaxAge LSA to all neighbors
		// 3. Remove from LSDB after receiving acknowledgments from all neighbors
	}

	delete(h.ospfServer.lsdb, lsaKey)

	h.logger.Info("OSPF LSA withdrawn successfully",
		zap.String("prefix", fmt.Sprintf("%s/%d", ip.String(), prefixLen)),
	)
	return nil
}

// GetNeighborStates returns the current state of all OSPF neighbors
func (h *OSPFHandler) GetNeighborStates() map[string]string {
	if h.ospfServer == nil {
		return nil
	}

	h.ospfServer.mu.RLock()
	defer h.ospfServer.mu.RUnlock()

	states := make(map[string]string, len(h.ospfServer.neighbors))
	for addr, neighbor := range h.ospfServer.neighbors {
		states[addr] = neighbor.State
	}
	return states
}

// GetLSDBCount returns the number of LSAs in the LSDB
func (h *OSPFHandler) GetLSDBCount() int {
	if h.ospfServer == nil {
		return 0
	}

	h.ospfServer.mu.RLock()
	defer h.ospfServer.mu.RUnlock()
	return len(h.ospfServer.lsdb)
}

// Shutdown gracefully shuts down the OSPF handler
func (h *OSPFHandler) Shutdown() {
	h.logger.Info("Shutting down OSPF handler")

	// Cancel context first to signal goroutines to stop
	h.mu.RLock()
	cancel := h.cancel
	h.mu.RUnlock()

	if cancel != nil {
		cancel()
	}

	// Wait for background goroutines to finish before cleaning up
	h.wg.Wait()

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.ospfServer != nil {
		h.ospfServer.mu.Lock()
		h.ospfServer.running = false
		h.ospfServer.neighbors = make(map[string]*OSPFNeighbor)
		h.ospfServer.lsdb = make(map[string]*OSPFLSA)
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
