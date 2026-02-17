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

// Package federation implements multi-cluster federation with hub-spoke topology,
// providing cross-cluster configuration synchronization, anti-entropy repair,
// split-brain detection, and vector clock-based conflict resolution.
package federation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// DefaultAntiEntropyInterval is how often to run anti-entropy
	DefaultAntiEntropyInterval = 5 * time.Minute

	// MerkleTreeDepth is the depth of the merkle tree for resource comparison
	MerkleTreeDepth = 4

	// MaxDriftResolutionBatch is the max resources to sync per anti-entropy run
	MaxDriftResolutionBatch = 100

	// antiEntropyRequestTimeout is the timeout for individual peer RPCs during anti-entropy
	antiEntropyRequestTimeout = 30 * time.Second
)

// PeerClientLookup is a function that retrieves a PeerClient by peer name.
type PeerClientLookup func(peerName string) (*PeerClient, bool)

// AntiEntropyConfig configures the anti-entropy mechanism
type AntiEntropyConfig struct {
	// Enabled controls whether anti-entropy is active
	Enabled bool

	// Interval is how often to run anti-entropy checks
	Interval time.Duration

	// BatchSize is max resources to sync per run
	BatchSize int

	// RepairMode controls how to handle detected drift
	// - "pull": Pull missing/outdated resources from peer
	// - "push": Push our resources to peer
	// - "bidirectional": Exchange resources in both directions
	RepairMode string
}

// DefaultAntiEntropyConfig returns sensible defaults
func DefaultAntiEntropyConfig() *AntiEntropyConfig {
	return &AntiEntropyConfig{
		Enabled:    true,
		Interval:   DefaultAntiEntropyInterval,
		BatchSize:  MaxDriftResolutionBatch,
		RepairMode: RepairModeBidirectional,
	}
}

// MerkleNode represents a node in the merkle tree
type MerkleNode struct {
	// Hash is the hash of this node
	Hash string

	// Level is the depth in the tree (0 = root)
	Level int

	// Prefix is the key prefix this node covers
	Prefix string

	// IsLeaf indicates this is a leaf node containing actual data
	IsLeaf bool

	// Children contains child nodes (for non-leaf nodes)
	Children map[string]*MerkleNode

	// ChildHashes contains child node hashes for quick comparison
	ChildHashes map[string]string

	// Resources contains resource keys (for leaf nodes)
	Resources []string
}

// MerkleTree is a hash tree for efficient resource comparison
type MerkleTree struct {
	root      *MerkleNode
	resources map[string]string // key -> hash
	mu        sync.RWMutex
	depth     int
}

// NewMerkleTree creates a new merkle tree
func NewMerkleTree(depth int) *MerkleTree {
	if depth <= 0 {
		depth = MerkleTreeDepth
	}
	return &MerkleTree{
		resources: make(map[string]string),
		depth:     depth,
	}
}

// Update adds or updates a resource in the tree
func (t *MerkleTree) Update(key string, hash string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.resources[key] = hash
	t.rebuild()
}

// Remove removes a resource from the tree
func (t *MerkleTree) Remove(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.resources, key)
	t.rebuild()
}

// GetRoot returns the root hash
func (t *MerkleTree) GetRoot() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.root == nil {
		return ""
	}
	return t.root.Hash
}

// GetNodeAt returns the node at a specific prefix
func (t *MerkleTree) GetNodeAt(prefix string) *MerkleNode {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.findNode(t.root, prefix)
}

// findNode recursively finds a node by prefix
func (t *MerkleTree) findNode(node *MerkleNode, prefix string) *MerkleNode {
	if node == nil {
		return nil
	}

	if node.Prefix == prefix {
		return node
	}

	if node.IsLeaf {
		return nil
	}

	// Find the child whose prefix matches the target prefix
	for childPrefix, child := range node.Children {
		if childPrefix == prefix {
			return child
		}
		// If the target prefix starts with this child's prefix, recurse into it
		if len(prefix) > len(childPrefix) && prefix[:len(childPrefix)] == childPrefix {
			result := t.findNode(child, prefix)
			if result != nil {
				return result
			}
		}
	}

	return nil
}

