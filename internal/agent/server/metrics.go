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
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

const (
	// DefaultMetricsPort is the default port for metrics endpoint
	DefaultMetricsPort = 9090
)

// MetricsServer serves Prometheus metrics on a dedicated port
type MetricsServer struct {
	logger      *zap.Logger
	mu          sync.Mutex
	server      *http.Server
	port        int
	rateLimiter *IPRateLimiter
}

// NewMetricsServer creates a new metrics server.
// The provided context controls the lifetime of background goroutines (e.g. rate-limiter cleanup).
func NewMetricsServer(ctx context.Context, logger *zap.Logger, port int) *MetricsServer {
	if port == 0 {
		port = DefaultMetricsPort
	}

	return &MetricsServer{
		logger:      logger,
		port:        port,
		rateLimiter: NewIPRateLimiter(ctx, DefaultObservabilityRateLimitConfig()),
	}
}

// Start starts the metrics server
func (m *MetricsServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Apply rate limiting middleware
	rateLimitMiddleware := RateLimitMiddleware(m.rateLimiter)

	// Register Prometheus metrics handler with rate limiting
	mux.Handle("/metrics", rateLimitMiddleware(promhttp.Handler()))

	// Health check endpoint with rate limiting
	mux.Handle("/health", rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})))

	// Root endpoint with info (no rate limiting needed for this informational endpoint)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("NovaEdge Metrics Server\n\nAvailable endpoints:\n- /metrics (Prometheus metrics)\n- /health (Health check)\n"))
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", m.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	m.mu.Lock()
	m.server = srv
	m.mu.Unlock()

	m.logger.Info("Starting metrics server", zap.Int("port", m.port))

	// Start server in goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.logger.Error("Metrics server error", zap.Error(err))
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Stop rate limiter cleanup routine
	m.rateLimiter.Stop()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer cancel()

	m.logger.Info("Shutting down metrics server")
	if err := srv.Shutdown(shutdownCtx); err != nil { //nolint:contextcheck // shutdown context intentionally derived from context.Background() after parent cancellation
		return fmt.Errorf("failed to shutdown metrics server: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the metrics server
func (m *MetricsServer) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	srv := m.server
	m.mu.Unlock()

	if srv == nil {
		return nil
	}

	m.logger.Info("Shutting down metrics server")
	return srv.Shutdown(ctx)
}
