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

package l4

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// L4ConnectionsTotal tracks the total number of L4 connections
	L4ConnectionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_l4_connections_total",
			Help: "Total number of L4 connections",
		},
		[]string{"protocol", "listener", "backend"},
	)

	// L4ActiveConnections tracks currently active L4 connections
	L4ActiveConnections = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_l4_active_connections",
			Help: "Number of currently active L4 connections",
		},
		[]string{"protocol", "listener"},
	)

	// L4BytesSent tracks bytes sent to clients
	L4BytesSent = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_l4_bytes_sent_total",
			Help: "Total bytes sent to clients",
		},
		[]string{"protocol", "listener", "backend"},
	)

	// L4BytesReceived tracks bytes received from clients
	L4BytesReceived = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_l4_bytes_received_total",
			Help: "Total bytes received from clients",
		},
		[]string{"protocol", "listener", "backend"},
	)

	// L4ConnectionDuration tracks L4 connection duration
	L4ConnectionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "novaedge_l4_connection_duration_seconds",
			Help:    "Duration of L4 connections in seconds",
			Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300, 600, 1800, 3600},
		},
		[]string{"protocol", "listener"},
	)

	// L4ConnectionErrors tracks L4 connection errors
	L4ConnectionErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_l4_connection_errors_total",
			Help: "Total number of L4 connection errors",
		},
		[]string{"protocol", "listener", "error_type"},
	)

	// L4UDPSessionsTotal tracks total UDP sessions
	L4UDPSessionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_l4_udp_sessions_total",
			Help: "Total number of UDP sessions",
		},
		[]string{"listener", "backend"},
	)

	// L4UDPActiveSessions tracks currently active UDP sessions
	L4UDPActiveSessions = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_l4_udp_active_sessions",
			Help: "Number of currently active UDP sessions",
		},
		[]string{"listener"},
	)

	// L4TLSPassthroughTotal tracks TLS passthrough connections
	L4TLSPassthroughTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_l4_tls_passthrough_total",
			Help: "Total number of TLS passthrough connections",
		},
		[]string{"listener", "sni"},
	)

	// L4SNIRoutingErrors tracks SNI routing errors
	L4SNIRoutingErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_l4_sni_routing_errors_total",
			Help: "Total number of SNI routing errors",
		},
		[]string{"listener", "error_type"},
	)
)
