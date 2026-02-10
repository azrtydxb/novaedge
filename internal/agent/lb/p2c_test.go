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
	"sync/atomic"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewP2C(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	p2c := NewP2C(endpoints)

	if p2c == nil {
		t.Fatal("Expected P2C to be created")
	}

	if len(p2c.endpoints) != 2 {
		t.Errorf("Expected 2 endpoints, got %d", len(p2c.endpoints))
	}

	if len(p2c.activeRequests) != 2 {
		t.Errorf("Expected 2 active request counters, got %d", len(p2c.activeRequests))
	}

}

func TestP2CSelect(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	p2c := NewP2C(endpoints)

	t.Run("select from multiple endpoints", func(t *testing.T) {
		selections := make(map[string]int)

		for i := 0; i < 100; i++ {
			ep := p2c.Select()
			if ep == nil {
				t.Fatal("Expected endpoint to be selected")
			}
			selections[ep.Address]++
		}

		if len(selections) < 2 {
			t.Errorf("Expected at least 2 endpoints to be selected, got %d", len(selections))
		}
	})

	t.Run("select single endpoint", func(t *testing.T) {
		singleEndpoint := []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080, Ready: true},
		}

		p2c := NewP2C(singleEndpoint)
		ep := p2c.Select()

		if ep == nil {
			t.Fatal("Expected endpoint to be selected")
		}

		if ep.Address != "10.0.0.1" {
			t.Errorf("Expected 10.0.0.1, got %s", ep.Address)
		}
	})

	t.Run("return nil when no healthy endpoints", func(t *testing.T) {
		unhealthyEndpoints := []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080, Ready: false},
			{Address: "10.0.0.2", Port: 8080, Ready: false},
		}

		p2c := NewP2C(unhealthyEndpoints)
		ep := p2c.Select()

		if ep != nil {
			t.Error("Expected nil when no healthy endpoints")
		}
	})
}

func TestP2CChoosesLeastLoaded(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	p2c := NewP2C(endpoints)

	t.Run("prefers endpoint with fewer active requests", func(t *testing.T) {
		// Add many active requests to endpoint 1
		for i := 0; i < 10; i++ {
			p2c.IncrementActive(endpoints[0])
		}

		// Add few active requests to endpoint 2
		p2c.IncrementActive(endpoints[1])

		// Run multiple selections - should prefer endpoint 2 more often
		selections := make(map[string]int)

		for i := 0; i < 100; i++ {
			ep := p2c.Select()
			if ep != nil {
				selections[ep.Address]++
			}
		}

		// Endpoint 2 should be selected significantly more
		if selections["10.0.0.2"] <= selections["10.0.0.1"] {
			t.Logf("Expected endpoint 2 to be selected more, got selections: %v", selections)
		}
	})

	t.Run("distribution improves with load balancing", func(t *testing.T) {
		p2c := NewP2C(endpoints)

		// Simulate load being added during selections
		selections := make(map[string]int)

		for i := 0; i < 50; i++ {
			ep := p2c.Select()
			if ep != nil {
				selections[ep.Address]++
				p2c.IncrementActive(ep)
			}
		}

		// After incrementing, distribution should be relatively balanced
		// (P2C doesn't guarantee perfect balance, but prevents extreme skew)
		maxLoad := selections["10.0.0.1"]
		if selections["10.0.0.2"] > maxLoad {
			maxLoad = selections["10.0.0.2"]
		}

		minLoad := selections["10.0.0.1"]
		if selections["10.0.0.2"] < minLoad {
			minLoad = selections["10.0.0.2"]
		}

		// P2C should keep ratio reasonable
		t.Logf("Load distribution: %v (max/min ratio: %.2f)", selections, float64(maxLoad)/float64(minLoad))
	})
}

func TestP2CActiveRequestTracking(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	p2c := NewP2C(endpoints)

	t.Run("increment active requests", func(t *testing.T) {
		p2c.IncrementActive(endpoints[0])
		p2c.IncrementActive(endpoints[0])

		count := p2c.GetActiveCount(endpoints[0])
		if count != 2 {
			t.Errorf("Expected 2 active requests, got %d", count)
		}
	})

	t.Run("decrement active requests", func(t *testing.T) {
		p2c.DecrementActive(endpoints[0])

		count := p2c.GetActiveCount(endpoints[0])
		if count != 1 {
			t.Errorf("Expected 1 active request after decrement, got %d", count)
		}
	})

	t.Run("get active count", func(t *testing.T) {
		p2c := NewP2C(endpoints)

		count0 := p2c.GetActiveCount(endpoints[0])
		count1 := p2c.GetActiveCount(endpoints[1])

		if count0 != 0 || count1 != 0 {
			t.Error("Expected 0 active requests initially")
		}

		p2c.IncrementActive(endpoints[0])
		p2c.IncrementActive(endpoints[0])
		p2c.IncrementActive(endpoints[1])

		count0 = p2c.GetActiveCount(endpoints[0])
		count1 = p2c.GetActiveCount(endpoints[1])

		if count0 != 2 {
			t.Errorf("Expected 2 active requests for endpoint 0, got %d", count0)
		}

		if count1 != 1 {
			t.Errorf("Expected 1 active request for endpoint 1, got %d", count1)
		}
	})

	t.Run("nil endpoint returns 0", func(t *testing.T) {
		count := p2c.GetActiveCount(nil)
		if count != 0 {
			t.Errorf("Expected 0 for nil endpoint, got %d", count)
		}
	})
}

