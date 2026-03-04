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

package gossip

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewConfigGossiper(t *testing.T) {
	logger := zap.NewNop()
	var resyncCalled bool
	resyncFunc := func() { resyncCalled = true }
	_ = resyncCalled

	g := NewConfigGossiper("test-node", resyncFunc, logger)

	require.NotNil(t, g)
	assert.Equal(t, "test-node", g.nodeName)
	assert.Equal(t, "224.0.0.100:9478", g.multicastAddr)
	assert.NotNil(t, g.forceResyncFunc)
	assert.NotNil(t, g.logger)
}

func TestConfigGossiper_UpdateGenTime(t *testing.T) {
	logger := zap.NewNop()
	g := NewConfigGossiper("test-node", func() {}, logger)

	assert.Equal(t, int64(0), g.currentGenTime.Load())

	g.UpdateGenTime(1234567890)
	assert.Equal(t, int64(1234567890), g.currentGenTime.Load())

	g.UpdateGenTime(9876543210)
	assert.Equal(t, int64(9876543210), g.currentGenTime.Load())
}

func TestConfigGossiper_handleMessage(t *testing.T) {
	logger := zap.NewNop()
	g := NewConfigGossiper("test-node", func() {}, logger)

	tests := []struct {
		name        string
		data        string
		expectStore bool
		expectedGen int64
	}{
		{
			name:        "valid message",
			data:        "config_version|peer-node|1234567890|1609459200000000000",
			expectStore: true,
			expectedGen: 1234567890,
		},
		{
			name:        "invalid prefix",
			data:        "invalid_prefix|peer-node|1234567890|1609459200000000000",
			expectStore: false,
			expectedGen: 0,
		},
		{
			name:        "missing parts",
			data:        "config_version|peer-node|1234567890",
			expectStore: false,
			expectedGen: 0,
		},
		{
			name:        "invalid genTime",
			data:        "config_version|peer-node|invalid|1609459200000000000",
			expectStore: false,
			expectedGen: 0,
		},
		{
			name:        "own message - should be ignored",
			data:        "config_version|test-node|9999999999|1609459200000000000",
			expectStore: false,
			expectedGen: 0,
		},
		{
			name:        "empty message",
			data:        "",
			expectStore: false,
			expectedGen: 0,
		},
		{
			name:        "malformed message",
			data:        "config_version||||",
			expectStore: false,
			expectedGen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear peer versions before each test
			g.peerVersions.Range(func(key, val any) bool {
				g.peerVersions.Delete(key)
				return true
			})

			g.handleMessage(tt.data)

			if tt.expectStore {
				val, ok := g.peerVersions.Load("peer-node")
				assert.True(t, ok, "expected peer to be stored")
				if ok {
					peer, ok := val.(peerState)
					require.True(t, ok)
					assert.Equal(t, tt.expectedGen, peer.genTime)
					assert.WithinDuration(t, time.Now(), peer.lastSeen, time.Second)
				}
			} else {
				// For own message, specifically check that test-node is not stored
				if tt.name == "own message - should be ignored" {
					_, ok := g.peerVersions.Load("test-node")
					assert.False(t, ok, "own node should not be stored in peer versions")
				}
			}
		})
	}
}

func TestConfigGossiper_handleMessage_UpdatesExistingPeer(t *testing.T) {
	logger := zap.NewNop()
	g := NewConfigGossiper("test-node", func() {}, logger)

	// First message
	g.handleMessage("config_version|peer-node|100|1609459200000000000")
	val1, ok1 := g.peerVersions.Load("peer-node")
	require.True(t, ok1)
	peer1, ok := val1.(peerState)
	require.True(t, ok)
	assert.Equal(t, int64(100), peer1.genTime)

	// Wait a bit to ensure different timestamp
	time.Sleep(10 * time.Millisecond)

	// Second message with updated genTime
	g.handleMessage("config_version|peer-node|200|1609459201000000000")
	val2, ok2 := g.peerVersions.Load("peer-node")
	require.True(t, ok2)
	peer2, ok := val2.(peerState)
	require.True(t, ok)
	assert.Equal(t, int64(200), peer2.genTime)
	assert.True(t, peer2.lastSeen.After(peer1.lastSeen))
}

func TestConfigGossiper_checkQuorum_NoConfig(t *testing.T) {
	logger := zap.NewNop()
	resyncCalled := int32(0)
	resyncFunc := func() { atomic.AddInt32(&resyncCalled, 1) }

	g := NewConfigGossiper("test-node", resyncFunc, logger)

	// No config applied (genTime = 0)
	g.checkQuorum()

	// Should not trigger resync
	assert.Equal(t, int32(0), atomic.LoadInt32(&resyncCalled))
}

