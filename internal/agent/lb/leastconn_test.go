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
	"sync"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestLeastConnSelect(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	lc := NewLeastConn(endpoints)

	// With no active connections, all endpoints should be selected
	selections := make(map[string]int)
	for i := 0; i < 300; i++ {
		ep := lc.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// All endpoints should be selected (random among ties)
	for _, ep := range endpoints {
		if selections[ep.Address] == 0 {
			t.Errorf("Endpoint %s was never selected", ep.Address)
		}
	}
}

func TestLeastConnSelectsFewestConnections(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	lc := NewLeastConn(endpoints)

	// Add connections: ep1=5, ep2=2, ep3=8
	for i := 0; i < 5; i++ {
		lc.IncrementActive(endpoints[0])
	}
	for i := 0; i < 2; i++ {
		lc.IncrementActive(endpoints[1])
	}
	for i := 0; i < 8; i++ {
		lc.IncrementActive(endpoints[2])
	}

	// Should always select endpoint with fewest connections (10.0.0.2)
	for i := 0; i < 100; i++ {
		ep := lc.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		if ep.Address != "10.0.0.2" {
			t.Errorf("Expected 10.0.0.2 (2 conns), got %s", ep.Address)
		}
	}
}

func TestLeastConnIncrementDecrement(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	lc := NewLeastConn(endpoints)

	// Increment
	lc.IncrementActive(endpoints[0])
	lc.IncrementActive(endpoints[0])
	lc.IncrementActive(endpoints[0])

	count := lc.GetActiveCount(endpoints[0])
	if count != 3 {
		t.Errorf("Expected 3 active connections, got %d", count)
	}

	// Decrement
	lc.DecrementActive(endpoints[0])
	count = lc.GetActiveCount(endpoints[0])
	if count != 2 {
		t.Errorf("Expected 2 active connections after decrement, got %d", count)
	}
}

func TestLeastConnWithUnhealthyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	lc := NewLeastConn(endpoints)

	// Should never select unhealthy endpoint
	for i := 0; i < 100; i++ {
		ep := lc.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		if ep.Address == "10.0.0.2" {
			t.Error("Selected unhealthy endpoint")
		}
	}
}

func TestLeastConnNoHealthyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: false},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
	}

	lc := NewLeastConn(endpoints)

	ep := lc.Select()
	if ep != nil {
		t.Error("Expected nil when no healthy endpoints")
	}
}

func TestLeastConnUpdateEndpoints(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	lc := NewLeastConn(initialEndpoints)

	// Add some connections to ep1
	lc.IncrementActive(initialEndpoints[0])
	lc.IncrementActive(initialEndpoints[0])

	// Update endpoints - ep1 remains, ep2 is removed, ep3 is added
	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	lc.UpdateEndpoints(newEndpoints)

	// ep1 should still have its connection count preserved
	count := lc.GetActiveCount(newEndpoints[0])
	if count != 2 {
		t.Errorf("Expected preserved count of 2 for ep1, got %d", count)
	}

	// ep3 should have 0 connections
	count = lc.GetActiveCount(newEndpoints[1])
	if count != 0 {
		t.Errorf("Expected 0 for new ep3, got %d", count)
	}

	// Should prefer ep3 (0 connections) over ep1 (2 connections)
	for i := 0; i < 50; i++ {
		ep := lc.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		if ep.Address != "10.0.0.3" {
			t.Errorf("Expected 10.0.0.3 (0 conns), got %s", ep.Address)
		}
	}
}

func TestLeastConnConcurrentAccess(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	lc := NewLeastConn(endpoints)

	// Run concurrent selections and increments/decrements
	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				ep := lc.Select()
				if ep != nil {
					lc.IncrementActive(ep)
					lc.DecrementActive(ep)
				}
			}
		}()
	}

	wg.Wait()

	// All connection counts should be back to 0
	for _, ep := range endpoints {
		count := lc.GetActiveCount(ep)
		if count != 0 {
			t.Errorf("Endpoint %s has %d active connections after concurrent test, expected 0",
				ep.Address, count)
		}
	}
}

func TestLeastConnNilEndpoint(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	lc := NewLeastConn(endpoints)

	// These should not panic
	lc.IncrementActive(nil)
	lc.DecrementActive(nil)

	count := lc.GetActiveCount(nil)
	if count != 0 {
		t.Errorf("Expected 0 for nil endpoint, got %d", count)
	}
}

func TestLeastConnSingleEndpoint(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	lc := NewLeastConn(endpoints)

	// With a single endpoint, should always return it
	for i := 0; i < 50; i++ {
		ep := lc.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		if ep.Address != "10.0.0.1" {
			t.Errorf("Expected 10.0.0.1, got %s", ep.Address)
		}
	}
}

func TestLeastConnFairDistribution(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	lc := NewLeastConn(endpoints)

	// Simulate realistic traffic: select endpoint, increment, process, decrement
	selections := make(map[string]int)
	const totalRequests = 300

	for i := 0; i < totalRequests; i++ {
		ep := lc.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
		lc.IncrementActive(ep)

		// Simulate: every 3rd request completes for a random previously-selected endpoint
		if i%3 == 0 && i > 0 {
			lc.DecrementActive(ep)
		}
	}

	// With least-connections, distribution should be reasonably fair
	// Each endpoint should get at least 20% of total requests
	minExpected := totalRequests / 5
	for addr, count := range selections {
		if count < minExpected {
			t.Errorf("Endpoint %s only got %d/%d requests, expected at least %d",
				addr, count, totalRequests, minExpected)
		}
	}
}
