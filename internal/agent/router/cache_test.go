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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestBuildCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		host     string
		path     string
		query    string
		expected string
	}{
		{
			name:     "simple GET",
			method:   "GET",
			host:     "example.com",
			path:     "/api/v1/users",
			expected: "GET|example.com|/api/v1/users",
		},
		{
			name:     "GET with query",
			method:   "GET",
			host:     "example.com",
			path:     "/api/v1/users",
			query:    "page=1",
			expected: "GET|example.com|/api/v1/users?page=1",
		},
		{
			name:     "HEAD request",
			method:   "HEAD",
			host:     "example.com",
			path:     "/",
			expected: "HEAD|example.com|/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "http://" + tt.host + tt.path
			if tt.query != "" {
				url += "?" + tt.query
			}
			req := httptest.NewRequest(tt.method, url, nil)
			req.Host = tt.host

			key := buildCacheKey(req)
			if key != tt.expected {
				t.Errorf("buildCacheKey() = %q, want %q", key, tt.expected)
			}
		})
	}
}

func TestParseCacheControl(t *testing.T) {
	tests := []struct {
		header  string
		noCache bool
		noStore bool
		private bool
		public  bool
		maxAge  int64
		sMaxAge int64
	}{
		{"", false, false, false, false, 0, 0},
		{"no-cache", true, false, false, false, 0, 0},
		{"no-store", false, true, false, false, 0, 0},
		{"private", false, false, true, false, 0, 0},
		{"public", false, false, false, true, 0, 0},
		{"max-age=300", false, false, false, false, 300, 0},
		{"s-maxage=600", false, false, false, false, 0, 600},
		{"public, max-age=300, s-maxage=600", false, false, false, true, 300, 600},
		{"no-cache, no-store", true, true, false, false, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			cc := parseCacheControl(tt.header)
			if cc.noCache != tt.noCache {
				t.Errorf("noCache = %v, want %v", cc.noCache, tt.noCache)
			}
			if cc.noStore != tt.noStore {
				t.Errorf("noStore = %v, want %v", cc.noStore, tt.noStore)
			}
			if cc.private != tt.private {
				t.Errorf("private = %v, want %v", cc.private, tt.private)
			}
			if cc.public != tt.public {
				t.Errorf("public = %v, want %v", cc.public, tt.public)
			}
			if cc.maxAge != tt.maxAge {
				t.Errorf("maxAge = %d, want %d", cc.maxAge, tt.maxAge)
			}
			if cc.sMaxAge != tt.sMaxAge {
				t.Errorf("sMaxAge = %d, want %d", cc.sMaxAge, tt.sMaxAge)
			}
		})
	}
}

