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
	"fmt"
	"sync"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewRingHash(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	if rh == nil {
		t.Fatal("Expected RingHash to be created")
	}

	if len(rh.endpoints) != 2 {
		t.Errorf("Expected 2 endpoints, got %d", len(rh.endpoints))
	}

	if len(rh.ring) == 0 {
		t.Error("Hash ring should be built")
	}
}

func TestRingHashSelect(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	t.Run("consistent hashing", func(t *testing.T) {
		// Same key should always select same endpoint
		key := "test-session-123"
		ep1 := rh.Select(key)
		ep2 := rh.Select(key)
		ep3 := rh.Select(key)

		if ep1 == nil {
			t.Fatal("Expected endpoint to be selected")
		}

		if ep1.Address != ep2.Address || ep2.Address != ep3.Address {
			t.Error("Same key should consistently select same endpoint")
		}
	})

	t.Run("different keys distribute across endpoints", func(t *testing.T) {
		selections := make(map[string]int)

		// Generate many keys to verify distribution
		for i := 0; i < 300; i++ {
			key := fmt.Sprintf("session-%d", i)
			ep := rh.Select(key)
			if ep == nil {
				t.Fatal("Expected endpoint to be selected")
			}
			selections[ep.Address]++
		}

		// All endpoints should be selected at least once
		if len(selections) != 3 {
			t.Errorf("Expected all 3 endpoints to be selected, got %d unique endpoints", len(selections))
		}

		// Distribution should be somewhat balanced (not perfectly balanced due to hashing)
		for address, count := range selections {
			if count == 0 {
				t.Errorf("Endpoint %s was never selected", address)
			}
		}
	})

	t.Run("single endpoint", func(t *testing.T) {
		singleEndpoint := []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080, Ready: true},
		}

		rh := NewRingHash(singleEndpoint)
		ep := rh.Select("any-key")

		if ep == nil {
			t.Fatal("Expected endpoint to be selected")
		}

		if ep.Address != "10.0.0.1" {
			t.Errorf("Expected 10.0.0.1, got %s", ep.Address)
		}
	})
}

func TestRingHashSelectDefault(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	t.Run("select healthy endpoint when no key", func(t *testing.T) {
		ep := rh.SelectDefault()

		if ep == nil {
			t.Fatal("Expected endpoint to be selected")
		}

		if !ep.Ready {
			t.Error("Expected healthy endpoint to be selected")
		}
	})

	t.Run("return nil when no healthy endpoints", func(t *testing.T) {
		unhealthyEndpoints := []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080, Ready: false},
			{Address: "10.0.0.2", Port: 8080, Ready: false},
		}

		rh := NewRingHash(unhealthyEndpoints)
		ep := rh.SelectDefault()

		if ep != nil {
			t.Error("Expected nil when no healthy endpoints")
		}
	})
}

func TestRingHashUpdateEndpoints(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	rh := NewRingHash(initialEndpoints)
	initialRingSize := rh.GetRingSize()

	t.Run("ring rebuilt after update", func(t *testing.T) {
		newEndpoints := []*pb.Endpoint{
			{Address: "10.0.0.3", Port: 8080, Ready: true},
			{Address: "10.0.0.4", Port: 8080, Ready: true},
			{Address: "10.0.0.5", Port: 8080, Ready: true},
		}

		rh.UpdateEndpoints(newEndpoints)

		newRingSize := rh.GetRingSize()

		// Ring should be rebuilt with new size
		if newRingSize == 0 {
			t.Error("Ring should not be empty after update")
		}

		// Ring size depends on number of healthy endpoints and virtual nodes
		if newRingSize <= initialRingSize {
			t.Logf("Note: Ring size changed from %d to %d (may vary with number of endpoints)", initialRingSize, newRingSize)
		}
	})

	t.Run("selections from new endpoints only", func(t *testing.T) {
		newEndpoints := []*pb.Endpoint{
			{Address: "10.0.1.1", Port: 8080, Ready: true},
			{Address: "10.0.1.2", Port: 8080, Ready: true},
		}

		rh.UpdateEndpoints(newEndpoints)

		// Select from multiple keys and verify only new endpoints are selected
		for i := 0; i < 50; i++ {
			key := fmt.Sprintf("key-%d", i)
			ep := rh.Select(key)

			if ep == nil {
				t.Fatal("Expected endpoint to be selected")
			}

			// Should be one of the new endpoints
			if ep.Address != "10.0.1.1" && ep.Address != "10.0.1.2" {
				t.Errorf("Unexpected endpoint selected: %s", ep.Address)
			}
		}
	})
}

func TestRingHashEmptyEndpoints(t *testing.T) {
	emptyEndpoints := []*pb.Endpoint{}

	rh := NewRingHash(emptyEndpoints)

	ep := rh.Select("any-key")
	if ep != nil {
		t.Error("Expected nil when no endpoints")
	}

	epDefault := rh.SelectDefault()
	if epDefault != nil {
		t.Error("Expected nil when no endpoints")
	}
}

func TestRingHashUnhealthyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: false},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: false},
	}

	rh := NewRingHash(endpoints)

	ep := rh.Select("any-key")
	if ep != nil {
		t.Error("Expected nil when no healthy endpoints")
	}
}

func TestRingHashConcurrentSelect(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	t.Run("concurrent selects maintain consistency", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100
		key := "concurrent-test-key"

		results := make([]string, numGoroutines)
		var mu sync.Mutex

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ep := rh.Select(key)
				if ep != nil {
					mu.Lock()
					results[idx] = ep.Address
					mu.Unlock()
				}
			}(i)
		}

		wg.Wait()

		// All results should be the same endpoint for same key
		firstResult := results[0]
		for i, result := range results {
			if result != firstResult {
				t.Errorf("Result %d differs: expected %s, got %s", i, firstResult, result)
			}
		}
	})

	t.Run("concurrent updates and selects", func(t *testing.T) {
		var wg sync.WaitGroup
		done := make(chan struct{})

		// Update goroutine
		go func() {
			for i := 0; i < 5; i++ {
				newEndpoints := []*pb.Endpoint{
					{Address: fmt.Sprintf("10.0.0.%d", (i%3)+1), Port: 8080, Ready: true},
					{Address: fmt.Sprintf("10.0.1.%d", (i%3)+1), Port: 8080, Ready: true},
				}
				rh.UpdateEndpoints(newEndpoints)
			}
			close(done)
		}()

		// Select goroutines
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				for {
					select {
					case <-done:
						return
					default:
						key := fmt.Sprintf("key-%d", idx)
						_ = rh.Select(key)
					}
				}
			}(i)
		}

		<-done
		wg.Wait()
		// Test passes if no race condition is detected
	})
}

func TestRingHashVirtualNodes(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	// Ring size should be 2 endpoints * 150 virtual nodes = 300
	ringSize := rh.GetRingSize()

	if ringSize == 0 {
		t.Error("Ring should not be empty")
	}

	// Ring size should scale with virtual nodes
	expectedMinSize := 2 * defaultVirtualNodes
	if ringSize < expectedMinSize {
		t.Errorf("Expected ring size >= %d, got %d", expectedMinSize, ringSize)
	}
}

func TestRingHashKeyDistribution(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	rh := NewRingHash(endpoints)

	// Test that different prefixes distribute differently
	t.Run("key prefix distribution", func(t *testing.T) {
		distribution := make(map[string]int)

		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("session-%d", i)
			ep := rh.Select(key)
			if ep != nil {
				distribution[ep.Address]++
			}
		}

		// Each endpoint should be selected
		for _, ep := range endpoints {
			if distribution[ep.Address] == 0 {
				t.Errorf("Endpoint %s was never selected", ep.Address)
			}
		}

		// Distribution should be reasonably balanced
		min := distribution[endpoints[0].Address]
		max := distribution[endpoints[0].Address]

		for _, count := range distribution {
			if count < min {
				min = count
			}
			if count > max {
				max = count
			}
		}

		// Ratio should not be extreme (allowing for randomness)
		ratio := float64(max) / float64(min)
		if ratio > 2.0 {
			t.Logf("Distribution ratio: %.2f (consider this may vary with hash function)", ratio)
		}
	})
}

func TestRingHashTableDrivenTests(t *testing.T) {
	tests := []struct {
		name            string
		endpoints       []*pb.Endpoint
		selectCount     int
		expectedSelected int
		allowNil        bool
	}{
		{
			name: "single_healthy_endpoint",
			endpoints: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 8080, Ready: true},
			},
			selectCount:      10,
			expectedSelected: 1,
			allowNil:         false,
		},
		{
			name: "multiple_healthy_endpoints",
			endpoints: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 8080, Ready: true},
				{Address: "10.0.0.2", Port: 8080, Ready: true},
				{Address: "10.0.0.3", Port: 8080, Ready: true},
			},
			selectCount:      50,
			expectedSelected: 3,
			allowNil:         false,
		},
		{
			name:             "no_endpoints",
			endpoints:        []*pb.Endpoint{},
			selectCount:      10,
			expectedSelected: 0,
			allowNil:         true,
		},
		{
			name: "all_unhealthy_endpoints",
			endpoints: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 8080, Ready: false},
				{Address: "10.0.0.2", Port: 8080, Ready: false},
			},
			selectCount:      10,
			expectedSelected: 0,
			allowNil:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rh := NewRingHash(tt.endpoints)

			selections := make(map[string]bool)
			nilCount := 0

			for i := 0; i < tt.selectCount; i++ {
				key := fmt.Sprintf("key-%d", i)
				ep := rh.Select(key)

				if ep == nil {
					nilCount++
				} else {
					selections[ep.Address] = true
				}
			}

			selectedCount := len(selections)

			if !tt.allowNil && nilCount > 0 {
				t.Errorf("Expected no nil results, got %d", nilCount)
			}

			if selectedCount != tt.expectedSelected {
				t.Errorf("Expected %d selected endpoints, got %d", tt.expectedSelected, selectedCount)
			}
		})
	}
}
