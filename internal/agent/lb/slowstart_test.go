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
	"time"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// --- SlowStartWeight unit tests ---

func TestSlowStartWeightLinearCurve(t *testing.T) {
	config := SlowStartConfig{
		Window:     10 * time.Second,
		Aggression: 1.0,
	}
	start := time.Now()

	tests := []struct {
		name       string
		elapsed    time.Duration
		baseWeight int
		wantMin    int
		wantMax    int
	}{
		{"0%", 0, 100, 1, 1},
		{"10%", 1 * time.Second, 100, 9, 11},
		{"25%", 2500 * time.Millisecond, 100, 24, 26},
		{"50%", 5 * time.Second, 100, 49, 51},
		{"75%", 7500 * time.Millisecond, 100, 74, 76},
		{"100%", 10 * time.Second, 100, 100, 100},
		{"past window", 15 * time.Second, 100, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &SlowStartState{StartTime: start}
			now := start.Add(tt.elapsed)

			got := SlowStartWeight(tt.baseWeight, state, config, now)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("SlowStartWeight(%d, elapsed=%v) = %d, want [%d, %d]",
					tt.baseWeight, tt.elapsed, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestSlowStartWeightAggressiveCurve(t *testing.T) {
	config := SlowStartConfig{
		Window:     10 * time.Second,
		Aggression: 2.0, // slower initial ramp, faster finish
	}
	start := time.Now()

	// With aggression=2.0, factor = (elapsed/window)^(1/2) = sqrt(elapsed/window)
	// At 25% elapsed: sqrt(0.25) = 0.5, so weight ~50
	// At 50% elapsed: sqrt(0.5) ~= 0.707, so weight ~71
	tests := []struct {
		name       string
		elapsed    time.Duration
		baseWeight int
		wantMin    int
		wantMax    int
	}{
		{"25% elapsed, aggression=2", 2500 * time.Millisecond, 100, 49, 51},
		{"50% elapsed, aggression=2", 5 * time.Second, 100, 70, 72},
		{"100% elapsed", 10 * time.Second, 100, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &SlowStartState{StartTime: start}
			now := start.Add(tt.elapsed)

			got := SlowStartWeight(tt.baseWeight, state, config, now)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("SlowStartWeight(%d, elapsed=%v, aggression=2) = %d, want [%d, %d]",
					tt.baseWeight, tt.elapsed, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestSlowStartWeightLowAggression(t *testing.T) {
	config := SlowStartConfig{
		Window:     10 * time.Second,
		Aggression: 0.5, // faster initial ramp, slower finish
	}
	start := time.Now()

	// With aggression=0.5, factor = (elapsed/window)^(1/0.5) = (elapsed/window)^2
	// At 50% elapsed: 0.5^2 = 0.25, so weight ~25
	state := &SlowStartState{StartTime: start}
	now := start.Add(5 * time.Second)

	got := SlowStartWeight(100, state, config, now)
	if got < 24 || got > 26 {
		t.Errorf("SlowStartWeight with aggression=0.5 at 50%%: got %d, want ~25", got)
	}
}

func TestSlowStartWeightEdgeCases(t *testing.T) {
	config := SlowStartConfig{
		Window:     10 * time.Second,
		Aggression: 1.0,
	}
	start := time.Now()

	t.Run("nil state returns baseWeight", func(t *testing.T) {
		got := SlowStartWeight(100, nil, config, start)
		if got != 100 {
			t.Errorf("Expected 100 for nil state, got %d", got)
		}
	})

	t.Run("zero window returns baseWeight", func(t *testing.T) {
		zeroConfig := SlowStartConfig{Window: 0, Aggression: 1.0}
		state := &SlowStartState{StartTime: start}
		got := SlowStartWeight(100, state, zeroConfig, start.Add(time.Second))
		if got != 100 {
			t.Errorf("Expected 100 for zero window, got %d", got)
		}
	})

	t.Run("negative window returns baseWeight", func(t *testing.T) {
		negConfig := SlowStartConfig{Window: -time.Second, Aggression: 1.0}
		state := &SlowStartState{StartTime: start}
		got := SlowStartWeight(100, state, negConfig, start.Add(time.Second))
		if got != 100 {
			t.Errorf("Expected 100 for negative window, got %d", got)
		}
	})

	t.Run("zero baseWeight returns 1", func(t *testing.T) {
		state := &SlowStartState{StartTime: start}
		got := SlowStartWeight(0, state, config, start.Add(5*time.Second))
		if got != 1 {
			t.Errorf("Expected 1 for zero baseWeight, got %d", got)
		}
	})

	t.Run("negative baseWeight returns 1", func(t *testing.T) {
		state := &SlowStartState{StartTime: start}
		got := SlowStartWeight(-10, state, config, start.Add(5*time.Second))
		if got != 1 {
			t.Errorf("Expected 1 for negative baseWeight, got %d", got)
		}
	})

	t.Run("now before startTime returns 1", func(t *testing.T) {
		state := &SlowStartState{StartTime: start.Add(time.Minute)}
		got := SlowStartWeight(100, state, config, start)
		if got != 1 {
			t.Errorf("Expected 1 when now < startTime, got %d", got)
		}
	})

	t.Run("zero aggression defaults to 1.0", func(t *testing.T) {
		zeroAgg := SlowStartConfig{Window: 10 * time.Second, Aggression: 0}
		state := &SlowStartState{StartTime: start}
		got := SlowStartWeight(100, state, zeroAgg, start.Add(5*time.Second))
		// Should behave like aggression=1.0 (linear), so ~50
		if got < 49 || got > 51 {
			t.Errorf("Expected ~50 with zero aggression (defaulting to 1.0), got %d", got)
		}
	})

	t.Run("minimum weight is always 1", func(t *testing.T) {
		state := &SlowStartState{StartTime: start}
		// Very small elapsed time relative to window
		got := SlowStartWeight(100, state, config, start.Add(time.Millisecond))
		if got < 1 {
			t.Errorf("Weight should never be less than 1, got %d", got)
		}
	})
}

// --- SlowStartManager tests ---

func TestSlowStartManagerNewEndpointEntersSlowStart(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(initialEndpoints)
	config := SlowStartConfig{
		Window:     30 * time.Second,
		Aggression: 1.0,
	}

	now := time.Now()
	sm := NewSlowStartManager(inner, config, initialEndpoints)
	sm.nowFunc = func() time.Time { return now }

	// Initial endpoints are NOT in slow start
	if sm.IsInSlowStart(initialEndpoints[0]) {
		t.Error("Initial endpoint should not be in slow start")
	}
	if sm.IsInSlowStart(initialEndpoints[1]) {
		t.Error("Initial endpoint should not be in slow start")
	}

	// Add a new endpoint
	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true}, // new
	}

	sm.UpdateEndpoints(newEndpoints)

	// New endpoint SHOULD be in slow start
	if !sm.IsInSlowStart(newEndpoints[2]) {
		t.Error("Newly added endpoint should be in slow start")
	}

	// Existing endpoints should NOT be in slow start
	if sm.IsInSlowStart(newEndpoints[0]) {
		t.Error("Existing endpoint should not be in slow start")
	}
	if sm.IsInSlowStart(newEndpoints[1]) {
		t.Error("Existing endpoint should not be in slow start")
	}
}

func TestSlowStartManagerRecoveredEndpointEntersSlowStart(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := SlowStartConfig{Window: 30 * time.Second, Aggression: 1.0}

	now := time.Now()
	sm := NewSlowStartManager(inner, config, endpoints)
	sm.nowFunc = func() time.Time { return now }

	// Simulate endpoint 2 going down (removed from list)
	downEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}
	sm.UpdateEndpoints(downEndpoints)

	if sm.IsInSlowStart(downEndpoints[0]) {
		t.Error("Surviving endpoint should not be in slow start")
	}

	// Simulate endpoint 2 recovering (re-appears)
	now = now.Add(5 * time.Minute) // time has passed
	recoveredEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true}, // recovered
	}
	sm.UpdateEndpoints(recoveredEndpoints)

	// Recovered endpoint should be in slow start
	if !sm.IsInSlowStart(recoveredEndpoints[1]) {
		t.Error("Recovered endpoint should be in slow start")
	}

	// Original endpoint should not
	if sm.IsInSlowStart(recoveredEndpoints[0]) {
		t.Error("Original endpoint should not be in slow start")
	}
}

func TestSlowStartManagerWeightReachesFullAfterWindow(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := SlowStartConfig{Window: 10 * time.Second, Aggression: 1.0}

	start := time.Now()
	sm := NewSlowStartManager(inner, config, endpoints)
	sm.nowFunc = func() time.Time { return start }

	// Add new endpoint
	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}
	sm.UpdateEndpoints(newEndpoints)

	ep2 := newEndpoints[1]

	// At start of window, weight should be minimal
	weight := sm.GetEffectiveWeight(ep2, 100)
	if weight > 5 {
		t.Errorf("At start of window, weight should be minimal, got %d", weight)
	}

	// At 50% of window, weight should be ~50
	sm.nowFunc = func() time.Time { return start.Add(5 * time.Second) }
	weight = sm.GetEffectiveWeight(ep2, 100)
	if weight < 45 || weight > 55 {
		t.Errorf("At 50%% of window, weight should be ~50, got %d", weight)
	}

	// At 100% of window, weight should be full
	sm.nowFunc = func() time.Time { return start.Add(10 * time.Second) }
	weight = sm.GetEffectiveWeight(ep2, 100)
	if weight != 100 {
		t.Errorf("At end of window, weight should be 100, got %d", weight)
	}

	// Past window, weight should be full
	sm.nowFunc = func() time.Time { return start.Add(20 * time.Second) }
	weight = sm.GetEffectiveWeight(ep2, 100)
	if weight != 100 {
		t.Errorf("Past window, weight should be 100, got %d", weight)
	}

	// After window, IsInSlowStart should return false
	if sm.IsInSlowStart(ep2) {
		t.Error("Endpoint should not be in slow start after window elapsed")
	}
}

func TestSlowStartManagerGetEffectiveWeight(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := SlowStartConfig{Window: 10 * time.Second, Aggression: 1.0}

	sm := NewSlowStartManager(inner, config, endpoints)

	t.Run("nil endpoint returns baseWeight", func(t *testing.T) {
		got := sm.GetEffectiveWeight(nil, 100)
		if got != 100 {
			t.Errorf("Expected 100 for nil endpoint, got %d", got)
		}
	})

	t.Run("non-slow-start endpoint returns baseWeight", func(t *testing.T) {
		got := sm.GetEffectiveWeight(endpoints[0], 100)
		if got != 100 {
			t.Errorf("Expected 100 for non-slow-start endpoint, got %d", got)
		}
	})

	t.Run("disabled slow start returns baseWeight", func(t *testing.T) {
		disabledConfig := SlowStartConfig{Window: 0}
		disabledSM := NewSlowStartManager(inner, disabledConfig, endpoints)
		got := disabledSM.GetEffectiveWeight(endpoints[0], 100)
		if got != 100 {
			t.Errorf("Expected 100 for disabled slow start, got %d", got)
		}
	})
}

func TestSlowStartManagerSelect(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := SlowStartConfig{Window: 30 * time.Second, Aggression: 1.0}

	sm := NewSlowStartManager(inner, config, endpoints)

	t.Run("select returns non-nil for healthy endpoints", func(t *testing.T) {
		for i := 0; i < 20; i++ {
			ep := sm.Select()
			if ep == nil {
				t.Fatal("Select returned nil")
			}
		}
	})

	t.Run("select returns nil when inner returns nil", func(t *testing.T) {
		emptyEndpoints := []*pb.Endpoint{}
		emptyInner := NewRoundRobin(emptyEndpoints)
		emptySM := NewSlowStartManager(emptyInner, config, emptyEndpoints)

		ep := emptySM.Select()
		if ep != nil {
			t.Error("Expected nil when no endpoints")
		}
	})

	t.Run("disabled window delegates directly", func(t *testing.T) {
		disabledConfig := SlowStartConfig{Window: 0}
		disabledSM := NewSlowStartManager(inner, disabledConfig, endpoints)

		for i := 0; i < 10; i++ {
			ep := disabledSM.Select()
			if ep == nil {
				t.Fatal("Select returned nil")
			}
		}
	})
}

func TestSlowStartManagerRegisterForSlowStart(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := SlowStartConfig{Window: 10 * time.Second, Aggression: 1.0}

	sm := NewSlowStartManager(inner, config, endpoints)

	// Endpoint 1 is initially not in slow start
	if sm.IsInSlowStart(endpoints[0]) {
		t.Error("Endpoint should not be in slow start initially")
	}

	// Explicitly register for slow start (e.g., health check recovery)
	sm.RegisterForSlowStart(endpoints[0])

	if !sm.IsInSlowStart(endpoints[0]) {
		t.Error("Endpoint should be in slow start after registration")
	}

	// Nil should not panic
	sm.RegisterForSlowStart(nil)
}

func TestSlowStartManagerPurgeExpired(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := SlowStartConfig{Window: 5 * time.Second, Aggression: 1.0}

	start := time.Now()
	sm := NewSlowStartManager(inner, config, endpoints)
	sm.nowFunc = func() time.Time { return start }

	// Add a new endpoint to trigger slow start
	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}
	sm.UpdateEndpoints(newEndpoints)

	// Endpoint 2 is in slow start
	if !sm.IsInSlowStart(newEndpoints[1]) {
		t.Fatal("Expected endpoint to be in slow start")
	}

	// Advance time past the window and purge
	sm.nowFunc = func() time.Time { return start.Add(10 * time.Second) }
	sm.PurgeExpired()

	// State should be cleaned up
	sm.mu.RLock()
	_, exists := sm.states[endpointKey(newEndpoints[1])]
	sm.mu.RUnlock()

	if exists {
		t.Error("Expected expired state to be purged")
	}
}

func TestSlowStartManagerConcurrentAccess(_ *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := SlowStartConfig{Window: 10 * time.Second, Aggression: 1.0}
	sm := NewSlowStartManager(inner, config, endpoints)

	var wg sync.WaitGroup

	// Concurrent selects
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sm.Select()
		}()
	}

	// Concurrent IsInSlowStart checks
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = sm.IsInSlowStart(endpoints[idx%len(endpoints)])
		}(i)
	}

	// Concurrent GetEffectiveWeight
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = sm.GetEffectiveWeight(endpoints[idx%len(endpoints)], 100)
		}(i)
	}

	// Concurrent UpdateEndpoints
	wg.Add(1)
	go func() {
		defer wg.Done()
		newEndpoints := []*pb.Endpoint{
			{Address: "10.0.0.4", Port: 8080, Ready: true},
			{Address: "10.0.0.5", Port: 8080, Ready: true},
		}
		sm.UpdateEndpoints(newEndpoints)
	}()

	wg.Wait()
	// Test passes if no race condition detected
}

