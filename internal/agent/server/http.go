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
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/config"
	"github.com/piwi3910/novaedge/internal/agent/router"
)

// HTTPServer manages HTTP listeners and routing
type HTTPServer struct {
	logger  *zap.Logger
	mu      sync.RWMutex
	servers map[int32]*http.Server // Port -> Server
	router  *router.Router
}

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(logger *zap.Logger) *HTTPServer {
	return &HTTPServer{
		logger:  logger,
		servers: make(map[int32]*http.Server),
		router:  router.NewRouter(logger),
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

	// Collect ports we need to listen on from VIP assignments
	portsNeeded := make(map[int32]bool)
	for _, vip := range snapshot.VipAssignments {
		if vip.IsActive {
			for _, port := range vip.Ports {
				portsNeeded[port] = true
			}
		}
	}

	// Stop servers on ports we no longer need
	for port, server := range s.servers {
		if !portsNeeded[port] {
			s.logger.Info("Stopping HTTP listener on unused port",
				zap.Int32("port", port),
			)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			server.Shutdown(ctx)
			cancel()
			delete(s.servers, port)
		}
	}

	// Start servers on new ports
	for port := range portsNeeded {
		if _, exists := s.servers[port]; !exists {
			if err := s.startListener(port); err != nil {
				s.logger.Error("Failed to start listener",
					zap.Int32("port", port),
					zap.Error(err),
				)
				// Don't fail the whole config update, continue with other ports
				continue
			}
		}
	}

	s.logger.Info("HTTP server configuration applied successfully",
		zap.Int("active_ports", len(s.servers)),
	)

	return nil
}

// startListener starts an HTTP listener on the specified port
func (s *HTTPServer) startListener(port int32) error {
	s.logger.Info("Starting HTTP listener", zap.Int32("port", port))

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      s,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	s.servers[port] = server

	// Start server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server error",
				zap.Int32("port", port),
				zap.Error(err),
			)
		}
	}()

	return nil
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
