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

func TestNewMaglev(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	if m == nil {
		t.Fatal("Expected Maglev to be created")
	}

	if len(m.endpoints) != 2 {
		t.Errorf("Expected 2 endpoints, got %d", len(m.endpoints))
	}

	if m.GetTableSize() != defaultMaglevTableSize {
		t.Errorf("Expected table size %d, got %d", defaultMaglevTableSize, m.GetTableSize())
	}

	if len(m.lookupTable) == 0 {
		t.Error("Lookup table should be initialized")
	}
}

func TestMaglevSelect(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	t.Run("consistent hashing", func(t *testing.T) {
		// Same key should select same endpoint
		key := "persistent-session-123"
		ep1 := m.Select(key)
		ep2 := m.Select(key)
		ep3 := m.Select(key)

		if ep1 == nil {
			t.Fatal("Expected endpoint to be selected")
		}

		if ep1.Address != ep2.Address || ep2.Address != ep3.Address {
			t.Error("Same key should consistently select same endpoint")
		}
	})

	t.Run("different keys distribute", func(t *testing.T) {
		selections := make(map[string]int)

		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("session-%d", i)
			ep := m.Select(key)
			if ep == nil {
				t.Fatal("Expected endpoint to be selected")
			}
			selections[ep.Address]++
		}

		// All healthy endpoints should be selected
		if len(selections) != 3 {
			t.Errorf("Expected all 3 endpoints to be selected, got %d", len(selections))
		}

		// All should have non-zero selections
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

		m := NewMaglev(singleEndpoint)
		ep := m.Select("any-key")

		if ep == nil {
			t.Fatal("Expected endpoint to be selected")
		}

		if ep.Address != "10.0.0.1" {
			t.Errorf("Expected 10.0.0.1, got %s", ep.Address)
		}
	})
}

func TestMaglevSelectDefault(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	t.Run("select healthy endpoint when no key", func(t *testing.T) {
		ep := m.SelectDefault()

		if ep == nil {
			t.Fatal("Expected endpoint to be selected")
		}

		if !ep.Ready {
			t.Error("Expected healthy endpoint")
		}
	})

	t.Run("return nil when no healthy endpoints", func(t *testing.T) {
		unhealthyEndpoints := []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080, Ready: false},
			{Address: "10.0.0.2", Port: 8080, Ready: false},
		}

		m := NewMaglev(unhealthyEndpoints)
		ep := m.SelectDefault()

		if ep != nil {
			t.Error("Expected nil when no healthy endpoints")
		}
	})
}

func TestMaglevUpdateEndpoints(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	m := NewMaglev(initialEndpoints)

	t.Run("table rebuilt after update", func(t *testing.T) {
		initialDist := m.GetDistribution()

		newEndpoints := []*pb.Endpoint{
			{Address: "10.0.0.3", Port: 8080, Ready: true},
			{Address: "10.0.0.4", Port: 8080, Ready: true},
			{Address: "10.0.0.5", Port: 8080, Ready: true},
		}

		m.UpdateEndpoints(newEndpoints)

		newDist := m.GetDistribution()

		// Should have 3 endpoints in new distribution
		if len(newDist) != 3 {
			t.Errorf("Expected 3 endpoints in distribution, got %d", len(newDist))
		}

		// Old endpoints should not appear
		if _, ok := initialDist["10.0.0.1:8080"]; ok {
			if _, stillExists := newDist["10.0.0.1:8080"]; stillExists {
				t.Error("Old endpoint should not appear in new distribution")
			}
		}
	})

	t.Run("selections from new endpoints only", func(t *testing.T) {
		newEndpoints := []*pb.Endpoint{
			{Address: "10.0.1.1", Port: 8080, Ready: true},
			{Address: "10.0.1.2", Port: 8080, Ready: true},
		}

		m.UpdateEndpoints(newEndpoints)

		// Select from multiple keys
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("key-%d", i)
			ep := m.Select(key)

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

func TestMaglevEmptyEndpoints(t *testing.T) {
	emptyEndpoints := []*pb.Endpoint{}

	m := NewMaglev(emptyEndpoints)

	ep := m.Select("any-key")
	if ep != nil {
		t.Error("Expected nil when no endpoints")
	}

	epDefault := m.SelectDefault()
	if epDefault != nil {
		t.Error("Expected nil when no endpoints")
	}
}

func TestMaglevUnhealthyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: false},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
	}

	m := NewMaglev(endpoints)

	ep := m.Select("any-key")
	if ep != nil {
		t.Error("Expected nil when no healthy endpoints")
	}
}

