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

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestRedirectScheme_Disabled(t *testing.T) {
	logger := zap.NewNop()
	middleware := NewRedirectSchemeMiddleware(nil, logger)

	if middleware.IsEnabled() {
		t.Error("Expected middleware to be disabled when config is nil")
	}
}

func TestRedirectScheme_HTTPToHTTPS(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.RedirectSchemeConfig{
		Enabled:    true,
		Scheme:     "https",
		Port:       443,
		StatusCode: 301,
	}

	middleware := NewRedirectSchemeMiddleware(config, logger)
	if !middleware.IsEnabled() {
		t.Fatal("Expected middleware to be enabled")
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/users?page=1", nil)
	req.Host = testCacheHost
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("Expected 301 redirect, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	expected := "https://example.com/api/v1/users?page=1"
	if location != expected {
		t.Errorf("Expected redirect to %q, got %q", expected, location)
	}
}

func TestRedirectScheme_PreservePath(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.RedirectSchemeConfig{
		Enabled:    true,
		Scheme:     "https",
		Port:       443,
		StatusCode: 302,
	}

	middleware := NewRedirectSchemeMiddleware(config, logger)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/path/to/resource?key=value&other=123", nil)
	req.Host = testCacheHost
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("Expected 302 redirect, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	expected := "https://example.com/path/to/resource?key=value&other=123"
	if location != expected {
		t.Errorf("Expected redirect to %q, got %q", expected, location)
	}
}

func TestRedirectScheme_SkipExclusions(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.RedirectSchemeConfig{
		Enabled:    true,
		Scheme:     "https",
		Port:       443,
		StatusCode: 301,
		Exclusions: []string{"/healthz", "/readyz", "/.well-known/"},
	}

	middleware := NewRedirectSchemeMiddleware(config, logger)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	wrapped := middleware.Wrap(handler)

	tests := []struct {
		path           string
		expectRedirect bool
	}{
		{"/healthz", false},
		{"/readyz", false},
		{"/.well-known/acme-challenge/abc", false},
		{"/api/v1/users", true},
		{"/", true},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "http://example.com"+tt.path, nil)
		req.Host = testCacheHost
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		if tt.expectRedirect {
			if rec.Code != http.StatusMovedPermanently {
				t.Errorf("Path %q: expected redirect, got status %d", tt.path, rec.Code)
			}
		} else {
			if rec.Code != http.StatusOK {
				t.Errorf("Path %q: expected 200 (no redirect), got status %d", tt.path, rec.Code)
			}
		}
	}
}

func TestRedirectScheme_301vs302(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		statusCode int32
		expected   int
	}{
		{301, http.StatusMovedPermanently},
		{302, http.StatusFound},
		{0, http.StatusMovedPermanently},   // Default should be 301
		{307, http.StatusMovedPermanently}, // Invalid should default to 301
	}

	for _, tt := range tests {
		config := &pb.RedirectSchemeConfig{
			Enabled:    true,
			Scheme:     "https",
			Port:       443,
			StatusCode: tt.statusCode,
		}

		middleware := NewRedirectSchemeMiddleware(config, logger)
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrapped := middleware.Wrap(handler)
		req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
		req.Host = testCacheHost
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		if rec.Code != tt.expected {
			t.Errorf("StatusCode %d: expected %d, got %d", tt.statusCode, tt.expected, rec.Code)
		}
	}
}

func TestRedirectScheme_AlreadyHTTPS(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.RedirectSchemeConfig{
		Enabled:    true,
		Scheme:     "https",
		Port:       443,
		StatusCode: 301,
	}

	middleware := NewRedirectSchemeMiddleware(config, logger)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	wrapped := middleware.Wrap(handler)

	// Request with X-Forwarded-Proto: https should not be redirected
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Host = testCacheHost
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for already-HTTPS request, got %d", rec.Code)
	}
}

func TestRedirectScheme_NonDefaultPort(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.RedirectSchemeConfig{
		Enabled:    true,
		Scheme:     "https",
		Port:       8443,
		StatusCode: 301,
	}

	middleware := NewRedirectSchemeMiddleware(config, logger)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Host = testCacheHost
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	location := rec.Header().Get("Location")
	expected := "https://example.com:8443/api"
	if location != expected {
		t.Errorf("Expected redirect to %q, got %q", expected, location)
	}
}
