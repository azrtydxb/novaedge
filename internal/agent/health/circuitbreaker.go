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
	"time"

	"go.uber.org/zap"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	// StateClosed - circuit is closed, requests flow through
	StateClosed CircuitBreakerState = iota
	// StateOpen - circuit is open, requests fail immediately
	StateOpen
	// StateHalfOpen - circuit is testing if backend has recovered
	StateHalfOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig configures circuit breaker behavior
type CircuitBreakerConfig struct {
	// MaxRequests in half-open state
	MaxRequests uint32
	// Interval for counting failures
	Interval time.Duration
	// Timeout before transitioning from open to half-open
	Timeout time.Duration
	// ConsecutiveErrors to trip the circuit
	ConsecutiveErrors uint32
}

// DefaultCircuitBreakerConfig returns default configuration
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxRequests:       1,
		Interval:          10 * time.Second,
		Timeout:           30 * time.Second,
		ConsecutiveErrors: 5,
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	mu     sync.RWMutex
	logger *zap.Logger

	// Configuration
	config CircuitBreakerConfig

	// Current state
	state CircuitBreakerState

	// State transition timestamp
	stateChangedAt time.Time

	// Counters
	consecutiveFailures  uint32
	consecutiveSuccesses uint32
	requestCount         uint32

	// Endpoint identifier
	endpoint string
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(endpoint string, config CircuitBreakerConfig, logger *zap.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		logger:         logger,
		config:         config,
		state:          StateClosed,
		stateChangedAt: time.Now(),
		endpoint:       endpoint,
	}
}

// Allow checks if a request should be allowed through
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case StateClosed:
		// Always allow in closed state
		return true

	case StateOpen:
		// Check if timeout has elapsed to try half-open
		if now.Sub(cb.stateChangedAt) >= cb.config.Timeout {
			cb.transitionToHalfOpen()
			return true
		}
		// Circuit is open, reject request
		return false

	case StateHalfOpen:
		// Allow limited requests in half-open state
		if cb.requestCount < cb.config.MaxRequests {
			cb.requestCount++
			return true
		}
		return false

	default:
		return false
	}
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses++

	switch cb.state {
	case StateHalfOpen:
		// Successful probe in half-open state
		cb.logger.Info("Circuit breaker probe succeeded, closing circuit",
			zap.String("endpoint", cb.endpoint),
		)
		cb.transitionToClosed()

	case StateClosed:
		// Normal operation, reset failure count
		// Already done above
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveSuccesses = 0
	cb.consecutiveFailures++

	switch cb.state {
	case StateHalfOpen:
		// Failed probe in half-open state, back to open
		cb.logger.Warn("Circuit breaker probe failed, reopening circuit",
			zap.String("endpoint", cb.endpoint),
		)
		cb.transitionToOpen()

	case StateClosed:
		// Check if we should open the circuit
		if cb.consecutiveFailures >= cb.config.ConsecutiveErrors {
			cb.logger.Warn("Circuit breaker opened due to consecutive failures",
				zap.String("endpoint", cb.endpoint),
				zap.Uint32("failures", cb.consecutiveFailures),
			)
			cb.transitionToOpen()
		}
	}
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// IsOpen returns true if circuit is open
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.GetState() == StateOpen
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.logger.Info("Resetting circuit breaker",
		zap.String("endpoint", cb.endpoint),
	)
	cb.transitionToClosed()
}

// transitionToOpen transitions to open state
func (cb *CircuitBreaker) transitionToOpen() {
	cb.state = StateOpen
	cb.stateChangedAt = time.Now()
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
	cb.requestCount = 0
}

// transitionToHalfOpen transitions to half-open state
func (cb *CircuitBreaker) transitionToHalfOpen() {
	cb.logger.Info("Circuit breaker entering half-open state",
		zap.String("endpoint", cb.endpoint),
	)
	cb.state = StateHalfOpen
	cb.stateChangedAt = time.Now()
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
	cb.requestCount = 0
}

// transitionToClosed transitions to closed state
func (cb *CircuitBreaker) transitionToClosed() {
	cb.state = StateClosed
	cb.stateChangedAt = time.Now()
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
	cb.requestCount = 0
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":                 cb.state.String(),
		"consecutive_failures":  cb.consecutiveFailures,
		"consecutive_successes": cb.consecutiveSuccesses,
		"state_duration_ms":     time.Since(cb.stateChangedAt).Milliseconds(),
		"endpoint":              cb.endpoint,
	}
}
