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
	"crypto/rand"
	"fmt"
	"hash/fnv"
	"math/big"
	"regexp"
	"sort"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// RouteEntry represents a single route rule
type RouteEntry struct {
	Route          *pb.Route
	Rule           *pb.RouteRule
	PathMatcher    PathMatcher
	Policies       []policyMiddleware
	HeaderRegexes  map[int]*regexp.Regexp // Cached compiled header regex patterns (index -> regex)
	ResponseFilter *ResponseFilter        // Response header modifications
	Limits         *pb.RouteLimitsConfig  // Per-route request limits and timeouts
	Buffering      *pb.BufferingConfig    // Request/response buffering settings
}

// compileHeaderRegexes pre-compiles all header regex patterns for a route rule
// This prevents regex compilation on every request (performance optimization)
func compileHeaderRegexes(rule *pb.RouteRule) map[int]*regexp.Regexp {
	regexes := make(map[int]*regexp.Regexp)

	for matchIdx, match := range rule.Matches {
		for headerIdx, header := range match.Headers {
			if header.Type == pb.HeaderMatchType_HEADER_REGULAR_EXPRESSION {
				if regex, err := regexp.Compile(header.Value); err == nil {
					// Store with a unique key combining match and header index
					key := matchIdx*1000 + headerIdx
					regexes[key] = regex
				}
			}
		}
	}

	return regexes
}

// selectWeightedBackend selects a backend from multiple backends based on their weights
// Uses weighted random selection algorithm
func selectWeightedBackend(backends []*pb.BackendRef) *pb.BackendRef {
	if len(backends) == 0 {
		return nil
	}

	// If only one backend, return it directly
	if len(backends) == 1 {
		return backends[0]
	}

	// Calculate total weight
	totalWeight := int32(0)
	for _, backend := range backends {
		weight := backend.Weight
		if weight <= 0 {
			weight = 1 // Default weight
		}
		totalWeight += weight
	}

	// Generate random number between 0 and totalWeight using crypto/rand
	bigRand, err := rand.Int(rand.Reader, big.NewInt(int64(totalWeight)))
	if err != nil {
		// Fallback to first backend if crypto/rand fails
		return backends[0]
	}
	randVal := bigRand.Int64()

	// Select backend based on weight (use int64 arithmetic to avoid integer overflow)
	var currentWeight int64
	for _, backend := range backends {
		weight := int64(backend.Weight)
		if weight <= 0 {
			weight = 1 // Default weight
		}
		currentWeight += weight
		if randVal < currentWeight {
			return backend
		}
	}

	// Fallback to first backend (should never reach here)
	return backends[0]
}

// hashEndpointList computes a hash of the endpoint list and LB policy for versioning.
// This allows us to detect when endpoints or the load balancing policy change
// without storing the full list. Including the LB policy ensures that changing
// from e.g. ROUND_ROBIN to EWMA forces load balancer recreation even if
// the endpoint set is identical.
func hashEndpointList(endpoints []*pb.Endpoint, lbPolicy pb.LoadBalancingPolicy) uint64 {
	// Start with the LB policy so that a policy change alone triggers recreation
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "lb:%d;", int32(lbPolicy))

	if len(endpoints) == 0 {
		return h.Sum64()
	}

	// Sort endpoints by address:port for consistent hashing
	// Create a copy to avoid modifying the original slice
	sortedEndpoints := make([]*pb.Endpoint, len(endpoints))
	copy(sortedEndpoints, endpoints)
	sort.Slice(sortedEndpoints, func(i, j int) bool {
		if sortedEndpoints[i].Address != sortedEndpoints[j].Address {
			return sortedEndpoints[i].Address < sortedEndpoints[j].Address
		}
		return sortedEndpoints[i].Port < sortedEndpoints[j].Port
	})

	// Compute hash
	for _, ep := range sortedEndpoints {
		// Hash address, port, and ready state
		_, _ = fmt.Fprintf(h, "%s:%d:%t;", ep.Address, ep.Port, ep.Ready)
	}
	return h.Sum64()
}
