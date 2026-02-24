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
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/config"
	"github.com/piwi3910/novaedge/internal/agent/lb"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestExactMatcher(t *testing.T) {
	matcher := &ExactMatcher{Path: "/api/v1/users"}

	tests := []struct {
		path     string
		expected bool
	}{
		{"/api/v1/users", true},
		{"/api/v1/users/", false},
		{"/api/v1", false},
		{"/api/v1/users/123", false},
	}

	for _, tt := range tests {
		result := matcher.Match(tt.path)
		if result != tt.expected {
			t.Errorf("ExactMatcher.Match(%q) = %v, want %v", tt.path, result, tt.expected)
		}
	}
}

func TestPrefixMatcher(t *testing.T) {
	matcher := &PrefixMatcher{Prefix: "/api/v1"}

	tests := []struct {
		path     string
		expected bool
	}{
		{"/api/v1", true},
		{"/api/v1/users", true},
		{"/api/v1/users/123", true},
		{"/api/v2", false},
		{"/api", false},
	}

	for _, tt := range tests {
		result := matcher.Match(tt.path)
		if result != tt.expected {
			t.Errorf("PrefixMatcher.Match(%q) = %v, want %v", tt.path, result, tt.expected)
		}
	}
}

func TestRegexMatcher(t *testing.T) {
	tests := []struct {
		pattern  string
		path     string
		expected bool
	}{
		{`^/api/v[0-9]+/users$`, "/api/v1/users", true},
		{`^/api/v[0-9]+/users$`, "/api/v2/users", true},
		{`^/api/v[0-9]+/users$`, "/api/v1/users/123", false},
		{`^/api/v[0-9]+/users$`, "/api/users", false},
		{`\.json$`, "/api/data.json", true},
		{`\.json$`, "/api/data.xml", false},
	}

	for _, tt := range tests {
		matcher, err := NewRegexMatcher(tt.pattern)
		if err != nil {
			t.Fatalf("Failed to create regex matcher: %v", err)
		}

		result := matcher.Match(tt.path)
		if result != tt.expected {
			t.Errorf("RegexMatcher(%q).Match(%q) = %v, want %v",
				tt.pattern, tt.path, result, tt.expected)
		}
	}
}

func TestCreatePathMatcher(t *testing.T) {
	tests := []struct {
		name        string
		matchType   pb.PathMatchType
		value       string
		testPath    string
		shouldMatch bool
	}{
		{
			name:        "exact match success",
			matchType:   pb.PathMatchType_EXACT,
			value:       "/api/users",
			testPath:    "/api/users",
			shouldMatch: true,
		},
		{
			name:        "exact match fail",
			matchType:   pb.PathMatchType_EXACT,
			value:       "/api/users",
			testPath:    "/api/users/123",
			shouldMatch: false,
		},
		{
			name:        "prefix match success",
			matchType:   pb.PathMatchType_PATH_PREFIX,
			value:       "/api",
			testPath:    "/api/users",
			shouldMatch: true,
		},
		{
			name:        "prefix match fail",
			matchType:   pb.PathMatchType_PATH_PREFIX,
			value:       "/api",
			testPath:    "/v2/users",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &pb.RouteRule{
				Matches: []*pb.RouteMatch{
					{
						Path: &pb.PathMatch{
							Type:  tt.matchType,
							Value: tt.value,
						},
					},
				},
			}

			matcher := createPathMatcher(rule)
			if matcher == nil {
				t.Fatal("createPathMatcher returned nil")
			}

			result := matcher.Match(tt.testPath)
			if result != tt.shouldMatch {
				t.Errorf("Match(%q) = %v, want %v", tt.testPath, result, tt.shouldMatch)
			}
		})
	}
}

func NewRegexMatcher(pattern string) (*RegexMatcher, error) {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &RegexMatcher{Pattern: regex}, nil
}

func TestApplyConfigCatchAllRoute(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	r := NewRouter(logger)

	// Create a snapshot with a route that has NO hostnames (catch-all)
	snapshot := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes: []*pb.Route{
				{
					Name:      "catch-all",
					Namespace: "default",
					Hostnames: nil, // empty hostnames = catch-all
					Rules: []*pb.RouteRule{
						{
							Matches: []*pb.RouteMatch{
								{
									Path: &pb.PathMatch{
										Type:  pb.PathMatchType_PATH_PREFIX,
										Value: "/",
									},
								},
							},
							BackendRefs: []*pb.BackendRef{
								{Namespace: "default", Name: "backend1"},
							},
						},
					},
				},
			},
			Clusters:  []*pb.Cluster{},
			Endpoints: map[string]*pb.EndpointList{},
			Gateways:  []*pb.Gateway{},
		},
	}

	err := r.ApplyConfig(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	// Verify that the catch-all route is stored under the empty string key
	snap := r.state.Load()

	if _, ok := snap.routeIndexes[""]; !ok {
		t.Fatal("Expected catch-all route index for empty hostname key")
	}

	if _, ok := snap.routes[""]; !ok {
		t.Fatal("Expected catch-all routes for empty hostname key")
	}

	if len(snap.routes[""]) != 1 {
		t.Fatalf("Expected 1 catch-all route, got %d", len(snap.routes[""]))
	}
}

