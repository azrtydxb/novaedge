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

package federation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Manager orchestrates federation between controllers
type Manager struct {
	// Configuration
	config *FederationConfig

	// Server handles incoming connections from peers
	server *Server

	// Clients for connecting to peers
	clients   map[string]*PeerClient
	clientsMu sync.RWMutex

	// Logger
	logger *zap.Logger

	// Context for lifecycle management
	ctx    context.Context
	cancel context.CancelFunc

	// State
	started bool
	mu      sync.RWMutex

	// Callbacks
	onResourceChange func(key ResourceKey, changeType ChangeType, data []byte)
}

// NewManager creates a new federation manager
func NewManager(config *FederationConfig, logger *zap.Logger) *Manager {
	return &Manager{
		config:  config,
		clients: make(map[string]*PeerClient),
		logger:  logger.Named("federation-manager"),
	}
}

// NewManagerFromCRD creates a federation manager from a NovaEdgeFederation CRD
func NewManagerFromCRD(federation *novaedgev1alpha1.NovaEdgeFederation, logger *zap.Logger) (*Manager, error) {
	config := crdToConfig(federation)
	return NewManager(config, logger), nil
}

// TLSCredentials holds TLS certificate data for a peer
type TLSCredentials struct {
	CACert     []byte
	ClientCert []byte
	ClientKey  []byte
}

// NewManagerFromCRDWithCreds creates a federation manager from a NovaEdgeFederation CRD with TLS credentials
func NewManagerFromCRDWithCreds(federation *novaedgev1alpha1.NovaEdgeFederation, tlsCreds map[string]*TLSCredentials, logger *zap.Logger) (*Manager, error) {
	config := crdToConfig(federation)

	// Apply TLS credentials to peers
	for _, peer := range config.Peers {
		if creds, ok := tlsCreds[peer.Name]; ok && creds != nil {
			peer.CACert = creds.CACert
			peer.ClientCert = creds.ClientCert
			peer.ClientKey = creds.ClientKey
			logger.Debug("Applied TLS credentials to peer",
				zap.String("peer", peer.Name),
				zap.Bool("has_ca", len(creds.CACert) > 0),
				zap.Bool("has_client_cert", len(creds.ClientCert) > 0),
			)
		}
	}

	return NewManager(config, logger), nil
}

// crdToConfig converts a NovaEdgeFederation CRD to a FederationConfig
func crdToConfig(fed *novaedgev1alpha1.NovaEdgeFederation) *FederationConfig {
	config := DefaultFederationConfig()
	config.FederationID = fed.Spec.FederationID

	// Local member
	config.LocalMember = &PeerInfo{
		Name:     fed.Spec.LocalMember.Name,
		Endpoint: fed.Spec.LocalMember.Endpoint,
		Region:   fed.Spec.LocalMember.Region,
		Zone:     fed.Spec.LocalMember.Zone,
		Labels:   fed.Spec.LocalMember.Labels,
	}

	// Peers
	for _, peer := range fed.Spec.Members {
		peerInfo := &PeerInfo{
			Name:     peer.Name,
			Endpoint: peer.Endpoint,
			Region:   peer.Region,
			Zone:     peer.Zone,
			Priority: peer.Priority,
			Labels:   peer.Labels,
		}

		if peer.TLS != nil {
			peerInfo.TLSEnabled = peer.TLS.Enabled == nil || *peer.TLS.Enabled
			peerInfo.TLSServerName = peer.TLS.ServerName
			peerInfo.InsecureSkipVerify = peer.TLS.InsecureSkipVerify
		}

		config.Peers = append(config.Peers, peerInfo)
	}

	// Sync config
	if fed.Spec.Sync != nil {
		if fed.Spec.Sync.Interval != nil {
			config.SyncInterval = fed.Spec.Sync.Interval.Duration
		}
		if fed.Spec.Sync.Timeout != nil {
			config.SyncTimeout = fed.Spec.Sync.Timeout.Duration
		}
		if fed.Spec.Sync.BatchSize > 0 {
			config.BatchSize = fed.Spec.Sync.BatchSize
		}
		if fed.Spec.Sync.Compression != nil {
			config.CompressionEnabled = *fed.Spec.Sync.Compression
		}
		config.ResourceTypes = fed.Spec.Sync.ResourceTypes
		config.ExcludeNamespaces = fed.Spec.Sync.ExcludeNamespaces
	}

	// Conflict resolution
	if fed.Spec.ConflictResolution != nil {
		config.ConflictResolutionStrategy = string(fed.Spec.ConflictResolution.Strategy)
		if fed.Spec.ConflictResolution.VectorClocks != nil {
			config.VectorClocksEnabled = *fed.Spec.ConflictResolution.VectorClocks
		}
		if fed.Spec.ConflictResolution.TombstoneTTL != nil {
			config.TombstoneTTL = fed.Spec.ConflictResolution.TombstoneTTL.Duration
		}
	}

	// Health check
	if fed.Spec.HealthCheck != nil {
		if fed.Spec.HealthCheck.Interval != nil {
			config.HealthCheckInterval = fed.Spec.HealthCheck.Interval.Duration
		}
		if fed.Spec.HealthCheck.Timeout != nil {
			config.HealthCheckTimeout = fed.Spec.HealthCheck.Timeout.Duration
		}
		if fed.Spec.HealthCheck.FailureThreshold > 0 {
			config.FailureThreshold = fed.Spec.HealthCheck.FailureThreshold
		}
		if fed.Spec.HealthCheck.SuccessThreshold > 0 {
			config.SuccessThreshold = fed.Spec.HealthCheck.SuccessThreshold
		}
	}

	return config
}

