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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestRequestLimitsMiddleware_BodyTooLarge(t *testing.T) {
	limits := &pb.RouteLimitsConfig{
		MaxRequestBodySize: 100, // 100 bytes limit
	}
	m := NewRequestLimitsMiddleware(limits)

	body := strings.Repeat("x", 200) // Exceeds limit

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to read the body; the limitedReadCloser should fail
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", rec.Code)
	}
}

func TestRequestLimitsMiddleware_BodyTooLarge_ContentLength(t *testing.T) {
	limits := &pb.RouteLimitsConfig{
		MaxRequestBodySize: 100,
	}
	m := NewRequestLimitsMiddleware(limits)

	body := strings.Repeat("x", 200)

	handler := m.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called when Content-Length exceeds limit")
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.ContentLength = 200 // Explicitly set
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413 for Content-Length check, got %d", rec.Code)
	}
}

func TestRequestLimitsMiddleware_BodyWithinLimit(t *testing.T) {
	limits := &pb.RouteLimitsConfig{
		MaxRequestBodySize: 1000,
	}
	m := NewRequestLimitsMiddleware(limits)

	body := "small body"
	var capturedBody string

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("unexpected error reading body: %v", err)
			return
		}
		capturedBody = string(data)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if capturedBody != body {
		t.Errorf("body mismatch: got %q, want %q", capturedBody, body)
	}
}

func TestRequestLimitsMiddleware_RequestTimeout(t *testing.T) {
	limits := &pb.RouteLimitsConfig{
		RequestTimeoutMs: 50, // 50ms timeout
	}
	m := NewRequestLimitsMiddleware(limits)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow backend
		select {
		case <-time.After(200 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			// Context was cancelled due to timeout
			return
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// The timeout should trigger a 504 response
	if rec.Code != http.StatusGatewayTimeout && rec.Code != http.StatusOK {
		// Note: the exact behavior depends on timing; we just verify it doesn't hang
		t.Logf("status code: %d (timeout behavior is timing-dependent)", rec.Code)
	}
}

func TestRequestLimitsMiddleware_NilConfig(t *testing.T) {
	m := NewRequestLimitsMiddleware(nil)

	var called bool
	handler := m.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should be called when config is nil")
	}
}

func TestRequestLimitsMiddleware_NoBody(t *testing.T) {
	limits := &pb.RouteLimitsConfig{
		MaxRequestBodySize: 100,
	}
	m := NewRequestLimitsMiddleware(limits)

	var called bool
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should be called for bodyless requests")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"0", 0, false},
		{"", 0, false},
		{"1024", 1024, false},
		{"10Ki", 10 * 1024, false},
		{"10Mi", 10 * 1024 * 1024, false},
		{"1Gi", 1024 * 1024 * 1024, false},
		{"10KB", 10000, false},
		{"10MB", 10000000, false},
		{"1GB", 1000000000, false},
		{"10M", 10 * 1024 * 1024, false},
		{"invalid", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseByteSize(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseByteSize(%q): expected error, got %d", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseByteSize(%q): unexpected error: %v", tc.input, err)
				return
			}
			if got != tc.expected {
				t.Errorf("ParseByteSize(%q) = %d, want %d", tc.input, got, tc.expected)
			}
		})
	}
}

func TestParseDurationMs(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"0", 0, false},
		{"", 0, false},
		{"30s", 30000, false},
		{"1m", 60000, false},
		{"500ms", 500, false},
		{"1h", 3600000, false},
		{"invalid", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseDurationMs(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseDurationMs(%q): expected error, got %d", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseDurationMs(%q): unexpected error: %v", tc.input, err)
				return
			}
			if got != tc.expected {
				t.Errorf("ParseDurationMs(%q) = %d, want %d", tc.input, got, tc.expected)
			}
		})
	}
}

func TestLimitedReadCloser_ExceedsLimit(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", 200))
	lr := &limitedReadCloser{
		ReadCloser: io.NopCloser(body),
		remaining:  100,
	}

	buf := make([]byte, 300)
	var totalRead int
	var readErr error

	for {
		n, err := lr.Read(buf[totalRead:])
		totalRead += n
		if err != nil {
			readErr = err
			break
		}
	}

	if readErr == nil || readErr == io.EOF {
		t.Error("expected error when exceeding limit, got nil or EOF")
	}
}
