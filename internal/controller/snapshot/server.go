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

package snapshot

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/azrtydxb/novaedge/internal/controller/meshca"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

const (
	// AgentExpiryDuration is the duration after which an agent is considered disconnected
	AgentExpiryDuration = 30 * time.Second

	// StatusCleanupInterval is how often the status cleanup goroutine runs
	StatusCleanupInterval = 10 * time.Second
)

// AgentStatusInfo tracks the health and connectivity of a node agent
type AgentStatusInfo struct {
	NodeName             string
	AgentVersion         string
	AppliedConfigVersion string
	Healthy              bool
	LastSeen             time.Time
	Connected            bool
	ActiveConnections    int64
	Errors               []string
	Metrics              map[string]int64
	// Remote cluster information (empty for local agents)
	ClusterName   string
	ClusterRegion string
	ClusterZone   string
}

// ConnectionStatus represents the connection state of an agent
type ConnectionStatus string

// Connection status values for agent tracking.
const (
	// ConnectionStatusConnected indicates an agent is connected.
	ConnectionStatusConnected ConnectionStatus = "connected"
	// ConnectionStatusDisconnected indicates an agent is disconnected.
	ConnectionStatusDisconnected ConnectionStatus = "disconnected"
)

// Server implements the ConfigService gRPC server
type Server struct {
	pb.UnimplementedConfigServiceServer

	client  client.Client
	builder *Builder
	cache   *Cache

	// Channels for notifying clients of updates
	updateNotifier chan string

	// Agent status tracking
	statusMap sync.Map // map[string]*AgentStatusInfo

	// Remote agent tracking for hub-spoke deployments
	remoteAgentTracker *RemoteAgentTracker

	// Mesh CA for issuing workload certificates
	meshCA *meshca.MeshCA

	// Metrics
	activeStreams int64
	streamsMu     sync.RWMutex

	// dirty tracks whether any resource change event has occurred since the
	// last snapshot rebuild. Watch handlers set it to true; the periodic
	// rebuild loop checks and clears it. Using atomic.Bool avoids locking
	// on the hot path.
	dirty atomic.Bool

	// debounceTimer coalesces rapid TriggerUpdate calls within a 100ms window.
	// Instead of notifying agents on every watch event, the timer is reset on
	// each call and the actual notification fires only after 100ms of quiet.
	debounceTimer *time.Timer
	debounceMu    sync.Mutex

	// Shutdown channel for cleanup goroutine
	shutdownCh chan struct{}
}

// NewServer creates a new gRPC config server
func NewServer(client client.Client) *Server {
	s := &Server{
		client:             client,
		builder:            NewBuilder(client),
		cache:              NewCache(),
		updateNotifier:     make(chan string, 100),
		remoteAgentTracker: NewRemoteAgentTracker(),
		shutdownCh:         make(chan struct{}),
	}

	// Start the status cleanup goroutine
	go s.cleanupStaleAgents()

	return s
}

// SetRemoteClusterHandler sets the handler for remote cluster updates
func (s *Server) SetRemoteClusterHandler(handler RemoteClusterHandler) {
	s.remoteAgentTracker.SetHandler(handler)
}

// GetRemoteAgentTracker returns the remote agent tracker for external access
func (s *Server) GetRemoteAgentTracker() *RemoteAgentTracker {
	return s.remoteAgentTracker
}

