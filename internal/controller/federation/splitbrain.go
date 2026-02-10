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
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// DefaultPartitionTimeout is how long without peer contact before declaring partition
	DefaultPartitionTimeout = 30 * time.Second

	// DefaultQuorumCheckInterval is how often to check quorum
	DefaultQuorumCheckInterval = 10 * time.Second

	// DefaultHealingGracePeriod is how long to wait after partition heals before accepting writes
	DefaultHealingGracePeriod = 5 * time.Second
)

// QuorumMode defines how quorum is calculated
type QuorumMode string

const (
	// QuorumModeControllers uses only controller-to-controller connectivity
	// Requires 3+ controllers for effective split-brain prevention
	QuorumModeControllers QuorumMode = "Controllers"

	// QuorumModeAgentAssisted uses agent reachability as additional quorum participants
	// Allows split-brain prevention with only 2 controllers by using agents as witnesses
	QuorumModeAgentAssisted QuorumMode = "AgentAssisted"
)

// SplitBrainConfig configures split-brain detection and handling
type SplitBrainConfig struct {
	// PartitionTimeout is how long without peer contact before declaring partition
	PartitionTimeout time.Duration

	// QuorumRequired requires a quorum of peers for write operations
	// If true, writes are rejected when we don't have quorum
	QuorumRequired bool

	// QuorumSize is the minimum number of reachable peers for quorum
	// Default is (N/2)+1 where N is total federation size
	// In AgentAssisted mode, this is the minimum agents that must be reachable
	QuorumSize int

	// HealingGracePeriod is how long to wait after partition heals
	// before fully accepting writes (to allow state reconciliation)
	HealingGracePeriod time.Duration

	// AutoResolveOnHeal automatically resolves conflicts when partition heals
	AutoResolveOnHeal bool

	// FencingEnabled enables write fencing during detected partition
	FencingEnabled bool

	// QuorumMode determines how quorum is calculated
	// - Controllers: Traditional controller-only quorum (requires 3+ controllers)
	// - AgentAssisted: Uses agent reachability for quorum (works with 2 controllers)
	QuorumMode QuorumMode

	// AgentQuorumWeight is the weight of each agent in quorum calculation
	// Only used in AgentAssisted mode. Default is 1.
	// Higher values give agents more voting power.
	AgentQuorumWeight int

	// ControllerQuorumWeight is the weight of each controller in quorum calculation
	// Only used in AgentAssisted mode. Default is 10.
	// Controllers typically have higher weight than individual agents.
	ControllerQuorumWeight int

	// MinAgentsForQuorum is the minimum number of agents required for quorum
	// Only used in AgentAssisted mode. Default is 1.
	// Prevents a controller with zero agents from claiming quorum.
	MinAgentsForQuorum int
}

// DefaultSplitBrainConfig returns sensible defaults
func DefaultSplitBrainConfig() *SplitBrainConfig {
	return &SplitBrainConfig{
		PartitionTimeout:       DefaultPartitionTimeout,
		QuorumRequired:         false, // Availability over consistency by default
		QuorumSize:             0,     // Calculated dynamically
		HealingGracePeriod:     DefaultHealingGracePeriod,
		AutoResolveOnHeal:      true,
		FencingEnabled:         false,
		QuorumMode:             QuorumModeControllers,
		AgentQuorumWeight:      1,
		ControllerQuorumWeight: 10,
		MinAgentsForQuorum:     1,
	}
}

// AgentAssistedSplitBrainConfig returns config optimized for 2-controller deployments
func AgentAssistedSplitBrainConfig() *SplitBrainConfig {
	return &SplitBrainConfig{
		PartitionTimeout:       DefaultPartitionTimeout,
		QuorumRequired:         true,
		QuorumSize:             0, // Will be calculated based on total agents
		HealingGracePeriod:     DefaultHealingGracePeriod,
		AutoResolveOnHeal:      true,
		FencingEnabled:         true,
		QuorumMode:             QuorumModeAgentAssisted,
		AgentQuorumWeight:      1,
		ControllerQuorumWeight: 10,
		MinAgentsForQuorum:     1,
	}
}

