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

package router

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/config"
	grpchandler "github.com/piwi3910/novaedge/internal/agent/grpc"
	"github.com/piwi3910/novaedge/internal/agent/lb"
	"github.com/piwi3910/novaedge/internal/agent/metrics"
	"github.com/piwi3910/novaedge/internal/agent/upstream"
	"github.com/piwi3910/novaedge/internal/agent/wasm"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// endpointKeyPool reduces allocations for endpoint key formatting in the hot path.
// Keys are short-lived strings like "10.0.0.1:8080" used for map lookups and metrics.
var endpointKeyPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 64) // pre-allocate for typical "host:port" strings
		return &b
	},
}

// formatEndpointKey builds an endpoint key string with minimal allocations using a pooled buffer
func formatEndpointKey(address string, port int32) string {
	poolVal := endpointKeyPool.Get()
	bp, ok := poolVal.(*[]byte)
	if !ok {
		// Fallback: allocate a new buffer if pool returned unexpected type
		b := make([]byte, 0, 64)
		bp = &b
	}
	b := (*bp)[:0]
	b = append(b, address...)
	b = append(b, ':')
	b = strconv.AppendInt(b, int64(port), 10)
	s := string(b)
	*bp = b
	endpointKeyPool.Put(bp)
	return s
}

// tracerName is the instrumentation name for the router tracer
const tracerName = "github.com/piwi3910/novaedge/internal/agent/router"

// Router routes HTTP requests to backends
type Router struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Routing table: hostname -> routes
	routes map[string][]*RouteEntry

	// Backend pools
	pools map[string]*upstream.Pool

	// Load balancers per cluster
	loadBalancers map[string]lb.LoadBalancer

	// Hash-based load balancers (RingHash, Maglev) stored separately
	// These require a key for consistent hashing
	hashBasedLBs map[string]interface{}

	// Sticky session wrappers per cluster
	stickyWrappers map[string]*lb.StickyWrapper

	// gRPC handler for gRPC-specific request processing
	grpcHandler *grpchandler.GRPCHandler

	// LB state caching: track endpoint versions to avoid unnecessary LB recreation
	endpointVersions map[string]uint64 // clusterKey -> hash of endpoint list

	// WASM plugin runtime
	wasmRuntime *wasm.Runtime

	// Request size limits (in bytes)
	maxRequestBodyBytes int64
	maxUploadBodyBytes  int64

	// Compression configuration (gateway-level)
	compressionConfig *pb.CompressionConfig

	// Response cache
	cache       *ResponseCache
	cacheConfig CacheConfig

	// Error page interceptor for custom error responses
	errorPages *ErrorPageInterceptor

	// Redirect scheme middleware for HTTP->HTTPS redirection
	redirectScheme *RedirectSchemeMiddleware

	// Access log middleware for request/response logging
	accessLog *AccessLogMiddleware
}

// NewRouter creates a new router
func NewRouter(logger *zap.Logger) *Router {
	return NewRouterWithCache(logger, DefaultCacheConfig())
}

// NewRouterWithCache creates a new router with the given cache configuration.
func NewRouterWithCache(logger *zap.Logger, cacheConfig CacheConfig) *Router {
	r := &Router{
		logger:              logger,
		routes:              make(map[string][]*RouteEntry),
		pools:               make(map[string]*upstream.Pool),
		loadBalancers:       make(map[string]lb.LoadBalancer),
		hashBasedLBs:        make(map[string]interface{}),
		stickyWrappers:      make(map[string]*lb.StickyWrapper),
		grpcHandler:         grpchandler.NewGRPCHandler(logger),
		endpointVersions:    make(map[string]uint64),
		maxRequestBodyBytes: 10 * 1024 * 1024,  // Default: 10MB
		maxUploadBodyBytes:  100 * 1024 * 1024, // Default: 100MB
		cacheConfig:         cacheConfig,
	}
	if cacheConfig.Enabled {
		r.cache = NewResponseCache(cacheConfig, logger)
	}
	return r
}

