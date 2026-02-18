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
	"testing"
)

func TestNewVectorClock(t *testing.T) {
	vc := NewVectorClock()
	if vc == nil {
		t.Fatal("expected vector clock to be created")
	}
	if vc.clocks == nil {
		t.Error("expected clocks map to be initialized")
	}
}

func TestVectorClock_Increment(t *testing.T) {
	vc := NewVectorClock()
	
	vc.Increment("node1")
	if vc.Get("node1") != 1 {
		t.Errorf("expected node1 clock to be 1, got %d", vc.Get("node1"))
	}
	
	vc.Increment("node1")
	if vc.Get("node1") != 2 {
		t.Errorf("expected node1 clock to be 2, got %d", vc.Get("node1"))
	}
	
	vc.Increment("node2")
	if vc.Get("node2") != 1 {
		t.Errorf("expected node2 clock to be 1, got %d", vc.Get("node2"))
	}
}

func TestVectorClock_Get_NonExistent(t *testing.T) {
	vc := NewVectorClock()
	
	if vc.Get("nonexistent") != 0 {
		t.Error("expected 0 for non-existent node")
	}
}

func TestVectorClock_ToMap(t *testing.T) {
	vc := NewVectorClock()
	vc.Increment("node1")
	vc.Increment("node1")
	vc.Increment("node2")
	
	m := vc.ToMap()
	if len(m) != 2 {
		t.Errorf("expected 2 entries in map, got %d", len(m))
	}
	if m["node1"] != 2 {
		t.Errorf("expected node1=2, got %d", m["node1"])
	}
	if m["node2"] != 1 {
		t.Errorf("expected node2=1, got %d", m["node2"])
	}
}

func TestNewVectorClockFromMap(t *testing.T) {
	m := map[string]int64{
		"node1": 5,
		"node2": 3,
		"node3": 1,
	}
	
	vc := NewVectorClockFromMap(m)
	if vc == nil {
		t.Fatal("expected vector clock to be created")
	}
	
	if vc.Get("node1") != 5 {
		t.Error("expected node1=5")
	}
	if vc.Get("node2") != 3 {
		t.Error("expected node2=3")
	}
	if vc.Get("node3") != 1 {
		t.Error("expected node3=1")
	}
}

func TestVectorClock_Merge(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Increment("node1")
	vc1.Increment("node1")
	vc1.Increment("node2")
	
	vc2 := NewVectorClock()
	vc2.Increment("node2")
	vc2.Increment("node2")
	vc2.Increment("node3")
	
	vc1.Merge(vc2)
	
	// Should take max of each node
	if vc1.Get("node1") != 2 {
		t.Errorf("expected node1=2, got %d", vc1.Get("node1"))
	}
	if vc1.Get("node2") != 2 {
		t.Errorf("expected node2=2 (max of 1,2), got %d", vc1.Get("node2"))
	}
	if vc1.Get("node3") != 1 {
		t.Errorf("expected node3=1, got %d", vc1.Get("node3"))
	}
}

func TestVectorClock_Compare_Concurrent(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Increment("node1")
	vc1.Increment("node1")
	
	vc2 := NewVectorClock()
	vc2.Increment("node2")
	vc2.Increment("node2")
	
	// Neither dominates the other - concurrent
	if vc1.Compare(vc2) != 0 {
		t.Error("expected concurrent (0)")
	}
}

func TestVectorClock_Compare_Greater(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Increment("node1")
	vc1.Increment("node1")
	vc1.Increment("node2")
	
	vc2 := NewVectorClock()
	vc2.Increment("node1")
	vc2.Increment("node2")
	
	// vc1 dominates vc2 (all entries >= and at least one >)
	if vc1.Compare(vc2) != 1 {
		t.Error("expected vc1 > vc2 (1)")
	}
}

func TestVectorClock_Compare_Less(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Increment("node1")
	
	vc2 := NewVectorClock()
	vc2.Increment("node1")
	vc2.Increment("node1")
	
	// vc2 dominates vc1
	if vc1.Compare(vc2) != -1 {
		t.Error("expected vc1 < vc2 (-1)")
	}
}

func TestVectorClock_Compare_Equal(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Increment("node1")
	vc1.Increment("node2")
	
	vc2 := NewVectorClock()
	vc2.Increment("node1")
	vc2.Increment("node2")
	
	// Same clocks but not "equal" in the dominance sense - this is concurrent
	cmp := vc1.Compare(vc2)
	if cmp != 0 {
		t.Errorf("expected concurrent/equal (0), got %d", cmp)
	}
}

func TestResourceKey_String(t *testing.T) {
	key := ResourceKey{
		Kind:      "ProxyGateway",
		Namespace: "default",
		Name:      "my-gateway",
	}
	
	expected := "ProxyGateway/default/my-gateway"
	if key.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, key.String())
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	
	if config == nil {
		t.Fatal("expected config to be created")
	}
	if config.Mode != ModeMesh {
		t.Errorf("expected default mode %s, got %s", ModeMesh, config.Mode)
	}
	if config.SyncInterval <= 0 {
		t.Error("expected positive sync interval")
	}
	if config.CompressionEnabled != true {
		t.Error("expected compression to be enabled by default")
	}
}

func TestConfig_Equal(t *testing.T) {
	config1 := DefaultConfig()
	config1.FederationID = "test-fed"
	config1.LocalMember = &PeerInfo{Name: "local", Endpoint: "local:9443"}
	
	config2 := DefaultConfig()
	config2.FederationID = "test-fed"
	config2.LocalMember = &PeerInfo{Name: "local", Endpoint: "local:9443"}
	
	if !config1.Equal(config2) {
		t.Error("expected configs to be equal")
	}
	
	config2.FederationID = "different"
	if config1.Equal(config2) {
		t.Error("expected configs to be different")
	}
}
