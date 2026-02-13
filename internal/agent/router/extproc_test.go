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
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

const testContentTypeJSON = "application/json"

// mockExtProcClient is a test double for extProcClient that allows
// controlling responses and simulating failures.
type mockExtProcClient struct {
	responses map[string]*ProcessingResponse
	err       error
	calls     []ProcessingRequest
}

func newMockExtProcClient() *mockExtProcClient {
	return &mockExtProcClient{
		responses: make(map[string]*ProcessingResponse),
	}
}

func (m *mockExtProcClient) ProcessRequest(_ context.Context, req *ProcessingRequest) (*ProcessingResponse, error) {
	m.calls = append(m.calls, *req)
	if m.err != nil {
		return nil, m.err
	}
	if resp, ok := m.responses[req.Phase]; ok {
		return resp, nil
	}
	return &ProcessingResponse{}, nil
}

func (m *mockExtProcClient) Close() error {
	return nil
}

func TestExtProcMiddleware_RequestHeaderProcessing(t *testing.T) {
	logger := zap.NewNop()
	mock := newMockExtProcClient()
	mock.responses[PhaseRequestHeaders] = &ProcessingResponse{
		HeadersToAdd: map[string]string{
			"X-Enriched":   headerValueTrue,
			"X-Request-Id": "ext-123",
		},
		HeadersToRemove: []string{"X-Internal"},
	}

	var capturedHeaders http.Header
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	})

	cfg := &ExtProcConfig{
		Address:                "localhost:50051",
		Timeout:                time.Second,
		FailOpen:               true,
		ProcessRequestHeaders:  true,
		ProcessResponseHeaders: false,
	}
	m := newExtProcMiddlewareWithClient(cfg, mock, logger, next)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("X-Internal", "secret")
	req.Header.Set("Accept", testContentTypeJSON)

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if capturedHeaders.Get("X-Enriched") != headerValueTrue {
		t.Errorf("expected X-Enriched header to be 'true', got %q", capturedHeaders.Get("X-Enriched"))
	}
	if capturedHeaders.Get("X-Request-Id") != "ext-123" {
		t.Errorf("expected X-Request-Id header to be 'ext-123', got %q", capturedHeaders.Get("X-Request-Id"))
	}
	if capturedHeaders.Get("X-Internal") != "" {
		t.Errorf("expected X-Internal header to be removed, got %q", capturedHeaders.Get("X-Internal"))
	}
	// Original headers should still be present
	if capturedHeaders.Get("Accept") != testContentTypeJSON {
		t.Errorf("expected Accept header preserved, got %q", capturedHeaders.Get("Accept"))
	}

	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 extproc call, got %d", len(mock.calls))
	}
	if mock.calls[0].Phase != PhaseRequestHeaders {
		t.Errorf("expected phase %q, got %q", PhaseRequestHeaders, mock.calls[0].Phase)
	}
}

func TestExtProcMiddleware_FailOpenBehavior(t *testing.T) {
	logger := zap.NewNop()
	mock := newMockExtProcClient()
	mock.err = errors.New("connection refused")

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	cfg := &ExtProcConfig{
		Address:               "localhost:50051",
		Timeout:               time.Second,
		FailOpen:              true,
		ProcessRequestHeaders: true,
	}
	m := newExtProcMiddlewareWithClient(cfg, mock, logger, next)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("expected next handler to be called in fail-open mode")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 in fail-open mode, got %d", rec.Code)
	}
}

func TestExtProcMiddleware_FailClosedBehavior(t *testing.T) {
	logger := zap.NewNop()
	mock := newMockExtProcClient()
	mock.err = errors.New("connection refused")

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	cfg := &ExtProcConfig{
		Address:               "localhost:50051",
		Timeout:               time.Second,
		FailOpen:              false,
		ProcessRequestHeaders: true,
	}
	m := newExtProcMiddlewareWithClient(cfg, mock, logger, next)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)

	if nextCalled {
		t.Error("expected next handler NOT to be called in fail-closed mode")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 in fail-closed mode, got %d", rec.Code)
	}
}