func TestServeHTTPCatchAllFallback(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	r := NewRouter(logger)

	// Set up a catch-all route under the empty hostname key
	entry := &RouteEntry{
		Route: &pb.Route{
			Name:      "catch-all",
			Namespace: "default",
		},
		Rule: &pb.RouteRule{
			Matches: []*pb.RouteMatch{
				{
					Path: &pb.PathMatch{
						Type:  pb.PathMatchType_PATH_PREFIX,
						Value: "/",
					},
				},
			},
		},
		PathMatcher: &PrefixMatcher{Prefix: "/"},
	}

	// Atomically swap in a new state with the catch-all route
	old := r.state.Load()
	updated := *old
	updated.routes = map[string][]*RouteEntry{
		"": {entry},
	}
	updated.routeIndexes = map[string]*routeIndex{
		"": newRouteIndex(updated.routes[""]),
	}
	r.state.Store(&updated)

	// Make a request with a specific hostname that has no route
	req := httptest.NewRequest("GET", "http://unknown-host.example.com/api/test", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Should NOT get 404 "No route found" since catch-all exists.
	// It may get 404 "No matching route rule" or other errors due to missing backends,
	// but the key assertion is that the catch-all route was tried.
	body := w.Body.String()
	if w.Code == http.StatusNotFound && body == "No route found\n" {
		t.Error("Expected catch-all route to handle request, but got 'No route found'")
	}
}

func TestApplyConfigPreservesLBStateOnEndpointUpdate(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	r := NewRouter(logger)

	// Create initial snapshot with a cluster and endpoints
	snapshot1 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes: []*pb.Route{
				{
					Name:      "test-route",
					Namespace: "default",
					Hostnames: []string{"example.com"},
					Rules: []*pb.RouteRule{
						{
							Matches: []*pb.RouteMatch{
								{Path: &pb.PathMatch{Type: pb.PathMatchType_PATH_PREFIX, Value: "/"}},
							},
							BackendRefs: []*pb.BackendRef{
								{Namespace: "default", Name: "backend1"},
							},
						},
					},
				},
			},
			Clusters: []*pb.Cluster{
				{
					Name:      "backend1",
					Namespace: "default",
					LbPolicy:  pb.LoadBalancingPolicy_ROUND_ROBIN,
				},
			},
			Endpoints: map[string]*pb.EndpointList{
				"default/backend1": {
					Endpoints: []*pb.Endpoint{
						{Address: "10.0.0.1", Port: 8080, Ready: true},
						{Address: "10.0.0.2", Port: 8080, Ready: true},
					},
				},
			},
			Gateways: []*pb.Gateway{},
		},
	}

	err := r.ApplyConfig(context.Background(), snapshot1)
	if err != nil {
		t.Fatalf("First ApplyConfig failed: %v", err)
	}

	// Capture the LB instance
	snap1 := r.state.Load()
	originalLB := snap1.loadBalancers["default/backend1"]
	if originalLB == nil {
		t.Fatal("Expected load balancer for default/backend1")
	}
	originalPool := snap1.pools["default/backend1"]
	if originalPool == nil {
		t.Fatal("Expected pool for default/backend1")
	}

	// Apply second snapshot with different endpoints but same LB policy
	snapshot2 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes:   snapshot1.Routes,
			Clusters: snapshot1.Clusters,
			Endpoints: map[string]*pb.EndpointList{
				"default/backend1": {
					Endpoints: []*pb.Endpoint{
						{Address: "10.0.0.1", Port: 8080, Ready: true},
						{Address: "10.0.0.3", Port: 8080, Ready: true}, // Changed endpoint
					},
				},
			},
			Gateways: []*pb.Gateway{},
		},
	}

	err = r.ApplyConfig(context.Background(), snapshot2)
	if err != nil {
		t.Fatalf("Second ApplyConfig failed: %v", err)
	}

	// The LB should be the SAME instance (in-place update, not recreated)
	snap2 := r.state.Load()
	updatedLB := snap2.loadBalancers["default/backend1"]
	if updatedLB == nil {
		t.Fatal("Expected load balancer for default/backend1 after update")
	}
	if updatedLB != originalLB {
		t.Error("Expected load balancer to be preserved (same instance) for endpoint-only change")
	}

	// The pool should also be reused
	updatedPool := snap2.pools["default/backend1"]
	if updatedPool != originalPool {
		t.Error("Expected pool to be reused (same instance)")
	}
}

