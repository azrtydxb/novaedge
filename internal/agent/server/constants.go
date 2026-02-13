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

import "time"

const (
	// ServerReadTimeout is the maximum duration for reading the entire request, including the body.
	ServerReadTimeout = 30 * time.Second

	// ServerWriteTimeout is the maximum duration before timing out writes of the response
	ServerWriteTimeout = 30 * time.Second

	// ServerIdleTimeout is the maximum amount of time to wait for the next request when keep-alives are enabled
	ServerIdleTimeout = 120 * time.Second

	// ServerReadHeaderTimeout is the amount of time allowed to read request headers
	ServerReadHeaderTimeout = 10 * time.Second

	// MaxHeaderBytes is the maximum size of request headers (1MB)
	MaxHeaderBytes = 1 << 20

	// GracefulShutdownTimeout is the timeout for gracefully shutting down listeners
	GracefulShutdownTimeout = 5 * time.Second

	// MetricsServerReadTimeout is the read timeout for the metrics endpoint.
	MetricsServerReadTimeout = 10 * time.Second

	// MetricsServerWriteTimeout is the write timeout for the metrics endpoint
	MetricsServerWriteTimeout = 10 * time.Second

	// MetricsServerIdleTimeout is the idle timeout for the metrics endpoint
	MetricsServerIdleTimeout = 60 * time.Second

	// HTTP3DefaultMaxIdleTimeout is the default maximum idle timeout for QUIC connections.
	HTTP3DefaultMaxIdleTimeout = 30 * time.Second

	// HTTP3DefaultMaxBiStreams is the default maximum number of bidirectional streams
	HTTP3DefaultMaxBiStreams = 100

	// HTTP3DefaultMaxUniStreams is the default maximum number of unidirectional streams
	HTTP3DefaultMaxUniStreams = 100

	// HTTP3AltSvcMaxAge is the max age for Alt-Svc header in seconds (30 days)
	HTTP3AltSvcMaxAge = 2592000
)