// StreamConfig implements the StreamConfig RPC method
func (s *Server) StreamConfig(req *pb.StreamConfigRequest, stream pb.ConfigService_StreamConfigServer) error {
	isRemote := req.ClusterName != ""
	logger := log.FromContext(stream.Context()).WithValues(
		"node", req.NodeName,
		"agentVersion", req.AgentVersion,
		"clusterName", req.ClusterName,
		"clusterRegion", req.ClusterRegion,
		"isRemote", isRemote,
	)
	logger.Info("Agent connected for config stream")

	// Mark agent as connected
	s.updateAgentConnectionWithCluster(req.NodeName, req.AgentVersion, req.ClusterName, req.ClusterRegion, req.ClusterZone, true)
	defer s.updateAgentConnectionWithCluster(req.NodeName, req.AgentVersion, req.ClusterName, req.ClusterRegion, req.ClusterZone, false)

	// Track remote agents separately
	if isRemote {
		info := &RemoteAgentInfo{
			NodeName:      req.NodeName,
			ClusterName:   req.ClusterName,
			ClusterRegion: req.ClusterRegion,
			ClusterZone:   req.ClusterZone,
			AgentVersion:  req.AgentVersion,
			Connected:     true,
			Healthy:       true,
			LastSeen:      time.Now(),
			Labels:        req.ClusterLabels,
		}
		s.remoteAgentTracker.RegisterAgent(info)
		defer s.remoteAgentTracker.UnregisterAgent(req.ClusterName, req.NodeName)
	}

	s.incrementStreams()
	defer s.decrementStreams()
	UpdateActiveStreams(s.GetActiveStreamCount())

	// Build initial snapshot for this node
	snapshot, err := s.builder.BuildSnapshot(stream.Context(), req.NodeName)
	if err != nil {
		logger.Error(err, "Failed to build initial snapshot")
		RecordSnapshotError(req.NodeName, "initial_build")
		return status.Errorf(codes.Internal, "failed to build snapshot: %v", err)
	}

	// Cache the snapshot
	s.cache.Set(req.NodeName, snapshot)
	UpdateCachedSnapshots(s.cache.GetCacheSize())

	// Send initial snapshot
	if err := stream.Send(snapshot); err != nil {
		logger.Error(err, "Failed to send initial snapshot")
		return status.Errorf(codes.Internal, "failed to send snapshot: %v", err)
	}

	RecordSnapshotUpdate(req.NodeName, "initial")
	logger.Info("Sent initial config snapshot", "version", snapshot.Version)

	// Track last sent version to skip unchanged snapshots
	lastVersion := snapshot.Version

	// Create update channel for this node
	updateCh := make(chan string, 10)
	s.cache.Subscribe(req.NodeName, updateCh)
	defer s.cache.Unsubscribe(req.NodeName, updateCh)

	// Listen for updates.
	// The periodic ticker is a 30-second fallback safety net. Actual changes
	// are delivered through updateCh (triggered by watch handlers). The
	// periodic rebuild only fires when the dirty flag is set, avoiding
	// unnecessary List() calls when nothing has changed.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			logger.Info("Stream context cancelled")
			return stream.Context().Err()

		case <-ticker.C:
			// Only rebuild if a resource change event has occurred
			if !s.dirty.Load() {
				continue
			}
			s.dirty.Store(false)

			// Periodic safety-net rebuild
			newSnapshot, err := s.builder.BuildSnapshot(stream.Context(), req.NodeName)
			if err != nil {
				logger.Error(err, "Failed to rebuild snapshot")
				RecordSnapshotError(req.NodeName, "periodic_rebuild")
				continue
			}

			// Skip if version hasn't changed since last push
			if newSnapshot.Version == lastVersion {
				continue
			}

			if err := stream.Send(newSnapshot); err != nil {
				logger.Error(err, "Failed to send updated snapshot")
				return status.Errorf(codes.Internal, "failed to send snapshot: %v", err)
			}
			lastVersion = newSnapshot.Version
			snapshot = newSnapshot
			s.cache.Set(req.NodeName, snapshot)
			RecordSnapshotUpdate(req.NodeName, "periodic")
			logger.Info("Sent updated config snapshot", "version", snapshot.Version)

		case <-updateCh:
			// Triggered update - rebuild and send
			newSnapshot, err := s.builder.BuildSnapshot(stream.Context(), req.NodeName)
			if err != nil {
				logger.Error(err, "Failed to rebuild snapshot after trigger")
				RecordSnapshotError(req.NodeName, "triggered_rebuild")
				continue
			}

			// Skip if version hasn't changed since last push (content-hash dedup)
			if newSnapshot.Version == lastVersion {
				continue
			}

			if err := stream.Send(newSnapshot); err != nil {
				logger.Error(err, "Failed to send triggered snapshot")
				return status.Errorf(codes.Internal, "failed to send snapshot: %v", err)
			}

			lastVersion = newSnapshot.Version
			snapshot = newSnapshot
			s.cache.Set(req.NodeName, snapshot)
			RecordSnapshotUpdate(req.NodeName, "triggered")
			logger.Info("Sent triggered config snapshot", "version", snapshot.Version)
		}
	}
}

