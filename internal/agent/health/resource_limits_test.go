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
)

func TestDefaultResourceLimitsConfig(t *testing.T) {
	cfg := DefaultResourceLimitsConfig()

	if cfg.MaxConnections != DefaultMaxConnections {
		t.Errorf("expected MaxConnections=%d, got %d", DefaultMaxConnections, cfg.MaxConnections)
	}
	if cfg.MaxPendingRequests != DefaultMaxPendingRequests {
		t.Errorf("expected MaxPendingRequests=%d, got %d", DefaultMaxPendingRequests, cfg.MaxPendingRequests)
	}
	if cfg.MaxRequests != DefaultMaxRequests {
		t.Errorf("expected MaxRequests=%d, got %d", DefaultMaxRequests, cfg.MaxRequests)
	}
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("expected MaxRetries=%d, got %d", DefaultMaxRetries, cfg.MaxRetries)
	}
}

func TestResourceLimits_ConnectionLimit(t *testing.T) {
	rl := NewResourceLimits(ResourceLimitsConfig{
		MaxConnections:     2,
		MaxPendingRequests: 1024,
		MaxRequests:        1024,
		MaxRetries:         3,
	}, "test-cluster")

	// Acquire up to the limit
	if !rl.TryAcquireConnection() {
		t.Error("first connection acquire should succeed")
	}
	if !rl.TryAcquireConnection() {
		t.Error("second connection acquire should succeed")
	}

	// Exceed the limit
	if rl.TryAcquireConnection() {
		t.Error("third connection acquire should fail (limit is 2)")
	}

	// Verify overflow was recorded
	connOverflow, _, _, _ := rl.OverflowCounts()
	if connOverflow != 1 {
		t.Errorf("expected 1 connection overflow, got %d", connOverflow)
	}

	// Release one and try again
	rl.ReleaseConnection()
	if !rl.TryAcquireConnection() {
		t.Error("connection acquire should succeed after release")
	}
}

func TestResourceLimits_PendingRequestLimit(t *testing.T) {
	rl := NewResourceLimits(ResourceLimitsConfig{
		MaxConnections:     1024,
		MaxPendingRequests: 3,
		MaxRequests:        1024,
		MaxRetries:         3,
	}, "test-cluster")

	for i := int64(0); i < 3; i++ {
		if !rl.TryAcquirePendingRequest() {
			t.Errorf("pending request acquire #%d should succeed", i+1)
		}
	}

	if rl.TryAcquirePendingRequest() {
		t.Error("pending request acquire should fail when limit is reached")
	}

	_, pendingOverflow, _, _ := rl.OverflowCounts()
	if pendingOverflow != 1 {
		t.Errorf("expected 1 pending overflow, got %d", pendingOverflow)
	}

	rl.ReleasePendingRequest()
	if !rl.TryAcquirePendingRequest() {
		t.Error("pending request acquire should succeed after release")
	}
}

func TestResourceLimits_RequestLimit(t *testing.T) {
	rl := NewResourceLimits(ResourceLimitsConfig{
		MaxConnections:     1024,
		MaxPendingRequests: 1024,
		MaxRequests:        1,
		MaxRetries:         3,
	}, "test-cluster")

	if !rl.TryAcquireRequest() {
		t.Error("first request acquire should succeed")
	}
	if rl.TryAcquireRequest() {
		t.Error("second request acquire should fail (limit is 1)")
	}

	_, _, reqOverflow, _ := rl.OverflowCounts()
	if reqOverflow != 1 {
		t.Errorf("expected 1 request overflow, got %d", reqOverflow)
	}

	rl.ReleaseRequest()
	if !rl.TryAcquireRequest() {
		t.Error("request acquire should succeed after release")
	}
}

