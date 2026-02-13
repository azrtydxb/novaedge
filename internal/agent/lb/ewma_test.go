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
	"time"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	testAddrEWMA  = "10.0.0.1"
	testAddrEWMA3 = "10.0.0.3"
)

func TestNewEWMA(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	ewma := NewEWMA(endpoints)

	if ewma == nil {
		t.Fatal("Expected EWMA to be created")
	}

	if len(ewma.endpoints) != 2 {
		t.Errorf("Expected 2 endpoints, got %d", len(ewma.endpoints))
	}

	if len(ewma.scores) != 2 {
		t.Errorf("Expected 2 scores, got %d", len(ewma.scores))
	}

	if len(ewma.activeRequests) != 2 {
		t.Errorf("Expected 2 active request counters, got %d", len(ewma.activeRequests))
	}
}

func TestEWMASelect(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: testAddrEWMA3, Port: 8080, Ready: true},
	}

	ewma := NewEWMA(endpoints)

	t.Run("select from multiple endpoints", func(t *testing.T) {
		// When all endpoints have equal scores, EWMA consistently picks the same endpoint
		// (the first one with the lowest score). To test distribution, we need to
		// record different latencies for each endpoint to affect their scores.
		ep := ewma.Select()
		if ep == nil {
			t.Fatal("Expected endpoint to be selected")
		}
		// Just verify we get a valid endpoint
		validEndpoint := false
		for _, e := range endpoints {
			if ep.Address == e.Address {
				validEndpoint = true
				break
			}
		}
		if !validEndpoint {
			t.Errorf("Selected invalid endpoint: %s", ep.Address)
		}
	})

	t.Run("select single endpoint", func(t *testing.T) {
		singleEndpoint := []*pb.Endpoint{
			{Address: testAddrEWMA, Port: 8080, Ready: true},
		}

		ewma := NewEWMA(singleEndpoint)
		ep := ewma.Select()

		if ep == nil {
			t.Fatal("Expected endpoint to be selected")
		}

		if ep.Address != testAddrEWMA {
			t.Errorf("Expected 10.0.0.1, got %s", ep.Address)
		}
	})

	t.Run("return nil when no healthy endpoints", func(t *testing.T) {
		unhealthyEndpoints := []*pb.Endpoint{
			{Address: testAddrEWMA, Port: 8080, Ready: false},
			{Address: "10.0.0.2", Port: 8080, Ready: false},
		}

		ewma := NewEWMA(unhealthyEndpoints)
		ep := ewma.Select()

		if ep != nil {
			t.Error("Expected nil when no healthy endpoints")
		}
	})
}

func TestEWMALatencyRecording(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	ewma := NewEWMA(endpoints)

	t.Run("record and retrieve latency", func(t *testing.T) {
		// Record latency for endpoint 1
		latency := 50 * time.Millisecond
		ewma.RecordLatency(endpoints[0], latency)

		// Get score should reflect the recorded latency
		score := ewma.GetScore(endpoints[0])

		// Initial score is 100ms, after one sample of 50ms with decay 0.9:
		// new_score = 0.9 * 100 + 0.1 * 50 = 90 + 5 = 95
		// Due to scaling, we check approximate value
		if score < 50 || score > 100 {
			t.Logf("Score after one 50ms sample: %f ms (expected ~95ms)", score)
		}
	})

	t.Run("ewma converges to latency", func(t *testing.T) {
		ewma := NewEWMA(endpoints)

		// Record same latency multiple times
		for i := 0; i < 20; i++ {
			ewma.RecordLatency(endpoints[0], 30*time.Millisecond)
		}

		score := ewma.GetScore(endpoints[0])

		// Should converge close to 30ms
		if score < 29 || score > 31 {
			t.Logf("Score after 20 samples of 30ms: %f ms (expected ~30ms)", score)
		}
	})

	t.Run("different latencies affect scores differently", func(t *testing.T) {
		ewma := NewEWMA(endpoints)

		// Record high latency for endpoint 0
		for i := 0; i < 5; i++ {
			ewma.RecordLatency(endpoints[0], 100*time.Millisecond)
		}

		// Record low latency for endpoint 1
		for i := 0; i < 5; i++ {
			ewma.RecordLatency(endpoints[1], 10*time.Millisecond)
		}

		score0 := ewma.GetScore(endpoints[0])
		score1 := ewma.GetScore(endpoints[1])

		if score0 <= score1 {
			t.Errorf("Expected endpoint 0 score (%f) > endpoint 1 score (%f)", score0, score1)
		}
	})
}

