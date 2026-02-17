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

package overload

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestLoadSheddingMiddleware_PassthroughWhenNotOverloaded(t *testing.T) {
	config := DefaultOverloadConfig()
	config.Enabled = true
	config.MemoryThreshold = 0.99
	config.GoroutineThreshold = 10000000
	config.MaxActiveConnections = 10000000

	mgr := NewManager(config, zap.NewNop())
	mw := NewLoadSheddingMiddleware(mgr, 30, zap.NewNop())

	backendCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backendCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !backendCalled {
		t.Error("expected backend to be called when not overloaded")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestLoadSheddingMiddleware_503WhenOverloaded(t *testing.T) {
	config := DefaultOverloadConfig()
	config.Enabled = true
	// Force overload via low connection threshold.
	config.MaxActiveConnections = 1
	config.ActiveConnectionRecoverThreshold = 0
	config.MemoryThreshold = 0.99
	config.GoroutineThreshold = 10000000

	mgr := NewManager(config, zap.NewNop())
	mgr.IncrementConnections()
	mgr.checkResources()

	mw := NewLoadSheddingMiddleware(mgr, 60, zap.NewNop())

	backendCalled := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backendCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if backendCalled {
		t.Error("expected backend NOT to be called when overloaded")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}
}

func TestLoadSheddingMiddleware_RetryAfterHeader(t *testing.T) {
	config := DefaultOverloadConfig()
	config.Enabled = true
	config.MaxActiveConnections = 1
	config.ActiveConnectionRecoverThreshold = 0
	config.MemoryThreshold = 0.99
	config.GoroutineThreshold = 10000000

	mgr := NewManager(config, zap.NewNop())
	mgr.IncrementConnections()
	mgr.checkResources()

	mw := NewLoadSheddingMiddleware(mgr, 45, zap.NewNop())

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter != "45" {
		t.Errorf("expected Retry-After header '45', got %q", retryAfter)
	}
}
