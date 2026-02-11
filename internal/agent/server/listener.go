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

	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// ListenerInfo contains information about a configured listener
type ListenerInfo struct {
	Gateway   string
	Listener  *pb.Listener
	TLSConfig *tls.Config
}

// startListener starts an HTTP, HTTPS, or HTTP/3 listener on the specified port
func (s *HTTPServer) startListener(ctx context.Context, port int32, listenerInfo *ListenerInfo) error {
	// Check if this is an HTTP/3 listener
	if listenerInfo.Listener.Protocol == pb.Protocol_HTTP3 {
		return s.startHTTP3Listener(ctx, port, listenerInfo)
	}

	protocol := "HTTP"
	if listenerInfo.TLSConfig != nil {
		protocol = "HTTPS (HTTP/2)"
	} else {
		protocol = "HTTP (HTTP/2 h2c)"
	}

	s.logger.Info("Starting HTTP/1.1 or HTTP/2 listener",
		zap.Int32("port", port),
		zap.String("protocol", protocol),
		zap.String("gateway", listenerInfo.Gateway),
		zap.String("listener", listenerInfo.Listener.Name),
	)

	// Create base handler - wrap with h2c for cleartext HTTP/2 support
	var handler http.Handler = s
	if listenerInfo.TLSConfig == nil {
		// Enable h2c (HTTP/2 without TLS) for cleartext connections
		h2s := &http2.Server{}
		handler = h2c.NewHandler(s, h2s)
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           handler,
		TLSConfig:         listenerInfo.TLSConfig,
		ReadTimeout:       ServerReadTimeout,
		WriteTimeout:      ServerWriteTimeout,
		IdleTimeout:       ServerIdleTimeout,
		MaxHeaderBytes:    MaxHeaderBytes,
		ReadHeaderTimeout: ServerReadHeaderTimeout,
	}

	// Enable HTTP/2 for TLS connections
	if listenerInfo.TLSConfig != nil {
		if err := http2.ConfigureServer(server, &http2.Server{}); err != nil {
			return fmt.Errorf("failed to configure HTTP/2: %w", err)
		}
	}

	s.servers[port] = server

	// Start server in goroutine
	go func() {
		var err error
		if listenerInfo.TLSConfig != nil {
			// Start HTTPS listener with HTTP/2
			// Note: We pass empty cert/key files because TLSConfig already has certificates
			err = server.ListenAndServeTLS("", "")
		} else {
			// Start HTTP listener with h2c support
			err = server.ListenAndServe()
		}

		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("Server error",
				zap.Int32("port", port),
				zap.String("protocol", protocol),
				zap.Error(err),
			)
		}
	}()

	return nil
}

// startHTTP3Listener starts an HTTP/3 listener on the specified port
func (s *HTTPServer) startHTTP3Listener(ctx context.Context, port int32, listenerInfo *ListenerInfo) error {
	// HTTP/3 requires TLS
	if listenerInfo.TLSConfig == nil {
		return fmt.Errorf("HTTP/3 requires TLS configuration")
	}

	s.logger.Info("Starting HTTP/3 listener",
		zap.Int32("port", port),
		zap.String("gateway", listenerInfo.Gateway),
		zap.String("listener", listenerInfo.Listener.Name),
	)

	// Get QUIC configuration, use defaults if not provided
	quicConfig := listenerInfo.Listener.Quic
	if quicConfig == nil {
		quicConfig = &pb.QUICConfig{
			MaxIdleTimeout: "30s",
			MaxBiStreams:   HTTP3DefaultMaxBiStreams,
			MaxUniStreams:  HTTP3DefaultMaxUniStreams,
			Enable_0Rtt:    true,
		}
	}

	// Create HTTP/3 server
	http3Server := NewHTTP3Server(
		s.logger,
		port,
		listenerInfo.TLSConfig,
		quicConfig,
		s, // Use HTTPServer as handler for routing
	)

	s.http3servers[port] = http3Server

	// Start server in goroutine
	go func() {
		if err := http3Server.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("HTTP/3 server error",
				zap.Int32("port", port),
				zap.Error(err),
			)
		}
	}()

	// Add Alt-Svc header advertising to corresponding HTTP/2 listener
	// This allows clients to upgrade to HTTP/3
	s.enableAltSvcAdvertising(port)

	return nil
}

// enableAltSvcAdvertising enables Alt-Svc header advertising for HTTP/3 on the corresponding HTTP/2 port
func (s *HTTPServer) enableAltSvcAdvertising(_ int32) {
	// The Alt-Svc header is added in the ServeHTTP method via addAltSvcHeader
}

// addAltSvcHeader adds the Alt-Svc header to advertise HTTP/3 availability
func (s *HTTPServer) addAltSvcHeader(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if there's an HTTP/3 server on any port
	for port, h3srv := range s.http3servers {
		// Use SetQuicHeaders if available for standard-compliant Alt-Svc
		if err := h3srv.SetQuicHeaders(w.Header()); err != nil {
			// Fallback to manual header
			altSvc := fmt.Sprintf("h3=\":%d\"; ma=%d", port, HTTP3AltSvcMaxAge)
			w.Header().Set("Alt-Svc", altSvc)
		}
		break // Only advertise one HTTP/3 endpoint for now
	}
}
