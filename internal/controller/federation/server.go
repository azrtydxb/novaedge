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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

var (
	errUnexpectedTypeInResourcesMapForKey = errors.New("unexpected type in resources map for key")
	errConflictNotFound                   = errors.New("conflict not found")
	errUnexpectedTypeForConflict          = errors.New("unexpected type for conflict")
)

const (
	// ProtocolVersion is the current federation protocol version
	ProtocolVersion = 1

	// HeartbeatInterval is how often to send heartbeats on sync streams
	HeartbeatInterval = 10 * time.Second

	// StreamTimeout is the timeout for stream operations
	StreamTimeout = 30 * time.Second
)

// Server implements the FederationService gRPC server
type Server struct {
	pb.UnimplementedFederationServiceServer

	// config holds the federation configuration
	config *Config

	// vectorClock is this controller's vector clock
	vectorClock *VectorClock

	// peerStates tracks the state of each peer
	peerStates sync.Map // map[string]*PeerState

	// resources tracks all synced resources
	resources sync.Map // map[string]*TrackedResource (keyed by ResourceKey.String())

	// tombstones tracks deleted resources
	tombstones sync.Map // map[string]*Tombstone

	// pendingChanges is the queue of changes to propagate
	pendingChanges chan *ChangeEntry

	// conflicts tracks unresolved conflicts
	conflicts sync.Map // map[string]*ConflictInfo

	// stats tracks sync statistics
	stats   *SyncStats
	statsMu sync.RWMutex

	// logger for federation operations
	logger *zap.Logger

	// shutdown channel for cleanup
	shutdownCh chan struct{}

	// activeStreams tracks active peer connections
	activeStreams sync.Map // map[string]context.CancelFunc

	// endpointCache stores remote cluster endpoints received via federation
	endpointCache *RemoteEndpointCache

	// changeCallbacks are called when a resource changes
	changeCallbacks []func(key ResourceKey, change ChangeType, data []byte)
	callbackMu      sync.RWMutex

	// agentCount tracks connected agents (for heartbeats)
	agentCount int32
	agentMu    sync.RWMutex
}

// NewServer creates a new federation server
func NewServer(config *Config, logger *zap.Logger) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	s := &Server{
		config:         config,
		vectorClock:    NewVectorClock(),
		pendingChanges: make(chan *ChangeEntry, 10000),
		endpointCache:  NewRemoteEndpointCache(),
		stats:          &SyncStats{},
		logger:         logger.Named("federation"),
		shutdownCh:     make(chan struct{}),
	}

	// Initialize vector clock with our member
	if config.LocalMember != nil {
		s.vectorClock.Increment(config.LocalMember.Name)
	}

	return s
}

// Start begins background processing for the federation server
func (s *Server) Start(ctx context.Context) error {
	s.logger.Info("Starting federation server",
		zap.String("federation_id", s.config.FederationID),
		zap.String("local_member", s.config.LocalMember.Name),
	)

	// Start the tombstone cleanup goroutine
	go s.cleanupTombstones(ctx)

	// Peer connections are managed by the federation Manager, not the Server.
	// See Manager.Start() in manager.go which spawns maintainPeerConnection
	// goroutines for each peer with proper gRPC client handling.

	return nil
}

// Stop gracefully shuts down the federation server
func (s *Server) Stop() {
	close(s.shutdownCh)

	// Cancel all active streams
	s.activeStreams.Range(func(_, value interface{}) bool {
		if cancel, ok := value.(context.CancelFunc); ok {
			cancel()
		}
		return true
	})
}

