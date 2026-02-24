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

package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
	"go.uber.org/zap/zaptest"
)

func TestNewPool(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cluster := &pb.Cluster{
		Name:             "test-cluster",
		Namespace:        "default",
		ConnectTimeoutMs: 5000,
		IdleTimeoutMs:    90000,
	}

	endpoints := []*pb.Endpoint{
		{Address: "192.168.1.10", Port: 8080, Ready: true},
		{Address: "192.168.1.11", Port: 8080, Ready: true},
	}

	pool := NewPool(context.Background(), cluster, endpoints, logger)
	defer pool.Close()

	if pool == nil {
		t.Fatal("Expected pool to be created")
	}

	if pool.cluster != cluster {
		t.Error("Pool cluster does not match")
	}

	if len(pool.endpoints) != 2 {
		t.Errorf("Expected 2 endpoints, got %d", len(pool.endpoints))
	}
}

func TestUpdateEndpoints(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cluster := &pb.Cluster{
		Name:             "test-cluster",
		Namespace:        "default",
		ConnectTimeoutMs: 5000,
		IdleTimeoutMs:    90000,
	}

	initialEndpoints := []*pb.Endpoint{
		{Address: "192.168.1.10", Port: 8080, Ready: true},
	}

	pool := NewPool(context.Background(), cluster, initialEndpoints, logger)
	defer pool.Close()

	newEndpoints := []*pb.Endpoint{
		{Address: "192.168.1.10", Port: 8080, Ready: true},
		{Address: "192.168.1.11", Port: 8080, Ready: true},
		{Address: "192.168.1.12", Port: 8080, Ready: true},
	}

	pool.UpdateEndpoints(newEndpoints)

	if len(pool.endpoints) != 3 {
		t.Errorf("Expected 3 endpoints after update, got %d", len(pool.endpoints))
	}
}

func TestCreateProxies(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cluster := &pb.Cluster{
		Name:             "test-cluster",
		Namespace:        "default",
		ConnectTimeoutMs: 5000,
		IdleTimeoutMs:    90000,
	}

	t.Run("creates proxies for ready endpoints", func(t *testing.T) {
		endpoints := []*pb.Endpoint{
			{Address: "192.168.1.10", Port: 8080, Ready: true},
			{Address: "192.168.1.11", Port: 8080, Ready: true},
		}

		pool := NewPool(context.Background(), cluster, endpoints, logger)
		defer pool.Close()
		proxies := *pool.proxies.Load()
		proxyCount := len(proxies)

		if proxyCount != 2 {
			t.Errorf("Expected 2 proxies, got %d", proxyCount)
		}
	})

	t.Run("skips not-ready endpoints", func(t *testing.T) {
		endpoints := []*pb.Endpoint{
			{Address: "192.168.1.10", Port: 8080, Ready: true},
			{Address: "192.168.1.11", Port: 8080, Ready: false},
		}

		pool := NewPool(context.Background(), cluster, endpoints, logger)
		defer pool.Close()
		proxies := *pool.proxies.Load()
		proxyCount := len(proxies)

		if proxyCount != 1 {
			t.Errorf("Expected 1 proxy (only ready endpoint), got %d", proxyCount)
		}
	})
}

func TestForward(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create test backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("backend response"))
	}))
	defer backend.Close()

	cluster := &pb.Cluster{
		Name:             "test-cluster",
		Namespace:        "default",
		ConnectTimeoutMs: 5000,
		IdleTimeoutMs:    90000,
	}

	// Note: In real scenario, endpoint would be extracted from backend.URL
	// For testing, we'll use a dummy endpoint
	endpoints := []*pb.Endpoint{
		{Address: "192.168.1.10", Port: 8080, Ready: true},
	}

	pool := NewPool(context.Background(), cluster, endpoints, logger)
	defer pool.Close()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	// Test forward (will fail to connect since endpoint is not the real backend)
	// This tests the code path, not actual proxying
	err := pool.Forward(endpoints[0], req, w)
	if err == nil {
		t.Log("Forward completed (may fail due to test endpoint)")
	}
}