// Cache returns the response cache, or nil if caching is disabled.
func (r *Router) Cache() *ResponseCache {
	return r.cache
}

// StopCache stops the cache background goroutines.
func (r *Router) StopCache() {
	if r.cache != nil {
		r.cache.Stop()
	}
}

// SetWASMRuntime sets the WASM runtime for the router.
func (r *Router) SetWASMRuntime(rt *wasm.Runtime) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wasmRuntime = rt
}

// ApplyConfig applies a new configuration to the router
func (r *Router) ApplyConfig(ctx context.Context, snapshot *config.Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Info("Applying router configuration",
		zap.Int("routes", len(snapshot.Routes)),
		zap.Int("clusters", len(snapshot.Clusters)),
	)

	// Update request size limits from gateway configuration
	r.updateRequestLimits(snapshot)

	// Update compression configuration from gateway settings
	r.updateCompressionConfig(snapshot)

	// Configure error pages, redirect scheme, and access log from gateway config
	r.configureMiddleware(snapshot)

	// Clear route table (rebuilt below); preserve load balancers
	// so they survive config snapshots with unchanged endpoints.
	r.routes = make(map[string][]*RouteEntry)
	newLoadBalancers := make(map[string]lb.LoadBalancer)
	newHashBasedLBs := make(map[string]interface{})

	// Build routing table
	for _, route := range snapshot.Routes {
		for _, hostname := range route.Hostnames {
			for _, rule := range route.Rules {
				entry := &RouteEntry{
					Route:          route,
					Rule:           rule,
					PathMatcher:    createPathMatcher(rule),
					Policies:       r.createPolicyMiddleware(ctx, route, snapshot),
					HeaderRegexes:  compileHeaderRegexes(rule),
					ResponseFilter: collectResponseFilters(rule.Filters),
					Limits:         rule.Limits,
					Buffering:      rule.Buffering,
				}

				// Build mirror config from rule if present
				if rule.MirrorBackend != nil {
					entry.MirrorConfig = &MirrorConfig{
						BackendRef: rule.MirrorBackend,
						Percentage: rule.MirrorPercent,
					}
					if entry.MirrorConfig.Percentage == 0 {
						entry.MirrorConfig.Percentage = 100
					}
				}
				r.routes[hostname] = append(r.routes[hostname], entry)
			}
		}
	}

	// Create upstream pools for each cluster
	newPools := make(map[string]*upstream.Pool)
	for _, cluster := range snapshot.Clusters {
		clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)

		// Get endpoints for this cluster
		endpointList := snapshot.Endpoints[clusterKey]
		if endpointList == nil {
			r.logger.Warn("No endpoints for cluster", zap.String("cluster", clusterKey))
			continue
		}

		// Create or reuse pool
		if existingPool, ok := r.pools[clusterKey]; ok {
			// Update existing pool with new endpoints
			existingPool.UpdateEndpoints(endpointList.Endpoints)
			newPools[clusterKey] = existingPool
		} else {
			// Create new pool
			pool := upstream.NewPool(ctx, cluster, endpointList.Endpoints, r.logger)
			newPools[clusterKey] = pool
		}

		// Check if endpoints changed by computing hash
		endpointHash := hashEndpointList(endpointList.Endpoints, cluster.LbPolicy)
		previousHash, exists := r.endpointVersions[clusterKey]

		// Only recreate load balancer if endpoints actually changed
		if !exists || previousHash != endpointHash {
			r.logger.Debug("Endpoints changed, recreating load balancer",
				zap.String("cluster", clusterKey),
				zap.Bool("new_cluster", !exists),
			)

			// Create load balancer based on cluster policy
			switch cluster.LbPolicy {
			case pb.LoadBalancingPolicy_P2C:
				newLoadBalancers[clusterKey] = lb.NewP2C(endpointList.Endpoints)
				r.logger.Debug("Created P2C load balancer", zap.String("cluster", clusterKey))

			case pb.LoadBalancingPolicy_EWMA:
				newLoadBalancers[clusterKey] = lb.NewEWMA(endpointList.Endpoints)
				r.logger.Debug("Created EWMA load balancer", zap.String("cluster", clusterKey))

			case pb.LoadBalancingPolicy_RING_HASH:
				// RingHash uses consistent hashing - store separately
				newHashBasedLBs[clusterKey] = lb.NewRingHash(endpointList.Endpoints)
				r.logger.Debug("Created RingHash load balancer", zap.String("cluster", clusterKey))

			case pb.LoadBalancingPolicy_MAGLEV:
				// Maglev uses consistent hashing - store separately
				newHashBasedLBs[clusterKey] = lb.NewMaglev(endpointList.Endpoints)
				r.logger.Debug("Created Maglev load balancer", zap.String("cluster", clusterKey))

			case pb.LoadBalancingPolicy_LEAST_CONN:
				newLoadBalancers[clusterKey] = lb.NewLeastConn(endpointList.Endpoints)
				r.logger.Debug("Created LeastConn load balancer", zap.String("cluster", clusterKey))

			case pb.LoadBalancingPolicy_ROUND_ROBIN, pb.LoadBalancingPolicy_LB_POLICY_UNSPECIFIED:
				newLoadBalancers[clusterKey] = lb.NewRoundRobin(endpointList.Endpoints)
				r.logger.Debug("Created RoundRobin load balancer", zap.String("cluster", clusterKey))

			default:
				// Fallback to round robin for unknown policies
				newLoadBalancers[clusterKey] = lb.NewRoundRobin(endpointList.Endpoints)
				r.logger.Warn("Unknown LB policy, using RoundRobin",
					zap.String("cluster", clusterKey),
					zap.Int32("policy", int32(cluster.LbPolicy)),
				)
			}

			// Update version tracking
			r.endpointVersions[clusterKey] = endpointHash
		} else {
			// Carry over existing load balancers when endpoints unchanged
			if existingLB, ok := r.loadBalancers[clusterKey]; ok {
				newLoadBalancers[clusterKey] = existingLB
			}
			if existingHashLB, ok := r.hashBasedLBs[clusterKey]; ok {
				newHashBasedLBs[clusterKey] = existingHashLB
			}
			r.logger.Debug("Endpoints unchanged, reusing load balancer",
				zap.String("cluster", clusterKey),
			)
		}
	}

	// Close pools that are no longer needed and clean up their metrics
	for key, pool := range r.pools {
		if _, needed := newPools[key]; !needed {
			r.logger.Info("Closing unused pool", zap.String("cluster", key))
			pool.Close()
			// Clean up stale Prometheus metrics for the removed cluster
			metrics.CleanupClusterMetrics(key)
			// Remove endpoint version tracking
			delete(r.endpointVersions, key)
		}
	}

	r.pools = newPools
	r.loadBalancers = newLoadBalancers
	r.hashBasedLBs = newHashBasedLBs

	// Create sticky session wrappers for clusters with session affinity configured
	newStickyWrappers := make(map[string]*lb.StickyWrapper)
	for _, cluster := range snapshot.Clusters {
		clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
		sa := cluster.GetSessionAffinity()
		if sa == nil || sa.GetType() == "" {
			continue
		}
		if sa.GetType() != "cookie" {
			continue
		}
		baseLB, hasLB := newLoadBalancers[clusterKey]
		if !hasLB {
			continue
		}
		endpointList := snapshot.Endpoints[clusterKey]
		if endpointList == nil {
			continue
		}
		cookieName := sa.GetCookieName()
		if cookieName == "" {
			cookieName = "NOVAEDGE_AFFINITY"
		}
		cookiePath := sa.GetCookiePath()
		if cookiePath == "" {
			cookiePath = "/"
		}
		cfg := lb.StickyConfig{
			CookieName: cookieName,
			TTL:        time.Duration(sa.GetCookieTtlSeconds()) * time.Second,
			Path:       cookiePath,
			Secure:     sa.GetCookieSecure(),
			SameSite:   lb.ParseSameSite(sa.GetCookieSameSite()),
		}
		newStickyWrappers[clusterKey] = lb.NewStickyWrapper(baseLB, cfg, endpointList.Endpoints)
		r.logger.Debug("Created sticky session wrapper",
			zap.String("cluster", clusterKey),
			zap.String("cookie", cookieName),
		)
	}
	r.stickyWrappers = newStickyWrappers

	r.logger.Info("Router configuration applied",
		zap.Int("hostnames", len(r.routes)),
		zap.Int("pools", len(r.pools)),
	)

	return nil
}