// SyncStream implements the bidirectional sync stream
func (s *Server) SyncStream(stream pb.FederationService_SyncStreamServer) error {
	ctx := stream.Context()

	// Receive handshake
	msg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive handshake: %v", err)
	}

	handshake := msg.GetHandshake()
	if handshake == nil {
		return status.Error(codes.InvalidArgument, "first message must be handshake")
	}

	// Validate federation ID
	if handshake.FederationId != s.config.FederationID {
		return status.Errorf(codes.PermissionDenied,
			"federation ID mismatch: expected %s, got %s",
			s.config.FederationID, handshake.FederationId)
	}

	// Validate protocol version
	if handshake.ProtocolVersion != ProtocolVersion {
		return status.Errorf(codes.FailedPrecondition,
			"protocol version mismatch: expected %d, got %d",
			ProtocolVersion, handshake.ProtocolVersion)
	}

	peerName := handshake.MemberName
	s.logger.Info("Federation peer connected",
		zap.String("peer", peerName),
		zap.String("region", handshake.Region),
		zap.String("zone", handshake.Zone),
	)

	// Update peer state
	s.updatePeerState(peerName, func(state *PeerState) {
		state.Connected = true
		state.Healthy = true
		state.LastSeen = time.Now()
		state.VectorClock = NewVectorClockFromMap(handshake.VectorClock)
	})
	defer s.updatePeerState(peerName, func(state *PeerState) {
		state.Connected = false
	})

	// Send our handshake response
	if err := stream.Send(&pb.SyncMessage{
		Message: &pb.SyncMessage_Handshake{
			Handshake: &pb.SyncHandshake{
				FederationId:    s.config.FederationID,
				MemberName:      s.config.LocalMember.Name,
				Region:          s.config.LocalMember.Region,
				Zone:            s.config.LocalMember.Zone,
				VectorClock:     s.vectorClock.ToMap(),
				ProtocolVersion: ProtocolVersion,
				Compression:     s.config.CompressionEnabled,
			},
		},
	}); err != nil {
		return status.Errorf(codes.Internal, "failed to send handshake: %v", err)
	}

	// Start goroutines for send and receive
	errCh := make(chan error, 2)

	// Receive goroutine
	go func() {
		errCh <- s.handleIncomingMessages(ctx, stream, peerName)
	}()

	// Send goroutine - sends pending changes and heartbeats
	go func() {
		errCh <- s.handleOutgoingMessages(ctx, stream, peerName)
	}()

	// Wait for either to error
	err = <-errCh
	if err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("Sync stream error",
			zap.String("peer", peerName),
			zap.Error(err),
		)
	}

	return err
}

// handleIncomingMessages processes messages from a peer
func (s *Server) handleIncomingMessages(ctx context.Context, stream pb.FederationService_SyncStreamServer, peerName string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		s.updatePeerState(peerName, func(state *PeerState) {
			state.LastSeen = time.Now()
		})

		switch m := msg.Message.(type) {
		case *pb.SyncMessage_Change:
			if err := s.handleResourceChange(ctx, peerName, m.Change); err != nil {
				s.logger.Error("Failed to handle resource change",
					zap.String("peer", peerName),
					zap.Error(err),
				)
			}

		case *pb.SyncMessage_Ack:
			s.handleSyncAck(peerName, m.Ack)

		case *pb.SyncMessage_Heartbeat:
			s.handleHeartbeat(peerName, m.Heartbeat)

		case *pb.SyncMessage_Conflict:
			s.handleConflictNotification(peerName, m.Conflict)
		}
	}
}

// handleOutgoingMessages sends pending changes and heartbeats to a peer
func (s *Server) handleOutgoingMessages(ctx context.Context, stream pb.FederationService_SyncStreamServer, peerName string) error {
	heartbeatTicker := time.NewTicker(HeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-heartbeatTicker.C:
			s.agentMu.RLock()
			agentCount := s.agentCount
			s.agentMu.RUnlock()

			if err := stream.Send(&pb.SyncMessage{
				Message: &pb.SyncMessage_Heartbeat{
					Heartbeat: &pb.Heartbeat{
						VectorClock:    s.vectorClock.ToMap(),
						Timestamp:      time.Now().UnixNano(),
						PendingChanges: safeIntToInt32(len(s.pendingChanges)),
						AgentCount:     agentCount,
					},
				},
			}); err != nil {
				return err
			}

		case change := <-s.pendingChanges:
			// Don't send changes back to their origin
			if change.Resource != nil && change.Resource.OriginMember == peerName {
				continue
			}
			if change.Tombstone != nil && change.Tombstone.OriginMember == peerName {
				continue
			}

			pbChange := s.changeEntryToProto(change)
			if err := stream.Send(&pb.SyncMessage{
				Message: &pb.SyncMessage_Change{
					Change: pbChange,
				},
			}); err != nil {
				return err
			}

			s.statsMu.Lock()
			s.stats.TotalChangesSent++
			s.statsMu.Unlock()
		}
	}
}

