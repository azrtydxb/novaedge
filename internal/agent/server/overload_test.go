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

package server

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// mockResourceReader provides controllable resource readings for tests.
type mockResourceReader struct {
	heapAlloc  atomic.Int64
	goroutines atomic.Int32
	openFDs    atomic.Int32
}

func (m *mockResourceReader) ReadMemStats(stats *runtime.MemStats) {
	stats.HeapAlloc = uint64(m.heapAlloc.Load())
}

func (m *mockResourceReader) NumGoroutine() int {
	return int(m.goroutines.Load())
}

func (m *mockResourceReader) CountOpenFDs() int {
	return int(m.openFDs.Load())
}

func newTestLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func TestOverloadManager_NotOverloadedByDefault(t *testing.T) {
	t.Parallel()

	mock := &mockResourceReader{}
	mock.heapAlloc.Store(0)
	mock.goroutines.Store(10)
	mock.openFDs.Store(5)

	config := OverloadConfig{
		MemoryThresholdBytes: 1024 * 1024 * 100, // 100MB
		GoroutineThreshold:   1000,
		FDThreshold:          500,
		CheckInterval:        10 * time.Millisecond,
	}

	om := newOverloadManagerWithReader(config, newTestLogger(), mock)

	// Before starting, should not be overloaded
	if om.IsOverloaded() {
		t.Fatal("expected not overloaded before start")
	}
	if om.CurrentAction() != ActionNone {
		t.Fatalf("expected ActionNone, got %v", om.CurrentAction())
	}
}

func TestOverloadManager_MemoryOverload(t *testing.T) {
	t.Parallel()

	mock := &mockResourceReader{}
	mock.heapAlloc.Store(50 * 1024 * 1024) // 50MB - under threshold
	mock.goroutines.Store(10)
	mock.openFDs.Store(5)

	config := OverloadConfig{
		MemoryThresholdBytes: 100 * 1024 * 1024, // 100MB threshold
		CheckInterval:        10 * time.Millisecond,
	}

	om := newOverloadManagerWithReader(config, newTestLogger(), mock)

	// Run check manually - should not be overloaded
	om.check()
	if om.IsOverloaded() {
		t.Fatal("expected not overloaded with heap under threshold")
	}

	// Exceed memory threshold
	mock.heapAlloc.Store(150 * 1024 * 1024) // 150MB - over 100MB threshold
	om.check()

	if !om.IsOverloaded() {
		t.Fatal("expected overloaded with heap over threshold")
	}
	if om.CurrentAction() != ActionRejectNew {
		t.Fatalf("expected ActionRejectNew, got %v", om.CurrentAction())
	}
}

func TestOverloadManager_GoroutineOverload(t *testing.T) {
	t.Parallel()

	mock := &mockResourceReader{}
	mock.heapAlloc.Store(0)
	mock.goroutines.Store(50) // under threshold
	mock.openFDs.Store(5)

	config := OverloadConfig{
		GoroutineThreshold: 100,
		CheckInterval:      10 * time.Millisecond,
	}

	om := newOverloadManagerWithReader(config, newTestLogger(), mock)

	// Run check - should not be overloaded
	om.check()
	if om.IsOverloaded() {
		t.Fatal("expected not overloaded with goroutines under threshold")
	}

	// Exceed goroutine threshold
	mock.goroutines.Store(150) // over 100 threshold
	om.check()

	if !om.IsOverloaded() {
		t.Fatal("expected overloaded with goroutines over threshold")
	}
	if om.CurrentAction() != ActionRejectNew {
		t.Fatalf("expected ActionRejectNew, got %v", om.CurrentAction())
	}
}

func TestOverloadManager_FDOverload(t *testing.T) {
	t.Parallel()

	mock := &mockResourceReader{}
	mock.heapAlloc.Store(0)
	mock.goroutines.Store(10)
	mock.openFDs.Store(50) // under threshold

	config := OverloadConfig{
		FDThreshold:   100,
		CheckInterval: 10 * time.Millisecond,
	}

	om := newOverloadManagerWithReader(config, newTestLogger(), mock)

	// Run check - should not be overloaded
	om.check()
	if om.IsOverloaded() {
		t.Fatal("expected not overloaded with FDs under threshold")
	}

	// Exceed FD threshold
	mock.openFDs.Store(150) // over 100 threshold
	om.check()

	if !om.IsOverloaded() {
		t.Fatal("expected overloaded with FDs over threshold")
	}
}

