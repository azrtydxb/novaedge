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
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
	"go.uber.org/zap"
)

// BenchmarkRouterServeHTTP benchmarks the full ServeHTTP hot path including
// route matching, response writer pooling, and metrics recording.
// The route has no backend pool, so this measures routing overhead (match + 404).
func BenchmarkRouterServeHTTP(b *testing.B) {
	logger := zap.NewNop()

	router := NewRouter(logger)

	// Set up minimal routing configuration directly (bypassing ApplyConfig
	// to avoid needing a full config.Snapshot with pools and LBs).
	// Atomically swap in a state snapshot with the test route.
	old := router.state.Load()
	updated := *old
	updated.routes = map[string][]*RouteEntry{
		"example.com": {
			{
				Route: &pb.Route{
					Name:      "test-route",
					Namespace: "default",
					Hostnames: []string{"example.com"},
				},
				Rule: &pb.RouteRule{
					Matches: []*pb.RouteMatch{
						{
							Path: &pb.PathMatch{
								Type:  pb.PathMatchType_PATH_PREFIX,
								Value: "/api",
							},
						},
					},
					BackendRefs: []*pb.BackendRef{
						{
							Name:      "test-backend",
							Namespace: "default",
							Weight:    1,
						},
					},
				},
				PathMatcher:   &PrefixMatcher{Prefix: "/api"},
				HeaderRegexes: make(map[int]*regexp.Regexp),
			},
		},
	}
	updated.routeIndexes = map[string]*routeIndex{
		"example.com": newRouteIndex(updated.routes["example.com"]),
	}
	router.state.Store(&updated)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/users", nil)
	req.Host = "example.com"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// BenchmarkRouteMatching benchmarks only the route matching logic
func BenchmarkRouteMatching(b *testing.B) {
	logger := zap.NewNop()
	router := NewRouter(logger)

	entry := &RouteEntry{
		Route: &pb.Route{
			Name:      "test-route",
			Namespace: "default",
		},
		Rule: &pb.RouteRule{
			Matches: []*pb.RouteMatch{
				{
					Path: &pb.PathMatch{
						Type:  pb.PathMatchType_PATH_PREFIX,
						Value: "/api",
					},
					Method: "GET",
				},
			},
		},
		PathMatcher:   &PrefixMatcher{Prefix: "/api"},
		HeaderRegexes: make(map[int]*regexp.Regexp),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		router.matchRoute(entry, req)
	}
}

// BenchmarkResponseWriterPool benchmarks the sync.Pool for response writers
func BenchmarkResponseWriterPool(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		rw := getResponseWriter(w)
		rw.WriteHeader(http.StatusOK)
		putResponseWriter(rw)
	}
}

// BenchmarkFormatEndpointKey benchmarks the pooled endpoint key formatting
// vs the naive fmt.Sprintf approach
func BenchmarkFormatEndpointKey(b *testing.B) {
	b.Run("pooled", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = formatEndpointKey("10.0.0.1", 8080)
		}
	})

	b.Run("sprintf", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = fmt.Sprintf("%s:%d", "10.0.0.1", 8080)
		}
	})
}

// BenchmarkHashEndpointList benchmarks endpoint list hashing for change detection
func BenchmarkHashEndpointList(b *testing.B) {
	endpoints := make([]*pb.Endpoint, 100)
	for i := range endpoints {
		endpoints[i] = &pb.Endpoint{
			Address: fmt.Sprintf("10.0.%d.%d", i/256, i%256),
			Port:    int32(8080) + int32(i), //nolint:gosec // i is bounded by loop range [0,100)
			Ready:   true,
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = hashEndpointList(endpoints, pb.LoadBalancingPolicy_ROUND_ROBIN)
	}
}

// BenchmarkSelectWeightedBackend benchmarks weighted backend selection
func BenchmarkSelectWeightedBackend(b *testing.B) {
	backends := []*pb.BackendRef{
		{Name: "backend-1", Namespace: "default", Weight: 3},
		{Name: "backend-2", Namespace: "default", Weight: 2},
		{Name: "backend-3", Namespace: "default", Weight: 1},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = selectWeightedBackend(backends)
	}
}
