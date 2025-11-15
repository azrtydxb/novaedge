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
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/piwi3910/novaedge/internal/agent/config"
	"github.com/piwi3910/novaedge/internal/agent/metrics"
	"github.com/piwi3910/novaedge/internal/agent/router"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// ListenerInfo contains information about a configured listener
type ListenerInfo struct {
	Gateway   string
	Listener  *pb.Listener
	TLSConfig *tls.Config
}

// HTTPServer manages HTTP/HTTPS listeners and routing
type HTTPServer struct {
	logger    *zap.Logger
	mu        sync.RWMutex
	servers   map[int32]*http.Server  // Port -> Server
	listeners map[int32]*ListenerInfo // Port -> Listener config
	router    *router.Router
}

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(logger *zap.Logger) *HTTPServer {
	return &HTTPServer{
		logger:    logger,
		servers:   make(map[int32]*http.Server),
		listeners: make(map[int32]*ListenerInfo),
		router:    router.NewRouter(logger),
	}
}

// Start starts the HTTP server (placeholder for now)
func (s *HTTPServer) Start(ctx context.Context) error {
	s.logger.Info("HTTP server started, waiting for configuration")
	<-ctx.Done()
	return ctx.Err()
}

// ApplyConfig applies a new configuration snapshot
func (s *HTTPServer) ApplyConfig(snapshot *config.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Applying HTTP server configuration",
		zap.String("version", snapshot.Version),
	)

	// Update router with new configuration
	if err := s.router.ApplyConfig(snapshot); err != nil {
		return fmt.Errorf("failed to update router: %w", err)
	}

	// Build listener configurations from gateways
	newListeners := make(map[int32]*ListenerInfo)
	for _, gateway := range snapshot.Gateways {
		for _, listener := range gateway.Listeners {
			// Only configure listeners on active VIPs
			if !s.isVIPActive(snapshot, gateway.VipRef, listener.Port) {
				continue
			}

			listenerInfo := &ListenerInfo{
				Gateway:  fmt.Sprintf("%s/%s", gateway.Namespace, gateway.Name),
				Listener: listener,
			}

			// Create TLS config if listener uses TLS
			if listener.Tls != nil {
				tlsConfig, err := s.createTLSConfig(listener.Tls, listener.Hostnames)
				if err != nil {
					s.logger.Error("Failed to create TLS config",
						zap.String("gateway", listenerInfo.Gateway),
						zap.String("listener", listener.Name),
						zap.Error(err),
					)
					continue
				}
				listenerInfo.TLSConfig = tlsConfig
			}

			newListeners[listener.Port] = listenerInfo
		}
	}

	// Stop servers on ports we no longer need
	for port, server := range s.servers {
		if _, needed := newListeners[port]; !needed {
			s.logger.Info("Stopping listener on unused port",
				zap.Int32("port", port),
			)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			server.Shutdown(ctx)
			cancel()
			delete(s.servers, port)
			delete(s.listeners, port)
		}
	}

	// Start new listeners
	for port, listenerInfo := range newListeners {
		if _, exists := s.servers[port]; !exists {
			if err := s.startListener(port, listenerInfo); err != nil {
				s.logger.Error("Failed to start listener",
					zap.Int32("port", port),
					zap.String("gateway", listenerInfo.Gateway),
					zap.Error(err),
				)
				continue
			}
		}
	}

	s.listeners = newListeners

	s.logger.Info("HTTP server configuration applied successfully",
		zap.Int("active_listeners", len(s.servers)),
	)

	return nil
}

// isVIPActive checks if a VIP is active for the given port
func (s *HTTPServer) isVIPActive(snapshot *config.Snapshot, vipRef string, port int32) bool {
	for _, vip := range snapshot.VipAssignments {
		if vip.VipName == vipRef && vip.IsActive {
			for _, vipPort := range vip.Ports {
				if vipPort == port {
					return true
				}
			}
		}
	}
	return false
}

