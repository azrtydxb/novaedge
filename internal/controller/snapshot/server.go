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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Server implements the ConfigService gRPC server
type Server struct {
	pb.UnimplementedConfigServiceServer

	client  client.Client
	builder *Builder
	cache   *SnapshotCache

	// Channels for notifying clients of updates
	updateNotifier chan string

	// Metrics
	activeStreams int64
	streamsMu     sync.RWMutex
}

// NewServer creates a new gRPC config server
func NewServer(client client.Client) *Server {
	return &Server{
		client:         client,
		builder:        NewBuilder(client),
		cache:          NewSnapshotCache(),
		updateNotifier: make(chan string, 100),
	}
}

// StreamConfig implements the StreamConfig RPC method
func (s *Server) StreamConfig(req *pb.StreamConfigRequest, stream pb.ConfigService_StreamConfigServer) error {
	logger := log.FromContext(stream.Context()).WithValues(
		"node", req.NodeName,
		"agentVersion", req.AgentVersion,
	)
	logger.Info("Agent connected for config stream")

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

	// Create update channel for this node
	updateCh := make(chan string, 10)
	s.cache.Subscribe(req.NodeName, updateCh)
	defer s.cache.Unsubscribe(req.NodeName, updateCh)

	// Listen for updates
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			logger.Info("Stream context cancelled")
			return stream.Context().Err()

		case <-ticker.C:
			// Periodic health check - rebuild snapshot
			newSnapshot, err := s.builder.BuildSnapshot(stream.Context(), req.NodeName)
			if err != nil {
				logger.Error(err, "Failed to rebuild snapshot")
				RecordSnapshotError(req.NodeName, "periodic_rebuild")
				continue
			}

			// Only send if version changed
			if newSnapshot.Version != snapshot.Version {
				if err := stream.Send(newSnapshot); err != nil {
					logger.Error(err, "Failed to send updated snapshot")
					return status.Errorf(codes.Internal, "failed to send snapshot: %v", err)
				}
				snapshot = newSnapshot
				s.cache.Set(req.NodeName, snapshot)
				RecordSnapshotUpdate(req.NodeName, "periodic")
				logger.Info("Sent updated config snapshot", "version", snapshot.Version)
			}

		case <-updateCh:
			// Triggered update - rebuild and send
			newSnapshot, err := s.builder.BuildSnapshot(stream.Context(), req.NodeName)
			if err != nil {
				logger.Error(err, "Failed to rebuild snapshot after trigger")
				RecordSnapshotError(req.NodeName, "triggered_rebuild")
				continue
			}

			if err := stream.Send(newSnapshot); err != nil {
				logger.Error(err, "Failed to send triggered snapshot")
				return status.Errorf(codes.Internal, "failed to send snapshot: %v", err)
			}

			snapshot = newSnapshot
			s.cache.Set(req.NodeName, snapshot)
			RecordSnapshotUpdate(req.NodeName, "triggered")
			logger.Info("Sent triggered config snapshot", "version", snapshot.Version)
		}
	}
}

// ReportStatus implements the ReportStatus RPC method
func (s *Server) ReportStatus(ctx context.Context, req *pb.AgentStatus) (*pb.StatusResponse, error) {
	logger := log.FromContext(ctx).WithValues(
		"node", req.NodeName,
		"version", req.AppliedConfigVersion,
		"healthy", req.Healthy,
	)

	if !req.Healthy {
		logger.Info("Agent reported unhealthy", "errors", req.Errors)
	}

	// Update metrics
	UpdateAgentStatus(req.NodeName, req.AppliedConfigVersion, req.Healthy)

	// TODO: Store agent status for monitoring/observability
	// Could be stored in ConfigMap or custom resource

	return &pb.StatusResponse{
		Acknowledged: true,
	}, nil
}

// TriggerUpdate triggers a config update for all nodes or a specific node
func (s *Server) TriggerUpdate(nodeName string) {
	if nodeName == "" {
		// Trigger update for all nodes
		s.cache.NotifyAll()
	} else {
		// Trigger update for specific node
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

// SnapshotCache caches config snapshots and manages update notifications
type SnapshotCache struct {
	mu          sync.RWMutex
	snapshots   map[string]*pb.ConfigSnapshot
	subscribers map[string][]chan string
}

// NewSnapshotCache creates a new snapshot cache
func NewSnapshotCache() *SnapshotCache {
	return &SnapshotCache{
		snapshots:   make(map[string]*pb.ConfigSnapshot),
		subscribers: make(map[string][]chan string),
	}
}

// Get retrieves a cached snapshot for a node
func (c *SnapshotCache) Get(nodeName string) (*pb.ConfigSnapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	snapshot, ok := c.snapshots[nodeName]
	return snapshot, ok
}

// Set caches a snapshot for a node
func (c *SnapshotCache) Set(nodeName string, snapshot *pb.ConfigSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshots[nodeName] = snapshot
}

// Subscribe registers a channel to receive update notifications for a node
func (c *SnapshotCache) Subscribe(nodeName string, ch chan string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribers[nodeName] = append(c.subscribers[nodeName], ch)
}

// Unsubscribe removes a channel from update notifications
func (c *SnapshotCache) Unsubscribe(nodeName string, ch chan string) {
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
func (c *SnapshotCache) Notify(nodeName string) {
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
func (c *SnapshotCache) NotifyAll() {
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
func (c *SnapshotCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshots = make(map[string]*pb.ConfigSnapshot)
}

// GetCacheSize returns the number of cached snapshots
func (c *SnapshotCache) GetCacheSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.snapshots)
}

// GetVersion returns the version of a cached snapshot
func (c *SnapshotCache) GetVersion(nodeName string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if snapshot, ok := c.snapshots[nodeName]; ok {
		return snapshot.Version
	}
	return ""
}

// String returns a human-readable representation of the cache
func (c *SnapshotCache) String() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return fmt.Sprintf("SnapshotCache{snapshots=%d, subscribers=%d}",
		len(c.snapshots), len(c.subscribers))
}
