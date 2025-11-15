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
	"strings"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewCORS(t *testing.T) {
	config := &pb.CORSConfig{
		AllowOrigins:     []string{"https://example.com"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Content-Type"},
		ExposeHeaders:    []string{"X-Custom-Header"},
		MaxAgeSeconds:    3600,
		AllowCredentials: true,
	}

	cors := NewCORS(config)

	if cors == nil {
		t.Fatal("Expected CORS instance, got nil")
	}

	if cors.config != config {
		t.Error("CORS config does not match")
	}
}

func TestIsOriginAllowed(t *testing.T) {
	tests := []struct {
		name         string
		allowOrigins []string
		origin       string
		expected     bool
		description  string
	}{
		{
			name:         "no origins specified",
			allowOrigins: []string{},
			origin:       "https://example.com",
			expected:     true,
			description:  "Should allow all origins when none specified",
		},
		{
			name:         "wildcard asterisk",
			allowOrigins: []string{"*"},
			origin:       "https://example.com",
			expected:     true,
			description:  "Should allow all origins with wildcard *",
		},
		{
			name:         "exact match",
			allowOrigins: []string{"https://example.com", "https://api.example.com"},
			origin:       "https://example.com",
			expected:     true,
			description:  "Should allow exact origin match",
		},
		{
			name:         "wildcard pattern match",
			allowOrigins: []string{"*.example.com"},
			origin:       "api.example.com",
			expected:     true,
			description:  "Should allow origin matching wildcard pattern",
		},
		{
			name:         "wildcard pattern subdomain match",
			allowOrigins: []string{"*.example.com"},
			origin:       "admin.api.example.com",
			expected:     true,
			description:  "Should allow origin with nested subdomain matching wildcard pattern",
		},
		{
			name:         "non-matching origin",
			allowOrigins: []string{"https://example.com", "https://api.example.com"},
			origin:       "https://evil.com",
			expected:     false,
			description:  "Should reject non-matching origin",
		},
		{
			name:         "wildcard pattern non-match",
			allowOrigins: []string{"*.example.com"},
			origin:       "example.com",
			expected:     false,
			description:  "Should reject origin not matching wildcard pattern",
		},
		{
			name:         "wildcard pattern different domain",
			allowOrigins: []string{"*.example.com"},
			origin:       "api.evil.com",
			expected:     false,
			description:  "Should reject origin with different domain",
		},
		{
			name:         "multiple allowed origins",
			allowOrigins: []string{"https://example.com", "*.api.example.com", "http://localhost:3000"},
			origin:       "v1.api.example.com",
			expected:     true,
			description:  "Should allow origin matching one of multiple patterns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &pb.CORSConfig{
				AllowOrigins: tt.allowOrigins,
			}
			cors := NewCORS(config)

			result := cors.isOriginAllowed(tt.origin)

			if result != tt.expected {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expected, result)
			}
		})
	}
}

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		value    string
		expected bool
	}{
		{
			name:     "prefix wildcard match",
			pattern:  "*.example.com",
			value:    "api.example.com",
			expected: true,
		},
		{
			name:     "prefix wildcard with protocol",
			pattern:  "https://*.example.com",
			value:    "https://api.example.com",
			expected: false, // Current implementation doesn't support wildcards in the middle
		},
		{
			name:     "prefix wildcard non-match",
			pattern:  "*.example.com",
			value:    "example.com",
			expected: false,
		},
		{
			name:     "exact match without wildcard",
			pattern:  "https://example.com",
			value:    "https://example.com",
			expected: true,
		},
		{
			name:     "non-match without wildcard",
			pattern:  "https://example.com",
			value:    "https://api.example.com",
			expected: false,
		},
		{
			name:     "nested subdomain wildcard",
			pattern:  "*.example.com",
			value:    "admin.api.example.com",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchWildcard(tt.pattern, tt.value)

			if result != tt.expected {
				t.Errorf("Expected %v for pattern %s and value %s, got %v",
					tt.expected, tt.pattern, tt.value, result)
			}
		})
	}
}

