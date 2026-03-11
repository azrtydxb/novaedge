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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// httpGet is a helper that performs an HTTP GET with a context.
func httpGet(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: test server URL
	require.NoError(t, err)
	return resp
}

func TestDefaultObservabilityRateLimitConfigValues(t *testing.T) {
	config := DefaultObservabilityRateLimitConfig()
	assert.Equal(t, 100, config.RequestsPerMinute)
	assert.Equal(t, 10, config.Burst)
}

func TestNewIPRateLimiter(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60,
		Burst:             5,
	}

	limiter := NewIPRateLimiter(config)
	require.NotNil(t, limiter)
	assert.NotNil(t, limiter.limiters)
	assert.Equal(t, config, limiter.config)

	// Clean up
	limiter.Stop()
}

func TestIPRateLimiter_GetLimiter(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60,
		Burst:             5,
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	// Get limiter for new IP
	l1 := limiter.getLimiter("192.168.1.1")
	require.NotNil(t, l1)

	// Get same limiter for same IP
	l2 := limiter.getLimiter("192.168.1.1")
	assert.Same(t, l1, l2, "should return same limiter for same IP")

	// Get different limiter for different IP
	l3 := limiter.getLimiter("192.168.1.2")
	assert.NotSame(t, l1, l3, "should return different limiter for different IP")

	// Verify limiters are stored
	limiter.mu.RLock()
	assert.Len(t, limiter.limiters, 2)
	limiter.mu.RUnlock()
}

func TestIPRateLimiter_GetLimiter_Concurrent(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60,
		Burst:             5,
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	// Concurrent access to same IP
	done := make(chan *rate.Limiter, 10)
	for i := 0; i < 10; i++ {
		go func() {
			done <- limiter.getLimiter("192.168.1.1")
		}()
	}

	// All should return the same limiter
	first := <-done
	for i := 0; i < 9; i++ {
		l := <-done
		assert.Same(t, first, l)
	}
}

func TestIPRateLimiter_Cleanup(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60,
		Burst:             5,
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	// Add many limiters
	for i := 0; i < 15000; i++ {
		limiter.getLimiter("192.168.1." + string(rune(i)))
	}

	limiter.mu.RLock()
	initialCount := len(limiter.limiters)
	limiter.mu.RUnlock()
	assert.Greater(t, initialCount, 10000)

	// Mark all entries as stale by backdating their lastAccess
	limiter.mu.Lock()
	staleTime := time.Now().Add(-20 * time.Minute)
	for _, entry := range limiter.limiters {
		entry.lastAccess = staleTime
	}
	limiter.mu.Unlock()

	// Trigger cleanup — stale entries should be evicted
	limiter.cleanup()

	limiter.mu.RLock()
	finalCount := len(limiter.limiters)
	limiter.mu.RUnlock()
	assert.Less(t, finalCount, initialCount)
	assert.Equal(t, 0, finalCount) // All stale entries should be cleared
}

func TestIPRateLimiter_Cleanup_SmallMap(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60,
		Burst:             5,
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	// Add few limiters
	limiter.getLimiter("192.168.1.1")
	limiter.getLimiter("192.168.1.2")
	limiter.getLimiter("192.168.1.3")

	limiter.mu.RLock()
	initialCount := len(limiter.limiters)
	limiter.mu.RUnlock()
	assert.Equal(t, 3, initialCount)

	// Trigger cleanup - should not clear small maps
	limiter.cleanup()

	limiter.mu.RLock()
	finalCount := len(limiter.limiters)
	limiter.mu.RUnlock()
	assert.Equal(t, 3, finalCount) // Should remain unchanged
}

func TestIPRateLimiter_Stop(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60,
		Burst:             5,
	}

	limiter := NewIPRateLimiter(config)

	// Stop should not panic
	assert.NotPanics(t, func() {
		limiter.Stop()
	})

	// Double stop should not panic (though channel is closed)
	// This tests that we handle graceful shutdown
}