// PartitionState represents the current partition state
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

// PartitionInfo contains information about a detected partition
type PartitionInfo struct {
	// State is the current partition state
	State PartitionState

	// DetectedAt is when the partition was first detected
	DetectedAt time.Time

	// HealedAt is when the partition healed (if healing/healed)
	HealedAt time.Time

	// ReachablePeers are peers we can still reach
	ReachablePeers []string

	// UnreachablePeers are peers we cannot reach
	UnreachablePeers []string

	// HaveQuorum indicates if we have quorum
	HaveQuorum bool

	// WritesFenced indicates if writes are being rejected
	WritesFenced bool

	// PendingConflicts is the number of conflicts to resolve
	PendingConflicts int

	// AgentQuorumInfo contains agent-based quorum information (AgentAssisted mode only)
	AgentQuorumInfo *AgentQuorumInfo
}

// AgentQuorumInfo contains information about agent-based quorum
type AgentQuorumInfo struct {
	// TotalAgents is the total number of known agents across all controllers
	TotalAgents int

	// ReachableAgents is the number of agents this controller can reach
	ReachableAgents int

	// QuorumThreshold is the minimum agents needed for quorum
	QuorumThreshold int

	// OurVotes is our calculated vote count (agents + controller weight)
	OurVotes int

	// TotalVotes is the total possible votes
	TotalVotes int

	// PeerAgentCounts maps peer controller names to their connected agent counts
	PeerAgentCounts map[string]int

	// AgentsByController maps controller names to lists of agent names
	AgentsByController map[string][]string
}

// AgentReachabilityReport is sent by agents to report which controllers they can reach
type AgentReachabilityReport struct {
	// AgentName is the unique identifier for this agent
	AgentName string

	// NodeName is the Kubernetes node name
	NodeName string

	// ClusterName is the cluster this agent belongs to
	ClusterName string

	// ReachableControllers is the list of controller names this agent can reach
	ReachableControllers []string

	// ControllerLatencies maps controller names to round-trip latency
	ControllerLatencies map[string]time.Duration

	// Timestamp is when this report was generated
	Timestamp time.Time
}

// SplitBrainDetector handles split-brain detection and mitigation
type SplitBrainDetector struct {
	config *SplitBrainConfig
	logger *zap.Logger

	// Server reference
	server *Server

	// Current state
	state         PartitionState
	partitionInfo *PartitionInfo
	stateMu       sync.RWMutex

	// Quorum tracking (Controllers mode)
	totalPeers     int
	reachablePeers map[string]time.Time
	peersMu        sync.RWMutex

	// Agent quorum tracking (AgentAssisted mode)
	agentReports   map[string]*AgentReachabilityReport // agentName -> report
	agentsMu       sync.RWMutex
	peerAgentCount map[string]int32 // peerName -> agentCount from heartbeats
	peerAgentMu    sync.RWMutex

	// Write fencing
	writesFenced bool
	fenceMu      sync.RWMutex

	// Context
	ctx    context.Context
	cancel context.CancelFunc

	// Callbacks
	onPartitionDetected func(*PartitionInfo)
	onPartitionHealed   func(*PartitionInfo)
	onQuorumLost        func()
	onQuorumRestored    func()
}

