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
	"regexp"
	"testing"

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