// rebuild reconstructs the merkle tree from resources
func (t *MerkleTree) rebuild() {
	if len(t.resources) == 0 {
		t.root = nil
		return
	}

	// Sort keys for deterministic tree construction
	keys := make([]string, 0, len(t.resources))
	for k := range t.resources {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build tree bottom-up
	t.root = t.buildNode(keys, 0, "")
}

// buildNode recursively builds a tree node
func (t *MerkleTree) buildNode(keys []string, level int, prefix string) *MerkleNode {
	if len(keys) == 0 {
		return nil
	}

	node := &MerkleNode{
		Level:  level,
		Prefix: prefix,
	}

	// If at max depth or few enough keys, make a leaf
	if level >= t.depth || len(keys) <= 4 {
		node.IsLeaf = true
		node.Resources = keys

		// Hash all resources
		h := sha256.New()
		for _, key := range keys {
			h.Write([]byte(key))
			h.Write([]byte(t.resources[key]))
		}
		node.Hash = hex.EncodeToString(h.Sum(nil)[:16])
		return node
	}

	// Split keys into buckets by first character after prefix
	buckets := make(map[string][]string)
	for _, key := range keys {
		if len(key) > len(prefix) {
			bucket := key[len(prefix) : len(prefix)+1]
			buckets[bucket] = append(buckets[bucket], key)
		} else {
			buckets[""] = append(buckets[""], key)
		}
	}

	// Build children
	node.Children = make(map[string]*MerkleNode)
	node.ChildHashes = make(map[string]string)
	childHashes := make([]string, 0, len(buckets))

	for bucket, bucketKeys := range buckets {
		childPrefix := prefix + bucket
		child := t.buildNode(bucketKeys, level+1, childPrefix)
		if child != nil {
			node.Children[childPrefix] = child
			node.ChildHashes[childPrefix] = child.Hash
			childHashes = append(childHashes, child.Hash)
		}
	}

	// Sort child hashes for deterministic parent hash
	sort.Strings(childHashes)

	// Hash children
	h := sha256.New()
	for _, ch := range childHashes {
		h.Write([]byte(ch))
	}
	node.Hash = hex.EncodeToString(h.Sum(nil)[:16])

	return node
}

// Compare compares this tree with another and returns differing prefixes
func (t *MerkleTree) Compare(other *MerkleTree) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if other == nil {
		// All our resources are different
		keys := make([]string, 0, len(t.resources))
		for k := range t.resources {
			keys = append(keys, k)
		}
		return keys
	}

	other.mu.RLock()
	defer other.mu.RUnlock()

	// Quick check: if roots match, trees are identical
	if t.root != nil && other.root != nil && t.root.Hash == other.root.Hash {
		return nil
	}

	// Find all differing keys
	var diffs []string

	// Keys in our tree
	for key, hash := range t.resources {
		if otherHash, ok := other.resources[key]; !ok || otherHash != hash {
			diffs = append(diffs, key)
		}
	}

	// Keys only in other tree
	for key := range other.resources {
		if _, ok := t.resources[key]; !ok {
			diffs = append(diffs, key)
		}
	}

	return diffs
}

// GetResourceHashes returns all resource key -> hash mappings
func (t *MerkleTree) GetResourceHashes() map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]string, len(t.resources))
	for k, v := range t.resources {
		result[k] = v
	}
	return result
}

// AntiEntropyManager handles periodic drift detection and repair
type AntiEntropyManager struct {
	config *AntiEntropyConfig
	logger *zap.Logger

	// Local merkle tree
	localTree *MerkleTree

	// Server reference for resource access
	server *Server

	// peerLookup retrieves a PeerClient by name
	peerLookup PeerClientLookup

	// driftReports caches the latest drift reports per peer
	driftReports []*DriftReport

	// Context for lifecycle
	ctx    context.Context
	cancel context.CancelFunc

	mu sync.RWMutex
}

// NewAntiEntropyManager creates a new anti-entropy manager
func NewAntiEntropyManager(config *AntiEntropyConfig, server *Server, peerLookup PeerClientLookup, logger *zap.Logger) *AntiEntropyManager {
	if config == nil {
		config = DefaultAntiEntropyConfig()
	}

	return &AntiEntropyManager{
		config:     config,
		server:     server,
		peerLookup: peerLookup,
		logger:     logger.Named("anti-entropy"),
		localTree:  NewMerkleTree(MerkleTreeDepth),
	}
}

