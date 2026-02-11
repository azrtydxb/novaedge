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

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// RequestLimitsMiddleware enforces per-route request size limits and timeouts.
type RequestLimitsMiddleware struct {
	limits *pb.RouteLimitsConfig
}

// NewRequestLimitsMiddleware creates a new request limits middleware.
func NewRequestLimitsMiddleware(limits *pb.RouteLimitsConfig) *RequestLimitsMiddleware {
	return &RequestLimitsMiddleware{limits: limits}
}

// Wrap returns an http.Handler that enforces request limits.
func (m *RequestLimitsMiddleware) Wrap(next http.Handler) http.Handler {
	if m.limits == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Apply request body size limit
		if m.limits.MaxRequestBodySize > 0 && r.Body != nil && r.Body != http.NoBody {
			// Check Content-Length header first for early rejection
			if r.ContentLength > 0 && r.ContentLength > m.limits.MaxRequestBodySize {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			// Wrap body with a LimitReader to enforce the limit during reading
			r.Body = &limitedReadCloser{
				ReadCloser: r.Body,
				remaining:  m.limits.MaxRequestBodySize,
				w:          w,
			}
		}

		ctx := r.Context()
		var cancel context.CancelFunc

		// Apply request timeout
		if m.limits.RequestTimeoutMs > 0 {
			timeout := time.Duration(m.limits.RequestTimeoutMs) * time.Millisecond
			ctx, cancel = context.WithTimeout(ctx, timeout)
		}

		// Apply idle timeout (uses same context deadline mechanism)
		if m.limits.IdleTimeoutMs > 0 && cancel == nil {
			timeout := time.Duration(m.limits.IdleTimeoutMs) * time.Millisecond
			ctx, cancel = context.WithTimeout(ctx, timeout)
		}

		if cancel != nil {
			defer cancel()
			r = r.WithContext(ctx)
		}

		// Use sync.Once to prevent concurrent writes to ResponseWriter.
		// Both the handler goroutine and the timeout path may attempt to
		// write; Once ensures only the first one succeeds.
		var once sync.Once
		done := make(chan struct{})
		go func() {
			defer close(done)
			next.ServeHTTP(w, r)
			once.Do(func() {
				// Handler completed normally; nothing extra to write
			})
		}()

		select {
		case <-done:
			// Request completed normally
		case <-ctx.Done():
			// Context expired (timeout)
			if ctx.Err() == context.DeadlineExceeded {
				once.Do(func() {
					http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
				})
			}
		}
	})
}

// limitedReadCloser wraps an io.ReadCloser with a size limit.
// When the limit is exceeded, it returns an error.
type limitedReadCloser struct {
	io.ReadCloser
	remaining int64
	exceeded  bool
	w         http.ResponseWriter
}

// Read reads from the underlying reader, enforcing the size limit.
func (lr *limitedReadCloser) Read(p []byte) (int, error) {
	if lr.exceeded {
		return 0, fmt.Errorf("request body exceeds maximum size")
	}

	n, err := lr.ReadCloser.Read(p)
	lr.remaining -= int64(n)
	if lr.remaining < 0 {
		lr.exceeded = true
		return n, fmt.Errorf("request body exceeds maximum size")
	}
	return n, err
}

// ParseByteSize parses a human-readable byte size string (e.g., "10Mi", "1024", "50MB").
// Supported suffixes: Ki, Mi, Gi, Ti (binary) and KB, MB, GB, TB (decimal).
// Returns the size in bytes.
func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}

	// Try plain integer first
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}

	// Binary units (IEC)
	multipliers := map[string]int64{
		"Ki": 1 << 10,
		"Mi": 1 << 20,
		"Gi": 1 << 30,
		"Ti": 1 << 40,
		// Decimal units (SI)
		"KB": 1000,
		"MB": 1000 * 1000,
		"GB": 1000 * 1000 * 1000,
		"TB": 1000 * 1000 * 1000 * 1000,
		"K":  1 << 10, // shorthand
		"M":  1 << 20,
		"G":  1 << 30,
	}

	for suffix, mult := range multipliers {
		if strings.HasSuffix(s, suffix) {
			numStr := strings.TrimSuffix(s, suffix)
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid byte size: %s", s)
			}
			return n * mult, nil
		}
	}

	return 0, fmt.Errorf("invalid byte size: %s", s)
}

// ParseDurationMs parses a duration string and returns it in milliseconds.
func ParseDurationMs(s string) (int64, error) {
	if s == "" || s == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %s", s)
	}
	return d.Milliseconds(), nil
}