// TestApplyConfigUpdatesHashBasedLBEndpoints is a regression test for the bug
// where hash-based load balancers (Maglev, RingHash) were not updated when
// endpoints changed. The root cause was that updateExistingLoadBalancer used a
// type assertion to lb.LoadBalancer, but Maglev.Select(key string) and
// RingHash.Select(key string) take a key argument and do NOT satisfy that
// interface. The fix adds explicit type cases for *lb.Maglev and *lb.RingHash.
func TestApplyConfigUpdatesHashBasedLBEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lbPolicy pb.LoadBalancingPolicy
	}{
		{"Maglev", pb.LoadBalancingPolicy_MAGLEV},
		{"RingHash", pb.LoadBalancingPolicy_RING_HASH},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger, _ := zap.NewDevelopment()
			r := NewRouter(logger)

			// First snapshot: cluster exists but 0 endpoints (simulates pods not ready)
			snapshot1 := &config.Snapshot{
				ConfigSnapshot: &pb.ConfigSnapshot{
					Routes: []*pb.Route{
						{
							Name:      "test-route",
							Namespace: "default",
							Hostnames: []string{"example.com"},
							Rules: []*pb.RouteRule{
								{
									Matches: []*pb.RouteMatch{
										{Path: &pb.PathMatch{Type: pb.PathMatchType_PATH_PREFIX, Value: "/"}},
									},
									BackendRefs: []*pb.BackendRef{
										{Namespace: "default", Name: "backend1"},
									},
								},
							},
						},
					},
					Clusters: []*pb.Cluster{
						{
							Name:      "backend1",
							Namespace: "default",
							LbPolicy:  tc.lbPolicy,
						},
					},
					Endpoints: map[string]*pb.EndpointList{
						"default/backend1": {
							Endpoints: []*pb.Endpoint{}, // empty — pods not ready
						},
					},
					Gateways: []*pb.Gateway{},
				},
			}

			err := r.ApplyConfig(context.Background(), snapshot1)
			if err != nil {
				t.Fatalf("First ApplyConfig failed: %v", err)
			}

			// Verify hash-based LB was created with 0 endpoints
			snap1 := r.state.Load()
			hashLB1 := snap1.hashBasedLBs["default/backend1"]
			if hashLB1 == nil {
				t.Fatal("Expected hash-based LB for default/backend1 after first apply")
			}

			// Second snapshot: same cluster, now with 3 endpoints (pods became ready)
			snapshot2 := &config.Snapshot{
				ConfigSnapshot: &pb.ConfigSnapshot{
					Routes:   snapshot1.Routes,
					Clusters: snapshot1.Clusters,
					Endpoints: map[string]*pb.EndpointList{
						"default/backend1": {
							Endpoints: []*pb.Endpoint{
								{Address: "10.0.0.1", Port: 8080, Ready: true},
								{Address: "10.0.0.2", Port: 8080, Ready: true},
								{Address: "10.0.0.3", Port: 8080, Ready: true},
							},
						},
					},
					Gateways: []*pb.Gateway{},
				},
			}

			err = r.ApplyConfig(context.Background(), snapshot2)
			if err != nil {
				t.Fatalf("Second ApplyConfig failed: %v", err)
			}

			// The hash-based LB should be the SAME instance (in-place update)
			snap2 := r.state.Load()
			hashLB2 := snap2.hashBasedLBs["default/backend1"]
			if hashLB2 == nil {
				t.Fatal("Expected hash-based LB after second apply")
			}
			if hashLB2 != hashLB1 {
				t.Error("Expected hash-based LB to be preserved (same instance)")
			}

			// The LB must now return endpoints (this is the actual regression check)
			switch hlb := hashLB2.(type) {
			case *lb.Maglev:
				ep := hlb.Select("test-key")
				if ep == nil {
					t.Error("Maglev.Select returned nil after endpoint update — LB was not updated")
				}
			case *lb.RingHash:
				ep := hlb.Select("test-key")
				if ep == nil {
					t.Error("RingHash.Select returned nil after endpoint update — LB was not updated")
				}
			}
		})
	}
}

