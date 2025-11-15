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
	// HTTP Request Metrics

	// HTTPRequestsTotal tracks total HTTP requests
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "status", "cluster"},
	)

	// HTTPRequestDuration tracks HTTP request duration
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "novaedge_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets, // 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
		},
		[]string{"method", "cluster"},
	)

	// HTTPRequestsInFlight tracks active HTTP requests
	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "novaedge_http_requests_in_flight",
			Help: "Number of HTTP requests currently being processed",
		},
	)

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

	// VIP Metrics

	// VIPStatus tracks VIP status (1=active, 0=inactive)
	VIPStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_vip_status",
			Help: "VIP status (1=active, 0=inactive)",
		},
		[]string{"vip_name", "address", "mode"},
	)

	// BGPSessionStatus tracks BGP session status (1=established, 0=down)
	BGPSessionStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_bgp_session_status",
			Help: "BGP session status (1=established, 0=down)",
		},
		[]string{"peer_address", "peer_as"},
	)

	// BGPAnnouncedRoutes tracks number of announced BGP routes
	BGPAnnouncedRoutes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "novaedge_bgp_announced_routes",
			Help: "Number of BGP routes currently announced",
		},
	)

	// OSPFNeighborStatus tracks OSPF neighbor status (1=full, 0=down)
	OSPFNeighborStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_ospf_neighbor_status",
			Help: "OSPF neighbor status (1=full, 0=down)",
		},
		[]string{"neighbor_address", "area_id"},
	)

	// OSPFAnnouncedRoutes tracks number of announced OSPF LSAs
	OSPFAnnouncedRoutes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "novaedge_ospf_announced_routes",
			Help: "Number of OSPF LSAs currently announced",
		},
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

	// TLS Metrics

	// TLSHandshakes tracks total TLS handshakes
	TLSHandshakes = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_tls_handshakes_total",
			Help: "Total number of TLS handshakes",
		},
	)

	// TLSHandshakeErrors tracks TLS handshake errors
	TLSHandshakeErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_tls_handshake_errors_total",
			Help: "Total number of TLS handshake errors",
		},
		[]string{"error_type"},
	)

	// TLSVersion tracks TLS version usage
	TLSVersion = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_tls_version_total",
			Help: "Total connections by TLS version",
		},
		[]string{"version"}, // tls1.2, tls1.3
	)

	// TLSCipherSuite tracks cipher suite usage
	TLSCipherSuite = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_tls_cipher_suite_total",
			Help: "Total connections by cipher suite",
		},
		[]string{"cipher"},
	)

	// HTTP/3 and QUIC Metrics

	// HTTP3ConnectionsTotal tracks total HTTP/3 connections
	HTTP3ConnectionsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_http3_connections_total",
			Help: "Total number of HTTP/3 connections established",
		},
	)

	// HTTP3RequestsTotal tracks total HTTP/3 requests
	HTTP3RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_http3_requests_total",
			Help: "Total number of HTTP/3 requests",
		},
		[]string{"method", "status"},
	)

	// QUICStreamsActive tracks active QUIC streams
	QUICStreamsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "novaedge_quic_streams_active",
			Help: "Number of active QUIC streams",
		},
	)

	// QUIC0RTTAccepted tracks 0-RTT resumption success
	QUIC0RTTAccepted = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_quic_0rtt_accepted_total",
			Help: "Total number of successful 0-RTT resumptions",
		},
	)

	// QUIC0RTTRejected tracks 0-RTT resumption rejections
	QUIC0RTTRejected = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_quic_0rtt_rejected_total",
			Help: "Total number of rejected 0-RTT resumptions",
		},
	)

	// QUICPacketsReceived tracks QUIC packets received
	QUICPacketsReceived = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_quic_packets_received_total",
			Help: "Total number of QUIC packets received",
		},
	)

	// QUICPacketsSent tracks QUIC packets sent
	QUICPacketsSent = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_quic_packets_sent_total",
			Help: "Total number of QUIC packets sent",
		},
	)

	// QUICConnectionErrors tracks QUIC connection errors
	QUICConnectionErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_quic_connection_errors_total",
			Help: "Total number of QUIC connection errors",
		},
		[]string{"error_type"},
	)

	// Policy Metrics

	// RateLimitAllowed tracks allowed requests
	RateLimitAllowed = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_rate_limit_allowed_total",
			Help: "Total number of requests allowed by rate limiter",
		},
	)

	// RateLimitDenied tracks denied requests
	RateLimitDenied = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_rate_limit_denied_total",
			Help: "Total number of requests denied by rate limiter",
		},
	)

	// CORSRequestsTotal tracks CORS requests
	CORSRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_cors_requests_total",
			Help: "Total number of CORS requests",
		},
		[]string{"type"}, // preflight, simple
	)

	// IPFilterDenied tracks IP filter denials
	IPFilterDenied = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_ip_filter_denied_total",
			Help: "Total number of requests denied by IP filter",
		},
		[]string{"filter_type"}, // allow_list, deny_list
	)

	// JWTValidationTotal tracks JWT validation attempts
	JWTValidationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_jwt_validation_total",
			Help: "Total number of JWT validation attempts",
		},
		[]string{"result"}, // success, failure
	)
)

// RecordHTTPRequest records an HTTP request
func RecordHTTPRequest(method, status, cluster string, duration float64) {
	HTTPRequestsTotal.WithLabelValues(method, status, cluster).Inc()
	HTTPRequestDuration.WithLabelValues(method, cluster).Observe(duration)
}

// RecordBackendRequest records a backend request
func RecordBackendRequest(cluster, endpoint, result string, duration float64) {
	BackendRequestsTotal.WithLabelValues(cluster, endpoint, result).Inc()
	if duration > 0 {
		BackendResponseDuration.WithLabelValues(cluster, endpoint).Observe(duration)
	}
}

// RecordHealthCheck records a health check
func RecordHealthCheck(cluster, endpoint, result string, duration float64) {
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

// SetVIPStatus sets VIP status
func SetVIPStatus(vipName, address, mode string, active bool) {
	value := 0.0
	if active {
		value = 1.0
	}
	VIPStatus.WithLabelValues(vipName, address, mode).Set(value)
}

// SetBGPSessionStatus sets BGP session status
func SetBGPSessionStatus(peerAddress, peerAS string, established bool) {
	value := 0.0
	if established {
		value = 1.0
	}
	BGPSessionStatus.WithLabelValues(peerAddress, peerAS).Set(value)
}

// SetOSPFNeighborStatus sets OSPF neighbor status
func SetOSPFNeighborStatus(neighborAddress, areaID string, full bool) {
	value := 0.0
	if full {
		value = 1.0
	}
	OSPFNeighborStatus.WithLabelValues(neighborAddress, areaID).Set(value)
}

// RecordLoadBalancerSelection records a load balancer selection
func RecordLoadBalancerSelection(cluster, algorithm, endpoint string) {
	LoadBalancerSelections.WithLabelValues(cluster, algorithm, endpoint).Inc()
}