func TestConfigGossiper_checkQuorum_Cooldown(t *testing.T) {
	logger := zap.NewNop()
	resyncCalled := int32(0)
	resyncFunc := func() { atomic.AddInt32(&resyncCalled, 1) }

	g := NewConfigGossiper("test-node", resyncFunc, logger)
	g.UpdateGenTime(1000)

	// Set last resync time to recent
	g.lastResyncTime.Store(time.Now().Add(-10 * time.Second).UnixNano())

	// Add peers with newer genTime
	g.peerVersions.Store("peer1", peerState{genTime: 2000, lastSeen: time.Now()})
	g.peerVersions.Store("peer2", peerState{genTime: 2000, lastSeen: time.Now()})

	g.checkQuorum()

	// Should not trigger resync due to cooldown
	assert.Equal(t, int32(0), atomic.LoadInt32(&resyncCalled))
}

func TestConfigGossiper_checkQuorum_ExpiredPeers(t *testing.T) {
	logger := zap.NewNop()
	resyncCalled := int32(0)
	resyncFunc := func() { atomic.AddInt32(&resyncCalled, 1) }

	g := NewConfigGossiper("test-node", resyncFunc, logger)
	g.UpdateGenTime(1000)

	// Add expired peer
	g.peerVersions.Store("expired-peer", peerState{
		genTime:  2000,
		lastSeen: time.Now().Add(-PeerExpiry - time.Minute),
	})

	g.checkQuorum()

	// Expired peer should be deleted
	_, ok := g.peerVersions.Load("expired-peer")
	assert.False(t, ok, "expired peer should be deleted")

	// No resync should happen with no active peers
	assert.Equal(t, int32(0), atomic.LoadInt32(&resyncCalled))
}

func TestConfigGossiper_checkQuorum_NoQuorum(t *testing.T) {
	logger := zap.NewNop()
	resyncCalled := int32(0)
	resyncFunc := func() { atomic.AddInt32(&resyncCalled, 1) }

	g := NewConfigGossiper("test-node", resyncFunc, logger)
	g.UpdateGenTime(1000)

	// Add peers but not majority with newer genTime
	g.peerVersions.Store("peer1", peerState{genTime: 500, lastSeen: time.Now()})  // older
	g.peerVersions.Store("peer2", peerState{genTime: 1500, lastSeen: time.Now()}) // newer but within threshold
	g.peerVersions.Store("peer3", peerState{genTime: 2000, lastSeen: time.Now()}) // newer

	g.checkQuorum()

	// Only1 out of 3 is significantly newer, not a majority
	assert.Equal(t, int32(0), atomic.LoadInt32(&resyncCalled))
}

func TestConfigGossiper_checkQuorum_QuorumTriggersResync(t *testing.T) {
	logger := zap.NewNop()
	resyncCalled := int32(0)
	resyncFunc := func() { atomic.AddInt32(&resyncCalled, 1) }

	g := NewConfigGossiper("test-node", resyncFunc, logger)
	g.UpdateGenTime(1000)

	// Add peers where majority have significantly newer genTime
	// Using GenTimeThreshold (60) as the threshold
	g.peerVersions.Store("peer1", peerState{genTime: 2000, lastSeen: time.Now()}) // significantly newer
	g.peerVersions.Store("peer2", peerState{genTime: 2000, lastSeen: time.Now()}) // significantly newer
	g.peerVersions.Store("peer3", peerState{genTime: 500, lastSeen: time.Now()})  // older

	g.checkQuorum()

	// 2 out of 3 peers are significantly newer - majority triggers resync
	assert.Equal(t, int32(1), atomic.LoadInt32(&resyncCalled))

	// Verify lastResyncTime was updated
	assert.Greater(t, g.lastResyncTime.Load(), int64(0))
}

func TestConfigGossiper_checkQuorum_ExactlyHalfNotQuorum(t *testing.T) {
	logger := zap.NewNop()
	resyncCalled := int32(0)
	resyncFunc := func() { atomic.AddInt32(&resyncCalled, 1) }

	g := NewConfigGossiper("test-node", resyncFunc, logger)
	g.UpdateGenTime(1000)

	// Add 4 peers, exactly2 are newer (not majority)
	g.peerVersions.Store("peer1", peerState{genTime: 2000, lastSeen: time.Now()})
	g.peerVersions.Store("peer2", peerState{genTime: 2000, lastSeen: time.Now()})
	g.peerVersions.Store("peer3", peerState{genTime: 500, lastSeen: time.Now()})
	g.peerVersions.Store("peer4", peerState{genTime: 500, lastSeen: time.Now()})

	g.checkQuorum()

	// 2 out of 4 is exactly half, not majority (> total/2)
	assert.Equal(t, int32(0), atomic.LoadInt32(&resyncCalled))
}

func TestConfigGossiper_checkQuorum_SingleNewerPeer(t *testing.T) {
	logger := zap.NewNop()
	resyncCalled := int32(0)
	resyncFunc := func() { atomic.AddInt32(&resyncCalled, 1) }

	g := NewConfigGossiper("test-node", resyncFunc, logger)
	g.UpdateGenTime(1000)

	// Single peer that is newer
	g.peerVersions.Store("peer1", peerState{genTime: 2000, lastSeen: time.Now()})

	g.checkQuorum()

	// 1 out of 1 is majority (> 1/2 = 0)
	assert.Equal(t, int32(1), atomic.LoadInt32(&resyncCalled))
}

