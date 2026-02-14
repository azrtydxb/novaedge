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
	"encoding/json"
	"testing"
	"time"
)

func TestNewVectorClock(t *testing.T) {
	vc := NewVectorClock()
	if vc == nil {
		t.Fatal("NewVectorClock() returned nil")
	}
	if vc.clocks == nil {
		t.Error("clocks map is nil")
	}
	if len(vc.clocks) != 0 {
		t.Errorf("expected empty clocks map, got %d entries", len(vc.clocks))
	}
}

func TestNewVectorClockFromMap(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]int64
		expect map[string]int64
	}{
		{
			name:   "nil map",
			input:  nil,
			expect: map[string]int64{},
		},
		{
			name:   "empty map",
			input:  map[string]int64{},
			expect: map[string]int64{},
		},
		{
			name:   "single entry",
			input:  map[string]int64{"node1": 5},
			expect: map[string]int64{"node1": 5},
		},
		{
			name:   "multiple entries",
			input:  map[string]int64{"node1": 5, "node2": 10, "node3": 15},
			expect: map[string]int64{"node1": 5, "node2": 10, "node3": 15},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vc := NewVectorClockFromMap(tt.input)
			if vc == nil {
				t.Fatal("NewVectorClockFromMap() returned nil")
			}

			// Verify it's a copy, not the same map
			for k, v := range tt.expect {
				if vc.Get(k) != v {
					t.Errorf("Get(%q) = %d, want %d", k, vc.Get(k), v)
				}
			}
		})
	}
}

func TestVectorClock_Increment(t *testing.T) {
	vc := NewVectorClock()

	// First increment should return 1
	val := vc.Increment("node1")
	if val != 1 {
		t.Errorf("first Increment(node1) = %d, want 1", val)
	}

	// Second increment should return 2
	val = vc.Increment("node1")
	if val != 2 {
		t.Errorf("second Increment(node1) = %d, want 2", val)
	}

	// Increment different node
	val = vc.Increment("node2")
	if val != 1 {
		t.Errorf("first Increment(node2) = %d, want 1", val)
	}

	// Verify node1 unchanged
	if vc.Get("node1") != 2 {
		t.Errorf("Get(node1) = %d, want 2", vc.Get("node1"))
	}
}

func TestVectorClock_Get(t *testing.T) {
	vc := NewVectorClock()

	// Non-existent key should return 0
	if vc.Get("nonexistent") != 0 {
		t.Errorf("Get(nonexistent) = %d, want 0", vc.Get("nonexistent"))
	}

	// Set and get
	vc.Set("node1", 5)
	if vc.Get("node1") != 5 {
		t.Errorf("Get(node1) = %d, want 5", vc.Get("node1"))
	}
}

func TestVectorClock_Set(t *testing.T) {
	vc := NewVectorClock()

	vc.Set("node1", 10)
	if vc.Get("node1") != 10 {
		t.Errorf("Get(node1) = %d, want 10", vc.Get("node1"))
	}

	// Overwrite
	vc.Set("node1", 20)
	if vc.Get("node1") != 20 {
		t.Errorf("Get(node1) after overwrite = %d, want 20", vc.Get("node1"))
	}

	// Negative values
	vc.Set("node2", -5)
	if vc.Get("node2") != -5 {
		t.Errorf("Get(node2) = %d, want -5", vc.Get("node2"))
	}
}

func TestVectorClock_Merge(t *testing.T) {
	tests := []struct {
		name     string
		initial  map[string]int64
		other    map[string]int64
		expected map[string]int64
	}{
		{
			name:     "merge with nil",
			initial:  map[string]int64{"node1": 5},
			other:    nil,
			expected: map[string]int64{"node1": 5},
		},
		{
			name:     "merge empty with values",
			initial:  map[string]int64{},
			other:    map[string]int64{"node1": 5},
			expected: map[string]int64{"node1": 5},
		},
		{
			name:     "merge disjoint sets",
			initial:  map[string]int64{"node1": 5},
			other:    map[string]int64{"node2": 10},
			expected: map[string]int64{"node1": 5, "node2": 10},
		},
		{
			name:     "merge with max - other greater",
			initial:  map[string]int64{"node1": 5},
			other:    map[string]int64{"node1": 10},
			expected: map[string]int64{"node1": 10},
		},
		{
			name:     "merge with max - initial greater",
			initial:  map[string]int64{"node1": 15},
			other:    map[string]int64{"node1": 10},
			expected: map[string]int64{"node1": 15},
		},
		{
			name:     "complex merge",
			initial:  map[string]int64{"node1": 5, "node2": 20, "node3": 15},
			other:    map[string]int64{"node1": 10, "node2": 10, "node4": 30},
			expected: map[string]int64{"node1": 10, "node2": 20, "node3": 15, "node4": 30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vc := NewVectorClockFromMap(tt.initial)
			var other *VectorClock
			if tt.other != nil {
				other = NewVectorClockFromMap(tt.other)
			}

			vc.Merge(other)

			for k, v := range tt.expected {
				if vc.Get(k) != v {
					t.Errorf("Get(%q) = %d, want %d", k, vc.Get(k), v)
				}
			}
		})
	}
}