// Start begins the anti-entropy process
func (m *AntiEntropyManager) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)

	if !m.config.Enabled {
		m.logger.Info("Anti-entropy is disabled")
		return
	}

	m.logger.Info("Starting anti-entropy manager",
		zap.Duration("interval", m.config.Interval),
		zap.String("repair_mode", m.config.RepairMode),
	)

	go m.runLoop()
}

// Stop stops the anti-entropy process
func (m *AntiEntropyManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// runLoop runs the periodic anti-entropy check
func (m *AntiEntropyManager) runLoop() {
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.runAntiEntropy()
		}
	}
}

// runAntiEntropy performs one anti-entropy cycle
func (m *AntiEntropyManager) runAntiEntropy() {
	m.logger.Debug("Running anti-entropy check")

	// Build local merkle tree from current resources
	m.rebuildLocalTree()

	// Get peer states
	if m.server == nil {
		return
	}

	peerStates := m.server.GetPeerStates()
	if len(peerStates) == 0 {
		m.logger.Debug("No peers for anti-entropy")
		return
	}

	// Collect drift reports for this cycle
	var reports []*DriftReport

	// Compare with each peer
	for peerName, peerState := range peerStates {
		if !peerState.Healthy || !peerState.Connected {
			continue
		}

		report := m.compareWithPeer(peerName)
		if report != nil {
			reports = append(reports, report)
		}
	}

	// Cache the reports
	m.mu.Lock()
	m.driftReports = reports
	m.mu.Unlock()
}

// rebuildLocalTree rebuilds the local merkle tree from server resources
func (m *AntiEntropyManager) rebuildLocalTree() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.localTree = NewMerkleTree(MerkleTreeDepth)

	// Get all resources from server
	if m.server == nil {
		return
	}

	// Iterate through tracked resources
	m.server.resources.Range(func(key, value interface{}) bool {
		keyStr, ok := key.(string)
		if !ok {
			return true
		}
		resource, ok := value.(*TrackedResource)
		if !ok {
			return true
		}

		m.localTree.Update(keyStr, resource.Hash)
		return true
	})
}

// getPeerClient retrieves a PeerClient for the given peer name
func (m *AntiEntropyManager) getPeerClient(peerName string) (*PeerClient, bool) {
	if m.peerLookup == nil {
		return nil, false
	}
	return m.peerLookup(peerName)
}

// compareWithPeer compares our state with a peer and initiates repair if needed.
// Returns a DriftReport describing any detected differences.
func (m *AntiEntropyManager) compareWithPeer(peerName string) *DriftReport {
	m.logger.Debug("Comparing state with peer", zap.String("peer", peerName))

	if m.server == nil {
		return nil
	}

	// Get our vector clock
	ourVC := m.server.vectorClock.ToMap()

	// Get peer's vector clock from state
	peerState, ok := m.server.peerStates.Load(peerName)
	if !ok {
		return nil
	}

	state, ok := peerState.(*PeerState)
	if !ok {
		return nil
	}
	peerVC := state.VectorClock

	// Compare vector clocks
	ourVCObj := NewVectorClockFromMap(ourVC)

	var peerVCObj *VectorClock
	if peerVC != nil {
		peerVCObj = peerVC
	} else {
		peerVCObj = NewVectorClock()
	}

	cmp := ourVCObj.Compare(peerVCObj)

	switch cmp {
	case 0:
		// Concurrent - potential conflicts, need full merkle tree comparison
		return m.handleConcurrentState(peerName)

	case 1:
		// We're ahead - peer might need updates
		if m.config.RepairMode == RepairModePush || m.config.RepairMode == RepairModeBidirectional {
			return m.handleWeAreAhead(peerName)
		}

	case -1:
		// Peer is ahead - we might need updates
		if m.config.RepairMode == RepairModePull || m.config.RepairMode == RepairModeBidirectional {
			return m.handlePeerIsAhead(peerName)
		}
	}

	return nil
}