// ServeHTTP routes incoming HTTP requests
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Extract trace context from incoming request headers (W3C TraceContext propagation)
	ctx := req.Context()
	propagator := otel.GetTextMapPropagator()
	ctx = propagator.Extract(ctx, propagation.HeaderCarrier(req.Header))

	// Start the main request span
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "HTTP "+req.Method,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.url", req.URL.String()),
			attribute.String("http.host", req.Host),
			attribute.String("http.scheme", req.URL.Scheme),
			attribute.String("http.user_agent", req.UserAgent()),
			attribute.String("http.target", req.URL.Path),
			attribute.String("net.peer.ip", req.RemoteAddr),
		),
	)
	defer span.End()

	// Store context with span in the request
	req = req.WithContext(ctx)

	// Determine request body size limit based on content type
	maxBodySize := r.maxRequestBodyBytes

	// For file uploads (multipart/form-data), use larger limit
	contentType := req.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		maxBodySize = r.maxUploadBodyBytes
	}

	// Limit request body size to prevent memory exhaustion attacks
	req.Body = http.MaxBytesReader(w, req.Body, maxBodySize)

	// Track request start time and in-flight requests
	startTime := time.Now()
	metrics.HTTPRequestsInFlight.Inc()
	defer metrics.HTTPRequestsInFlight.Dec()

	// Get a response writer from the pool to reduce allocations
	wrappedWriter := getResponseWriter(w)
	defer putResponseWriter(wrappedWriter)

	// Defer metrics and span status recording
	defer func() {
		duration := time.Since(startTime).Seconds()
		statusCode := strconv.Itoa(wrappedWriter.statusCode)

		// Record span status based on HTTP status code
		span.SetAttributes(
			attribute.Int("http.status_code", wrappedWriter.statusCode),
			attribute.Float64("http.duration_seconds", duration),
		)

		if wrappedWriter.statusCode >= 400 {
			span.SetStatus(codes.Error, http.StatusText(wrappedWriter.statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}

		// We'll set cluster in handleRoute, for now use "unknown"
		metrics.RecordHTTPRequest(req.Method, statusCode, "unknown", duration)

		// Record access log entry if access logging is enabled
		if r.accessLog != nil && r.accessLog.IsEnabled() {
			if r.accessLog.shouldSample() && r.accessLog.shouldLog(wrappedWriter.statusCode) {
				entry := AccessLogEntry{
					ClientIP:      extractClientIP(req),
					Timestamp:     startTime.UTC().Format(time.RFC3339Nano),
					Method:        req.Method,
					URI:           req.RequestURI,
					Protocol:      req.Proto,
					StatusCode:    wrappedWriter.statusCode,
					BodyBytesSent: 0,
					Duration:      duration,
					UserAgent:     req.UserAgent(),
					Referer:       req.Referer(),
					RequestID:     req.Header.Get("X-Request-ID"),
					Host:          req.Host,
				}
				r.accessLog.writeEntry(entry)
			}
		}
	}()

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check if redirect scheme middleware should short-circuit the request
	if r.redirectScheme != nil && r.redirectScheme.IsEnabled() {
		// Check if request should be redirected (HTTP -> HTTPS)
		if req.TLS == nil && req.Header.Get("X-Forwarded-Proto") != r.redirectScheme.scheme {
			if !r.redirectScheme.isExcluded(req.URL.Path) {
				r.redirectScheme.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})).ServeHTTP(wrappedWriter, req)
				return
			}
		}
	}

	// Extract hostname (without port)
	hostname := req.Host
	if idx := strings.Index(hostname, ":"); idx != -1 {
		hostname = hostname[:idx]
	}

	// Add hostname to span
	span.SetAttributes(attribute.String("http.hostname", hostname))

	// Find matching route
	routes, ok := r.routes[hostname]
	if !ok {
		span.AddEvent("route_not_found", trace.WithAttributes(
			attribute.String("hostname", hostname),
		))
		r.logger.Warn("No route for hostname", zap.String("hostname", hostname))
		http.Error(wrappedWriter, "No route found", http.StatusNotFound)
		return
	}

	// Match against route rules
	for _, entry := range routes {
		if r.matchRoute(entry, req) {
			// Add route matching info to span
			span.AddEvent("route_matched", trace.WithAttributes(
				attribute.String("route.name", entry.Route.Name),
				attribute.String("route.namespace", entry.Route.Namespace),
			))
			span.SetAttributes(
				attribute.String("novaedge.route.name", entry.Route.Name),
				attribute.String("novaedge.route.namespace", entry.Route.Namespace),
			)
			r.handleRoute(entry, wrappedWriter, req)
			return
		}
	}

	// No matching rule
	span.AddEvent("no_matching_rule", trace.WithAttributes(
		attribute.String("hostname", hostname),
		attribute.String("path", req.URL.Path),
	))
	r.logger.Warn("No matching rule for request",
		zap.String("hostname", hostname),
		zap.String("path", req.URL.Path),
	)
	http.Error(wrappedWriter, "No matching route rule", http.StatusNotFound)
}