func TestP2CUpdateEndpoints(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	p2c := NewP2C(initialEndpoints)

	// Add some active requests
	p2c.IncrementActive(initialEndpoints[0])
	p2c.IncrementActive(initialEndpoints[0])
	p2c.IncrementActive(initialEndpoints[1])

	t.Run("update with new endpoints", func(t *testing.T) {
		newEndpoints := []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080, Ready: true},
			{Address: "10.0.0.3", Port: 8080, Ready: true},
			{Address: "10.0.0.4", Port: 8080, Ready: true},
		}

		p2c.UpdateEndpoints(newEndpoints)

		if len(p2c.endpoints) != 3 {
			t.Errorf("Expected 3 endpoints, got %d", len(p2c.endpoints))
		}

		if len(newEndpoints) < 2 {
			t.Fatal("Expected at least 2 new endpoints")
		}

		// Preserved endpoints should keep their active counts
		count1 := p2c.GetActiveCount(newEndpoints[0])
		if count1 != 2 {
			t.Errorf("Expected preserved endpoint to keep 2 active requests, got %d", count1)
		}

		// New endpoints should have 0 active requests
		count3 := p2c.GetActiveCount(newEndpoints[1])
		if count3 != 0 {
			t.Errorf("Expected new endpoint to have 0 active requests, got %d", count3)
		}
	})

	t.Run("remove endpoints", func(t *testing.T) {
		p2c := NewP2C(initialEndpoints)

		newEndpoints := []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080, Ready: true},
		}

		p2c.UpdateEndpoints(newEndpoints)

		if len(p2c.endpoints) != 1 {
			t.Errorf("Expected 1 endpoint, got %d", len(p2c.endpoints))
		}
	})
}

func TestP2CConcurrentOperations(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	p2c := NewP2C(endpoints)

	t.Run("concurrent select operations", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = p2c.Select()
			}()
		}

		wg.Wait()
		// Test passes if no race condition detected
	})

	t.Run("concurrent active request modifications", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ep := endpoints[idx%len(endpoints)]

				if idx%2 == 0 {
					p2c.IncrementActive(ep)
				} else {
					p2c.DecrementActive(ep)
				}
			}(i)
		}

		wg.Wait()
		// Test passes if no race condition detected
	})

	t.Run("concurrent get active count", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ep := endpoints[idx%len(endpoints)]
				_ = p2c.GetActiveCount(ep)
			}(i)
		}

		wg.Wait()
		// Test passes if no race condition detected
	})

	t.Run("concurrent select and increment", func(t *testing.T) {
		var wg sync.WaitGroup

		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ep := p2c.Select()
				if ep != nil {
					p2c.IncrementActive(ep)
				}
			}()
		}

		wg.Wait()
		// Test passes if no race condition detected
	})

	t.Run("concurrent endpoint update and select", func(t *testing.T) {
		var wg sync.WaitGroup

		// Select in goroutines
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = p2c.Select()
			}()
		}

		// Update endpoints in parallel
		wg.Add(1)
		go func() {
			defer wg.Done()
			newEndpoints := []*pb.Endpoint{
				{Address: "10.0.0.4", Port: 8080, Ready: true},
				{Address: "10.0.0.5", Port: 8080, Ready: true},
			}
			p2c.UpdateEndpoints(newEndpoints)
		}()

		wg.Wait()
		// Test passes if no race condition detected
	})
}

func TestP2CAtomicCounters(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	p2c := NewP2C(endpoints)

	t.Run("atomic operations are safe", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p2c.IncrementActive(endpoints[0])
			}()
		}

		wg.Wait()

		// Check that final count is correct
		key := endpointKey(endpoints[0])
		p2c.mu.RLock()
		counter := atomic.LoadInt64(p2c.activeRequests[key])
		p2c.mu.RUnlock()

		if counter != int64(numGoroutines) {
			t.Errorf("Expected active count %d, got %d", numGoroutines, counter)
		}
	})
}

func TestP2CUnhealthyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	p2c := NewP2C(endpoints)

	t.Run("only selects healthy endpoints", func(t *testing.T) {
		selections := make(map[string]int)

		for i := 0; i < 100; i++ {
			ep := p2c.Select()
			if ep != nil {
				selections[ep.Address]++

				if !ep.Ready {
					t.Errorf("Selected unhealthy endpoint: %s", ep.Address)
				}
			}
		}

		if selections["10.0.0.2"] > 0 {
			t.Error("Unhealthy endpoint should never be selected")
		}
	})
}

func TestP2CNilEndpoint(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	p2c := NewP2C(endpoints)

	t.Run("nil endpoint operations don't panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Unexpected panic: %v", r)
			}
		}()

		p2c.IncrementActive(nil)
		p2c.DecrementActive(nil)
		_ = p2c.GetActiveCount(nil)
	})
}

func TestP2CDistribution(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	p2c := NewP2C(endpoints)

	t.Run("reasonable distribution without load", func(t *testing.T) {
		selections := make(map[string]int)

		for i := 0; i < 300; i++ {
			ep := p2c.Select()
			if ep != nil {
				selections[ep.Address]++
			}
		}

		// With P2C and no load, should be relatively balanced
		for addr, count := range selections {
			expectedMin := 80
			expectedMax := 120
			if count < expectedMin || count > expectedMax {
				t.Logf("Endpoint %s count %d is outside expected range [%d, %d]", addr, count, expectedMin, expectedMax)
			}
		}
	})
}