// startListener starts an HTTP or HTTPS listener on the specified port
func (s *HTTPServer) startListener(port int32, listenerInfo *ListenerInfo) error {
	protocol := "HTTP"
	if listenerInfo.TLSConfig != nil {
		protocol = "HTTPS (HTTP/2)"
	} else {
		protocol = "HTTP (HTTP/2 h2c)"
	}

	s.logger.Info("Starting listener",
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
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      handler,
		TLSConfig:    listenerInfo.TLSConfig,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
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

		if err != nil && err != http.ErrServerClosed {
			s.logger.Error("Server error",
				zap.Int32("port", port),
				zap.String("protocol", protocol),
				zap.Error(err),
			)
		}
	}()

	return nil
}

// createTLSConfig creates a tls.Config from protobuf TLS configuration
func (s *HTTPServer) createTLSConfig(tlsConfig *pb.TLSConfig, hostnames []string) (*tls.Config, error) {
	// Parse certificate and key
	cert, err := tls.X509KeyPair(tlsConfig.Cert, tlsConfig.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   s.parseTLSVersion(tlsConfig.MinVersion),
		CipherSuites: s.parseCipherSuites(tlsConfig.CipherSuites),
	}

	// Enable SNI for multiple hostnames
	if len(hostnames) > 0 {
		config.GetCertificate = func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// Track TLS metrics
			metrics.TLSHandshakes.Inc()

			// Simple SNI: return the cert if it matches any hostname
			// In production, you'd want more sophisticated certificate selection
			for _, hostname := range hostnames {
				if clientHello.ServerName == hostname || clientHello.ServerName == "" {
					return &cert, nil
				}
			}

			// Return default certificate if no match
			return &cert, nil
		}
	}

	return config, nil
}

// parseTLSVersion converts string TLS version to constant
func (s *HTTPServer) parseTLSVersion(version string) uint16 {
	switch version {
	case "TLS1.2":
		return tls.VersionTLS12
	case "TLS1.3":
		return tls.VersionTLS13
	default:
		return tls.VersionTLS12 // Default to TLS 1.2
	}
}

// parseCipherSuites converts cipher suite names to constants
func (s *HTTPServer) parseCipherSuites(suites []string) []uint16 {
	if len(suites) == 0 {
		return nil // Use Go's default secure cipher suites
	}

	// Map of cipher suite names to constants
	cipherMap := map[string]uint16{
		"TLS_AES_128_GCM_SHA256":                        tls.TLS_AES_128_GCM_SHA256,
		"TLS_AES_256_GCM_SHA384":                        tls.TLS_AES_256_GCM_SHA384,
		"TLS_CHACHA20_POLY1305_SHA256":                  tls.TLS_CHACHA20_POLY1305_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":         tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":         tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256":       tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384":       tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256": tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	}

	var result []uint16
	for _, name := range suites {
		if id, ok := cipherMap[name]; ok {
			result = append(result, id)
		}
	}

	return result
}

// ServeHTTP handles HTTP requests (implements http.Handler)
func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	s.logger.Debug("Incoming request",
		zap.String("method", r.Method),
		zap.String("host", r.Host),
		zap.String("path", r.URL.Path),
		zap.String("remote_addr", r.RemoteAddr),
	)

	// Route the request
	s.router.ServeHTTP(w, r)

	// Log request completion
	duration := time.Since(startTime)
	s.logger.Info("Request completed",
		zap.String("method", r.Method),
		zap.String("host", r.Host),
		zap.String("path", r.URL.Path),
		zap.Duration("duration", duration),
	)
}

// Shutdown gracefully shuts down all HTTP servers
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Shutting down HTTP servers", zap.Int("count", len(s.servers)))

	var wg sync.WaitGroup
	errors := make(chan error, len(s.servers))

	for port, server := range s.servers {
		wg.Add(1)
		go func(port int32, srv *http.Server) {
			defer wg.Done()
			s.logger.Info("Shutting down listener", zap.Int32("port", port))
			if err := srv.Shutdown(ctx); err != nil {
				errors <- fmt.Errorf("error shutting down port %d: %w", port, err)
			}
		}(port, server)
	}

	wg.Wait()
	close(errors)

	// Collect any errors
	var shutdownErr error
	for err := range errors {
		if shutdownErr == nil {
			shutdownErr = err
		}
	}

	s.servers = make(map[int32]*http.Server)
	return shutdownErr
}
