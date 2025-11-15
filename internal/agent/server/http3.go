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
	"fmt"
	"net/http"
	"time"

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

	return &HTTP3Server{
		logger:  logger.With(zap.String("server", "http3"), zap.Int32("port", port)),
		handler: handler,
		port:    port,
		config:  quicConfig,
		server: &http3.Server{
			Addr:      addr,
			Handler:   handler,
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
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP/3 server error", zap.Error(err))
			errChan <- err
		}
	}()

	// Wait for context cancellation or error
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	}
}

// Shutdown gracefully shuts down the HTTP/3 server
func (s *HTTP3Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down HTTP/3 server")

	// Close the server
	if err := s.server.Close(); err != nil {
		s.logger.Error("Error closing HTTP/3 server", zap.Error(err))
		return err
	}

	s.logger.Info("HTTP/3 server shutdown complete")
	return nil
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