func TestApplyConfigRecreatesLBOnPolicyChange(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	r := NewRouter(logger)

	// Create initial snapshot with ROUND_ROBIN
	snapshot1 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes: []*pb.Route{
				{
					Name:      "test-route",
					Namespace: "default",
					Hostnames: []string{"example.com"},
					Rules: []*pb.RouteRule{
						{
							Matches: []*pb.RouteMatch{
								{Path: &pb.PathMatch{Type: pb.PathMatchType_PATH_PREFIX, Value: "/"}},
							},
							BackendRefs: []*pb.BackendRef{
								{Namespace: "default", Name: "backend1"},
							},
						},
					},
				},
			},
			Clusters: []*pb.Cluster{
				{
					Name:      "backend1",
					Namespace: "default",
					LbPolicy:  pb.LoadBalancingPolicy_ROUND_ROBIN,
				},
			},
			Endpoints: map[string]*pb.EndpointList{
				"default/backend1": {
					Endpoints: []*pb.Endpoint{
						{Address: "10.0.0.1", Port: 8080, Ready: true},
						{Address: "10.0.0.2", Port: 8080, Ready: true},
					},
				},
			},
			Gateways: []*pb.Gateway{},
		},
	}

	err := r.ApplyConfig(context.Background(), snapshot1)
	if err != nil {
		t.Fatalf("First ApplyConfig failed: %v", err)
	}

	snap1 := r.state.Load()
	originalLB := snap1.loadBalancers["default/backend1"]
	if originalLB == nil {
		t.Fatal("Expected load balancer for default/backend1")
	}

	// Apply second snapshot: change LB policy from ROUND_ROBIN to LEAST_CONN
	snapshot2 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes: snapshot1.Routes,
			Clusters: []*pb.Cluster{
				{
					Name:      "backend1",
					Namespace: "default",
					LbPolicy:  pb.LoadBalancingPolicy_LEAST_CONN, // Changed!
				},
			},
			Endpoints: snapshot1.Endpoints,
			Gateways:  []*pb.Gateway{},
		},
	}

	err = r.ApplyConfig(context.Background(), snapshot2)
	if err != nil {
		t.Fatalf("Second ApplyConfig failed: %v", err)
	}

	// The LB should be a DIFFERENT instance (policy changed = full recreate)
	snap2 := r.state.Load()
	newLB := snap2.loadBalancers["default/backend1"]
	if newLB == nil {
		t.Fatal("Expected load balancer for default/backend1 after policy change")
	}
	if newLB == originalLB {
		t.Error("Expected load balancer to be recreated (different instance) after LB policy change")
	}
}

func TestHashEndpointListIncludesLBPolicy(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	hashRR := hashEndpointList(endpoints, pb.LoadBalancingPolicy_ROUND_ROBIN)
	hashEWMA := hashEndpointList(endpoints, pb.LoadBalancingPolicy_EWMA)
	hashP2C := hashEndpointList(endpoints, pb.LoadBalancingPolicy_P2C)

	// Same endpoints with different LB policies must produce different hashes
	if hashRR == hashEWMA {
		t.Error("Expected different hashes for ROUND_ROBIN vs EWMA with same endpoints")
	}
	if hashRR == hashP2C {
		t.Error("Expected different hashes for ROUND_ROBIN vs P2C with same endpoints")
	}
	if hashEWMA == hashP2C {
		t.Error("Expected different hashes for EWMA vs P2C with same endpoints")
	}

	// Same endpoints and same policy must produce the same hash
	hashRR2 := hashEndpointList(endpoints, pb.LoadBalancingPolicy_ROUND_ROBIN)
	if hashRR != hashRR2 {
		t.Error("Expected same hash for identical endpoints and policy")
	}
}

func TestHashEndpointListDeterministic(t *testing.T) {
	// Endpoints in different order should produce the same hash (sorted internally)
	endpoints1 := []*pb.Endpoint{
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}
	endpoints2 := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	hash1 := hashEndpointList(endpoints1, pb.LoadBalancingPolicy_ROUND_ROBIN)
	hash2 := hashEndpointList(endpoints2, pb.LoadBalancingPolicy_ROUND_ROBIN)

	if hash1 != hash2 {
		t.Error("Expected same hash regardless of endpoint order")
	}
}

func TestHashEndpointListEmpty(t *testing.T) {
	hash := hashEndpointList(nil, pb.LoadBalancingPolicy_ROUND_ROBIN)
	if hash == 0 {
		t.Error("Expected non-zero hash even for empty endpoints (LB policy is hashed)")
	}

	// Different policies with empty endpoints should differ
	hashEWMA := hashEndpointList(nil, pb.LoadBalancingPolicy_EWMA)
	if hash == hashEWMA {
		t.Error("Expected different hashes for different policies even with empty endpoints")
	}
}
