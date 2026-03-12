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
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/azrtydxb/novaedge/internal/pkg/convert"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

var (
	errUnexpectedTypeInResourcesMapForKey = errors.New("unexpected type in resources map for key")
	errConflictNotFound                   = errors.New("conflict not found")
	errUnexpectedTypeForConflict          = errors.New("unexpected type for conflict")
	errNoPeerOutbox                       = errors.New("no outbox channel for peer")
)

const (
	// ProtocolVersion is the current federation protocol version
	ProtocolVersion = 1

	// HeartbeatInterval is how often to send heartbeats on sync streams
	HeartbeatInterval = 10 * time.Second

	// StreamTimeout is the timeout for stream operations
	StreamTimeout = 30 * time.Second

	// peerMessageRate is the steady-state token rate (messages/sec) for per-peer rate limiting.
	peerMessageRate rate.Limit = 100

	// peerMessageBurst is the burst capacity for per-peer rate limiting.
	peerMessageBurst = 200

	// fullSyncCooldown is the minimum interval between RequestFullSync calls from the same peer.
	fullSyncCooldown = 30 * time.Second
)

// Server implements the FederationService gRPC server
type Server struct {
	pb.UnimplementedFederationServiceServer

	// config holds the federation configuration, accessed atomically to
	// avoid data races between gRPC handlers reading it and UpdateConfig
	// writing it.
	config atomic.Pointer[Config]

	// vectorClock is this controller's vector clock
	vectorClock *VectorClock

	// peerStates tracks the state of each peer
	peerStates sync.Map // map[string]*PeerState

	// resources tracks all synced resources
	resources sync.Map // map[string]*TrackedResource (keyed by ResourceKey.String())

	// tombstones tracks deleted resources
	tombstones sync.Map // map[string]*Tombstone

	// pendingChanges is the queue of changes to propagate (kept for backward compatibility)
	pendingChanges chan *ChangeEntry

	// peerOutboxes holds per-peer buffered channels so that each connected
	// peer receives every change independently (fan-out).
	peerOutboxes   map[string]chan *ChangeEntry
	peerOutboxesMu sync.Mutex

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

	// peerLimiters holds a per-peer rate.Limiter to cap incoming message rates.
	peerLimiters sync.Map // map[string]*rate.Limiter

	// fullSyncCooldowns tracks the last time each peer called RequestFullSync.
	fullSyncCooldowns sync.Map // map[string]time.Time
	// fullSyncMu serializes the load-check-store in checkFullSyncCooldown to
	// prevent two concurrent callers from both passing the cooldown check
	// before either records the new timestamp.
	fullSyncMu sync.Mutex

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
		vectorClock:    NewVectorClock(),
		pendingChanges: make(chan *ChangeEntry, 10000),
		peerOutboxes:   make(map[string]chan *ChangeEntry),
		endpointCache:  NewRemoteEndpointCache(),
		stats:          &SyncStats{},
		logger:         logger.Named("federation"),
		shutdownCh:     make(chan struct{}),
	}
	s.config.Store(config)

	// Initialize vector clock with our member
	if config.LocalMember != nil {
		s.vectorClock.Increment(config.LocalMember.Name)
	}

	return s
}