// Start begins the federation manager
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return fmt.Errorf("federation manager already started")
	}
	m.started = true
	m.mu.Unlock()

	derivedCtx, cancel := context.WithCancel(ctx)
	m.ctx = derivedCtx
	m.cancel = cancel

	m.logger.Info("Starting federation manager",
		zap.String("federation_id", m.config.FederationID),
		zap.String("local_member", m.config.LocalMember.Name),
		zap.Int("peer_count", len(m.config.Peers)),
	)

	// Create the server
	m.server = NewServer(m.config, m.logger)

	// Register change callback if set
	if m.onResourceChange != nil {
		m.server.OnChange(m.onResourceChange)
	}

	// Start the server
	if err := m.server.Start(derivedCtx); err != nil {
		return fmt.Errorf("failed to start federation server: %w", err)
	}

	// Create clients for each peer
	for _, peer := range m.config.Peers {
		client := NewPeerClient(peer, m.config, m.logger)
		m.clientsMu.Lock()
		m.clients[peer.Name] = client
		m.clientsMu.Unlock()

		// Start connection loop
		go m.maintainPeerConnection(peer.Name, client)
	}

	// Start health checker
	go m.runHealthChecker(derivedCtx)

	return nil
}

// Stop gracefully shuts down the federation manager
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	m.started = false
	m.mu.Unlock()

	m.logger.Info("Stopping federation manager")

	// Cancel context
	if m.cancel != nil {
		m.cancel()
	}

	// Stop server
	if m.server != nil {
		m.server.Stop()
	}

	// Disconnect all clients
	m.clientsMu.Lock()
	for _, client := range m.clients {
		client.Disconnect()
	}
	m.clients = make(map[string]*PeerClient)
	m.clientsMu.Unlock()
}

// RegisterServer registers the federation gRPC service with a server
func (m *Manager) RegisterServer(grpcServer *grpc.Server) {
	if m.server != nil {
		m.server.RegisterServer(grpcServer)
	}
}

// RecordChange records a local resource change to propagate to peers
func (m *Manager) RecordChange(key ResourceKey, changeType ChangeType, data []byte, labels map[string]string) {
	if m.server != nil {
		m.server.RecordLocalChange(key, changeType, data, labels)
	}
}

// OnResourceChange sets a callback for when resources change (from peers)
func (m *Manager) OnResourceChange(fn func(key ResourceKey, changeType ChangeType, data []byte)) {
	m.onResourceChange = fn
	if m.server != nil {
		m.server.OnChange(fn)
	}
}

// GetPhase returns the current federation phase
func (m *Manager) GetPhase() FederationPhase {
	if m.server != nil {
		return m.server.getPhase()
	}
	return PhaseInitializing
}

// GetPeerStates returns the state of all peers
func (m *Manager) GetPeerStates() map[string]*PeerState {
	if m.server != nil {
		return m.server.GetPeerStates()
	}
	return nil
}

// GetStats returns federation statistics
func (m *Manager) GetStats() SyncStats {
	if m.server != nil {
		return m.server.GetStats()
	}
	return SyncStats{}
}

// GetConflicts returns pending conflicts
func (m *Manager) GetConflicts() []*ConflictInfo {
	if m.server != nil {
		return m.server.GetConflicts()
	}
	return nil
}

// ResolveConflict resolves a pending conflict
func (m *Manager) ResolveConflict(keyStr string, useLocal bool) error {
	if m.server != nil {
		return m.server.ResolveConflict(keyStr, useLocal)
	}
	return fmt.Errorf("federation server not started")
}

// GetVectorClock returns the current vector clock
func (m *Manager) GetVectorClock() map[string]int64 {
	if m.server != nil {
		return m.server.vectorClock.ToMap()
	}
	return nil
}

// SetAgentCount updates the connected agent count
func (m *Manager) SetAgentCount(count int32) {
	if m.server != nil {
		m.server.SetAgentCount(count)
	}
}