// handleResourceChange processes an incoming resource change from a peer
func (s *Server) handleResourceChange(_ context.Context, peerName string, change *pb.ResourceChange) error {
	key := ResourceKey{
		Kind:      change.ResourceType,
		Namespace: change.Namespace,
		Name:      change.Name,
	}
	keyStr := key.String()

	// Update our vector clock
	s.vectorClock.MergeMap(change.VectorClock)

	// Check for conflicts
	if existing, ok := s.resources.Load(keyStr); ok {
		existingRes, ok := existing.(*TrackedResource)
		if !ok {
			return fmt.Errorf("%w: %s", errUnexpectedTypeInResourcesMapForKey, keyStr)
		}
		existingVC := NewVectorClockFromMap(existingRes.VectorClock)
		incomingVC := NewVectorClockFromMap(change.VectorClock)

		if existingVC.Concurrent(incomingVC) {
			// Concurrent changes - potential conflict
			return s.handleConflict(key, existingRes, change, peerName)
		}

		// If incoming is older, ignore it
		if incomingVC.HappenedBefore(existingVC) {
			s.logger.Debug("Ignoring older change",
				zap.String("key", keyStr),
				zap.String("peer", peerName),
			)
			return nil
		}
	}

	// Apply the change
	switch change.ChangeType {
	case pb.ChangeType_CREATED, pb.ChangeType_UPDATED:
		resource := &TrackedResource{
			Key:             key,
			ResourceVersion: change.ResourceVersion,
			Hash:            change.ResourceHash,
			Data:            change.ResourceData,
			VectorClock:     change.VectorClock,
			OriginMember:    change.OriginMember,
			LastModified:    time.Unix(0, change.Timestamp),
			Labels:          change.Labels,
		}
		s.resources.Store(keyStr, resource)

		// Remove any tombstone
		s.tombstones.Delete(keyStr)

		// If the resource is ServiceEndpoints, update the endpoint cache
		if change.ResourceType == "ServiceEndpoints" {
			s.applyServiceEndpoints(change.ResourceData, peerName)
		}

		// Notify callbacks
		s.notifyChange(key, ChangeType(change.ChangeType.String()), change.ResourceData)

	case pb.ChangeType_DELETED:
		s.resources.Delete(keyStr)
		tombstone := &Tombstone{
			Key:          key,
			DeletionTime: time.Unix(0, change.Timestamp),
			VectorClock:  change.VectorClock,
			OriginMember: change.OriginMember,
		}
		s.tombstones.Store(keyStr, tombstone)

		// If the resource is ServiceEndpoints, remove from the endpoint cache
		if change.ResourceType == "ServiceEndpoints" {
			s.deleteServiceEndpoints(change.Namespace, change.Name, peerName)
		}

		// Notify callbacks
		s.notifyChange(key, ChangeTypeDeleted, nil)
	}

	s.statsMu.Lock()
	s.stats.TotalChangesReceived++
	s.statsMu.Unlock()

	s.logger.Debug("Applied change from peer",
		zap.String("key", keyStr),
		zap.String("type", change.ChangeType.String()),
		zap.String("peer", peerName),
	)

	return nil
}

