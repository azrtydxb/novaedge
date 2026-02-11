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

package server

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP3ActiveRequests tracks the number of active HTTP/3 requests
	HTTP3ActiveRequests = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "http3",
		Name:      "active_requests",
		Help:      "Number of active HTTP/3 requests",
	})

	// QUICStreams tracks the number of active QUIC streams
	QUICStreams = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "http3",
		Name:      "quic_streams",
		Help:      "Number of active QUIC streams",
	})

	// QUICHandshakeDuration tracks QUIC handshake durations
	QUICHandshakeDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "novaedge",
		Subsystem: "http3",
		Name:      "quic_handshake_duration_seconds",
		Help:      "Duration of QUIC handshakes in seconds",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
	})

	// QUICZeroRTTAttempts tracks 0-RTT connection attempts
	QUICZeroRTTAttempts = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "novaedge",
		Subsystem: "http3",
		Name:      "quic_0rtt_attempts_total",
		Help:      "Total number of QUIC 0-RTT connection attempts",
	})

	// QUICZeroRTTSuccesses tracks successful 0-RTT connections
	QUICZeroRTTSuccesses = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "novaedge",
		Subsystem: "http3",
		Name:      "quic_0rtt_successes_total",
		Help:      "Total number of successful QUIC 0-RTT connections",
	})

	// HTTP3Requests references the HTTP/3 request counter from cluster metrics.
	// We reuse the metric defined in internal/agent/metrics to avoid duplicate registration.
	// Use metrics.HTTP3RequestsTotal directly for tracking HTTP/3 requests.

	// SSEActiveConnections tracks the number of active SSE connections
	SSEActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "sse",
		Name:      "active_connections",
		Help:      "Number of active SSE connections",
	})

	// SSEConnectionDuration tracks SSE connection duration
	SSEConnectionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "novaedge",
		Subsystem: "sse",
		Name:      "connection_duration_seconds",
		Help:      "Duration of SSE connections in seconds",
		Buckets:   []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600},
	})

	// SSEHeartbeatsSent tracks the total number of SSE heartbeats sent
	SSEHeartbeatsSent = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "novaedge",
		Subsystem: "sse",
		Name:      "heartbeats_sent_total",
		Help:      "Total number of SSE heartbeat comments sent",
	})

	// GRPCRequestsTotal tracks gRPC requests by service, method, and status
	GRPCRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novaedge",
		Subsystem: "grpc",
		Name:      "requests_total",
		Help:      "Total number of gRPC requests",
	}, []string{"service", "method", "grpc_code"})
)
