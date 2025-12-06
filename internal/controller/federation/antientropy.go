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
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// DefaultAntiEntropyInterval is how often to run anti-entropy
	DefaultAntiEntropyInterval = 5 * time.Minute

	// MerkleTreeDepth is the depth of the merkle tree for resource comparison
	MerkleTreeDepth = 4

	// MaxDriftResolutionBatch is the max resources to sync per anti-entropy run
	MaxDriftResolutionBatch = 100
)

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
		RepairMode: "bidirectional",
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

	// Children contains child node hashes (for non-leaf nodes)
	Children map[string]string

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

	return t.findNode(t.root, prefix, 0)
}

// findNode recursively finds a node by prefix
func (t *MerkleTree) findNode(node *MerkleNode, prefix string, level int) *MerkleNode {
	if node == nil {
		return nil
	}

	if node.Prefix == prefix {
		return node
	}

	if node.IsLeaf {
		return nil
	}

	// Find the child that matches the prefix
	for childPrefix := range node.Children {
		if len(prefix) >= len(childPrefix) && prefix[:len(childPrefix)] == childPrefix {
			// This child's prefix is a prefix of what we're looking for
			// We need to traverse deeper, but for simplicity return the node
			// if it's a direct match or the closest parent
		}
	}

	return node
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
	node.Children = make(map[string]string)
	childHashes := make([]string, 0, len(buckets))

	for bucket, bucketKeys := range buckets {
		childPrefix := prefix + bucket
		child := t.buildNode(bucketKeys, level+1, childPrefix)
		if child != nil {
			node.Children[childPrefix] = child.Hash
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

	// Context for lifecycle
	ctx    context.Context
	cancel context.CancelFunc

	mu sync.RWMutex
}

// NewAntiEntropyManager creates a new anti-entropy manager
func NewAntiEntropyManager(config *AntiEntropyConfig, server *Server, logger *zap.Logger) *AntiEntropyManager {
	if config == nil {
		config = DefaultAntiEntropyConfig()
	}

	return &AntiEntropyManager{
		config:    config,
		server:    server,
		logger:    logger.Named("anti-entropy"),
		localTree: NewMerkleTree(MerkleTreeDepth),
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
		zap.String("repairMode", m.config.RepairMode),
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

	// Compare with each peer
	for peerName, peerState := range peerStates {
		if !peerState.Healthy || !peerState.Connected {
			continue
		}

		m.compareWithPeer(peerName)
	}
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
		keyStr := key.(string)
		resource := value.(*TrackedResource)

		m.localTree.Update(keyStr, resource.Hash)
		return true
	})
}

// compareWithPeer compares our state with a peer and initiates repair if needed
func (m *AntiEntropyManager) compareWithPeer(peerName string) {
	m.logger.Debug("Comparing state with peer", zap.String("peer", peerName))

	// In a full implementation, we would:
	// 1. Request the peer's merkle tree root
	// 2. If roots differ, request child nodes to find divergence
	// 3. Request specific resources that differ
	// 4. Apply conflict resolution

	// For now, we do a simple hash comparison using vector clocks
	if m.server == nil {
		return
	}

	// Get our vector clock
	ourVC := m.server.vectorClock.ToMap()

	// Get peer's vector clock from state
	peerState, ok := m.server.peerStates.Load(peerName)
	if !ok {
		return
	}

	state := peerState.(*PeerState)
	peerVC := state.VectorClock

	// Compare vector clocks
	ourVCObj := NewVectorClockFromMap(ourVC)

	// peerVC is already a *VectorClock, use it directly or convert from map if nil
	var peerVCObj *VectorClock
	if peerVC != nil {
		peerVCObj = peerVC
	} else {
		peerVCObj = NewVectorClock()
	}

	cmp := ourVCObj.Compare(peerVCObj)

	switch cmp {
	case 0:
		// Concurrent - potential conflicts, need detailed comparison
		m.logger.Debug("Concurrent state with peer, detailed comparison needed",
			zap.String("peer", peerName),
		)
		// TODO: Request full sync or merkle tree comparison

	case 1:
		// We're ahead - peer might need updates
		if m.config.RepairMode == "push" || m.config.RepairMode == "bidirectional" {
			m.logger.Debug("We're ahead of peer, may need to push updates",
				zap.String("peer", peerName),
			)
			// TODO: Push updates to peer
		}

	case -1:
		// Peer is ahead - we might need updates
		if m.config.RepairMode == "pull" || m.config.RepairMode == "bidirectional" {
			m.logger.Debug("Peer is ahead, may need to pull updates",
				zap.String("peer", peerName),
			)
			// TODO: Request full sync from peer
		}
	}
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

// GetDriftReports returns recent drift reports
func (m *AntiEntropyManager) GetDriftReports() []*DriftReport {
	// In a full implementation, this would return cached drift reports
	return nil
}
