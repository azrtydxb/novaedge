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

	// Outlier Detection Metrics

	// EndpointEjectionsTotal tracks total ejections by endpoint and reason.
	EndpointEjectionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_endpoint_ejections_total",
			Help: "Total number of endpoint ejections by outlier detection",
		},
		[]string{"cluster", "endpoint", "reason"}, // reason: success_rate, failure_percentage, consecutive_errors
	)

	// EndpointsEjected tracks the current number of ejected endpoints per cluster.
	EndpointsEjected = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_endpoints_ejected",
			Help: "Current number of ejected endpoints per cluster",
		},
		[]string{"cluster"},
	)

	// CircuitBreakerOverflowTotal tracks circuit breaker resource limit overflows
	CircuitBreakerOverflowTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_circuit_breaker_overflow_total",
			Help: "Total number of circuit breaker resource limit overflows",
		},
		[]string{"cluster", "type"}, // type: connections, pending, requests, retries
	)

	// PassiveHealthDroppedTotal tracks passive health events dropped because the
	// channel was full. A sustained increase indicates the channel size should be
	// increased or the request rate is overwhelming the passive health processor.
	PassiveHealthDroppedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_passive_health_events_dropped_total",
			Help: "Total number of passive health events dropped due to full channel",
		},
		[]string{"cluster"},
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

	// PoolActiveConnections tracks active (in-flight) connections in pool
	PoolActiveConnections = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_pool_active_connections",
			Help: "Number of active (in-flight) connections in pool",
		},
		[]string{"cluster"},
	)

	// PoolHitsTotal tracks proxy cache hits (reused existing reverse proxy)
	PoolHitsTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_pool_hits_total",
			Help: "Total number of proxy cache hits (reused existing proxy on config update)",
		},
		[]string{"cluster"},
	)

	// PoolMissesTotal tracks proxy cache misses (had to create new reverse proxy)
	PoolMissesTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_pool_misses_total",
			Help: "Total number of proxy cache misses (created new proxy on config update)",
		},
		[]string{"cluster"},
	)

	// InsecureBackendConnectionsTotal tracks backend connections with InsecureSkipVerify enabled.
	// This is a security audit metric to monitor backends bypassing TLS certificate verification.
	InsecureBackendConnectionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "upstream",
			Name:      "insecure_backend_connections_total",
			Help:      "Total number of backend connections with InsecureSkipVerify enabled.",
		},
		[]string{"backend"},
	)
)

// RecordBackendRequest records a backend request
func RecordBackendRequest(cluster, endpoint, result string, duration float64) {
	endpoint = resolveEndpointLabel(cluster, endpoint)

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
	endpoint = resolveEndpointLabel(cluster, endpoint)

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

// RecordEndpointEjection records an outlier detection ejection event.
func RecordEndpointEjection(cluster, endpoint, reason string) {
	EndpointEjectionsTotal.WithLabelValues(cluster, endpoint, reason).Inc()
}

// SetEndpointsEjected sets the current number of ejected endpoints for a cluster.
func SetEndpointsEjected(cluster string, count int) {
	EndpointsEjected.WithLabelValues(cluster).Set(float64(count))
}

// RecordCircuitBreakerOverflow records a circuit breaker resource limit overflow event.
func RecordCircuitBreakerOverflow(cluster, limitType string) {
	CircuitBreakerOverflowTotal.WithLabelValues(cluster, limitType).Inc()
}

// RecordPassiveHealthDropped increments the counter for dropped passive health events.
func RecordPassiveHealthDropped(cluster string) {
	PassiveHealthDroppedTotal.WithLabelValues(cluster).Inc()
}

// CleanupClusterMetrics removes tracking for a cluster that no longer exists.
// This also deletes all associated Prometheus metric series to prevent stale data.
func CleanupClusterMetrics(cluster string) {
	endpointTracker.cleanupCluster(cluster)

	// Also clean up pool-level metrics for the cluster
	PoolConnectionsTotal.DeleteLabelValues(cluster)
	PoolIdleConnections.DeleteLabelValues(cluster)
	PoolActiveConnections.DeleteLabelValues(cluster)
	PoolHitsTotal.DeleteLabelValues(cluster)
	PoolMissesTotal.DeleteLabelValues(cluster)
	EndpointsEjected.DeleteLabelValues(cluster)
	PassiveHealthDroppedTotal.DeleteLabelValues(cluster)
	InsecureBackendConnectionsTotal.DeleteLabelValues(cluster)
}