// ReportStatus implements the ReportStatus RPC method
func (s *Server) ReportStatus(ctx context.Context, req *pb.AgentStatus) (*pb.StatusResponse, error) {
	isRemote := req.ClusterName != ""
	logger := log.FromContext(ctx).WithValues(
		"node", req.NodeName,
		"version", req.AppliedConfigVersion,
		"healthy", req.Healthy,
		"clusterName", req.ClusterName,
		"isRemote", isRemote,
	)

	if !req.Healthy {
		logger.Info("Agent reported unhealthy", "errors", req.Errors)
	}

	// Update metrics
	UpdateAgentStatus(req.NodeName, req.AppliedConfigVersion, req.Healthy)

	// Store agent status for monitoring/observability
	s.storeAgentStatus(req)

	// Update remote agent tracker if this is a remote agent
	if isRemote {
		s.remoteAgentTracker.UpdateAgentStatus(req.ClusterName, req.NodeName, req.Healthy)
	}

	return &pb.StatusResponse{
		Acknowledged: true,
	}, nil
}

// TriggerUpdate triggers a config update for all nodes or a specific node.
// It also marks the server as dirty so the periodic fallback rebuild knows
// that resources have changed.
//
// Rapid calls are debounced within a 100ms coalescing window: the actual
// notification is delayed until 100ms after the last call, preventing
// unnecessary snapshot rebuilds during bursts of watch events.
func (s *Server) TriggerUpdate(nodeName string) {
	s.dirty.Store(true)

	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()

	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
	}
	s.debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
		s.doTrigger(nodeName)
	})
}

// doTrigger performs the actual cache notification after the debounce window.
func (s *Server) doTrigger(nodeName string) {
	if nodeName == "" {
		s.cache.NotifyAll()
	} else {
		s.cache.Notify(nodeName)
	}
}

// GetActiveStreamCount returns the number of active streams
func (s *Server) GetActiveStreamCount() int64 {
	s.streamsMu.RLock()
	defer s.streamsMu.RUnlock()
	return s.activeStreams
}

func (s *Server) incrementStreams() {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	s.activeStreams++
	UpdateActiveStreams(s.activeStreams)
}

func (s *Server) decrementStreams() {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	s.activeStreams--
	UpdateActiveStreams(s.activeStreams)
}

// RegisterServer registers the config service with a gRPC server
func (s *Server) RegisterServer(grpcServer *grpc.Server) {
	pb.RegisterConfigServiceServer(grpcServer, s)
}

// storeAgentStatus stores the status information from an agent report
func (s *Server) storeAgentStatus(req *pb.AgentStatus) {
	// Get or create agent status info
	var status *AgentStatusInfo
	if val, ok := s.statusMap.Load(req.NodeName); ok {
		if s, ok := val.(*AgentStatusInfo); ok {
			status = s
		} else {
			status = &AgentStatusInfo{
				NodeName: req.NodeName,
			}
		}
	} else {
		status = &AgentStatusInfo{
			NodeName: req.NodeName,
		}
	}

	// Update status fields
	status.AppliedConfigVersion = req.AppliedConfigVersion
	status.Healthy = req.Healthy
	status.LastSeen = time.Now()
	status.Errors = req.Errors
	status.Metrics = req.Metrics

	// Extract active connections from metrics if present
	if activeConns, ok := req.Metrics["active_connections"]; ok {
		status.ActiveConnections = activeConns
	}

	// Store updated status
	s.statusMap.Store(req.NodeName, status)
}

