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

// Package agent defines core interfaces for NovaEdge agent components.
//
// These interfaces enable:
//   - Better testability through mocking
//   - Loose coupling between components
//   - Easier unit testing with fake implementations
//   - Clear contracts between subsystems
//
// Usage:
//
//	// Production code uses concrete implementations
//	forwarder := upstream.NewPool(ctx, cluster, endpoints, logger)
//
//	// Test code can use mocks
//	forwarder := &MockForwarder{...}
package agent

import (
	"context"
	"net/http"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Forwarder represents a component that can forward HTTP requests to backend endpoints
// This is the primary interface for upstream connection pools
type Forwarder interface {
	// Forward forwards an HTTP request to the specified endpoint
	Forward(endpoint *pb.Endpoint, req *http.Request, w http.ResponseWriter) error

	// GetHealthyEndpoints returns only healthy endpoints
	GetHealthyEndpoints() []*pb.Endpoint

	// RecordSuccess records a successful request to an endpoint (for passive health checking)
	RecordSuccess(endpoint *pb.Endpoint)

	// RecordFailure records a failed request to an endpoint (for passive health checking)
	RecordFailure(endpoint *pb.Endpoint)

	// GetBackendURL returns the backend URL for a given endpoint
	GetBackendURL(endpoint *pb.Endpoint) string

	// UpdateEndpoints updates the pool with new endpoints
	UpdateEndpoints(endpoints []*pb.Endpoint)

	// GetStats returns pool statistics
	GetStats() map[string]interface{}

	// Close closes the forwarder and all connections
	Close()
}

// HealthChecker represents a component that performs health checks on backend endpoints
type HealthChecker interface {
	// Start starts the health checker with the given context
	Start(ctx context.Context)

	// Stop stops the health checker
	Stop()

	// IsHealthy returns true if an endpoint is healthy
	IsHealthy(endpoint *pb.Endpoint) bool

	// RecordSuccess records a successful request (for passive health checking)
	RecordSuccess(endpoint *pb.Endpoint)

	// RecordFailure records a failed request (for passive health checking)
	RecordFailure(endpoint *pb.Endpoint)

	// GetHealthyEndpoints returns only healthy endpoints
	GetHealthyEndpoints() []*pb.Endpoint

	// UpdateEndpoints updates the list of endpoints to check
	UpdateEndpoints(endpoints []*pb.Endpoint)
}

// LoadBalancer represents a component that selects backend endpoints using various algorithms
type LoadBalancer interface {
	// PickEndpoint selects an endpoint from the list using the load balancing algorithm
	// Returns nil if no suitable endpoint is available
	PickEndpoint(endpoints []*pb.Endpoint) *pb.Endpoint

	// RecordRequest records request completion for latency-aware algorithms
	// Used by P2C, EWMA, and other algorithms that track endpoint performance
	RecordRequest(endpoint *pb.Endpoint, latency float64)
}

// VIPManager represents a component that manages Virtual IP addresses
type VIPManager interface {
	// Start starts the VIP manager with the given context
	Start(ctx context.Context) error

	// Stop stops the VIP manager and releases VIPs
	Stop() error

	// ApplyVIPs applies VIP assignments to the node
	ApplyVIPs(vips []*pb.VIPAssignment) error

	// GetActiveVIPs returns the list of currently active VIPs on this node
	GetActiveVIPs() []string

	// GetStats returns VIP manager statistics
	GetStats() map[string]interface{}
}

// FilterChain represents a chain of request/response filters
type FilterChain interface {
	// Apply applies all filters in the chain to the request
	// Returns true if the request should continue, false if it should be rejected
	Apply(w http.ResponseWriter, r *http.Request) bool
}

// Filter represents a single request/response filter
type Filter interface {
	// Process processes the request and returns true if it should continue
	// The filter can modify the request, write to the response, or both
	Process(w http.ResponseWriter, r *http.Request) bool

	// Name returns the filter name for logging and debugging
	Name() string
}

// Router represents the main request routing component
type Router interface {
	// ServeHTTP handles HTTP requests (implements http.Handler)
	ServeHTTP(w http.ResponseWriter, r *http.Request)

	// ApplyConfig applies a new configuration snapshot
	ApplyConfig(snapshot interface{}) error
}

// ConfigWatcher represents a component that watches for configuration updates
type ConfigWatcher interface {
	// Start begins watching for config updates and calls applyFunc when updates arrive
	Start(applyFunc func(interface{}) error) error
}

// MetricsCollector represents a component that collects and exposes metrics
type MetricsCollector interface {
	// RecordRequest records request metrics
	RecordRequest(method, path string, status int, duration float64)

	// RecordHealthCheck records health check metrics
	RecordHealthCheck(cluster, endpoint, result string, duration float64)

	// SetBackendHealth sets the health status of a backend
	SetBackendHealth(cluster, endpoint string, healthy bool)

	// RecordVIPFailover records a VIP failover event
	RecordVIPFailover(vip, fromNode, toNode string)
}

// CircuitBreaker represents a circuit breaker for endpoint protection
type CircuitBreaker interface {
	// IsOpen returns true if the circuit breaker is open (endpoint unavailable)
	IsOpen() bool

	// RecordSuccess records a successful request
	RecordSuccess()

	// RecordFailure records a failed request
	RecordFailure()

	// State returns the current circuit breaker state
	State() string

	// Reset manually resets the circuit breaker to closed state
	Reset()
}
