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

// Package router implements HTTP request routing and forwarding to backend services.
//
// The router is the core of the NovaEdge data plane, responsible for:
//   - Matching incoming requests against configured routes
//   - Applying filters and policies
//   - Selecting backend endpoints via load balancing
//   - Forwarding requests to backends (HTTP/1.1, HTTP/2, WebSocket, gRPC)
//   - Tracking metrics and observability
//
// # Request Flow
//
// 1. Request arrives at Router.ServeHTTP
// 2. Hostname extraction and route lookup
// 3. Route rule matching (path, method, headers)
// 4. Policy middleware execution (rate limiting, CORS, JWT, etc.)
// 5. Filter application (header modifications, redirects, rewrites)
// 6. Backend selection via load balancer
// 7. Request forwarding to selected endpoint
// 8. Response proxying back to client
//
// # Route Matching
//
// Routes are organized by hostname for fast lookup. Each hostname can have
// multiple route rules that are evaluated in order. A route matches when:
//   - Path matches (exact, prefix, or regex)
//   - HTTP method matches (if specified)
//   - All header conditions match (if specified)
//
// Example route matching:
//
//	routes["api.example.com"] = []*RouteEntry{
//	    {PathMatch: "/v1/users", Method: "GET", ...},
//	    {PathMatch: "/v1/", Method: "", ...}, // prefix match, any method
//	}
//
// # Protocol Support
//
// The router handles multiple protocols:
//
// HTTP/1.1 and HTTP/2:
//   - Standard HTTP request/response proxying
//   - Header manipulation and forwarding
//   - Connection pooling and keepalive
//
// WebSocket:
//   - Automatic protocol upgrade detection
//   - Bidirectional message proxying
//   - Origin validation
//   - Graceful connection closure
//
// gRPC:
//   - HTTP/2-based RPC proxying
//   - Streaming support (unary, server-stream, client-stream, bidi)
//   - Header preservation (grpc-*, content-type)
//   - Error propagation
//
// # Load Balancing Integration
//
// The router integrates with multiple load balancing algorithms:
//   - Round Robin: Simple rotation
//   - P2C: Power of Two Choices
//   - EWMA: Latency-aware selection
//   - Ring Hash: Consistent hashing for session affinity
//   - Maglev: Google's consistent hashing
//
// Load balancer selection is configured per backend cluster.
//
// # Policy Enforcement
//
// Policies are applied as middleware in the request processing chain:
//   - Rate Limiting: Token bucket algorithm per client
//   - CORS: Cross-origin resource sharing headers
//   - IP Allow/Deny Lists: CIDR-based filtering
//   - JWT Validation: Token verification and claims extraction
//
// Policies are attached to routes via TargetRef and executed in order.
//
// # Performance Optimizations
//
// Several optimizations ensure high throughput:
//   - Regex compilation is cached at config time
//   - Connection pools are maintained per backend
//   - Load balancer state is reused across requests
//   - Metrics use atomic operations to avoid locks
//   - Read locks for request handling, write locks only for config updates
//
// # Observability
//
// The router exports comprehensive metrics:
//   - Request count, duration, and status codes
//   - Backend selection and health
//   - In-flight request tracking
//   - Error rates and types
//
// All metrics are exported via Prometheus format.
//
// # Thread Safety
//
// The router is safe for concurrent use:
//   - Configuration updates use write locks
//   - Request handling uses read locks
//   - Load balancers handle concurrency internally
//   - Connection pools are thread-safe
//
// Configuration updates are atomic - either fully applied or rolled back.
//
// DEPRECATED: This package will be removed once --forwarding-plane=rust is
// validated and the Rust dataplane handles all L7 routing natively.
// See docs/plans/forwarding-deprecation.md for the removal timeline.
package router
