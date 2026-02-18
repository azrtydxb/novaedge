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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
	"go.uber.org/zap"
)

// BenchmarkEndpointKey benchmarks the endpoint key generation helper
func BenchmarkEndpointKey(b *testing.B) {
	ep := &pb.Endpoint{
		Address: "10.0.0.1",
		Port:    8080,
	}

	b.Run("endpointKey", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = endpointKey(ep)
		}
	})

	b.Run("sprintf", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = fmt.Sprintf("%s:%d", ep.Address, ep.Port)
		}
	})
}

// BenchmarkPoolForward benchmarks the Forward method with a real backend
func BenchmarkPoolForward(b *testing.B) {
	logger := zap.NewNop()

	// Start a real test backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// Parse the backend address
	host := backend.Listener.Addr().String()
	// Extract IP and port
	var ip string
	var port int
	if _, err := fmt.Sscanf(host, "%[^:]:%d", &ip, &port); err != nil {
		b.Fatalf("failed to parse host %q: %v", host, err)
	}

	cluster := &pb.Cluster{
		Name:             "bench-forward-cluster",
		Namespace:        "bench",
		ConnectTimeoutMs: 5000,
	}

	endpoints := []*pb.Endpoint{
		{Address: ip, Port: int32(port), Ready: true}, //nolint:gosec // port is parsed from test server address, always valid
	}

	pool := NewPool(context.Background(), cluster, endpoints, logger)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		_ = pool.Forward(endpoints[0], req, w)
	}

	b.StopTimer()
	pool.Close()
}

// BenchmarkPoolUpdateEndpoints benchmarks endpoint updates (including connection draining)
func BenchmarkPoolUpdateEndpoints(b *testing.B) {
	logger := zap.NewNop()

	cluster := &pb.Cluster{
		Name:             "bench-update-cluster",
		Namespace:        "bench",
		ConnectTimeoutMs: 5000,
	}

	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	pool := NewPool(context.Background(), cluster, initialEndpoints, logger)

	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: true},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		pool.UpdateEndpoints(newEndpoints)
	}

	b.StopTimer()
	pool.Close()
}

// BenchmarkPoolGetStats benchmarks the GetStats method
func BenchmarkPoolGetStats(b *testing.B) {
	logger := zap.NewNop()

	cluster := &pb.Cluster{
		Name:             "bench-stats-cluster",
		Namespace:        "bench",
		ConnectTimeoutMs: 5000,
	}

	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	pool := NewPool(context.Background(), cluster, endpoints, logger)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = pool.GetStats()
	}

	b.StopTimer()
	pool.Close()
}