// updateAgentConnection updates the connection status of an agent
func (s *Server) updateAgentConnection(nodeName, agentVersion string, connected bool) {
	s.updateAgentConnectionWithCluster(nodeName, agentVersion, "", "", "", connected)
}

// updateAgentConnectionWithCluster updates the connection status of an agent with cluster info
func (s *Server) updateAgentConnectionWithCluster(nodeName, agentVersion, clusterName, clusterRegion, clusterZone string, connected bool) {
	// For remote agents, use cluster-scoped key
	key := nodeName
	if clusterName != "" {
		key = clusterName + "/" + nodeName
	}

	// Get or create agent status info
	var status *AgentStatusInfo
	if val, ok := s.statusMap.Load(key); ok {
		if s, ok := val.(*AgentStatusInfo); ok {
			status = s
		} else {
			status = &AgentStatusInfo{
				NodeName: nodeName,
			}
		}
	} else {
		status = &AgentStatusInfo{
			NodeName: nodeName,
		}
	}

	// Update connection fields
	status.Connected = connected
	status.AgentVersion = agentVersion
	status.LastSeen = time.Now()
	status.ClusterName = clusterName
	status.ClusterRegion = clusterRegion
	status.ClusterZone = clusterZone

	// Store updated status
	s.statusMap.Store(key, status)
}

// GetAgentStatus retrieves the status of a specific agent
func (s *Server) GetAgentStatus(nodeName string) (*AgentStatusInfo, bool) {
	val, ok := s.statusMap.Load(nodeName)
	if !ok {
		return nil, false
	}

	status, ok := val.(*AgentStatusInfo)
	if !ok {
		return nil, false
	}
	// Create a copy to avoid concurrent modification issues
	statusCopy := *status
	if status.Errors != nil {
		statusCopy.Errors = make([]string, len(status.Errors))
		copy(statusCopy.Errors, status.Errors)
	}
	if status.Metrics != nil {
		statusCopy.Metrics = make(map[string]int64)
		for k, v := range status.Metrics {
			statusCopy.Metrics[k] = v
		}
	}

	return &statusCopy, true
}

// GetAllAgentStatuses retrieves the status of all agents
func (s *Server) GetAllAgentStatuses() []*AgentStatusInfo {
	statuses := make([]*AgentStatusInfo, 0)

	s.statusMap.Range(func(_, value interface{}) bool {
		status, ok := value.(*AgentStatusInfo)
		if !ok {
			return true
		}
		// Create a copy to avoid concurrent modification issues
		statusCopy := *status
		if status.Errors != nil {
			statusCopy.Errors = make([]string, len(status.Errors))
			copy(statusCopy.Errors, status.Errors)
		}
		if status.Metrics != nil {
			statusCopy.Metrics = make(map[string]int64)
			for k, v := range status.Metrics {
				statusCopy.Metrics[k] = v
			}
		}
		statuses = append(statuses, &statusCopy)
		return true
	})

	return statuses
}

// cleanupStaleAgents runs periodically to mark agents as disconnected if they haven't reported in
func (s *Server) cleanupStaleAgents() {
	ticker := time.NewTicker(StatusCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			s.statusMap.Range(func(key, value interface{}) bool {
				status, ok := value.(*AgentStatusInfo)
				if !ok {
					return true
				}

				// If agent hasn't been seen in AgentExpiryDuration, mark as disconnected
				if now.Sub(status.LastSeen) > AgentExpiryDuration && status.Connected {
					status.Connected = false
					s.statusMap.Store(key, status)

					// Log the disconnection
					log.Log.Info("Agent marked as disconnected due to inactivity",
						"node", status.NodeName,
						"lastSeen", status.LastSeen,
					)
				}
				return true
			})
		case <-s.shutdownCh:
			return
		}
	}
}

// SetMeshCA configures the mesh CA for issuing workload certificates.
func (s *Server) SetMeshCA(ca *meshca.MeshCA) {
	s.meshCA = ca
}

// SetFederationProvider sets the federation state provider on the underlying
// snapshot builder so that built snapshots include federation metadata and
// remote endpoints from federated clusters.
func (s *Server) SetFederationProvider(provider FederationStateProvider) {
	s.builder.SetFederationProvider(provider)
}