// NewSplitBrainDetector creates a new split-brain detector
func NewSplitBrainDetector(config *SplitBrainConfig, server *Server, totalPeers int, logger *zap.Logger) *SplitBrainDetector {
	if config == nil {
		config = DefaultSplitBrainConfig()
	}

	// Calculate quorum size if not specified
	if config.QuorumSize <= 0 {
		if config.QuorumMode == QuorumModeAgentAssisted {
			// For agent-assisted mode, quorum will be calculated dynamically based on agents
			config.QuorumSize = 0
		} else {
			config.QuorumSize = (totalPeers / 2) + 1
		}
	}

	return &SplitBrainDetector{
		config:         config,
		server:         server,
		logger:         logger.Named("split-brain"),
		state:          PartitionStateHealthy,
		totalPeers:     totalPeers,
		reachablePeers: make(map[string]time.Time),
		agentReports:   make(map[string]*AgentReachabilityReport),
		peerAgentCount: make(map[string]int32),
	}
}

// Start begins split-brain detection
func (d *SplitBrainDetector) Start(ctx context.Context) {
	d.ctx, d.cancel = context.WithCancel(ctx)

	d.logger.Info("Starting split-brain detector",
		zap.Duration("partition_timeout", d.config.PartitionTimeout),
		zap.Bool("quorum_required", d.config.QuorumRequired),
		zap.Int("quorum_size", d.config.QuorumSize),
	)

	go d.runDetectionLoop()
}

// Stop stops split-brain detection
func (d *SplitBrainDetector) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
}

// RecordPeerContact records contact with a peer
func (d *SplitBrainDetector) RecordPeerContact(peerName string) {
	d.peersMu.Lock()
	d.reachablePeers[peerName] = time.Now()
	d.peersMu.Unlock()

	// Check if this heals a partition
	d.checkPartitionHealing()
}

// RecordPeerFailure records a failure to contact a peer
func (d *SplitBrainDetector) RecordPeerFailure(peerName string) {
	// No state mutation needed: we intentionally do not remove the peer entry,
	// we just skip updating the timestamp so it will age out naturally.

	// Check for partition
	d.checkForPartition()
}

// GetState returns the current partition state
func (d *SplitBrainDetector) GetState() PartitionState {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return d.state
}

// GetPartitionInfo returns information about the current partition
func (d *SplitBrainDetector) GetPartitionInfo() *PartitionInfo {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()

	if d.partitionInfo != nil {
		info := *d.partitionInfo
		return &info
	}
	return nil
}

// HaveQuorum returns true if we have quorum
func (d *SplitBrainDetector) HaveQuorum() bool {
	d.peersMu.RLock()
	defer d.peersMu.RUnlock()

	reachable := d.countReachablePeers()
	// Include ourselves in the count
	return (reachable + 1) >= d.config.QuorumSize
}

// AreWritesFenced returns true if writes are currently fenced
func (d *SplitBrainDetector) AreWritesFenced() bool {
	d.fenceMu.RLock()
	defer d.fenceMu.RUnlock()
	return d.writesFenced
}

// CanAcceptWrite returns true if we can accept a write operation
func (d *SplitBrainDetector) CanAcceptWrite() bool {
	// If fencing is enabled and writes are fenced, reject
	if d.config.FencingEnabled && d.AreWritesFenced() {
		return false
	}

	// If quorum is required and we don't have it, reject
	if d.config.QuorumRequired && !d.HaveQuorum() {
		return false
	}

	return true
}

// OnPartitionDetected sets the callback for partition detection
func (d *SplitBrainDetector) OnPartitionDetected(fn func(*PartitionInfo)) {
	d.onPartitionDetected = fn
}

// OnPartitionHealed sets the callback for partition healing
func (d *SplitBrainDetector) OnPartitionHealed(fn func(*PartitionInfo)) {
	d.onPartitionHealed = fn
}

// OnQuorumLost sets the callback for quorum loss
func (d *SplitBrainDetector) OnQuorumLost(fn func()) {
	d.onQuorumLost = fn
}

// OnQuorumRestored sets the callback for quorum restoration
func (d *SplitBrainDetector) OnQuorumRestored(fn func()) {
	d.onQuorumRestored = fn
}

