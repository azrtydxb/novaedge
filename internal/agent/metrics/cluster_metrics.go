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

// Package metrics provides Prometheus metrics collection, sampling, and OpenTelemetry
// integration for the NovaEdge agent data plane.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// TLS Metrics

	// TLSHandshakes tracks total TLS handshakes
	TLSHandshakes = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "tls",
			Name:      "handshakes_total",
			Help:      "Total number of TLS handshakes",
		},
	)

	// TLSHandshakeErrors tracks TLS handshake errors
	TLSHandshakeErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "tls",
			Name:      "handshake_errors_total",
			Help:      "Total number of TLS handshake errors",
		},
		[]string{"error_type"},
	)

	// TLSVersion tracks TLS version usage
	TLSVersion = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "tls",
			Name:      "version_total",
			Help:      "Total connections by TLS version",
		},
		[]string{"version"}, // tls1.2, tls1.3
	)

	// TLSCipherSuite tracks cipher suite usage
	TLSCipherSuite = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "tls",
			Name:      "cipher_suite_total",
			Help:      "Total connections by cipher suite",
		},
		[]string{"cipher"},
	)

	// HTTP/3 and QUIC Metrics

	// HTTP3ConnectionsTotal tracks total HTTP/3 connections
	HTTP3ConnectionsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "http3",
			Name:      "connections_total",
			Help:      "Total number of HTTP/3 connections established",
		},
	)

	// HTTP3RequestsTotal tracks total HTTP/3 requests
	HTTP3RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "http3",
			Name:      "requests_total",
			Help:      "Total number of HTTP/3 requests",
		},
		[]string{"method", "status_class"},
	)

	// QUICStreamsActive tracks active QUIC streams
	QUICStreamsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "quic",
			Name:      "streams_active",
			Help:      "Number of active QUIC streams",
		},
	)

	// QUIC0RTTAccepted tracks 0-RTT resumption success
	QUIC0RTTAccepted = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "quic",
			Name:      "0rtt_accepted_total",
			Help:      "Total number of successful 0-RTT resumptions",
		},
	)

	// QUIC0RTTRejected tracks 0-RTT resumption rejections
	QUIC0RTTRejected = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "quic",
			Name:      "0rtt_rejected_total",
			Help:      "Total number of rejected 0-RTT resumptions",
		},
	)

	// QUICPacketsReceived tracks QUIC packets received
	QUICPacketsReceived = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "quic",
			Name:      "packets_received_total",
			Help:      "Total number of QUIC packets received",
		},
	)

	// QUICPacketsSent tracks QUIC packets sent
	QUICPacketsSent = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "quic",
			Name:      "packets_sent_total",
			Help:      "Total number of QUIC packets sent",
		},
	)

	// QUICConnectionErrors tracks QUIC connection errors
	QUICConnectionErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "quic",
			Name:      "connection_errors_total",
			Help:      "Total number of QUIC connection errors",
		},
		[]string{"error_type"},
	)

	// Policy Metrics

	// RateLimitAllowed tracks allowed requests
	RateLimitAllowed = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "rate_limit",
			Name:      "allowed_total",
			Help:      "Total number of requests allowed by rate limiter",
		},
	)

	// RateLimitDenied tracks denied requests
	RateLimitDenied = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "rate_limit",
			Name:      "denied_total",
			Help:      "Total number of requests denied by rate limiter",
		},
	)

	// CORSRequestsTotal tracks CORS requests
	CORSRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "cors",
			Name:      "requests_total",
			Help:      "Total number of CORS requests",
		},
		[]string{"type"}, // preflight, simple
	)

	// IPFilterDenied tracks IP filter denials
	IPFilterDenied = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "ip_filter",
			Name:      "denied_total",
			Help:      "Total number of requests denied by IP filter",
		},
		[]string{"filter_type"}, // allow_list, deny_list
	)

	// JWTValidationTotal tracks JWT validation attempts
	JWTValidationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "jwt",
			Name:      "validation_total",
			Help:      "Total number of JWT validation attempts",
		},
		[]string{"result"}, // success, failure
	)

	// JWTBlacklistSize tracks the current number of entries in the JWT token blacklist
	JWTBlacklistSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "jwt",
			Name:      "blacklist_size",
			Help:      "Current number of entries in the JWT token blacklist",
		},
	)

	// JWTRevocationsTotal tracks total number of JWT token revocations
	JWTRevocationsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "jwt",
			Name:      "revocations_total",
			Help:      "Total number of JWT tokens revoked",
		},
	)

	// JWTBlockedTotal tracks total number of requests blocked by the JWT blacklist
	JWTBlockedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "jwt",
			Name:      "blocked_total",
			Help:      "Total number of requests blocked due to revoked JWT tokens",
		},
	)

	// SecurityHeadersAppliedTotal tracks security headers applied
	SecurityHeadersAppliedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "security",
			Name:      "headers_applied_total",
			Help:      "Total number of responses with security headers applied",
		},
	)

	// ResponseHeadersModifiedTotal tracks response header modifications
	ResponseHeadersModifiedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "response",
			Name:      "headers_modified_total",
			Help:      "Total number of responses with modified headers",
		},
	)

	// BasicAuthTotal tracks Basic Auth validation attempts
	BasicAuthTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "basic_auth",
			Name:      "total",
			Help:      "Total number of HTTP Basic Auth validation attempts",
		},
		[]string{"result"}, // success, failure
	)

	// ForwardAuthTotal tracks forward auth delegation attempts
	ForwardAuthTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "forward_auth",
			Name:      "total",
			Help:      "Total number of forward auth delegation attempts",
		},
		[]string{"result", "source"}, // success/failure/error, cached/live
	)

	// OIDCAuthTotal tracks OIDC authentication events
	OIDCAuthTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "oidc_auth",
			Name:      "total",
			Help:      "Total number of OIDC authentication events",
		},
		[]string{"result"}, // success, redirect, callback_success, exchange_error, forbidden, logout
	)
)