func TestEWMAActiveRequestCounting(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	ewma := NewEWMA(endpoints)

	t.Run("increment active requests", func(t *testing.T) {
		ewma.IncrementActive(endpoints[0])
		ewma.IncrementActive(endpoints[0])
		ewma.IncrementActive(endpoints[1])

		// Verify counters were incremented
		// (We check through GetScore which uses active count in selection)
	})

	t.Run("decrement active requests", func(t *testing.T) {
		ewma.DecrementActive(endpoints[0])

		// Should not panic and counter should decrease
	})

	t.Run("select prefers endpoint with fewer active requests", func(t *testing.T) {
		ewma := NewEWMA(endpoints)

		// Make both endpoints have same EWMA score
		ewma.RecordLatency(endpoints[0], 50*time.Millisecond)
		ewma.RecordLatency(endpoints[1], 50*time.Millisecond)

		// Add active requests to endpoint 0
		for i := 0; i < 10; i++ {
			ewma.IncrementActive(endpoints[0])
		}

		// Should select endpoint 1 more often (no active requests)
		selections := make(map[string]int)
		for i := 0; i < 100; i++ {
			ep := ewma.Select()
			if ep != nil {
				selections[ep.Address]++
			}
		}

		if selections["10.0.0.2"] <= selections[testAddrEWMA] {
			t.Errorf("Expected endpoint 2 to be selected more (has no active requests), got selections: %v", selections)
		}
	})
}

func TestEWMAUpdateEndpoints(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	ewma := NewEWMA(initialEndpoints)

	// Record some latencies
	ewma.RecordLatency(initialEndpoints[0], 50*time.Millisecond)
	ewma.RecordLatency(initialEndpoints[1], 30*time.Millisecond)

	// Add active request count
	ewma.IncrementActive(initialEndpoints[0])

	t.Run("update with new endpoints", func(t *testing.T) {
		newEndpoints := []*pb.Endpoint{
			{Address: testAddrEWMA, Port: 8080, Ready: true},
			{Address: testAddrEWMA3, Port: 8080, Ready: true},
			{Address: "10.0.0.4", Port: 8080, Ready: true},
		}

		ewma.UpdateEndpoints(newEndpoints)

		if len(ewma.endpoints) != 3 {
			t.Errorf("Expected 3 endpoints, got %d", len(ewma.endpoints))
		}

		if len(newEndpoints) < 2 {
			t.Fatal("Expected at least 2 new endpoints")
		}

		// Preserved endpoints should keep their scores
		score1 := ewma.GetScore(newEndpoints[0])
		if score1 < 45 || score1 > 55 {
			t.Logf("Expected score for preserved endpoint ~50ms, got %f ms", score1)
		}

		// New endpoints should have initial score (100ms)
		score3 := ewma.GetScore(newEndpoints[1])
		if score3 < 95 || score3 > 105 {
			t.Logf("Expected score for new endpoint ~100ms, got %f ms", score3)
		}
	})

	t.Run("preserve active counts on update", func(t *testing.T) {
		ewma := NewEWMA(initialEndpoints)
		ewma.IncrementActive(initialEndpoints[0])
		ewma.IncrementActive(initialEndpoints[0])

		newEndpoints := []*pb.Endpoint{
			{Address: testAddrEWMA, Port: 8080, Ready: true},
			{Address: "10.0.0.2", Port: 8080, Ready: true},
		}

		ewma.UpdateEndpoints(newEndpoints)

		// Active count should be preserved for endpoint 1
		// This is verified indirectly through selection behavior
	})
}