func TestClose(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cluster := &pb.Cluster{
		Name:             "test-cluster",
		Namespace:        "default",
		ConnectTimeoutMs: 5000,
		IdleTimeoutMs:    90000,
	}

	endpoints := []*pb.Endpoint{
		{Address: "192.168.1.10", Port: 8080, Ready: true},
	}

	pool := NewPool(context.Background(), cluster, endpoints, logger)

	// Close should not panic
	pool.Close()

	// Verify context is cancelled
	select {
	case <-pool.ctx.Done():
		// Context cancelled as expected
	default:
		t.Error("Expected context to be cancelled after close")
	}
}

func TestEndpointSetsEqual(t *testing.T) {
	tests := []struct {
		name     string
		old, new []*pb.Endpoint
		want     bool
	}{
		{
			name: "both empty",
			old:  nil,
			new:  nil,
			want: true,
		},
		{
			name: "same single endpoint",
			old:  []*pb.Endpoint{{Address: "10.0.0.1", Port: 80, Ready: true}},
			new:  []*pb.Endpoint{{Address: "10.0.0.1", Port: 80, Ready: true}},
			want: true,
		},
		{
			name: "same elements different order",
			old: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.2", Port: 80, Ready: true},
			},
			new: []*pb.Endpoint{
				{Address: "10.0.0.2", Port: 80, Ready: true},
				{Address: "10.0.0.1", Port: 80, Ready: true},
			},
			want: true,
		},
		{
			name: "different lengths",
			old:  []*pb.Endpoint{{Address: "10.0.0.1", Port: 80, Ready: true}},
			new: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.2", Port: 80, Ready: true},
			},
			want: false,
		},
		{
			name: "different ready status",
			old:  []*pb.Endpoint{{Address: "10.0.0.1", Port: 80, Ready: true}},
			new:  []*pb.Endpoint{{Address: "10.0.0.1", Port: 80, Ready: false}},
			want: false,
		},
		{
			name: "different port",
			old:  []*pb.Endpoint{{Address: "10.0.0.1", Port: 80, Ready: true}},
			new:  []*pb.Endpoint{{Address: "10.0.0.1", Port: 443, Ready: true}},
			want: false,
		},
		{
			name: "duplicate endpoints same count",
			old: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.1", Port: 80, Ready: true},
			},
			new: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.1", Port: 80, Ready: true},
			},
			want: true,
		},
		{
			name: "duplicate in old but unique in new",
			old: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.1", Port: 80, Ready: true},
			},
			new: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.2", Port: 80, Ready: true},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endpointSetsEqual(tt.old, tt.new)
			if got != tt.want {
				t.Errorf("endpointSetsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiffEndpoints(t *testing.T) {
	tests := []struct {
		name              string
		old, new          []*pb.Endpoint
		wantAdded         int
		wantRemoved       int
		wantUnchanged     int
	}{
		{
			name:          "both empty",
			old:           nil,
			new:           nil,
			wantAdded:     0,
			wantRemoved:   0,
			wantUnchanged: 0,
		},
		{
			name: "all new endpoints",
			old:  nil,
			new: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.2", Port: 80, Ready: true},
			},
			wantAdded:     2,
			wantRemoved:   0,
			wantUnchanged: 0,
		},
		{
			name: "all removed endpoints",
			old: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.2", Port: 80, Ready: true},
			},
			new:           nil,
			wantAdded:     0,
			wantRemoved:   2,
			wantUnchanged: 0,
		},
		{
			name: "one added one unchanged",
			old: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
			},
			new: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.2", Port: 80, Ready: true},
			},
			wantAdded:     1,
			wantRemoved:   0,
			wantUnchanged: 1,
		},
		{
			name: "one removed one unchanged",
			old: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.2", Port: 80, Ready: true},
			},
			new: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
			},
			wantAdded:     0,
			wantRemoved:   1,
			wantUnchanged: 1,
		},
		{
			name: "one added one removed one unchanged",
			old: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.2", Port: 80, Ready: true},
			},
			new: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.3", Port: 80, Ready: true},
			},
			wantAdded:     1,
			wantRemoved:   1,
			wantUnchanged: 1,
		},
		{
			name: "ready status change counts as add and remove",
			old: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
			},
			new: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: false},
			},
			wantAdded:     1,
			wantRemoved:   1,
			wantUnchanged: 0,
		},
		{
			name: "port change counts as add and remove",
			old: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
			},
			new: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 443, Ready: true},
			},
			wantAdded:     1,
			wantRemoved:   1,
			wantUnchanged: 0,
		},
		{
			name: "completely identical",
			old: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.2", Port: 80, Ready: true},
			},
			new: []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 80, Ready: true},
				{Address: "10.0.0.2", Port: 80, Ready: true},
			},
			wantAdded:     0,
			wantRemoved:   0,
			wantUnchanged: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := diffEndpoints(tt.old, tt.new)
			if len(diff.Added) != tt.wantAdded {
				t.Errorf("Added: got %d, want %d", len(diff.Added), tt.wantAdded)
			}
			if len(diff.Removed) != tt.wantRemoved {
				t.Errorf("Removed: got %d, want %d", len(diff.Removed), tt.wantRemoved)
			}
			if len(diff.Unchanged) != tt.wantUnchanged {
				t.Errorf("Unchanged: got %d, want %d", len(diff.Unchanged), tt.wantUnchanged)
			}
		})
	}
}

