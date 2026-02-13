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
	proto "github.com/piwi3910/novaedge/internal/proto/gen"
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
	ocspStapler      *OCSPStapler   // OCSP stapling manager
	cachedAltSvc     atomic.Value   // stores string - cached Alt-Svc header value
	drainManager     *DrainManager  // Manages graceful connection draining on config reload
}

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(logger *zap.Logger) *HTTPServer {
	return &HTTPServer{
		logger:       logger,
		servers:      make(map[int32]*http.Server),
		http3servers: make(map[int32]*HTTP3Server),
		listeners:    make(map[int32]*ListenerInfo),
		router:       router.NewRouter(logger),
		ocspStapler:  NewOCSPStapler(logger),
		drainManager: NewDrainManager(logger, DefaultDrainTimeout),
	}
}

// Start starts the HTTP server (placeholder for now)
func (s *HTTPServer) Start(ctx context.Context) error {
	s.logger.Info("HTTP server started, waiting for configuration")
	<-ctx.Done()
	return ctx.Err()
}

// DrainManager returns the server's DrainManager instance for external use.
func (s *HTTPServer) DrainManager() *DrainManager {
	return s.drainManager
}

// ApplyConfig applies a new configuration snapshot. If there are active
// connections, it initiates a graceful drain on the old configuration
// before switching to the new one, allowing in-flight requests to complete.
func (s *HTTPServer) ApplyConfig(ctx context.Context, snapshot *config.Snapshot) error {
	// Drain active connections from the previous configuration before
	// acquiring the lock and swapping. This allows in-flight requests
	// to finish while we prepare the new configuration.
	if s.drainManager.ActiveConnections() > 0 {
		s.logger.Info("Active connections detected, initiating graceful drain before config swap",
			zap.Int64("active_connections", s.drainManager.ActiveConnections()),
		)
		s.drainManager.StartDrain(ctx)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Applying HTTP server configuration",
		zap.String("version", snapshot.Version),
	)

	// Update router with new configuration
	if err := s.router.ApplyConfig(ctx, snapshot); err != nil {
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
				// Check for mTLS and OCSP extensions
				gatewayKey := fmt.Sprintf("%s/%s", gateway.Namespace, gateway.Name)
				ext := snapshot.GetListenerExtensions(gatewayKey, listener.Name)
				var clientAuth *proto.ClientAuthConfig
				var enableOCSP bool
				if ext != nil {
					clientAuth = ext.ClientAuth
					enableOCSP = ext.OCSPStapling
				}

				tlsConfig, err := s.createTLSConfigWithMTLS(listener, clientAuth, enableOCSP)
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

			// Configure PROXY protocol if extensions specify it
			if ext := snapshot.GetListenerExtensions(
				fmt.Sprintf("%s/%s", gateway.Namespace, gateway.Name),
				listener.Name,
			); ext != nil && ext.ProxyProtocol != nil && ext.ProxyProtocol.Enabled {
				listenerInfo.ProxyProtocol = ext.ProxyProtocol
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
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := server.Shutdown(shutdownCtx); err != nil {
				s.logger.Error("Error shutting down HTTP listener on unused port",
					zap.Int32("port", port),
					zap.Error(err),
				)
			}
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
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := server.Shutdown(shutdownCtx); err != nil {
				s.logger.Error("Error shutting down HTTP/3 listener on unused port",
					zap.Int32("port", port),
					zap.Error(err),
				)
			}
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
			if err := s.startListener(ctx, port, listenerInfo); err != nil {
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

	// Update cached Alt-Svc header value for lock-free reads
	s.updateAltSvcCache()

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

	// Track in-flight request for shutdown and drain awareness
	s.inFlightRequests.Add(1)
	defer s.inFlightRequests.Done()

	s.drainManager.TrackConnection()
	defer s.drainManager.ReleaseConnection()

	// If draining, signal HTTP/1.x clients to close the connection after
	// this response so subsequent requests use the new configuration.
	if s.drainManager.IsDraining() && !r.ProtoAtLeast(2, 0) {
		w.Header().Set("Connection", "close")
	}

	startTime := time.Now()

	// Add Alt-Svc header to advertise HTTP/3 availability
	s.addAltSvcHeader(w, r)

	// Inject client certificate headers if mTLS is active
	injectClientCertHeaders(r)

	if ce := s.logger.Check(zap.DebugLevel, "Incoming request"); ce != nil {
		ce.Write(
			zap.String("method", r.Method),
			zap.String("host", r.Host),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
		)
	}

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
	s.updateAltSvcCache()

	if shutdownErr != nil {
		return shutdownErr
	}

	s.logger.Info("HTTP server shutdown completed successfully")
	return nil
}
