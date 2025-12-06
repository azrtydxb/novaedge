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
	"time"
)

// PeerInfo contains information about a federation peer
type PeerInfo struct {
	// Name is the unique name of this peer
	Name string

	// Endpoint is the gRPC endpoint (host:port)
	Endpoint string

	// Region is the geographic region
	Region string

	// Zone is the availability zone
	Zone string

	// Priority for sync operations (lower = higher priority)
	Priority int32

	// Labels are additional metadata
	Labels map[string]string

	// TLS configuration
	TLSEnabled         bool
	TLSServerName      string
	InsecureSkipVerify bool
}

// PeerState represents the current state of a federation peer
type PeerState struct {
	// Info contains static peer configuration
	Info *PeerInfo

	// VectorClock is the peer's last known vector clock
	VectorClock *VectorClock

	// LastSeen is when we last successfully communicated
	LastSeen time.Time

	// LastSyncTime is when we last synced successfully
	LastSyncTime time.Time

	// Healthy indicates if the peer is reachable
	Healthy bool

	// Connected indicates if we have an active stream
	Connected bool

	// AgentCount is how many agents are connected to this peer
	AgentCount int32

	// SyncLag is the estimated sync lag with this peer
	SyncLag time.Duration

	// LastError is the most recent error message
	LastError string

	// ConsecutiveFailures counts consecutive health check failures
	ConsecutiveFailures int32
}

// ResourceKey uniquely identifies a Kubernetes resource
type ResourceKey struct {
	Kind      string
	Namespace string
	Name      string
}

// String returns the string representation of a resource key
func (k ResourceKey) String() string {
	if k.Namespace == "" {
		return k.Kind + "/" + k.Name
	}
	return k.Kind + "/" + k.Namespace + "/" + k.Name
}

// TrackedResource represents a resource being synced across the federation
type TrackedResource struct {
	Key ResourceKey

	// ResourceVersion is the Kubernetes resource version
	ResourceVersion string

	// Hash is a hash of the resource content for change detection
	Hash string

	// Data is the serialized resource data
	Data []byte

	// VectorClock is the clock when this version was created
	VectorClock map[string]int64

	// OriginMember is which member originated this version
	OriginMember string

	// LastModified is when this resource was last modified
	LastModified time.Time

	// Labels are resource labels for filtering
	Labels map[string]string
}

// Tombstone represents a deleted resource
type Tombstone struct {
	Key ResourceKey

	// DeletionTime is when the resource was deleted
	DeletionTime time.Time

	// VectorClock at deletion
	VectorClock map[string]int64

	// OriginMember is which member deleted the resource
	OriginMember string
}

// ConflictInfo represents a detected conflict
type ConflictInfo struct {
	Key ResourceKey

	// LocalVersion is our version of the resource
	LocalVersion *TrackedResource

	// RemoteVersion is the conflicting remote version
	RemoteVersion *TrackedResource

	// DetectedAt is when the conflict was detected
	DetectedAt time.Time

	// Resolution is how the conflict was resolved (if automatic)
	Resolution ConflictResolutionType

	// RequiresManual indicates if manual intervention is needed
	RequiresManual bool
}

// ConflictResolutionType defines how a conflict was resolved
type ConflictResolutionType string

const (
	// ConflictResolutionNone - conflict not yet resolved
	ConflictResolutionNone ConflictResolutionType = ""

	// ConflictResolutionLastWriterWins - newer timestamp wins
	ConflictResolutionLastWriterWins ConflictResolutionType = "LastWriterWins"

	// ConflictResolutionMerged - resources were merged
	ConflictResolutionMerged ConflictResolutionType = "Merged"

	// ConflictResolutionPendingManual - requires manual resolution
	ConflictResolutionPendingManual ConflictResolutionType = "PendingManual"

	// ConflictResolutionLocalWins - local version was kept
	ConflictResolutionLocalWins ConflictResolutionType = "LocalWins"

	// ConflictResolutionRemoteWins - remote version was applied
	ConflictResolutionRemoteWins ConflictResolutionType = "RemoteWins"
)

