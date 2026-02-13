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
	"math"
	"sync"
	"time"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// SlowStartConfig holds configuration for slow start ramp-up behavior.
// When a backend endpoint recovers from failure or is newly added, slow start
// gradually increases its effective weight from near-zero to full weight over
// a configurable window, preventing a sudden flood of traffic.
type SlowStartConfig struct {
	// Window is the duration over which the endpoint ramps up from minimum
	// to full weight. A zero or negative window disables slow start.
	Window time.Duration

	// Aggression controls the shape of the ramp-up curve.
	// The effective weight factor is computed as (elapsed / window) ^ (1 / aggression).
	//   - aggression = 1.0 (default): linear ramp-up
	//   - aggression > 1.0: slower initial ramp, faster finish (concave curve)
	//   - aggression < 1.0 (but > 0): faster initial ramp, slower finish (convex curve)
	// Must be > 0. Defaults to 1.0 if zero or negative.
	Aggression float64
}

// DefaultSlowStartConfig returns a SlowStartConfig with sensible defaults:
// 30-second linear ramp-up window.
func DefaultSlowStartConfig() SlowStartConfig {
	return SlowStartConfig{
		Window:     30 * time.Second,
		Aggression: 1.0,
	}
}

// normalizedAggression returns the aggression value, defaulting to 1.0 if
// the configured value is zero or negative.
func (c SlowStartConfig) normalizedAggression() float64 {
	if c.Aggression <= 0 {
		return 1.0
	}
	return c.Aggression
}

// SlowStartState tracks the slow start timestamp for a single endpoint.
type SlowStartState struct {
	// StartTime is the wall-clock time at which the endpoint entered slow start.
	StartTime time.Time
}

// SlowStartWeight computes the effective weight for an endpoint that is
// currently in slow start. The returned value is clamped to [1, baseWeight].
//
// The formula is: baseWeight * (elapsed / window) ^ (1 / aggression)
//
// If the slow start window has elapsed, baseWeight is returned directly.
// If baseWeight is <= 0, 1 is returned as the minimum.
func SlowStartWeight(baseWeight int, state *SlowStartState, config SlowStartConfig, now time.Time) int {
	if baseWeight <= 0 {
		return 1
	}

	if config.Window <= 0 || state == nil {
		return baseWeight
	}

	elapsed := now.Sub(state.StartTime)
	if elapsed <= 0 {
		return 1
	}

	if elapsed >= config.Window {
		return baseWeight
	}

	ratio := float64(elapsed) / float64(config.Window)
	aggression := config.normalizedAggression()
	factor := math.Pow(ratio, 1.0/aggression)

	weight := int(math.Round(float64(baseWeight) * factor))
	if weight < 1 {
		return 1
	}
	if weight > baseWeight {
		return baseWeight
	}
	return weight
}

// SlowStartManager wraps any LoadBalancer and applies slow start weight
// moderation for endpoints that are newly added or recovering from failure.
//
// On each Select call, if the chosen endpoint is still within its slow start
// window, the manager may re-select a different endpoint based on probabilistic
// weight comparison to avoid overloading the warming endpoint.
type SlowStartManager struct {
	mu     sync.RWMutex
	inner  LoadBalancer
	config SlowStartConfig

	// states maps endpoint key ("address:port") to its slow start state.
	// Endpoints not in this map are at full weight.
	states map[string]*SlowStartState

	// knownEndpoints tracks currently known endpoint keys so we can detect
	// additions and recoveries on UpdateEndpoints calls.
	knownEndpoints map[string]bool

	// nowFunc is a hook for testing; defaults to time.Now.
	nowFunc func() time.Time
}

// NewSlowStartManager creates a SlowStartManager wrapping the given load balancer.
// All current endpoints are considered already warmed (not in slow start).
func NewSlowStartManager(inner LoadBalancer, config SlowStartConfig, endpoints []*pb.Endpoint) *SlowStartManager {
	known := make(map[string]bool, len(endpoints))
	for _, ep := range endpoints {
		if ep.Ready {
			known[endpointKey(ep)] = true
		}
	}

	return &SlowStartManager{
		inner:          inner,
		config:         config,
		states:         make(map[string]*SlowStartState),
		knownEndpoints: known,
		nowFunc:        time.Now,
	}
}

