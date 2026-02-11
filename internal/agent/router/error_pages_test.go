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
	"strings"
	"testing"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestErrorPageInterceptor_Disabled(t *testing.T) {
	logger := zap.NewNop()
	interceptor := NewErrorPageInterceptor(nil, logger)

	if interceptor.IsEnabled() {
		t.Error("Expected interceptor to be disabled when config is nil")
	}

	// Wrap should return the handler unchanged
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("original"))
	})

	wrapped := interceptor.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}
	if rec.Body.String() != "original" {
		t.Errorf("Expected original body, got %q", rec.Body.String())
	}
}

func TestErrorPageInterceptor_Custom404(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.ErrorPageConfig{
		Enabled: true,
		Pages: map[int32]string{
			404: `<html><body>Custom 404: {{.StatusCode}} {{.StatusText}}</body></html>`,
		},
	}

	interceptor := NewErrorPageInterceptor(config, logger)
	if !interceptor.IsEnabled() {
		t.Fatal("Expected interceptor to be enabled")
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("backend error"))
	})

	wrapped := interceptor.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Custom 404: 404 Not Found") {
		t.Errorf("Expected custom error page with template variables, got %q", body)
	}

	// Should not contain the original backend body
	if strings.Contains(body, "backend error") {
		t.Error("Custom error page should replace backend response body")
	}
}

func TestErrorPageInterceptor_Custom503(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.ErrorPageConfig{
		Enabled: true,
		Pages: map[int32]string{
			503: `<h1>Service Unavailable</h1><p>Code: {{.StatusCode}}</p>`,
		},
	}

	interceptor := NewErrorPageInterceptor(config, logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	wrapped := interceptor.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Service Unavailable") {
		t.Errorf("Expected custom 503 page, got %q", body)
	}
	if !strings.Contains(body, "Code: 503") {
		t.Errorf("Expected status code in template, got %q", body)
	}
}

func TestErrorPageInterceptor_TemplateVariables(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.ErrorPageConfig{
		Enabled: true,
		Pages: map[int32]string{
			500: `Code={{.StatusCode}} Text={{.StatusText}} ID={{.RequestID}}`,
		},
	}

	interceptor := NewErrorPageInterceptor(config, logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	wrapped := interceptor.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("X-Request-ID", "test-req-123")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Code=500") {
		t.Errorf("Expected StatusCode in template, got %q", body)
	}
	if !strings.Contains(body, "Text=Internal Server Error") {
		t.Errorf("Expected StatusText in template, got %q", body)
	}
	if !strings.Contains(body, "ID=test-req-123") {
		t.Errorf("Expected RequestID in template, got %q", body)
	}
}

func TestErrorPageInterceptor_DefaultPage(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.ErrorPageConfig{
		Enabled:     true,
		Pages:       map[int32]string{},
		DefaultPage: `<div>Default Error: {{.StatusCode}}</div>`,
	}

	interceptor := NewErrorPageInterceptor(config, logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	wrapped := interceptor.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Default Error: 502") {
		t.Errorf("Expected default error page for 502, got %q", body)
	}
}

func TestErrorPageInterceptor_SuccessNotIntercepted(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.ErrorPageConfig{
		Enabled: true,
		Pages: map[int32]string{
			404: `<h1>Not Found</h1>`,
		},
	}

	interceptor := NewErrorPageInterceptor(config, logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	wrapped := interceptor.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "success" {
		t.Errorf("Expected original body for 200, got %q", rec.Body.String())
	}
}

func TestErrorPageInterceptor_BuiltInDefault(t *testing.T) {
	logger := zap.NewNop()
	config := &pb.ErrorPageConfig{
		Enabled: true,
		Pages:   map[int32]string{},
		// No DefaultPage set - should use built-in
	}

	interceptor := NewErrorPageInterceptor(config, logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	wrapped := interceptor.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "404") {
		t.Errorf("Expected built-in error page with status code, got %q", body)
	}
	if !strings.Contains(body, "Not Found") {
		t.Errorf("Expected built-in error page with status text, got %q", body)
	}
}