// handleConflict handles a detected conflict between local and remote versions
func (s *Server) handleConflict(key ResourceKey, local *TrackedResource, remote *pb.ResourceChange, peerName string) error {
	keyStr := key.String()

	s.statsMu.Lock()
	s.stats.ConflictsDetected++
	s.statsMu.Unlock()

	s.logger.Warn("Conflict detected",
		zap.String("key", keyStr),
		zap.String("peer", peerName),
		zap.String("strategy", s.config.ConflictResolutionStrategy),
	)

	remoteResource := &TrackedResource{
		Key:             key,
		ResourceVersion: remote.ResourceVersion,
		Hash:            remote.ResourceHash,
		Data:            remote.ResourceData,
		VectorClock:     remote.VectorClock,
		OriginMember:    remote.OriginMember,
		LastModified:    time.Unix(0, remote.Timestamp),
		Labels:          remote.Labels,
	}

	conflict := &ConflictInfo{
		Key:           key,
		LocalVersion:  local,
		RemoteVersion: remoteResource,
		DetectedAt:    time.Now(),
	}

	switch s.config.ConflictResolutionStrategy {
	case StrategyLastWriterWins:
		// Compare timestamps - newer wins
		if remoteResource.LastModified.After(local.LastModified) {
			// Remote wins
			s.resources.Store(keyStr, remoteResource)
			conflict.Resolution = ConflictResolutionRemoteWins
			s.notifyChange(key, ChangeTypeUpdated, remoteResource.Data)
		} else {
			// Local wins - keep existing
			conflict.Resolution = ConflictResolutionLocalWins
		}
		s.statsMu.Lock()
		s.stats.ConflictsResolved++
		s.statsMu.Unlock()

	case "Merge":
		// Attempt to merge - this is resource-type specific
		merged, err := s.mergeResources(local, remoteResource)
		if err != nil {
			conflict.RequiresManual = true
			conflict.Resolution = ConflictResolutionPendingManual
			s.conflicts.Store(keyStr, conflict)
			return err
		}
		s.resources.Store(keyStr, merged)
		conflict.Resolution = ConflictResolutionMerged
		s.statsMu.Lock()
		s.stats.ConflictsResolved++
		s.statsMu.Unlock()
		s.notifyChange(key, ChangeTypeUpdated, merged.Data)

	case "Manual":
		conflict.RequiresManual = true
		conflict.Resolution = ConflictResolutionPendingManual
		s.conflicts.Store(keyStr, conflict)
	}

	return nil
}

// mergeResources attempts to merge two versions of a resource
func (s *Server) mergeResources(local, remote *TrackedResource) (*TrackedResource, error) {
	// For now, we implement a simple JSON merge for maps
	// In practice, this should be resource-type specific

	var localData, remoteData map[string]interface{}
	if err := json.Unmarshal(local.Data, &localData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal local data: %w", err)
	}
	if err := json.Unmarshal(remote.Data, &remoteData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal remote data: %w", err)
	}

	// Simple merge: remote fields override local
	merged := mergeMaps(localData, remoteData)

	mergedData, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged data: %w", err)
	}

	// Create merged resource with combined vector clock
	mergedVC := NewVectorClockFromMap(local.VectorClock)
	mergedVC.MergeMap(remote.VectorClock)
	mergedVC.Increment(s.config.LocalMember.Name)

	return &TrackedResource{
		Key:          local.Key,
		Data:         mergedData,
		VectorClock:  mergedVC.ToMap(),
		OriginMember: s.config.LocalMember.Name,
		LastModified: time.Now(),
		Labels:       mergeLabelMaps(local.Labels, remote.Labels),
	}, nil
}

// mergeMaps recursively merges two maps
func mergeMaps(base, overlay map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		if baseVal, ok := result[k]; ok {
			if baseMap, ok := baseVal.(map[string]interface{}); ok {
				if overlayMap, ok := v.(map[string]interface{}); ok {
					result[k] = mergeMaps(baseMap, overlayMap)
					continue
				}
			}
		}
		result[k] = v
	}
	return result
}

// mergeLabelMaps merges two label maps
func mergeLabelMaps(base, overlay map[string]string) map[string]string {
	if base == nil && overlay == nil {
		return nil
	}
	result := make(map[string]string)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		result[k] = v
	}
	return result
}