func TestSlowStartManagerWeightCurveOverTime(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := SlowStartConfig{Window: 10 * time.Second, Aggression: 1.0}

	start := time.Now()
	sm := NewSlowStartManager(inner, config, endpoints)
	sm.nowFunc = func() time.Time { return start }

	// Add a new endpoint
	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}
	sm.UpdateEndpoints(newEndpoints)

	ep := newEndpoints[1]
	baseWeight := 100

	// Verify monotonically increasing weight over time
	prevWeight := 0
	for pct := 1; pct <= 100; pct++ {
		elapsed := time.Duration(pct) * config.Window / 100
		sm.nowFunc = func() time.Time { return start.Add(elapsed) }

		weight := sm.GetEffectiveWeight(ep, baseWeight)

		if weight < prevWeight {
			t.Errorf("Weight decreased at %d%%: %d < %d", pct, weight, prevWeight)
		}
		prevWeight = weight
	}

	// Final weight should be the base weight
	if prevWeight != baseWeight {
		t.Errorf("Final weight should be %d, got %d", baseWeight, prevWeight)
	}
}

func TestSlowStartManagerWithP2C(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewP2C(endpoints)
	config := SlowStartConfig{Window: 10 * time.Second, Aggression: 1.0}

	sm := NewSlowStartManager(inner, config, endpoints)

	// Select should work correctly with P2C
	for i := 0; i < 20; i++ {
		ep := sm.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
	}

	// Add new endpoint
	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}
	sm.UpdateEndpoints(newEndpoints)
	if len(newEndpoints) < 3 {
		t.Fatal("test requires at least 3 new endpoints")
	}

	// New endpoint should be in slow start
	if !sm.IsInSlowStart(newEndpoints[2]) {
		t.Error("New endpoint should be in slow start")
	}

	// Select should still work
	for i := 0; i < 20; i++ {
		ep := sm.Select()
		if ep == nil {
			t.Fatal("Select returned nil after update")
		}
	}
}

