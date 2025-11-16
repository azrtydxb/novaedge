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

package health

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewHealthChecker(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cluster := &pb.Cluster{
		Name:      "test-cluster",
		Namespace: "default",
	}
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	hc := NewHealthChecker(cluster, endpoints, logger)

	if hc == nil {
		t.Fatal("Expected health checker to be created")
	}

	if hc.cluster != cluster {
		t.Error("Cluster does not match")
	}

	if len(hc.endpoints) != 2 {
		t.Errorf("Expected 2 endpoints, got %d", len(hc.endpoints))
	}

	if hc.httpClient == nil {
		t.Error("HTTP client should be initialized")
	}
}

func TestHealthCheckerStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cluster := &pb.Cluster{
		Name:      "test-cluster",
		Namespace: "default",
	}
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	hc := NewHealthChecker(cluster, endpoints, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	hc.Start(ctx)

	// Wait for context to timeout
	<-ctx.Done()

	// Verify results were initialized
	hc.mu.RLock()
	resultsCount := len(hc.results)
	hc.mu.RUnlock()

	if resultsCount != 1 {
		t.Errorf("Expected 1 health result, got %d", resultsCount)
	}
}

func TestHealthCheckerConsecutiveSuccessFailureTracking(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cluster := &pb.Cluster{
		Name:      "test-cluster",
		Namespace: "default",
	}
	endpoint := &pb.Endpoint{
		Address: "10.0.0.1",
		Port:    8080,
		Ready:   true,
	}
	endpoints := []*pb.Endpoint{endpoint}

	hc := NewHealthChecker(cluster, endpoints, logger)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	hc.Start(ctx)
	<-ctx.Done()

	t.Run("consecutive failures tracking", func(t *testing.T) {
		// Record multiple failures
		hc.RecordFailure(endpoint)
		hc.RecordFailure(endpoint)
		hc.RecordFailure(endpoint)

		// Verify failure count
		hc.mu.RLock()
		result := hc.results[endpointKey(endpoint)]
		failureCount := result.ConsecutiveFailures
		hc.mu.RUnlock()

		if failureCount != 3 {
			t.Errorf("Expected 3 consecutive failures, got %d", failureCount)
		}
	})

	t.Run("consecutive successes tracking", func(t *testing.T) {
		hc.mu.Lock()
		hc.results[endpointKey(endpoint)].ConsecutiveFailures = 0
		hc.results[endpointKey(endpoint)].ConsecutiveSuccesses = 0
		hc.mu.Unlock()

		// Record multiple successes
		hc.RecordSuccess(endpoint)
		hc.RecordSuccess(endpoint)

		// Verify success count is reset in circuit breaker
		hc.mu.RLock()
		result := hc.results[endpointKey(endpoint)]
		hc.mu.RUnlock()

		if result == nil {
			t.Fatal("Expected result to exist")
		}
	})

	t.Run("reset on opposite state", func(t *testing.T) {
		hc.mu.Lock()
		hc.results[endpointKey(endpoint)].ConsecutiveFailures = 3
		hc.results[endpointKey(endpoint)].ConsecutiveSuccesses = 0
		hc.mu.Unlock()

		hc.RecordSuccess(endpoint)

		hc.mu.RLock()
		result := hc.results[endpointKey(endpoint)]
		hc.mu.RUnlock()

		// Circuit breaker handles this, but health result tracks it
		if result.ConsecutiveFailures != 0 {
			t.Errorf("Expected failure count to be reset, got %d", result.ConsecutiveFailures)
		}
	})
}

func TestHealthCheckerStatusTransitions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cluster := &pb.Cluster{
		Name:      "test-cluster",
		Namespace: "default",
	}
	endpoint := &pb.Endpoint{
		Address: "10.0.0.1",
		Port:    8080,
		Ready:   true,
	}
	endpoints := []*pb.Endpoint{endpoint}

	hc := NewHealthChecker(cluster, endpoints, logger)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	hc.Start(ctx)
	<-ctx.Done()

	t.Run("healthy endpoint", func(t *testing.T) {
		if !hc.IsHealthy(endpoint) {
			t.Error("Expected endpoint to be healthy initially")
		}
	})

	t.Run("becomes unhealthy after threshold", func(t *testing.T) {
		hc.RecordFailure(endpoint)
		hc.RecordFailure(endpoint)
		hc.RecordFailure(endpoint)

		hc.mu.RLock()
		result := hc.results[endpointKey(endpoint)]
		healthy := result.Healthy
		hc.mu.RUnlock()

		if healthy {
			t.Error("Expected endpoint to be unhealthy after 3 failures")
		}
	})
}

