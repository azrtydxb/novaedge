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
	"net"
	"net/http"

	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

var (
	errHTTP3RequiresTLSConfiguration = errors.New("HTTP/3 requires TLS configuration")
)

// ListenerInfo contains information about a configured listener
type ListenerInfo struct {
	Gateway       string
	Listener      *pb.Listener
	TLSConfig     *tls.Config
	ProxyProtocol *pb.ProxyProtocolConfig // PROXY protocol configuration
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

	// Start server in goroutine with optional PROXY protocol wrapping
	go func() {
		var err error
		addr := fmt.Sprintf(":%d", port)
		switch {
		case listenerInfo.ProxyProtocol != nil && listenerInfo.ProxyProtocol.Enabled:
			// Use custom listener with PROXY protocol support
			err = s.startWithProxyProtocol(server, port, listenerInfo)
		default:
			// Create listener with SO_REUSEPORT for better multi-core scalability
			lc := NewReusePortListenConfig()
			ln, listenErr := lc.Listen(context.Background(), "tcp", addr)
			if listenErr != nil {
				s.logger.Error("Failed to create listener",
					zap.Int32("port", port),
					zap.Error(listenErr),
				)
				return
			}
			if listenerInfo.TLSConfig != nil {
				ln = tls.NewListener(ln, listenerInfo.TLSConfig)
			}
			err = server.Serve(ln)
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
		return errHTTP3RequiresTLSConfiguration
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

// addAltSvcHeader adds the Alt-Svc header to advertise HTTP/3 availability.
// Uses atomic.Value for lock-free reads on the hot path.
func (s *HTTPServer) addAltSvcHeader(w http.ResponseWriter, _ *http.Request) {
	if altSvc, ok := s.cachedAltSvc.Load().(string); ok && altSvc != "" {
		w.Header().Set("Alt-Svc", altSvc)
	}
}

// updateAltSvcCache rebuilds the cached Alt-Svc header value from current HTTP/3 servers.
// Must be called under s.mu lock whenever http3servers changes.
func (s *HTTPServer) updateAltSvcCache() {
	for port, h3srv := range s.http3servers {
		// Try to get the header from the QUIC server
		tempHeader := make(http.Header)
		if err := h3srv.SetQuicHeaders(tempHeader); err == nil {
			if altSvc := tempHeader.Get("Alt-Svc"); altSvc != "" {
				s.cachedAltSvc.Store(altSvc)
				return
			}
		}
		// Fallback to manual header
		altSvc := fmt.Sprintf("h3=\":%d\"; ma=%d", port, HTTP3AltSvcMaxAge)
		s.cachedAltSvc.Store(altSvc)
		return
	}
	// No HTTP/3 servers, clear cache
	s.cachedAltSvc.Store("")
}

// startWithProxyProtocol starts a server with PROXY protocol listener wrapping
func (s *HTTPServer) startWithProxyProtocol(server *http.Server, port int32, listenerInfo *ListenerInfo) error {
	addr := fmt.Sprintf(":%d", port)
	lc := NewReusePortListenConfig()
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// Wrap with PROXY protocol listener
	ppListener, err := NewProxyProtocolListener(
		ln,
		listenerInfo.ProxyProtocol.Version,
		listenerInfo.ProxyProtocol.TrustedCidrs,
		s.logger,
	)
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("failed to create PROXY protocol listener: %w", err)
	}

	s.logger.Info("PROXY protocol enabled on listener",
		zap.Int32("port", port),
		zap.Int32("version", listenerInfo.ProxyProtocol.Version),
		zap.Strings("trusted_cidrs", listenerInfo.ProxyProtocol.TrustedCidrs),
	)

	var listener net.Listener = ppListener
	if listenerInfo.TLSConfig != nil {
		listener = tls.NewListener(ppListener, listenerInfo.TLSConfig)
	}

	return server.Serve(listener)
}
