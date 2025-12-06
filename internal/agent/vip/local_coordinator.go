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

package vip

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// LocalCoordinationPort is the UDP port used for local VIP coordination
	LocalCoordinationPort = 9477

	// HeartbeatInterval is how often to send heartbeats
	HeartbeatInterval = 1 * time.Second

	// ElectionTimeout is how long to wait before assuming leadership
	ElectionTimeout = 3 * time.Second

	// Priority is used for election (lower wins)
	// In production, this would be derived from node name hash
	DefaultPriority = 100
)

// LocalCoordinator provides local VIP coordination between agents
// when the controller is unavailable (autonomous mode)
type LocalCoordinator struct {
	// Configuration
	nodeName    string
	priority    int32
	vips        map[string]*CoordinatedVIPState // VIP address -> state
	vipsMu      sync.RWMutex
	logger      *zap.Logger

	// Network
	conn     *net.UDPConn
	peerAddr *net.UDPAddr

	// State
	ctx    context.Context
	cancel context.CancelFunc
	active bool
	mu     sync.RWMutex

	// Callbacks
	onBecomeLeader   func(vip string)
	onBecomeFollower func(vip string)
}

// CoordinatedVIPState tracks the state of a VIP in local coordination
type CoordinatedVIPState struct {
	// VIP address
	Address string

	// Current leader (node name)
	Leader string

	// Leader priority
	LeaderPriority int32

	// Last heartbeat from leader
	LastHeartbeat time.Time

	// Are we the leader?
	IsLeader bool

	// VIP mode (L2ARP, BGP, OSPF)
	Mode string
}

// CoordinationMessage is sent between agents for VIP coordination
type CoordinationMessage struct {
	// Type of message
	Type MessageType `json:"type"`

	// VIP address
	VIPAddress string `json:"vipAddress"`

	// Sender node name
	NodeName string `json:"nodeName"`

	// Sender priority
	Priority int32 `json:"priority"`

	// Timestamp
	Timestamp int64 `json:"timestamp"`
}

// MessageType defines the type of coordination message
type MessageType string

const (
	// MessageTypeHeartbeat is sent by the leader
	MessageTypeHeartbeat MessageType = "heartbeat"

	// MessageTypeElection is sent to start an election
	MessageTypeElection MessageType = "election"

	// MessageTypeVictory is sent when winning election
	MessageTypeVictory MessageType = "victory"

	// MessageTypeYield is sent when yielding to a higher priority node
	MessageTypeYield MessageType = "yield"
)

// NewLocalCoordinator creates a new local VIP coordinator
func NewLocalCoordinator(nodeName string, priority int32, logger *zap.Logger) *LocalCoordinator {
	if priority == 0 {
		priority = DefaultPriority
	}

	return &LocalCoordinator{
		nodeName: nodeName,
		priority: priority,
		vips:     make(map[string]*CoordinatedVIPState),
		logger:   logger.Named("local-coord"),
	}
}

// Start begins local VIP coordination
func (c *LocalCoordinator) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.active {
		c.mu.Unlock()
		return nil
	}
	c.active = true
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	c.logger.Info("Starting local VIP coordination",
		zap.String("node", c.nodeName),
		zap.Int32("priority", c.priority),
	)

	// Create UDP socket for multicast
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("224.0.0.100:%d", LocalCoordinationPort))
	if err != nil {
		return fmt.Errorf("failed to resolve multicast address: %w", err)
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to create multicast socket: %w", err)
	}
	c.conn = conn
	c.peerAddr = addr

	// Start receiver
	go c.receiveLoop()

	// Start heartbeat sender
	go c.heartbeatLoop()

	// Start election checker
	go c.electionLoop()

	return nil
}

// Stop stops local VIP coordination
func (c *LocalCoordinator) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.active {
		return
	}

	c.active = false
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		c.conn.Close()
	}

	c.logger.Info("Stopped local VIP coordination")
}

// AddVIP adds a VIP to coordinate
func (c *LocalCoordinator) AddVIP(vipAddress, mode string) {
	c.vipsMu.Lock()
	defer c.vipsMu.Unlock()

	if _, exists := c.vips[vipAddress]; !exists {
		c.vips[vipAddress] = &CoordinatedVIPState{
			Address: vipAddress,
			Mode:    mode,
		}
		c.logger.Info("Added VIP for local coordination", zap.String("vip", vipAddress))
	}
}

// RemoveVIP removes a VIP from coordination
func (c *LocalCoordinator) RemoveVIP(vipAddress string) {
	c.vipsMu.Lock()
	defer c.vipsMu.Unlock()

	delete(c.vips, vipAddress)
	c.logger.Info("Removed VIP from local coordination", zap.String("vip", vipAddress))
}

// IsLeader returns true if we're the leader for the given VIP
func (c *LocalCoordinator) IsLeader(vipAddress string) bool {
	c.vipsMu.RLock()
	defer c.vipsMu.RUnlock()

	if state, ok := c.vips[vipAddress]; ok {
		return state.IsLeader
	}
	return false
}