func TestOverloadManager_HysteresisRecovery(t *testing.T) {
	t.Parallel()

	mock := &mockResourceReader{}
	mock.heapAlloc.Store(0)
	mock.goroutines.Store(10)
	mock.openFDs.Store(5)

	// 100MB threshold => recovery at 90MB (0.9 * 100MB)
	config := OverloadConfig{
		MemoryThresholdBytes: 100 * 1024 * 1024,
		CheckInterval:        10 * time.Millisecond,
	}

	om := newOverloadManagerWithReader(config, newTestLogger(), mock)

	// Exceed threshold
	mock.heapAlloc.Store(150 * 1024 * 1024) // 150MB
	om.check()
	if !om.IsOverloaded() {
		t.Fatal("expected overloaded")
	}

	// Drop below threshold but above hysteresis band (between 90MB and 100MB)
	// 95MB is above 90MB (recovery point), so should remain overloaded
	mock.heapAlloc.Store(95 * 1024 * 1024)
	om.check()
	if !om.IsOverloaded() {
		t.Fatal("expected still overloaded within hysteresis band")
	}

	// Drop below hysteresis recovery point (below 90MB)
	mock.heapAlloc.Store(80 * 1024 * 1024) // 80MB < 90MB
	om.check()
	if om.IsOverloaded() {
		t.Fatal("expected recovered after dropping below hysteresis threshold")
	}
	if om.CurrentAction() != ActionNone {
		t.Fatalf("expected ActionNone after recovery, got %v", om.CurrentAction())
	}
}

func TestOverloadManager_GoroutineHysteresisRecovery(t *testing.T) {
	t.Parallel()

	mock := &mockResourceReader{}
	mock.heapAlloc.Store(0)
	mock.goroutines.Store(10)
	mock.openFDs.Store(5)

	// 1000 goroutine threshold => recovery at 900 (0.9 * 1000)
	config := OverloadConfig{
		GoroutineThreshold: 1000,
		CheckInterval:      10 * time.Millisecond,
	}

	om := newOverloadManagerWithReader(config, newTestLogger(), mock)

	// Exceed threshold
	mock.goroutines.Store(1500)
	om.check()
	if !om.IsOverloaded() {
		t.Fatal("expected overloaded")
	}

	// Drop into hysteresis band (between 900 and 1000)
	mock.goroutines.Store(950)
	om.check()
	if !om.IsOverloaded() {
		t.Fatal("expected still overloaded within hysteresis band")
	}

	// Drop below recovery threshold
	mock.goroutines.Store(800) // 800 < 900
	om.check()
	if om.IsOverloaded() {
		t.Fatal("expected recovered after dropping below hysteresis threshold")
	}
}

func TestOverloadMiddleware_Returns503WhenOverloaded(t *testing.T) {
	t.Parallel()

	mock := &mockResourceReader{}
	mock.heapAlloc.Store(150 * 1024 * 1024) // 150MB - over threshold
	mock.goroutines.Store(10)
	mock.openFDs.Store(5)

	config := OverloadConfig{
		MemoryThresholdBytes: 100 * 1024 * 1024,
		CheckInterval:        10 * time.Millisecond,
	}

	om := newOverloadManagerWithReader(config, newTestLogger(), mock)

	// Trigger overload detection
	om.check()
	if !om.IsOverloaded() {
		t.Fatal("expected overloaded for middleware test")
	}

	// Create middleware chain
	nextCalled := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		nextCalled = true
	})

	middleware := OverloadMiddleware(om)(next)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != overloadRetryAfterSeconds {
		t.Fatalf("expected Retry-After header %q, got %q", overloadRetryAfterSeconds, rec.Header().Get("Retry-After"))
	}
	if nextCalled {
		t.Fatal("expected next handler to NOT be called when overloaded")
	}
}

func TestOverloadMiddleware_PassesThroughWhenNotOverloaded(t *testing.T) {
	t.Parallel()

	mock := &mockResourceReader{}
	mock.heapAlloc.Store(50 * 1024 * 1024) // 50MB - under threshold
	mock.goroutines.Store(10)
	mock.openFDs.Store(5)

	config := OverloadConfig{
		MemoryThresholdBytes: 100 * 1024 * 1024,
		CheckInterval:        10 * time.Millisecond,
	}

	om := newOverloadManagerWithReader(config, newTestLogger(), mock)

	// Ensure not overloaded
	om.check()
	if om.IsOverloaded() {
		t.Fatal("expected not overloaded for passthrough test")
	}

	// Create middleware chain
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := OverloadMiddleware(om)(next)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !nextCalled {
		t.Fatal("expected next handler to be called when not overloaded")
	}
	if rec.Header().Get("Retry-After") != "" {
		t.Fatal("expected no Retry-After header when not overloaded")
	}
}

