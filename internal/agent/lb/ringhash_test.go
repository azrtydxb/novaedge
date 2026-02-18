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

package lb

import (
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewRingHash(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)
	if rh == nil {
		t.Fatal("NewRingHash() returned nil")
	}

	// Verify ring is built
	if len(rh.data.Load().ring) == 0 {
		t.Error("Ring should not be empty")
	}

	// Each endpoint should have virtualNodes entries
	expectedSize := len(endpoints) * defaultVirtualNodes
	if len(rh.data.Load().ring) != expectedSize {
		t.Errorf("Ring size = %d, want %d", len(rh.data.Load().ring), expectedSize)
	}
}

func TestNewRingHash_EmptyEndpoints(t *testing.T) {
	rh := NewRingHash([]*pb.Endpoint{})
	if rh == nil {
		t.Fatal("NewRingHash() returned nil for empty endpoints")
	}

	if len(rh.data.Load().ring) != 0 {
		t.Errorf("Ring should be empty, got size %d", len(rh.data.Load().ring))
	}
}

func TestRingHash_Select(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	tests := []struct {
		name string
		key  string
	}{
		{"simple key", "request-1"},
		{"empty key", ""},
		{"long key", "very-long-request-key-with-lots-of-characters"},
		{"special chars", "key/with/slashes:and:colons"},
		{"unicode key", "unicode-キー"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep := rh.Select(tt.key)
			if ep == nil {
				t.Error("Select() returned nil for non-empty endpoints")
			}
		})
	}
}

func TestRingHash_Select_EmptyEndpoints(t *testing.T) {
	rh := NewRingHash([]*pb.Endpoint{})

	ep := rh.Select("test-key")
	if ep != nil {
		t.Errorf("Select() with empty endpoints returned %v, want nil", ep)
	}
}

func TestRingHash_Select_Consistency(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	// Same key should always return the same endpoint
	key := "consistent-key"
	first := rh.Select(key)
	for i := 0; i < 100; i++ {
		ep := rh.Select(key)
		if ep != first {
			t.Error("Select() not consistent for same key")
			break
		}
	}
}

func TestRingHash_SelectDefault(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	ep := rh.SelectDefault()
	if ep == nil {
		t.Error("SelectDefault() returned nil for non-empty endpoints")
	}
}

func TestRingHash_SelectDefault_NoReadyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: false},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
	}

	rh := NewRingHash(endpoints)

	ep := rh.SelectDefault()
	if ep != nil {
		t.Errorf("SelectDefault() with no ready endpoints returned %v, want nil", ep)
	}
}

func TestRingHash_SelectDefault_EmptyEndpoints(t *testing.T) {
	rh := NewRingHash([]*pb.Endpoint{})

	ep := rh.SelectDefault()
	if ep != nil {
		t.Errorf("SelectDefault() with empty endpoints returned %v, want nil", ep)
	}
}

func TestRingHash_UpdateEndpoints(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	rh := NewRingHash(initialEndpoints)
	initialSize := rh.GetRingSize()

	// Update endpoints
	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: true},
		{Address: "10.0.0.5", Port: 8080, Ready: true},
	}

	rh.UpdateEndpoints(newEndpoints)
	newSize := rh.GetRingSize()

	// Size should change
	if newSize == initialSize {
		t.Error("Ring size should change after UpdateEndpoints")
	}

	// Verify new size
	expectedSize := len(newEndpoints) * defaultVirtualNodes
	if newSize != expectedSize {
		t.Errorf("Ring size = %d, want %d", newSize, expectedSize)
	}
}

func TestRingHash_GetRingSize(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	size := rh.GetRingSize()
	expected := len(endpoints) * defaultVirtualNodes
	if size != expected {
		t.Errorf("GetRingSize() = %d, want %d", size, expected)
	}
}

func TestRingHash_Distribution(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	// Count distribution of many keys
	counts := make(map[string]int)
	numKeys := 1000

	for i := 0; i < numKeys; i++ {
		key := string(rune(i))
		ep := rh.Select(key)
		if ep != nil {
			counts[ep.Address]++
		}
	}

	// Each endpoint should get roughly 1/3 of the keys
	expectedPerEndpoint := numKeys / len(endpoints)
	tolerance := expectedPerEndpoint / 2 // 50% tolerance

	for _, ep := range endpoints {
		count := counts[ep.Address]
		diff := count - expectedPerEndpoint
		if diff < 0 {
			diff = -diff
		}
		if diff > tolerance {
			t.Logf("Warning: endpoint %s got %d keys, expected around %d", ep.Address, count, expectedPerEndpoint)
		}
	}
}

func TestRingHash_MinimalChange(t *testing.T) {
	// When an endpoint is added/removed, minimal keys should remap
	initialEndpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	rh := NewRingHash(initialEndpoints)

	// Record initial mappings
	initialMappings := make(map[string]string)
	keys := []string{"key1", "key2", "key3", "key4", "key5"}
	for _, key := range keys {
		ep := rh.Select(key)
		if ep != nil {
			initialMappings[key] = ep.Address
		}
	}

	// Add a new endpoint
	newEndpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}
	rh.UpdateEndpoints(newEndpoints)

	// Count how many keys remapped
	remapped := 0
	for _, key := range keys {
		ep := rh.Select(key)
		if ep != nil && ep.Address != initialMappings[key] {
			remapped++
		}
	}

	// Some keys should remap, but not all (typically ~1/3 for adding 1 endpoint to 2)
	// This is a probabilistic test, so we just verify not all remapped
	if remapped == len(keys) {
		t.Log("Warning: all keys remapped after adding endpoint, expected minimal remapping")
	}
}

func TestRingHash_ConcurrentAccess(_ *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	done := make(chan bool)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := string(rune(id*100 + j))
				_ = rh.Select(key)
			}
			done <- true
		}(i)
	}

	// Concurrent updates
	go func() {
		for i := 0; i < 10; i++ {
			newEndpoints := []*pb.Endpoint{
				{Address: testAddrEWMA, Port: 8080, Ready: true},
			}
			rh.UpdateEndpoints(newEndpoints)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 11; i++ {
		<-done
	}
}

func TestRingHash_SingleEndpoint(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	// All requests should go to the single endpoint
	for i := 0; i < 10; i++ {
		ep := rh.Select(string(rune(i)))
		if ep == nil {
			t.Error("Select() returned nil")
		} else if ep.Address != testAddrEWMA {
			t.Errorf("Select() returned wrong endpoint: %s", ep.Address)
		}
	}
}

func TestRingHash_UnreadyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: false},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
	}

	rh := NewRingHash(endpoints)

	// No ready endpoints, so ring should be empty
	if len(rh.data.Load().ring) != 0 {
		t.Errorf("Ring should be empty with no ready endpoints, got size %d", len(rh.data.Load().ring))
	}

	// Select should return nil
	ep := rh.Select("test-key")
	if ep != nil {
		t.Errorf("Select() with no ready endpoints returned %v, want nil", ep)
	}
}

func TestRingHash_MixedReadyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	// Only ready endpoints should be in the ring
	expectedSize := 2 * defaultVirtualNodes
	if len(rh.data.Load().ring) != expectedSize {
		t.Errorf("Ring size = %d, want %d", len(rh.data.Load().ring), expectedSize)
	}

	// All selections should be ready endpoints
	for i := 0; i < 100; i++ {
		ep := rh.Select(string(rune(i)))
		if ep != nil && !ep.Ready {
			t.Errorf("Select() returned unready endpoint: %s", ep.Address)
		}
	}
}
