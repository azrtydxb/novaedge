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

package proto

// CompressionConfig defines response compression settings for a gateway.
// Compression is applied after the backend response is received but before
// it is sent to the client.
type CompressionConfig struct {
	// Enabled enables response compression
	Enabled bool `json:"enabled,omitempty"`

	// MinSize is the minimum response body size in bytes before compression triggers.
	// Responses smaller than this are sent uncompressed. Default: 1024 (1KB).
	MinSize int64 `json:"min_size,omitempty"`

	// Level is the compression level. For gzip: 1-9 (1=fastest, 9=best).
	// For brotli: 0-11 (0=fastest, 11=best). Default: 6 for gzip, 4 for brotli.
	Level int32 `json:"level,omitempty"`

	// Algorithms is the list of supported compression algorithms in preference order.
	// Supported values: "gzip", "br". Default: ["gzip", "br"].
	Algorithms []string `json:"algorithms,omitempty"`

	// ExcludeTypes is a list of content type patterns to skip compression.
	// Supports glob-style wildcards (e.g., "image/*", "video/*").
	// Default: ["image/*", "video/*", "audio/*", "application/zip",
	//           "application/gzip", "application/x-brotli"].
	ExcludeTypes []string `json:"exclude_types,omitempty"`
}

// RouteLimitsConfig defines per-route request size limits and timeouts.
type RouteLimitsConfig struct {
	// MaxRequestBodySize is the maximum allowed request body size in bytes.
	// Requests exceeding this limit receive a 413 Payload Too Large response.
	// 0 means use the gateway default.
	MaxRequestBodySize int64 `json:"max_request_body_size,omitempty"`

	// RequestTimeoutMs is the total request timeout in milliseconds.
	// If the upstream does not respond within this time, a 504 Gateway Timeout
	// is returned. 0 means no timeout.
	RequestTimeoutMs int64 `json:"request_timeout_ms,omitempty"`

	// IdleTimeoutMs is the idle timeout in milliseconds for the connection.
	// If no data is received within this time, the connection is closed.
	// 0 means no idle timeout.
	IdleTimeoutMs int64 `json:"idle_timeout_ms,omitempty"`
}

// BufferingConfig defines request and response buffering settings.
type BufferingConfig struct {
	// RequestBuffering enables buffering the entire request body before forwarding.
	// This is required for retry support since the body must be re-readable.
	RequestBuffering bool `json:"request_buffering,omitempty"`

	// ResponseBuffering enables buffering the entire response body before sending
	// to the client. This is useful for response transformations.
	ResponseBuffering bool `json:"response_buffering,omitempty"`

	// MaxBufferSize is the maximum buffer size in bytes.
	// If the body exceeds this size, a 413 Payload Too Large is returned for
	// requests, or the response is streamed without buffering.
	// Default: 0 (no limit, stream through).
	MaxBufferSize int64 `json:"max_buffer_size,omitempty"`

	// MemoryThreshold is the body size threshold in bytes at which buffering
	// switches from memory to a temporary file. Default: 1048576 (1MB).
	MemoryThreshold int64 `json:"memory_threshold,omitempty"`
}