func TestConfigGossiper_checkQuorum_GenTimeThreshold(t *testing.T) {
	logger := zap.NewNop()
	resyncCalled := int32(0)
	resyncFunc := func() { atomic.AddInt32(&resyncCalled, 1) }

	g := NewConfigGossiper("test-node", resyncFunc, logger)
	g.UpdateGenTime(1000)

	// Peer with genTime within threshold (1000 + 60 = 1060)
	g.peerVersions.Store("peer1", peerState{genTime: 1050, lastSeen: time.Now()}) // within threshold
	g.peerVersions.Store("peer2", peerState{genTime: 1059, lastSeen: time.Now()}) // within threshold

	g.checkQuorum()

	// Peers within threshold should not count as "newer"
	assert.Equal(t, int32(0), atomic.LoadInt32(&resyncCalled))

	// Now test with peer just above threshold
	g.peerVersions.Store("peer3", peerState{genTime: 1061, lastSeen: time.Now()}) // just above threshold

	g.checkQuorum()

	// 1 out of 3 is not majority
	assert.Equal(t, int32(0), atomic.LoadInt32(&resyncCalled))
}

func TestPeerState(t *testing.T) {
	now := time.Now()
	peer := peerState{
		genTime:  1234567890,
		lastSeen: now,
	}

	assert.Equal(t, int64(1234567890), peer.genTime)
	assert.Equal(t, now, peer.lastSeen)
}

func TestConstants(t *testing.T) {
	assert.Equal(t, 9478, GossipPort)
	assert.Equal(t, "224.0.0.100", MulticastAddr)
	assert.Equal(t, 5*time.Second, BroadcastInterval)
	assert.Equal(t, 5*time.Second, QuorumCheckInterval)
	assert.Equal(t, 15*time.Second, PeerExpiry)
	assert.Equal(t, int64(60), GenTimeThreshold)
	assert.Equal(t, 30*time.Second, ResyncCooldown)
	assert.Equal(t, "config_version", messagePrefix)
}

func TestConfigGossiper_checkQuorum_MultipleResyncs(t *testing.T) {
	logger := zap.NewNop()
	resyncCalled := int32(0)
	resyncFunc := func() { atomic.AddInt32(&resyncCalled, 1) }

	g := NewConfigGossiper("test-node", resyncFunc, logger)
	g.UpdateGenTime(1000)

	// Add majority newer peers
	g.peerVersions.Store("peer1", peerState{genTime: 2000, lastSeen: time.Now()})
	g.peerVersions.Store("peer2", peerState{genTime: 2000, lastSeen: time.Now()})

	// First check should trigger resync
	g.checkQuorum()
	assert.Equal(t, int32(1), atomic.LoadInt32(&resyncCalled))

	// Second check immediately should not trigger due to cooldown
	g.checkQuorum()
	assert.Equal(t, int32(1), atomic.LoadInt32(&resyncCalled))

	// After cooldown period, should be able to trigger again
	g.lastResyncTime.Store(time.Now().Add(-ResyncCooldown - time.Second).UnixNano())
	g.checkQuorum()
	assert.Equal(t, int32(2), atomic.LoadInt32(&resyncCalled))
}

// Test that checkQuorum handles invalid peer state gracefully
func TestConfigGossiper_checkQuorum_InvalidPeerState(t *testing.T) {
	logger := zap.NewNop()
	resyncCalled := int32(0)
	resyncFunc := func() { atomic.AddInt32(&resyncCalled, 1) }

	g := NewConfigGossiper("test-node", resyncFunc, logger)
	g.UpdateGenTime(1000)

	// Store invalid peer state (not peerState type)
	g.peerVersions.Store("invalid-peer", "not a peerState")

	// Should not panic and should continue processing
	g.checkQuorum()

	// Invalid entry should be skipped, no resync
	assert.Equal(t, int32(0), atomic.LoadInt32(&resyncCalled))
}

// Benchmark tests
func BenchmarkConfigGossiper_handleMessage(b *testing.B) {
	logger := zap.NewNop()
	g := NewConfigGossiper("test-node", func() {}, logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.handleMessage("config_version|peer-node|1234567890|1609459200000000000")
	}
}

func BenchmarkConfigGossiper_checkQuorum(b *testing.B) {
	logger := zap.NewNop()
	g := NewConfigGossiper("test-node", func() {}, logger)
	g.UpdateGenTime(1000)

	// Add some peers
	for i := 0; i < 10; i++ {
		g.peerVersions.Store(string(rune('a'+i)), peerState{genTime: 2000, lastSeen: time.Now()})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.checkQuorum()
	}
}

func BenchmarkConfigGossiper_UpdateGenTime(b *testing.B) {
	logger := zap.NewNop()
	g := NewConfigGossiper("test-node", func() {}, logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.UpdateGenTime(int64(i))
	}
}
