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
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestNewCircuitBreaker(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultCircuitBreakerConfig()

	cb := NewCircuitBreaker("endpoint1:8080", config, logger)

	if cb == nil {
		t.Fatal("Expected circuit breaker to be created")
	}

	if cb.endpoint != "endpoint1:8080" {
		t.Errorf("Expected endpoint endpoint1:8080, got %s", cb.endpoint)
	}

	if cb.GetState() != StateClosed {
		t.Errorf("Expected initial state to be Closed, got %s", cb.GetState())
	}
}

func TestCircuitBreakerStateTransitions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := CircuitBreakerConfig{
		MaxRequests:       1,
		Interval:          10 * time.Second,
		Timeout:           100 * time.Millisecond,
		ConsecutiveErrors: 3,
	}

	cb := NewCircuitBreaker("test-endpoint", config, logger)

	t.Run("closed to open transition", func(t *testing.T) {
		// Record consecutive failures to open the circuit
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}

		if cb.GetState() != StateOpen {
			t.Errorf("Expected state to be Open, got %s", cb.GetState())
		}

		if !cb.IsOpen() {
			t.Error("Expected IsOpen() to return true")
		}
	})

	t.Run("open to half-open transition", func(t *testing.T) {
		// Wait for timeout to elapse
		time.Sleep(config.Timeout + 10*time.Millisecond)

		// Attempt to allow a request (should transition to half-open)
		allowed := cb.Allow()
		if !allowed {
			t.Error("Expected request to be allowed during half-open transition")
		}

		if cb.GetState() != StateHalfOpen {
			t.Errorf("Expected state to be HalfOpen, got %s", cb.GetState())
		}
	})

	t.Run("half-open to closed on success", func(t *testing.T) {
		// Record success in half-open state
		cb.RecordSuccess()

		if cb.GetState() != StateClosed {
			t.Errorf("Expected state to be Closed, got %s", cb.GetState())
		}
	})

	t.Run("half-open back to open on failure", func(t *testing.T) {
		// First get to half-open again
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}
		time.Sleep(config.Timeout + 10*time.Millisecond)
		cb.Allow()

		// Record failure in half-open state
		cb.RecordFailure()

		if cb.GetState() != StateOpen {
			t.Errorf("Expected state to be Open after probe failure, got %s", cb.GetState())
		}
	})
}

func TestCircuitBreakerAllowLogic(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultCircuitBreakerConfig()
	cb := NewCircuitBreaker("test-endpoint", config, logger)

	t.Run("allow in closed state", func(t *testing.T) {
		if !cb.Allow() {
			t.Error("Expected Allow() to return true in Closed state")
		}
	})

	t.Run("deny in open state", func(t *testing.T) {
		// Open the circuit
		for i := 0; i < int(config.ConsecutiveErrors); i++ {
			cb.RecordFailure()
		}

		if cb.Allow() {
			t.Error("Expected Allow() to return false in Open state")
		}
	})

	t.Run("limited allows in half-open state", func(t *testing.T) {
		// Wait for transition to half-open
		time.Sleep(config.Timeout + 10*time.Millisecond)

		// First request should be allowed
		allowed1 := cb.Allow()
		if !allowed1 {
			t.Error("Expected first request to be allowed in HalfOpen state")
		}

		// Second request should be denied (MaxRequests=1)
		allowed2 := cb.Allow()
		if allowed2 {
			t.Error("Expected second request to be denied in HalfOpen state")
		}
	})
}

