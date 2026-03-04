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
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"

	"google.golang.org/protobuf/proto"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// SnapshotEnhancer adds federation metadata to config snapshots
type SnapshotEnhancer struct {
	// Configuration
	federationID   string
	controllerName string

	// Vector clock
	vectorClock *VectorClock

	// Sequence number for ordering
	sequenceNumber int64

	// Available controllers for agent failover
	controllers   []*ControllerEndpoint
	controllersMu sync.RWMutex
}

// ControllerEndpoint represents a controller endpoint for failover
type ControllerEndpoint struct {
	Name        string
	Endpoint    string
	Priority    int32
	Region      string
	Zone        string
	Healthy     bool
	VectorClock map[string]int64
}

// NewSnapshotEnhancer creates a new snapshot enhancer
func NewSnapshotEnhancer(federationID, controllerName string) *SnapshotEnhancer {
	return &SnapshotEnhancer{
		federationID:   federationID,
		controllerName: controllerName,
		vectorClock:    NewVectorClock(),
	}
}

// SetVectorClock sets the vector clock to use
func (e *SnapshotEnhancer) SetVectorClock(vc *VectorClock) {
	e.vectorClock = vc
}

// UpdateControllers updates the list of available controllers
func (e *SnapshotEnhancer) UpdateControllers(controllers []*ControllerEndpoint) {
	e.controllersMu.Lock()
	defer e.controllersMu.Unlock()
	e.controllers = controllers
}

// EnhanceSnapshot adds federation metadata to a snapshot
func (e *SnapshotEnhancer) EnhanceSnapshot(snapshot *pb.ConfigSnapshot, fromFederation bool) *pb.ConfigSnapshot {
	if snapshot == nil {
		return nil
	}

	// Increment sequence number
	seq := atomic.AddInt64(&e.sequenceNumber, 1)

	// Increment our clock for local changes
	if !fromFederation {
		e.vectorClock.Increment(e.controllerName)
	}

	// Calculate content hash
	contentHash := e.calculateContentHash(snapshot)

	// Add federation metadata
	snapshot.FederationMetadata = &pb.FederationMetadata{
		VectorClock:      e.vectorClock.ToMap(),
		FederationId:     e.federationID,
		OriginController: e.controllerName,
		ContentHash:      contentHash,
		SequenceNumber:   seq,
		FromFederation:   fromFederation,
	}

	// Add available controllers
	e.controllersMu.RLock()
	snapshot.AvailableControllers = make([]*pb.ControllerInfo, 0, len(e.controllers))
	for _, ctrl := range e.controllers {
		snapshot.AvailableControllers = append(snapshot.AvailableControllers, &pb.ControllerInfo{
			Name:        ctrl.Name,
			Endpoint:    ctrl.Endpoint,
			Priority:    ctrl.Priority,
			Region:      ctrl.Region,
			Zone:        ctrl.Zone,
			Healthy:     ctrl.Healthy,
			VectorClock: ctrl.VectorClock,
		})
	}
	e.controllersMu.RUnlock()

	return snapshot
}

// calculateContentHash calculates a hash of the snapshot content
func (e *SnapshotEnhancer) calculateContentHash(snapshot *pb.ConfigSnapshot) string {
	// Create a copy without metadata for hashing
	cloned := proto.Clone(snapshot)
	snapshotCopy, ok := cloned.(*pb.ConfigSnapshot)
	if !ok {
		return ""
	}
	snapshotCopy.FederationMetadata = nil
	snapshotCopy.AvailableControllers = nil
	snapshotCopy.Version = ""

	data, err := proto.Marshal(snapshotCopy)
	if err != nil {
		return ""
	}

	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:16])
}

// MergeVectorClock merges an incoming vector clock
func (e *SnapshotEnhancer) MergeVectorClock(incoming map[string]int64) {
	e.vectorClock.MergeMap(incoming)
}

// GetVectorClock returns the current vector clock
func (e *SnapshotEnhancer) GetVectorClock() map[string]int64 {
	return e.vectorClock.ToMap()
}

// CompareSnapshots compares two snapshots using vector clocks
// Returns:
//
//	 1 if a happened after b
//	-1 if a happened before b
//	 0 if concurrent (conflict)
func CompareSnapshots(a, b *pb.ConfigSnapshot) int {
	if a == nil || a.FederationMetadata == nil {
		if b == nil || b.FederationMetadata == nil {
			return 0
		}
		return -1
	}
	if b == nil || b.FederationMetadata == nil {
		return 1
	}

	vcA := NewVectorClockFromMap(a.FederationMetadata.VectorClock)
	vcB := NewVectorClockFromMap(b.FederationMetadata.VectorClock)

	return vcA.Compare(vcB)
}

// IsNewerSnapshot returns true if a is newer than b based on vector clocks
func IsNewerSnapshot(a, b *pb.ConfigSnapshot) bool {
	return CompareSnapshots(a, b) == 1
}

// SnapshotsAreConcurrent returns true if the snapshots are concurrent (potential conflict)
func SnapshotsAreConcurrent(a, b *pb.ConfigSnapshot) bool {
	return CompareSnapshots(a, b) == 0
}

