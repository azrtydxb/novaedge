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
	"math"
	"net"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

// BFD session states per RFC 5880
const (
	BFDStateAdminDown BFDSessionState = 0
	BFDStateDown      BFDSessionState = 1
	BFDStateInit      BFDSessionState = 2
	BFDStateUp        BFDSessionState = 3
)

// BFD protocol constants
const (
	bfdDefaultDesiredMinTx  = 500 * time.Millisecond
	bfdDefaultRequiredMinRx = 500 * time.Millisecond
	bfdDefaultDetectMult    = 3
	bfdControlPort          = 3784
	bfdEchoPort             = 3785
)

// BFDSessionState represents the BFD state machine state
type BFDSessionState int32

// String returns the string representation of a BFD session state
func (s BFDSessionState) String() string {
	switch s {
	case BFDStateAdminDown:
		return "AdminDown"
	case BFDStateDown:
		return "Down"
	case BFDStateInit:
		return "Init"
	case BFDStateUp:
		return "Up"
	default:
		return fmt.Sprintf("Unknown(%d)", s)
	}
}

// BFDConfig holds BFD session configuration
type BFDConfig struct {
	// DetectMultiplier is the detection time multiplier
	DetectMultiplier int32

	// DesiredMinTxInterval is the desired minimum transmit interval
	DesiredMinTxInterval time.Duration

	// RequiredMinRxInterval is the required minimum receive interval
	RequiredMinRxInterval time.Duration

	// EchoMode enables BFD echo mode
	EchoMode bool
}

// BFDSession represents an active BFD session with a peer
type BFDSession struct {
	mu sync.RWMutex

	// Configuration
	config BFDConfig

	// Session identifiers
	localDiscriminator  uint32
	remoteDiscriminator uint32
	peerAddress         net.IP

	// State machine
	state       BFDSessionState
	remoteState BFDSessionState

	// Timing
	desiredMinTxInterval  time.Duration
	requiredMinRxInterval time.Duration
	detectMultiplier      int32
	detectionTime         time.Duration

	// Timestamps
	lastPacketReceived time.Time
	lastPacketSent     time.Time
	stateChangedAt     time.Time

	// Statistics
	packetsRx    uint64
	packetsTx    uint64
	sessionFlaps uint64

	// Callback for state changes
	onStateChange func(peer net.IP, oldState, newState BFDSessionState)

	// Logger
	logger *zap.Logger
}

// BFDManager manages BFD sessions for BGP peers
type BFDManager struct {
	mu sync.RWMutex

	logger   *zap.Logger
	sessions map[string]*BFDSession // key: peer IP address

	// Discriminator counter for generating unique local discriminators
	nextDiscriminator uint32

	// Context for lifecycle management
	ctx    context.Context
	cancel context.CancelFunc

	// Callback when BFD detects a neighbor is down
	onNeighborDown func(peerIP net.IP)

	// Callback when BFD detects a neighbor has recovered (session transitions to Up)
	onNeighborUp func(peerIP net.IP)

	// UDP transport for BFD control packets
	transport *bfdTransport

	// ListenPort is the UDP port to listen on for BFD control packets.
	// Defaults to bfdControlPort (3784). Set to 0 for OS-assigned port in tests.
	ListenPort int

	// PeerPort is the destination UDP port for sending BFD control packets.
	// Defaults to bfdControlPort (3784). Override for testing with multiple
	// managers on localhost.
	PeerPort int
}

// BFD Prometheus metrics
var (
	bfdSessionState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_bfd_session_state",
			Help: "BFD session state (0=AdminDown, 1=Down, 2=Init, 3=Up)",
		},
		[]string{"peer_address"},
	)

	bfdPacketRx = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_bfd_packet_rx_total",
			Help: "Total BFD packets received",
		},
		[]string{"peer_address"},
	)

	bfdPacketTx = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_bfd_packet_tx_total",
			Help: "Total BFD packets transmitted",
		},
		[]string{"peer_address"},
	)

	bfdSessionFlaps = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_bfd_session_flaps_total",
			Help: "Total BFD session flaps (transitions from Up to Down)",
		},
		[]string{"peer_address"},
	)
)

// NewBFDManager creates a new BFD manager
func NewBFDManager(logger *zap.Logger, onNeighborDown, onNeighborUp func(peerIP net.IP)) *BFDManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &BFDManager{
		logger:            logger.Named("bfd"),
		sessions:          make(map[string]*BFDSession),
		nextDiscriminator: 1,
		ctx:               ctx,
		cancel:            cancel,
		onNeighborDown:    onNeighborDown,
		onNeighborUp:      onNeighborUp,
		ListenPort:        bfdControlPort,
		PeerPort:          bfdControlPort,
	}
}