func TestResponseCacheHitMiss(t *testing.T) {
	logger := zap.NewNop()
	config := CacheConfig{
		Enabled:      true,
		MaxSize:      1024 * 1024,
		DefaultTTL:   5 * time.Minute,
		MaxTTL:       1 * time.Hour,
		MaxEntrySize: 512 * 1024,
	}
	cache := NewResponseCache(config, logger)
	defer cache.Stop()

	backend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"test"}`))
	})

	handler := cache.Middleware(backend)

	// First request: MISS
	req1 := httptest.NewRequest(http.MethodGet, "http://example.com/api/data", nil)
	req1.Host = "example.com"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Errorf("first request status = %d, want %d", rec1.Code, http.StatusOK)
	}

	// Second request: HIT
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/api/data", nil)
	req2.Host = "example.com"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("second request status = %d, want %d", rec2.Code, http.StatusOK)
	}
	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("second request X-Cache = %q, want %q", rec2.Header().Get("X-Cache"), "HIT")
	}
}

func TestResponseCacheSkipNonGET(t *testing.T) {
	logger := zap.NewNop()
	config := CacheConfig{
		Enabled:      true,
		MaxSize:      1024 * 1024,
		DefaultTTL:   5 * time.Minute,
		MaxTTL:       1 * time.Hour,
		MaxEntrySize: 512 * 1024,
	}
	cache := NewResponseCache(config, logger)
	defer cache.Stop()

	callCount := 0
	backend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := cache.Middleware(backend)

	// POST should not be cached
	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/data", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if callCount != 1 {
		t.Errorf("backend called %d times, want 1", callCount)
	}

	// Second POST should also hit backend
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)

	if callCount != 2 {
		t.Errorf("backend called %d times after second POST, want 2", callCount)
	}
}

func TestResponseCacheRespectNoStore(t *testing.T) {
	logger := zap.NewNop()
	config := CacheConfig{
		Enabled:      true,
		MaxSize:      1024 * 1024,
		DefaultTTL:   5 * time.Minute,
		MaxTTL:       1 * time.Hour,
		MaxEntrySize: 512 * 1024,
	}
	cache := NewResponseCache(config, logger)
	defer cache.Stop()

	callCount := 0
	backend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("sensitive"))
	})

	handler := cache.Middleware(backend)

	// First request
	req := httptest.NewRequest(http.MethodGet, "http://example.com/secret", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Second request should also hit backend (no-store)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)

	if callCount != 2 {
		t.Errorf("backend called %d times, want 2 (no-store should prevent caching)", callCount)
	}
}

func TestResponseCacheSkipSetCookie(t *testing.T) {
	logger := zap.NewNop()
	config := CacheConfig{
		Enabled:      true,
		MaxSize:      1024 * 1024,
		DefaultTTL:   5 * time.Minute,
		MaxTTL:       1 * time.Hour,
		MaxEntrySize: 512 * 1024,
	}
	cache := NewResponseCache(config, logger)
	defer cache.Stop()

	callCount := 0
	backend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Set-Cookie", "session=abc123")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("with cookie"))
	})

	handler := cache.Middleware(backend)

	// First request
	req := httptest.NewRequest(http.MethodGet, "http://example.com/login", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Second request should hit backend (Set-Cookie prevents caching)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)

	if callCount != 2 {
		t.Errorf("backend called %d times, want 2 (Set-Cookie should prevent caching)", callCount)
	}
}

func TestResponseCacheConditionalETag(t *testing.T) {
	logger := zap.NewNop()
	config := CacheConfig{
		Enabled:      true,
		MaxSize:      1024 * 1024,
		DefaultTTL:   5 * time.Minute,
		MaxTTL:       1 * time.Hour,
		MaxEntrySize: 512 * 1024,
	}
	cache := NewResponseCache(config, logger)
	defer cache.Stop()

	backend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data"))
	})

	handler := cache.Middleware(backend)

	// First request to populate cache
	req := httptest.NewRequest(http.MethodGet, "http://example.com/data", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Second request with If-None-Match matching the ETag
	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/data", nil)
	req2.Host = "example.com"
	req2.Header.Set("If-None-Match", `"abc123"`)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Errorf("conditional request status = %d, want %d", rec2.Code, http.StatusNotModified)
	}
}

func TestResponseCacheTTLFromMaxAge(t *testing.T) {
	logger := zap.NewNop()
	config := CacheConfig{
		Enabled:      true,
		MaxSize:      1024 * 1024,
		DefaultTTL:   5 * time.Minute,
		MaxTTL:       1 * time.Hour,
		MaxEntrySize: 512 * 1024,
	}
	cache := NewResponseCache(config, logger)
	defer cache.Stop()

	header := http.Header{}
	header.Set("Cache-Control", "max-age=120")
	ttl := cache.determineTTL(header)

	expected := 120 * time.Second
	if ttl != expected {
		t.Errorf("determineTTL() = %v, want %v", ttl, expected)
	}
}

func TestResponseCacheTTLFromSMaxAge(t *testing.T) {
	logger := zap.NewNop()
	config := CacheConfig{
		Enabled:      true,
		MaxSize:      1024 * 1024,
		DefaultTTL:   5 * time.Minute,
		MaxTTL:       1 * time.Hour,
		MaxEntrySize: 512 * 1024,
	}
	cache := NewResponseCache(config, logger)
	defer cache.Stop()

	header := http.Header{}
	header.Set("Cache-Control", "public, max-age=120, s-maxage=300")
	ttl := cache.determineTTL(header)

	// s-maxage takes precedence
	expected := 300 * time.Second
	if ttl != expected {
		t.Errorf("determineTTL() = %v, want %v", ttl, expected)
	}
}

func TestResponseCachePurge(t *testing.T) {
	logger := zap.NewNop()
	config := CacheConfig{
		Enabled:      true,
		MaxSize:      1024 * 1024,
		DefaultTTL:   5 * time.Minute,
		MaxTTL:       1 * time.Hour,
		MaxEntrySize: 512 * 1024,
	}
	cache := NewResponseCache(config, logger)
	defer cache.Stop()

	backend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cached"))
	})

	handler := cache.Middleware(backend)

	// Populate cache
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/data", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify cache hit
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Error("expected cache hit before purge")
	}

	// Purge
	count := cache.Purge("*")
	if count < 1 {
		t.Errorf("Purge('*') = %d, want >= 1", count)
	}

	// Verify cache miss after purge
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req)
	if rec3.Header().Get("X-Cache") == "HIT" {
		t.Error("expected cache miss after purge, got HIT")
	}
}

func TestResponseCacheDisabled(t *testing.T) {
	logger := zap.NewNop()
	config := CacheConfig{
		Enabled: false,
	}
	cache := NewResponseCache(config, logger)
	defer cache.Stop()

	callCount := 0
	backend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := cache.Middleware(backend)

	// Both requests should hit backend
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/data", nil)
	req.Host = "example.com"
	handler.ServeHTTP(httptest.NewRecorder(), req)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if callCount != 2 {
		t.Errorf("backend called %d times, want 2 (cache disabled)", callCount)
	}
}
