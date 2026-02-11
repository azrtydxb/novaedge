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
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"go.uber.org/zap"
)

// HTTP3Server handles HTTP/3 requests using QUIC
type HTTP3Server struct {
	logger  *zap.Logger
	server  *http3.Server
	handler http.Handler
	port    int32
	config  *pb.QUICConfig
}

// NewHTTP3Server creates a new HTTP/3 server
func NewHTTP3Server(logger *zap.Logger, port int32, tlsConfig *tls.Config, quicConfig *pb.QUICConfig, handler http.Handler) *HTTP3Server {
	addr := fmt.Sprintf(":%d", port)

	// Wrap handler with HTTP/3 metrics collection
	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HTTP3ActiveRequests.Inc()
		defer HTTP3ActiveRequests.Dec()

		// Wrap the response writer to capture status code
		rw := &http3ResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		handler.ServeHTTP(rw, r)

		metrics.HTTP3RequestsTotal.WithLabelValues(r.Method, strconv.Itoa(rw.statusCode)).Inc()
	})

	return &HTTP3Server{
		logger:  logger.With(zap.String("server", "http3"), zap.Int32("port", port)),
		handler: handler,
		port:    port,
		config:  quicConfig,
		server: &http3.Server{
			Addr:      addr,
			Handler:   metricsHandler,
			TLSConfig: tlsConfig,
			QUICConfig: &quic.Config{
				MaxIdleTimeout:                 parseTimeout(quicConfig.GetMaxIdleTimeout(), 30*time.Second),
				MaxIncomingStreams:             quicConfig.GetMaxBiStreams(),
				MaxIncomingUniStreams:          quicConfig.GetMaxUniStreams(),
				Allow0RTT:                      quicConfig.GetEnable_0Rtt(),
				EnableDatagrams:                true,
				DisablePathMTUDiscovery:        false,
				InitialStreamReceiveWindow:     1 << 20,  // 1 MB
				MaxStreamReceiveWindow:         6 << 20,  // 6 MB
				InitialConnectionReceiveWindow: 1 << 20,  // 1 MB
				MaxConnectionReceiveWindow:     15 << 20, // 15 MB
			},
		},
	}
}

// http3ResponseWriter wraps http.ResponseWriter to capture the status code for metrics
type http3ResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *http3ResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *http3ResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *http3ResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Start starts the HTTP/3 server
func (s *HTTP3Server) Start(ctx context.Context) error {
	s.logger.Info("Starting HTTP/3 server",
		zap.String("address", s.server.Addr),
		zap.Bool("0-RTT", s.config.GetEnable_0Rtt()),
		zap.Int64("max_bi_streams", s.config.GetMaxBiStreams()),
		zap.Int64("max_uni_streams", s.config.GetMaxUniStreams()))

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("HTTP/3 server error", zap.Error(err))
			errChan <- err
		}
	}()

	// Wait for context cancellation or error
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.Shutdown(shutdownCtx) //nolint:contextcheck // shutdown context intentionally derived from context.Background() after parent cancellation
	}
}

// Shutdown gracefully shuts down the HTTP/3 server
// HTTP/3 uses QUIC which has built-in graceful connection closure
func (s *HTTP3Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down HTTP/3 server gracefully")

	// Create a channel to signal completion
	done := make(chan error, 1)

	go func() {
		// Close the server (http3.Server doesn't have CloseGracefully)
		// The underlying QUIC implementation handles connection drainage
		err := s.server.Close()
		done <- err
	}()

	// Wait for graceful close or context timeout
	select {
	case err := <-done:
		if err != nil {
			s.logger.Error("Error during graceful HTTP/3 shutdown", zap.Error(err))
			// Fall back to immediate close
			if closeErr := s.server.Close(); closeErr != nil {
				s.logger.Error("Error during immediate HTTP/3 close", zap.Error(closeErr))
			}
			return err
		}
		s.logger.Info("HTTP/3 server graceful shutdown complete")
		return nil
	case <-ctx.Done():
		s.logger.Warn("HTTP/3 graceful shutdown timeout, forcing close")
		// Context timed out, force close
		if err := s.server.Close(); err != nil {
			s.logger.Error("Error forcing HTTP/3 server close", zap.Error(err))
			return err
		}
		return ctx.Err()
	}
}

// SetQuicHeaders configures the Alt-Svc header to advertise HTTP/3 support
// This is called by the HTTP/1.1 and HTTP/2 handler to let clients know HTTP/3 is available
func (s *HTTP3Server) SetQuicHeaders(hdr http.Header) error {
	return s.server.SetQUICHeaders(hdr)
}

// parseTimeout parses a duration string or returns a default
func parseTimeout(timeoutStr string, defaultTimeout time.Duration) time.Duration {
	if timeoutStr == "" {
		return defaultTimeout
	}

	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return defaultTimeout
	}

	return timeout
}

// GetPort returns the port the server is listening on
func (s *HTTP3Server) GetPort() int32 {
	return s.port
}

// SupportsEarlyData returns whether 0-RTT is enabled
func (s *HTTP3Server) SupportsEarlyData() bool {
	return s.config.GetEnable_0Rtt()
}
