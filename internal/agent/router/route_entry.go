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
	"hash/fnv"
	"math/rand"
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

	// Generate random number between 0 and totalWeight
	randVal := rand.Int31n(totalWeight)

	// Select backend based on weight
	currentWeight := int32(0)
	for _, backend := range backends {
		weight := backend.Weight
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

// hashEndpointList computes a hash of the endpoint list for versioning
// This allows us to detect when endpoints change without storing the full list
func hashEndpointList(endpoints []*pb.Endpoint) uint64 {
	if len(endpoints) == 0 {
		return 0
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
	h := fnv.New64a()
	for _, ep := range sortedEndpoints {
		// Hash address, port, and ready state
		fmt.Fprintf(h, "%s:%d:%t;", ep.Address, ep.Port, ep.Ready)
	}
	return h.Sum64()
}