func TestExtProcMiddleware_TimeoutHandling(t *testing.T) {
	logger := zap.NewNop()

	// Create a client that simulates a slow external service by blocking
	// until the context is canceled.
	slowClient := &slowExtProcClient{}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	cfg := &ExtProcConfig{
		Address:               "localhost:50051",
		Timeout:               10 * time.Millisecond,
		FailOpen:              true,
		ProcessRequestHeaders: true,
	}
	m := newExtProcMiddlewareWithClient(cfg, slowClient, logger, next)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)

	// With fail-open, request should still succeed despite timeout
	if !nextCalled {
		t.Error("expected next handler to be called after timeout with fail-open")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

// slowExtProcClient blocks until the context is canceled.
type slowExtProcClient struct{}

func (s *slowExtProcClient) ProcessRequest(ctx context.Context, _ *ProcessingRequest) (*ProcessingResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *slowExtProcClient) Close() error {
	return nil
}

func TestExtProcMiddleware_HeaderModificationsApplied(t *testing.T) {
	logger := zap.NewNop()
	mock := newMockExtProcClient()
	mock.responses[PhaseRequestHeaders] = &ProcessingResponse{
		HeadersToAdd: map[string]string{
			"Authorization": "Bearer injected-token",
		},
	}
	mock.responses[PhaseResponseHeaders] = &ProcessingResponse{
		HeadersToAdd: map[string]string{
			"X-Processed-By": "extproc-service",
		},
		HeadersToRemove: []string{"Server"},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request header was modified
		if r.Header.Get("Authorization") != "Bearer injected-token" {
			t.Errorf("expected Authorization header injected, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Server", "novaedge")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("hello")); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	})

	cfg := &ExtProcConfig{
		Address:                "localhost:50051",
		Timeout:                time.Second,
		FailOpen:               true,
		ProcessRequestHeaders:  true,
		ProcessResponseHeaders: true,
	}
	m := newExtProcMiddlewareWithClient(cfg, mock, logger, next)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Processed-By") != "extproc-service" {
		t.Errorf("expected response header X-Processed-By, got %q", rec.Header().Get("X-Processed-By"))
	}
	if rec.Header().Get("Server") != "" {
		t.Errorf("expected Server header to be removed, got %q", rec.Header().Get("Server"))
	}
	if rec.Header().Get("Content-Type") != "text/plain" {
		t.Errorf("expected Content-Type preserved, got %q", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "hello" {
		t.Errorf("expected body 'hello', got %q", rec.Body.String())
	}
}

func TestExtProcMiddleware_ImmediateResponse(t *testing.T) {
	logger := zap.NewNop()
	mock := newMockExtProcClient()
	mock.responses[PhaseRequestHeaders] = &ProcessingResponse{
		ImmediateResponseCode: http.StatusForbidden,
		ImmediateResponseBody: []byte("access denied by external policy"),
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	cfg := &ExtProcConfig{
		Address:               "localhost:50051",
		Timeout:               time.Second,
		FailOpen:              true,
		ProcessRequestHeaders: true,
	}
	m := newExtProcMiddlewareWithClient(cfg, mock, logger, next)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)

	if nextCalled {
		t.Error("expected next handler NOT to be called when immediate response returned")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.Code)
	}
	if rec.Body.String() != "access denied by external policy" {
		t.Errorf("expected body 'access denied by external policy', got %q", rec.Body.String())
	}
}

func TestExtProcMiddleware_RequestBodyProcessing(t *testing.T) {
	logger := zap.NewNop()
	mock := newMockExtProcClient()
	mock.responses[PhaseRequestHeaders] = &ProcessingResponse{}
	mock.responses[PhaseRequestBody] = &ProcessingResponse{
		HeadersToAdd: map[string]string{
			"X-Body-Inspected": headerValueTrue,
		},
	}

	var capturedBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		capturedBody = string(data)
		w.WriteHeader(http.StatusOK)
	})

	cfg := &ExtProcConfig{
		Address:               "localhost:50051",
		Timeout:               time.Second,
		FailOpen:              true,
		ProcessRequestHeaders: true,
		ProcessRequestBody:    true,
	}
	m := newExtProcMiddlewareWithClient(cfg, mock, logger, next)

	body := `{"user":"test","action":"create"}`
	req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(body))
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if capturedBody != body {
		t.Errorf("expected body preserved for downstream, got %q", capturedBody)
	}

	// Verify that the body was sent to the extproc service
	var foundBodyCall bool
	for _, call := range mock.calls {
		if call.Phase == PhaseRequestBody {
			foundBodyCall = true
			if string(call.Body) != body {
				t.Errorf("expected extproc to receive body %q, got %q", body, string(call.Body))
			}
		}
	}
	if !foundBodyCall {
		t.Error("expected a request_body phase call to extproc")
	}
}

