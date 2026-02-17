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

package overload

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"
)

// LoadSheddingMiddleware returns HTTP 503 Service Unavailable with a Retry-After
// header when the system is in an overloaded state. Non-overloaded requests pass
// through to the next handler.
type LoadSheddingMiddleware struct {
	manager    *Manager
	retryAfter int // seconds for Retry-After header
	logger     *zap.Logger
}

// NewLoadSheddingMiddleware creates a new load shedding middleware.
// The retryAfter parameter specifies the Retry-After header value in seconds.
func NewLoadSheddingMiddleware(manager *Manager, retryAfter int, logger *zap.Logger) *LoadSheddingMiddleware {
	if retryAfter <= 0 {
		retryAfter = 30
	}
	return &LoadSheddingMiddleware{
		manager:    manager,
		retryAfter: retryAfter,
		logger:     logger.With(zap.String("component", "load-shedding")),
	}
}

// Wrap returns an http.Handler that rejects requests with 503 when the system
// is overloaded.
func (m *LoadSheddingMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.manager.ShouldShed() {
			state := m.manager.GetState()
			shedTotal.Inc()

			m.logger.Debug("shedding request due to overload",
				zap.String("reason", state.ShedReason),
				zap.String("path", r.URL.Path),
			)

			w.Header().Set("Retry-After", fmt.Sprintf("%d", m.retryAfter))
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("service overloaded"))
			return
		}

		next.ServeHTTP(w, r)
	})
}