// RequestMeshCertificate implements the RequestMeshCertificate RPC.
// Agents call this to obtain a signed mesh workload certificate.
func (s *Server) RequestMeshCertificate(ctx context.Context, req *pb.MeshCertificateRequest) (*pb.MeshCertificateResponse, error) {
	logger := log.FromContext(ctx).WithValues("node", req.NodeName)

	if s.meshCA == nil {
		return nil, status.Errorf(codes.Unavailable, "mesh CA not initialized")
	}

	if len(req.Csr) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "CSR is required")
	}
	if req.NodeName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "node_name is required")
	}

	certPEM, err := s.meshCA.SignCSR(req.Csr, req.NodeName)
	if err != nil {
		logger.Error(err, "Failed to sign mesh CSR")
		return nil, status.Errorf(codes.Internal, "failed to sign CSR: %v", err)
	}

	spiffeID := fmt.Sprintf("spiffe://%s/agent/%s", s.meshCA.TrustDomain(), req.NodeName)

	logger.Info("Issued mesh workload certificate", "spiffeID", spiffeID)

	return &pb.MeshCertificateResponse{
		Certificate:   certPEM,
		CaCertificate: s.meshCA.CACertPEM(),
		SpiffeId:      spiffeID,
		ExpiryUnix:    time.Now().Add(24 * time.Hour).Unix(),
	}, nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() {
	s.debounceMu.Lock()
	if s.debounceTimer != nil {
		s.debounceTimer.Stop()
	}
	s.debounceMu.Unlock()
	close(s.shutdownCh)
}

// Cache caches config snapshots and manages update notifications
type Cache struct {
	mu          sync.RWMutex
	snapshots   map[string]*pb.ConfigSnapshot
	subscribers map[string][]chan string
}

// NewCache creates a new snapshot cache
func NewCache() *Cache {
	return &Cache{
		snapshots:   make(map[string]*pb.ConfigSnapshot),
		subscribers: make(map[string][]chan string),
	}
}

// Get retrieves a cached snapshot for a node
func (c *Cache) Get(nodeName string) (*pb.ConfigSnapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	snapshot, ok := c.snapshots[nodeName]
	return snapshot, ok
}

// Set caches a snapshot for a node
func (c *Cache) Set(nodeName string, snapshot *pb.ConfigSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshots[nodeName] = snapshot
}

// Subscribe registers a channel to receive update notifications for a node
func (c *Cache) Subscribe(nodeName string, ch chan string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribers[nodeName] = append(c.subscribers[nodeName], ch)
}

// Unsubscribe removes a channel from update notifications
func (c *Cache) Unsubscribe(nodeName string, ch chan string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	subs := c.subscribers[nodeName]
	for i, sub := range subs {
		if sub == ch {
			c.subscribers[nodeName] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
}

// Notify sends an update notification to subscribers of a specific node
func (c *Cache) Notify(nodeName string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, ch := range c.subscribers[nodeName] {
		select {
		case ch <- nodeName:
		default:
			// Channel full, skip
		}
	}
}

// NotifyAll sends an update notification to all subscribers
func (c *Cache) NotifyAll() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for nodeName := range c.subscribers {
		for _, ch := range c.subscribers[nodeName] {
			select {
			case ch <- nodeName:
			default:
				// Channel full, skip
			}
		}
	}
}

// Clear removes all cached snapshots
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshots = make(map[string]*pb.ConfigSnapshot)
}

// GetCacheSize returns the number of cached snapshots
func (c *Cache) GetCacheSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.snapshots)
}

// GetVersion returns the version of a cached snapshot
func (c *Cache) GetVersion(nodeName string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if snapshot, ok := c.snapshots[nodeName]; ok {
		return snapshot.Version
	}
	return ""
}

// String returns a human-readable representation of the cache
func (c *Cache) String() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return fmt.Sprintf("Cache{snapshots=%d, subscribers=%d}",
		len(c.snapshots), len(c.subscribers))
}