// GetLeader returns the current leader for a VIP
func (c *LocalCoordinator) GetLeader(vipAddress string) string {
	c.vipsMu.RLock()
	defer c.vipsMu.RUnlock()

	if state, ok := c.vips[vipAddress]; ok {
		return state.Leader
	}
	return ""
}

// OnBecomeLeader sets the callback for when we become leader
func (c *LocalCoordinator) OnBecomeLeader(fn func(vip string)) {
	c.onBecomeLeader = fn
}

// OnBecomeFollower sets the callback for when we become follower
func (c *LocalCoordinator) OnBecomeFollower(fn func(vip string)) {
	c.onBecomeFollower = fn
}

// receiveLoop receives coordination messages from peers
func (c *LocalCoordinator) receiveLoop() {
	buf := make([]byte, 1024)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			c.logger.Error("Error reading from UDP", zap.Error(err))
			continue
		}

		c.handleMessage(buf[:n])
	}
}

// handleMessage processes a received coordination message
func (c *LocalCoordinator) handleMessage(data []byte) {
	// Parse message (simplified - in production use proper serialization)
	// For now, assume format: type|vip|nodeName|priority|timestamp
	var msg CoordinationMessage
	// Simple parsing - in production use JSON or protobuf
	if _, err := fmt.Sscanf(string(data), "%s|%s|%s|%d|%d",
		&msg.Type, &msg.VIPAddress, &msg.NodeName, &msg.Priority, &msg.Timestamp); err != nil {
		return
	}

	// Ignore our own messages
	if msg.NodeName == c.nodeName {
		return
	}

	c.vipsMu.Lock()
	defer c.vipsMu.Unlock()

	state, ok := c.vips[msg.VIPAddress]
	if !ok {
		return // Not tracking this VIP
	}

	switch msg.Type {
	case MessageTypeHeartbeat, MessageTypeVictory:
		// Another node claims leadership
		if msg.Priority < c.priority || (msg.Priority == c.priority && msg.NodeName < c.nodeName) {
			// They have higher priority
			if state.IsLeader {
				state.IsLeader = false
				c.logger.Info("Yielding VIP leadership",
					zap.String("vip", msg.VIPAddress),
					zap.String("to", msg.NodeName),
				)
				if c.onBecomeFollower != nil {
					go c.onBecomeFollower(msg.VIPAddress)
				}
			}
			state.Leader = msg.NodeName
			state.LeaderPriority = msg.Priority
			state.LastHeartbeat = time.Now()
		}

	case MessageTypeElection:
		// Election in progress
		if c.priority < msg.Priority || (c.priority == msg.Priority && c.nodeName < msg.NodeName) {
			// We have higher priority - claim leadership
			c.sendMessage(msg.VIPAddress, MessageTypeVictory)
		}

	case MessageTypeYield:
		// Node is yielding
		if state.Leader == msg.NodeName {
			// Start new election
			c.sendMessage(msg.VIPAddress, MessageTypeElection)
		}
	}
}

// heartbeatLoop sends periodic heartbeats for VIPs we lead
func (c *LocalCoordinator) heartbeatLoop() {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.vipsMu.RLock()
			for vip, state := range c.vips {
				if state.IsLeader {
					c.sendMessage(vip, MessageTypeHeartbeat)
				}
			}
			c.vipsMu.RUnlock()
		}
	}
}

// electionLoop checks for leader timeout and starts elections
func (c *LocalCoordinator) electionLoop() {
	ticker := time.NewTicker(ElectionTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.checkElections()
		}
	}
}

// checkElections checks if we should start elections for any VIPs
func (c *LocalCoordinator) checkElections() {
	c.vipsMu.Lock()
	defer c.vipsMu.Unlock()

	now := time.Now()

	for vip, state := range c.vips {
		if state.IsLeader {
			continue // We're already the leader
		}

		// Check if leader has timed out
		if state.Leader == "" || now.Sub(state.LastHeartbeat) > ElectionTimeout {
			// No leader or leader timed out - claim leadership
			state.IsLeader = true
			state.Leader = c.nodeName
			state.LeaderPriority = c.priority

			c.logger.Info("Becoming VIP leader",
				zap.String("vip", vip),
				zap.String("previous", state.Leader),
			)

			c.sendMessage(vip, MessageTypeVictory)

			if c.onBecomeLeader != nil {
				go c.onBecomeLeader(vip)
			}
		}
	}
}

// sendMessage sends a coordination message
func (c *LocalCoordinator) sendMessage(vip string, msgType MessageType) {
	msg := fmt.Sprintf("%s|%s|%s|%d|%d",
		msgType, vip, c.nodeName, c.priority, time.Now().UnixNano())

	if c.conn != nil && c.peerAddr != nil {
		c.conn.WriteToUDP([]byte(msg), c.peerAddr)
	}
}

// GetStatus returns the status of all coordinated VIPs
func (c *LocalCoordinator) GetStatus() map[string]CoordinatedVIPState {
	c.vipsMu.RLock()
	defer c.vipsMu.RUnlock()

	result := make(map[string]CoordinatedVIPState)
	for k, v := range c.vips {
		result[k] = *v
	}
	return result
}
