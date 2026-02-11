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

	"go.uber.org/zap/zaptest"
)

func TestIsSSERequest_WithAcceptHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Accept", "text/event-stream")

	if !IsSSERequest(req) {
		t.Error("expected SSE request to be detected with Accept: text/event-stream")
	}
}

func TestIsSSERequest_WithoutAcceptHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	if IsSSERequest(req) {
		t.Error("should not detect SSE without Accept header")
	}
}

func TestIsSSERequest_WithWrongAcceptHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Accept", "application/json")

	if IsSSERequest(req) {
		t.Error("should not detect SSE with application/json Accept header")
	}
}

func TestIsSSERequest_NormalHTTPRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Accept", "text/html")

	if IsSSERequest(req) {
		t.Error("normal HTTP request should not be detected as SSE")
	}
}

func TestDefaultSSEConfig(t *testing.T) {
	cfg := DefaultSSEConfig()

	if cfg.IdleTimeout != SSEDefaultIdleTimeout {
		t.Errorf("expected idle timeout %v, got %v", SSEDefaultIdleTimeout, cfg.IdleTimeout)
	}
	if cfg.HeartbeatInterval != SSEDefaultHeartbeatInterval {
		t.Errorf("expected heartbeat interval %v, got %v", SSEDefaultHeartbeatInterval, cfg.HeartbeatInterval)
	}
	if cfg.MaxConnections != 0 {
		t.Errorf("expected unlimited max connections (0), got %d", cfg.MaxConnections)
	}
}

func TestNewSSEProxy_WithNilConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	proxy := NewSSEProxy(logger, nil)

	if proxy == nil {
		t.Fatal("expected non-nil proxy")
	}

	cfg := proxy.getConfig()
	if cfg.IdleTimeout != SSEDefaultIdleTimeout {
		t.Errorf("expected default idle timeout, got %v", cfg.IdleTimeout)
	}
}

func TestNewSSEProxy_WithCustomConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	customConfig := &SSEConfig{
		IdleTimeout:       10 * time.Minute,
		HeartbeatInterval: 15 * time.Second,
		MaxConnections:    100,
	}
	proxy := NewSSEProxy(logger, customConfig)

	if proxy == nil {
		t.Fatal("expected non-nil proxy")
	}

	cfg := proxy.getConfig()
	if cfg.IdleTimeout != 10*time.Minute {
		t.Errorf("expected idle timeout 10m, got %v", cfg.IdleTimeout)
	}
	if cfg.HeartbeatInterval != 15*time.Second {
		t.Errorf("expected heartbeat interval 15s, got %v", cfg.HeartbeatInterval)
	}
	if cfg.MaxConnections != 100 {
		t.Errorf("expected max connections 100, got %d", cfg.MaxConnections)
	}
}

func TestSSEProxy_ActiveConnections(t *testing.T) {
	logger := zaptest.NewLogger(t)
	proxy := NewSSEProxy(logger, nil)

	if proxy.ActiveConnections() != 0 {
		t.Errorf("expected 0 active connections, got %d", proxy.ActiveConnections())
	}
}

func TestSSEProxy_Draining(t *testing.T) {
	logger := zaptest.NewLogger(t)
	proxy := NewSSEProxy(logger, nil)

	if proxy.IsDraining() {
		t.Error("proxy should not be draining initially")
	}

	proxy.SetDraining(true)
	if !proxy.IsDraining() {
		t.Error("proxy should be draining after SetDraining(true)")
	}

	proxy.SetDraining(false)
	if proxy.IsDraining() {
		t.Error("proxy should not be draining after SetDraining(false)")
	}
}

func TestSSEProxy_UpdateConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	proxy := NewSSEProxy(logger, nil)

	newConfig := &SSEConfig{
		IdleTimeout:       15 * time.Minute,
		HeartbeatInterval: 45 * time.Second,
		MaxConnections:    200,
	}
	proxy.UpdateConfig(newConfig)

	cfg := proxy.getConfig()
	if cfg.IdleTimeout != 15*time.Minute {
		t.Errorf("expected updated idle timeout 15m, got %v", cfg.IdleTimeout)
	}
	if cfg.MaxConnections != 200 {
		t.Errorf("expected updated max connections 200, got %d", cfg.MaxConnections)
	}
}

func TestSSEProxy_UpdateConfig_Nil(t *testing.T) {
	logger := zaptest.NewLogger(t)
	proxy := NewSSEProxy(logger, &SSEConfig{
		IdleTimeout: 10 * time.Minute,
	})

	proxy.UpdateConfig(nil)

	cfg := proxy.getConfig()
	if cfg.IdleTimeout != 10*time.Minute {
		t.Errorf("nil update should preserve existing config, got %v", cfg.IdleTimeout)
	}
}

func TestSSEProxy_RejectsDrainingConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	proxy := NewSSEProxy(logger, nil)
	proxy.SetDraining(true)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Accept", "text/event-stream")

	err := proxy.ProxySSE(recorder, req, "http://backend:8080")
	if err == nil {
		t.Error("expected error when proxy is draining")
	}

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", recorder.Code)
	}
}

func TestSSEProxy_RejectsMaxConnections(t *testing.T) {
	logger := zaptest.NewLogger(t)
	proxy := NewSSEProxy(logger, &SSEConfig{
		IdleTimeout:       SSEDefaultIdleTimeout,
		HeartbeatInterval: SSEDefaultHeartbeatInterval,
		MaxConnections:    0, // Set to 0 which means unlimited
	})

	// With MaxConnections=0 (unlimited), no rejection should happen based on count
	// Set a non-zero limit to test rejection
	proxy.UpdateConfig(&SSEConfig{
		IdleTimeout:       SSEDefaultIdleTimeout,
		HeartbeatInterval: SSEDefaultHeartbeatInterval,
		MaxConnections:    1,
	})

	// Simulate an active connection
	proxy.activeConnections.Add(1)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	err := proxy.ProxySSE(recorder, req, "http://backend:8080")
	if err == nil {
		t.Error("expected error when max connections reached")
	}

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", recorder.Code)
	}

	proxy.activeConnections.Add(-1)
}

func TestCopySSEHeaders(t *testing.T) {
	src := http.Header{}
	src.Set("Authorization", "Bearer token123")
	src.Set("Cookie", "session=abc")
	src.Set("User-Agent", "test-agent")
	src.Set("X-Request-ID", "req-123")
	src.Set("X-Custom-Header", "custom-value")
	src.Set("Content-Type", "text/event-stream") // Should not be copied

	dst := http.Header{}
	copySSEHeaders(src, dst)

	if dst.Get("Authorization") != "Bearer token123" {
		t.Error("expected Authorization header to be copied")
	}
	if dst.Get("Cookie") != "session=abc" {
		t.Error("expected Cookie header to be copied")
	}
	if dst.Get("User-Agent") != "test-agent" {
		t.Error("expected User-Agent header to be copied")
	}
	if dst.Get("X-Request-ID") != "req-123" {
		t.Error("expected X-Request-ID header to be copied")
	}
	if dst.Get("X-Custom-Header") != "custom-value" {
		t.Error("expected X-Custom-Header to be copied")
	}
}
