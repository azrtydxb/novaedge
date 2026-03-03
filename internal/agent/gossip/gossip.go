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

// Package gossip implements agent-to-agent config version gossip using
// UDP multicast. When a quorum of peers report a different (newer) config
// version, the lagging agent forces a resync from the controller.
package gossip

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	// GossipPort is the UDP multicast port for config version gossip.
	// Distinct from VIP coordination port (9477).
	GossipPort = 9478

	// MulticastAddr is the multicast group used for gossip.
	MulticastAddr = "224.0.0.100"

	// BroadcastInterval is how often to announce our config version.
	BroadcastInterval = 5 * time.Second

	// QuorumCheckInterval is how often to check if we're behind quorum.
	QuorumCheckInterval = 5 * time.Second

	// PeerExpiry is how long before a silent peer is removed.
	// Set to 3× BroadcastInterval.
	PeerExpiry = 15 * time.Second

	// messagePrefix identifies config gossip messages.
	messagePrefix = "config_version"
)

// peerState tracks the last known config version and heartbeat of a peer.
type peerState struct {
	version  string
	lastSeen time.Time
}

// ConfigGossiper manages UDP multicast gossip for config version consensus.
type ConfigGossiper struct {
	nodeName        string
	multicastAddr   string
	conn            *net.UDPConn
	currentVersion  atomic.Value // string
	peerVersions    sync.Map     // map[string]peerState
	forceResyncFunc func()
	logger          *zap.Logger
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewConfigGossiper creates a new config version gossiper.
// forceResyncFunc is called when quorum detects this agent is behind.
func NewConfigGossiper(nodeName string, forceResyncFunc func(), logger *zap.Logger) *ConfigGossiper {
	g := &ConfigGossiper{
		nodeName:        nodeName,
		multicastAddr:   fmt.Sprintf("%s:%d", MulticastAddr, GossipPort),
		forceResyncFunc: forceResyncFunc,
		logger:          logger.Named("gossip"),
	}
	g.currentVersion.Store("")
	return g
}

// Start begins the gossip protocol. It launches three goroutines:
// broadcastLoop, receiveLoop, and quorumCheckLoop.
func (g *ConfigGossiper) Start(ctx context.Context) error {
	g.ctx, g.cancel = context.WithCancel(ctx)

	addr, err := net.ResolveUDPAddr("udp4", g.multicastAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve multicast address %s: %w", g.multicastAddr, err)
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to create multicast socket: %w", err)
	}
	g.conn = conn

	go g.broadcastLoop(addr)
	go g.receiveLoop()
	go g.quorumCheckLoop()

	g.logger.Info("Config gossip started",
		zap.String("node", g.nodeName),
		zap.String("multicast", g.multicastAddr),
	)
	return nil
}

// UpdateVersion updates the current config version. Called by the watcher
// after successfully applying a new config snapshot.
func (g *ConfigGossiper) UpdateVersion(version string) {
	g.currentVersion.Store(version)
	g.logger.Debug("Gossip version updated", zap.String("version", version))
}

// broadcastLoop multicasts this node's config version at regular intervals.
func (g *ConfigGossiper) broadcastLoop(addr *net.UDPAddr) {
	ticker := time.NewTicker(BroadcastInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			ver, _ := g.currentVersion.Load().(string)
			if ver == "" {
				continue // no config applied yet
			}

			msg := fmt.Sprintf("%s|%s|%s|%d",
				messagePrefix, g.nodeName, ver, time.Now().UnixNano())

			if _, err := g.conn.WriteToUDP([]byte(msg), addr); err != nil {
				g.logger.Debug("Failed to broadcast config version", zap.Error(err))
			}
		}
	}
}

// receiveLoop listens for peer config version announcements.
func (g *ConfigGossiper) receiveLoop() {
	buf := make([]byte, 512)

	for {
		select {
		case <-g.ctx.Done():
			return
		default:
		}

		if err := g.conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			g.logger.Debug("Failed to set read deadline", zap.Error(err))
			continue
		}

		n, _, err := g.conn.ReadFromUDP(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			// Check if context is done (socket closed during shutdown)
			select {
			case <-g.ctx.Done():
				return
			default:
			}
			g.logger.Debug("Error reading gossip message", zap.Error(err))
			continue
		}

		g.handleMessage(string(buf[:n]))
	}
}

// handleMessage parses and processes a received gossip message.
// Format: config_version|<nodeName>|<configVersion>|<timestamp>
func (g *ConfigGossiper) handleMessage(data string) {
	parts := strings.SplitN(data, "|", 4)
	if len(parts) != 4 || parts[0] != messagePrefix {
		return
	}

	peerName := parts[1]
	peerVersion := parts[2]

	// Ignore our own messages
	if peerName == g.nodeName {
		return
	}

	g.peerVersions.Store(peerName, peerState{
		version:  peerVersion,
		lastSeen: time.Now(),
	})
}

// quorumCheckLoop periodically checks if a quorum of peers have a different
// config version, indicating this agent is lagging.
func (g *ConfigGossiper) quorumCheckLoop() {
	ticker := time.NewTicker(QuorumCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.checkQuorum()
		}
	}
}

// checkQuorum determines if a majority of known peers have a different
// (presumably newer) config version, and forces a resync if so.
func (g *ConfigGossiper) checkQuorum() {
	myVersion, _ := g.currentVersion.Load().(string)
	if myVersion == "" {
		return // no config applied yet, nothing to compare
	}

	total := 0
	newerCount := 0
	now := time.Now()

	g.peerVersions.Range(func(key, val any) bool {
		peer, ok := val.(peerState)
		if !ok {
			return true
		}

		// Expire peers not seen in PeerExpiry
		if now.Sub(peer.lastSeen) > PeerExpiry {
			g.peerVersions.Delete(key)
			return true
		}

		total++
		if peer.version != myVersion {
			newerCount++
		}
		return true
	})

	// Quorum: majority of known peers have a different (newer) version
	if total > 0 && newerCount > total/2 {
		g.logger.Warn("Config version behind quorum, forcing resync",
			zap.String("myVersion", myVersion),
			zap.Int("peers", total),
			zap.Int("newerPeers", newerCount),
		)
		g.forceResyncFunc()
	}
}
