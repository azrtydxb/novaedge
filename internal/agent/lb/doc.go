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

// Package lb implements various load balancing algorithms for distributing traffic
// across backend endpoints.
//
// Available Load Balancing Algorithms:
//
// # Round Robin
//
// The simplest algorithm that distributes requests evenly across all healthy endpoints
// in a circular fashion. Each endpoint receives an equal share of traffic over time.
//
// Use cases:
//   - Homogeneous backends with similar capacity
//   - Stateless applications
//   - Simple, predictable traffic distribution
//
// Example:
//
//	endpoints := []*pb.Endpoint{
//	    {Address: "10.0.0.1", Port: 8080, Ready: true},
//	    {Address: "10.0.0.2", Port: 8080, Ready: true},
//	}
//	lb := lb.NewRoundRobin(endpoints)
//	endpoint := lb.Select() // Returns endpoints in rotation
//
// # Power of Two Choices (P2C)
//
// Selects the best of two randomly chosen endpoints based on active request count.
// This provides better load distribution than pure random while maintaining low overhead.
//
// Use cases:
//   - Backends with varying response times
//   - Applications with concurrent request handling
//   - Better than round robin for heterogeneous backends
//
// Example:
//
//	lb := lb.NewP2C(endpoints)
//	endpoint := lb.Select()
//	lb.IncrementActive(endpoint)
//	defer lb.DecrementActive(endpoint)
//
// # Exponentially Weighted Moving Average (EWMA)
//
// Latency-aware load balancing that tracks weighted average response times for each endpoint.
// Endpoints with lower latency receive more traffic.
//
// Use cases:
//   - Backends with significantly different performance characteristics
//   - Multi-zone deployments where latency varies
//   - Performance-sensitive applications
//
// Example:
//
//	lb := lb.NewEWMA(endpoints)
//	endpoint := lb.Select()
//	start := time.Now()
//	// ... make request ...
//	lb.RecordLatency(endpoint, time.Since(start))
//
// # Ring Hash
//
// Consistent hashing using a ring structure. Provides session affinity by mapping
// requests to the same endpoint based on a hash key (typically client IP).
//
// Use cases:
//   - Session-aware applications
//   - Caching backends (minimize cache misses on rebalancing)
//   - Applications requiring sticky sessions
//
// Example:
//
//	lb := lb.NewRingHash(endpoints)
//	endpoint := lb.Select(clientIP) // Same client always goes to same endpoint
//
// # Maglev
//
// Google's Maglev consistent hashing algorithm, optimized for minimal disruption
// when endpoints change while maintaining even distribution.
//
// Use cases:
//   - Same as Ring Hash but with better distribution
//   - Large-scale deployments
//   - Frequent endpoint changes
//
// Example:
//
//	lb := lb.NewMaglev(endpoints)
//	endpoint := lb.Select(clientIP)
//
// # Choosing an Algorithm
//
// Selection criteria:
//   - Use Round Robin for simple, equal distribution
//   - Use P2C when backends have similar but variable capacity
//   - Use EWMA when latency-awareness is critical
//   - Use Ring Hash or Maglev when session affinity is required
//
// All algorithms automatically filter out unhealthy endpoints and handle
// endpoint additions/removals gracefully.
package lb
