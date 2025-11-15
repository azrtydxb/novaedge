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
	"fmt"
	"net/http"
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
	logger *zap.Logger
	server *http.Server
	port   int
}

// NewMetricsServer creates a new metrics server
func NewMetricsServer(logger *zap.Logger, port int) *MetricsServer {
	if port == 0 {
		port = DefaultMetricsPort
	}

	return &MetricsServer{
		logger: logger,
		port:   port,
	}
}

// Start starts the metrics server
func (m *MetricsServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Register Prometheus metrics handler
	mux.Handle("/metrics", promhttp.Handler())

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Root endpoint with info
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("NovaEdge Metrics Server\n\nAvailable endpoints:\n- /metrics (Prometheus metrics)\n- /health (Health check)\n"))
	})

	m.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", m.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	m.logger.Info("Starting metrics server", zap.Int("port", m.port))

	// Start server in goroutine
	go func() {
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			m.logger.Error("Metrics server error", zap.Error(err))
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m.logger.Info("Shutting down metrics server")
	if err := m.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown metrics server: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the metrics server
func (m *MetricsServer) Shutdown(ctx context.Context) error {
	if m.server == nil {
		return nil
	}

	m.logger.Info("Shutting down metrics server")
	return m.server.Shutdown(ctx)
}