// handleConcurrentState handles the case where vector clocks are concurrent,
// meaning both sides may have changes the other doesn't. It performs a full
// merkle tree comparison by requesting all resources from the peer.
func (m *AntiEntropyManager) handleConcurrentState(peerName string) *DriftReport {
	m.logger.Info("Concurrent state detected, performing full comparison",
		zap.String("peer", peerName),
	)

	client, ok := m.getPeerClient(peerName)
	if !ok {
		m.logger.Warn("Cannot find peer client for anti-entropy comparison",
			zap.String("peer", peerName),
		)
		return nil
	}

	reqCtx, cancel := context.WithTimeout(m.ctx, antiEntropyRequestTimeout)
	defer cancel()

	// Request all resources from peer for comparison
	batches, err := client.RequestFullSync(reqCtx, m.server.config.ResourceTypes, nil, m.server.vectorClock.ToMap())
	if err != nil {
		m.logger.Error("Failed to request full sync from peer for anti-entropy",
			zap.String("peer", peerName),
			zap.Error(err),
		)
		return nil
	}

	// Build a peer tree from received resources
	peerResources := make(map[string]*pb.ResourceChange)
	for _, batch := range batches {
		for _, change := range batch.Resources {
			key := ResourceKey{
				Kind:      change.ResourceType,
				Namespace: change.Namespace,
				Name:      change.Name,
			}
			peerResources[key.String()] = change
		}
	}

	// Build a peer merkle tree for comparison
	peerTree := NewMerkleTree(MerkleTreeDepth)
	for keyStr, change := range peerResources {
		peerTree.Update(keyStr, change.ResourceHash)
	}

	// Compare trees
	m.mu.RLock()
	diffs := m.localTree.Compare(peerTree)
	m.mu.RUnlock()

	report := &DriftReport{
		Peer:      peerName,
		Timestamp: time.Now(),
	}

	if len(diffs) == 0 {
		m.logger.Debug("No drift detected with peer", zap.String("peer", peerName))
		return report
	}

	report.DifferingKeys = diffs

	// Categorize diffs and reconcile
	m.reconcileWithPeerResources(peerName, peerResources, report)

	m.logger.Info("Anti-entropy concurrent comparison complete",
		zap.String("peer", peerName),
		zap.Int("diffs", len(diffs)),
		zap.Int("missing_locally", len(report.MissingLocally)),
		zap.Int("missing_remotely", len(report.MissingRemotely)),
		zap.Int("hash_mismatches", len(report.HashMismatches)),
	)

	return report
}

// handleWeAreAhead handles the case where our vector clock is strictly ahead
// of the peer, meaning we have changes the peer doesn't know about.
func (m *AntiEntropyManager) handleWeAreAhead(peerName string) *DriftReport {
	m.logger.Info("We are ahead of peer, pushing updates",
		zap.String("peer", peerName),
	)

	client, ok := m.getPeerClient(peerName)
	if !ok {
		m.logger.Warn("Cannot find peer client for push",
			zap.String("peer", peerName),
		)
		return nil
	}

	// Request peer resources to find what they're missing
	reqCtx, cancel := context.WithTimeout(m.ctx, antiEntropyRequestTimeout)
	defer cancel()

	batches, err := client.RequestFullSync(reqCtx, m.server.config.ResourceTypes, nil, m.server.vectorClock.ToMap())
	if err != nil {
		m.logger.Error("Failed to request full sync from peer for push comparison",
			zap.String("peer", peerName),
			zap.Error(err),
		)
		return nil
	}

	// Build set of peer resource keys and hashes
	peerResourceHashes := make(map[string]string)
	for _, batch := range batches {
		for _, change := range batch.Resources {
			key := ResourceKey{
				Kind:      change.ResourceType,
				Namespace: change.Namespace,
				Name:      change.Name,
			}
			peerResourceHashes[key.String()] = change.ResourceHash
		}
	}

	report := &DriftReport{
		Peer:      peerName,
		Timestamp: time.Now(),
	}

	// Find resources the peer is missing or has outdated versions of
	sent := 0
	m.server.resources.Range(func(key, value interface{}) bool {
		if sent >= m.config.BatchSize {
			return false
		}

		keyStr, ok := key.(string)
		if !ok {
			return true
		}
		resource, ok := value.(*TrackedResource)
		if !ok {
			return true
		}

		peerHash, peerHas := peerResourceHashes[keyStr]
		if !peerHas {
			// Peer is missing this resource
			report.MissingRemotely = append(report.MissingRemotely, keyStr)
			if err := m.pushResourceToPeer(client, resource); err != nil {
				m.logger.Error("Failed to push resource to peer",
					zap.String("peer", peerName),
					zap.String("key", keyStr),
					zap.Error(err),
				)
			} else {
				sent++
			}
		} else if peerHash != resource.Hash {
			// Peer has an outdated version
			report.HashMismatches = append(report.HashMismatches, keyStr)
			if err := m.pushResourceToPeer(client, resource); err != nil {
				m.logger.Error("Failed to push updated resource to peer",
					zap.String("peer", peerName),
					zap.String("key", keyStr),
					zap.Error(err),
				)
			} else {
				sent++
			}
		}

		return true
	})

	report.DifferingKeys = make([]string, 0, len(report.MissingRemotely)+len(report.HashMismatches))
	report.DifferingKeys = append(report.DifferingKeys, report.MissingRemotely...)
	report.DifferingKeys = append(report.DifferingKeys, report.HashMismatches...)

	m.logger.Info("Anti-entropy push complete",
		zap.String("peer", peerName),
		zap.Int("pushed", sent),
		zap.Int("missing_remotely", len(report.MissingRemotely)),
		zap.Int("hash_mismatches", len(report.HashMismatches)),
	)

	return report
}