func TestHandleCORS(t *testing.T) {
	// Create test handler
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	t.Run("no Origin header", func(t *testing.T) {
		config := &pb.CORSConfig{
			AllowOrigins: []string{"https://example.com"},
		}
		cors := NewCORS(config)
		middleware := HandleCORS(cors)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// Should not set CORS headers when no Origin header
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("Should not set CORS headers without Origin")
		}

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("allowed origin exact match", func(t *testing.T) {
		config := &pb.CORSConfig{
			AllowOrigins: []string{"https://example.com"},
		}
		cors := NewCORS(config)
		middleware := HandleCORS(cors)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
			t.Errorf("Expected Access-Control-Allow-Origin: https://example.com, got: %s",
				rec.Header().Get("Access-Control-Allow-Origin"))
		}

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("allowed origin wildcard match", func(t *testing.T) {
		config := &pb.CORSConfig{
			AllowOrigins: []string{"*.example.com"},
		}
		cors := NewCORS(config)
		middleware := HandleCORS(cors)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "api.example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "api.example.com" {
			t.Errorf("Expected Access-Control-Allow-Origin: api.example.com, got: %s",
				rec.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("disallowed origin", func(t *testing.T) {
		config := &pb.CORSConfig{
			AllowOrigins: []string{"https://example.com"},
		}
		cors := NewCORS(config)
		middleware := HandleCORS(cors)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "https://evil.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// Should not set CORS headers for disallowed origin
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("Should not set CORS headers for disallowed origin")
		}

		// Should still process request
		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("preflight request", func(t *testing.T) {
		config := &pb.CORSConfig{
			AllowOrigins:  []string{"https://example.com"},
			AllowMethods:  []string{"GET", "POST", "PUT"},
			AllowHeaders:  []string{"Content-Type", "Authorization"},
			MaxAgeSeconds: 3600,
		}
		cors := NewCORS(config)
		middleware := HandleCORS(cors)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("Expected status %d for preflight, got %d", http.StatusNoContent, rec.Code)
		}

		if !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "GET") {
			t.Errorf("Expected Access-Control-Allow-Methods to contain GET, got: %s",
				rec.Header().Get("Access-Control-Allow-Methods"))
		}

		actualHeaders := rec.Header().Get("Access-Control-Allow-Headers")
		if !strings.Contains(actualHeaders, "Content-Type") {
			t.Errorf("Expected Access-Control-Allow-Headers to contain Content-Type, got: %s", actualHeaders)
		}

		if rec.Header().Get("Access-Control-Max-Age") != "3600" {
			t.Errorf("Expected Access-Control-Max-Age: 3600, got: %s",
				rec.Header().Get("Access-Control-Max-Age"))
		}
	})

	t.Run("allow credentials enabled", func(t *testing.T) {
		config := &pb.CORSConfig{
			AllowOrigins:     []string{"https://example.com"},
			AllowCredentials: true,
		}
		cors := NewCORS(config)
		middleware := HandleCORS(cors)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("Expected Access-Control-Allow-Credentials: true, got: %s",
				rec.Header().Get("Access-Control-Allow-Credentials"))
		}
	})

	t.Run("allow credentials disabled", func(t *testing.T) {
		config := &pb.CORSConfig{
			AllowOrigins:     []string{"https://example.com"},
			AllowCredentials: false,
		}
		cors := NewCORS(config)
		middleware := HandleCORS(cors)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Error("Should not set Access-Control-Allow-Credentials when disabled")
		}
	})

	t.Run("expose headers", func(t *testing.T) {
		config := &pb.CORSConfig{
			AllowOrigins:  []string{"https://example.com"},
			ExposeHeaders: []string{"X-Custom-Header", "X-Another-Header"},
		}
		cors := NewCORS(config)
		middleware := HandleCORS(cors)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		exposeHeaders := rec.Header().Get("Access-Control-Expose-Headers")
		if !strings.Contains(exposeHeaders, "X-Custom-Header") {
			t.Errorf("Expected Access-Control-Expose-Headers to contain X-Custom-Header, got: %s", exposeHeaders)
		}
		if !strings.Contains(exposeHeaders, "X-Another-Header") {
			t.Errorf("Expected Access-Control-Expose-Headers to contain X-Another-Header, got: %s", exposeHeaders)
		}
	})

	t.Run("max age not set when zero", func(t *testing.T) {
		config := &pb.CORSConfig{
			AllowOrigins:  []string{"https://example.com"},
			MaxAgeSeconds: 0,
		}
		cors := NewCORS(config)
		middleware := HandleCORS(cors)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Max-Age") != "" {
			t.Error("Should not set Access-Control-Max-Age when MaxAgeSeconds is 0")
		}
	})

	t.Run("wildcard origin allows all", func(t *testing.T) {
		config := &pb.CORSConfig{
			AllowOrigins: []string{"*"},
		}
		cors := NewCORS(config)
		middleware := HandleCORS(cors)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "https://any-origin.com")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "https://any-origin.com" {
			t.Errorf("Expected Access-Control-Allow-Origin: https://any-origin.com, got: %s",
				rec.Header().Get("Access-Control-Allow-Origin"))
		}
	})
}
