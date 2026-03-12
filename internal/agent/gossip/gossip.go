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
// UDP multicast. When a quorum of peers report a newer generation time,
// the lagging agent forces a resync from the controller.
package gossip

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
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

	// GenTimeThreshold is the minimum difference in generation time (seconds)
	// before considering a peer "newer". Per-node snapshots have different
	// VIP assignments, so version hashes will always differ across nodes.
	// Comparing generation times with a threshold avoids false-positive
	// resync loops that cause config churn. (#866)
	GenTimeThreshold int64 = 60

	// ResyncCooldown is how long to wait after a force resync before
	// allowing another one. This gives the agent time to reconnect and
	// receive a new config snapshot before the gossip tears down the
	// stream again. (#866)
	ResyncCooldown = 30 * time.Second

	// messagePrefix identifies config gossip messages.
	messagePrefix = "config_version"

	// maxPeers is the maximum number of peers tracked in the peer table.
	maxPeers = 256
)

// peerState tracks the last known config generation time and heartbeat of a peer.
type peerState struct {
	genTime  int64
	lastSeen time.Time
}

// ConfigGossiper manages UDP multicast gossip for config version consensus.
type ConfigGossiper struct {
	nodeName        string
	multicastAddr   string
	psk             []byte // Pre-shared key for HMAC-SHA256 authentication (nil = disabled)
	conn            *net.UDPConn
	currentGenTime  atomic.Int64
	peerVersions    sync.Map // map[string]peerState
	peerCount       atomic.Int32
	forceResyncFunc func()
	lastResyncTime  atomic.Int64 // Unix nano timestamp of last force resync
	logger          *zap.Logger
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// NewConfigGossiper creates a new config version gossiper.
// forceResyncFunc is called when quorum detects this agent is behind.
// psk is an optional pre-shared key for HMAC-SHA256 message authentication;
// pass nil or empty to disable authentication (backward compatible).
func NewConfigGossiper(nodeName string, forceResyncFunc func(), logger *zap.Logger, psk []byte) *ConfigGossiper {
	return &ConfigGossiper{
		nodeName:        nodeName,
		multicastAddr:   fmt.Sprintf("%s:%d", MulticastAddr, GossipPort),
		psk:             psk,
		forceResyncFunc: forceResyncFunc,
		logger:          logger.Named("gossip"),
	}
}

// Start begins the gossip protocol. It launches three goroutines:
// broadcastLoop, receiveLoop, and quorumCheckLoop.
func (g *ConfigGossiper) Start(ctx context.Context) error {
	childCtx, cancel := context.WithCancel(ctx)
	// cancel is deferred so early-return error paths release the context;
	// on success we nil-out cancel so the deferred call is a no-op, and
	// ownership transfers to g.cancel for later cleanup.
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()

	addr, err := net.ResolveUDPAddr("udp4", g.multicastAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve multicast address %s: %w", g.multicastAddr, err)
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to create multicast socket: %w", err)
	}

	g.ctx = childCtx
	g.cancel = cancel
	cancel = nil // ownership transferred to g.cancel
	g.conn = conn

	g.wg.Add(3)
	go func() { defer g.wg.Done(); g.broadcastLoop(addr) }()
	go func() { defer g.wg.Done(); g.receiveLoop() }()
	go func() { defer g.wg.Done(); g.quorumCheckLoop() }()

	g.logger.Info("Config gossip started",
		zap.String("node", g.nodeName),
		zap.String("multicast", g.multicastAddr),
	)
	return nil
}

// UpdateGenTime updates the current config generation time. Called by the
// watcher after successfully applying a new config snapshot. (#866)
func (g *ConfigGossiper) UpdateGenTime(genTime int64) {
	g.currentGenTime.Store(genTime)
	g.logger.Debug("Gossip generation time updated", zap.Int64("genTime", genTime))
}

// Stop cancels the gossip context, closes the UDP connection, and waits for
// all goroutines to exit cleanly.
func (g *ConfigGossiper) Stop() {
	if g.cancel != nil {
		g.cancel()
	}
	if g.conn != nil {
		_ = g.conn.Close()
	}
	g.wg.Wait()
	g.logger.Info("Config gossip stopped")
}

// broadcastLoop multicasts this node's config generation time at regular intervals.
func (g *ConfigGossiper) broadcastLoop(addr *net.UDPAddr) {
	ticker := time.NewTicker(BroadcastInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			genTime := g.currentGenTime.Load()
			if genTime == 0 {
				continue // no config applied yet
			}

			msg := fmt.Sprintf("%s|%s|%d|%d",
				messagePrefix, g.nodeName, genTime, time.Now().UnixNano())

			if len(g.psk) > 0 {
				mac := hmac.New(sha256.New, g.psk)
				mac.Write([]byte(msg))
				msg = msg + "|" + hex.EncodeToString(mac.Sum(nil))
			}

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
// Format: config_version|<nodeName>|<genTime>|<timestamp>[|<hmac>]
func (g *ConfigGossiper) handleMessage(data string) {
	if len(g.psk) > 0 {
		// Expect HMAC as 5th field
		lastPipe := strings.LastIndex(data, "|")
		if lastPipe < 0 {
			return
		}
		payload := data[:lastPipe]
		receivedMAC, err := hex.DecodeString(data[lastPipe+1:])
		if err != nil {
			return
		}
		mac := hmac.New(sha256.New, g.psk)
		mac.Write([]byte(payload))
		if !hmac.Equal(mac.Sum(nil), receivedMAC) {
			g.logger.Debug("Gossip message HMAC verification failed, dropping")
			return
		}
		data = payload
	}

	parts := strings.SplitN(data, "|", 4)
	if len(parts) != 4 || parts[0] != messagePrefix {
		return
	}

	peerName := parts[1]
	peerGenTime, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return
	}

	// Ignore our own messages
	if peerName == g.nodeName {
		return
	}

	// Cap peer table at maxPeers entries
	if _, loaded := g.peerVersions.Load(peerName); !loaded {
		if g.peerCount.Load() >= maxPeers {
			g.logger.Debug("Peer table full, dropping message",
				zap.String("peer", peerName))
			return
		}
	}

	if _, loaded := g.peerVersions.LoadOrStore(peerName, peerState{
		genTime:  peerGenTime,
		lastSeen: time.Now(),
	}); loaded {
		// Existing peer: just update
		g.peerVersions.Store(peerName, peerState{
			genTime:  peerGenTime,
			lastSeen: time.Now(),
		})
	} else {
		// New peer: increment counter
		g.peerCount.Add(1)
	}
}

// quorumCheckLoop periodically checks if a quorum of peers have a newer
// generation time, indicating this agent is lagging.
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

// checkQuorum determines if a majority of known peers have a significantly
// newer generation time, and forces a resync if so. (#866)
//
// Per-node snapshots differ (VIP assignments vary by node), so version
// hashes will always differ. Instead we compare GenerationTime (set once
// per build cycle, identical for all nodes) with a threshold to avoid
// false-positive resync loops.
func (g *ConfigGossiper) checkQuorum() {
	myGenTime := g.currentGenTime.Load()
	if myGenTime == 0 {
		return // no config applied yet, nothing to compare
	}

	// Cooldown: don't fire another resync if we recently triggered one.
	// This gives the agent time to reconnect and receive a new snapshot. (#866)
	if lastResync := g.lastResyncTime.Load(); lastResync > 0 {
		elapsed := time.Since(time.Unix(0, lastResync))
		if elapsed < ResyncCooldown {
			return
		}
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
			g.peerCount.Add(-1)
			return true
		}

		total++
		if peer.genTime > myGenTime+GenTimeThreshold {
			newerCount++
		}
		return true
	})

	// Quorum: majority of known peers have a significantly newer generation time
	if total > 0 && newerCount > total/2 {
		g.logger.Warn("Config generation time behind quorum, forcing resync",
			zap.Int64("myGenTime", myGenTime),
			zap.Int("peers", total),
			zap.Int("newerPeers", newerCount),
		)
		g.lastResyncTime.Store(time.Now().UnixNano())
		g.forceResyncFunc()
	}
}