// matchRoute checks if a request matches a route entry
func (r *Router) matchRoute(entry *RouteEntry, req *http.Request) bool {
	// Evaluate boolean expression first if present
	if entry.Expression != nil {
		if !entry.Expression.Evaluate(req) {
			return false
		}
	}

	// Check if there are any matches defined
	if len(entry.Rule.Matches) == 0 {
		// No matches means match all
		return true
	}

	// Check each match condition
	for matchIdx, match := range entry.Rule.Matches {
		if r.matchCondition(match, matchIdx, req, entry.PathMatcher, entry.HeaderRegexes) {
			return true
		}
	}

	return false
}

// matchCondition checks if a request matches a specific match condition
func (r *Router) matchCondition(match *pb.RouteMatch, matchIdx int, req *http.Request, pathMatcher PathMatcher, cachedRegexes map[int]*regexp.Regexp) bool {
	// Check path match
	if match.Path != nil {
		if pathMatcher != nil {
			if !pathMatcher.Match(req.URL.Path) {
				return false
			}
		}
	}

	// Check method match
	if match.Method != "" && match.Method != req.Method {
		return false
	}

	// Check header matches
	for headerIdx, headerMatch := range match.Headers {
		headerValue := req.Header.Get(headerMatch.Name)
		if !r.matchHeader(headerMatch, headerIdx, matchIdx, headerValue, cachedRegexes) {
			return false
		}
	}

	return true
}

