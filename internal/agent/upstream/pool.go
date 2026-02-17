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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/health"
	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// DefaultConnectTimeout is the fallback connect timeout when ConnectTimeoutMs is zero or negative.
const DefaultConnectTimeout = 60 * time.Second

// Pool manages connections to backend endpoints
type Pool struct {
	logger    *zap.Logger
	cluster   *pb.Cluster
	endpoints []*pb.Endpoint

	// HTTP transport with connection pooling
	transport *http.Transport

	// Reverse proxies per endpoint - atomic for lock-free reads in Forward()
	proxies atomic.Pointer[map[string]*httputil.ReverseProxy]

	// Mutex only needed for endpoint updates (write path)
	mu sync.Mutex

	// Health checker for endpoints
	healthChecker *health.Checker

	// Outlier detector for statistical endpoint ejection
	outlierDetector *health.OutlierDetector

	// Context for health checker
	ctx    context.Context
	cancel context.CancelFunc

	// Metrics tracking (atomic counters for lock-free reads)
	activeConns int64 // Atomic counter for active connections
	totalConns  int64 // Atomic counter for total connections served
	poolHits    int64 // Atomic counter for proxy cache hits
	poolMisses  int64 // Atomic counter for proxy cache misses

	// Cluster key cached to avoid repeated fmt.Sprintf in hot path
	clusterKey string
}

// NewPool creates a new connection pool
func NewPool(ctx context.Context, cluster *pb.Cluster, endpoints []*pb.Endpoint, logger *zap.Logger) *Pool {
	// Apply default values for connection pool configuration
	poolConfig := cluster.ConnectionPool
	if poolConfig == nil {
		poolConfig = &pb.ConnectionPool{
			MaxIdleConns:            1000,
			MaxIdleConnsPerHost:     100,
			MaxConnsPerHost:         0,
			IdleConnTimeoutMs:       90000,
			ResponseHeaderTimeoutMs: 10000,
		}
	}

	// Apply defaults if values are zero
	maxIdleConns := int(poolConfig.MaxIdleConns)
	if maxIdleConns <= 0 {
		maxIdleConns = 1000
	}
	maxIdleConnsPerHost := int(poolConfig.MaxIdleConnsPerHost)
	if maxIdleConnsPerHost <= 0 {
		maxIdleConnsPerHost = 100
	}
	idleConnTimeout := time.Duration(poolConfig.IdleConnTimeoutMs) * time.Millisecond
	if idleConnTimeout <= 0 {
		idleConnTimeout = 90 * time.Second
	}
	responseHeaderTimeout := time.Duration(poolConfig.ResponseHeaderTimeoutMs) * time.Millisecond
	if responseHeaderTimeout <= 0 {
		responseHeaderTimeout = 10 * time.Second
	}
	writeBufferSize := int(poolConfig.WriteBufferSize)
	if writeBufferSize <= 0 {
		writeBufferSize = 32 * 1024 // Default: 32KB
	}
	readBufferSize := int(poolConfig.ReadBufferSize)
	if readBufferSize <= 0 {
		readBufferSize = 32 * 1024 // Default: 32KB
	}

	// Create HTTP transport with connection pooling
	// This transport supports both HTTP/1.1, HTTP/2, and gRPC
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   connectTimeout(cluster.ConnectTimeoutMs),
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:           maxIdleConns,
		MaxIdleConnsPerHost:    maxIdleConnsPerHost,
		MaxConnsPerHost:        int(poolConfig.MaxConnsPerHost),
		IdleConnTimeout:        idleConnTimeout,
		ResponseHeaderTimeout:  responseHeaderTimeout,
		TLSHandshakeTimeout:    10 * time.Second,
		ExpectContinueTimeout:  1 * time.Second,
		DisableKeepAlives:      poolConfig.DisableKeepAlives,
		MaxResponseHeaderBytes: int64(poolConfig.MaxResponseHeaderBytes),
		ForceAttemptHTTP2:      true,
		WriteBufferSize:        writeBufferSize,
		ReadBufferSize:         readBufferSize,
	}

	// Configure backend TLS if enabled
	clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
	if cluster.Tls != nil && cluster.Tls.Enabled {
		transport.TLSClientConfig = createBackendTLSConfig(cluster.Tls, clusterKey, logger)
	}

	// Create context for health checker derived from parent
	poolCtx, cancel := context.WithCancel(ctx)

	pool := &Pool{
		logger:     logger,
		cluster:    cluster,
		endpoints:  endpoints,
		transport:  transport,
		ctx:        poolCtx,
		cancel:     cancel,
		clusterKey: clusterKey,
	}
	// Initialize atomic proxies
	emptyProxies := make(map[string]*httputil.ReverseProxy)
	pool.proxies.Store(&emptyProxies)

	// Create and start health checker
	pool.healthChecker = health.NewChecker(cluster, endpoints, logger)
	pool.healthChecker.Start(poolCtx)

	// Initialize outlier detector if configured
	if od := cluster.GetOutlierDetection(); od != nil {
		odCfg := health.OutlierDetectionConfig{
			Interval:                 time.Duration(od.IntervalMs) * time.Millisecond,
			BaseEjectionTime:         time.Duration(od.BaseEjectionDurationMs) * time.Millisecond,
			MaxEjectionPercent:       float64(od.MaxEjectionPercent),
			SuccessRateMinHosts:      int(od.SuccessRateMinHosts),
			SuccessRateRequestVolume: int(od.SuccessRateMinRequests),
			SuccessRateStdevFactor:   od.SuccessRateStdevFactor,
			ConsecutiveErrors:        int(od.Consecutive_5XxThreshold),
		}
		if odCfg.Interval == 0 {
			odCfg = health.DefaultOutlierDetectionConfig()
		}
		pool.outlierDetector = health.NewOutlierDetector(cluster.Name, odCfg, logger)
		pool.outlierDetector.Start(poolCtx)
	}

	// Create reverse proxies for each endpoint
	pool.createProxies()

	// Start metrics collection goroutine
	go pool.updateMetrics()

	// Log startup audit message for backends with InsecureSkipVerify
	if cluster.Tls != nil && cluster.Tls.Enabled && cluster.Tls.InsecureSkipVerify {
		logger.Error("SECURITY AUDIT: Pool started for backend with InsecureSkipVerify enabled — "+
			"TLS certificate verification is disabled for all connections to this backend",
			zap.String("backend", clusterKey),
			zap.Int("endpoint_count", len(endpoints)),
		)
	}

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