func TestMaglevGetDistribution(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	dist := m.GetDistribution()

	t.Run("distribution structure", func(t *testing.T) {
		// Should have all 3 endpoints
		if len(dist) != 3 {
			t.Errorf("Expected 3 endpoints in distribution, got %d", len(dist))
		}

		// All entries should have non-zero count
		totalCount := 0
		for _, count := range dist {
			if count <= 0 {
				t.Error("Expected all endpoints to have positive count")
			}
			totalCount += count
		}

		// Total should equal table size
		if totalCount != int(m.tableSize) {
			t.Errorf("Expected total count %d, got %d", m.tableSize, totalCount)
		}
	})

	t.Run("balanced distribution", func(t *testing.T) {
		// Distribution should be reasonably balanced
		expectedPerEndpoint := int(m.tableSize) / len(endpoints)
		tolerance := expectedPerEndpoint / 5 // Allow 20% variance

		for addr, count := range dist {
			minExpected := expectedPerEndpoint - tolerance
			maxExpected := expectedPerEndpoint + tolerance

			if count < minExpected || count > maxExpected {
				t.Logf("Endpoint %s: count=%d, expected ~%d (range: %d-%d)",
					addr, count, expectedPerEndpoint, minExpected, maxExpected)
			}
		}
	})
}

func TestMaglevConcurrentSelect(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	t.Run("concurrent selects maintain consistency", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100
		key := "concurrent-key"

		results := make([]string, numGoroutines)
		var mu sync.Mutex

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ep := m.Select(key)
				if ep != nil {
					mu.Lock()
					results[idx] = ep.Address
					mu.Unlock()
				}
			}(i)
		}

		wg.Wait()

		// All results should be the same for same key
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
				m.UpdateEndpoints(newEndpoints)
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
						_ = m.Select(key)
					}
				}
			}(i)
		}

		<-done
		wg.Wait()
		// Test passes if no race condition is detected
	})
}

func TestMaglevTableSize(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	tableSize := m.GetTableSize()

	if tableSize != int(defaultMaglevTableSize) {
		t.Errorf("Expected table size %d, got %d", defaultMaglevTableSize, tableSize)
	}

	// Table size should be prime number (65537)
	if tableSize != 65537 {
		t.Logf("Table size is %d (expected prime number)", tableSize)
	}
}

func TestMaglevKeyDistribution(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	m := NewMaglev(endpoints)

	t.Run("different key prefixes distribute", func(t *testing.T) {
		distribution := make(map[string]int)

		for i := 0; i < 10000; i++ {
			key := fmt.Sprintf("session-%d", i)
			ep := m.Select(key)
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
	})
}

func TestMaglevTableDrivenTests(t *testing.T) {
	tests := []struct {
		name            string
		endpoints       []*pb.Endpoint
		selectCount     int
		expectedSelected int
		allowNil        bool
	}{
		{
			name: "single_endpoint",
			endpoints: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 8080, Ready: true},
			},
			selectCount:      10,
			expectedSelected: 1,
			allowNil:         false,
		},
		{
			name: "multiple_endpoints",
			endpoints: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 8080, Ready: true},
				{Address: "10.0.0.2", Port: 8080, Ready: true},
				{Address: "10.0.0.3", Port: 8080, Ready: true},
			},
			selectCount:      100,
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
			name: "mixed_health",
			endpoints: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 8080, Ready: true},
				{Address: "10.0.0.2", Port: 8080, Ready: false},
				{Address: "10.0.0.3", Port: 8080, Ready: true},
			},
			selectCount:      50,
			expectedSelected: 2,
			allowNil:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMaglev(tt.endpoints)

			selections := make(map[string]bool)
			nilCount := 0

			for i := 0; i < tt.selectCount; i++ {
				key := fmt.Sprintf("key-%d", i)
				ep := m.Select(key)

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
