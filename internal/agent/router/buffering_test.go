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

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestRequestBufferingMiddleware_BuffersRequestBody(t *testing.T) {
	config := &pb.BufferingConfig{
		RequestBuffering: true,
		MaxBufferSize:    10 * 1024 * 1024, // 10MB
		MemoryThreshold:  1024,             // 1KB
	}
	m := NewRequestBufferingMiddleware(config)

	body := strings.Repeat("buffered request body content. ", 100)
	var capturedBody string

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read buffered body: %v", err)
			return
		}
		capturedBody = string(data)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedBody != body {
		t.Errorf("buffered body mismatch: got %d bytes, want %d bytes", len(capturedBody), len(body))
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestRequestBufferingMiddleware_MaxSizeExceeded(t *testing.T) {
	config := &pb.BufferingConfig{
		RequestBuffering: true,
		MaxBufferSize:    100, // Very small limit
		MemoryThreshold:  50,
	}
	m := NewRequestBufferingMiddleware(config)

	body := strings.Repeat("x", 200) // Exceeds max size

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when body exceeds max size")
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", rec.Code)
	}
}

func TestRequestBufferingMiddleware_Disabled(t *testing.T) {
	// Request buffering disabled
	config := &pb.BufferingConfig{
		RequestBuffering: false,
	}
	m := NewRequestBufferingMiddleware(config)

	body := "test body"
	var capturedBody string

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		capturedBody = string(data)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedBody != body {
		t.Errorf("body should pass through when buffering disabled")
	}
}

func TestRequestBufferingMiddleware_NilConfig(t *testing.T) {
	m := NewRequestBufferingMiddleware(nil)

	body := "test body"
	var called bool

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should be called when config is nil")
	}
}

func TestRequestBufferingMiddleware_NoBody(t *testing.T) {
	config := &pb.BufferingConfig{
		RequestBuffering: true,
		MaxBufferSize:    1024,
	}
	m := NewRequestBufferingMiddleware(config)

	var called bool
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should be called for bodyless requests")
	}
}

func TestRequestBufferingMiddleware_SpillToFile(t *testing.T) {
	config := &pb.BufferingConfig{
		RequestBuffering: true,
		MaxBufferSize:    10 * 1024 * 1024, // 10MB
		MemoryThreshold:  100,              // Very small threshold to force file spill
	}
	m := NewRequestBufferingMiddleware(config)

	body := strings.Repeat("file-spill-test-data-", 100) // >100 bytes
	var capturedBody string

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read file-spilled body: %v", err)
			return
		}
		capturedBody = string(data)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedBody != body {
		t.Errorf("file-spilled body mismatch: got %d bytes, want %d bytes", len(capturedBody), len(body))
	}
}

func TestResponseBufferingMiddleware_BuffersResponseBody(t *testing.T) {
	config := &pb.BufferingConfig{
		ResponseBuffering: true,
		MaxBufferSize:     10 * 1024 * 1024,
	}
	m := NewResponseBufferingMiddleware(config)

	body := strings.Repeat("buffered response body. ", 100)

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	respBody, _ := io.ReadAll(result.Body)
	if string(respBody) != body {
		t.Errorf("buffered response body mismatch: got %d bytes, want %d bytes", len(respBody), len(body))
	}

	if result.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}
}

func TestResponseBufferingMiddleware_MaxSizeOverflow(t *testing.T) {
	config := &pb.BufferingConfig{
		ResponseBuffering: true,
		MaxBufferSize:     50, // Very small limit
	}
	m := NewResponseBufferingMiddleware(config)

	body := strings.Repeat("x", 100) // Exceeds max buffer size

	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	// When overflow happens, the buffered writer discards excess data
	respBody, _ := io.ReadAll(result.Body)
	if len(respBody) > 50 {
		t.Errorf("expected response to be truncated at max buffer size, got %d bytes", len(respBody))
	}
}

func TestResponseBufferingMiddleware_Disabled(t *testing.T) {
	config := &pb.BufferingConfig{
		ResponseBuffering: false,
	}
	m := NewResponseBufferingMiddleware(config)

	body := "passthrough body"
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	respBody, _ := io.ReadAll(result.Body)
	if string(respBody) != body {
		t.Error("body should pass through when response buffering is disabled")
	}
}