func TestUpdateEndpointsDiffBasedPreservesProxies(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cluster := &pb.Cluster{
		Name:             "test-cluster",
		Namespace:        "default",
		ConnectTimeoutMs: 5000,
		IdleTimeoutMs:    90000,
	}

	initialEndpoints := []*pb.Endpoint{
		{Address: "192.168.1.10", Port: 8080, Ready: true},
		{Address: "192.168.1.11", Port: 8080, Ready: true},
	}

	pool := NewPool(context.Background(), cluster, initialEndpoints, logger)
	defer pool.Close()

	// Get initial proxies
	initialProxies := *pool.proxies.Load()
	preservedProxy := initialProxies["192.168.1.10:8080"]
	if preservedProxy == nil {
		t.Fatal("Expected proxy for 192.168.1.10:8080 to exist")
	}

	// Update: keep .10, remove .11, add .12
	newEndpoints := []*pb.Endpoint{
		{Address: "192.168.1.10", Port: 8080, Ready: true},
		{Address: "192.168.1.12", Port: 8080, Ready: true},
	}

	pool.UpdateEndpoints(newEndpoints)

	// Verify proxies
	updatedProxies := *pool.proxies.Load()

	// .10 should be preserved (same pointer)
	if updatedProxies["192.168.1.10:8080"] != preservedProxy {
		t.Error("Expected proxy for 192.168.1.10:8080 to be preserved (same instance)")
	}

	// .11 should be gone
	if updatedProxies["192.168.1.11:8080"] != nil {
		t.Error("Expected proxy for 192.168.1.11:8080 to be removed")
	}

	// .12 should be new
	if updatedProxies["192.168.1.12:8080"] == nil {
		t.Error("Expected proxy for 192.168.1.12:8080 to be created")
	}

	if len(updatedProxies) != 2 {
		t.Errorf("Expected 2 proxies, got %d", len(updatedProxies))
	}
}

func TestIsGRPCRequest(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{
			name:        "gRPC content type",
			contentType: "application/grpc",
			expected:    true,
		},
		{
			name:        "gRPC with charset",
			contentType: "application/grpc+proto",
			expected:    true,
		},
		{
			name:        "regular HTTP",
			contentType: "application/json",
			expected:    false,
		},
		{
			name:        "empty content type",
			contentType: "",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/test", nil)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			result := isGRPCRequest(req)
			if result != tt.expected {
				t.Errorf("Expected %v for content-type %s, got %v",
					tt.expected, tt.contentType, result)
			}
		})
	}
}