// handleSyncAck processes an acknowledgment from a peer
func (s *Server) handleSyncAck(peerName string, ack *pb.SyncAck) {
	s.updatePeerState(peerName, func(state *PeerState) {
		state.VectorClock = NewVectorClockFromMap(ack.VectorClock)
		state.LastSyncTime = time.Now()
	})

	// Log any errors
	for _, err := range ack.Errors {
		s.logger.Warn("Peer reported change error",
			zap.String("peer", peerName),
			zap.String("change_id", err.ChangeId),
			zap.String("error", err.ErrorMessage),
		)
	}
}

// handleHeartbeat processes a heartbeat from a peer
func (s *Server) handleHeartbeat(peerName string, heartbeat *pb.Heartbeat) {
	s.updatePeerState(peerName, func(state *PeerState) {
		state.VectorClock = NewVectorClockFromMap(heartbeat.VectorClock)
		state.LastSeen = time.Now()
		state.Healthy = true
		state.AgentCount = heartbeat.AgentCount
	})
}

// handleConflictNotification processes a conflict notification from a peer
func (s *Server) handleConflictNotification(peerName string, notification *pb.ConflictNotification) {
	key := ResourceKey{
		Kind:      notification.ResourceType,
		Namespace: notification.Namespace,
		Name:      notification.Name,
	}

	s.logger.Info("Received conflict notification from peer",
		zap.String("peer", peerName),
		zap.String("key", key.String()),
		zap.String("resolution", notification.Resolution.String()),
	)
}

// GetState implements the GetState RPC
func (s *Server) GetState(_ context.Context, req *pb.GetStateRequest) (*pb.GetStateResponse, error) {
	if req.FederationId != s.config.FederationID {
		return nil, status.Error(codes.PermissionDenied, "federation ID mismatch")
	}

	// Count resources by type
	resourceCounts := make(map[string]int32)
	s.resources.Range(func(_, value interface{}) bool {
		res, ok := value.(*TrackedResource)
		if !ok {
			return true
		}
		resourceCounts[res.Key.Kind]++
		return true
	})

	// Collect last sync times
	lastSyncTimes := make(map[string]int64)
	s.peerStates.Range(func(key, value interface{}) bool {
		state, ok := value.(*PeerState)
		if !ok {
			return true
		}
		keyStr, ok := key.(string)
		if !ok {
			return true
		}
		if !state.LastSyncTime.IsZero() {
			lastSyncTimes[keyStr] = state.LastSyncTime.UnixNano()
		}
		return true
	})

	// Count pending conflicts
	var conflictCount int32
	s.conflicts.Range(func(_, _ interface{}) bool {
		conflictCount++
		return true
	})

	s.agentMu.RLock()
	agentCount := s.agentCount
	s.agentMu.RUnlock()

	return &pb.GetStateResponse{
		MemberName:       s.config.LocalMember.Name,
		Region:           s.config.LocalMember.Region,
		Zone:             s.config.LocalMember.Zone,
		VectorClock:      s.vectorClock.ToMap(),
		LastSyncTimes:    lastSyncTimes,
		ResourceCounts:   resourceCounts,
		Healthy:          true,
		Phase:            string(s.getPhase()),
		AgentCount:       agentCount,
		ConflictsPending: conflictCount,
	}, nil
}

// Ping implements the Ping RPC
func (s *Server) Ping(_ context.Context, req *pb.PingRequest) (*pb.PingResponse, error) {
	if req.FederationId != s.config.FederationID {
		return nil, status.Error(codes.PermissionDenied, "federation ID mismatch")
	}

	return &pb.PingResponse{
		MemberName: s.config.LocalMember.Name,
		Timestamp:  time.Now().UnixNano(),
		Healthy:    true,
	}, nil
}