func TestResourceLimits_RetryLimit(t *testing.T) {
	rl := NewResourceLimits(ResourceLimitsConfig{
		MaxConnections:     1024,
		MaxPendingRequests: 1024,
		MaxRequests:        1024,
		MaxRetries:         2,
	}, "test-cluster")

	if !rl.TryAcquireRetry() {
		t.Error("first retry acquire should succeed")
	}
	if !rl.TryAcquireRetry() {
		t.Error("second retry acquire should succeed")
	}
	if rl.TryAcquireRetry() {
		t.Error("third retry acquire should fail (limit is 2)")
	}

	_, _, _, retryOverflow := rl.OverflowCounts()
	if retryOverflow != 1 {
		t.Errorf("expected 1 retry overflow, got %d", retryOverflow)
	}

	rl.ReleaseRetry()
	if !rl.TryAcquireRetry() {
		t.Error("retry acquire should succeed after release")
	}
}

func TestResourceLimits_AcquireReleaseCycle(t *testing.T) {
	rl := NewResourceLimits(ResourceLimitsConfig{
		MaxConnections:     1,
		MaxPendingRequests: 1,
		MaxRequests:        1,
		MaxRetries:         1,
	}, "test-cluster")

	// Run multiple acquire/release cycles to verify counters stay correct
	for i := 0; i < 100; i++ {
		if !rl.TryAcquireConnection() {
			t.Fatalf("connection acquire failed on cycle %d", i)
		}
		rl.ReleaseConnection()

		if !rl.TryAcquirePendingRequest() {
			t.Fatalf("pending request acquire failed on cycle %d", i)
		}
		rl.ReleasePendingRequest()

		if !rl.TryAcquireRequest() {
			t.Fatalf("request acquire failed on cycle %d", i)
		}
		rl.ReleaseRequest()

		if !rl.TryAcquireRetry() {
			t.Fatalf("retry acquire failed on cycle %d", i)
		}
		rl.ReleaseRetry()
	}

	// Verify no overflows occurred
	connOv, pendOv, reqOv, retryOv := rl.OverflowCounts()
	if connOv != 0 || pendOv != 0 || reqOv != 0 || retryOv != 0 {
		t.Errorf("expected zero overflows, got conn=%d pending=%d req=%d retry=%d",
			connOv, pendOv, reqOv, retryOv)
	}

	// Verify active counts are zero
	connAct, pendAct, reqAct, retryAct := rl.ActiveCounts()
	if connAct != 0 || pendAct != 0 || reqAct != 0 || retryAct != 0 {
		t.Errorf("expected zero active counts, got conn=%d pending=%d req=%d retry=%d",
			connAct, pendAct, reqAct, retryAct)
	}
}

func TestResourceLimits_OverflowCountingAccurate(t *testing.T) {
	rl := NewResourceLimits(ResourceLimitsConfig{
		MaxConnections:     1,
		MaxPendingRequests: 1,
		MaxRequests:        1,
		MaxRetries:         1,
	}, "test-cluster")

	// Acquire all slots
	rl.TryAcquireConnection()
	rl.TryAcquirePendingRequest()
	rl.TryAcquireRequest()
	rl.TryAcquireRetry()

	// Attempt overflow multiple times
	for i := 0; i < 5; i++ {
		rl.TryAcquireConnection()
		rl.TryAcquirePendingRequest()
		rl.TryAcquireRequest()
		rl.TryAcquireRetry()
	}

	connOv, pendOv, reqOv, retryOv := rl.OverflowCounts()
	if connOv != 5 {
		t.Errorf("expected 5 connection overflows, got %d", connOv)
	}
	if pendOv != 5 {
		t.Errorf("expected 5 pending overflows, got %d", pendOv)
	}
	if reqOv != 5 {
		t.Errorf("expected 5 request overflows, got %d", reqOv)
	}
	if retryOv != 5 {
		t.Errorf("expected 5 retry overflows, got %d", retryOv)
	}

	// Verify active counts remain at 1 (not inflated by failed acquires)
	connAct, pendAct, reqAct, retryAct := rl.ActiveCounts()
	if connAct != 1 {
		t.Errorf("expected 1 active connection, got %d", connAct)
	}
	if pendAct != 1 {
		t.Errorf("expected 1 active pending request, got %d", pendAct)
	}
	if reqAct != 1 {
		t.Errorf("expected 1 active request, got %d", reqAct)
	}
	if retryAct != 1 {
		t.Errorf("expected 1 active retry, got %d", retryAct)
	}
}

