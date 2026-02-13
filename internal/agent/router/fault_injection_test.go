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

package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// newTestFaultMiddleware creates a FaultInjectionMiddleware with a deterministic
// random function for testing. The randFunc is called once per delay check and
// once per abort check per request.
func newTestFaultMiddleware(config *FaultInjectionConfig, randFunc func() float64) *FaultInjectionMiddleware {
	m := NewFaultInjectionMiddleware(config, zap.NewNop())
	m.randFloat = randFunc
	return m
}

// okHandler is a simple handler that returns 200 OK with a known body.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
})

func TestFaultInjection_DelayInjection(t *testing.T) {
	// Always inject delay (randFloat returns 0, which is < any positive percent)
	m := newTestFaultMiddleware(&FaultInjectionConfig{
		DelayDuration: 50 * time.Millisecond,
		DelayPercent:  100,
	}, func() float64 { return 0 })

	handler := m.Wrap(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	// Verify the delay was applied (with some tolerance)
	if elapsed < 40*time.Millisecond {
		t.Errorf("expected delay of ~50ms, but elapsed was %v", elapsed)
	}

	// Verify delay header was set
	faultHeader := rec.Header().Get(faultInjectedHeader)
	if faultHeader != "delay=50ms" {
		t.Errorf("expected X-Fault-Injected header 'delay=50ms', got %q", faultHeader)
	}

	// Verify the request still reached the backend
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", rec.Body.String())
	}
}

func TestFaultInjection_AbortInjection(t *testing.T) {
	// Always inject abort (randFloat returns 0)
	m := newTestFaultMiddleware(&FaultInjectionConfig{
		AbortStatusCode: http.StatusServiceUnavailable,
		AbortPercent:    100,
	}, func() float64 { return 0 })

	handler := m.Wrap(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Verify the abort status code
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	// Verify abort header
	faultHeader := rec.Header().Get(faultInjectedHeader)
	if faultHeader != "abort=503" {
		t.Errorf("expected X-Fault-Injected header 'abort=503', got %q", faultHeader)
	}

	// Verify JSON error body
	var errResp faultErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status_code %d in body, got %d", http.StatusServiceUnavailable, errResp.StatusCode)
	}
	if errResp.FaultType != "injected_abort" {
		t.Errorf("expected fault_type 'injected_abort', got %q", errResp.FaultType)
	}

	// Verify Content-Type
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", ct)
	}
}

