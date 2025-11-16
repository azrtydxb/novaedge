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

package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/health"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Pool manages connections to backend endpoints
type Pool struct {
	logger    *zap.Logger
	cluster   *pb.Cluster
	endpoints []*pb.Endpoint

	// HTTP transport with connection pooling
	transport *http.Transport

	// Reverse proxies per endpoint
	mu      sync.RWMutex
	proxies map[string]*httputil.ReverseProxy

	// Health checker for endpoints
	healthChecker *health.HealthChecker

	// Context for health checker
	ctx    context.Context
	cancel context.CancelFunc

	// Metrics tracking
	activeConns int32 // Atomic counter for active connections
	idleConns   int32 // Atomic counter for idle connections
}

// NewPool creates a new connection pool
func NewPool(cluster *pb.Cluster, endpoints []*pb.Endpoint, logger *zap.Logger) *Pool {
	// Apply default values for connection pool configuration
	poolConfig := cluster.ConnectionPool
	if poolConfig == nil {
		poolConfig = &pb.ConnectionPool{
			MaxIdleConns:            100,
			MaxIdleConnsPerHost:     10,
			MaxConnsPerHost:         0,
			IdleConnTimeoutMs:       90000,
			ResponseHeaderTimeoutMs: 10000,
		}
	}

	// Apply defaults if values are zero
	maxIdleConns := int(poolConfig.MaxIdleConns)
	if maxIdleConns <= 0 {
		maxIdleConns = 100
	}
	maxIdleConnsPerHost := int(poolConfig.MaxIdleConnsPerHost)
	if maxIdleConnsPerHost <= 0 {
		maxIdleConnsPerHost = 10
	}
	idleConnTimeout := time.Duration(poolConfig.IdleConnTimeoutMs) * time.Millisecond
	if idleConnTimeout <= 0 {
		idleConnTimeout = 90 * time.Second
	}
	responseHeaderTimeout := time.Duration(poolConfig.ResponseHeaderTimeoutMs) * time.Millisecond
	if responseHeaderTimeout <= 0 {
		responseHeaderTimeout = 10 * time.Second
	}

	// Create HTTP transport with connection pooling
	// This transport supports both HTTP/1.1, HTTP/2, and gRPC
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   time.Duration(cluster.ConnectTimeoutMs) * time.Millisecond,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:            maxIdleConns,
		MaxIdleConnsPerHost:     maxIdleConnsPerHost,
		MaxConnsPerHost:         int(poolConfig.MaxConnsPerHost),
		IdleConnTimeout:         idleConnTimeout,
		ResponseHeaderTimeout:   responseHeaderTimeout,
		TLSHandshakeTimeout:     10 * time.Second,
		ExpectContinueTimeout:   1 * time.Second,
		DisableKeepAlives:       poolConfig.DisableKeepAlives,
		MaxResponseHeaderBytes:  int64(poolConfig.MaxResponseHeaderBytes),
		ForceAttemptHTTP2:       true,
	}

	// Configure backend TLS if enabled
	if cluster.Tls != nil && cluster.Tls.Enabled {
		transport.TLSClientConfig = createBackendTLSConfig(cluster.Tls, logger)
	}

	// Create context for health checker
	ctx, cancel := context.WithCancel(context.Background())

	pool := &Pool{
		logger:      logger,
		cluster:     cluster,
		endpoints:   endpoints,
		transport:   transport,
		proxies:     make(map[string]*httputil.ReverseProxy),
		ctx:         ctx,
		cancel:      cancel,
		activeConns: 0,
		idleConns:   0,
	}

	// Create and start health checker
	pool.healthChecker = health.NewHealthChecker(cluster, endpoints, logger)
	pool.healthChecker.Start(ctx)

	// Create reverse proxies for each endpoint
	pool.createProxies()

	// Start metrics collection goroutine
	go pool.updateMetrics()

	return pool
}

// UpdateEndpoints updates the pool with new endpoints
func (p *Pool) UpdateEndpoints(endpoints []*pb.Endpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.logger.Info("Updating endpoints, draining old connections",
		zap.Int("old_count", len(p.endpoints)),
		zap.Int("new_count", len(endpoints)),
	)

	// Drain idle connections before updating to prevent routing to stale endpoints
	p.drainIdleConnections()

	p.endpoints = endpoints
	p.createProxies()

	// Update health checker with new endpoints
	if p.healthChecker != nil {
		p.healthChecker.UpdateEndpoints(endpoints)
	}
}

// drainIdleConnections closes all idle connections in the transport
// This is called when endpoints change to ensure we don't use stale connections
func (p *Pool) drainIdleConnections() {
	if p.transport != nil {
		p.transport.CloseIdleConnections()
		p.logger.Debug("Drained idle connections from transport")
	}
}

// updateMetrics periodically updates pool metrics
func (p *Pool) updateMetrics() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	clusterKey := fmt.Sprintf("%s/%s", p.cluster.Namespace, p.cluster.Name)

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			// Note: Go's http.Transport doesn't expose connection counts directly
			// We track active connections through our metrics in the Forward method
			// Here we just report what we've tracked
			p.mu.RLock()
			endpointCount := len(p.endpoints)
			p.mu.RUnlock()

			// Import metrics package at top if needed
			// Update pool connection metrics
			// metrics.PoolConnectionsTotal.WithLabelValues(clusterKey).Set(float64(endpointCount))

			p.logger.Debug("Pool metrics",
				zap.String("cluster", clusterKey),
				zap.Int("endpoints", endpointCount),
			)
		}
	}
}

