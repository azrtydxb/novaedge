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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FederationMode defines the federation operating mode
// +kubebuilder:validation:Enum=hub-spoke;mesh;unified
type FederationMode string

const (
	// FederationModeHubSpoke enables one-way config push from hub to spoke clusters
	FederationModeHubSpoke FederationMode = "hub-spoke"

	// FederationModeMesh enables bidirectional sync with cross-cluster endpoint merging
	FederationModeMesh FederationMode = "mesh"

	// FederationModeUnified enables shared service namespace with location-aware routing
	FederationModeUnified FederationMode = "unified"
)

// NovaEdgeFederationSpec defines the desired state for federation configuration.
// It configures active/active federation between multiple controllers.
type NovaEdgeFederationSpec struct {
	// Mode configures the federation operating mode.
	// hub-spoke: one-way config push from hub to spoke clusters.
	// mesh: bidirectional sync with cross-cluster endpoint merging.
	// unified: shared service namespace with location-aware routing.
	// +kubebuilder:default=mesh
	// +optional
	Mode FederationMode `json:"mode,omitempty"`
	// FederationID is a unique identifier for this federation
	// All members must use the same federation ID
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	FederationID string `json:"federationID"`

	// LocalMember defines this controller's identity in the federation
	// +kubebuilder:validation:Required
	LocalMember FederationMember `json:"localMember"`

	// Members defines the other federation members (peer controllers)
	// +optional
	Members []FederationPeer `json:"members,omitempty"`

	// Sync defines configuration synchronization settings
	// +optional
	Sync *FederationSyncConfig `json:"sync,omitempty"`

	// ConflictResolution defines how to handle conflicting changes
	// +optional
	ConflictResolution *ConflictResolutionConfig `json:"conflictResolution,omitempty"`

	// HealthCheck defines health checking for federation members
	// +optional
	HealthCheck *FederationHealthCheck `json:"healthCheck,omitempty"`

	// SplitBrain defines split-brain detection and protection settings
	// +optional
	SplitBrain *SplitBrainCRDConfig `json:"splitBrain,omitempty"`

	// Paused suspends federation sync
	// +kubebuilder:default=false
	// +optional
	Paused bool `json:"paused,omitempty"`
}