func TestFaultInjection_ZeroPercentNoFaults(t *testing.T) {
	// Both percentages are 0, so no faults should be injected
	m := newTestFaultMiddleware(&FaultInjectionConfig{
		DelayDuration:   100 * time.Millisecond,
		DelayPercent:    0,
		AbortStatusCode: http.StatusServiceUnavailable,
		AbortPercent:    0,
	}, func() float64 { return 0 })

	handler := m.Wrap(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	// Should return immediately with no faults
	if elapsed > 20*time.Millisecond {
		t.Errorf("expected no delay, but elapsed was %v", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Header().Get(faultInjectedHeader) != "" {
		t.Errorf("expected no fault header, got %q", rec.Header().Get(faultInjectedHeader))
	}
}

func TestFaultInjection_PercentageFiltering(t *testing.T) {
	// Set delay at 50%, abort at 50%
	// The rand function returns 75, which is >= 50, so neither should fire
	m := newTestFaultMiddleware(&FaultInjectionConfig{
		DelayDuration:   100 * time.Millisecond,
		DelayPercent:    50,
		AbortStatusCode: http.StatusServiceUnavailable,
		AbortPercent:    50,
	}, func() float64 { return 75 })

	handler := m.Wrap(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	// Neither fault should fire
	if elapsed > 20*time.Millisecond {
		t.Errorf("expected no delay, but elapsed was %v", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Header().Get(faultInjectedHeader) != "" {
		t.Errorf("expected no fault header, got %q", rec.Header().Get(faultInjectedHeader))
	}
}

func TestFaultInjection_PercentageFilteringApplies(t *testing.T) {
	// Set delay at 50%, abort at 50%
	// The rand function returns 25, which is < 50, so both should fire
	m := newTestFaultMiddleware(&FaultInjectionConfig{
		DelayDuration:   50 * time.Millisecond,
		DelayPercent:    50,
		AbortStatusCode: http.StatusBadGateway,
		AbortPercent:    50,
	}, func() float64 { return 25 })

	handler := m.Wrap(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	// Delay should have been applied
	if elapsed < 40*time.Millisecond {
		t.Errorf("expected delay of ~50ms, but elapsed was %v", elapsed)
	}

	// Abort should have been applied
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected status %d, got %d", http.StatusBadGateway, rec.Code)
	}

	// Both headers should be present
	headers := rec.Header().Values(faultInjectedHeader)
	if len(headers) != 2 {
		t.Fatalf("expected 2 X-Fault-Injected headers, got %d: %v", len(headers), headers)
	}
	if headers[0] != "delay=50ms" {
		t.Errorf("expected first header 'delay=50ms', got %q", headers[0])
	}
	if headers[1] != "abort=502" {
		t.Errorf("expected second header 'abort=502', got %q", headers[1])
	}
}

func TestFaultInjection_HeaderActivationRequired(t *testing.T) {
	// Fault injection requires the x-fault-inject header
	m := newTestFaultMiddleware(&FaultInjectionConfig{
		DelayDuration:    50 * time.Millisecond,
		DelayPercent:     100,
		AbortStatusCode:  http.StatusServiceUnavailable,
		AbortPercent:     100,
		HeaderActivation: "x-fault-inject",
	}, func() float64 { return 0 })

	handler := m.Wrap(okHandler)

	// Request WITHOUT the activation header
	t.Run("without activation header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		start := time.Now()
		handler.ServeHTTP(rec, req)
		elapsed := time.Since(start)

		// Should pass through without fault
		if elapsed > 20*time.Millisecond {
			t.Errorf("expected no delay, but elapsed was %v", elapsed)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
		if rec.Header().Get(faultInjectedHeader) != "" {
			t.Errorf("expected no fault header, got %q", rec.Header().Get(faultInjectedHeader))
		}
	})

	// Request WITH the activation header
	t.Run("with activation header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("x-fault-inject", "true")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// Abort should have fired (delay + abort both at 100%)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}
	})
}

func TestFaultInjection_DelayAndAbortTogether(t *testing.T) {
	// Both delay and abort at 100%
	m := newTestFaultMiddleware(&FaultInjectionConfig{
		DelayDuration:   50 * time.Millisecond,
		DelayPercent:    100,
		AbortStatusCode: http.StatusInternalServerError,
		AbortPercent:    100,
	}, func() float64 { return 0 })

	// Track whether the downstream handler is called
	var handlerCalled atomic.Bool
	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled.Store(true)
		w.WriteHeader(http.StatusOK)
	})

	handler := m.Wrap(downstream)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	// Delay should have been applied
	if elapsed < 40*time.Millisecond {
		t.Errorf("expected delay of ~50ms, but elapsed was %v", elapsed)
	}

	// Abort should have prevented downstream from being called
	if handlerCalled.Load() {
		t.Error("expected downstream handler NOT to be called when abort is injected")
	}

	// Status code should be the abort code
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestFaultInjection_NilConfig(t *testing.T) {
	m := NewFaultInjectionMiddleware(nil, zap.NewNop())
	handler := m.Wrap(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", rec.Body.String())
	}
}

func TestFaultInjection_HundredPercentAlwaysFaults(t *testing.T) {
	// Use a real random function; at 100% every request should be faulted
	m := NewFaultInjectionMiddleware(&FaultInjectionConfig{
		AbortStatusCode: http.StatusTeapot,
		AbortPercent:    100,
	}, zap.NewNop())

	handler := m.Wrap(okHandler)

	for i := range 20 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusTeapot {
			t.Errorf("iteration %d: expected status %d, got %d", i, http.StatusTeapot, rec.Code)
		}
	}
}

func TestFaultInjection_DelayOnlyNoAbort(t *testing.T) {
	// Only delay is configured, no abort
	m := newTestFaultMiddleware(&FaultInjectionConfig{
		DelayDuration: 30 * time.Millisecond,
		DelayPercent:  100,
	}, func() float64 { return 0 })

	handler := m.Wrap(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should have delay but still reach backend
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", rec.Body.String())
	}

	// Should have delay header but no abort header
	headers := rec.Header().Values(faultInjectedHeader)
	if len(headers) != 1 {
		t.Fatalf("expected 1 X-Fault-Injected header, got %d: %v", len(headers), headers)
	}
	if headers[0] != "delay=30ms" {
		t.Errorf("expected header 'delay=30ms', got %q", headers[0])
	}
}
