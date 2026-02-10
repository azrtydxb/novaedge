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
	"testing"
	"time"

	"go.uber.org/zap"
)

const testEndpointAddr = "10.0.0.1:8080"

func TestCircuitBreakerState_String(t *testing.T) {
	tests := []struct {
		state    CircuitBreakerState
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitBreakerState(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.expected {
			t.Errorf("state %d: expected %q, got %q", tt.state, tt.expected, got)
		}
	}
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	config := DefaultCircuitBreakerConfig()

	if config.MaxRequests != 1 {
		t.Errorf("expected MaxRequests=1, got %d", config.MaxRequests)
	}
	if config.Interval != 10*time.Second {
		t.Errorf("expected Interval=10s, got %v", config.Interval)
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("expected Timeout=30s, got %v", config.Timeout)
	}
	if config.ConsecutiveErrors != 5 {
		t.Errorf("expected ConsecutiveErrors=5, got %d", config.ConsecutiveErrors)
	}
}

func TestNewCircuitBreaker(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultCircuitBreakerConfig()

	cb := NewCircuitBreaker(testEndpointAddr, config, logger)

	if cb == nil {
		t.Fatal("expected non-nil circuit breaker")
	}
	if cb.state != StateClosed {
		t.Errorf("expected initial state Closed, got %v", cb.state)
	}
	if cb.endpoint != testEndpointAddr {
		t.Errorf("expected endpoint '10.0.0.1:8080', got %q", cb.endpoint)
	}
}

func TestCircuitBreaker_ClosedState_AllowsRequests(t *testing.T) {
	logger := zap.NewNop()
	cb := NewCircuitBreaker("ep", DefaultCircuitBreakerConfig(), logger)
	cb.SetCluster("default/test")

	if !cb.Allow() {
		t.Error("closed circuit breaker should allow requests")
	}
	if cb.IsOpen() {
		t.Error("circuit breaker should not be open initially")
	}
}

func TestCircuitBreaker_ClosedToOpen_ConsecutiveFailures(t *testing.T) {
	logger := zap.NewNop()
	config := CircuitBreakerConfig{
		MaxRequests:       1,
		Interval:          10 * time.Second,
		Timeout:           100 * time.Millisecond, // Short for testing
		ConsecutiveErrors: 3,
	}
	cb := NewCircuitBreaker("ep", config, logger)
	cb.SetCluster("default/test")

	// Record consecutive failures below threshold
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.GetState() != StateClosed {
		t.Error("should still be closed below threshold")
	}

	// Third failure should trip the circuit
	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Errorf("expected open state after %d failures, got %v", config.ConsecutiveErrors, cb.GetState())
	}
	if !cb.IsOpen() {
		t.Error("IsOpen should return true when state is open")
	}
}

func TestCircuitBreaker_OpenState_RejectsRequests(t *testing.T) {
	logger := zap.NewNop()
	config := CircuitBreakerConfig{
		MaxRequests:       1,
		Interval:          10 * time.Second,
		Timeout:           1 * time.Hour, // Long timeout so it stays open
		ConsecutiveErrors: 1,
	}
	cb := NewCircuitBreaker("ep", config, logger)
	cb.SetCluster("default/test")

	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Fatal("expected open state")
	}
	if cb.Allow() {
		t.Error("open circuit breaker should reject requests")
	}
}

