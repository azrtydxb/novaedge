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

package policy

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewRateLimiter(t *testing.T) {
	config := &pb.RateLimitConfig{
		RequestsPerSecond: 10,
		Burst:             20,
		Key:               "source-ip",
	}

	limiter := NewRateLimiter(config)

	if limiter == nil {
		t.Fatal("Expected rate limiter, got nil")
	}

	if limiter.config != config {
		t.Error("Rate limiter config does not match")
	}

	if limiter.limiters == nil {
		t.Error("Expected limiters map to be initialized")
	}
}

func TestExtractKey(t *testing.T) {
	// Setup global trusted proxies for IP extraction
	SetGlobalTrustedProxies([]string{"10.0.0.0/8"})
	defer func() {
		trustedProxyCIDRs = nil
	}()

	tests := []struct {
		name        string
		key         string
		remoteAddr  string
		headerName  string
		headerValue string
		expected    string
	}{
		{
			name:       "source-ip key",
			key:        "source-ip",
			remoteAddr: "1.2.3.4:12345",
			expected:   "1.2.3.4",
		},
		{
			name:       "empty key defaults to source-ip",
			key:        "",
			remoteAddr: "5.6.7.8:12345",
			expected:   "5.6.7.8",
		},
		{
			name:        "custom header key",
			key:         "X-API-Key",
			remoteAddr:  "1.2.3.4:12345",
			headerName:  "X-API-Key",
			headerValue: "test-api-key-123",
			expected:    "test-api-key-123",
		},
		{
			name:       "custom header missing falls back to default",
			key:        "X-API-Key",
			remoteAddr: "1.2.3.4:12345",
			expected:   "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &pb.RateLimitConfig{
				RequestsPerSecond: 10,
				Burst:             20,
				Key:               tt.key,
			}
			limiter := NewRateLimiter(config)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.headerName != "" && tt.headerValue != "" {
				req.Header.Set(tt.headerName, tt.headerValue)
			}

			key, err := limiter.extractKey(req)
			if err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if key != tt.expected {
				t.Errorf("Expected key %s, got %s", tt.expected, key)
			}
		})
	}
}

func TestGetLimiter(t *testing.T) {
	config := &pb.RateLimitConfig{
		RequestsPerSecond: 10,
		Burst:             20,
	}
	limiter := NewRateLimiter(config)

	t.Run("first access creates new limiter", func(t *testing.T) {
		key := "test-key-1"
		rateLimiter := limiter.getLimiter(key)

		if rateLimiter == nil {
			t.Error("Expected rate limiter, got nil")
		}

		if len(limiter.limiters) != 1 {
			t.Errorf("Expected 1 limiter, got %d", len(limiter.limiters))
		}
	})

	t.Run("subsequent access reuses existing limiter", func(t *testing.T) {
		key := "test-key-2"
		rateLimiter1 := limiter.getLimiter(key)
		rateLimiter2 := limiter.getLimiter(key)

		if rateLimiter1 != rateLimiter2 {
			t.Error("Expected same rate limiter instance")
		}
	})

	t.Run("concurrent access creates only one limiter", func(t *testing.T) {
		key := "test-key-concurrent"
		var wg sync.WaitGroup
		numGoroutines := 10

		limiters := make([]*rate.Limiter, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				limiters[idx] = limiter.getLimiter(key)
			}(i)
		}

		wg.Wait()

		// All should be the same instance
		firstLimiter := limiters[0]
		for i := 1; i < numGoroutines; i++ {
			if limiters[i] != firstLimiter {
				t.Errorf("Expected all limiters to be the same instance, but limiter %d differs", i)
			}
		}
	})

	t.Run("default burst equals RPS when burst is 0", func(t *testing.T) {
		config := &pb.RateLimitConfig{
			RequestsPerSecond: 5,
			Burst:             0, // Should default to RPS
		}
		limiter := NewRateLimiter(config)

		key := "test-burst"
		rateLimiter := limiter.getLimiter(key)

		// The burst should be 5 (same as RPS)
		// We can verify this by checking that we can make burst number of requests immediately
		allowed := 0
		for i := 0; i < 10; i++ {
			if rateLimiter.Allow() {
				allowed++
			}
		}

		if allowed != 5 {
			t.Errorf("Expected burst of 5 requests, got %d", allowed)
		}
	})
}