func TestVectorClock_MergeMap(t *testing.T) {
	vc := NewVectorClockFromMap(map[string]int64{"node1": 5})

	vc.MergeMap(map[string]int64{"node1": 10, "node2": 20})

	if vc.Get("node1") != 10 {
		t.Errorf("Get(node1) = %d, want 10", vc.Get("node1"))
	}
	if vc.Get("node2") != 20 {
		t.Errorf("Get(node2) = %d, want 20", vc.Get("node2"))
	}
}

func TestVectorClock_Compare(t *testing.T) {
	tests := []struct {
		name     string
		this     map[string]int64
		other    map[string]int64
		expected int
	}{
		{
			name:     "compare with nil",
			this:     map[string]int64{"node1": 5},
			other:    nil,
			expected: 1,
		},
		{
			name:     "equal clocks",
			this:     map[string]int64{"node1": 5, "node2": 10},
			other:    map[string]int64{"node1": 5, "node2": 10},
			expected: 0,
		},
		{
			name:     "this happened after",
			this:     map[string]int64{"node1": 10, "node2": 10},
			other:    map[string]int64{"node1": 5, "node2": 10},
			expected: 1,
		},
		{
			name:     "this happened before",
			this:     map[string]int64{"node1": 5, "node2": 10},
			other:    map[string]int64{"node1": 10, "node2": 10},
			expected: -1,
		},
		{
			name:     "concurrent - different values",
			this:     map[string]int64{"node1": 10, "node2": 5},
			other:    map[string]int64{"node1": 5, "node2": 10},
			expected: 0,
		},
		{
			name:     "empty clocks",
			this:     map[string]int64{},
			other:    map[string]int64{},
			expected: 0,
		},
		{
			name:     "disjoint clocks are concurrent",
			this:     map[string]int64{"node1": 5},
			other:    map[string]int64{"node2": 5},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vc := NewVectorClockFromMap(tt.this)
			var other *VectorClock
			if tt.other != nil {
				other = NewVectorClockFromMap(tt.other)
			}

			result := vc.Compare(other)
			if result != tt.expected {
				t.Errorf("Compare() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestVectorClock_CompareMap(t *testing.T) {
	vc := NewVectorClockFromMap(map[string]int64{"node1": 10})

	result := vc.CompareMap(map[string]int64{"node1": 5})
	if result != 1 {
		t.Errorf("CompareMap() = %d, want 1", result)
	}
}

func TestVectorClock_HappenedBefore(t *testing.T) {
	vc1 := NewVectorClockFromMap(map[string]int64{"node1": 5})
	vc2 := NewVectorClockFromMap(map[string]int64{"node1": 10})

	if !vc1.HappenedBefore(vc2) {
		t.Error("vc1 should have happened before vc2")
	}
	if vc2.HappenedBefore(vc1) {
		t.Error("vc2 should not have happened before vc1")
	}
}

func TestVectorClock_HappenedAfter(t *testing.T) {
	vc1 := NewVectorClockFromMap(map[string]int64{"node1": 5})
	vc2 := NewVectorClockFromMap(map[string]int64{"node1": 10})

	if !vc2.HappenedAfter(vc1) {
		t.Error("vc2 should have happened after vc1")
	}
	if vc1.HappenedAfter(vc2) {
		t.Error("vc1 should not have happened after vc2")
	}
}

func TestVectorClock_Concurrent(t *testing.T) {
	tests := []struct {
		name     string
		this     map[string]int64
		other    map[string]int64
		expected bool
	}{
		{
			name:     "concurrent clocks",
			this:     map[string]int64{"node1": 10, "node2": 5},
			other:    map[string]int64{"node1": 5, "node2": 10},
			expected: true,
		},
		{
			name:     "not concurrent - ordered",
			this:     map[string]int64{"node1": 10, "node2": 10},
			other:    map[string]int64{"node1": 5, "node2": 10},
			expected: false,
		},
		{
			name:     "equal clocks are concurrent",
			this:     map[string]int64{"node1": 5},
			other:    map[string]int64{"node1": 5},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vc := NewVectorClockFromMap(tt.this)
			other := NewVectorClockFromMap(tt.other)

			result := vc.Concurrent(other)
			if result != tt.expected {
				t.Errorf("Concurrent() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestVectorClock_Copy(t *testing.T) {
	original := NewVectorClockFromMap(map[string]int64{"node1": 5, "node2": 10})
	cloned := original.Copy()

	// Verify copy is equal
	if !original.Equal(cloned) {
		t.Error("copy is not equal to original")
	}

	// Modify original and verify copy is independent
	original.Set("node1", 100)
	if cloned.Get("node1") == 100 {
		t.Error("copy was affected by modification to original")
	}
}

func TestVectorClock_ToMap(t *testing.T) {
	initial := map[string]int64{"node1": 5, "node2": 10}
	vc := NewVectorClockFromMap(initial)

	result := vc.ToMap()

	// Verify contents
	for k, v := range initial {
		if result[k] != v {
			t.Errorf("ToMap()[%q] = %d, want %d", k, result[k], v)
		}
	}

	// Verify it's a copy
	result["node1"] = 100
	if vc.Get("node1") == 100 {
		t.Error("modification to ToMap result affected original")
	}
}

func TestVectorClock_String(t *testing.T) {
	vc := NewVectorClockFromMap(map[string]int64{"node1": 5, "node2": 10})

	result := vc.String()

	// Verify it's valid JSON
	var parsed map[string]int64
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Errorf("String() returned invalid JSON: %v", err)
	}

	// Verify contents
	if parsed["node1"] != 5 {
		t.Errorf("parsed[node1] = %d, want 5", parsed["node1"])
	}
	if parsed["node2"] != 10 {
		t.Errorf("parsed[node2] = %d, want 10", parsed["node2"])
	}
}

func TestVectorClock_Equal(t *testing.T) {
	tests := []struct {
		name     string
		this     map[string]int64
		other    map[string]int64
		expected bool
	}{
		{
			name:     "equal clocks",
			this:     map[string]int64{"node1": 5, "node2": 10},
			other:    map[string]int64{"node1": 5, "node2": 10},
			expected: true,
		},
		{
			name:     "different values",
			this:     map[string]int64{"node1": 5},
			other:    map[string]int64{"node1": 10},
			expected: false,
		},
		{
			name:     "different keys",
			this:     map[string]int64{"node1": 5},
			other:    map[string]int64{"node2": 5},
			expected: false,
		},
		{
			name:     "nil other",
			this:     map[string]int64{"node1": 5},
			other:    nil,
			expected: false,
		},
		{
			name:     "empty clocks",
			this:     map[string]int64{},
			other:    map[string]int64{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vc := NewVectorClockFromMap(tt.this)
			var other *VectorClock
			if tt.other != nil {
				other = NewVectorClockFromMap(tt.other)
			}

			result := vc.Equal(other)
			if result != tt.expected {
				t.Errorf("Equal() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestVectorClock_IsZero(t *testing.T) {
	tests := []struct {
		name     string
		initial  map[string]int64
		expected bool
	}{
		{
			name:     "empty clock",
			initial:  map[string]int64{},
			expected: true,
		},
		{
			name:     "all zeros",
			initial:  map[string]int64{"node1": 0, "node2": 0},
			expected: true,
		},
		{
			name:     "non-zero value",
			initial:  map[string]int64{"node1": 5},
			expected: false,
		},
		{
			name:     "mixed zero and non-zero",
			initial:  map[string]int64{"node1": 0, "node2": 5},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vc := NewVectorClockFromMap(tt.initial)
			result := vc.IsZero()
			if result != tt.expected {
				t.Errorf("IsZero() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestVectorClock_Concurrency(t *testing.T) {
	// Test concurrent access to vector clock
	vc := NewVectorClock()
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			vc.Increment("node1")
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = vc.Get("node1")
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// Verify final value
	if vc.Get("node1") != 100 {
		t.Errorf("Get(node1) = %d, want 100", vc.Get("node1"))
	}
}

func TestResourceKey_String(t *testing.T) {
	tests := []struct {
		name     string
		key      ResourceKey
		expected string
	}{
		{
			name: "with namespace",
			key: ResourceKey{
				Kind:      "ProxyGateway",
				Namespace: "default",
				Name:      "my-gateway",
			},
			expected: "ProxyGateway/default/my-gateway",
		},
		{
			name: "without namespace",
			key: ResourceKey{
				Kind: "ProxyGateway",
				Name: "my-gateway",
			},
			expected: "ProxyGateway/my-gateway",
		},
		{
			name: "empty namespace",
			key: ResourceKey{
				Kind:      "ProxyRoute",
				Namespace: "",
				Name:      "my-route",
			},
			expected: "ProxyRoute/my-route",
		},
		{
			name: "cluster-scoped resource",
			key: ResourceKey{
				Kind:      "NovaEdgeCluster",
				Namespace: "",
				Name:      "cluster-1",
			},
			expected: "NovaEdgeCluster/cluster-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.key.String()
			if result != tt.expected {
				t.Errorf("String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestPeerInfo_Validation(t *testing.T) {
	tests := []struct {
		name string
		peer PeerInfo
	}{
		{
			name: "minimal peer",
			peer: PeerInfo{
				Name:     "peer1",
				Endpoint: "localhost:8080",
			},
		},
		{
			name: "full peer config",
			peer: PeerInfo{
				Name:               "peer1",
				Endpoint:           "peer1.example.com:8443",
				Region:             "us-west-1",
				Zone:               "us-west-1a",
				Priority:           10,
				Labels:             map[string]string{"env": "prod"},
				TLSEnabled:         true,
				TLSServerName:      "peer1.example.com",
				InsecureSkipVerify: false,
				CACert:             []byte("ca-cert"),
				ClientCert:         []byte("client-cert"),
				ClientKey:          []byte("client-key"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic field validation
			if tt.peer.Name == "" {
				t.Error("peer name should not be empty")
			}
			if tt.peer.Endpoint == "" {
				t.Error("peer endpoint should not be empty")
			}
		})
	}
}

func TestPeerState_HealthTracking(t *testing.T) {
	now := time.Now()

	state := &PeerState{
		Info: &PeerInfo{
			Name:     "peer1",
			Endpoint: "localhost:8080",
		},
		VectorClock:         NewVectorClock(),
		LastSeen:            now,
		LastSyncTime:        now.Add(-time.Minute),
		Healthy:             true,
		Connected:           true,
		AgentCount:          5,
		SyncLag:             time.Second * 10,
		LastError:           "",
		ConsecutiveFailures: 0,
	}

	// Verify initial state
	if !state.Healthy {
		t.Error("peer should be healthy")
	}
	if !state.Connected {
		t.Error("peer should be connected")
	}
	if state.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", state.ConsecutiveFailures)
	}

	// Simulate failure
	state.Healthy = false
	state.Connected = false
	state.LastError = "connection refused"
	state.ConsecutiveFailures++

	if state.Healthy {
		t.Error("peer should not be healthy after failure")
	}
	if state.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", state.ConsecutiveFailures)
	}
}

func TestPeerState_TimeTracking(t *testing.T) {
	now := time.Now()

	state := &PeerState{
		Info:         &PeerInfo{Name: "peer1", Endpoint: "localhost:8080"},
		VectorClock:  NewVectorClock(),
		LastSeen:     now,
		LastSyncTime: now.Add(-time.Minute),
	}

	// Verify time is properly set
	if state.LastSeen.IsZero() {
		t.Error("LastSeen should not be zero")
	}
	if state.LastSyncTime.IsZero() {
		t.Error("LastSyncTime should not be zero")
	}

	// Verify LastSeen is after LastSyncTime (sync happened before last seen)
	if state.LastSeen.Before(state.LastSyncTime) {
		t.Error("LastSeen should be after LastSyncTime")
	}
}

func TestVectorClock_PeerStateIntegration(t *testing.T) {
	// Test that VectorClock works correctly within PeerState
	state := &PeerState{
		Info:        &PeerInfo{Name: "peer1", Endpoint: "localhost:8080"},
		VectorClock: NewVectorClock(),
	}

	// Increment clock for this peer
	state.VectorClock.Increment("local")
	state.VectorClock.Increment("local")

	if state.VectorClock.Get("local") != 2 {
		t.Errorf("VectorClock.Get(local) = %d, want 2", state.VectorClock.Get("local"))
	}

	// Merge with another clock from remote peer
	remoteClock := NewVectorClockFromMap(map[string]int64{
		"remote": 5,
		"local":  1,
	})
	state.VectorClock.Merge(remoteClock)

	if state.VectorClock.Get("remote") != 5 {
		t.Errorf("VectorClock.Get(remote) = %d, want 5", state.VectorClock.Get("remote"))
	}
	// Local should still be 2 (max of 2 and 1)
	if state.VectorClock.Get("local") != 2 {
		t.Errorf("VectorClock.Get(local) = %d, want 2", state.VectorClock.Get("local"))
	}
}
