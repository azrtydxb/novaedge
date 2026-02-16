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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestHandleForwardAuth_AllowedResponse(t *testing.T) {
	// Create a mock auth server that returns 200
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify forwarded headers
		if method := r.Header.Get("X-Forwarded-Method"); method != "GET" {
			t.Errorf("expected X-Forwarded-Method GET, got %s", method)
		}

		// Return auth headers
		w.Header().Set("X-Auth-User", "testuser")
		w.Header().Set("X-Auth-Groups", "admin,users")
		w.WriteHeader(http.StatusOK)
	}))
	defer authServer.Close()

	config := &pb.ForwardAuthConfig{
		Address:         authServer.URL,
		ResponseHeaders: []string{"X-Auth-User", "X-Auth-Groups"},
		TimeoutMs:       5000,
	}

	handler := NewForwardAuthHandler(context.Background(), config, zap.NewNop(), WithForwardAuthHTTPClient(&http.Client{}))

	nextCalled := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		nextCalled = true
		// Verify response headers were copied
		if user := r.Header.Get("X-Auth-User"); user != "testuser" {
			t.Errorf("expected X-Auth-User=testuser, got %s", user)
		}
		if groups := r.Header.Get("X-Auth-Groups"); groups != "admin,users" {
			t.Errorf("expected X-Auth-Groups=admin,users, got %s", groups)
		}
	})

	middleware := HandleForwardAuth(handler)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req.Header.Set("Authorization", "Bearer token123")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("expected next handler to be called")
	}
}

func TestHandleForwardAuth_UnauthorizedResponse(t *testing.T) {
	// Create a mock auth server that returns 401
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="test"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer authServer.Close()

	config := &pb.ForwardAuthConfig{
		Address:   authServer.URL,
		TimeoutMs: 5000,
	}

	handler := NewForwardAuthHandler(context.Background(), config, zap.NewNop(), WithForwardAuthHTTPClient(&http.Client{}))

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("next handler should not be called on 401")
	})

	middleware := HandleForwardAuth(handler)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestHandleForwardAuth_ForbiddenResponse(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer authServer.Close()

	config := &pb.ForwardAuthConfig{
		Address:   authServer.URL,
		TimeoutMs: 5000,
	}

	handler := NewForwardAuthHandler(context.Background(), config, zap.NewNop(), WithForwardAuthHTTPClient(&http.Client{}))

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("next handler should not be called on 403")
	})

	middleware := HandleForwardAuth(handler)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.Code)
	}
}

func TestHandleForwardAuth_HeaderForwarding(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify only specified headers were forwarded
		if auth := r.Header.Get("Authorization"); auth != "Bearer mytoken" {
			t.Errorf("expected Authorization header forwarded, got %q", auth)
		}
		if cookie := r.Header.Get("Cookie"); cookie != "" {
			t.Errorf("expected Cookie header NOT forwarded, got %q", cookie)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer authServer.Close()

	config := &pb.ForwardAuthConfig{
		Address:     authServer.URL,
		AuthHeaders: []string{"Authorization"}, // Only forward Authorization
		TimeoutMs:   5000,
	}

	handler := NewForwardAuthHandler(context.Background(), config, zap.NewNop(), WithForwardAuthHTTPClient(&http.Client{}))

	nextCalled := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		nextCalled = true
	})

	middleware := HandleForwardAuth(handler)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer mytoken")
	req.Header.Set("Cookie", "session=abc123")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("expected next handler to be called")
	}
}

func TestHandleForwardAuth_CachingSuccess(t *testing.T) {
	callCount := 0
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("X-Auth-User", "cached-user")
		w.WriteHeader(http.StatusOK)
	}))
	defer authServer.Close()

	config := &pb.ForwardAuthConfig{
		Address:         authServer.URL,
		ResponseHeaders: []string{"X-Auth-User"},
		TimeoutMs:       5000,
		CacheTtlSeconds: 60,
	}

	handler := NewForwardAuthHandler(context.Background(), config, zap.NewNop(), WithForwardAuthHTTPClient(&http.Client{}))

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})

	middleware := HandleForwardAuth(handler)(next)

	// First request
	req1 := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req1.Header.Set("Authorization", "Bearer token1")
	rec1 := httptest.NewRecorder()
	middleware.ServeHTTP(rec1, req1)

	// Second request with same auth
	req2 := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req2.Header.Set("Authorization", "Bearer token1")
	rec2 := httptest.NewRecorder()
	middleware.ServeHTTP(rec2, req2)

	// Auth server should only be called once (second should use cache)
	if callCount != 1 {
		t.Errorf("expected auth server to be called 1 time, got %d", callCount)
	}
}

func TestHandleForwardAuth_AuthServiceUnavailable(t *testing.T) {
	// Use an address that won't connect
	config := &pb.ForwardAuthConfig{
		Address:   "http://127.0.0.1:1",
		TimeoutMs: 100,
	}

	handler := NewForwardAuthHandler(context.Background(), config, zap.NewNop(), WithForwardAuthHTTPClient(&http.Client{}))

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("next handler should not be called")
	})

	middleware := HandleForwardAuth(handler)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}
}

func TestRequestProto(t *testing.T) {
	// Test HTTP
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if proto := requestProto(req); proto != "http" {
		t.Errorf("expected http, got %s", proto)
	}

	// Test with X-Forwarded-Proto
	req.Header.Set("X-Forwarded-Proto", "https")
	if proto := requestProto(req); proto != "https" {
		t.Errorf("expected https, got %s", proto)
	}
}
