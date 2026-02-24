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
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// faultInjectedHeader is the response header added when a fault is injected.
const faultInjectedHeader = "X-Fault-Injected"

// FaultInjectionConfig holds configuration for fault injection middleware.
// It supports both delay injection (adding latency) and abort injection
// (returning an error status code), each applied to a configurable
// percentage of requests.
type FaultInjectionConfig struct {
	// DelayDuration is the fixed delay to inject into matching requests.
	DelayDuration time.Duration

	// DelayPercent is the percentage of requests to delay (0-100).
	// A value of 0 disables delay injection; 100 delays all requests.
	DelayPercent float64

	// AbortStatusCode is the HTTP status code returned for aborted requests.
	AbortStatusCode int

	// AbortPercent is the percentage of requests to abort (0-100).
	// A value of 0 disables abort injection; 100 aborts all requests.
	AbortPercent float64

	// HeaderActivation is an optional header name that must be present in the
	// request to activate fault injection. When empty, fault injection is
	// always active. Example: "x-fault-inject".
	HeaderActivation string

	// Route is the route name used for Prometheus metrics labels.
	Route string
}

// faultErrorResponse is the JSON body returned when a request is aborted.
type faultErrorResponse struct {
	Error      string `json:"error"`
	StatusCode int    `json:"status_code"`
	FaultType  string `json:"fault_type"`
}

// FaultInjectionMiddleware is an HTTP middleware that injects faults into
// requests for chaos engineering purposes. It can introduce artificial
// latency (delay) and/or return error responses (abort) based on
// configurable percentages.
type FaultInjectionMiddleware struct {
	config *FaultInjectionConfig
	logger *zap.Logger
	// randFloat is a function returning a float64 in [0, 100). Injected for testing.
	randFloat func() float64
	// Pre-computed header value strings to avoid fmt.Sprintf in the hot path.
	delayHeaderValue string
	abortHeaderValue string
}

// NewFaultInjectionMiddleware creates a new FaultInjectionMiddleware with the
// given configuration. If logger is nil, a no-op logger is used.
func NewFaultInjectionMiddleware(config *FaultInjectionConfig, logger *zap.Logger) *FaultInjectionMiddleware {
	if logger == nil {
		logger = zap.NewNop()
	}
	m := &FaultInjectionMiddleware{
		config: config,
		logger: logger,
		randFloat: func() float64 {
			var buf [8]byte
			if _, err := rand.Read(buf[:]); err != nil {
				return 0
			}
			return float64(binary.LittleEndian.Uint64(buf[:])) / float64(^uint64(0)) * 100
		},
	}
	// Pre-compute header value strings at config time so the hot path
	// avoids fmt.Sprintf entirely.
	if config != nil {
		if config.DelayDuration > 0 {
			m.delayHeaderValue = "delay=" + config.DelayDuration.String()
		}
		if config.AbortStatusCode > 0 {
			m.abortHeaderValue = "abort=" + strconv.Itoa(config.AbortStatusCode)
		}
	}
	return m
}

// Wrap returns an http.Handler that wraps the given handler with fault
// injection logic. If the configuration is nil or both percentages are zero,
// the next handler is returned unchanged.
func (m *FaultInjectionMiddleware) Wrap(next http.Handler) http.Handler {
	if m.config == nil {
		return next
	}
	if m.config.DelayPercent <= 0 && m.config.AbortPercent <= 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If header activation is configured, only apply when the header is present
		if m.config.HeaderActivation != "" {
			if r.Header.Get(m.config.HeaderActivation) == "" {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Determine whether to inject delay
		applyDelay := m.config.DelayPercent > 0 && m.config.DelayDuration > 0 && m.randFloat() < m.config.DelayPercent

		// Determine whether to inject abort
		applyAbort := m.config.AbortPercent > 0 && m.config.AbortStatusCode > 0 && m.randFloat() < m.config.AbortPercent

		// Apply delay first (if applicable)
		if applyDelay {
			m.logger.Debug("Injecting fault delay",
				zap.Duration("delay", m.config.DelayDuration),
				zap.String("path", r.URL.Path),
				zap.String("method", r.Method),
			)

			select {
			case <-time.After(m.config.DelayDuration):
			case <-r.Context().Done():
				// Request was cancelled during the injected delay
				return
			}

			w.Header().Add(faultInjectedHeader, m.delayHeaderValue)
			FaultInjectionDelaysTotal.WithLabelValues(m.config.Route, r.Method).Inc()
		}

		// Apply abort (if applicable)
		if applyAbort {
			m.logger.Debug("Injecting fault abort",
				zap.Int("status_code", m.config.AbortStatusCode),
				zap.String("path", r.URL.Path),
				zap.String("method", r.Method),
			)

			w.Header().Add(faultInjectedHeader, m.abortHeaderValue)
			FaultInjectionAbortsTotal.WithLabelValues(m.config.Route, r.Method, strconv.Itoa(m.config.AbortStatusCode)).Inc()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(m.config.AbortStatusCode)

			resp := faultErrorResponse{
				Error:      http.StatusText(m.config.AbortStatusCode),
				StatusCode: m.config.AbortStatusCode,
				FaultType:  "injected_abort",
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				m.logger.Debug("Failed to write fault abort response body", zap.Error(err))
			}
			return
		}

		// No abort; proceed to the next handler
		next.ServeHTTP(w, r)
	})
}