// runDetectionLoop runs the periodic partition detection
func (d *SplitBrainDetector) runDetectionLoop() {
	ticker := time.NewTicker(DefaultQuorumCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.checkForPartition()
			d.checkQuorum()
		}
	}
}

// checkForPartition checks if we're in a network partition
func (d *SplitBrainDetector) checkForPartition() {
	d.peersMu.RLock()
	reachable := d.getReachablePeerNames()
	unreachable := d.getUnreachablePeerNames()
	d.peersMu.RUnlock()

	d.stateMu.Lock()
	defer d.stateMu.Unlock()

	oldState := d.state

	if len(unreachable) == 0 {
		// All peers reachable
		if d.state != PartitionStateHealthy {
			d.transitionToHealthy()
		}
		return
	}

	if len(unreachable) > 0 && len(reachable) > 0 {
		// Some peers unreachable - suspected partition
		switch d.state {
		case PartitionStateHealthy:
			d.state = PartitionStateSuspected
			d.partitionInfo = &PartitionInfo{
				State:            PartitionStateSuspected,
				DetectedAt:       time.Now(),
				ReachablePeers:   reachable,
				UnreachablePeers: unreachable,
				HaveQuorum:       d.HaveQuorum(),
			}
			d.logger.Warn("Partition suspected",
				zap.Strings("unreachable", unreachable),
				zap.Strings("reachable", reachable),
			)
		case PartitionStateSuspected:
			// Check if suspected long enough to confirm
			if d.partitionInfo != nil && time.Since(d.partitionInfo.DetectedAt) > d.config.PartitionTimeout {
				d.state = PartitionStateConfirmed
				d.partitionInfo.State = PartitionStateConfirmed
				d.logger.Error("Partition confirmed",
					zap.Strings("unreachable", unreachable),
					zap.Duration("duration", time.Since(d.partitionInfo.DetectedAt)),
				)

				// Enable fencing if configured
				if d.config.FencingEnabled {
					d.fenceMu.Lock()
					d.writesFenced = true
					d.partitionInfo.WritesFenced = true
					d.fenceMu.Unlock()
				}

				// Notify callback
				if d.onPartitionDetected != nil {
					go d.onPartitionDetected(d.partitionInfo)
				}
			}
		}
	}

	if len(reachable) == 0 && d.totalPeers > 0 {
		// No peers reachable
		if d.state != PartitionStateConfirmed {
			d.state = PartitionStateConfirmed
			d.partitionInfo = &PartitionInfo{
				State:            PartitionStateConfirmed,
				DetectedAt:       time.Now(),
				ReachablePeers:   nil,
				UnreachablePeers: unreachable,
				HaveQuorum:       false,
			}
			d.logger.Error("Complete partition - no peers reachable")

			if d.config.FencingEnabled {
				d.fenceMu.Lock()
				d.writesFenced = true
				d.partitionInfo.WritesFenced = true
				d.fenceMu.Unlock()
			}

			if d.onPartitionDetected != nil {
				go d.onPartitionDetected(d.partitionInfo)
			}
		}
	}

	// Update metrics
	if oldState != d.state {
		FederationPhaseGauge.WithLabelValues(string(d.state)).Set(1)
		for _, phase := range []string{string(PartitionStateHealthy), string(PartitionStateSuspected), string(PartitionStateConfirmed), string(PartitionStateHealing)} {
			if phase != string(d.state) {
				FederationPhaseGauge.WithLabelValues(phase).Set(0)
			}
		}
	}
}

// checkPartitionHealing checks if partition is healing
func (d *SplitBrainDetector) checkPartitionHealing() {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()

	if d.state != PartitionStateConfirmed && d.state != PartitionStateSuspected {
		return
	}

	d.peersMu.RLock()
	unreachable := d.getUnreachablePeerNames()
	d.peersMu.RUnlock()

	if len(unreachable) == 0 {
		// All peers now reachable - start healing
		d.state = PartitionStateHealing
		if d.partitionInfo != nil {
			d.partitionInfo.State = PartitionStateHealing
			d.partitionInfo.HealedAt = time.Now()
		}

		d.logger.Info("Partition healing - all peers now reachable")

		// Start healing process
		go d.runHealingProcess()
	}
}

