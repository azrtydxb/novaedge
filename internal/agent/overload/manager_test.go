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

package overload

import (
	"testing"

	"go.uber.org/zap"
)

func TestManager_NormalState(t *testing.T) {
	config := DefaultOverloadConfig()
	config.Enabled = true
	// Set very high thresholds so we never trigger.
	config.MemoryThreshold = 0.99
	config.GoroutineThreshold = 10000000
	config.MaxActiveConnections = 10000000

	m := NewManager(config, zap.NewNop())

	if m.ShouldShed() {
		t.Error("expected ShouldShed=false in normal state")
	}

	state := m.GetState()
	if state.IsOverloaded {
		t.Error("expected IsOverloaded=false in initial state")
	}
}

func TestManager_DisabledNeverSheds(t *testing.T) {
	config := DefaultOverloadConfig()
	config.Enabled = false

	m := NewManager(config, zap.NewNop())

	// Even if we manually set overloaded, ShouldShed respects Enabled flag.
	if m.ShouldShed() {
		t.Error("disabled manager should never shed")
	}
}

func TestManager_MemoryThresholdTriggers(t *testing.T) {
	config := DefaultOverloadConfig()
	config.Enabled = true
	// Set a low memory threshold to trigger easily.
	config.MemoryThreshold = 0.01
	config.MemoryRecoverThreshold = 0.005
	// Set high thresholds for other monitors.
	config.GoroutineThreshold = 10000000
	config.MaxActiveConnections = 10000000
	config.MemoryLimitBytes = 1024 // tiny limit to ensure ratio is high

	m := NewManager(config, zap.NewNop())
	m.checkResources()

	if !m.ShouldShed() {
		t.Error("expected ShouldShed=true when memory threshold exceeded")
	}

	state := m.GetState()
	if state.ShedReason != "memory" {
		t.Errorf("expected shed reason 'memory', got %q", state.ShedReason)
	}
}

func TestManager_GoroutineThresholdTriggers(t *testing.T) {
	config := DefaultOverloadConfig()
	config.Enabled = true
	// Set goroutine threshold to 1 to trigger easily.
	config.GoroutineThreshold = 1
	config.GoroutineRecoverThreshold = 0
	// Set high thresholds for other monitors.
	config.MemoryThreshold = 0.99
	config.MaxActiveConnections = 10000000

	m := NewManager(config, zap.NewNop())
	m.checkResources()

	if !m.ShouldShed() {
		t.Error("expected ShouldShed=true when goroutine threshold exceeded")
	}

	state := m.GetState()
	if state.ShedReason != "goroutines" {
		t.Errorf("expected shed reason 'goroutines', got %q", state.ShedReason)
	}
}

func TestManager_ConnectionThresholdTriggers(t *testing.T) {
	config := DefaultOverloadConfig()
	config.Enabled = true
	// Set connection threshold to 1.
	config.MaxActiveConnections = 1
	config.ActiveConnectionRecoverThreshold = 0
	// Set high thresholds for other monitors.
	config.MemoryThreshold = 0.99
	config.GoroutineThreshold = 10000000

	m := NewManager(config, zap.NewNop())
	m.IncrementConnections()
	m.checkResources()

	if !m.ShouldShed() {
		t.Error("expected ShouldShed=true when connection threshold exceeded")
	}

	state := m.GetState()
	if state.ShedReason != "connections" {
		t.Errorf("expected shed reason 'connections', got %q", state.ShedReason)
	}
}

func TestManager_Hysteresis(t *testing.T) {
	config := DefaultOverloadConfig()
	config.Enabled = true
	// Use connection threshold for easy testing.
	config.MaxActiveConnections = 5
	config.ActiveConnectionRecoverThreshold = 3
	// Set high thresholds for other monitors.
	config.MemoryThreshold = 0.99
	config.GoroutineThreshold = 10000000

	m := NewManager(config, zap.NewNop())

	// Add 5 connections to trigger overload.
	for i := int64(0); i < 5; i++ {
		m.IncrementConnections()
	}
	m.checkResources()
	if !m.ShouldShed() {
		t.Error("expected overload at 5 connections")
	}

	// Drop to 4 - still above recover threshold (3), should remain overloaded.
	m.DecrementConnections()
	m.checkResources()
	if !m.ShouldShed() {
		t.Error("expected to remain overloaded at 4 connections (recover threshold is 3)")
	}

	// Drop to 2 - below recover threshold, should recover.
	m.DecrementConnections()
	m.DecrementConnections()
	m.checkResources()
	if m.ShouldShed() {
		t.Error("expected recovery at 2 connections")
	}
}

func TestManager_ConnectionCounting(t *testing.T) {
	config := DefaultOverloadConfig()
	m := NewManager(config, zap.NewNop())

	m.IncrementConnections()
	m.IncrementConnections()
	m.IncrementConnections()

	if got := m.activeConns.Load(); got != 3 {
		t.Errorf("expected 3 active connections, got %d", got)
	}

	m.DecrementConnections()
	if got := m.activeConns.Load(); got != 2 {
		t.Errorf("expected 2 active connections, got %d", got)
	}
}

func TestManager_Callbacks(t *testing.T) {
	config := DefaultOverloadConfig()
	config.Enabled = true
	config.MaxActiveConnections = 2
	config.ActiveConnectionRecoverThreshold = 1
	config.MemoryThreshold = 0.99
	config.GoroutineThreshold = 10000000

	m := NewManager(config, zap.NewNop())

	startCalled := false
	endCalled := false
	m.SetCallbacks(
		func() { startCalled = true },
		func() { endCalled = true },
	)

	// Trigger overload.
	m.IncrementConnections()
	m.IncrementConnections()
	m.checkResources()
	if !startCalled {
		t.Error("expected onOverloadStart callback to be called")
	}

	// Recover.
	m.DecrementConnections()
	m.DecrementConnections()
	m.checkResources()
	if !endCalled {
		t.Error("expected onOverloadEnd callback to be called")
	}
}