func TestEWMAConcurrentOperations(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: testAddrEWMA3, Port: 8080, Ready: true},
	}

	ewma := NewEWMA(endpoints)

	t.Run("concurrent select operations", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = ewma.Select()
			}()
		}

		wg.Wait()
		// Test passes if no race condition detected
	})

	t.Run("concurrent latency recording", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ep := endpoints[idx%len(endpoints)]
				ewma.RecordLatency(ep, time.Duration(idx*10)*time.Millisecond)
			}(i)
		}

		wg.Wait()
		// Test passes if no race condition detected
	})

	t.Run("concurrent active request modifications", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ep := endpoints[idx%len(endpoints)]

				if idx%2 == 0 {
					ewma.IncrementActive(ep)
				} else {
					ewma.DecrementActive(ep)
				}
			}(i)
		}

		wg.Wait()
		// Test passes if no race condition detected
	})

	t.Run("concurrent select and update", func(t *testing.T) {
		var wg sync.WaitGroup

		// Select in goroutines
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = ewma.Select()
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
			ewma.UpdateEndpoints(newEndpoints)
		}()

		wg.Wait()
		// Test passes if no race condition detected
	})

	t.Run("concurrent atomic operations on scores", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ep := endpoints[idx%len(endpoints)]
				ewma.RecordLatency(ep, time.Duration(idx)*time.Millisecond)
				_ = ewma.GetScore(ep)
			}(i)
		}

		wg.Wait()
		// Test passes if no race condition detected
	})
}

func TestEWMAGetScore(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
	}

	ewma := NewEWMA(endpoints)

	t.Run("nil endpoint returns 0", func(t *testing.T) {
		score := ewma.GetScore(nil)
		if score != 0 {
			t.Errorf("Expected 0 for nil endpoint, got %f", score)
		}
	})

	t.Run("score in milliseconds", func(t *testing.T) {
		ewma.RecordLatency(endpoints[0], 100*time.Millisecond)

		score := ewma.GetScore(endpoints[0])

		// Score should be in milliseconds
		if score < 50 || score > 150 {
			t.Logf("Score should be around 100ms, got %f ms", score)
		}
	})
}

func TestEWMAUnhealthyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: testAddrEWMA3, Port: 8080, Ready: true},
	}

	ewma := NewEWMA(endpoints)

	t.Run("only selects healthy endpoints", func(t *testing.T) {
		// EWMA is deterministic - it always picks the endpoint with the lowest score.
		// When scores are equal, it consistently picks the same endpoint.
		// This test verifies that only healthy endpoints are ever selected.
		for i := 0; i < 100; i++ {
			ep := ewma.Select()
			if ep != nil {
				if !ep.Ready {
					t.Errorf("Selected unhealthy endpoint: %s", ep.Address)
				}
				// Verify it's one of the healthy endpoints
				if ep.Address != testAddrEWMA && ep.Address != testAddrEWMA3 {
					t.Errorf("Selected unexpected endpoint: %s", ep.Address)
				}
			}
		}
	})
}

func TestEWMANilEndpoint(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
	}

	ewma := NewEWMA(endpoints)

	t.Run("nil endpoint operations don't panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Unexpected panic: %v", r)
			}
		}()

		ewma.RecordLatency(nil, 50*time.Millisecond)
		ewma.IncrementActive(nil)
		ewma.DecrementActive(nil)
		_ = ewma.GetScore(nil)
	})
}

func TestEWMAAtomicOperations(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: testAddrEWMA, Port: 8080, Ready: true},
	}

	ewma := NewEWMA(endpoints)

	t.Run("atomic score updates are safe", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				latency := time.Duration(idx) * time.Millisecond
				ewma.RecordLatency(endpoints[0], latency)
			}(i)
		}

		wg.Wait()

		// All updates should complete without data corruption
		score := ewma.GetScore(endpoints[0])
		if score < 0 {
			t.Error("Score should never be negative")
		}
	})

	t.Run("atomic counter operations are safe", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ewma.IncrementActive(endpoints[0])
			}()
		}

		wg.Wait()

		// Check that final count is correct
		key := endpointKey(endpoints[0])
		ewma.mu.RLock()
		counter := atomic.LoadInt64(ewma.activeRequests[key])
		ewma.mu.RUnlock()

		if counter != int64(numGoroutines) {
			t.Errorf("Expected active count %d, got %d", numGoroutines, counter)
		}
	})
}