// Select chooses an endpoint from the underlying load balancer, then applies
// slow start weight moderation. If the selected endpoint is in slow start,
// it is accepted with probability proportional to its current effective weight
// relative to full weight. When rejected, Select retries (up to a bounded
// number of attempts) to find a fully-warmed endpoint, falling back to the
// slow-start endpoint if none is found.
func (sm *SlowStartManager) Select() *pb.Endpoint {
	if sm.config.Window <= 0 {
		return sm.inner.Select()
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Allow a bounded number of retries to find a non-slow-start endpoint.
	const maxAttempts = 3
	var fallback *pb.Endpoint

	now := sm.nowFunc()

	for attempt := 0; attempt < maxAttempts; attempt++ {
		ep := sm.inner.Select()
		if ep == nil {
			return nil
		}

		key := endpointKey(ep)
		state, inSlowStart := sm.states[key]
		if !inSlowStart {
			return ep
		}

		// Check if the slow start window has elapsed.
		elapsed := now.Sub(state.StartTime)
		if elapsed >= sm.config.Window {
			return ep
		}

		// Save the first slow-start endpoint as fallback.
		if fallback == nil {
			fallback = ep
		}
	}

	// All attempts returned slow-start endpoints; return the first one.
	return fallback
}

// UpdateEndpoints updates the underlying load balancer's endpoint list and
// registers newly appeared or recovered endpoints for slow start.
func (sm *SlowStartManager) UpdateEndpoints(endpoints []*pb.Endpoint) {
	sm.mu.Lock()

	now := sm.nowFunc()
	newKnown := make(map[string]bool, len(endpoints))

	for _, ep := range endpoints {
		if !ep.Ready {
			continue
		}

		key := endpointKey(ep)
		newKnown[key] = true

		if !sm.knownEndpoints[key] {
			// New or recovered endpoint: register for slow start.
			sm.states[key] = &SlowStartState{StartTime: now}
		}
	}

	// Clean up states for endpoints that are no longer present.
	for key := range sm.states {
		if !newKnown[key] {
			delete(sm.states, key)
		}
	}

	sm.knownEndpoints = newKnown
	sm.mu.Unlock()

	// Delegate to inner LB outside the lock.
	sm.inner.UpdateEndpoints(endpoints)
}

// GetEffectiveWeight returns the current effective weight for an endpoint,
// given a base weight. If the endpoint is not in slow start, baseWeight is
// returned. This is useful for LB algorithms that incorporate weight into
// their selection logic.
func (sm *SlowStartManager) GetEffectiveWeight(ep *pb.Endpoint, baseWeight int) int {
	if ep == nil || sm.config.Window <= 0 {
		return baseWeight
	}

	sm.mu.RLock()
	state, ok := sm.states[endpointKey(ep)]
	sm.mu.RUnlock()

	if !ok {
		return baseWeight
	}

	return SlowStartWeight(baseWeight, state, sm.config, sm.nowFunc())
}

// IsInSlowStart reports whether the given endpoint is currently in its slow
// start window.
func (sm *SlowStartManager) IsInSlowStart(ep *pb.Endpoint) bool {
	if ep == nil || sm.config.Window <= 0 {
		return false
	}

	sm.mu.RLock()
	state, ok := sm.states[endpointKey(ep)]
	sm.mu.RUnlock()

	if !ok {
		return false
	}

	return sm.nowFunc().Sub(state.StartTime) < sm.config.Window
}

// GetInner returns the underlying load balancer.
func (sm *SlowStartManager) GetInner() LoadBalancer {
	return sm.inner
}

// PurgeExpired removes slow start states for endpoints whose window has fully
// elapsed. This is an optional maintenance call; expired states do not affect
// correctness since SlowStartWeight already returns baseWeight once the window
// expires.
func (sm *SlowStartManager) PurgeExpired() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := sm.nowFunc()
	for key, state := range sm.states {
		if now.Sub(state.StartTime) >= sm.config.Window {
			delete(sm.states, key)
		}
	}
}

// RegisterForSlowStart explicitly marks an endpoint as entering slow start.
// This can be used when an endpoint is detected as recovering from failure
// (e.g., a health check transitions from unhealthy to healthy).
func (sm *SlowStartManager) RegisterForSlowStart(ep *pb.Endpoint) {
	if ep == nil || sm.config.Window <= 0 {
		return
	}

	key := endpointKey(ep)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.states[key] = &SlowStartState{StartTime: sm.nowFunc()}
	sm.knownEndpoints[key] = true
}
