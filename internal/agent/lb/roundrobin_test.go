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

func TestRoundRobinSelect(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	lb := NewRoundRobin(endpoints)

	// Test round-robin distribution
	selections := make(map[string]int)
	totalSelections := 300

	for i := 0; i < totalSelections; i++ {
		ep := lb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// Each endpoint should be selected roughly equally (100 times each)
	expectedPerEndpoint := totalSelections / len(endpoints)
	for address, count := range selections {
		if count != expectedPerEndpoint {
			t.Errorf("Endpoint %s selected %d times, expected %d", address, count, expectedPerEndpoint)
		}
	}
}

func TestRoundRobinWithUnhealthyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false}, // Unhealthy
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	lb := NewRoundRobin(endpoints)

	// Should only select healthy endpoints
	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		ep := lb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		if !ep.Ready {
			t.Errorf("Selected unhealthy endpoint: %s", ep.Address)
		}
		selections[ep.Address]++
	}

	// Should never select the unhealthy endpoint
	if selections["10.0.0.2"] > 0 {
		t.Error("Unhealthy endpoint was selected")
	}

	// Should select other two endpoints
	if selections["10.0.0.1"] == 0 || selections["10.0.0.3"] == 0 {
		t.Error("Not all healthy endpoints were selected")
	}
}

func TestRoundRobinNoHealthyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: false},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
	}

	lb := NewRoundRobin(endpoints)

	// Should return nil when no healthy endpoints
	ep := lb.Select()
	if ep != nil {
		t.Error("Expected nil when no healthy endpoints")
	}
}

func TestRoundRobinUpdateEndpoints(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	lb := NewRoundRobin(initialEndpoints)

	// Select a few times
	for i := 0; i < 5; i++ {
		lb.Select()
	}

	// Update endpoints
	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: true},
		{Address: "10.0.0.5", Port: 8080, Ready: true},
	}

	lb.UpdateEndpoints(newEndpoints)

	// Should now select from new endpoints only
	selections := make(map[string]int)
	for i := 0; i < 30; i++ {
		ep := lb.Select()
		if ep == nil {
			t.Fatal("Select returned nil after update")
		}
		selections[ep.Address]++
	}

	// Should never select old endpoints
	if selections["10.0.0.1"] > 0 || selections["10.0.0.2"] > 0 {
		t.Error("Old endpoints were selected after update")
	}

	// Should select new endpoints
	if len(selections) != 3 {
		t.Errorf("Expected 3 different endpoints to be selected, got %d", len(selections))
	}
}

func TestFilterHealthy(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: false},
	}

	healthy := filterHealthy(endpoints)

	if len(healthy) != 2 {
		t.Errorf("Expected 2 healthy endpoints, got %d", len(healthy))
	}

	for _, ep := range healthy {
		if !ep.Ready {
			t.Errorf("Unhealthy endpoint in filtered list: %s", ep.Address)
		}
	}
}