// handlePeerIsAhead handles the case where the peer's vector clock is strictly
// ahead, meaning the peer has changes we don't know about.
func (m *AntiEntropyManager) handlePeerIsAhead(peerName string) *DriftReport {
	m.logger.Info("Peer is ahead, pulling updates",
		zap.String("peer", peerName),
	)

	client, ok := m.getPeerClient(peerName)
	if !ok {
		m.logger.Warn("Cannot find peer client for pull",
			zap.String("peer", peerName),
		)
		return nil
	}

	reqCtx, cancel := context.WithTimeout(m.ctx, antiEntropyRequestTimeout)
	defer cancel()

	batches, err := client.RequestFullSync(reqCtx, m.server.config.ResourceTypes, nil, m.server.vectorClock.ToMap())
	if err != nil {
		m.logger.Error("Failed to request full sync from peer for pull",
			zap.String("peer", peerName),
			zap.Error(err),
		)
		return nil
	}

	// Build peer resources map
	peerResources := make(map[string]*pb.ResourceChange)
	for _, batch := range batches {
		for _, change := range batch.Resources {
			key := ResourceKey{
				Kind:      change.ResourceType,
				Namespace: change.Namespace,
				Name:      change.Name,
			}
			peerResources[key.String()] = change
		}
	}

	report := &DriftReport{
		Peer:      peerName,
		Timestamp: time.Now(),
	}

	// Apply peer resources that we're missing or have outdated
	m.reconcileWithPeerResources(peerName, peerResources, report)

	m.logger.Info("Anti-entropy pull complete",
		zap.String("peer", peerName),
		zap.Int("missing_locally", len(report.MissingLocally)),
		zap.Int("hash_mismatches", len(report.HashMismatches)),
	)

	return report
}

// reconcileWithPeerResources compares peer resources with local resources
// and applies differences. In bidirectional mode, it both pulls missing
// resources from the peer and pushes resources the peer is missing.
func (m *AntiEntropyManager) reconcileWithPeerResources(peerName string, peerResources map[string]*pb.ResourceChange, report *DriftReport) {
	// Build local resource hashes
	localHashes := make(map[string]string)
	m.server.resources.Range(func(key, value interface{}) bool {
		keyStr, ok := key.(string)
		if !ok {
			return true
		}
		resource, ok := value.(*TrackedResource)
		if !ok {
			return true
		}
		localHashes[keyStr] = resource.Hash
		return true
	})

	applied := 0

	// Find resources that we're missing or that differ
	for keyStr, peerChange := range peerResources {
		if applied >= m.config.BatchSize {
			break
		}

		localHash, localHas := localHashes[keyStr]
		if !localHas {
			// We're missing this resource - pull it
			report.MissingLocally = append(report.MissingLocally, keyStr)
			if err := m.server.handleResourceChange(m.ctx, peerName, peerChange); err != nil {
				m.logger.Error("Failed to apply peer resource during reconciliation",
					zap.String("peer", peerName),
					zap.String("key", keyStr),
					zap.Error(err),
				)
			} else {
				applied++
			}
		} else if localHash != peerChange.ResourceHash {
			// Resource exists but differs
			report.HashMismatches = append(report.HashMismatches, keyStr)
			// Let handleResourceChange deal with conflict resolution
			if err := m.server.handleResourceChange(m.ctx, peerName, peerChange); err != nil {
				m.logger.Error("Failed to apply mismatched resource during reconciliation",
					zap.String("peer", peerName),
					zap.String("key", keyStr),
					zap.Error(err),
				)
			} else {
				applied++
			}
		}
	}

	// In push or bidirectional mode, also push resources the peer is missing
	if m.config.RepairMode == RepairModePush || m.config.RepairMode == RepairModeBidirectional {
		client, ok := m.getPeerClient(peerName)
		if !ok {
			return
		}

		pushed := 0
		peerKeys := make(map[string]bool, len(peerResources))
		for k := range peerResources {
			peerKeys[k] = true
		}

		m.server.resources.Range(func(key, value interface{}) bool {
			if pushed+applied >= m.config.BatchSize {
				return false
			}

			keyStr, ok := key.(string)
			if !ok {
				return true
			}

			if peerKeys[keyStr] {
				return true // Peer already has it
			}

			resource, ok := value.(*TrackedResource)
			if !ok {
				return true
			}

			report.MissingRemotely = append(report.MissingRemotely, keyStr)
			if err := m.pushResourceToPeer(client, resource); err != nil {
				m.logger.Error("Failed to push resource to peer during reconciliation",
					zap.String("peer", peerName),
					zap.String("key", keyStr),
					zap.Error(err),
				)
			} else {
				pushed++
			}

			return true
		})
	}
}