// RequestFullSync implements the RequestFullSync RPC
func (s *Server) RequestFullSync(req *pb.FullSyncRequest, stream pb.FederationService_RequestFullSyncServer) error {
	if req.FederationId != s.config.FederationID {
		return status.Error(codes.PermissionDenied, "federation ID mismatch")
	}

	s.logger.Info("Full sync requested",
		zap.String("requester", req.RequesterMember),
		zap.Strings("resourceTypes", req.ResourceTypes),
	)

	// Collect resources to sync
	var resources []*pb.ResourceChange
	requesterVC := NewVectorClockFromMap(req.VectorClock)

	s.resources.Range(func(_, value interface{}) bool {
		res, ok := value.(*TrackedResource)
		if !ok {
			return true
		}

		// Filter by resource type if specified
		if len(req.ResourceTypes) > 0 {
			found := false
			for _, rt := range req.ResourceTypes {
				if res.Key.Kind == rt {
					found = true
					break
				}
			}
			if !found {
				return true
			}
		}

		// Filter by namespace if specified
		if len(req.Namespaces) > 0 {
			found := false
			for _, ns := range req.Namespaces {
				if res.Key.Namespace == ns {
					found = true
					break
				}
			}
			if !found {
				return true
			}
		}

		// Only send if our version is newer
		resVC := NewVectorClockFromMap(res.VectorClock)
		if !resVC.HappenedAfter(requesterVC) && !resVC.Concurrent(requesterVC) {
			return true
		}

		resources = append(resources, &pb.ResourceChange{
			ChangeId:        uuid.New().String(),
			VectorClock:     res.VectorClock,
			ChangeType:      pb.ChangeType_UPDATED,
			ResourceType:    res.Key.Kind,
			Namespace:       res.Key.Namespace,
			Name:            res.Key.Name,
			ResourceVersion: res.ResourceVersion,
			ResourceData:    res.Data,
			ResourceHash:    res.Hash,
			Timestamp:       res.LastModified.UnixNano(),
			OriginMember:    res.OriginMember,
			Labels:          res.Labels,
		})
		return true
	})

	// Send in batches
	batchSize := int(s.config.BatchSize)
	totalResources := len(resources)
	batchNum := 0

	for i := 0; i < len(resources); i += batchSize {
		end := i + batchSize
		if end > len(resources) {
			end = len(resources)
		}

		batch := &pb.ResourceBatch{
			BatchNumber:    safeIntToInt32(batchNum),
			IsLast:         end == len(resources),
			Resources:      resources[i:end],
			TotalResources: safeIntToInt32(totalResources),
			VectorClock:    s.vectorClock.ToMap(),
		}

		if err := stream.Send(batch); err != nil {
			return err
		}
		batchNum++
	}

	// If no resources, send empty final batch
	if len(resources) == 0 {
		if err := stream.Send(&pb.ResourceBatch{
			BatchNumber:    0,
			IsLast:         true,
			Resources:      nil,
			TotalResources: 0,
			VectorClock:    s.vectorClock.ToMap(),
		}); err != nil {
			return err
		}
	}

	s.statsMu.Lock()
	s.stats.FullSyncs++
	s.statsMu.Unlock()

	return nil
}

// RegisterServer registers the federation service with a gRPC server
func (s *Server) RegisterServer(grpcServer *grpc.Server) {
	pb.RegisterFederationServiceServer(grpcServer, s)
}

// RecordLocalChange records a local change to be propagated to peers
func (s *Server) RecordLocalChange(key ResourceKey, changeType ChangeType, data []byte, labels map[string]string) {
	s.vectorClock.Increment(s.config.LocalMember.Name)
	clock := s.vectorClock.ToMap()

	entry := &ChangeEntry{
		ID:           uuid.New().String(),
		Key:          key,
		Type:         changeType,
		VectorClock:  clock,
		Timestamp:    time.Now(),
		Acknowledged: make(map[string]bool),
	}

	keyStr := key.String()

	if changeType == ChangeTypeDeleted {
		entry.Tombstone = &Tombstone{
			Key:          key,
			DeletionTime: time.Now(),
			VectorClock:  clock,
			OriginMember: s.config.LocalMember.Name,
		}
		s.tombstones.Store(keyStr, entry.Tombstone)
		s.resources.Delete(keyStr)
	} else {
		entry.Resource = &TrackedResource{
			Key:          key,
			Data:         data,
			VectorClock:  clock,
			OriginMember: s.config.LocalMember.Name,
			LastModified: time.Now(),
			Labels:       labels,
		}
		s.resources.Store(keyStr, entry.Resource)
		s.tombstones.Delete(keyStr)
	}

	// Queue for propagation
	select {
	case s.pendingChanges <- entry:
	default:
		s.logger.Warn("Pending changes queue full, dropping oldest")
	}
}