// runHealingProcess handles partition healing
func (d *SplitBrainDetector) runHealingProcess() {
	d.logger.Info("Starting partition healing process",
		zap.Duration("grace_period", d.config.HealingGracePeriod),
	)

	// Wait for grace period to allow state reconciliation
	select {
	case <-d.ctx.Done():
		return
	case <-time.After(d.config.HealingGracePeriod):
	}

	// Check if we're still healing (not back in partition)
	d.stateMu.Lock()
	if d.state != PartitionStateHealing {
		d.stateMu.Unlock()
		return
	}

	// Auto-resolve conflicts if configured
	if d.config.AutoResolveOnHeal && d.server != nil {
		conflicts := d.server.GetConflicts()
		if len(conflicts) > 0 {
			d.logger.Info("Auto-resolving conflicts after partition heal",
				zap.Int("conflicts", len(conflicts)),
			)
			for _, conflict := range conflicts {
				// Use last-writer-wins for auto-resolution
				key := conflict.Key.String()
				if err := d.server.ResolveConflict(key, false); err != nil {
					d.logger.Warn("Failed to auto-resolve conflict",
						zap.String("key", key),
						zap.Error(err),
					)
				}
			}
		}
	}

	// Remove fencing
	d.fenceMu.Lock()
	d.writesFenced = false
	d.fenceMu.Unlock()

	// Transition to healthy
	d.transitionToHealthy()
	d.stateMu.Unlock()
}

// transitionToHealthy transitions to healthy state
func (d *SplitBrainDetector) transitionToHealthy() {
	oldInfo := d.partitionInfo

	d.state = PartitionStateHealthy
	d.partitionInfo = nil

	d.fenceMu.Lock()
	d.writesFenced = false
	d.fenceMu.Unlock()

	d.logger.Info("Partition healed - federation healthy")

	if d.onPartitionHealed != nil && oldInfo != nil {
		go d.onPartitionHealed(oldInfo)
	}
}

// checkQuorum checks if we have quorum and handles transitions
func (d *SplitBrainDetector) checkQuorum() {
	hadQuorum := d.HaveQuorum()

	d.peersMu.Lock()
	// Clean up stale peer entries
	cutoff := time.Now().Add(-d.config.PartitionTimeout)
	for peer, lastSeen := range d.reachablePeers {
		if lastSeen.Before(cutoff) {
			delete(d.reachablePeers, peer)
		}
	}
	d.peersMu.Unlock()

	haveQuorum := d.HaveQuorum()

	if hadQuorum && !haveQuorum {
		d.logger.Warn("Quorum lost",
			zap.Int("reachable", d.countReachablePeers()),
			zap.Int("required", d.config.QuorumSize),
		)
		if d.onQuorumLost != nil {
			go d.onQuorumLost()
		}
	} else if !hadQuorum && haveQuorum {
		d.logger.Info("Quorum restored",
			zap.Int("reachable", d.countReachablePeers()),
			zap.Int("required", d.config.QuorumSize),
		)
		if d.onQuorumRestored != nil {
			go d.onQuorumRestored()
		}
	}
}

// countReachablePeers counts peers contacted within timeout
func (d *SplitBrainDetector) countReachablePeers() int {
	cutoff := time.Now().Add(-d.config.PartitionTimeout)
	count := 0
	for _, lastSeen := range d.reachablePeers {
		if lastSeen.After(cutoff) {
			count++
		}
	}
	return count
}

// getReachablePeerNames returns names of reachable peers
func (d *SplitBrainDetector) getReachablePeerNames() []string {
	cutoff := time.Now().Add(-d.config.PartitionTimeout)
	var names []string
	for peer, lastSeen := range d.reachablePeers {
		if lastSeen.After(cutoff) {
			names = append(names, peer)
		}
	}
	return names
}