// FederationMember defines a controller's identity in the federation
type FederationMember struct {
	// Name is a unique name for this member within the federation
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// Region is the geographic region of this member
	// +optional
	Region string `json:"region,omitempty"`

	// Zone is the availability zone of this member
	// +optional
	Zone string `json:"zone,omitempty"`

	// Endpoint is the gRPC endpoint for federation communication
	// Format: "host:port"
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// Labels are additional labels for this member
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// FederationPeer defines a peer controller in the federation
type FederationPeer struct {
	// Name is a unique name for this peer within the federation
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`

	// Region is the geographic region of this peer
	// +optional
	Region string `json:"region,omitempty"`

	// Zone is the availability zone of this peer
	// +optional
	Zone string `json:"zone,omitempty"`

	// Endpoint is the gRPC endpoint for this peer
	// Format: "host:port"
	// +kubebuilder:validation:Required
	Endpoint string `json:"endpoint"`

	// TLS defines mTLS configuration for communicating with this peer
	// +optional
	TLS *FederationTLS `json:"tls,omitempty"`

	// Priority determines the order for sync operations (lower = higher priority)
	// +kubebuilder:default=100
	// +optional
	Priority int32 `json:"priority,omitempty"`

	// Labels are additional labels for this peer
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// FederationTLS defines mTLS configuration for federation communication
type FederationTLS struct {
	// Enabled enables mTLS for peer communication
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// CASecretRef references a secret containing the CA certificate
	// +optional
	CASecretRef *corev1.SecretReference `json:"caSecretRef,omitempty"`

	// ClientCertSecretRef references a secret containing the client certificate and key
	// +optional
	ClientCertSecretRef *corev1.SecretReference `json:"clientCertSecretRef,omitempty"`

	// ServerName is the expected server name for TLS verification
	// +optional
	ServerName string `json:"serverName,omitempty"`

	// InsecureSkipVerify skips TLS certificate verification (NOT recommended)
	// +kubebuilder:default=false
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// FederationSyncConfig defines synchronization settings
type FederationSyncConfig struct {
	// Interval is how often to sync with peers
	// +kubebuilder:default="5s"
	// +optional
	Interval *metav1.Duration `json:"interval,omitempty"`

	// Timeout is the timeout for sync operations
	// +kubebuilder:default="30s"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// BatchSize is the maximum number of resources per sync batch
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +optional
	BatchSize int32 `json:"batchSize,omitempty"`

	// Compression enables compression for sync traffic
	// +kubebuilder:default=true
	// +optional
	Compression *bool `json:"compression,omitempty"`

	// ResourceTypes specifies which resource types to sync
	// If empty, all NovaEdge resources are synced
	// +optional
	ResourceTypes []string `json:"resourceTypes,omitempty"`

	// ExcludeNamespaces excludes specific namespaces from sync
	// +optional
	ExcludeNamespaces []string `json:"excludeNamespaces,omitempty"`
}

// ConflictResolutionConfig defines how to handle conflicting changes
type ConflictResolutionConfig struct {
	// Strategy is the conflict resolution strategy
	// - LastWriterWins: The most recent change wins (based on vector clocks)
	// - Merge: Attempt to merge changes (for lists/maps)
	// - Manual: Flag conflicts for manual resolution
	// +kubebuilder:default="LastWriterWins"
	// +kubebuilder:validation:Enum=LastWriterWins;Merge;Manual
	// +optional
	Strategy ConflictResolutionStrategy `json:"strategy,omitempty"`

	// VectorClocks enables vector clocks for change ordering
	// +kubebuilder:default=true
	// +optional
	VectorClocks *bool `json:"vectorClocks,omitempty"`

	// TombstoneTTL is how long to keep deletion records
	// +kubebuilder:default="24h"
	// +optional
	TombstoneTTL *metav1.Duration `json:"tombstoneTTL,omitempty"`
}

// ConflictResolutionStrategy defines conflict resolution strategies
type ConflictResolutionStrategy string

const (
	// ConflictResolutionLastWriterWins uses timestamps to determine the winner
	ConflictResolutionLastWriterWins ConflictResolutionStrategy = "LastWriterWins"
	// ConflictResolutionMerge attempts to merge conflicting changes
	ConflictResolutionMerge ConflictResolutionStrategy = "Merge"
	// ConflictResolutionManual flags conflicts for manual resolution
	ConflictResolutionManual ConflictResolutionStrategy = "Manual"
)

// FederationHealthCheck defines health checking for federation members
type FederationHealthCheck struct {
	// Interval is how often to check peer health
	// +kubebuilder:default="10s"
	// +optional
	Interval *metav1.Duration `json:"interval,omitempty"`

	// Timeout is the timeout for health checks
	// +kubebuilder:default="5s"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// FailureThreshold is the number of failures before marking a peer unhealthy
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +optional
	FailureThreshold int32 `json:"failureThreshold,omitempty"`

	// SuccessThreshold is the number of successes before marking a peer healthy
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	SuccessThreshold int32 `json:"successThreshold,omitempty"`
}

// QuorumMode defines how quorum is calculated for split-brain prevention
// +kubebuilder:validation:Enum=Controllers;AgentAssisted
type QuorumMode string

const (
	// QuorumModeControllers uses only controller-to-controller connectivity
	// Requires 3+ controllers for effective split-brain prevention
	QuorumModeControllers QuorumMode = "Controllers"

	// QuorumModeAgentAssisted uses agent reachability as additional quorum participants
	// Allows split-brain prevention with only 2 controllers by using agents as witnesses
	QuorumModeAgentAssisted QuorumMode = "AgentAssisted"
)

// SplitBrainCRDConfig defines split-brain detection and protection settings
type SplitBrainCRDConfig struct {
	// Enabled enables split-brain detection
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// PartitionTimeout is how long without peer contact before declaring partition
	// +kubebuilder:default="30s"
	// +optional
	PartitionTimeout *metav1.Duration `json:"partitionTimeout,omitempty"`

	// QuorumMode determines how quorum is calculated
	// - Controllers: Traditional controller-only quorum (requires 3+ controllers)
	// - AgentAssisted: Uses agent reachability for quorum (works with 2 controllers)
	// +kubebuilder:default="Controllers"
	// +optional
	QuorumMode QuorumMode `json:"quorumMode,omitempty"`

	// QuorumRequired requires a quorum of peers for write operations
	// If true, writes are rejected when we don't have quorum
	// +kubebuilder:default=false
	// +optional
	QuorumRequired *bool `json:"quorumRequired,omitempty"`

	// FencingEnabled enables write fencing during detected partition
	// When true, writes are blocked during confirmed partition
	// +kubebuilder:default=false
	// +optional
	FencingEnabled *bool `json:"fencingEnabled,omitempty"`

	// HealingGracePeriod is how long to wait after partition heals
	// before fully accepting writes (to allow state reconciliation)
	// +kubebuilder:default="5s"
	// +optional
	HealingGracePeriod *metav1.Duration `json:"healingGracePeriod,omitempty"`

	// AutoResolveOnHeal automatically resolves conflicts when partition heals
	// +kubebuilder:default=true
	// +optional
	AutoResolveOnHeal *bool `json:"autoResolveOnHeal,omitempty"`

	// AgentQuorum configures agent-assisted quorum settings
	// Only used when QuorumMode is AgentAssisted
	// +optional
	AgentQuorum *AgentQuorumConfig `json:"agentQuorum,omitempty"`
}

// AgentQuorumConfig configures agent-assisted quorum for split-brain prevention
type AgentQuorumConfig struct {
	// ControllerWeight is the voting weight of each controller
	// Controllers typically have higher weight than individual agents
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +optional
	ControllerWeight int32 `json:"controllerWeight,omitempty"`

	// AgentWeight is the voting weight of each agent
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	AgentWeight int32 `json:"agentWeight,omitempty"`

	// MinAgentsForQuorum is the minimum number of agents required for quorum
	// Prevents a controller with zero agents from claiming quorum
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	MinAgentsForQuorum int32 `json:"minAgentsForQuorum,omitempty"`
}

// NovaEdgeFederationStatus defines the observed state of NovaEdgeFederation
type NovaEdgeFederationStatus struct {
	// Phase is the current phase of the federation
	// +optional
	Phase FederationPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Members contains the status of each federation member
	// +optional
	Members []FederationMemberStatus `json:"members,omitempty"`

	// LastSyncTime is when the last successful sync occurred
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// SyncLag is the average sync lag across all members
	// +optional
	SyncLag *metav1.Duration `json:"syncLag,omitempty"`

	// LocalVectorClock is this controller's vector clock
	// +optional
	LocalVectorClock map[string]int64 `json:"localVectorClock,omitempty"`

	// ConflictsPending is the number of conflicts awaiting resolution
	// +optional
	ConflictsPending int32 `json:"conflictsPending,omitempty"`

	// SplitBrain contains the current split-brain detection status
	// +optional
	SplitBrain *SplitBrainStatus `json:"splitBrain,omitempty"`

	// ObservedGeneration is the generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// SplitBrainStatus contains the current split-brain detection status
type SplitBrainStatus struct {
	// PartitionState is the current partition state
	// +optional
	PartitionState PartitionState `json:"partitionState,omitempty"`

	// HaveQuorum indicates if we currently have quorum
	// +optional
	HaveQuorum bool `json:"haveQuorum,omitempty"`

	// WritesFenced indicates if writes are currently blocked
	// +optional
	WritesFenced bool `json:"writesFenced,omitempty"`

	// PartitionDetectedAt is when the partition was detected (if any)
	// +optional
	PartitionDetectedAt *metav1.Time `json:"partitionDetectedAt,omitempty"`

	// ReachablePeers lists peers we can currently reach
	// +optional
	ReachablePeers []string `json:"reachablePeers,omitempty"`

	// UnreachablePeers lists peers we cannot currently reach
	// +optional
	UnreachablePeers []string `json:"unreachablePeers,omitempty"`

	// AgentQuorumStatus contains agent-assisted quorum information
	// +optional
	AgentQuorumStatus *AgentQuorumStatus `json:"agentQuorumStatus,omitempty"`
}

// AgentQuorumStatus contains agent-assisted quorum status information
type AgentQuorumStatus struct {
	// TotalAgents is the total number of agents across all controllers
	// +optional
	TotalAgents int32 `json:"totalAgents,omitempty"`

	// ReachableAgents is the number of agents this controller can reach
	// +optional
	ReachableAgents int32 `json:"reachableAgents,omitempty"`

	// OurVotes is our calculated vote count
	// +optional
	OurVotes int32 `json:"ourVotes,omitempty"`

	// TotalVotes is the total possible votes
	// +optional
	TotalVotes int32 `json:"totalVotes,omitempty"`

	// QuorumThreshold is the minimum votes needed for quorum
	// +optional
	QuorumThreshold int32 `json:"quorumThreshold,omitempty"`
}

// PartitionState represents the current network partition state
// +kubebuilder:validation:Enum=Healthy;Suspected;Confirmed;Healing
type PartitionState string

const (
	// PartitionStateHealthy means all peers are reachable
	PartitionStateHealthy PartitionState = "Healthy"
	// PartitionStateSuspected means some peers are not responding
	PartitionStateSuspected PartitionState = "Suspected"
	// PartitionStateConfirmed means we've confirmed a network partition
	PartitionStateConfirmed PartitionState = "Confirmed"
	// PartitionStateHealing means partition is healing, reconciliation in progress
	PartitionStateHealing PartitionState = "Healing"
)

// FederationPhase represents the phase of the federation
type FederationPhase string

const (
	// FederationPhaseInitializing means the federation is starting up
	FederationPhaseInitializing FederationPhase = "Initializing"
	// FederationPhaseSyncing means initial sync is in progress
	FederationPhaseSyncing FederationPhase = "Syncing"
	// FederationPhaseHealthy means all members are healthy and in sync
	FederationPhaseHealthy FederationPhase = "Healthy"
	// FederationPhaseDegraded means some members are unhealthy or out of sync
	FederationPhaseDegraded FederationPhase = "Degraded"
	// FederationPhasePartitioned means the local member is partitioned from peers
	FederationPhasePartitioned FederationPhase = "Partitioned"
)

// FederationMemberStatus represents the status of a federation member
type FederationMemberStatus struct {
	// Name is the member name
	Name string `json:"name"`

	// Healthy indicates if the member is reachable and responding
	// +optional
	Healthy bool `json:"healthy,omitempty"`

	// LastSeen is when this member was last successfully contacted
	// +optional
	LastSeen *metav1.Time `json:"lastSeen,omitempty"`

	// LastSyncTime is when the last successful sync with this member occurred
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// SyncLag is the sync lag with this member
	// +optional
	SyncLag *metav1.Duration `json:"syncLag,omitempty"`

	// VectorClock is this member's last known vector clock
	// +optional
	VectorClock map[string]int64 `json:"vectorClock,omitempty"`

	// AgentCount is the number of agents connected to this member
	// +optional
	AgentCount int32 `json:"agentCount,omitempty"`

	// Error contains the last error message if unhealthy
	// +optional
	Error string `json:"error,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=fed
// +kubebuilder:printcolumn:name="Federation",type=string,JSONPath=`.spec.federationID`
// +kubebuilder:printcolumn:name="Local",type=string,JSONPath=`.spec.localMember.name`
// +kubebuilder:printcolumn:name="Members",type=integer,JSONPath=`.status.members[?(@.healthy==true)]`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NovaEdgeFederation is the Schema for the novaedgefederations API
// It configures active/active federation between multiple NovaEdge controllers
type NovaEdgeFederation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NovaEdgeFederationSpec   `json:"spec,omitempty"`
	Status NovaEdgeFederationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NovaEdgeFederationList contains a list of NovaEdgeFederation
type NovaEdgeFederationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NovaEdgeFederation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NovaEdgeFederation{}, &NovaEdgeFederationList{})
}
