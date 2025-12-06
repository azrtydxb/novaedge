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
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/config"
	"github.com/piwi3910/novaedge/internal/agent/router"
)

// HTTPServer manages HTTP/HTTPS/HTTP3 listeners and routing
type HTTPServer struct {
	logger           *zap.Logger
	mu               sync.RWMutex
	servers          map[int32]*http.Server  // Port -> HTTP/1.1 or HTTP/2 Server
	http3servers     map[int32]*HTTP3Server  // Port -> HTTP/3 Server
	listeners        map[int32]*ListenerInfo // Port -> Listener config
	router           *router.Router
	inFlightRequests sync.WaitGroup // Track in-flight requests for graceful shutdown
	shuttingDown     atomic.Bool    // Flag to indicate shutdown in progress
}

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(logger *zap.Logger) *HTTPServer {
	return &HTTPServer{
		logger:       logger,
		servers:      make(map[int32]*http.Server),
		http3servers: make(map[int32]*HTTP3Server),
		listeners:    make(map[int32]*ListenerInfo),
		router:       router.NewRouter(logger),
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
			if listener.Tls != nil || len(listener.TlsCertificates) > 0 {
				tlsConfig, err := s.createTLSConfigWithSNI(listener)
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
			s.logger.Info("Stopping HTTP/1.1 or HTTP/2 listener on unused port",
				zap.Int32("port", port),
			)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			server.Shutdown(ctx)
			cancel()
			delete(s.servers, port)
			delete(s.listeners, port)
		}
	}

	// Stop HTTP/3 servers on ports we no longer need
	for port, server := range s.http3servers {
		if _, needed := newListeners[port]; !needed {
			s.logger.Info("Stopping HTTP/3 listener on unused port",
				zap.Int32("port", port),
			)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			server.Shutdown(ctx)
			cancel()
			delete(s.http3servers, port)
		}
	}

	// Start new listeners
	for port, listenerInfo := range newListeners {
		// Check if listener already exists (either HTTP or HTTP/3)
		_, httpExists := s.servers[port]
		_, http3Exists := s.http3servers[port]
		if !httpExists && !http3Exists {
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
		zap.Int("active_http_listeners", len(s.servers)),
		zap.Int("active_http3_listeners", len(s.http3servers)),
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

// ServeHTTP handles HTTP requests (implements http.Handler)
func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if shutdown is in progress
	if s.shuttingDown.Load() {
		http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
		return
	}

	// Track in-flight request
	s.inFlightRequests.Add(1)
	defer s.inFlightRequests.Done()

	startTime := time.Now()

	// Add Alt-Svc header to advertise HTTP/3 availability
	s.addAltSvcHeader(w, r)

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

// Shutdown gracefully shuts down all HTTP servers with timeout
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Mark as shutting down to reject new requests
	s.shuttingDown.Store(true)

	s.logger.Info("Shutting down HTTP servers",
		zap.Int("http_servers", len(s.servers)),
		zap.Int("http3_servers", len(s.http3servers)))

	// Determine shutdown timeout from configuration or use default
	shutdownTimeout := 30 * time.Second

	// Check if any listener has a custom graceful shutdown timeout
	for _, listenerInfo := range s.listeners {
		if listenerInfo.Listener.GracefulShutdownTimeoutMs > 0 {
			timeout := time.Duration(listenerInfo.Listener.GracefulShutdownTimeoutMs) * time.Millisecond
			if timeout > shutdownTimeout {
				shutdownTimeout = timeout
			}
		}
	}

	// Create context with timeout for shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	s.logger.Info("Graceful shutdown initiated",
		zap.Duration("timeout", shutdownTimeout))

	// Shutdown HTTP/1.1 and HTTP/2 servers
	var wg sync.WaitGroup
	errors := make(chan error, len(s.servers)+len(s.http3servers))

	for port, server := range s.servers {
		wg.Add(1)
		go func(port int32, srv *http.Server) {
			defer wg.Done()
			s.logger.Info("Shutting down HTTP listener", zap.Int32("port", port))
			if err := srv.Shutdown(shutdownCtx); err != nil {
				s.logger.Error("Error shutting down HTTP listener",
					zap.Int32("port", port),
					zap.Error(err))
				errors <- fmt.Errorf("error shutting down HTTP port %d: %w", port, err)
			}
		}(port, server)
	}

	// Shutdown HTTP/3 servers
	for port, http3Server := range s.http3servers {
		wg.Add(1)
		go func(port int32, srv *HTTP3Server) {
			defer wg.Done()
			s.logger.Info("Shutting down HTTP/3 listener", zap.Int32("port", port))
			if err := srv.Shutdown(shutdownCtx); err != nil {
				s.logger.Error("Error shutting down HTTP/3 listener",
					zap.Int32("port", port),
					zap.Error(err))
				errors <- fmt.Errorf("error shutting down HTTP/3 port %d: %w", port, err)
			}
		}(port, http3Server)
	}

	// Wait for all server shutdowns to complete
	wg.Wait()
	close(errors)

	// Wait for in-flight requests to complete or timeout
	done := make(chan struct{})
	go func() {
		s.inFlightRequests.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("All in-flight requests completed")
	case <-shutdownCtx.Done():
		s.logger.Warn("Shutdown timeout reached, some requests may have been interrupted")
	}

	// Collect any errors
	var shutdownErr error
	for err := range errors {
		if shutdownErr == nil {
			shutdownErr = err
		} else {
			s.logger.Error("Additional shutdown error", zap.Error(err))
		}
	}

	s.servers = make(map[int32]*http.Server)
	s.http3servers = make(map[int32]*HTTP3Server)

	if shutdownErr != nil {
		return shutdownErr
	}

	s.logger.Info("HTTP server shutdown completed successfully")
	return nil
}