// updateMetrics periodically updates pool metrics and reports them via Prometheus
func (p *Pool) updateMetrics() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			activeConns := atomic.LoadInt64(&p.activeConns)
			totalConns := atomic.LoadInt64(&p.totalConns)
			hits := atomic.LoadInt64(&p.poolHits)
			misses := atomic.LoadInt64(&p.poolMisses)

			p.mu.Lock()
			endpointCount := len(p.endpoints)
			p.mu.Unlock()
			proxies := *p.proxies.Load()
			proxyCount := len(proxies)

			// Report pool connection metrics to Prometheus
			metrics.PoolConnectionsTotal.WithLabelValues(p.clusterKey).Set(float64(proxyCount))
			metrics.PoolIdleConnections.WithLabelValues(p.clusterKey).Set(float64(proxyCount - int(activeConns)))
			metrics.PoolActiveConnections.WithLabelValues(p.clusterKey).Set(float64(activeConns))
			metrics.PoolHitsTotal.WithLabelValues(p.clusterKey).Set(float64(hits))
			metrics.PoolMissesTotal.WithLabelValues(p.clusterKey).Set(float64(misses))

			if ce := p.logger.Check(zap.DebugLevel, "Pool metrics"); ce != nil {
				ce.Write(
					zap.String("cluster", p.clusterKey),
					zap.Int("endpoints", endpointCount),
					zap.Int("proxies", proxyCount),
					zap.Int64("active_conns", activeConns),
					zap.Int64("total_conns", totalConns),
					zap.Int64("pool_hits", hits),
					zap.Int64("pool_misses", misses),
				)
			}
		}
	}
}

// createProxies creates reverse proxies for all endpoints
func (p *Pool) createProxies() {
	newProxies := make(map[string]*httputil.ReverseProxy)

	// Load current proxies for reuse
	currentProxies := *p.proxies.Load()

	for _, ep := range p.endpoints {
		if !ep.Ready {
			continue
		}

		key := endpointKey(ep)

		// Reuse existing proxy if available (pool hit)
		if proxy, ok := currentProxies[key]; ok {
			newProxies[key] = proxy
			atomic.AddInt64(&p.poolHits, 1)
			continue
		}

		// Pool miss — creating new proxy
		atomic.AddInt64(&p.poolMisses, 1)

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

		// Custom director to preserve headers for gRPC
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			// Trace context is already injected by the forwarding layer (forwarding.go)
			// so no need to inject it again here.

			// Preserve gRPC-specific headers
			if isGRPCRequest(req) {
				if ct := req.Header.Get("Content-Type"); ct != "" {
					req.Header.Set("Content-Type", ct)
				}
			}
		}

		newProxies[key] = proxy
	}

	p.proxies.Store(&newProxies)
}