func TestExtProcMiddleware_NoProcessing(t *testing.T) {
	logger := zap.NewNop()
	mock := newMockExtProcClient()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	cfg := &ExtProcConfig{
		Address:                "localhost:50051",
		Timeout:                time.Second,
		FailOpen:               true,
		ProcessRequestHeaders:  false,
		ProcessRequestBody:     false,
		ProcessResponseHeaders: false,
		ProcessResponseBody:    false,
	}
	m := newExtProcMiddlewareWithClient(cfg, mock, logger, next)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("expected next handler to be called")
	}
	if len(mock.calls) != 0 {
		t.Errorf("expected no extproc calls, got %d", len(mock.calls))
	}
}

func TestExtProcMiddleware_DefaultConfig(t *testing.T) {
	cfg := DefaultExtProcConfig("localhost:50051")

	if cfg.Address != "localhost:50051" {
		t.Errorf("expected address 'localhost:50051', got %q", cfg.Address)
	}
	if cfg.Timeout != DefaultExtProcTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultExtProcTimeout, cfg.Timeout)
	}
	if !cfg.FailOpen {
		t.Error("expected FailOpen to be true by default")
	}
	if !cfg.ProcessRequestHeaders {
		t.Error("expected ProcessRequestHeaders to be true by default")
	}
	if cfg.ProcessRequestBody {
		t.Error("expected ProcessRequestBody to be false by default")
	}
	if !cfg.ProcessResponseHeaders {
		t.Error("expected ProcessResponseHeaders to be true by default")
	}
	if cfg.ProcessResponseBody {
		t.Error("expected ProcessResponseBody to be false by default")
	}
}

func TestExtProcMiddleware_FailOpenResponsePhase(t *testing.T) {
	logger := zap.NewNop()

	// Client that succeeds for request phase but fails for response phase
	phaseClient := &phaseAwareClient{
		failPhases: map[string]bool{PhaseResponseHeaders: true},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok")); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	})

	cfg := &ExtProcConfig{
		Address:                "localhost:50051",
		Timeout:                time.Second,
		FailOpen:               true,
		ProcessRequestHeaders:  false,
		ProcessResponseHeaders: true,
	}
	m := newExtProcMiddlewareWithClient(cfg, phaseClient, logger, next)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)

	// Should still return the original response
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", rec.Body.String())
	}
}

// phaseAwareClient fails for specific phases.
type phaseAwareClient struct {
	failPhases map[string]bool
}

func (p *phaseAwareClient) ProcessRequest(_ context.Context, req *ProcessingRequest) (*ProcessingResponse, error) {
	if p.failPhases[req.Phase] {
		return nil, errors.New("service unavailable for phase")
	}
	return &ProcessingResponse{}, nil
}

func (p *phaseAwareClient) Close() error {
	return nil
}

func TestExtProcResponseWriter_CapturesResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	ew := newExtProcResponseWriter(rec)

	ew.Header().Set("X-Custom", "value")
	ew.WriteHeader(http.StatusCreated)
	if _, err := ew.Write([]byte("response body")); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Before flush, underlying recorder should have nothing
	if rec.Code != http.StatusOK {
		t.Errorf("expected recorder to still be at default 200 before flush, got %d", rec.Code)
	}

	ew.flush()

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status 201 after flush, got %d", rec.Code)
	}
	if rec.Header().Get("X-Custom") != "value" {
		t.Errorf("expected X-Custom header after flush, got %q", rec.Header().Get("X-Custom"))
	}
	if rec.Body.String() != "response body" {
		t.Errorf("expected body after flush, got %q", rec.Body.String())
	}
}

func TestExtProcMiddleware_TimeoutFailClosed(t *testing.T) {
	logger := zap.NewNop()
	slowClient := &slowExtProcClient{}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	cfg := &ExtProcConfig{
		Address:               "localhost:50051",
		Timeout:               10 * time.Millisecond,
		FailOpen:              false,
		ProcessRequestHeaders: true,
	}
	m := newExtProcMiddlewareWithClient(cfg, slowClient, logger, next)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)

	if nextCalled {
		t.Error("expected next handler NOT to be called when timeout and fail-closed")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 when timeout and fail-closed, got %d", rec.Code)
	}
}