// Start starts the BFD manager's background processes
func (m *BFDManager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()

	m.logger.Info("Starting BFD manager")

	// Start the UDP transport for BFD control packets
	transport := newBFDTransport(m.logger, m, m.ListenPort)
	if err := transport.Start(m.ctx); err != nil {
		m.logger.Warn("Failed to start BFD transport, running without UDP",
			zap.Error(err),
		)
	} else {
		m.mu.Lock()
		m.transport = transport
		m.mu.Unlock()
	}

	// Start the detection timer loop
	go m.detectionLoop()

	// Start the transmit loop
	go m.transmitLoop()

	return nil
}

// Stop stops the BFD manager
func (m *BFDManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.transport != nil {
		m.transport.Stop()
		m.transport = nil
	}

	if m.cancel != nil {
		m.cancel()
	}

	m.logger.Info("BFD manager stopped")
}

// AddSession creates a new BFD session for a peer
func (m *BFDManager) AddSession(peerIP net.IP, config BFDConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	peerKey := peerIP.String()
	if _, exists := m.sessions[peerKey]; exists {
		m.logger.Debug("BFD session already exists", zap.String("peer", peerKey))
		return nil
	}

	// Apply defaults
	if config.DetectMultiplier <= 0 {
		config.DetectMultiplier = bfdDefaultDetectMult
	}
	if config.DesiredMinTxInterval <= 0 {
		config.DesiredMinTxInterval = bfdDefaultDesiredMinTx
	}
	if config.RequiredMinRxInterval <= 0 {
		config.RequiredMinRxInterval = bfdDefaultRequiredMinRx
	}

	session := &BFDSession{
		config:                config,
		localDiscriminator:    m.nextDiscriminator,
		peerAddress:           peerIP,
		state:                 BFDStateDown,
		remoteState:           BFDStateDown,
		desiredMinTxInterval:  config.DesiredMinTxInterval,
		requiredMinRxInterval: config.RequiredMinRxInterval,
		detectMultiplier:      config.DetectMultiplier,
		detectionTime:         time.Duration(config.DetectMultiplier) * config.RequiredMinRxInterval,
		stateChangedAt:        time.Now(),
		logger:                m.logger.With(zap.String("peer", peerKey)),
		onStateChange:         m.handleStateChange,
	}

	m.nextDiscriminator++
	m.sessions[peerKey] = session

	// Initialize metrics
	bfdSessionState.WithLabelValues(peerKey).Set(float64(BFDStateDown))

	m.logger.Info("BFD session created",
		zap.String("peer", peerKey),
		zap.Int32("detect_mult", config.DetectMultiplier),
		zap.Duration("desired_min_tx", config.DesiredMinTxInterval),
		zap.Duration("required_min_rx", config.RequiredMinRxInterval),
	)

	return nil
}

// UpdateSession updates the timing parameters of an existing BFD session.
// If the session does not exist, it creates a new one with the given config.
func (m *BFDManager) UpdateSession(peerIP net.IP, config BFDConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	peerKey := peerIP.String()
	session, exists := m.sessions[peerKey]
	if !exists {
		// Session doesn't exist — create via unlocked helper
		m.mu.Unlock()
		err := m.AddSession(peerIP, config)
		m.mu.Lock()
		return err
	}

	// Apply defaults
	if config.DetectMultiplier <= 0 {
		config.DetectMultiplier = bfdDefaultDetectMult
	}
	if config.DesiredMinTxInterval <= 0 {
		config.DesiredMinTxInterval = bfdDefaultDesiredMinTx
	}
	if config.RequiredMinRxInterval <= 0 {
		config.RequiredMinRxInterval = bfdDefaultRequiredMinRx
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	session.config = config
	session.desiredMinTxInterval = config.DesiredMinTxInterval
	session.requiredMinRxInterval = config.RequiredMinRxInterval
	session.detectMultiplier = config.DetectMultiplier
	session.detectionTime = time.Duration(config.DetectMultiplier) * config.RequiredMinRxInterval

	m.logger.Info("BFD session updated",
		zap.String("peer", peerKey),
		zap.Int32("detect_mult", config.DetectMultiplier),
		zap.Duration("desired_min_tx", config.DesiredMinTxInterval),
		zap.Duration("required_min_rx", config.RequiredMinRxInterval),
	)

	return nil
}

// RemoveSession removes a BFD session for a peer
func (m *BFDManager) RemoveSession(peerIP net.IP) {
	m.mu.Lock()
	defer m.mu.Unlock()

	peerKey := peerIP.String()
	if session, exists := m.sessions[peerKey]; exists {
		session.mu.Lock()
		session.state = BFDStateAdminDown
		session.mu.Unlock()

		bfdSessionState.WithLabelValues(peerKey).Set(float64(BFDStateAdminDown))
		delete(m.sessions, peerKey)

		m.logger.Info("BFD session removed", zap.String("peer", peerKey))
	}
}

// GetSessionState returns the state of a BFD session
func (m *BFDManager) GetSessionState(peerIP net.IP) BFDSessionState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	peerKey := peerIP.String()
	if session, exists := m.sessions[peerKey]; exists {
		session.mu.RLock()
		defer session.mu.RUnlock()
		return session.state
	}

	return BFDStateDown
}