// OnChange registers a callback to be called when resources change
func (s *Server) OnChange(callback func(key ResourceKey, change ChangeType, data []byte)) {
	s.callbackMu.Lock()
	defer s.callbackMu.Unlock()
	s.changeCallbacks = append(s.changeCallbacks, callback)
}

// notifyChange notifies all registered callbacks of a change
func (s *Server) notifyChange(key ResourceKey, change ChangeType, data []byte) {
	s.callbackMu.RLock()
	defer s.callbackMu.RUnlock()
	for _, cb := range s.changeCallbacks {
		go cb(key, change, data)
	}
}

// SetAgentCount updates the connected agent count
func (s *Server) SetAgentCount(count int32) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	s.agentCount = count
}

// GetStats returns sync statistics
func (s *Server) GetStats() SyncStats {
	s.statsMu.RLock()
	defer s.statsMu.RUnlock()
	return *s.stats
}

// GetPeerStates returns the state of all peers
func (s *Server) GetPeerStates() map[string]*PeerState {
	result := make(map[string]*PeerState)
	s.peerStates.Range(func(key, value interface{}) bool {
		keyStr, ok := key.(string)
		if !ok {
			return true
		}
		state, ok := value.(*PeerState)
		if !ok {
			return true
		}
		result[keyStr] = state
		return true
	})
	return result
}

// GetConflicts returns all pending conflicts
func (s *Server) GetConflicts() []*ConflictInfo {
	var conflicts []*ConflictInfo
	s.conflicts.Range(func(_, value interface{}) bool {
		conflict, ok := value.(*ConflictInfo)
		if !ok {
			return true
		}
		conflicts = append(conflicts, conflict)
		return true
	})
	return conflicts
}

// ResolveConflict manually resolves a conflict
func (s *Server) ResolveConflict(keyStr string, useLocal bool) error {
	val, ok := s.conflicts.Load(keyStr)
	if !ok {
		return fmt.Errorf("%w: %s", errConflictNotFound, keyStr)
	}

	conflict, ok := val.(*ConflictInfo)
	if !ok {
		return fmt.Errorf("%w: %s", errUnexpectedTypeForConflict, keyStr)
	}
	key := conflict.Key

	if useLocal {
		// Keep local version - it's already there
		conflict.Resolution = ConflictResolutionLocalWins
	} else {
		// Use remote version
		s.resources.Store(keyStr, conflict.RemoteVersion)
		conflict.Resolution = ConflictResolutionRemoteWins
		s.notifyChange(key, ChangeTypeUpdated, conflict.RemoteVersion.Data)
	}

	s.conflicts.Delete(keyStr)

	s.statsMu.Lock()
	s.stats.ConflictsResolved++
	s.statsMu.Unlock()

	return nil
}

// updatePeerState updates the state of a peer
func (s *Server) updatePeerState(peerName string, updateFn func(*PeerState)) {
	val, _ := s.peerStates.LoadOrStore(peerName, &PeerState{})
	state, ok := val.(*PeerState)
	if !ok {
		return
	}
	updateFn(state)
}

// getPhase returns the current federation phase
func (s *Server) getPhase() Phase {
	healthyCount := 0
	connectedCount := 0
	totalPeers := len(s.config.Peers)

	s.peerStates.Range(func(_, value interface{}) bool {
		state, ok := value.(*PeerState)
		if !ok {
			return true
		}
		if state.Connected {
			connectedCount++
		}
		if state.Healthy {
			healthyCount++
		}
		return true
	})

	if totalPeers == 0 {
		return PhaseHealthy
	}

	if connectedCount == 0 {
		return PhasePartitioned
	}

	if healthyCount == totalPeers {
		return PhaseHealthy
	}

	return PhaseDegraded
}