func TestCircuitBreaker_OpenToHalfOpen_AfterTimeout(t *testing.T) {
	logger := zap.NewNop()
	config := CircuitBreakerConfig{
		MaxRequests:       1,
		Interval:          10 * time.Second,
		Timeout:           50 * time.Millisecond,
		ConsecutiveErrors: 1,
	}
	cb := NewCircuitBreaker("ep", config, logger)
	cb.SetCluster("default/test")

	// Trip the circuit
	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Fatal("expected open state")
	}

	// Wait for timeout to elapse
	time.Sleep(100 * time.Millisecond)

	// Next Allow() should transition to half-open
	allowed := cb.Allow()
	if !allowed {
		t.Error("should allow request after timeout (transition to half-open)")
	}
	if cb.GetState() != StateHalfOpen {
		t.Errorf("expected half-open state, got %v", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenToClosed_OnSuccess(t *testing.T) {
	logger := zap.NewNop()
	config := CircuitBreakerConfig{
		MaxRequests:       1,
		Interval:          10 * time.Second,
		Timeout:           50 * time.Millisecond,
		ConsecutiveErrors: 1,
	}
	cb := NewCircuitBreaker("ep", config, logger)
	cb.SetCluster("default/test")

	// Trip the circuit
	cb.RecordFailure()
	time.Sleep(100 * time.Millisecond)
	cb.Allow() // Transitions to half-open

	if cb.GetState() != StateHalfOpen {
		t.Fatal("expected half-open state")
	}

	// Record success in half-open state
	cb.RecordSuccess()
	if cb.GetState() != StateClosed {
		t.Errorf("expected closed state after success in half-open, got %v", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenToOpen_OnFailure(t *testing.T) {
	logger := zap.NewNop()
	config := CircuitBreakerConfig{
		MaxRequests:       1,
		Interval:          10 * time.Second,
		Timeout:           50 * time.Millisecond,
		ConsecutiveErrors: 1,
	}
	cb := NewCircuitBreaker("ep", config, logger)
	cb.SetCluster("default/test")

	// Trip the circuit
	cb.RecordFailure()
	time.Sleep(100 * time.Millisecond)
	cb.Allow() // Transitions to half-open

	if cb.GetState() != StateHalfOpen {
		t.Fatal("expected half-open state")
	}

	// Record failure in half-open state
	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Errorf("expected open state after failure in half-open, got %v", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpen_LimitsRequests(t *testing.T) {
	logger := zap.NewNop()
	config := CircuitBreakerConfig{
		MaxRequests:       2,
		Interval:          10 * time.Second,
		Timeout:           50 * time.Millisecond,
		ConsecutiveErrors: 1,
	}
	cb := NewCircuitBreaker("ep", config, logger)
	cb.SetCluster("default/test")

	// Trip the circuit
	cb.RecordFailure()
	time.Sleep(100 * time.Millisecond)

	// First call transitions from open to half-open (does not count toward MaxRequests)
	if !cb.Allow() {
		t.Error("transition request should be allowed")
	}
	if cb.GetState() != StateHalfOpen {
		t.Fatal("expected half-open state after timeout")
	}

	// Second and third calls count toward MaxRequests=2
	if !cb.Allow() {
		t.Error("first counted request in half-open should be allowed")
	}
	if !cb.Allow() {
		t.Error("second counted request in half-open should be allowed (MaxRequests=2)")
	}
	// Fourth call should be rejected (exceeded MaxRequests)
	if cb.Allow() {
		t.Error("request beyond MaxRequests in half-open should be rejected")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	logger := zap.NewNop()
	config := CircuitBreakerConfig{
		MaxRequests:       1,
		Interval:          10 * time.Second,
		Timeout:           1 * time.Hour,
		ConsecutiveErrors: 1,
	}
	cb := NewCircuitBreaker("ep", config, logger)
	cb.SetCluster("default/test")

	// Trip the circuit
	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Fatal("expected open state")
	}

	// Reset
	cb.Reset()
	if cb.GetState() != StateClosed {
		t.Errorf("expected closed state after reset, got %v", cb.GetState())
	}
	if !cb.Allow() {
		t.Error("should allow requests after reset")
	}
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	logger := zap.NewNop()
	config := CircuitBreakerConfig{
		MaxRequests:       1,
		Interval:          10 * time.Second,
		Timeout:           30 * time.Second,
		ConsecutiveErrors: 5,
	}
	cb := NewCircuitBreaker("ep", config, logger)
	cb.SetCluster("default/test")

	// Record some failures but not enough to trip
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	// A success should reset the counter
	cb.RecordSuccess()

	// Now we need 5 more failures to trip
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.GetState() != StateClosed {
		t.Error("should still be closed, success reset the failure count")
	}

	// Fifth failure after reset should trip it
	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Error("should be open after 5 consecutive failures")
	}
}

func TestCircuitBreaker_GetStats(t *testing.T) {
	logger := zap.NewNop()
	cb := NewCircuitBreaker(testEndpointAddr, DefaultCircuitBreakerConfig(), logger)
	cb.SetCluster("default/test")

	stats := cb.GetStats()

	if stats["state"] != "closed" {
		t.Errorf("expected state 'closed', got %v", stats["state"])
	}
	if stats["endpoint"] != testEndpointAddr {
		t.Errorf("expected endpoint '10.0.0.1:8080', got %v", stats["endpoint"])
	}
	if cf, ok := stats["consecutive_failures"].(uint32); !ok {
		t.Errorf("expected consecutive_failures to be uint32, got %T", stats["consecutive_failures"])
	} else if cf != 0 {
		t.Errorf("expected 0 consecutive failures, got %v", cf)
	}
	if cs, ok := stats["consecutive_successes"].(uint32); !ok {
		t.Errorf("expected consecutive_successes to be uint32, got %T", stats["consecutive_successes"])
	} else if cs != 0 {
		t.Errorf("expected 0 consecutive successes, got %v", cs)
	}
}

func TestCircuitBreaker_SetCluster(t *testing.T) {
	logger := zap.NewNop()
	cb := NewCircuitBreaker("ep", DefaultCircuitBreakerConfig(), logger)

	cb.SetCluster("prod/myservice")

	if cb.cluster != "prod/myservice" {
		t.Errorf("expected cluster 'prod/myservice', got %q", cb.cluster)
	}
}