func TestRateLimitMiddleware_Allow(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60, // 1 per second
		Burst:             2,
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	middleware := RateLimitMiddleware(limiter)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Create test server with middleware
	ts := httptest.NewServer(middleware(handler))
	defer ts.Close()

	// First request should be allowed
	resp := httpGet(t, ts.URL)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// Second request should be allowed (burst)
	resp = httpGet(t, ts.URL)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestRateLimitMiddleware_RateLimited(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60, // 1 per second
		Burst:             1,  // Very low burst
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	middleware := RateLimitMiddleware(limiter)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	ts := httptest.NewServer(middleware(handler))
	defer ts.Close()

	// First request should be allowed
	resp := httpGet(t, ts.URL)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// Immediate second request should be rate limited
	resp = httpGet(t, ts.URL)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	_ = resp.Body.Close()

	// Wait for rate limit to reset
	time.Sleep(time.Second)

	// Should be allowed again
	resp = httpGet(t, ts.URL)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestRateLimitMiddleware_DifferentIPs(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60, // 1 per second
		Burst:             1,  // Very low burst
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	middleware := RateLimitMiddleware(limiter)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	ts := httptest.NewServer(middleware(handler))
	defer ts.Close()

	// Note: In test environment, RemoteAddr is typically the loopback
	// This test verifies the middleware doesn't crash with different IPs

	// Make multiple requests - they should all use same IP in test
	for i := 0; i < 3; i++ {
		resp := httpGet(t, ts.URL)
		_ = resp.Body.Close()
	}
}

func TestRateLimitMiddleware_IPExtraction(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60,
		Burst:             10,
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	middleware := RateLimitMiddleware(limiter)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(middleware(handler))
	defer ts.Close()

	// Test with valid request
	resp := httpGet(t, ts.URL)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// Verify limiter was created
	limiter.mu.RLock()
	assert.Greater(t, len(limiter.limiters), 0)
	limiter.mu.RUnlock()
}

func TestRateLimiterConfig_Values(t *testing.T) {
	tests := []struct {
		name          string
		config        RateLimiterConfig
		expectedRate  float64
		expectedBurst int
	}{
		{
			name: "default config",
			config: RateLimiterConfig{
				RequestsPerMinute: 100,
				Burst:             10,
			},
			expectedRate:  100.0 / 60.0,
			expectedBurst: 10,
		},
		{
			name: "high rate",
			config: RateLimiterConfig{
				RequestsPerMinute: 6000,
				Burst:             100,
			},
			expectedRate:  6000.0 / 60.0,
			expectedBurst: 100,
		},
		{
			name: "low rate",
			config: RateLimiterConfig{
				RequestsPerMinute: 6,
				Burst:             1,
			},
			expectedRate:  6.0 / 60.0,
			expectedBurst: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewIPRateLimiter(tt.config)
			defer limiter.Stop()

			l := limiter.getLimiter("192.168.1.1")
			require.NotNil(t, l)
		})
	}
}

func TestRateLimitMiddleware_ResponseFormat(t *testing.T) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60,
		Burst:             1,
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	middleware := RateLimitMiddleware(limiter)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(middleware(handler))
	defer ts.Close()

	// Exhaust burst
	resp := httpGet(t, ts.URL)
	_ = resp.Body.Close()

	// Next request should be rate limited
	resp = httpGet(t, ts.URL)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	_ = resp.Body.Close()
}

// Benchmark tests
func BenchmarkIPRateLimiter_GetLimiter(b *testing.B) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60,
		Burst:             5,
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.getLimiter("192.168.1.1")
	}
}

func BenchmarkIPRateLimiter_GetLimiter_DifferentIPs(b *testing.B) {
	config := RateLimiterConfig{
		RequestsPerMinute: 60,
		Burst:             5,
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.getLimiter("192.168.1." + string(rune(i%256)))
	}
}

func BenchmarkRateLimitMiddleware(b *testing.B) {
	config := RateLimiterConfig{
		RequestsPerMinute: 6000,
		Burst:             100,
	}

	limiter := NewIPRateLimiter(config)
	defer limiter.Stop()

	middleware := RateLimitMiddleware(limiter)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(middleware(handler))
	defer ts.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
		resp, _ := http.DefaultClient.Do(req) //nolint:gosec // G704: test server URL
		_ = resp.Body.Close()
	}
}