// Start begins background processing for the federation server
func (s *Server) Start(ctx context.Context) error {
	s.logger.Info("Starting federation server",
		zap.String("federation_id", s.config.Load().FederationID),
		zap.String("local_member", s.config.Load().LocalMember.Name),
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
	s.activeStreams.Range(func(_, value any) bool {
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
	if handshake.FederationId != s.config.Load().FederationID {
		return status.Errorf(codes.PermissionDenied,
			"federation ID mismatch: expected %s, got %s",
			s.config.Load().FederationID, handshake.FederationId)
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
				FederationId:    s.config.Load().FederationID,
				MemberName:      s.config.Load().LocalMember.Name,
				Region:          s.config.Load().LocalMember.Region,
				Zone:            s.config.Load().LocalMember.Zone,
				VectorClock:     s.vectorClock.ToMap(),
				ProtocolVersion: ProtocolVersion,
				Compression:     s.config.Load().CompressionEnabled,
			},
		},
	}); err != nil {
		return status.Errorf(codes.Internal, "failed to send handshake: %v", err)
	}

	// Create a per-peer outbox channel so every connected peer gets every
	// change independently (fan-out instead of competing consumers).
	peerCh := make(chan *ChangeEntry, 1000)
	s.peerOutboxesMu.Lock()
	s.peerOutboxes[peerName] = peerCh
	s.peerOutboxesMu.Unlock()
	defer func() {
		s.peerOutboxesMu.Lock()
		delete(s.peerOutboxes, peerName)
		close(peerCh)
		s.peerOutboxesMu.Unlock()
		// Drain remaining entries so they can be garbage-collected.
		for entry := range peerCh {
			_ = entry
		}
	}()

	// Start goroutines for send and receive with a derived context so
	// that when the first goroutine exits we can signal the other to stop,
	// preventing a goroutine leak (fixes #932).
	streamCtx, streamCancel := context.WithCancel(ctx)
	s.activeStreams.Store(peerName, streamCancel)
	defer s.activeStreams.Delete(peerName)
	defer streamCancel()

	errCh := make(chan error, 2)

	// Receive goroutine
	go func() {
		errCh <- s.handleIncomingMessages(streamCtx, stream, peerName)
	}()

	// Send goroutine - sends pending changes and heartbeats
	go func() {
		errCh <- s.handleOutgoingMessages(streamCtx, stream, peerName)
	}()

	// Wait for the first goroutine to finish, then cancel the other.
	err = <-errCh
	streamCancel()

	// Drain the second error so the goroutine is not leaked.
	err2 := <-errCh

	// Report the first non-nil, non-context-canceled error.
	if err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("Sync stream error",
			zap.String("peer", peerName),
			zap.Error(err),
		)
		return err
	}
	if err2 != nil && !errors.Is(err2, context.Canceled) {
		s.logger.Error("Sync stream error",
			zap.String("peer", peerName),
			zap.Error(err2),
		)
		return err2
	}

	return err
}

