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
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

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
}

// NewPool creates a new connection pool
func NewPool(cluster *pb.Cluster, endpoints []*pb.Endpoint, logger *zap.Logger) *Pool {
	// Create HTTP transport with connection pooling
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   time.Duration(cluster.ConnectTimeoutMs) * time.Millisecond,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       time.Duration(cluster.IdleTimeoutMs) * time.Millisecond,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// Create context for health checker
	ctx, cancel := context.WithCancel(context.Background())

	pool := &Pool{
		logger:    logger,
		cluster:   cluster,
		endpoints: endpoints,
		transport: transport,
		proxies:   make(map[string]*httputil.ReverseProxy),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Create and start health checker
	pool.healthChecker = health.NewHealthChecker(cluster, endpoints, logger)
	pool.healthChecker.Start(ctx)

	// Create reverse proxies for each endpoint
	pool.createProxies()

	return pool
}

// UpdateEndpoints updates the pool with new endpoints
func (p *Pool) UpdateEndpoints(endpoints []*pb.Endpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.endpoints = endpoints
	p.createProxies()

	// Update health checker with new endpoints
	if p.healthChecker != nil {
		p.healthChecker.UpdateEndpoints(endpoints)
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

		// Custom error handler
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			p.logger.Error("Proxy error",
				zap.String("backend", key),
				zap.Error(err),
			)
			w.WriteHeader(http.StatusBadGateway)
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