func TestAllow(t *testing.T) {
	// Setup global trusted proxies for IP extraction
	SetGlobalTrustedProxies([]string{"10.0.0.0/8"})
	defer func() {
		trustedProxyCIDRs = nil
	}()

	t.Run("request allowed within rate limit", func(t *testing.T) {
		config := &pb.RateLimitConfig{
			RequestsPerSecond: 100,
			Burst:             100,
			Key:               "source-ip",
		}
		limiter := NewRateLimiter(config)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "1.2.3.4:12345"

		allowed, err := limiter.Allow(req)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if !allowed {
			t.Error("Expected request to be allowed")
		}
	})

	t.Run("request denied when exceeds rate limit", func(t *testing.T) {
		config := &pb.RateLimitConfig{
			RequestsPerSecond: 1,
			Burst:             1,
			Key:               "source-ip",
		}
		limiter := NewRateLimiter(config)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "2.3.4.5:12345"

		// First request should be allowed (uses burst token)
		allowed, _ := limiter.Allow(req)
		if !allowed {
			t.Error("Expected first request to be allowed")
		}

		// Second immediate request should be denied
		allowed, _ = limiter.Allow(req)
		if allowed {
			t.Error("Expected second immediate request to be denied")
		}
	})

	t.Run("multiple keys have separate limiters", func(t *testing.T) {
		config := &pb.RateLimitConfig{
			RequestsPerSecond: 1,
			Burst:             1,
			Key:               "source-ip",
		}
		limiter := NewRateLimiter(config)

		req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req1.RemoteAddr = "3.4.5.6:12345"

		req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req2.RemoteAddr = "4.5.6.7:12345"

		// First request from IP1 uses its burst token
		allowed1, _ := limiter.Allow(req1)
		if !allowed1 {
			t.Error("Expected request from IP1 to be allowed")
		}

		// Second request from IP1 should be denied
		allowed1, _ = limiter.Allow(req1)
		if allowed1 {
			t.Error("Expected second request from IP1 to be denied")
		}

		// But first request from IP2 should be allowed (different limiter)
		allowed2, _ := limiter.Allow(req2)
		if !allowed2 {
			t.Error("Expected request from IP2 to be allowed")
		}
	})

	t.Run("burst behavior allows multiple requests", func(t *testing.T) {
		config := &pb.RateLimitConfig{
			RequestsPerSecond: 1,
			Burst:             3,
			Key:               "source-ip",
		}
		limiter := NewRateLimiter(config)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "5.6.7.8:12345"

		// Should allow burst number of requests immediately
		allowedCount := 0
		for i := 0; i < 5; i++ {
			allowed, _ := limiter.Allow(req)
			if allowed {
				allowedCount++
			}
		}

		if allowedCount != 3 {
			t.Errorf("Expected 3 requests to be allowed (burst), got %d", allowedCount)
		}
	})
}