// Forward forwards an HTTP request to the specified endpoint
func (p *Pool) Forward(endpoint *pb.Endpoint, req *http.Request, w http.ResponseWriter) error {
	key := endpointKey(endpoint)
	proxies := *p.proxies.Load()
	proxy, ok := proxies[key]
	if !ok {
		return fmt.Errorf("no proxy for endpoint %s", key)
	}

	// Track active connections for pool metrics
	atomic.AddInt64(&p.activeConns, 1)
	atomic.AddInt64(&p.totalConns, 1)
	defer atomic.AddInt64(&p.activeConns, -1)

	// Set up request context with timeout
	ctx := req.Context()
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, connectTimeout(p.cluster.ConnectTimeoutMs))
	defer cancel()

	// Create new request with modified context
	reqWithContext := req.WithContext(ctx)

	// Forward request
	proxy.ServeHTTP(w, reqWithContext)

	return nil
}

// connectTimeout returns the connect timeout duration, falling back to DefaultConnectTimeout
// when the configured value is zero or negative.
func connectTimeout(ms int64) time.Duration {
	timeout := time.Duration(ms) * time.Millisecond
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}
	return timeout
}

// endpointKey builds a key for an endpoint using net.JoinHostPort
func endpointKey(ep *pb.Endpoint) string {
	return net.JoinHostPort(ep.Address, strconv.FormatInt(int64(ep.Port), 10))
}

// Close closes the pool and all connections
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop health checker
	if p.healthChecker != nil {
		p.healthChecker.Stop()
	}
	if p.outlierDetector != nil {
		p.outlierDetector.Stop()
	}
	if p.cancel != nil {
		p.cancel()
	}

	p.transport.CloseIdleConnections()
	emptyProxies := make(map[string]*httputil.ReverseProxy)
	p.proxies.Store(&emptyProxies)
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
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.endpoints
}

// OutlierDetector returns the pool's outlier detector, or nil if not configured.
func (p *Pool) OutlierDetector() *health.OutlierDetector {
	return p.outlierDetector
}

// GetStats returns pool statistics
func (p *Pool) GetStats() map[string]interface{} {
	p.mu.Lock()
	endpointCount := len(p.endpoints)
	p.mu.Unlock()
	proxies := *p.proxies.Load()

	return map[string]interface{}{
		"total_endpoints":   endpointCount,
		"healthy_endpoints": len(proxies),
		"cluster":           p.clusterKey,
		"active_conns":      atomic.LoadInt64(&p.activeConns),
		"total_conns":       atomic.LoadInt64(&p.totalConns),
		"pool_hits":         atomic.LoadInt64(&p.poolHits),
		"pool_misses":       atomic.LoadInt64(&p.poolMisses),
	}
}

// GetBackendURL returns the backend URL for a given endpoint
func (p *Pool) GetBackendURL(endpoint *pb.Endpoint) string {
	scheme := "http"
	if p.cluster.Tls != nil && p.cluster.Tls.Enabled {
		scheme = "https"
	}
	return scheme + "://" + endpointKey(endpoint)
}

// GetClusterKey returns the cached cluster key (namespace/name)
func (p *Pool) GetClusterKey() string {
	return p.clusterKey
}

// createBackendTLSConfig creates a TLS config for backend connections
func createBackendTLSConfig(backendTLS *pb.BackendTLS, clusterKey string, logger *zap.Logger) *tls.Config {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// InsecureSkipVerify is an explicit user configuration for backend connections
	// where the backend may use self-signed certificates.
	// This is a security risk: certificate verification is completely disabled,
	// making the connection vulnerable to man-in-the-middle attacks.
	if backendTLS.InsecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
		logger.Error("SECURITY AUDIT: Backend TLS configured with InsecureSkipVerify=true, "+
			"certificate verification disabled — connection is vulnerable to MITM attacks",
			zap.String("backend", clusterKey),
		)
		metrics.InsecureBackendConnectionsTotal.WithLabelValues(clusterKey).Inc()
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
