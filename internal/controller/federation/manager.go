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
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

var (
	errFederationManagerAlreadyStarted = errors.New("federation manager already started")
	errFederationServerNotStarted      = errors.New("federation server not started")
	errFederationManagerIsNotStarted   = errors.New("federation manager is not started")
	errPeerNotFound                    = errors.New("peer not found")
)

// Manager orchestrates federation between controllers
type Manager struct {
	// Configuration
	config *Config

	// Server handles incoming connections from peers
	server *Server

	// Split-brain detector (nil when disabled)
	splitBrain *SplitBrainDetector

	// Anti-entropy manager for drift detection and repair
	antiEntropy *AntiEntropyManager

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
func NewManager(config *Config, logger *zap.Logger) *Manager {
	return &Manager{
		config:  config,
		clients: make(map[string]*PeerClient),
		logger:  logger.Named("federation-manager"),
	}
}

// NewManagerFromCRD creates a federation manager from a NovaEdgeFederation CRD
func NewManagerFromCRD(federation *novaedgev1alpha1.NovaEdgeFederation, logger *zap.Logger) (*Manager, error) {
	config := CRDToConfig(federation)
	return NewManager(config, logger), nil
}

// TLSCredentials holds TLS certificate data for a peer
type TLSCredentials struct {
	CACert     []byte
	ClientCert []byte
	ClientKey  []byte //nolint:gosec // G117: struct field name for TLS credential holder, not a hardcoded credential
}

// NewManagerFromCRDWithCreds creates a federation manager from a NovaEdgeFederation CRD with TLS credentials
func NewManagerFromCRDWithCreds(federation *novaedgev1alpha1.NovaEdgeFederation, tlsCreds map[string]*TLSCredentials, logger *zap.Logger) (*Manager, error) {
	config := CRDToConfig(federation)

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

// CRDToConfig converts a NovaEdgeFederation CRD to a Config
func CRDToConfig(fed *novaedgev1alpha1.NovaEdgeFederation) *Config {
	config := DefaultConfig()
	config.FederationID = fed.Spec.FederationID

	// Mode (defaults to "mesh" via DefaultConfig)
	if fed.Spec.Mode != "" {
		config.Mode = string(fed.Spec.Mode)
	}

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
		return errFederationManagerAlreadyStarted
	}
	m.started = true
	m.mu.Unlock()

	derivedCtx, cancel := context.WithCancel(ctx)
	m.ctx = derivedCtx
	m.cancel = cancel

	m.logger.Info("Starting federation manager",
		zap.String("federation_id", m.config.FederationID),
		zap.String("mode", m.config.Mode),
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

	// Create and start split-brain detector when enabled
	if m.config.SplitBrain != nil {
		m.splitBrain = NewSplitBrainDetector(
			m.config.SplitBrain,
			m.server,
			len(m.config.Peers),
			m.logger,
		)
		m.splitBrain.Start(derivedCtx)
		m.logger.Info("Split-brain detector started",
			zap.Bool("fencing_enabled", m.config.SplitBrain.FencingEnabled),
			zap.Bool("quorum_required", m.config.SplitBrain.QuorumRequired),
			zap.String("quorum_mode", string(m.config.SplitBrain.QuorumMode)),
		)
	}

	// Create and start anti-entropy manager with a peer client lookup function
	// that safely retrieves clients from the Manager's client map.
	// In hub-spoke mode, anti-entropy pull is skipped because the hub only
	// pushes configuration and does not pull from spoke clusters.
	if m.config.Mode != ModeHubSpoke {
		m.antiEntropy = NewAntiEntropyManager(
			DefaultAntiEntropyConfig(),
			m.server,
			func(peerName string) (*PeerClient, bool) {
				m.clientsMu.RLock()
				defer m.clientsMu.RUnlock()
				c, ok := m.clients[peerName]
				return c, ok
			},
			m.logger,
		)
		m.antiEntropy.Start(derivedCtx)
		m.logger.Info("Anti-entropy manager started")
	} else {
		m.logger.Info("Anti-entropy skipped in hub-spoke mode")
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

	// Start periodic metrics collection
	go m.runMetricsCollector(derivedCtx)

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

	// Stop split-brain detector
	if m.splitBrain != nil {
		m.splitBrain.Stop()
	}

	// Stop anti-entropy manager
	if m.antiEntropy != nil {
		m.antiEntropy.Stop()
	}

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
func (m *Manager) GetPhase() Phase {
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
	return errFederationServerNotStarted
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

// UpdateConfig applies a new configuration to a running manager without restart.
// Peers are added, removed, or updated dynamically and sync parameters are
// adjusted in-place.
func (m *Manager) UpdateConfig(newConfig *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return errFederationManagerIsNotStarted
	}

	oldConfig := m.config

	// Nothing to do when configs are identical.
	if oldConfig.Equal(newConfig) {
		m.logger.Debug("Config unchanged, skipping update")
		return nil
	}

	m.logger.Info("Applying federation config update",
		zap.String("federation_id", newConfig.FederationID),
	)

	// --- Peer diff ---
	oldPeers := make(map[string]*PeerInfo, len(oldConfig.Peers))
	for _, p := range oldConfig.Peers {
		oldPeers[p.Name] = p
	}
	newPeers := make(map[string]*PeerInfo, len(newConfig.Peers))
	for _, p := range newConfig.Peers {
		newPeers[p.Name] = p
	}

	// Remove peers that no longer exist
	for name := range oldPeers {
		if _, exists := newPeers[name]; !exists {
			m.logger.Info("Removing federation peer", zap.String("peer", name))
			m.clientsMu.Lock()
			if client, ok := m.clients[name]; ok {
				client.Disconnect()
				delete(m.clients, name)
			}
			m.clientsMu.Unlock()
		}
	}

	// Add new peers
	for name, peerInfo := range newPeers {
		if _, exists := oldPeers[name]; !exists {
			m.logger.Info("Adding federation peer", zap.String("peer", name))
			client := NewPeerClient(peerInfo, newConfig, m.logger)
			m.clientsMu.Lock()
			m.clients[name] = client
			m.clientsMu.Unlock()
			go m.maintainPeerConnection(name, client)
		}
	}

	// Update existing peers whose TLS credentials changed
	for name, newPeer := range newPeers {
		oldPeer, exists := oldPeers[name]
		if !exists {
			continue // already handled above
		}
		if !peerInfoEqual(oldPeer, newPeer) {
			m.logger.Info("Reconnecting federation peer due to config change",
				zap.String("peer", name),
			)
			m.clientsMu.Lock()
			if client, ok := m.clients[name]; ok {
				client.Disconnect()
				delete(m.clients, name)
			}
			newClient := NewPeerClient(newPeer, newConfig, m.logger)
			m.clients[name] = newClient
			m.clientsMu.Unlock()
			go m.maintainPeerConnection(name, newClient)
		}
	}

	// --- Update server sync parameters ---
	if m.server != nil {
		if oldConfig.SyncInterval != newConfig.SyncInterval {
			m.logger.Info("Updating sync interval",
				zap.Duration("old", oldConfig.SyncInterval),
				zap.Duration("new", newConfig.SyncInterval),
			)
		}
		if oldConfig.SyncTimeout != newConfig.SyncTimeout {
			m.logger.Info("Updating sync timeout",
				zap.Duration("old", oldConfig.SyncTimeout),
				zap.Duration("new", newConfig.SyncTimeout),
			)
		}
		if oldConfig.BatchSize != newConfig.BatchSize {
			m.logger.Info("Updating batch size",
				zap.Int32("old", oldConfig.BatchSize),
				zap.Int32("new", newConfig.BatchSize),
			)
		}
		// The server holds a pointer to config; update it atomically.
		m.server.config.Store(newConfig)
	}

	// --- Update anti-entropy if interval changed ---
	if m.antiEntropy != nil && oldConfig.SyncInterval != newConfig.SyncInterval {
		m.logger.Info("Anti-entropy interval will take effect on next cycle")
	}

	// Store the new config
	m.config = newConfig

	m.logger.Info("Federation config update applied successfully")
	return nil
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
			select {
			case <-m.ctx.Done():
				return
			case <-time.After(backoff):
			}
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
			select {
			case <-m.ctx.Done():
				return
			case <-time.After(backoff):
			}
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
			select {
			case <-m.ctx.Done():
				return
			case <-time.After(backoff):
			}
		}
	}
}

// handlePeerMessage processes a message from a peer
func (m *Manager) handlePeerMessage(peerName string, msg *pb.SyncMessage) {
	// Forward to server for processing
	switch msgType := msg.Message.(type) {
	case *pb.SyncMessage_Change:
		// In hub-spoke mode the hub only pushes; ignore incoming resource
		// changes from spoke clusters to enforce one-way data flow.
		if m.config.Mode == ModeHubSpoke {
			m.logger.Debug("Ignoring incoming resource change in hub-spoke mode",
				zap.String("peer", peerName),
			)
			return
		}
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

// runMetricsCollector periodically collects federation metrics
func (m *Manager) runMetricsCollector(ctx context.Context) {
	collector := NewMetricsCollector(m)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collector.Collect()
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

			healthCtx, cancel := context.WithTimeout(ctx, m.config.HealthCheckTimeout)
			defer cancel()

			latency, err := c.Ping(healthCtx)
			if err != nil {
				m.logger.Debug("Peer health check failed",
					zap.String("peer", c.peer.Name),
					zap.Error(err),
				)
				if m.splitBrain != nil {
					m.splitBrain.RecordPeerFailure(c.peer.Name)
				}
			} else {
				m.logger.Debug("Peer health check succeeded",
					zap.String("peer", c.peer.Name),
					zap.Duration("latency", latency),
				)
				if m.splitBrain != nil {
					m.splitBrain.RecordPeerContact(c.peer.Name)
				}
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
		return fmt.Errorf("%w: %s", errPeerNotFound, peerName)
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

// AreWritesFenced returns true if the split-brain detector is fencing writes.
// When split-brain detection is disabled, writes are never fenced.
func (m *Manager) AreWritesFenced() bool {
	if m.splitBrain == nil {
		return false
	}
	return m.splitBrain.AreWritesFenced()
}

// GetPartitionInfo returns the current partition info from the split-brain detector.
// Returns nil when split-brain detection is disabled or no partition is detected.
func (m *Manager) GetPartitionInfo() *PartitionInfo {
	if m.splitBrain == nil {
		return nil
	}
	return m.splitBrain.GetPartitionInfo()
}

// GetFederationID returns the federation identifier.
func (m *Manager) GetFederationID() string {
	return m.config.FederationID
}

// GetLocalMemberName returns the name of the local federation member.
func (m *Manager) GetLocalMemberName() string {
	if m.config.LocalMember != nil {
		return m.config.LocalMember.Name
	}
	return ""
}

// IsActive returns true when the federation manager has been started and the
// underlying server is running.
func (m *Manager) IsActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started && m.server != nil
}

// GetMode returns the configured federation mode.
func (m *Manager) GetMode() string {
	return m.config.Mode
}

// IsUnifiedMode returns true when the federation operates in unified mode,
// which enforces shared service namespace and location-aware routing.
func (m *Manager) IsUnifiedMode() bool {
	return m.config.Mode == ModeUnified
}

// GetRemoteEndpoints returns the ServiceEndpoints from all federated clusters
// for the given service. Returns an empty slice when federation is not active,
// no remote endpoints exist, or the mode is hub-spoke (which does not merge
// endpoints from spoke clusters).
func (m *Manager) GetRemoteEndpoints(namespace, serviceName string) []*pb.ServiceEndpoints {
	if m.server == nil {
		return []*pb.ServiceEndpoints{}
	}
	// In hub-spoke mode, the hub does not consume endpoints from spokes
	if m.config.Mode == ModeHubSpoke {
		return []*pb.ServiceEndpoints{}
	}
	return m.server.GetEndpointCache().GetForService(namespace, serviceName)
}