// cleanupTombstones removes old tombstones
func (s *Server) cleanupTombstones(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownCh:
			return
		case <-ticker.C:
			now := time.Now()
			s.tombstones.Range(func(key, value interface{}) bool {
				tombstone, ok := value.(*Tombstone)
				if !ok {
					return true
				}
				if now.Sub(tombstone.DeletionTime) > s.config.TombstoneTTL {
					s.tombstones.Delete(key)
				}
				return true
			})
		}
	}
}

// changeEntryToProto converts a ChangeEntry to protobuf
func (s *Server) changeEntryToProto(entry *ChangeEntry) *pb.ResourceChange {
	change := &pb.ResourceChange{
		ChangeId:     entry.ID,
		VectorClock:  entry.VectorClock,
		ResourceType: entry.Key.Kind,
		Namespace:    entry.Key.Namespace,
		Name:         entry.Key.Name,
		Timestamp:    entry.Timestamp.UnixNano(),
	}

	switch entry.Type {
	case ChangeTypeCreated:
		change.ChangeType = pb.ChangeType_CREATED
	case ChangeTypeUpdated:
		change.ChangeType = pb.ChangeType_UPDATED
	case ChangeTypeDeleted:
		change.ChangeType = pb.ChangeType_DELETED
	}

	if entry.Resource != nil {
		change.ResourceData = entry.Resource.Data
		change.ResourceHash = entry.Resource.Hash
		change.ResourceVersion = entry.Resource.ResourceVersion
		change.OriginMember = entry.Resource.OriginMember
		change.Labels = entry.Resource.Labels
	}

	if entry.Tombstone != nil {
		change.OriginMember = entry.Tombstone.OriginMember
	}

	return change
}

// applyServiceEndpoints deserializes a ServiceEndpoints proto message from the
// resource data and stores it in the endpoint cache under the origin peer's
// cluster name.
func (s *Server) applyServiceEndpoints(data []byte, peerName string) {
	var svcEndpoints pb.ServiceEndpoints
	if err := proto.Unmarshal(data, &svcEndpoints); err != nil {
		s.logger.Error("Failed to unmarshal ServiceEndpoints",
			zap.String("peer", peerName),
			zap.Error(err),
		)
		return
	}

	// Use the cluster_name from the message if set, otherwise fall back to
	// the peer name that sent it.
	cluster := svcEndpoints.ClusterName
	if cluster == "" {
		cluster = peerName
	}

	s.endpointCache.Update(cluster, &svcEndpoints)

	s.logger.Debug("Updated remote endpoint cache",
		zap.String("cluster", cluster),
		zap.String("service", fmt.Sprintf("%s/%s", svcEndpoints.Namespace, svcEndpoints.ServiceName)),
		zap.Int("endpoints", len(svcEndpoints.Endpoints)),
	)
}

// deleteServiceEndpoints removes a service's endpoints from the remote cache.
// The namespace and name correspond to the ResourceChange fields.
func (s *Server) deleteServiceEndpoints(namespace, name, peerName string) {
	// The change name for ServiceEndpoints is the service name.
	s.endpointCache.Delete(peerName, namespace, name)

	s.logger.Debug("Deleted remote endpoint from cache",
		zap.String("peer", peerName),
		zap.String("service", fmt.Sprintf("%s/%s", namespace, name)),
	)
}

// GetEndpointCache returns the remote endpoint cache for direct access by the
// Manager or other components.
func (s *Server) GetEndpointCache() *RemoteEndpointCache {
	return s.endpointCache
}

// safeIntToInt32 safely converts an int to int32, clamping to max int32 value if needed
func safeIntToInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v) //nolint:gosec // bounds checked above
}
