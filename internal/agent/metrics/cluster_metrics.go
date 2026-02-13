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

	// JWTBlacklistSize tracks the current number of entries in the JWT token blacklist
	JWTBlacklistSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "novaedge_jwt_blacklist_size",
			Help: "Current number of entries in the JWT token blacklist",
		},
	)

	// JWTRevocationsTotal tracks total number of JWT token revocations
	JWTRevocationsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_jwt_revocations_total",
			Help: "Total number of JWT tokens revoked",
		},
	)

	// JWTBlockedTotal tracks total number of requests blocked by the JWT blacklist
	JWTBlockedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_jwt_blocked_total",
			Help: "Total number of requests blocked due to revoked JWT tokens",
		},
	)

	// SecurityHeadersAppliedTotal tracks security headers applied
	SecurityHeadersAppliedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_security_headers_applied_total",
			Help: "Total number of responses with security headers applied",
		},
	)

	// ResponseHeadersModifiedTotal tracks response header modifications
	ResponseHeadersModifiedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_response_headers_modified_total",
			Help: "Total number of responses with modified headers",
		},
	)

	// BasicAuthTotal tracks Basic Auth validation attempts
	BasicAuthTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_basic_auth_total",
			Help: "Total number of HTTP Basic Auth validation attempts",
		},
		[]string{"result"}, // success, failure
	)

	// ForwardAuthTotal tracks forward auth delegation attempts
	ForwardAuthTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_forward_auth_total",
			Help: "Total number of forward auth delegation attempts",
		},
		[]string{"result", "source"}, // success/failure/error, cached/live
	)

	// OIDCAuthTotal tracks OIDC authentication events
	OIDCAuthTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_oidc_auth_total",
			Help: "Total number of OIDC authentication events",
		},
		[]string{"result"}, // success, redirect, callback_success, exchange_error, forbidden, logout
	)
)

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