// matchHeader checks if a header value matches, using cached regexes when available
func (r *Router) matchHeader(match *pb.HeaderMatch, headerIdx, matchIdx int, value string, cachedRegexes map[int]*regexp.Regexp) bool {
	switch match.Type {
	case pb.HeaderMatchType_HEADER_EXACT:
		return value == match.Value
	case pb.HeaderMatchType_HEADER_REGULAR_EXPRESSION:
		// Use cached regex if available (performance optimization)
		key := matchIdx*1000 + headerIdx
		if regex, ok := cachedRegexes[key]; ok {
			return regex.MatchString(value)
		}
		// Fallback: compile on the fly (shouldn't happen if caching is working)
		// Log this as it indicates a problem with caching
		r.logger.Warn("Regex not cached, compiling on-the-fly", zap.String("pattern", match.Value))
		if regex, err := regexp.Compile(match.Value); err == nil {
			return regex.MatchString(value)
		}
		return false
	default:
		return value == match.Value
	}
}

// updateRequestLimits updates request size limits from gateway configuration
func (r *Router) updateRequestLimits(snapshot *config.Snapshot) {
	// Find the maximum request size limits across all gateways
	maxRequest := r.maxRequestBodyBytes
	maxUpload := r.maxUploadBodyBytes

	for _, gateway := range snapshot.Gateways {
		for _, listener := range gateway.Listeners {
			if listener.MaxRequestBodyBytes > 0 {
				if listener.MaxRequestBodyBytes > maxRequest {
					maxRequest = listener.MaxRequestBodyBytes
				}
			}
			if listener.MaxUploadBodyBytes > 0 {
				if listener.MaxUploadBodyBytes > maxUpload {
					maxUpload = listener.MaxUploadBodyBytes
				}
			}
		}
	}

	// Update limits if they changed
	if maxRequest != r.maxRequestBodyBytes || maxUpload != r.maxUploadBodyBytes {
		r.logger.Info("Updated request size limits",
			zap.Int64("max_request_body_bytes", maxRequest),
			zap.Int64("max_upload_body_bytes", maxUpload))
		r.maxRequestBodyBytes = maxRequest
		r.maxUploadBodyBytes = maxUpload
	}
}

