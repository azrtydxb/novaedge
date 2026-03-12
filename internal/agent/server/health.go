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
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// HealthServer provides health and readiness endpoints for Kubernetes probes
type HealthServer struct {
	logger      *zap.Logger
	port        int
	server      *http.Server
	ready       atomic.Bool
	rateLimiter *IPRateLimiter
}

// NewHealthServer creates a new health probe server.
// The provided context controls the lifetime of background goroutines (e.g. rate-limiter cleanup).
func NewHealthServer(ctx context.Context, logger *zap.Logger, port int) *HealthServer {
	return &HealthServer{
		logger:      logger,
		port:        port,
		rateLimiter: NewIPRateLimiter(ctx, DefaultObservabilityRateLimitConfig()),
	}
}

// Start starts the health probe server
func (h *HealthServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Apply rate limiting middleware to all endpoints
	rateLimitMiddleware := RateLimitMiddleware(h.rateLimiter)

	// Liveness probe - returns 200 if process is running
	mux.Handle("/healthz", rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})))

	// Readiness probe - returns 200 if agent has received valid config
	readyHandler := rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if h.ready.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ready"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("Not Ready"))
		}
	}))
	mux.Handle("/ready", readyHandler)
	mux.Handle("/readyz", readyHandler)

	// Detailed status endpoint
	mux.Handle("/status", rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if h.ready.Load() {
			_, _ = w.Write([]byte(`{"status":"ready","healthy":true}`))
		} else {
			_, _ = w.Write([]byte(`{"status":"not_ready","healthy":false}`))
		}
	})))

	h.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", h.port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	h.logger.Info("Starting health probe server", zap.Int("port", h.port))

	// Use WithoutCancel so the shutdown timeout is not already expired
	// when the parent context is cancelled.
	detachedCtx := context.WithoutCancel(ctx)
	go func() {
		<-ctx.Done()
		h.logger.Info("Shutting down health probe server")

		// Stop rate limiter cleanup routine
		h.rateLimiter.Stop()

		shutdownCtx, cancel := context.WithTimeout(detachedCtx, serverShutdownTimeout)
		defer cancel()
		if err := h.server.Shutdown(shutdownCtx); err != nil {
			h.logger.Error("Health server shutdown error", zap.Error(err))
		}
	}()

	if err := h.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("health server error: %w", err)
	}

	return nil
}

// SetReady marks the agent as ready (has received valid config)
func (h *HealthServer) SetReady(ready bool) {
	h.ready.Store(ready)
	if ready {
		h.logger.Info("Agent marked as ready")
	} else {
		h.logger.Info("Agent marked as not ready")
	}
}