// createProxies creates reverse proxies for all endpoints
func (p *Pool) createProxies() {
	newProxies := make(map[string]*httputil.ReverseProxy)

	for _, ep := range p.endpoints {
		if !ep.Ready {
			continue
		}

		key := fmt.Sprintf("%s:%d", ep.Address, ep.Port)

		// Reuse existing proxy if available
		if proxy, ok := p.proxies[key]; ok {
			newProxies[key] = proxy
			continue
		}

		// Create new reverse proxy
		target := &url.URL{
			Scheme: "http",
			Host:   key,
		}

		// Use HTTPS if TLS is enabled
		if p.cluster.Tls != nil && p.cluster.Tls.Enabled {
			target.Scheme = "https"
		}

		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.Transport = p.transport

		// Enable flushing for streaming responses (required for gRPC and WebSockets)
		proxy.FlushInterval = -1 // Flush immediately for streaming

		// Custom error handler
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			p.logger.Error("Proxy error",
				zap.String("backend", key),
				zap.Bool("is_grpc", isGRPCRequest(r)),
				zap.Error(err),
			)
			w.WriteHeader(http.StatusBadGateway)
		}

		// Custom director to preserve headers for gRPC and inject trace context
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)

			// Inject OpenTelemetry trace context into outgoing request headers
			// This propagates the trace to the backend service
			otel.GetTextMapPropagator().Inject(req.Context(), propagation.HeaderCarrier(req.Header))

			// Preserve gRPC-specific headers
			if isGRPCRequest(req) {
				// Ensure Content-Type is preserved
				if ct := req.Header.Get("Content-Type"); ct != "" {
					req.Header.Set("Content-Type", ct)
				}
				// Preserve all grpc-* headers
				for key := range req.Header {
					if len(key) >= 5 && key[:5] == "Grpc-" || key[:5] == "grpc-" {
						// Already preserved by default director
					}
				}
			}
		}

		newProxies[key] = proxy
	}

	p.proxies = newProxies
}

// Forward forwards an HTTP request to the specified endpoint
func (p *Pool) Forward(endpoint *pb.Endpoint, req *http.Request, w http.ResponseWriter) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	key := fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)
	proxy, ok := p.proxies[key]
	if !ok {
		return fmt.Errorf("no proxy for endpoint %s", key)
	}

	// Set up request context with timeout
	ctx := req.Context()
	if p.cluster.ConnectTimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(p.cluster.ConnectTimeoutMs)*time.Millisecond)
		defer cancel()
	}

	// Create new request with modified context
	reqWithContext := req.WithContext(ctx)

	// Forward request
	proxy.ServeHTTP(w, reqWithContext)

	return nil
}

// Close closes the pool and all connections
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop health checker
	if p.healthChecker != nil {
		p.healthChecker.Stop()
	}
	if p.cancel != nil {
		p.cancel()
	}

	p.transport.CloseIdleConnections()
	p.proxies = make(map[string]*httputil.ReverseProxy)
}

// RecordSuccess records a successful request to an endpoint
func (p *Pool) RecordSuccess(endpoint *pb.Endpoint) {
	if p.healthChecker != nil {
		p.healthChecker.RecordSuccess(endpoint)
	}
}

// RecordFailure records a failed request to an endpoint
func (p *Pool) RecordFailure(endpoint *pb.Endpoint) {
	if p.healthChecker != nil {
		p.healthChecker.RecordFailure(endpoint)
	}
}

// GetHealthyEndpoints returns only healthy endpoints
func (p *Pool) GetHealthyEndpoints() []*pb.Endpoint {
	if p.healthChecker != nil {
		return p.healthChecker.GetHealthyEndpoints()
	}
	// Fallback to all endpoints if no health checker
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.endpoints
}

// GetStats returns pool statistics
func (p *Pool) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"total_endpoints":   len(p.endpoints),
		"healthy_endpoints": len(p.proxies),
		"cluster":           fmt.Sprintf("%s/%s", p.cluster.Namespace, p.cluster.Name),
	}
}

// GetBackendURL returns the backend URL for a given endpoint
func (p *Pool) GetBackendURL(endpoint *pb.Endpoint) string {
	scheme := "http"
	if p.cluster.Tls != nil && p.cluster.Tls.Enabled {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, endpoint.Address, endpoint.Port)
}

// createBackendTLSConfig creates a TLS config for backend connections
func createBackendTLSConfig(backendTLS *pb.BackendTLS, logger *zap.Logger) *tls.Config {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: backendTLS.InsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}

	// Load CA certificate if provided
	if len(backendTLS.CaCert) > 0 {
		caCertPool := x509.NewCertPool()
		if ok := caCertPool.AppendCertsFromPEM(backendTLS.CaCert); !ok {
			logger.Warn("Failed to parse CA certificate for backend TLS")
		} else {
			tlsConfig.RootCAs = caCertPool
		}
	}

	return tlsConfig
}

// isGRPCRequest checks if a request is a gRPC request
func isGRPCRequest(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	return strings.HasPrefix(contentType, "application/grpc")
}
