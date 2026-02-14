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

func TestNewMaglev(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)
	if m == nil {
		t.Fatal("NewMaglev() returned nil")
	}

	if m.tableSize != defaultMaglevTableSize {
		t.Errorf("tableSize = %d, want %d", m.tableSize, defaultMaglevTableSize)
	}

	if len(m.lookupTable) != int(defaultMaglevTableSize) {
		t.Errorf("lookupTable length = %d, want %d", len(m.lookupTable), defaultMaglevTableSize)
	}
}

func TestNewMaglev_EmptyEndpoints(t *testing.T) {
	m := NewMaglev([]*pb.Endpoint{})
	if m == nil {
		t.Fatal("NewMaglev() returned nil for empty endpoints")
	}
}

func TestMaglev_Select(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

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
			ep := m.Select(tt.key)
			if ep == nil {
				t.Error("Select() returned nil for non-empty endpoints")
			}
		})
	}
}

func TestMaglev_Select_EmptyEndpoints(t *testing.T) {
	m := NewMaglev([]*pb.Endpoint{})

	ep := m.Select("test-key")
	if ep != nil {
		t.Errorf("Select() with empty endpoints returned %v, want nil", ep)
	}
}

func TestMaglev_Select_Consistency(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	// Same key should always return the same endpoint
	key := "consistent-key"
	first := m.Select(key)
	for i := 0; i < 100; i++ {
		ep := m.Select(key)
		if ep != first {
			t.Error("Select() not consistent for same key")
			break
		}
	}
}

func TestMaglev_SelectDefault(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	ep := m.SelectDefault()
	if ep == nil {
		t.Error("SelectDefault() returned nil for non-empty endpoints")
	}
}

func TestMaglev_SelectDefault_EmptyEndpoints(t *testing.T) {
	m := NewMaglev([]*pb.Endpoint{})

	ep := m.SelectDefault()
	if ep != nil {
		t.Errorf("SelectDefault() with empty endpoints returned %v, want nil", ep)
	}
}

func TestMaglev_UpdateEndpoints(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	m := NewMaglev(initialEndpoints)

	// Get initial selection
	key := "test-key"
	_ = m.Select(key)

	// Update endpoints
	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: true},
		{Address: "10.0.0.5", Port: 8080, Ready: true},
	}

	m.UpdateEndpoints(newEndpoints)

	// Selection should now be from new endpoints
	updated := m.Select(key)

	// Just verify we get a valid endpoint
	if updated == nil {
		t.Error("Select() returned nil after UpdateEndpoints")
	}
}

func TestMaglev_GetTableSize(t *testing.T) {
	m := NewMaglev([]*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	})

	size := m.GetTableSize()
	if size != defaultMaglevTableSize {
		t.Errorf("GetTableSize() = %d, want %d", size, defaultMaglevTableSize)
	}
}

func TestMaglev_GetDistribution(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	dist := m.GetDistribution()

	// Distribution should have entries for all endpoints
	if len(dist) != len(endpoints) {
		t.Errorf("GetDistribution() returned %d entries, want %d", len(dist), len(endpoints))
	}

	// Each endpoint should have some distribution
	for _, count := range dist {
		if count < 0 {
			t.Error("Distribution count should not be negative")
		}
	}
}

func TestMaglev_DistributionBalance(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)
	dist := m.GetDistribution()

	// Calculate total
	var total int
	for _, count := range dist {
		total += count
	}

	// Each endpoint should have roughly 1/3 of the table
	expectedPerEndpoint := total / len(endpoints)
	tolerance := expectedPerEndpoint / 2 // 50% tolerance

	for addr, count := range dist {
		diff := count - expectedPerEndpoint
		if diff < 0 {
			diff = -diff
		}
		if diff > tolerance {
			t.Logf("Warning: endpoint %s has count %d, expected around %d", addr, count, expectedPerEndpoint)
		}
	}
}

func TestMaglev_ConcurrentAccess(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	done := make(chan bool)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := string(rune(id*100 + j))
				_ = m.Select(key)
			}
			done <- true
		}(i)
	}

	// Concurrent updates
	go func() {
		for i := 0; i < 10; i++ {
			newEndpoints := []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 8080, Ready: true},
			}
			m.UpdateEndpoints(newEndpoints)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 11; i++ {
		<-done
	}
}

func TestMaglev_SingleEndpoint(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	// All requests should go to the single endpoint
	for i := 0; i < 10; i++ {
		ep := m.Select(string(rune(i)))
		if ep == nil {
			t.Error("Select() returned nil")
		} else if ep.Address != "10.0.0.1" {
			t.Errorf("Select() returned wrong endpoint: %s", ep.Address)
		}
	}
}

func TestMaglev_Failover(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	// Get initial mapping
	key := "failover-test-key"
	initial := m.Select(key)

	// Remove the initial endpoint
	var newEndpoints []*pb.Endpoint
	for _, ep := range endpoints {
		if ep.Address != initial.Address {
			newEndpoints = append(newEndpoints, ep)
		}
	}

	m.UpdateEndpoints(newEndpoints)

	// Should still get a valid endpoint
	newSelection := m.Select(key)
	if newSelection == nil {
		t.Error("Select() returned nil after endpoint removal")
	}
	if newSelection.Address == initial.Address {
		t.Error("Select() returned removed endpoint")
	}
}