func TestSlowStartManagerWithEWMA(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewEWMA(endpoints)
	config := SlowStartConfig{Window: 10 * time.Second, Aggression: 1.0}

	sm := NewSlowStartManager(inner, config, endpoints)

	for i := 0; i < 10; i++ {
		ep := sm.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
	}
}

func TestSlowStartManagerWithLeastConn(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewLeastConn(endpoints)
	config := SlowStartConfig{Window: 10 * time.Second, Aggression: 1.0}

	sm := NewSlowStartManager(inner, config, endpoints)

	for i := 0; i < 10; i++ {
		ep := sm.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
	}
}

func TestSlowStartManagerEndpointNotReadyIgnored(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := SlowStartConfig{Window: 10 * time.Second, Aggression: 1.0}

	sm := NewSlowStartManager(inner, config, endpoints)

	// Add an endpoint that is not ready
	updatedEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false}, // not ready
	}

	sm.UpdateEndpoints(updatedEndpoints)

	// Not-ready endpoint should NOT be tracked for slow start
	notReadyEP := &pb.Endpoint{Address: "10.0.0.2", Port: 8080, Ready: false}
	if sm.IsInSlowStart(notReadyEP) {
		t.Error("Not-ready endpoint should not be in slow start")
	}
}

func TestSlowStartManagerCleanupOnEndpointRemoval(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := SlowStartConfig{Window: 30 * time.Second, Aggression: 1.0}

	sm := NewSlowStartManager(inner, config, endpoints)

	// Add new endpoint
	withNewEP := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}
	sm.UpdateEndpoints(withNewEP)

	if !sm.IsInSlowStart(withNewEP[1]) {
		t.Fatal("New endpoint should be in slow start")
	}

	// Remove the new endpoint
	sm.UpdateEndpoints(endpoints)

	// State for removed endpoint should be cleaned up
	sm.mu.RLock()
	_, exists := sm.states["10.0.0.2:8080"]
	sm.mu.RUnlock()

	if exists {
		t.Error("State for removed endpoint should be cleaned up")
	}
}

func TestSlowStartConfigNormalizedAggression(t *testing.T) {
	tests := []struct {
		name       string
		aggression float64
		want       float64
	}{
		{"positive", 2.0, 2.0},
		{"one", 1.0, 1.0},
		{"zero defaults to 1", 0, 1.0},
		{"negative defaults to 1", -1.0, 1.0},
		{"small positive", 0.1, 0.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := SlowStartConfig{Aggression: tt.aggression}
			got := c.normalizedAggression()
			if got != tt.want {
				t.Errorf("normalizedAggression() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestDefaultSlowStartConfig(t *testing.T) {
	config := DefaultSlowStartConfig()

	if config.Window != 30*time.Second {
		t.Errorf("Expected window 30s, got %v", config.Window)
	}
	if config.Aggression != 1.0 {
		t.Errorf("Expected aggression 1.0, got %f", config.Aggression)
	}
}
