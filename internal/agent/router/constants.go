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

package router

import "strconv"

const (
	// WebSocket buffer sizes

	// DefaultWebSocketReadBufferSize is the default read buffer size for WebSocket connections (4KB)
	// This buffer is used when reading WebSocket frames from clients and backends.
	// Larger buffers improve performance for large messages but consume more memory.
	DefaultWebSocketReadBufferSize = 4096

	// DefaultWebSocketWriteBufferSize is the default write buffer size for WebSocket connections (4KB)
	// This buffer is used when writing WebSocket frames to clients and backends.
	// Larger buffers reduce syscall overhead but consume more memory.
	DefaultWebSocketWriteBufferSize = 4096

	// Request size limits

	// DefaultMaxRequestBodySize is the default maximum request body size (10MB)
	// This prevents memory exhaustion attacks from extremely large request bodies.
	// Can be overridden per-gateway via policy configuration in the future.
	DefaultMaxRequestBodySize = 10 * 1024 * 1024 // 10MB
)

// statusText provides pre-computed status code strings to avoid strconv.Itoa allocations
// in the hot path. HTTP status codes are in the range 100-599, so a 600-element array
// covers all standard codes with direct index access.
var statusText [600]string

func init() {
	for i := range statusText {
		statusText[i] = strconv.Itoa(i)
	}
}

// statusString returns a pre-computed string for HTTP status codes in the range [0, 600).
// For out-of-range codes it falls back to strconv.Itoa.
func statusString(code int) string {
	if code >= 0 && code < len(statusText) {
		return statusText[code]
	}
	return strconv.Itoa(code)
}