// getUnreachablePeerNames returns names of unreachable peers
func (d *SplitBrainDetector) getUnreachablePeerNames() []string {
	cutoff := time.Now().Add(-d.config.PartitionTimeout)
	reachable := make(map[string]bool)
	for peer, lastSeen := range d.reachablePeers {
		if lastSeen.After(cutoff) {
			reachable[peer] = true
		}
	}

	// Get all known peers from server
	var unreachable []string
	if d.server != nil {
		d.server.peerStates.Range(func(key, _ interface{}) bool {
			peerName, ok := key.(string)
			if !ok {
				return true
			}
			if !reachable[peerName] {
				unreachable = append(unreachable, peerName)
			}
			return true
		})
	}

	return unreachable
}

// ============================================================================
// Agent-Assisted Quorum Methods
// ============================================================================

// RecordAgentReachability records an agent's controller reachability report
func (d *SplitBrainDetector) RecordAgentReachability(report *AgentReachabilityReport) {
	if d.config.QuorumMode != QuorumModeAgentAssisted {
		return
	}

	d.agentsMu.Lock()
	defer d.agentsMu.Unlock()

	report.Timestamp = time.Now()
	d.agentReports[report.AgentName] = report

	d.logger.Debug("Recorded agent reachability",
		zap.String("agent", report.AgentName),
		zap.Strings("reachableControllers", report.ReachableControllers),
	)
}

// UpdatePeerAgentCount updates the agent count for a peer (from heartbeats)
func (d *SplitBrainDetector) UpdatePeerAgentCount(peerName string, count int32) {
	if d.config.QuorumMode != QuorumModeAgentAssisted {
		return
	}

	d.peerAgentMu.Lock()
	d.peerAgentCount[peerName] = count
	d.peerAgentMu.Unlock()
}

// GetAgentQuorumInfo returns information about agent-based quorum
func (d *SplitBrainDetector) GetAgentQuorumInfo() *AgentQuorumInfo {
	if d.config.QuorumMode != QuorumModeAgentAssisted {
		return nil
	}

	d.agentsMu.RLock()
	d.peerAgentMu.RLock()
	defer d.agentsMu.RUnlock()
	defer d.peerAgentMu.RUnlock()

	// Count agents that can reach us
	ourControllerName := ""
	if d.server != nil && d.server.config.LocalMember != nil {
		ourControllerName = d.server.config.LocalMember.Name
	}

	agentsByController := make(map[string][]string)
	reachableAgents := 0
	cutoff := time.Now().Add(-d.config.PartitionTimeout)

	for agentName, report := range d.agentReports {
		if report.Timestamp.Before(cutoff) {
			continue // Stale report
		}

		for _, controller := range report.ReachableControllers {
			agentsByController[controller] = append(agentsByController[controller], agentName)
			if controller == ourControllerName {
				reachableAgents++
			}
		}
	}

	// Get local agent count from server
	localAgentCount := 0
	if d.server != nil {
		d.server.agentMu.RLock()
		localAgentCount = int(d.server.agentCount)
		d.server.agentMu.RUnlock()
	}

	// Use the higher of reported or connected
	if localAgentCount > reachableAgents {
		reachableAgents = localAgentCount
	}

	// Calculate total agents across all controllers
	totalAgents := reachableAgents
	peerAgentCounts := make(map[string]int)
	for peerName, count := range d.peerAgentCount {
		peerAgentCounts[peerName] = int(count)
		totalAgents += int(count)
	}

	// Calculate votes
	// Our votes = our agents + controller weight (ourselves)
	ourVotes := reachableAgents*d.config.AgentQuorumWeight + d.config.ControllerQuorumWeight

	// Total votes = all agents + all controllers
	totalControllers := 1 + len(d.peerAgentCount) // us + peers
	totalVotes := totalAgents*d.config.AgentQuorumWeight + totalControllers*d.config.ControllerQuorumWeight

	// Quorum threshold is majority of votes
	quorumThreshold := (totalVotes / 2) + 1

	return &AgentQuorumInfo{
		TotalAgents:        totalAgents,
		ReachableAgents:    reachableAgents,
		QuorumThreshold:    quorumThreshold,
		OurVotes:           ourVotes,
		TotalVotes:         totalVotes,
		PeerAgentCounts:    peerAgentCounts,
		AgentsByController: agentsByController,
	}
}