// updateCompressionConfig extracts compression configuration from gateway settings.
func (r *Router) updateCompressionConfig(snapshot *config.Snapshot) {
	for _, gateway := range snapshot.Gateways {
		if gateway.Compression != nil && gateway.Compression.Enabled {
			r.compressionConfig = gateway.Compression
			r.logger.Info("Compression enabled",
				zap.Bool("enabled", gateway.Compression.Enabled),
				zap.Int64("min_size", gateway.Compression.MinSize),
				zap.Int32("level", gateway.Compression.Level),
				zap.Strings("algorithms", gateway.Compression.Algorithms),
			)
			return
		}
	}
	// No gateway has compression enabled
	r.compressionConfig = nil
}

// configureMiddleware configures error pages, redirect scheme, and access log from gateway config
func (r *Router) configureMiddleware(snapshot *config.Snapshot) {
	// Close previous access log middleware if any
	if r.accessLog != nil {
		r.accessLog.Close()
	}

	// Find first gateway with error page / redirect / access log config
	// In a multi-gateway setup, each gateway could have its own config;
	// for now, we use the first configured one as the default.
	for _, gw := range snapshot.Gateways {
		if gw.ErrorPages != nil && gw.ErrorPages.Enabled {
			r.errorPages = NewErrorPageInterceptor(gw.ErrorPages, r.logger)
			r.logger.Info("Error page interceptor configured",
				zap.String("gateway", gw.Name),
				zap.Int("custom_pages", len(gw.ErrorPages.Pages)),
			)
			break
		}
	}

	for _, gw := range snapshot.Gateways {
		if gw.RedirectScheme != nil && gw.RedirectScheme.Enabled {
			r.redirectScheme = NewRedirectSchemeMiddleware(gw.RedirectScheme, r.logger)
			r.logger.Info("Redirect scheme middleware configured",
				zap.String("gateway", gw.Name),
				zap.String("scheme", gw.RedirectScheme.Scheme),
				zap.Int32("port", gw.RedirectScheme.Port),
			)
			break
		}
	}

	// Access log can be configured per-route; check gateway-level routes
	for _, route := range snapshot.Routes {
		if route.AccessLog != nil && route.AccessLog.Enabled {
			alm, err := NewAccessLogMiddleware(route.AccessLog, r.logger)
			if err != nil {
				r.logger.Error("Failed to create access log middleware",
					zap.String("route", route.Name),
					zap.Error(err),
				)
				continue
			}
			r.accessLog = alm
			r.logger.Info("Access log middleware configured",
				zap.String("route", route.Name),
				zap.String("format", route.AccessLog.Format),
				zap.String("output", route.AccessLog.Output),
			)
			break
		}
	}
}

// Close cleans up resources used by the router
func (r *Router) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.accessLog != nil {
		r.accessLog.Close()
	}

	for _, pool := range r.pools {
		pool.Close()
	}
}
