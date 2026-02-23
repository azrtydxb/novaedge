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

// ExtractResourceChanges extracts changed resources from a snapshot compared to a baseline
func ExtractResourceChanges(current, baseline *pb.ConfigSnapshot) []*pb.ResourceChange {
	changes := make([]*pb.ResourceChange, 0, len(current.Gateways)+len(current.Routes)+len(current.Clusters)+len(current.Policies))

	// Build maps of current resources
	currentGateways := make(map[string]*pb.Gateway)
	for _, gw := range current.Gateways {
		key := gw.Namespace + "/" + gw.Name
		currentGateways[key] = gw
	}

	currentRoutes := make(map[string]*pb.Route)
	for _, r := range current.Routes {
		key := r.Namespace + "/" + r.Name
		currentRoutes[key] = r
	}

	currentClusters := make(map[string]*pb.Cluster)
	for _, c := range current.Clusters {
		key := c.Namespace + "/" + c.Name
		currentClusters[key] = c
	}

	currentPolicies := make(map[string]*pb.Policy)
	for _, p := range current.Policies {
		key := p.Namespace + "/" + p.Name
		currentPolicies[key] = p
	}

	// Compare with baseline if provided
	if baseline != nil {
		// Check for deleted gateways
		for _, gw := range baseline.Gateways {
			key := gw.Namespace + "/" + gw.Name
			if _, exists := currentGateways[key]; !exists {
				changes = append(changes, &pb.ResourceChange{
					ChangeType:   pb.ChangeType_DELETED,
					ResourceType: "ProxyGateway",
					Namespace:    gw.Namespace,
					Name:         gw.Name,
				})
			}
		}

		// Check for deleted routes
		for _, r := range baseline.Routes {
			key := r.Namespace + "/" + r.Name
			if _, exists := currentRoutes[key]; !exists {
				changes = append(changes, &pb.ResourceChange{
					ChangeType:   pb.ChangeType_DELETED,
					ResourceType: "ProxyRoute",
					Namespace:    r.Namespace,
					Name:         r.Name,
				})
			}
		}

		// Check for deleted clusters
		for _, c := range baseline.Clusters {
			key := c.Namespace + "/" + c.Name
			if _, exists := currentClusters[key]; !exists {
				changes = append(changes, &pb.ResourceChange{
					ChangeType:   pb.ChangeType_DELETED,
					ResourceType: "ProxyBackend",
					Namespace:    c.Namespace,
					Name:         c.Name,
				})
			}
		}

		// Check for deleted policies
		for _, p := range baseline.Policies {
			key := p.Namespace + "/" + p.Name
			if _, exists := currentPolicies[key]; !exists {
				changes = append(changes, &pb.ResourceChange{
					ChangeType:   pb.ChangeType_DELETED,
					ResourceType: "ProxyPolicy",
					Namespace:    p.Namespace,
					Name:         p.Name,
				})
			}
		}
	}

	// Extract vector clock (may be nil if metadata not set)
	var vectorClock map[string]int64
	if current.FederationMetadata != nil {
		vectorClock = current.FederationMetadata.VectorClock
	}

	// Build baseline content hashes for comparison
	baselineHashes := make(map[string]string) // key -> content hash
	if baseline != nil {
		for _, gw := range baseline.Gateways {
			key := gw.Namespace + "/" + gw.Name
			data, _ := proto.Marshal(gw)
			baselineHashes["ProxyGateway/"+key] = hashBytes(data)
		}
		for _, r := range baseline.Routes {
			key := r.Namespace + "/" + r.Name
			data, _ := proto.Marshal(r)
			baselineHashes["ProxyRoute/"+key] = hashBytes(data)
		}
		for _, c := range baseline.Clusters {
			key := c.Namespace + "/" + c.Name
			data, _ := proto.Marshal(c)
			baselineHashes["ProxyBackend/"+key] = hashBytes(data)
		}
		for _, p := range baseline.Policies {
			key := p.Namespace + "/" + p.Name
			data, _ := proto.Marshal(p)
			baselineHashes["ProxyPolicy/"+key] = hashBytes(data)
		}
	}

	// Detect created/updated resources by comparing content hashes
	for _, gw := range currentGateways {
		data, _ := proto.Marshal(gw)
		hash := hashBytes(data)
		lookupKey := "ProxyGateway/" + gw.Namespace + "/" + gw.Name
		changeType := pb.ChangeType_CREATED
		if oldHash, existed := baselineHashes[lookupKey]; existed {
			if oldHash == hash {
				continue // unchanged, skip
			}
			changeType = pb.ChangeType_UPDATED
		}
		changes = append(changes, &pb.ResourceChange{
			ChangeType:   changeType,
			ResourceType: "ProxyGateway",
			Namespace:    gw.Namespace,
			Name:         gw.Name,
			ResourceData: data,
			ResourceHash: hash,
			VectorClock:  vectorClock,
		})
	}

	for _, r := range currentRoutes {
		data, _ := proto.Marshal(r)
		hash := hashBytes(data)
		lookupKey := "ProxyRoute/" + r.Namespace + "/" + r.Name
		changeType := pb.ChangeType_CREATED
		if oldHash, existed := baselineHashes[lookupKey]; existed {
			if oldHash == hash {
				continue // unchanged, skip
			}
			changeType = pb.ChangeType_UPDATED
		}
		changes = append(changes, &pb.ResourceChange{
			ChangeType:   changeType,
			ResourceType: "ProxyRoute",
			Namespace:    r.Namespace,
			Name:         r.Name,
			ResourceData: data,
			ResourceHash: hash,
			VectorClock:  vectorClock,
		})
	}

	for _, c := range currentClusters {
		data, _ := proto.Marshal(c)
		hash := hashBytes(data)
		lookupKey := "ProxyBackend/" + c.Namespace + "/" + c.Name
		changeType := pb.ChangeType_CREATED
		if oldHash, existed := baselineHashes[lookupKey]; existed {
			if oldHash == hash {
				continue // unchanged, skip
			}
			changeType = pb.ChangeType_UPDATED
		}
		changes = append(changes, &pb.ResourceChange{
			ChangeType:   changeType,
			ResourceType: "ProxyBackend",
			Namespace:    c.Namespace,
			Name:         c.Name,
			ResourceData: data,
			ResourceHash: hash,
			VectorClock:  vectorClock,
		})
	}

	for _, p := range currentPolicies {
		data, _ := proto.Marshal(p)
		hash := hashBytes(data)
		lookupKey := "ProxyPolicy/" + p.Namespace + "/" + p.Name
		changeType := pb.ChangeType_CREATED
		if oldHash, existed := baselineHashes[lookupKey]; existed {
			if oldHash == hash {
				continue // unchanged, skip
			}
			changeType = pb.ChangeType_UPDATED
		}
		changes = append(changes, &pb.ResourceChange{
			ChangeType:   changeType,
			ResourceType: "ProxyPolicy",
			Namespace:    p.Namespace,
			Name:         p.Name,
			ResourceData: data,
			ResourceHash: hash,
			VectorClock:  vectorClock,
		})
	}

	return changes
}

// hashBytes calculates a short hash of data
func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}
