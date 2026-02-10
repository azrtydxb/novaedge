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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Backend Metrics

	// BackendRequestsTotal tracks requests sent to backends
	BackendRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_backend_requests_total",
			Help: "Total number of requests sent to backends",
		},
		[]string{"cluster", "endpoint", "result"}, // result: success, failure
	)

	// BackendResponseDuration tracks backend response time
	BackendResponseDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "novaedge_backend_response_duration_seconds",
			Help:    "Backend response duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"cluster", "endpoint"},
	)

	// BackendHealthStatus tracks backend health (1=healthy, 0=unhealthy)
	BackendHealthStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_backend_health_status",
			Help: "Backend health status (1=healthy, 0=unhealthy)",
		},
		[]string{"cluster", "endpoint"},
	)

	// BackendActiveConnections tracks active connections per backend
	BackendActiveConnections = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_backend_active_connections",
			Help: "Number of active connections to backend",
		},
		[]string{"cluster", "endpoint"},
	)

	// Health Check Metrics

	// HealthChecksTotal tracks health check attempts
	HealthChecksTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_health_checks_total",
			Help: "Total number of health check attempts",
		},
		[]string{"cluster", "endpoint", "result"}, // result: success, failure
	)

	// HealthCheckDuration tracks health check duration
	HealthCheckDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "novaedge_health_check_duration_seconds",
			Help:    "Health check duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"cluster", "endpoint"},
	)

	// Circuit Breaker Metrics

	// CircuitBreakerState tracks circuit breaker state (0=closed, 1=half-open, 2=open)
	CircuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_circuit_breaker_state",
			Help: "Circuit breaker state (0=closed, 1=half-open, 2=open)",
		},
		[]string{"cluster", "endpoint"},
	)

	// CircuitBreakerTransitions tracks state transitions
	CircuitBreakerTransitions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_circuit_breaker_transitions_total",
			Help: "Total number of circuit breaker state transitions",
		},
		[]string{"cluster", "endpoint", "from_state", "to_state"},
	)

	// Load Balancer Metrics

	// LoadBalancerSelections tracks load balancer endpoint selections
	LoadBalancerSelections = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_load_balancer_selections_total",
			Help: "Total number of load balancer endpoint selections",
		},
		[]string{"cluster", "algorithm", "endpoint"},
	)

	// Connection Pool Metrics

	// PoolConnectionsTotal tracks total connections in pool
	PoolConnectionsTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_pool_connections_total",
			Help: "Total number of connections in pool",
		},
		[]string{"cluster"},
	)

	// PoolIdleConnections tracks idle connections in pool
	PoolIdleConnections = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_pool_idle_connections",
			Help: "Number of idle connections in pool",
		},
		[]string{"cluster"},
	)
)

// RecordBackendRequest records a backend request
func RecordBackendRequest(cluster, endpoint, result string, duration float64) {
	// Check cardinality limit for endpoint-specific metrics
	if !endpointTracker.shouldTrackEndpoint(cluster, endpoint) {
		// Use aggregated label for endpoints beyond cardinality limit
		endpoint = "other"
	}

	// Sample high-frequency metrics
	metricKey := cluster + ":" + endpoint
	if shouldSample(metricKey) {
		BackendRequestsTotal.WithLabelValues(cluster, endpoint, result).Inc()
		if duration > 0 {
			BackendResponseDuration.WithLabelValues(cluster, endpoint).Observe(duration)
		}
	}
}

// RecordHealthCheck records a health check
func RecordHealthCheck(cluster, endpoint, result string, duration float64) {
	// Check cardinality limit
	if !endpointTracker.shouldTrackEndpoint(cluster, endpoint) {
		endpoint = "other"
	}

	HealthChecksTotal.WithLabelValues(cluster, endpoint, result).Inc()
	HealthCheckDuration.WithLabelValues(cluster, endpoint).Observe(duration)
}

// SetBackendHealth sets backend health status
func SetBackendHealth(cluster, endpoint string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	BackendHealthStatus.WithLabelValues(cluster, endpoint).Set(value)
}

// SetCircuitBreakerState sets circuit breaker state
func SetCircuitBreakerState(cluster, endpoint string, state int) {
	CircuitBreakerState.WithLabelValues(cluster, endpoint).Set(float64(state))
}

// RecordCircuitBreakerTransition records a circuit breaker state transition
func RecordCircuitBreakerTransition(cluster, endpoint, fromState, toState string) {
	CircuitBreakerTransitions.WithLabelValues(cluster, endpoint, fromState, toState).Inc()
}

// RecordLoadBalancerSelection records a load balancer selection
func RecordLoadBalancerSelection(cluster, algorithm, endpoint string) {
	// Check cardinality limit
	if !endpointTracker.shouldTrackEndpoint(cluster, endpoint) {
		endpoint = "other"
	}

	// Sample this high-frequency metric
	if shouldSample(cluster + ":" + endpoint) {
		LoadBalancerSelections.WithLabelValues(cluster, algorithm, endpoint).Inc()
	}
}

// CleanupClusterMetrics removes tracking for a cluster that no longer exists
func CleanupClusterMetrics(cluster string) {
	endpointTracker.cleanupCluster(cluster)
}