// maintainPeerConnection maintains a connection to a peer
func (m *Manager) maintainPeerConnection(peerName string, client *PeerClient) {
	backoff := time.Second

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		// Connect to peer
		if err := client.Connect(m.ctx); err != nil {
			m.logger.Error("Failed to connect to peer",
				zap.String("peer", peerName),
				zap.Error(err),
			)
			time.Sleep(backoff)
			backoff = min(backoff*2, 30*time.Second)
			continue
		}

		// Start sync stream
		if err := client.StartSyncStream(m.ctx, m.server.vectorClock.ToMap()); err != nil {
			m.logger.Error("Failed to start sync stream",
				zap.String("peer", peerName),
				zap.Error(err),
			)
			client.Disconnect()
			time.Sleep(backoff)
			backoff = min(backoff*2, 30*time.Second)
			continue
		}

		// Reset backoff on successful connection
		backoff = time.Second

		// Set up message handler
		client.OnMessage(func(msg *pb.SyncMessage) {
			m.handlePeerMessage(peerName, msg)
		})

		// Set up disconnect handler
		disconnected := make(chan struct{})
		client.OnDisconnect(func() {
			close(disconnected)
		})

		// Wait for disconnect or context cancellation
		select {
		case <-m.ctx.Done():
			client.Disconnect()
			return
		case <-disconnected:
			m.logger.Info("Peer disconnected, will reconnect",
				zap.String("peer", peerName),
			)
			time.Sleep(backoff)
		}
	}
}

// handlePeerMessage processes a message from a peer
func (m *Manager) handlePeerMessage(peerName string, msg *pb.SyncMessage) {
	// Forward to server for processing
	switch msgType := msg.Message.(type) {
	case *pb.SyncMessage_Change:
		if err := m.server.handleResourceChange(m.ctx, peerName, msgType.Change); err != nil {
			m.logger.Error("Failed to handle resource change",
				zap.String("peer", peerName),
				zap.Error(err),
			)
		}

	case *pb.SyncMessage_Ack:
		m.server.handleSyncAck(peerName, msgType.Ack)

	case *pb.SyncMessage_Heartbeat:
		m.server.handleHeartbeat(peerName, msgType.Heartbeat)

	case *pb.SyncMessage_Conflict:
		m.server.handleConflictNotification(peerName, msgType.Conflict)
	}
}

// runHealthChecker periodically checks peer health
func (m *Manager) runHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(m.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkPeerHealth(ctx)
		}
	}
}

// checkPeerHealth checks the health of all peers
func (m *Manager) checkPeerHealth(ctx context.Context) {
	m.clientsMu.RLock()
	clients := make([]*PeerClient, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}
	m.clientsMu.RUnlock()

	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *PeerClient) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(ctx, m.config.HealthCheckTimeout)
			defer cancel()

			latency, err := c.Ping(ctx)
			if err != nil {
				m.logger.Debug("Peer health check failed",
					zap.String("peer", c.peer.Name),
					zap.Error(err),
				)
			} else {
				m.logger.Debug("Peer health check succeeded",
					zap.String("peer", c.peer.Name),
					zap.Duration("latency", latency),
				)
			}
		}(client)
	}
	wg.Wait()
}

// RequestFullSync requests a full sync from a specific peer
func (m *Manager) RequestFullSync(peerName string) error {
	m.clientsMu.RLock()
	client, ok := m.clients[peerName]
	m.clientsMu.RUnlock()

	if !ok {
		return fmt.Errorf("peer not found: %s", peerName)
	}

	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	defer cancel()

	batches, err := client.RequestFullSync(ctx, m.config.ResourceTypes, nil, m.server.vectorClock.ToMap())
	if err != nil {
		return fmt.Errorf("failed to request full sync: %w", err)
	}

	m.logger.Info("Received full sync from peer",
		zap.String("peer", peerName),
		zap.Int("batches", len(batches)),
	)

	// Apply all resources from the sync
	for _, batch := range batches {
		for _, change := range batch.Resources {
			if err := m.server.handleResourceChange(ctx, peerName, change); err != nil {
				m.logger.Error("Failed to apply synced resource",
					zap.String("key", fmt.Sprintf("%s/%s/%s", change.ResourceType, change.Namespace, change.Name)),
					zap.Error(err),
				)
			}
		}
	}

	return nil
}

// IsHealthy returns whether the federation is healthy
func (m *Manager) IsHealthy() bool {
	return m.GetPhase() == PhaseHealthy
}

// IsDegraded returns whether the federation is degraded
func (m *Manager) IsDegraded() bool {
	phase := m.GetPhase()
	return phase == PhaseDegraded || phase == PhasePartitioned
}

// min returns the minimum of two durations
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