func TestResourceLimits_ConcurrentAccess(t *testing.T) {
	const maxConns int64 = 10
	const goroutines = 50
	const iterations = 100

	rl := NewResourceLimits(ResourceLimitsConfig{
		MaxConnections:     maxConns,
		MaxPendingRequests: maxConns,
		MaxRequests:        maxConns,
		MaxRetries:         maxConns,
	}, "concurrent-cluster")

	var wg sync.WaitGroup

	// Launch goroutines that all try to acquire and release connections
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if rl.TryAcquireConnection() {
					// Verify we never exceed the limit
					active := rl.activeConnections.Load()
					if active > maxConns {
						t.Errorf("active connections %d exceeded max %d", active, maxConns)
					}
					rl.ReleaseConnection()
				}
			}
		}()
	}

	// Also test concurrent pending requests
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if rl.TryAcquirePendingRequest() {
					rl.ReleasePendingRequest()
				}
			}
		}()
	}

	// Also test concurrent requests
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if rl.TryAcquireRequest() {
					rl.ReleaseRequest()
				}
			}
		}()
	}

	// Also test concurrent retries
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if rl.TryAcquireRetry() {
					rl.ReleaseRetry()
				}
			}
		}()
	}

	wg.Wait()

	// After all goroutines complete, active counts should be zero
	connAct, pendAct, reqAct, retryAct := rl.ActiveCounts()
	if connAct != 0 {
		t.Errorf("expected 0 active connections after concurrent test, got %d", connAct)
	}
	if pendAct != 0 {
		t.Errorf("expected 0 active pending requests after concurrent test, got %d", pendAct)
	}
	if reqAct != 0 {
		t.Errorf("expected 0 active requests after concurrent test, got %d", reqAct)
	}
	if retryAct != 0 {
		t.Errorf("expected 0 active retries after concurrent test, got %d", retryAct)
	}

	// There should be some overflows since goroutines > maxConns
	connOv, pendOv, reqOv, retryOv := rl.OverflowCounts()
	t.Logf("overflow counts: conn=%d pending=%d req=%d retry=%d", connOv, pendOv, reqOv, retryOv)
	if connOv == 0 {
		t.Log("warning: no connection overflows in concurrent test (possible but unlikely)")
	}
	if pendOv == 0 {
		t.Log("warning: no pending request overflows in concurrent test (possible but unlikely)")
	}
	if reqOv == 0 {
		t.Log("warning: no request overflows in concurrent test (possible but unlikely)")
	}
	if retryOv == 0 {
		t.Log("warning: no retry overflows in concurrent test (possible but unlikely)")
	}
}

func TestResourceLimits_ActiveCounts(t *testing.T) {
	rl := NewResourceLimits(ResourceLimitsConfig{
		MaxConnections:     10,
		MaxPendingRequests: 10,
		MaxRequests:        10,
		MaxRetries:         10,
	}, "test-cluster")

	// Initially all zero
	connAct, pendAct, reqAct, retryAct := rl.ActiveCounts()
	if connAct != 0 || pendAct != 0 || reqAct != 0 || retryAct != 0 {
		t.Error("expected all zero active counts initially")
	}

	// Acquire some resources
	rl.TryAcquireConnection()
	rl.TryAcquireConnection()
	rl.TryAcquirePendingRequest()
	rl.TryAcquireRequest()
	rl.TryAcquireRequest()
	rl.TryAcquireRequest()
	rl.TryAcquireRetry()

	connAct, pendAct, reqAct, retryAct = rl.ActiveCounts()
	if connAct != 2 {
		t.Errorf("expected 2 active connections, got %d", connAct)
	}
	if pendAct != 1 {
		t.Errorf("expected 1 active pending request, got %d", pendAct)
	}
	if reqAct != 3 {
		t.Errorf("expected 3 active requests, got %d", reqAct)
	}
	if retryAct != 1 {
		t.Errorf("expected 1 active retry, got %d", retryAct)
	}
}
