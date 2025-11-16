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

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/config"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// BenchmarkRouteMatching tests the performance of route matching
func BenchmarkRouteMatching(b *testing.B) {
	logger := zap.NewNop()
	router := NewRouter(logger)

	// Create test configuration
	snapshot := &config.Snapshot{
		Routes: []*pb.Route{
			{
				Name:      "test-route",
				Namespace: "default",
				Hostnames: []string{"example.com"},
				Rules: []*pb.RouteRule{
					{
						Matches: []*pb.RouteMatch{
							{
								Path: &pb.PathMatch{
									Type:  pb.PathMatchType_PATH_PREFIX,
									Value: "/api",
								},
								Method: "GET",
							},
						},
						BackendRefs: []*pb.BackendRef{
							{
								Name:      "backend",
								Namespace: "default",
								Weight:    100,
							},
						},
					},
				},
			},
		},
		Clusters: []*pb.Cluster{
			{
				Name:             "backend",
				Namespace:        "default",
				LbPolicy:         pb.LoadBalancingPolicy_ROUND_ROBIN,
				ConnectTimeoutMs: 5000,
				IdleTimeoutMs:    90000,
			},
		},
		Endpoints: map[string]*pb.EndpointList{
			"default/backend": {
				Endpoints: []*pb.Endpoint{
					{Address: "10.0.0.1", Port: 8080, Ready: true},
					{Address: "10.0.0.2", Port: 8080, Ready: true},
				},
			},
		},
	}

	router.ApplyConfig(snapshot)

	req := httptest.NewRequest("GET", "http://example.com/api/users", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		router.mu.RLock()
		routes := router.routes["example.com"]
		if routes != nil {
			for _, entry := range routes {
				router.matchRoute(entry, req)
			}
		}
		router.mu.RUnlock()
	}
}

// BenchmarkEndpointHashing tests the performance of endpoint hashing
func BenchmarkEndpointHashing(b *testing.B) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: true},
		{Address: "10.0.0.5", Port: 8080, Ready: true},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		hashEndpointList(endpoints)
	}
}

// BenchmarkResponseWriterPool tests the performance of response writer pooling
func BenchmarkResponseWriterPool(b *testing.B) {
	w := httptest.NewRecorder()

	b.Run("WithPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			rw := getResponseWriter(w)
			rw.WriteHeader(http.StatusOK)
			putResponseWriter(rw)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			rw := &responseWriterWithStatus{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}
			rw.WriteHeader(http.StatusOK)
		}
	})
}

// BenchmarkLoadBalancerSelection tests LB selection performance
func BenchmarkLoadBalancerSelection(b *testing.B) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: true},
		{Address: "10.0.0.5", Port: 8080, Ready: true},
	}

	b.Run("WeightedBackend", func(b *testing.B) {
		backends := []*pb.BackendRef{
			{Name: "backend1", Namespace: "default", Weight: 50},
			{Name: "backend2", Namespace: "default", Weight: 30},
			{Name: "backend3", Namespace: "default", Weight: 20},
		}

		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			selectWeightedBackend(backends)
		}
	})

	b.Run("EndpointHash", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			hashEndpointList(endpoints)
		}
	})
}

// BenchmarkConfigApply tests the performance of applying configuration
func BenchmarkConfigApply(b *testing.B) {
	logger := zap.NewNop()

	// Create a moderately complex configuration
	snapshot := &config.Snapshot{
		Routes: []*pb.Route{
			{
				Name:      "route1",
				Namespace: "default",
				Hostnames: []string{"example.com", "www.example.com"},
				Rules: []*pb.RouteRule{
					{
						Matches: []*pb.RouteMatch{
							{
								Path: &pb.PathMatch{
									Type:  pb.PathMatchType_PATH_PREFIX,
									Value: "/api",
								},
							},
						},
						BackendRefs: []*pb.BackendRef{
							{Name: "backend1", Namespace: "default", Weight: 100},
						},
					},
				},
			},
		},
		Clusters: []*pb.Cluster{
			{
				Name:             "backend1",
				Namespace:        "default",
				LbPolicy:         pb.LoadBalancingPolicy_ROUND_ROBIN,
				ConnectTimeoutMs: 5000,
				IdleTimeoutMs:    90000,
			},
		},
		Endpoints: map[string]*pb.EndpointList{
			"default/backend1": {
				Endpoints: []*pb.Endpoint{
					{Address: "10.0.0.1", Port: 8080, Ready: true},
					{Address: "10.0.0.2", Port: 8080, Ready: true},
					{Address: "10.0.0.3", Port: 8080, Ready: true},
				},
			},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		router := NewRouter(logger)
		router.ApplyConfig(snapshot)
	}
}