func TestOverloadManager_DisabledThresholds(t *testing.T) {
	t.Parallel()

	mock := &mockResourceReader{}
	mock.heapAlloc.Store(999 * 1024 * 1024) // very high
	mock.goroutines.Store(99999)            // very high
	mock.openFDs.Store(99999)               // very high

	// All thresholds disabled (0)
	config := OverloadConfig{
		MemoryThresholdBytes: 0,
		GoroutineThreshold:   0,
		FDThreshold:          0,
		CheckInterval:        10 * time.Millisecond,
	}

	om := newOverloadManagerWithReader(config, newTestLogger(), mock)
	om.check()

	if om.IsOverloaded() {
		t.Fatal("expected not overloaded when all thresholds are disabled")
	}
}

func TestOverloadManager_StartStop(t *testing.T) {
	t.Parallel()

	mock := &mockResourceReader{}
	mock.heapAlloc.Store(0)
	mock.goroutines.Store(10)
	mock.openFDs.Store(5)

	config := OverloadConfig{
		MemoryThresholdBytes: 100 * 1024 * 1024,
		CheckInterval:        10 * time.Millisecond,
	}

	om := newOverloadManagerWithReader(config, newTestLogger(), mock)

	// Start in background
	go om.Start(t.Context())

	// Allow a few check cycles
	time.Sleep(50 * time.Millisecond)

	// Should not be overloaded
	if om.IsOverloaded() {
		t.Fatal("expected not overloaded")
	}

	// Trigger overload
	mock.heapAlloc.Store(150 * 1024 * 1024)

	// Wait for check cycle
	time.Sleep(50 * time.Millisecond)

	if !om.IsOverloaded() {
		t.Fatal("expected overloaded after exceeding threshold")
	}

	// Stop should complete without hanging
	om.Stop()

	// Stop is idempotent
	om.Stop()
}

func TestOverloadManager_DefaultCheckInterval(t *testing.T) {
	t.Parallel()

	config := OverloadConfig{
		CheckInterval: 0, // should default to 1s
	}

	om := NewOverloadManager(config, newTestLogger())

	if om.config.CheckInterval != defaultOverloadCheckInterval {
		t.Fatalf("expected default check interval %v, got %v", defaultOverloadCheckInterval, om.config.CheckInterval)
	}
}

func TestOverloadAction_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		action   OverloadAction
		expected string
	}{
		{ActionNone, "none"},
		{ActionRejectNew, "reject_new"},
		{ActionReduceTimeouts, "reduce_timeouts"},
		{OverloadAction(99), "unknown(99)"},
	}

	for _, tc := range tests {
		if tc.action.String() != tc.expected {
			t.Errorf("expected %q for action %d, got %q", tc.expected, int(tc.action), tc.action.String())
		}
	}
}

func TestOverloadManager_MultipleThresholds(t *testing.T) {
	t.Parallel()

	mock := &mockResourceReader{}
	mock.heapAlloc.Store(50 * 1024 * 1024) // under memory threshold
	mock.goroutines.Store(50)              // under goroutine threshold
	mock.openFDs.Store(50)                 // under FD threshold

	config := OverloadConfig{
		MemoryThresholdBytes: 100 * 1024 * 1024,
		GoroutineThreshold:   100,
		FDThreshold:          100,
		CheckInterval:        10 * time.Millisecond,
	}

	om := newOverloadManagerWithReader(config, newTestLogger(), mock)

	// All under threshold
	om.check()
	if om.IsOverloaded() {
		t.Fatal("expected not overloaded when all resources under threshold")
	}

	// Only goroutines exceed threshold
	mock.goroutines.Store(150)
	om.check()
	if !om.IsOverloaded() {
		t.Fatal("expected overloaded when goroutines exceed threshold")
	}

	// Recover goroutines, but now FDs exceed
	mock.goroutines.Store(10)
	mock.openFDs.Store(150)
	om.check()
	if !om.IsOverloaded() {
		t.Fatal("expected overloaded when FDs exceed threshold")
	}

	// Recover everything
	mock.openFDs.Store(10)
	om.check()
	if om.IsOverloaded() {
		t.Fatal("expected recovered when all resources under threshold")
	}
}