// SnapshotNeedsSync returns true if this snapshot should be synced to a peer
// based on comparing our snapshot's vector clock with the peer's
func SnapshotNeedsSync(snapshot *pb.ConfigSnapshot, peerVectorClock map[string]int64) bool {
	if snapshot == nil || snapshot.FederationMetadata == nil {
		return false
	}

	ourVC := NewVectorClockFromMap(snapshot.FederationMetadata.VectorClock)
	peerVC := NewVectorClockFromMap(peerVectorClock)

	// Sync if our snapshot is newer or concurrent
	cmp := ourVC.Compare(peerVC)
	return cmp >= 0
}

// namespacedResource is a common interface for resources with Namespace and Name fields.
type namespacedResource interface {
	proto.Message
	GetNamespace() string
	GetName() string
}

// resourceKey returns the "namespace/name" key for a resource.
func resourceKey(r namespacedResource) string {
	return r.GetNamespace() + "/" + r.GetName()
}

// detectDeletions appends DELETED changes for baseline resources not present in currentKeys.
func detectDeletions[T namespacedResource](baseline []T, currentKeys map[string]bool, resourceType string, changes *[]*pb.ResourceChange) {
	for _, r := range baseline {
		if !currentKeys[resourceKey(r)] {
			*changes = append(*changes, &pb.ResourceChange{
				ChangeType:   pb.ChangeType_DELETED,
				ResourceType: resourceType,
				Namespace:    r.GetNamespace(),
				Name:         r.GetName(),
			})
		}
	}
}

// buildBaselineHashes builds a map of "ResourceType/ns/name" -> content hash from baseline resources.
func buildBaselineHashes[T namespacedResource](resources []T, resourceType string, hashes map[string]string) {
	for _, r := range resources {
		data, _ := proto.Marshal(r)
		hashes[resourceType+"/"+resourceKey(r)] = hashBytes(data)
	}
}

// detectCreatesUpdates appends CREATED or UPDATED changes for current resources by comparing content hashes.
func detectCreatesUpdates[T namespacedResource](currentMap map[string]T, resourceType string, baselineHashes map[string]string, vectorClock map[string]int64, changes *[]*pb.ResourceChange) {
	for _, r := range currentMap {
		data, _ := proto.Marshal(r)
		hash := hashBytes(data)
		lookupKey := resourceType + "/" + resourceKey(r)
		changeType := pb.ChangeType_CREATED
		if oldHash, existed := baselineHashes[lookupKey]; existed {
			if oldHash == hash {
				continue
			}
			changeType = pb.ChangeType_UPDATED
		}
		*changes = append(*changes, &pb.ResourceChange{
			ChangeType:   changeType,
			ResourceType: resourceType,
			Namespace:    r.GetNamespace(),
			Name:         r.GetName(),
			ResourceData: data,
			ResourceHash: hash,
			VectorClock:  vectorClock,
		})
	}
}

// buildResourceMap builds a map of "ns/name" -> resource from a slice.
func buildResourceMap[T namespacedResource](resources []T) map[string]T {
	m := make(map[string]T, len(resources))
	for _, r := range resources {
		m[resourceKey(r)] = r
	}
	return m
}

// keysFromMap returns the set of keys from a map as a bool map.
func keysFromMap[T any](m map[string]T) map[string]bool {
	keys := make(map[string]bool, len(m))
	for k := range m {
		keys[k] = true
	}
	return keys
}

// ExtractResourceChanges extracts changed resources from a snapshot compared to a baseline
func ExtractResourceChanges(current, baseline *pb.ConfigSnapshot) []*pb.ResourceChange {
	changes := make([]*pb.ResourceChange, 0, len(current.Gateways)+len(current.Routes)+len(current.Clusters)+len(current.Policies))

	// Build maps of current resources
	currentGateways := buildResourceMap(current.Gateways)
	currentRoutes := buildResourceMap(current.Routes)
	currentClusters := buildResourceMap(current.Clusters)
	currentPolicies := buildResourceMap(current.Policies)

	// Detect deletions against baseline
	if baseline != nil {
		detectDeletions(baseline.Gateways, keysFromMap(currentGateways), "ProxyGateway", &changes)
		detectDeletions(baseline.Routes, keysFromMap(currentRoutes), "ProxyRoute", &changes)
		detectDeletions(baseline.Clusters, keysFromMap(currentClusters), "ProxyBackend", &changes)
		detectDeletions(baseline.Policies, keysFromMap(currentPolicies), "ProxyPolicy", &changes)
	}

	// Extract vector clock
	var vectorClock map[string]int64
	if current.FederationMetadata != nil {
		vectorClock = current.FederationMetadata.VectorClock
	}

	// Build baseline content hashes
	baselineHashes := make(map[string]string)
	if baseline != nil {
		buildBaselineHashes(baseline.Gateways, "ProxyGateway", baselineHashes)
		buildBaselineHashes(baseline.Routes, "ProxyRoute", baselineHashes)
		buildBaselineHashes(baseline.Clusters, "ProxyBackend", baselineHashes)
		buildBaselineHashes(baseline.Policies, "ProxyPolicy", baselineHashes)
	}

	// Detect creates/updates
	detectCreatesUpdates(currentGateways, "ProxyGateway", baselineHashes, vectorClock, &changes)
	detectCreatesUpdates(currentRoutes, "ProxyRoute", baselineHashes, vectorClock, &changes)
	detectCreatesUpdates(currentClusters, "ProxyBackend", baselineHashes, vectorClock, &changes)
	detectCreatesUpdates(currentPolicies, "ProxyPolicy", baselineHashes, vectorClock, &changes)

	return changes
}

// hashBytes calculates a short hash of data
func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}
