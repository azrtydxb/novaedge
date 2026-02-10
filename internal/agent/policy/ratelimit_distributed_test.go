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
	"testing"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestDistributedRateLimiter_FallbackToLocal(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.DistributedRateLimitConfig{
		RequestsPerSecond: 10,
		Burst:             20,
		Algorithm:         "sliding-window",
		Key:               "source-ip",
	}

	// Create limiter without Redis (nil client = always fallback to local)
	limiter := NewDistributedRateLimiter(config, nil, logger)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	// Should succeed with local limiter
	allowed, limit, _, _ := limiter.Allow(req)
	if !allowed {
		t.Error("first request should be allowed (local fallback)")
	}
	if limit != 10 {
		t.Errorf("expected limit=10, got %d", limit)
	}
}

func TestDistributedRateLimiter_ExtractKey_SourceIP(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.DistributedRateLimitConfig{
		RequestsPerSecond: 10,
		Key:               "source-ip",
	}
	limiter := NewDistributedRateLimiter(config, nil, logger)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	key := limiter.extractKey(req)
	if key != "192.168.1.1" {
		t.Errorf("expected key=192.168.1.1, got %s", key)
	}
}

func TestDistributedRateLimiter_ExtractKey_Header(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.DistributedRateLimitConfig{
		RequestsPerSecond: 10,
		Key:               "header:X-API-Key",
	}
	limiter := NewDistributedRateLimiter(config, nil, logger)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Header.Set("X-API-Key", "my-api-key-123")

	key := limiter.extractKey(req)
	if key != "my-api-key-123" {
		t.Errorf("expected key=my-api-key-123, got %s", key)
	}
}

func TestDistributedRateLimiter_ExtractKey_HeaderMissing(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.DistributedRateLimitConfig{
		RequestsPerSecond: 10,
		Key:               "header:X-API-Key",
	}
	limiter := NewDistributedRateLimiter(config, nil, logger)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)

	key := limiter.extractKey(req)
	if key != "default" {
		t.Errorf("expected key=default for missing header, got %s", key)
	}
}

func TestHandleDistributedRateLimit_RateLimitHeaders(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.DistributedRateLimitConfig{
		RequestsPerSecond: 100,
		Burst:             200,
		Key:               "source-ip",
	}

	limiter := NewDistributedRateLimiter(config, nil, logger)
	handler := HandleDistributedRateLimit(limiter)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := handler(next)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Check rate limit headers are present
	if w.Header().Get("X-RateLimit-Limit") == "" {
		t.Error("expected X-RateLimit-Limit header")
	}
	if w.Header().Get("X-RateLimit-Remaining") == "" {
		t.Error("expected X-RateLimit-Remaining header")
	}
	if w.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("expected X-RateLimit-Reset header")
	}
}