// GetSessionCount returns the number of active BFD sessions
func (m *BFDManager) GetSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// ProcessPacket processes a received BFD control packet from a peer
func (m *BFDManager) ProcessPacket(peerIP net.IP, remoteState BFDSessionState, remoteDiscriminator uint32) {
	m.mu.RLock()
	session, exists := m.sessions[peerIP.String()]
	m.mu.RUnlock()

	if !exists {
		return
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	session.lastPacketReceived = time.Now()
	session.packetsRx++
	session.remoteDiscriminator = remoteDiscriminator
	session.remoteState = remoteState

	bfdPacketRx.WithLabelValues(peerIP.String()).Inc()

	// BFD state machine transitions per RFC 5880 Section 6.8.6
	oldState := session.state
	session.processStateMachine(remoteState)

	if oldState != session.state {
		session.stateChangedAt = time.Now()
		bfdSessionState.WithLabelValues(peerIP.String()).Set(float64(session.state))

		if session.onStateChange != nil {
			session.onStateChange(peerIP, oldState, session.state)
		}
	}
}

// processStateMachine implements the BFD state machine per RFC 5880
func (s *BFDSession) processStateMachine(remoteState BFDSessionState) {
	switch s.state {
	case BFDStateAdminDown:
		// No state transitions from AdminDown via protocol
		return

	case BFDStateDown:
		switch remoteState {
		case BFDStateDown:
			s.state = BFDStateInit
			s.logger.Debug("BFD state transition: Down -> Init")
		case BFDStateInit:
			s.state = BFDStateUp
			s.logger.Info("BFD session established",
				zap.String("peer", s.peerAddress.String()),
			)
		}

	case BFDStateInit:
		switch remoteState {
		case BFDStateInit, BFDStateUp:
			s.state = BFDStateUp
			s.logger.Info("BFD session established",
				zap.String("peer", s.peerAddress.String()),
			)
		}

	case BFDStateUp:
		if remoteState == BFDStateDown || remoteState == BFDStateAdminDown {
			s.state = BFDStateDown
			s.sessionFlaps++
			bfdSessionFlaps.WithLabelValues(s.peerAddress.String()).Inc()
			s.logger.Warn("BFD session down",
				zap.String("peer", s.peerAddress.String()),
				zap.String("remote_state", remoteState.String()),
			)
		}
	}
}

// handleStateChange is called when a BFD session state changes
func (m *BFDManager) handleStateChange(peerIP net.IP, oldState, newState BFDSessionState) {
	m.logger.Info("BFD session state changed",
		zap.String("peer", peerIP.String()),
		zap.String("old_state", oldState.String()),
		zap.String("new_state", newState.String()),
	)

	// If session went down from Up, notify BGP handler to withdraw routes
	if oldState == BFDStateUp && (newState == BFDStateDown || newState == BFDStateAdminDown) {
		m.logger.Warn("BFD detected neighbor failure, triggering route withdrawal",
			zap.String("peer", peerIP.String()),
		)
		if m.onNeighborDown != nil {
			m.onNeighborDown(peerIP)
		}
	}

	// If session recovered to Up, notify BGP handler to re-announce routes
	if newState == BFDStateUp && oldState != BFDStateUp {
		m.logger.Info("BFD detected neighbor recovery, triggering route re-announcement",
			zap.String("peer", peerIP.String()),
		)
		if m.onNeighborUp != nil {
			m.onNeighborUp(peerIP)
		}
	}
}

// detectionLoop checks for session timeouts
func (m *BFDManager) detectionLoop() {
	// Check at a rate faster than the fastest possible detection time
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkDetectionTimeouts()
		}
	}
}