func TestCircuitBreakerConsecutiveCounters(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultCircuitBreakerConfig()
	cb := NewCircuitBreaker("test-endpoint", config, logger)

	t.Run("consecutive failures counter", func(t *testing.T) {
		cb.RecordFailure()
		cb.RecordFailure()

		stats := cb.GetStats()
		failures := stats["consecutive_failures"].(uint32)

		if failures != 2 {
			t.Errorf("Expected 2 consecutive failures, got %d", failures)
		}
	})

	t.Run("reset failures on success", func(t *testing.T) {
		// Reset state first
		cb.Reset()

		// Record some failures then success
		cb.RecordFailure()
		cb.RecordFailure()
		cb.RecordSuccess()

		stats := cb.GetStats()
		failures := stats["consecutive_failures"].(uint32)
		successes := stats["consecutive_successes"].(uint32)

		if failures != 0 {
			t.Errorf("Expected failures to be reset to 0, got %d", failures)
		}

		if successes == 0 {
			t.Error("Expected successes to be incremented")
		}
	})

	t.Run("reset successes on failure", func(t *testing.T) {
		cb.Reset()

		cb.RecordSuccess()
		cb.RecordSuccess()
		cb.RecordFailure()

		stats := cb.GetStats()
		successes := stats["consecutive_successes"].(uint32)
		failures := stats["consecutive_failures"].(uint32)

		if successes != 0 {
			t.Errorf("Expected successes to be reset to 0, got %d", successes)
		}

		if failures == 0 {
			t.Error("Expected failures to be incremented")
		}
	})
}

func TestCircuitBreakerRequestCounting(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := CircuitBreakerConfig{
		MaxRequests:       2,
		Interval:          10 * time.Second,
		Timeout:           50 * time.Millisecond,
		ConsecutiveErrors: 3,
	}

	cb := NewCircuitBreaker("test-endpoint", config, logger)

	t.Run("half-open request limiting", func(t *testing.T) {
		// Open the circuit
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}

		// Wait for half-open transition
		time.Sleep(config.Timeout + 10*time.Millisecond)

		// Should allow up to MaxRequests
		allowedCount := 0
		for i := 0; i < 5; i++ {
			if cb.Allow() {
				allowedCount++
			}
		}

		if allowedCount != int(config.MaxRequests) {
			t.Errorf("Expected %d allowed requests, got %d", config.MaxRequests, allowedCount)
		}
	})

	t.Run("request counter reset on state change", func(t *testing.T) {
		cb.Reset()

		// Open again
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}

		// Transition to half-open
		time.Sleep(config.Timeout + 10*time.Millisecond)

		// Allow some requests
		cb.Allow()
		cb.Allow()

		// Success should reset counter
		cb.RecordSuccess()

		// Should allow requests again in closed state
		if !cb.Allow() {
			t.Error("Expected Allow() to return true after resetting in Closed state")
		}
	})
}

func TestCircuitBreakerTimeoutBehavior(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := CircuitBreakerConfig{
		MaxRequests:       1,
		Interval:          10 * time.Second,
		Timeout:           100 * time.Millisecond,
		ConsecutiveErrors: 2,
	}

	cb := NewCircuitBreaker("test-endpoint", config, logger)

	t.Run("timeout transitions to half-open", func(t *testing.T) {
		// Open the circuit
		cb.RecordFailure()
		cb.RecordFailure()

		if cb.GetState() != StateOpen {
			t.Fatalf("Expected Open state, got %s", cb.GetState())
		}

		// Before timeout, should deny requests
		if cb.Allow() {
			t.Error("Expected request to be denied before timeout")
		}

		// Wait for timeout
		time.Sleep(config.Timeout + 10*time.Millisecond)

		// After timeout, should allow transition to half-open
		if !cb.Allow() {
			t.Error("Expected Allow() to return true after timeout")
		}

		if cb.GetState() != StateHalfOpen {
			t.Errorf("Expected HalfOpen state after timeout, got %s", cb.GetState())
		}
	})

	t.Run("exact timeout boundary", func(t *testing.T) {
		cb.Reset()

		cb.RecordFailure()
		cb.RecordFailure()

		// Get state change timestamp
		stateTime := time.Now()

		// Try just before timeout
		time.Sleep(config.Timeout - 10*time.Millisecond)

		if cb.Allow() {
			t.Logf("Allow returned true before timeout (may indicate clock skew)")
		}

		// Try after timeout
		time.Sleep(20 * time.Millisecond)

		if !cb.Allow() {
			t.Error("Expected Allow() to return true after timeout elapsed")
		}

		_ = stateTime // Use variable to avoid unused error
	})
}

func TestCircuitBreakerConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultCircuitBreakerConfig()
	cb := NewCircuitBreaker("test-endpoint", config, logger)

	t.Run("concurrent Allow calls", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100
		allowedCount := 0
		var mu sync.Mutex

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if cb.Allow() {
					mu.Lock()
					allowedCount++
					mu.Unlock()
				}
			}()
		}

		wg.Wait()

		if allowedCount != numGoroutines {
			t.Errorf("Expected all requests to be allowed in Closed state, got %d", allowedCount)
		}
	})

	t.Run("concurrent RecordSuccess and RecordFailure", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				if idx%2 == 0 {
					cb.RecordSuccess()
				} else {
					cb.RecordFailure()
				}
			}(i)
		}

		wg.Wait()
		// Test passes if no race condition is detected
	})

	t.Run("concurrent GetState calls", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = cb.GetState()
			}()
		}

		wg.Wait()
		// Test passes if no race condition is detected
	})

	t.Run("concurrent GetStats calls", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = cb.GetStats()
			}()
		}

		wg.Wait()
		// Test passes if no race condition is detected
	})
}

func TestCircuitBreakerGetStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultCircuitBreakerConfig()
	cb := NewCircuitBreaker("test-endpoint", config, logger)

	t.Run("stats structure", func(t *testing.T) {
		stats := cb.GetStats()

		if _, ok := stats["state"]; !ok {
			t.Error("Expected 'state' in stats")
		}

		if _, ok := stats["consecutive_failures"]; !ok {
			t.Error("Expected 'consecutive_failures' in stats")
		}

		if _, ok := stats["consecutive_successes"]; !ok {
			t.Error("Expected 'consecutive_successes' in stats")
		}

		if _, ok := stats["state_duration_ms"]; !ok {
			t.Error("Expected 'state_duration_ms' in stats")
		}

		if _, ok := stats["endpoint"]; !ok {
			t.Error("Expected 'endpoint' in stats")
		}
	})

	t.Run("stats accuracy", func(t *testing.T) {
		cb.RecordFailure()
		cb.RecordFailure()

		stats := cb.GetStats()

		state := stats["state"].(string)
		if state != "closed" {
			t.Errorf("Expected state 'closed', got %s", state)
		}

		failures := stats["consecutive_failures"].(uint32)
		if failures != 2 {
			t.Errorf("Expected 2 failures, got %d", failures)
		}

		endpoint := stats["endpoint"].(string)
		if endpoint != "test-endpoint" {
			t.Errorf("Expected endpoint 'test-endpoint', got %s", endpoint)
		}
	})
}

func TestCircuitBreakerReset(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultCircuitBreakerConfig()
	cb := NewCircuitBreaker("test-endpoint", config, logger)

	// Open the circuit
	for i := 0; i < int(config.ConsecutiveErrors); i++ {
		cb.RecordFailure()
	}

	if cb.GetState() != StateOpen {
		t.Fatalf("Expected Open state before reset, got %s", cb.GetState())
	}

	// Reset
	cb.Reset()

	if cb.GetState() != StateClosed {
		t.Errorf("Expected Closed state after reset, got %s", cb.GetState())
	}

	stats := cb.GetStats()
	if failures := stats["consecutive_failures"].(uint32); failures != 0 {
		t.Errorf("Expected 0 failures after reset, got %d", failures)
	}

	if successes := stats["consecutive_successes"].(uint32); successes != 0 {
		t.Errorf("Expected 0 successes after reset, got %d", successes)
	}
}

func TestCircuitBreakerSetCluster(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultCircuitBreakerConfig()
	cb := NewCircuitBreaker("test-endpoint", config, logger)

	cb.SetCluster("test-cluster/default")

	// Verify cluster is set (can't directly access, but should not panic)
	stats := cb.GetStats()
	if stats == nil {
		t.Error("Expected GetStats() to work after SetCluster()")
	}
}

func TestCircuitBreakerStateString(t *testing.T) {
	tests := []struct {
		state    CircuitBreakerState
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitBreakerState(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.state.String() != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, tt.state.String())
			}
		})
	}
}