// pushResourceToPeer sends a local tracked resource to a peer via SendChange
func (m *AntiEntropyManager) pushResourceToPeer(client *PeerClient, resource *TrackedResource) error {
	change := &pb.ResourceChange{
		ChangeId:        uuid.New().String(),
		VectorClock:     resource.VectorClock,
		ChangeType:      pb.ChangeType_UPDATED,
		ResourceType:    resource.Key.Kind,
		Namespace:       resource.Key.Namespace,
		Name:            resource.Key.Name,
		ResourceVersion: resource.ResourceVersion,
		ResourceData:    resource.Data,
		ResourceHash:    resource.Hash,
		Timestamp:       resource.LastModified.UnixNano(),
		OriginMember:    resource.OriginMember,
		Labels:          resource.Labels,
	}

	return client.SendChange(change)
}

// UpdateResource updates a resource in the local merkle tree
func (m *AntiEntropyManager) UpdateResource(key, hash string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.localTree.Update(key, hash)
}

// RemoveResource removes a resource from the local merkle tree
func (m *AntiEntropyManager) RemoveResource(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.localTree.Remove(key)
}

// GetLocalTreeRoot returns the root hash of the local merkle tree
func (m *AntiEntropyManager) GetLocalTreeRoot() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.localTree.GetRoot()
}

// DriftReport contains information about detected drift
type DriftReport struct {
	// Peer is the peer we compared with
	Peer string

	// Timestamp is when the comparison was done
	Timestamp time.Time

	// DifferingKeys are the keys that differ
	DifferingKeys []string

	// MissingLocally are keys the peer has that we don't
	MissingLocally []string

	// MissingRemotely are keys we have that the peer doesn't
	MissingRemotely []string

	// HashMismatches are keys where both have the resource but hashes differ
	HashMismatches []string
}

// GetDriftReports returns the most recent drift reports from the last anti-entropy cycle
func (m *AntiEntropyManager) GetDriftReports() []*DriftReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.driftReports == nil {
		return nil
	}

	// Return a copy to avoid data races
	reports := make([]*DriftReport, len(m.driftReports))
	copy(reports, m.driftReports)
	return reports
}

// GetDriftReportForPeer returns the latest drift report for a specific peer, or nil
func (m *AntiEntropyManager) GetDriftReportForPeer(peerName string) *DriftReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, report := range m.driftReports {
		if report.Peer == peerName {
			return report
		}
	}
	return nil
}

// TotalDrift returns the total number of differing keys across all peers
func (m *AntiEntropyManager) TotalDrift() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := 0
	for _, report := range m.driftReports {
		total += len(report.DifferingKeys)
	}
	return total
}

// FormatDriftSummary returns a summary string useful for logging
func (m *AntiEntropyManager) FormatDriftSummary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.driftReports) == 0 {
		return "no drift reports"
	}

	total := 0
	for _, report := range m.driftReports {
		total += len(report.DifferingKeys)
	}

	return fmt.Sprintf("%d peers checked, %d total differences", len(m.driftReports), total)
}