func TestHandleRateLimit(t *testing.T) {
	// Setup global trusted proxies for IP extraction
	SetGlobalTrustedProxies([]string{"10.0.0.0/8"})
	defer func() {
		trustedProxyCIDRs = nil
	}()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	t.Run("request allowed", func(t *testing.T) {
		config := &pb.RateLimitConfig{
			RequestsPerSecond: 100,
			Burst:             100,
			Key:               "source-ip",
		}
		limiter := NewRateLimiter(config)
		middleware := HandleRateLimit(limiter)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "1.2.3.4:12345"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("request denied with proper headers", func(t *testing.T) {
		config := &pb.RateLimitConfig{
			RequestsPerSecond: 1,
			Burst:             1,
			Key:               "source-ip",
		}
		limiter := NewRateLimiter(config)
		middleware := HandleRateLimit(limiter)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "2.3.4.5:12345"

		// First request should succeed
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req)

		if rec1.Code != http.StatusOK {
			t.Errorf("Expected first request status %d, got %d", http.StatusOK, rec1.Code)
		}

		// Second immediate request should be rate limited
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req)

		if rec2.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status %d, got %d", http.StatusTooManyRequests, rec2.Code)
		}

		// Check rate limit headers
		if rec2.Header().Get("X-RateLimit-Limit") != "1" {
			t.Errorf("Expected X-RateLimit-Limit: 1, got: %s", rec2.Header().Get("X-RateLimit-Limit"))
		}

		if rec2.Header().Get("X-RateLimit-Remaining") != "0" {
			t.Errorf("Expected X-RateLimit-Remaining: 0, got: %s", rec2.Header().Get("X-RateLimit-Remaining"))
		}

		if rec2.Header().Get("Retry-After") != "1" {
			t.Errorf("Expected Retry-After: 1, got: %s", rec2.Header().Get("Retry-After"))
		}
	})

	t.Run("multiple requests over time", func(t *testing.T) {
		config := &pb.RateLimitConfig{
			RequestsPerSecond: 10,
			Burst:             2,
			Key:               "source-ip",
		}
		limiter := NewRateLimiter(config)
		middleware := HandleRateLimit(limiter)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "3.4.5.6:12345"

		// First 2 requests should succeed (burst)
		for i := 0; i < 2; i++ {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Request %d: expected status %d, got %d", i+1, http.StatusOK, rec.Code)
			}
		}

		// 3rd immediate request should be denied
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status %d, got %d", http.StatusTooManyRequests, rec.Code)
		}

		// Wait for token to refill (at 10 RPS, should get 1 token in 100ms)
		time.Sleep(150 * time.Millisecond)

		// Should be allowed again
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d after waiting, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("rate limiting by custom header", func(t *testing.T) {
		config := &pb.RateLimitConfig{
			RequestsPerSecond: 1,
			Burst:             1,
			Key:               "X-API-Key",
		}
		limiter := NewRateLimiter(config)
		middleware := HandleRateLimit(limiter)
		handler := middleware(nextHandler)

		// Two requests with different API keys should each get their own limit
		req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req1.RemoteAddr = "1.2.3.4:12345"
		req1.Header.Set("X-API-Key", "key-1")

		req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req2.RemoteAddr = "1.2.3.4:12345" // Same IP
		req2.Header.Set("X-API-Key", "key-2")

		// First request with key-1 should succeed
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Errorf("Expected status %d for key-1, got %d", http.StatusOK, rec1.Code)
		}

		// Second request with key-1 should be denied
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req1)
		if rec2.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status %d for second key-1 request, got %d", http.StatusTooManyRequests, rec2.Code)
		}

		// But first request with key-2 should succeed (different limiter)
		rec3 := httptest.NewRecorder()
		handler.ServeHTTP(rec3, req2)
		if rec3.Code != http.StatusOK {
			t.Errorf("Expected status %d for key-2, got %d", http.StatusOK, rec3.Code)
		}
	})
}

func TestCleanup(t *testing.T) {
	config := &pb.RateLimitConfig{
		RequestsPerSecond: 10,
		Burst:             20,
	}
	limiter := NewRateLimiter(config)

	// Create some limiters
	limiter.getLimiter("key1")
	limiter.getLimiter("key2")
	limiter.getLimiter("key3")

	if len(limiter.limiters) != 3 {
		t.Errorf("Expected 3 limiters, got %d", len(limiter.limiters))
	}

	// Call cleanup (currently a no-op, but tests the function exists and doesn't error)
	limiter.Cleanup(1 * time.Hour)

	// Limiters should still exist (cleanup is not implemented yet)
	if len(limiter.limiters) != 3 {
		t.Errorf("Expected 3 limiters after cleanup, got %d", len(limiter.limiters))
	}
}
