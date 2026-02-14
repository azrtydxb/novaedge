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

package vault

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

const testHealthPath = "/v1/sys/health"

func TestHealthStatus_IsHealthy(t *testing.T) {
	tests := []struct {
		name        string
		status      HealthStatus
		wantHealthy bool
	}{
		{
			name: "healthy - initialized, unsealed, active",
			status: HealthStatus{
				Initialized: true,
				Sealed:      false,
				Standby:     false,
			},
			wantHealthy: true,
		},
		{
			name: "unhealthy - not initialized",
			status: HealthStatus{
				Initialized: false,
				Sealed:      false,
				Standby:     false,
			},
			wantHealthy: false,
		},
		{
			name: "unhealthy - sealed",
			status: HealthStatus{
				Initialized: true,
				Sealed:      true,
				Standby:     false,
			},
			wantHealthy: false,
		},
		{
			name: "unhealthy - standby",
			status: HealthStatus{
				Initialized: true,
				Sealed:      false,
				Standby:     true,
			},
			wantHealthy: false,
		},
		{
			name: "unhealthy - sealed and standby",
			status: HealthStatus{
				Initialized: true,
				Sealed:      true,
				Standby:     true,
			},
			wantHealthy: false,
		},
		{
			name: "unhealthy - all false",
			status: HealthStatus{
				Initialized: false,
				Sealed:      false,
				Standby:     false,
			},
			wantHealthy: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.status.IsHealthy()
			if got != tt.wantHealthy {
				t.Errorf("IsHealthy() = %v, want %v", got, tt.wantHealthy)
			}
		})
	}
}

func TestNewHealthChecker(t *testing.T) {
	// With nil logger
	checker := NewHealthChecker(nil, nil)
	if checker == nil {
		t.Fatal("NewHealthChecker() should not return nil")
	}
	if checker.logger == nil {
		t.Error("logger should be set to nop logger when nil is passed")
	}

	// With provided logger
	logger := zap.NewNop()
	checker = NewHealthChecker(nil, logger)
	if checker.logger != logger {
		t.Error("logger should be the provided logger")
	}
}

func TestHealthChecker_Check_Healthy(t *testing.T) {
	// Create a test server that returns healthy status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == testHealthPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"initialized": true,
				"sealed": false,
				"standby": false,
				"server_time_utc": 1234567890,
				"version": "1.15.0",
				"cluster_name": "test-cluster",
				"cluster_id": "test-id"
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create client with test server URL
	client := &Client{
		httpClient: &vaultHTTPClient{
			address: server.URL,
			token:   "test-token",
		},
	}

	checker := NewHealthChecker(client, zap.NewNop())
	err := checker.Check(context.Background())
	if err != nil {
		t.Errorf("Check() returned unexpected error: %v", err)
	}
}

func TestHealthChecker_Check_Sealed(t *testing.T) {
	// Create a test server that returns sealed status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == testHealthPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{
				"initialized": true,
				"sealed": true,
				"standby": false
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		httpClient: &vaultHTTPClient{
			address: server.URL,
			token:   "test-token",
		},
	}

	checker := NewHealthChecker(client, zap.NewNop())
	err := checker.Check(context.Background())
	if err == nil {
		t.Error("Check() should return error when vault is sealed")
	}
	if err.Error() != "vault is sealed" {
		t.Errorf("Check() error = %v, want 'vault is sealed'", err)
	}
}

func TestHealthChecker_Check_NotInitialized(t *testing.T) {
	// Create a test server that returns not initialized status
	// Note: sealed must be false so we hit the !Initialized check first
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == testHealthPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{
				"initialized": false,
				"sealed": false,
				"standby": false
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		httpClient: &vaultHTTPClient{
			address: server.URL,
			token:   "test-token",
		},
	}

	checker := NewHealthChecker(client, zap.NewNop())
	err := checker.Check(context.Background())
	if err == nil {
		t.Error("Check() should return error when vault is not initialized")
	}
	if err.Error() != "vault is not initialized" {
		t.Errorf("Check() error = %v, want 'vault is not initialized'", err)
	}
}

func TestHealthChecker_Check_Standby(t *testing.T) {
	// Create a test server that returns standby status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == testHealthPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{
				"initialized": true,
				"sealed": false,
				"standby": true
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		httpClient: &vaultHTTPClient{
			address: server.URL,
			token:   "test-token",
		},
	}

	checker := NewHealthChecker(client, zap.NewNop())
	err := checker.Check(context.Background())
	if err == nil {
		t.Error("Check() should return error when vault is in standby")
	}
	if err.Error() != "vault is in standby mode" {
		t.Errorf("Check() error = %v, want 'vault is in standby mode'", err)
	}
}

func TestHealthChecker_Handler_Healthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == testHealthPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"initialized": true, "sealed": false, "standby": false}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		httpClient: &vaultHTTPClient{
			address: server.URL,
			token:   "test-token",
		},
	}

	checker := NewHealthChecker(client, zap.NewNop())
	handler := checker.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health/vault", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Handler() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "vault healthy" {
		t.Errorf("Handler() body = %q, want 'vault healthy'", rec.Body.String())
	}
}

func TestHealthChecker_Handler_Unhealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == testHealthPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"initialized": true, "sealed": true, "standby": false}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		httpClient: &vaultHTTPClient{
			address: server.URL,
			token:   "test-token",
		},
	}

	checker := NewHealthChecker(client, zap.NewNop())
	handler := checker.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health/vault", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Handler() status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHealthChecker_CheckerFunc(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == testHealthPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"initialized": true, "sealed": false, "standby": false}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{
		httpClient: &vaultHTTPClient{
			address: server.URL,
			token:   "test-token",
		},
	}

	checker := NewHealthChecker(client, zap.NewNop())
	checkerFunc := checker.CheckerFunc()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	err := checkerFunc(req)
	if err != nil {
		t.Errorf("CheckerFunc() returned unexpected error: %v", err)
	}
}

func TestHealthChecker_Check_ConnectionError(t *testing.T) {
	client := &Client{
		httpClient: &vaultHTTPClient{
			address: "http://127.0.0.1:9999", // Non-existent server
			token:   "test-token",
		},
	}

	checker := NewHealthChecker(client, zap.NewNop())
	err := checker.Check(context.Background())
	if err == nil {
		t.Error("Check() should return error when connection fails")
	}
}