// checkDetectionTimeouts checks all sessions for detection timeout
func (m *BFDManager) checkDetectionTimeouts() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	for peerKey, session := range m.sessions {
		session.mu.Lock()

		// Only check timeout for sessions in Init or Up state
		if session.state != BFDStateInit && session.state != BFDStateUp {
			session.mu.Unlock()
			continue
		}

		// Check if detection time has elapsed since last packet
		if !session.lastPacketReceived.IsZero() && now.Sub(session.lastPacketReceived) > session.detectionTime {
			oldState := session.state
			session.state = BFDStateDown
			session.stateChangedAt = now

			if oldState == BFDStateUp {
				session.sessionFlaps++
				bfdSessionFlaps.WithLabelValues(peerKey).Inc()
			}

			bfdSessionState.WithLabelValues(peerKey).Set(float64(BFDStateDown))

			session.logger.Warn("BFD detection timeout",
				zap.Duration("detection_time", session.detectionTime),
				zap.Duration("since_last_packet", now.Sub(session.lastPacketReceived)),
			)

			if session.onStateChange != nil {
				go session.onStateChange(session.peerAddress, oldState, BFDStateDown)
			}
		}

		session.mu.Unlock()
	}
}

// transmitLoop sends BFD control packets at the configured interval
func (m *BFDManager) transmitLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.sendControlPackets()
		}
	}
}

// sendControlPackets sends BFD control packets for all active sessions via
// the UDP transport. If the transport is nil (e.g., in unit tests that don't
// require UDP), the method updates metrics and timestamps without sending.
func (m *BFDManager) sendControlPackets() {
	m.mu.RLock()
	transport := m.transport
	peerPort := m.PeerPort
	m.mu.RUnlock()

	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	for peerKey, session := range m.sessions {
		session.mu.Lock()

		// Skip AdminDown sessions
		if session.state == BFDStateAdminDown {
			session.mu.Unlock()
			continue
		}

		// Check if it's time to send a packet
		if !session.lastPacketSent.IsZero() && now.Sub(session.lastPacketSent) < session.desiredMinTxInterval {
			session.mu.Unlock()
			continue
		}

		// Build BFD control packet from session state
		pkt := &bfdControlPacket{
			Version:               bfdVersion,
			Diagnostic:            bfdDiagNone,
			State:                 session.state,
			DetectMult:            clampInt32ToUint8(session.detectMultiplier),
			MyDiscriminator:       session.localDiscriminator,
			YourDiscriminator:     session.remoteDiscriminator,
			DesiredMinTxInterval:  clampDurationToMicroseconds(session.desiredMinTxInterval),
			RequiredMinRxInterval: clampDurationToMicroseconds(session.requiredMinRxInterval),
		}

		// Send via transport if available
		if transport != nil {
			if err := transport.Send(session.peerAddress, peerPort, pkt); err != nil {
				session.logger.Debug("Failed to send BFD control packet",
					zap.Error(err),
				)
				session.mu.Unlock()
				continue
			}
		}

		session.lastPacketSent = now
		session.packetsTx++
		bfdPacketTx.WithLabelValues(peerKey).Inc()

		session.logger.Debug("BFD control packet sent",
			zap.String("state", session.state.String()),
			zap.Uint32("local_discr", session.localDiscriminator),
		)

		session.mu.Unlock()
	}
}

// GetAllSessionStates returns the state of all BFD sessions
func (m *BFDManager) GetAllSessionStates() map[string]BFDSessionState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	states := make(map[string]BFDSessionState, len(m.sessions))
	for peerKey, session := range m.sessions {
		session.mu.RLock()
		states[peerKey] = session.state
		session.mu.RUnlock()
	}
	return states
}

// GetSessionStats returns statistics for a BFD session
func (m *BFDManager) GetSessionStats(peerIP net.IP) (packetsRx, packetsTx, flaps uint64, ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	peerKey := peerIP.String()
	if session, exists := m.sessions[peerKey]; exists {
		session.mu.RLock()
		defer session.mu.RUnlock()
		return session.packetsRx, session.packetsTx, session.sessionFlaps, true
	}
	return 0, 0, 0, false
}

// clampDurationToMicroseconds safely converts a time.Duration to microseconds
// as uint32, clamping to [0, math.MaxUint32] to avoid overflow (gosec G115).
func clampDurationToMicroseconds(d time.Duration) uint32 {
	us := int64(d / time.Microsecond)
	if us < 0 {
		return 0
	}
	if us > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(us)
}