// peerLimiterFor returns the rate.Limiter for the given peer, creating one if it does not yet exist.
func (s *Server) peerLimiterFor(peerName string) *rate.Limiter {
	val, _ := s.peerLimiters.LoadOrStore(peerName, rate.NewLimiter(peerMessageRate, peerMessageBurst))
	lim, _ := val.(*rate.Limiter)
	return lim
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

		// Enforce per-peer rate limit to prevent message floods.
		if err := s.peerLimiterFor(peerName).Wait(ctx); err != nil {
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

	// Look up the per-peer outbox created in SyncStream.
	s.peerOutboxesMu.Lock()
	peerCh, ok := s.peerOutboxes[peerName]
	s.peerOutboxesMu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", errNoPeerOutbox, peerName)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-heartbeatTicker.C:
			s.agentMu.RLock()
			agentCount := s.agentCount
			s.agentMu.RUnlock()

			// Report the number of entries queued for this peer.
			if err := stream.Send(&pb.SyncMessage{
				Message: &pb.SyncMessage_Heartbeat{
					Heartbeat: &pb.Heartbeat{
						VectorClock:    s.vectorClock.ToMap(),
						Timestamp:      time.Now().UnixNano(),
						PendingChanges: convert.SafeIntToInt32(len(peerCh)),
						AgentCount:     agentCount,
					},
				},
			}); err != nil {
				return err
			}

		case change, chanOpen := <-peerCh:
			if !chanOpen {
				return nil
			}
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
	case pb.ChangeType_CHANGE_TYPE_UNSPECIFIED:
		// Ignore unspecified change types
		return nil
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

		// Notify callbacks using internal constants to avoid proto-string/internal-string mismatch.
		var localChangeType ChangeType
		if change.ChangeType == pb.ChangeType_CREATED {
			localChangeType = ChangeTypeCreated
		} else {
			localChangeType = ChangeTypeUpdated
		}
		s.notifyChange(key, localChangeType, change.ResourceData)

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
		zap.String("strategy", s.config.Load().ConflictResolutionStrategy),
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

	switch s.config.Load().ConflictResolutionStrategy {
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

	var localData, remoteData map[string]any
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
	mergedVC.Increment(s.config.Load().LocalMember.Name)

	return &TrackedResource{
		Key:          local.Key,
		Data:         mergedData,
		VectorClock:  mergedVC.ToMap(),
		OriginMember: s.config.Load().LocalMember.Name,
		LastModified: time.Now(),
		Labels:       mergeLabelMaps(local.Labels, remote.Labels),
	}, nil
}

// mergeMaps recursively merges two maps
func mergeMaps(base, overlay map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		if baseVal, ok := result[k]; ok {
			if baseMap, ok := baseVal.(map[string]any); ok {
				if overlayMap, ok := v.(map[string]any); ok {
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
	if req.FederationId != s.config.Load().FederationID {
		return nil, status.Error(codes.PermissionDenied, "federation ID mismatch")
	}

	// Count resources by type
	resourceCounts := make(map[string]int32)
	s.resources.Range(func(_, value any) bool {
		res, ok := value.(*TrackedResource)
		if !ok {
			return true
		}
		resourceCounts[res.Key.Kind]++
		return true
	})

	// Collect last sync times
	lastSyncTimes := make(map[string]int64)
	s.peerStates.Range(func(key, value any) bool {
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
	s.conflicts.Range(func(_, _ any) bool {
		conflictCount++
		return true
	})

	s.agentMu.RLock()
	agentCount := s.agentCount
	s.agentMu.RUnlock()

	return &pb.GetStateResponse{
		MemberName:       s.config.Load().LocalMember.Name,
		Region:           s.config.Load().LocalMember.Region,
		Zone:             s.config.Load().LocalMember.Zone,
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
	if req.FederationId != s.config.Load().FederationID {
		return nil, status.Error(codes.PermissionDenied, "federation ID mismatch")
	}

	return &pb.PingResponse{
		MemberName: s.config.Load().LocalMember.Name,
		Timestamp:  time.Now().UnixNano(),
		Healthy:    true,
	}, nil
}

// RequestFullSync implements the RequestFullSync RPC
func (s *Server) RequestFullSync(req *pb.FullSyncRequest, stream pb.FederationService_RequestFullSyncServer) error {
	if req.FederationId != s.config.Load().FederationID {
		return status.Error(codes.PermissionDenied, "federation ID mismatch")
	}

	// TODO: validate req.RequesterMember against the authenticated peer identity
	// extracted from the TLS client certificate to prevent a peer from
	// impersonating another peer's cooldown slot.
	if err := s.checkFullSyncCooldown(req.RequesterMember); err != nil {
		return err
	}

	s.logger.Info("Full sync requested",
		zap.String("requester", req.RequesterMember),
		zap.Strings("resourceTypes", req.ResourceTypes),
	)

	resources := s.collectFullSyncResources(req)

	if err := s.sendFullSyncBatches(stream, resources); err != nil {
		return err
	}

	s.statsMu.Lock()
	s.stats.FullSyncs++
	s.statsMu.Unlock()

	return nil
}

// checkFullSyncCooldown enforces a minimum interval between RequestFullSync calls from the same peer.
// fullSyncMu serializes the load-check-store sequence to prevent a race where two concurrent
// callers both pass the cooldown check before either records the new timestamp.
func (s *Server) checkFullSyncCooldown(peerName string) error {
	s.fullSyncMu.Lock()
	defer s.fullSyncMu.Unlock()

	now := time.Now()
	if last, ok := s.fullSyncCooldowns.Load(peerName); ok {
		if lastTime, ok := last.(time.Time); ok && now.Sub(lastTime) < fullSyncCooldown {
			return status.Errorf(codes.ResourceExhausted,
				"full sync requested too soon; retry after %s",
				(fullSyncCooldown - now.Sub(lastTime)).Truncate(time.Second),
			)
		}
	}
	s.fullSyncCooldowns.Store(peerName, now)
	return nil
}

// collectFullSyncResources gathers the resources that should be sent in a full sync response.
func (s *Server) collectFullSyncResources(req *pb.FullSyncRequest) []*pb.ResourceChange {
	var resources []*pb.ResourceChange
	requesterVC := NewVectorClockFromMap(req.VectorClock)

	s.resources.Range(func(_, value any) bool {
		res, ok := value.(*TrackedResource)
		if !ok {
			return true
		}
		if !s.resourceMatchesFilter(res, req.ResourceTypes, req.Namespaces) {
			return true
		}
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
	return resources
}

// resourceMatchesFilter returns true when res passes the resource-type and namespace filters from req.
func (s *Server) resourceMatchesFilter(res *TrackedResource, resourceTypes, namespaces []string) bool {
	if len(resourceTypes) > 0 {
		found := false
		for _, rt := range resourceTypes {
			if res.Key.Kind == rt {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(namespaces) > 0 {
		found := false
		for _, ns := range namespaces {
			if res.Key.Namespace == ns {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// sendFullSyncBatches sends collected resources to the stream in batch messages.
func (s *Server) sendFullSyncBatches(stream pb.FederationService_RequestFullSyncServer, resources []*pb.ResourceChange) error {
	if len(resources) == 0 {
		return stream.Send(&pb.ResourceBatch{
			BatchNumber:    0,
			IsLast:         true,
			TotalResources: 0,
			VectorClock:    s.vectorClock.ToMap(),
		})
	}

	batchSize := int(s.config.Load().BatchSize)
	if batchSize == 0 {
		// BatchSize=0 would cause an infinite loop; treat it as "one batch".
		batchSize = len(resources)
	}
	totalResources := len(resources)
	for i, batchNum := 0, 0; i < len(resources); i, batchNum = i+batchSize, batchNum+1 {
		end := i + batchSize
		if end > len(resources) {
			end = len(resources)
		}
		batch := &pb.ResourceBatch{
			BatchNumber:    convert.SafeIntToInt32(batchNum),
			IsLast:         end == len(resources),
			Resources:      resources[i:end],
			TotalResources: convert.SafeIntToInt32(totalResources),
			VectorClock:    s.vectorClock.ToMap(),
		}
		if err := stream.Send(batch); err != nil {
			return err
		}
	}
	return nil
}

// RegisterServer registers the federation service with a gRPC server
func (s *Server) RegisterServer(grpcServer *grpc.Server) {
	pb.RegisterFederationServiceServer(grpcServer, s)
}

// RecordLocalChange records a local change to be propagated to peers
func (s *Server) RecordLocalChange(key ResourceKey, changeType ChangeType, data []byte, labels map[string]string) {
	s.vectorClock.Increment(s.config.Load().LocalMember.Name)
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
			OriginMember: s.config.Load().LocalMember.Name,
		}
		s.tombstones.Store(keyStr, entry.Tombstone)
		s.resources.Delete(keyStr)
	} else {
		entry.Resource = &TrackedResource{
			Key:          key,
			Data:         data,
			VectorClock:  clock,
			OriginMember: s.config.Load().LocalMember.Name,
			LastModified: time.Now(),
			Labels:       labels,
		}
		s.resources.Store(keyStr, entry.Resource)
		s.tombstones.Delete(keyStr)
	}

	// Fan out to all connected peer outboxes.
	s.peerOutboxesMu.Lock()
	for peer, ch := range s.peerOutboxes {
		select {
		case ch <- entry:
		default:
			s.logger.Warn("Peer outbox full, dropping newest change",
				zap.String("peer", peer),
			)
		}
	}
	s.peerOutboxesMu.Unlock()
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

// GetPeerStates returns a snapshot copy of the state of all peers.
// Each returned *PeerState is a shallow copy taken under that entry's lock.
func (s *Server) GetPeerStates() map[string]*PeerState {
	result := make(map[string]*PeerState)
	s.peerStates.Range(func(key, value any) bool {
		keyStr, ok := key.(string)
		if !ok {
			return true
		}
		state, ok := value.(*PeerState)
		if !ok {
			return true
		}
		state.mu.Lock()
		stateCopy := PeerState{
			Info:                state.Info,
			VectorClock:         state.VectorClock,
			LastSeen:            state.LastSeen,
			LastSyncTime:        state.LastSyncTime,
			Healthy:             state.Healthy,
			Connected:           state.Connected,
			AgentCount:          state.AgentCount,
			SyncLag:             state.SyncLag,
			LastError:           state.LastError,
			ConsecutiveFailures: state.ConsecutiveFailures,
		}
		state.mu.Unlock()
		result[keyStr] = &stateCopy
		return true
	})
	return result
}

// GetConflicts returns all pending conflicts
func (s *Server) GetConflicts() []*ConflictInfo {
	var conflicts []*ConflictInfo
	s.conflicts.Range(func(_, value any) bool {
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
	state.mu.Lock()
	updateFn(state)
	state.mu.Unlock()
}

// getPhase returns the current federation phase
func (s *Server) getPhase() Phase {
	healthyCount := 0
	connectedCount := 0
	totalPeers := len(s.config.Load().Peers)

	s.peerStates.Range(func(_, value any) bool {
		state, ok := value.(*PeerState)
		if !ok {
			return true
		}
		state.mu.Lock()
		connected := state.Connected
		healthy := state.Healthy
		state.mu.Unlock()
		if connected {
			connectedCount++
		}
		if healthy {
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
			s.tombstones.Range(func(key, value any) bool {
				tombstone, ok := value.(*Tombstone)
				if !ok {
					return true
				}
				if now.Sub(tombstone.DeletionTime) > s.config.Load().TombstoneTTL {
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
