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
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

const testProtoHTTP11 = "HTTP/1.1"

func newTestDrainManager(timeout time.Duration) *DrainManager {
	logger, _ := zap.NewDevelopment()
	return NewDrainManager(logger, timeout)
}

func TestDrainManagerDefaults(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	dm := NewDrainManager(logger, 0)
	if dm.drainTimeout != DefaultDrainTimeout {
		t.Errorf("expected default timeout %v, got %v", DefaultDrainTimeout, dm.drainTimeout)
	}
	if dm.IsDraining() {
		t.Error("new DrainManager should not be in draining state")
	}
	if dm.ActiveConnections() != 0 {
		t.Errorf("expected 0 active connections, got %d", dm.ActiveConnections())
	}
}

func TestDrainMiddleware_NotDraining(t *testing.T) {
	dm := newTestDrainManager(5 * time.Second)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := dm.DrainMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Simulate HTTP/1.1
	req.Proto = testProtoHTTP11
	req.ProtoMajor = 1
	req.ProtoMinor = 1

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Connection header should NOT be set when not draining
	if connHeader := rr.Header().Get("Connection"); connHeader != "" {
		t.Errorf("expected no Connection header when not draining, got %q", connHeader)
	}
}

func TestDrainMiddleware_DrainingHTTP11(t *testing.T) {
	dm := newTestDrainManager(5 * time.Second)

	// Manually set draining to true for this test
	dm.draining.Store(true)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := dm.DrainMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Proto = testProtoHTTP11
	req.ProtoMajor = 1
	req.ProtoMinor = 1

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	connHeader := rr.Header().Get("Connection")
	if connHeader != "close" {
		t.Errorf("expected Connection: close when draining HTTP/1.1, got %q", connHeader)
	}
}

func TestDrainMiddleware_DrainingHTTP2(t *testing.T) {
	dm := newTestDrainManager(5 * time.Second)

	dm.draining.Store(true)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := dm.DrainMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Proto = "HTTP/2.0"
	req.ProtoMajor = 2
	req.ProtoMinor = 0

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Connection header should NOT be set for HTTP/2
	if connHeader := rr.Header().Get("Connection"); connHeader != "" {
		t.Errorf("expected no Connection header for HTTP/2, got %q", connHeader)
	}
}

func TestDrainWaitsForActiveConnections(t *testing.T) {
	dm := newTestDrainManager(5 * time.Second)

	// Simulate an active connection
	dm.TrackConnection()

	drainDone := make(chan struct{})
	go func() {
		dm.StartDrain(context.Background())
		close(drainDone)
	}()

	// Give the drain goroutine a moment to start waiting
	time.Sleep(50 * time.Millisecond)

	// Drain should not be done yet because we have an active connection
	select {
	case <-drainDone:
		t.Fatal("drain should not have completed while connections are active")
	default:
		// expected
	}

	// Release the connection
	dm.ReleaseConnection()

	// Now drain should complete quickly
	select {
	case <-drainDone:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("drain did not complete after all connections were released")
	}
}

func TestDrainRespectsTimeout(t *testing.T) {
	dm := newTestDrainManager(100 * time.Millisecond)

	// Simulate a connection that will not be released
	dm.TrackConnection()

	start := time.Now()
	dm.StartDrain(context.Background())
	elapsed := time.Since(start)

	// The drain should have completed due to timeout, not connection release
	if elapsed < 80*time.Millisecond {
		t.Errorf("drain completed too quickly (%v), expected at least ~100ms timeout", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("drain took too long (%v), expected ~100ms timeout", elapsed)
	}

	// Clean up: release the leaked connection to avoid WaitGroup panic
	dm.ReleaseConnection()
}

func TestDrainRespectsContextCancellation(t *testing.T) {
	dm := newTestDrainManager(10 * time.Second)

	// Simulate a connection that will not be released
	dm.TrackConnection()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	dm.StartDrain(ctx)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("drain did not respect context cancellation, took %v", elapsed)
	}

	// Clean up
	dm.ReleaseConnection()
}

func TestConcurrentConnectionTracking(t *testing.T) {
	dm := newTestDrainManager(5 * time.Second)

	const numGoroutines = 100
	var wg sync.WaitGroup

	// Track and release connections concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dm.TrackConnection()
			// Simulate some work
			time.Sleep(time.Millisecond)
			dm.ReleaseConnection()
		}()
	}

	wg.Wait()

	if count := dm.ActiveConnections(); count != 0 {
		t.Errorf("expected 0 active connections after all released, got %d", count)
	}
}

func TestConcurrentDrainWithTraffic(t *testing.T) {
	dm := newTestDrainManager(2 * time.Second)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond) // simulate work
		w.WriteHeader(http.StatusOK)
	})

	handler := dm.DrainMiddleware(innerHandler)

	const numRequests = 20
	var wg sync.WaitGroup

	// Start some requests
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Proto = testProtoHTTP11
			req.ProtoMajor = 1
			req.ProtoMinor = 1
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rr.Code)
			}
		}()
	}

	// Start drain while requests are in-flight
	drainDone := make(chan struct{})
	go func() {
		dm.StartDrain(context.Background())
		close(drainDone)
	}()

	// All requests should still complete
	wg.Wait()

	// Drain should complete after all requests finish
	select {
	case <-drainDone:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("drain did not complete after all concurrent requests finished")
	}

	if count := dm.ActiveConnections(); count != 0 {
		t.Errorf("expected 0 active connections after drain, got %d", count)
	}
}

func TestDrainManagerIsDrainingResetsAfterDrain(t *testing.T) {
	dm := newTestDrainManager(100 * time.Millisecond)

	if dm.IsDraining() {
		t.Error("should not be draining initially")
	}

	// Start and complete a drain with no active connections
	dm.StartDrain(context.Background())

	if dm.IsDraining() {
		t.Error("should not be draining after drain completes")
	}
}

func TestTrackAndReleaseConnection(t *testing.T) {
	dm := newTestDrainManager(5 * time.Second)

	dm.TrackConnection()
	if count := dm.ActiveConnections(); count != 1 {
		t.Errorf("expected 1 active connection, got %d", count)
	}

	dm.TrackConnection()
	if count := dm.ActiveConnections(); count != 2 {
		t.Errorf("expected 2 active connections, got %d", count)
	}

	dm.ReleaseConnection()
	if count := dm.ActiveConnections(); count != 1 {
		t.Errorf("expected 1 active connection after release, got %d", count)
	}

	dm.ReleaseConnection()
	if count := dm.ActiveConnections(); count != 0 {
		t.Errorf("expected 0 active connections after all released, got %d", count)
	}
}