// HaveAgentQuorum returns true if we have quorum based on agent connectivity
func (d *SplitBrainDetector) HaveAgentQuorum() bool {
	if d.config.QuorumMode != QuorumModeAgentAssisted {
		return d.HaveQuorum() // Fall back to controller-based quorum
	}

	info := d.GetAgentQuorumInfo()
	if info == nil {
		return true // No info available, assume we have quorum
	}

	// Check minimum agents requirement
	if info.ReachableAgents < d.config.MinAgentsForQuorum {
		d.logger.Debug("Agent quorum check: insufficient agents",
			zap.Int("reachable", info.ReachableAgents),
			zap.Int("minimum", d.config.MinAgentsForQuorum),
		)
		return false
	}

	// Check vote threshold
	haveQuorum := info.OurVotes >= info.QuorumThreshold

	d.logger.Debug("Agent quorum check",
		zap.Int("our_votes", info.OurVotes),
		zap.Int("threshold", info.QuorumThreshold),
		zap.Int("total_votes", info.TotalVotes),
		zap.Int("reachable_agents", info.ReachableAgents),
		zap.Bool("have_quorum", haveQuorum),
	)

	return haveQuorum
}

// CanAcceptWriteWithAgentQuorum checks if we can accept writes using agent-based quorum
func (d *SplitBrainDetector) CanAcceptWriteWithAgentQuorum() bool {
	// If fencing is enabled and writes are fenced, reject
	if d.config.FencingEnabled && d.AreWritesFenced() {
		return false
	}

	// Check agent quorum if enabled
	if d.config.QuorumMode == QuorumModeAgentAssisted {
		return d.HaveAgentQuorum()
	}

	// Fall back to standard quorum check
	return d.CanAcceptWrite()
}

// CleanupStaleAgentReports removes agent reports that haven't been updated recently
func (d *SplitBrainDetector) CleanupStaleAgentReports() {
	d.agentsMu.Lock()
	defer d.agentsMu.Unlock()

	cutoff := time.Now().Add(-d.config.PartitionTimeout * 2) // 2x partition timeout for agents
	for agentName, report := range d.agentReports {
		if report.Timestamp.Before(cutoff) {
			delete(d.agentReports, agentName)
			d.logger.Debug("Removed stale agent report", zap.String("agent", agentName))
		}
	}
}

// GetAgentReachabilityStats returns statistics about agent reachability
func (d *SplitBrainDetector) GetAgentReachabilityStats() map[string]interface{} {
	info := d.GetAgentQuorumInfo()
	if info == nil {
		return map[string]interface{}{
			"mode":    string(d.config.QuorumMode),
			"enabled": false,
		}
	}

	return map[string]interface{}{
		"mode":             string(d.config.QuorumMode),
		"enabled":          true,
		"totalAgents":      info.TotalAgents,
		"reachableAgents":  info.ReachableAgents,
		"ourVotes":         info.OurVotes,
		"totalVotes":       info.TotalVotes,
		"quorumThreshold":  info.QuorumThreshold,
		"haveQuorum":       d.HaveAgentQuorum(),
		"peerAgentCounts":  info.PeerAgentCounts,
		"controllerWeight": d.config.ControllerQuorumWeight,
		"agentWeight":      d.config.AgentQuorumWeight,
	}
}