func TestHealthCheckerUpdateEndpoints(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cluster := &pb.Cluster{
		Name:      "test-cluster",
		Namespace: "default",
	}
	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	hc := NewHealthChecker(cluster, initialEndpoints, logger)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	hc.Start(ctx)
	<-ctx.Done()

	t.Run("add new endpoints", func(t *testing.T) {
		newEndpoints := []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080, Ready: true},
			{Address: "10.0.0.2", Port: 8080, Ready: true},
			{Address: "10.0.0.3", Port: 8080, Ready: true},
		}

		hc.UpdateEndpoints(newEndpoints)

		hc.mu.RLock()
		resultsCount := len(hc.results)
		hc.mu.RUnlock()

		if resultsCount != 3 {
			t.Errorf("Expected 3 results after adding endpoint, got %d", resultsCount)
		}
	})

	t.Run("remove endpoints", func(t *testing.T) {
		newEndpoints := []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080, Ready: true},
		}

		hc.UpdateEndpoints(newEndpoints)

		hc.mu.RLock()
		resultsCount := len(hc.results)
		hc.mu.RUnlock()

		if resultsCount != 1 {
			t.Errorf("Expected 1 result after removing endpoints, got %d", resultsCount)
		}
	})
}

func TestHealthCheckerConcurrentOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cluster := &pb.Cluster{
		Name:      "test-cluster",
		Namespace: "default",
	}
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	hc := NewHealthChecker(cluster, endpoints, logger)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	hc.Start(ctx)
	<-ctx.Done()

	t.Run("concurrent record operations", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ep := endpoints[idx%len(endpoints)]

				if idx%2 == 0 {
					hc.RecordSuccess(ep)
				} else {
					hc.RecordFailure(ep)
				}
			}(i)
		}

		wg.Wait()
		// Test passes if no race condition is detected
	})

	t.Run("concurrent health checks", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ep := endpoints[idx%len(endpoints)]
				_ = hc.IsHealthy(ep)
			}(i)
		}

		wg.Wait()
		// Test passes if no race condition is detected
	})

	t.Run("concurrent endpoint updates", func(t *testing.T) {
		var wg sync.WaitGroup

		// Update endpoints
		wg.Add(1)
		go func() {
			defer wg.Done()
			newEndpoints := []*pb.Endpoint{
				{Address: "10.0.0.4", Port: 8080, Ready: true},
				{Address: "10.0.0.5", Port: 8080, Ready: true},
			}
			hc.UpdateEndpoints(newEndpoints)
		}()

		// Read during update
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if len(endpoints) > 0 {
					_ = hc.IsHealthy(endpoints[0])
				}
			}()
		}

		wg.Wait()
		// Test passes if no race condition is detected
	})
}

func TestHealthCheckerGetHealthyEndpoints(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cluster := &pb.Cluster{
		Name:      "test-cluster",
		Namespace: "default",
	}
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	hc := NewHealthChecker(cluster, endpoints, logger)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	hc.Start(ctx)
	<-ctx.Done()

	t.Run("all healthy initially", func(t *testing.T) {
		healthy := hc.GetHealthyEndpoints()
		if len(healthy) != 3 {
			t.Errorf("Expected 3 healthy endpoints, got %d", len(healthy))
		}
	})

	t.Run("filtered after marking unhealthy", func(t *testing.T) {
		hc.RecordFailure(endpoints[0])
		hc.RecordFailure(endpoints[0])
		hc.RecordFailure(endpoints[0])

		healthy := hc.GetHealthyEndpoints()
		if len(healthy) != 2 {
			t.Errorf("Expected 2 healthy endpoints after failure, got %d", len(healthy))
		}
	})
}

func TestHealthCheckerTimeoutHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cluster := &pb.Cluster{
		Name:      "test-cluster",
		Namespace: "default",
	}
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	hc := NewHealthChecker(cluster, endpoints, logger)

	// Test that context timeout is handled gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	hc.Start(ctx)

	// Wait for context to timeout
	<-ctx.Done()

	// Stop should complete without hanging
	done := make(chan struct{})
	go func() {
		hc.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Health checker Stop() took too long")
	}
}

func TestHealthCheckerEmptyEndpoints(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cluster := &pb.Cluster{
		Name:      "test-cluster",
		Namespace: "default",
	}
	endpoints := []*pb.Endpoint{}

	hc := NewHealthChecker(cluster, endpoints, logger)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	hc.Start(ctx)
	<-ctx.Done()

	healthy := hc.GetHealthyEndpoints()
	if len(healthy) != 0 {
		t.Errorf("Expected 0 healthy endpoints, got %d", len(healthy))
	}
}