// SyncStats contains statistics about federation sync
type SyncStats struct {
	// TotalChangesReceived is the total number of changes received
	TotalChangesReceived int64

	// TotalChangesSent is the total number of changes sent
	TotalChangesSent int64

	// LastSyncDuration is how long the last sync took
	LastSyncDuration time.Duration

	// ConflictsDetected is the total number of conflicts detected
	ConflictsDetected int64

	// ConflictsResolved is the total number of conflicts resolved
	ConflictsResolved int64

	// PendingChanges is the number of changes waiting to be synced
	PendingChanges int64

	// FullSyncs is the number of full syncs performed
	FullSyncs int64

	// IncrementalSyncs is the number of incremental syncs
	IncrementalSyncs int64
}

// FederationPhase represents the current phase of the federation
type FederationPhase string

const (
	// PhaseInitializing - federation is starting up
	PhaseInitializing FederationPhase = "Initializing"

	// PhaseSyncing - initial sync is in progress
	PhaseSyncing FederationPhase = "Syncing"

	// PhaseHealthy - all members are healthy and in sync
	PhaseHealthy FederationPhase = "Healthy"

	// PhaseDegraded - some members are unhealthy or out of sync
	PhaseDegraded FederationPhase = "Degraded"

	// PhasePartitioned - local member is partitioned from peers
	PhasePartitioned FederationPhase = "Partitioned"
)

// ChangeEntry represents a change to be propagated
type ChangeEntry struct {
	// ID is a unique identifier for this change
	ID string

	// Key is the resource key
	Key ResourceKey

	// Type is the type of change
	Type ChangeType

	// Resource is the resource data (nil for deletes)
	Resource *TrackedResource

	// Tombstone is set for deletes
	Tombstone *Tombstone

	// VectorClock after this change
	VectorClock map[string]int64

	// Timestamp is when the change was created
	Timestamp time.Time

	// Acknowledged tracks which peers have acknowledged
	Acknowledged map[string]bool
}

// ChangeType represents the type of resource change
type ChangeType string

const (
	// ChangeTypeCreated - resource was created
	ChangeTypeCreated ChangeType = "Created"

	// ChangeTypeUpdated - resource was updated
	ChangeTypeUpdated ChangeType = "Updated"

	// ChangeTypeDeleted - resource was deleted
	ChangeTypeDeleted ChangeType = "Deleted"
)

// FederationConfig holds the runtime configuration for federation
type FederationConfig struct {
	// FederationID is the unique identifier for this federation
	FederationID string

	// LocalMember is this controller's identity
	LocalMember *PeerInfo

	// Peers are the other members of the federation
	Peers []*PeerInfo

	// SyncInterval is how often to sync with peers
	SyncInterval time.Duration

	// SyncTimeout is the timeout for sync operations
	SyncTimeout time.Duration

	// BatchSize is the maximum number of resources per sync batch
	BatchSize int32

	// CompressionEnabled enables compression for sync traffic
	CompressionEnabled bool

	// ResourceTypes to sync (empty = all)
	ResourceTypes []string

	// ExcludeNamespaces to exclude from sync
	ExcludeNamespaces []string

	// ConflictResolutionStrategy is the default conflict resolution
	ConflictResolutionStrategy string

	// VectorClocksEnabled enables vector clocks for ordering
	VectorClocksEnabled bool

	// TombstoneTTL is how long to keep deletion records
	TombstoneTTL time.Duration

	// HealthCheckInterval is how often to check peer health
	HealthCheckInterval time.Duration

	// HealthCheckTimeout is the timeout for health checks
	HealthCheckTimeout time.Duration

	// FailureThreshold is failures before marking unhealthy
	FailureThreshold int32

	// SuccessThreshold is successes before marking healthy
	SuccessThreshold int32
}

// DefaultFederationConfig returns a FederationConfig with sensible defaults
func DefaultFederationConfig() *FederationConfig {
	return &FederationConfig{
		SyncInterval:               5 * time.Second,
		SyncTimeout:                30 * time.Second,
		BatchSize:                  100,
		CompressionEnabled:         true,
		ConflictResolutionStrategy: "LastWriterWins",
		VectorClocksEnabled:        true,
		TombstoneTTL:               24 * time.Hour,
		HealthCheckInterval:        10 * time.Second,
		HealthCheckTimeout:         5 * time.Second,
		FailureThreshold:           3,
		SuccessThreshold:           1,
	}
}
