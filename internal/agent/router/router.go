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
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// routerState holds the immutable routing state that is atomically swapped
// on each configuration update. ServeHTTP loads a snapshot of this state
// once per request, so in-flight requests are never blocked by config updates.
type routerState struct {
	// Routing table: hostname -> routes
	routes map[string][]*RouteEntry

	// Radix-tree indexes per hostname for O(log n) path lookup (built at config time)
	routeIndexes map[string]*routeIndex

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
	grpcHandler *grpchandler.Handler

	// WASM plugin runtime
	wasmRuntime *wasm.Runtime

	// Request size limits (in bytes)
	maxRequestBodyBytes int64
	maxUploadBodyBytes  int64

	// Compression configuration (gateway-level)
	compressionConfig *pb.CompressionConfig

	// Response cache
	cache *ResponseCache

	// Error page interceptor for custom error responses
	errorPages *ErrorPageInterceptor

	// Redirect scheme middleware for HTTP->HTTPS redirection
	redirectScheme *RedirectSchemeMiddleware

	// Access log middleware for request/response logging
	accessLog *AccessLogMiddleware
}

// Router routes HTTP requests to backends
type Router struct {
	logger *zap.Logger

	// mu serializes ApplyConfig and Close calls. It is NOT held during
	// request serving; ServeHTTP uses the lock-free atomic pointer instead.
	mu sync.Mutex

	// state holds the current immutable routing state. ServeHTTP atomically
	// loads this pointer once per request for lock-free access.
	state atomic.Pointer[routerState]

	// LB state caching: track endpoint versions to avoid unnecessary LB recreation.
	// Only accessed under mu in ApplyConfig.
	endpointVersions map[string]uint64

	// cacheConfig is set once during construction and never changes.
	cacheConfig CacheConfig
}

// NewRouter creates a new router
func NewRouter(logger *zap.Logger) *Router {
	return NewRouterWithCache(logger, DefaultCacheConfig())
}

// NewRouterWithCache creates a new router with the given cache configuration.
func NewRouterWithCache(logger *zap.Logger, cacheConfig CacheConfig) *Router {
	initial := &routerState{
		routes:              make(map[string][]*RouteEntry),
		routeIndexes:        make(map[string]*routeIndex),
		pools:               make(map[string]*upstream.Pool),
		loadBalancers:       make(map[string]lb.LoadBalancer),
		hashBasedLBs:        make(map[string]interface{}),
		stickyWrappers:      make(map[string]*lb.StickyWrapper),
		grpcHandler:         grpchandler.NewHandler(logger),
		maxRequestBodyBytes: 10 * 1024 * 1024,  // Default: 10MB
		maxUploadBodyBytes:  100 * 1024 * 1024, // Default: 100MB
	}
	if cacheConfig.Enabled {
		initial.cache = NewResponseCache(cacheConfig, logger)
	}

	r := &Router{
		logger:           logger,
		endpointVersions: make(map[string]uint64),
		cacheConfig:      cacheConfig,
	}
	r.state.Store(initial)
	return r
}

// Cache returns the response cache, or nil if caching is disabled.
func (r *Router) Cache() *ResponseCache {
	snap := r.state.Load()
	return snap.cache
}

// StopCache stops the cache background goroutines.
func (r *Router) StopCache() {
	snap := r.state.Load()
	if snap.cache != nil {
		snap.cache.Stop()
	}
}

// SetWASMRuntime sets the WASM runtime for the router.
// It atomically swaps a new state snapshot with the updated runtime.
func (r *Router) SetWASMRuntime(rt *wasm.Runtime) {
	r.mu.Lock()
	defer r.mu.Unlock()

	old := r.state.Load()
	updated := *old
	updated.wasmRuntime = rt
	r.state.Store(&updated)
}

// ApplyConfig applies a new configuration to the router
func (r *Router) ApplyConfig(ctx context.Context, snapshot *config.Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Info("Applying router configuration",
		zap.Int("routes", len(snapshot.Routes)),
		zap.Int("clusters", len(snapshot.Clusters)),
	)

	// Load the previous state to carry forward pools and LB state
	prev := r.state.Load()

	// Start building the new state from scratch
	newState := &routerState{
		grpcHandler: prev.grpcHandler,
		wasmRuntime: prev.wasmRuntime,
	}

	// Update request size limits from gateway configuration
	newState.maxRequestBodyBytes = prev.maxRequestBodyBytes
	newState.maxUploadBodyBytes = prev.maxUploadBodyBytes
	r.updateRequestLimits(snapshot, newState, prev)

	// Update compression configuration from gateway settings
	r.updateCompressionConfig(snapshot, newState)

	// Configure error pages, redirect scheme, and access log from gateway config
	r.configureMiddleware(snapshot, newState, prev)

	// Carry forward cache (it is shared across snapshots)
	newState.cache = prev.cache

	// Build routing table
	newRoutes := make(map[string][]*RouteEntry)
	newLoadBalancers := make(map[string]lb.LoadBalancer)
	newHashBasedLBs := make(map[string]interface{})

	for _, route := range snapshot.Routes {
		// Routes without hostnames act as catch-all: use "" as the key
		hostnames := route.Hostnames
		if len(hostnames) == 0 {
			hostnames = []string{""}
		}
		for _, hostname := range hostnames {
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
					Filters:        buildFilters(rule.Filters),
				}

				// Pre-compile boolean routing expression at config time
				// to avoid per-request parsing overhead (same pattern as
				// compileHeaderRegexes and createPathMatcher above).
				if expr := route.GetExpression(); expr != "" {
					compiled, err := CompileExpression(expr)
					if err != nil {
						r.logger.Error("Failed to compile route expression, skipping",
							zap.String("route", route.Name),
							zap.String("expression", expr),
							zap.Error(err),
						)
					} else {
						entry.Expression = compiled
						r.logger.Debug("Compiled route expression",
							zap.String("route", route.Name),
							zap.String("expression", expr),
						)
					}
				}

				// Pre-compute cluster keys for all backend refs to avoid
				// per-request string concatenation in the forwarding hot path.
				if len(rule.BackendRefs) > 0 {
					entry.BackendClusterKeys = make(map[*pb.BackendRef]string, len(rule.BackendRefs))
					for _, ref := range rule.BackendRefs {
						entry.BackendClusterKeys[ref] = ref.Namespace + "/" + ref.Name
					}
				}

				// Build mirror config from rule if present
				if rule.MirrorBackend != nil {
					entry.MirrorConfig = &MirrorConfig{
						BackendRef: rule.MirrorBackend,
						Percentage: rule.MirrorPercent,
						ClusterKey: rule.MirrorBackend.Namespace + "/" + rule.MirrorBackend.Name,
					}
					if entry.MirrorConfig.Percentage == 0 {
						entry.MirrorConfig.Percentage = 100
					}
				}
				newRoutes[hostname] = append(newRoutes[hostname], entry)
			}
		}
	}

	// Sort routes by specificity so the most specific matches are tried first.
	// This ensures exact matches beat prefix matches, longer prefixes beat shorter ones,
	// and routes with more conditions (headers, methods) are preferred.
	newRouteIndexes := make(map[string]*routeIndex, len(newRoutes))
	for hostname := range newRoutes {
		sortRoutesBySpecificity(newRoutes[hostname])
		// Build radix tree index for O(log n) path lookup instead of O(n) linear scan
		newRouteIndexes[hostname] = newRouteIndex(newRoutes[hostname])
	}
	newState.routes = newRoutes
	newState.routeIndexes = newRouteIndexes

	// Create upstream pools for each cluster
	newPools := make(map[string]*upstream.Pool)
	for _, cluster := range snapshot.Clusters {
		clusterKey := cluster.Namespace + "/" + cluster.Name

		// Get endpoints for this cluster
		endpointList := snapshot.Endpoints[clusterKey]
		if endpointList == nil {
			r.logger.Warn("No endpoints for cluster", zap.String("cluster", clusterKey))
			continue
		}

		// Create or reuse pool
		if existingPool, ok := prev.pools[clusterKey]; ok {
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
			if existingLB, ok := prev.loadBalancers[clusterKey]; ok {
				newLoadBalancers[clusterKey] = existingLB
			}
			if existingHashLB, ok := prev.hashBasedLBs[clusterKey]; ok {
				newHashBasedLBs[clusterKey] = existingHashLB
			}
			r.logger.Debug("Endpoints unchanged, reusing load balancer",
				zap.String("cluster", clusterKey),
			)
		}
	}

	// Close pools that are no longer needed and clean up their metrics
	for key, pool := range prev.pools {
		if _, needed := newPools[key]; !needed {
			r.logger.Info("Closing unused pool", zap.String("cluster", key))
			pool.Close()
			// Clean up stale Prometheus metrics for the removed cluster
			metrics.CleanupClusterMetrics(key)
			// Remove endpoint version tracking
			delete(r.endpointVersions, key)
		}
	}

	newState.pools = newPools
	newState.loadBalancers = newLoadBalancers
	newState.hashBasedLBs = newHashBasedLBs

	// Create sticky session wrappers for clusters with session affinity configured
	newStickyWrappers := make(map[string]*lb.StickyWrapper)
	for _, cluster := range snapshot.Clusters {
		clusterKey := cluster.Namespace + "/" + cluster.Name
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
	newState.stickyWrappers = newStickyWrappers

	// Atomically publish the new state so ServeHTTP picks it up without locking.
	r.state.Store(newState)

	r.logger.Info("Router configuration applied",
		zap.Int("hostnames", len(newState.routes)),
		zap.Int("pools", len(newState.pools)),
	)

	return nil
}

// ServeHTTP routes incoming HTTP requests
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Atomically load the current routing state. This is the only
	// synchronization needed; no locks are held for the request lifetime.
	snap := r.state.Load()

	// Extract trace context from incoming request headers (W3C TraceContext propagation)
	ctx := req.Context()
	propagator := otel.GetTextMapPropagator()
	ctx = propagator.Extract(ctx, propagation.HeaderCarrier(req.Header))

	// Start the main request span. Attributes are set lazily below to avoid
	// allocating attribute.String values when the span is not sampled (#131).
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "HTTP "+req.Method,
		trace.WithSpanKind(trace.SpanKindServer),
	)
	defer span.End()

	// Only allocate and set span attributes when the trace is actually being recorded.
	// With typical sampling ratios this skips the attribute allocations for most requests.
	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.host", req.Host),
			attribute.String("http.scheme", req.URL.Scheme),
			attribute.String("http.target", req.URL.Path),
			attribute.String("net.peer.ip", req.RemoteAddr),
		)
	}

	// Inject pipeline state into context so handleRoute does not need a second WithContext call
	state := GetPipelineStateFromPool()
	defer PutPipelineStateToPool(state)
	ctx = WithPipelineState(ctx, state)

	// Store all context values (span + pipeline state) in the request with a single WithContext
	req = req.WithContext(ctx)

	// Determine request body size limit based on content type
	maxBodySize := snap.maxRequestBodyBytes

	// For file uploads (multipart/form-data), use larger limit
	contentType := req.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		maxBodySize = snap.maxUploadBodyBytes
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
		statusClass := metrics.StatusClass(wrappedWriter.statusCode)

		// Record span status based on HTTP status code (only if sampled)
		if span.IsRecording() {
			span.SetAttributes(
				attribute.Int("http.status_code", wrappedWriter.statusCode),
				attribute.Float64("http.duration_seconds", duration),
			)

			if wrappedWriter.statusCode >= 400 {
				span.SetStatus(codes.Error, http.StatusText(wrappedWriter.statusCode))
			} else {
				span.SetStatus(codes.Ok, "")
			}
		}

		// We'll set cluster in handleRoute, for now use "unknown"
		metrics.RecordHTTPRequest(req.Method, statusClass, "unknown", duration)

		// Record access log entry if access logging is enabled
		if snap.accessLog != nil && snap.accessLog.IsEnabled() {
			if snap.accessLog.shouldSample() && snap.accessLog.shouldLog(wrappedWriter.statusCode) {
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
				snap.accessLog.writeEntry(entry)
			}
		}
	}()

	// Check if redirect scheme middleware should short-circuit the request
	if snap.redirectScheme != nil && snap.redirectScheme.IsEnabled() {
		// Check if request should be redirected (HTTP -> HTTPS)
		if req.TLS == nil && req.Header.Get("X-Forwarded-Proto") != snap.redirectScheme.scheme {
			if !snap.redirectScheme.isExcluded(req.URL.Path) {
				snap.redirectScheme.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})).ServeHTTP(wrappedWriter, req)
				return
			}
		}
	}

	// Extract hostname (without port)
	hostname := req.Host
	if idx := strings.Index(hostname, ":"); idx != -1 {
		hostname = hostname[:idx]
	}

	// Add hostname to span (only if sampled)
	recording := span.IsRecording()
	if recording {
		span.SetAttributes(attribute.String("http.hostname", hostname))
	}

	// Find matching route using radix tree index for O(log n) path lookup.
	// If no hostname-specific routes exist, fall back to catch-all routes (empty hostname key).
	rIdx, ok := snap.routeIndexes[hostname]
	if !ok {
		rIdx, ok = snap.routeIndexes[""]
	}
	if !ok {
		if recording {
			span.AddEvent("route_not_found", trace.WithAttributes(
				attribute.String("hostname", hostname),
			))
		}
		r.logger.Warn("No route for hostname", zap.String("hostname", hostname))
		http.Error(wrappedWriter, "No route found", http.StatusNotFound)
		return
	}

	// Radix tree lookup: walks the tree by path, checks match conditions at each node.
	// In detailed trace mode, wrap the lookup in a child span for visibility.
	var entry *RouteEntry
	if DefaultTraceVerbosity.ShouldTraceDetailed() && recording {
		var matchSpan trace.Span
		_, matchSpan = tracer.Start(req.Context(), "route_matching")
		entry = rIdx.lookup(req.URL.Path, req, r.matchRoute)
		if entry != nil {
			matchSpan.SetAttributes(
				attribute.String("novaedge.route.name", entry.Route.Name),
				attribute.String("novaedge.route.namespace", entry.Route.Namespace),
			)
		}
		matchSpan.End()
	} else {
		entry = rIdx.lookup(req.URL.Path, req, r.matchRoute)
	}

	if entry != nil {
		if recording {
			span.AddEvent("route_matched", trace.WithAttributes(
				attribute.String("route.name", entry.Route.Name),
				attribute.String("route.namespace", entry.Route.Namespace),
			))
			span.SetAttributes(
				attribute.String("novaedge.route.name", entry.Route.Name),
				attribute.String("novaedge.route.namespace", entry.Route.Namespace),
			)
		}
		r.handleRoute(snap, entry, wrappedWriter, req)
		return
	}

	// No matching rule
	if recording {
		span.AddEvent("no_matching_rule", trace.WithAttributes(
			attribute.String("hostname", hostname),
			attribute.String("path", req.URL.Path),
		))
	}
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
		if err := validateRegexPattern(match.Value); err != nil {
			r.logger.Warn("Regex pattern validation failed", zap.String("pattern", match.Value), zap.Error(err))
			return false
		}
		if regex, err := regexp.Compile(match.Value); err == nil {
			return regex.MatchString(value)
		}
		return false
	default:
		return value == match.Value
	}
}

// updateRequestLimits updates request size limits from gateway configuration
func (r *Router) updateRequestLimits(snapshot *config.Snapshot, newState *routerState, prev *routerState) {
	// Find the maximum request size limits across all gateways
	maxRequest := newState.maxRequestBodyBytes
	maxUpload := newState.maxUploadBodyBytes

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
	if maxRequest != prev.maxRequestBodyBytes || maxUpload != prev.maxUploadBodyBytes {
		r.logger.Info("Updated request size limits",
			zap.Int64("max_request_body_bytes", maxRequest),
			zap.Int64("max_upload_body_bytes", maxUpload))
	}
	newState.maxRequestBodyBytes = maxRequest
	newState.maxUploadBodyBytes = maxUpload
}

// updateCompressionConfig extracts compression configuration from gateway settings.
func (r *Router) updateCompressionConfig(snapshot *config.Snapshot, newState *routerState) {
	for _, gateway := range snapshot.Gateways {
		if gateway.Compression != nil && gateway.Compression.Enabled {
			newState.compressionConfig = gateway.Compression
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
	newState.compressionConfig = nil
}

// configureMiddleware configures error pages, redirect scheme, and access log from gateway config
func (r *Router) configureMiddleware(snapshot *config.Snapshot, newState *routerState, prev *routerState) {
	// Close previous access log middleware if any
	if prev.accessLog != nil {
		prev.accessLog.Close()
	}

	// Find first gateway with error page / redirect / access log config
	// In a multi-gateway setup, each gateway could have its own config;
	// for now, we use the first configured one as the default.
	for _, gw := range snapshot.Gateways {
		if gw.ErrorPages != nil && gw.ErrorPages.Enabled {
			newState.errorPages = NewErrorPageInterceptor(gw.ErrorPages, r.logger)
			r.logger.Info("Error page interceptor configured",
				zap.String("gateway", gw.Name),
				zap.Int("custom_pages", len(gw.ErrorPages.Pages)),
			)
			break
		}
	}

	for _, gw := range snapshot.Gateways {
		if gw.RedirectScheme != nil && gw.RedirectScheme.Enabled {
			newState.redirectScheme = NewRedirectSchemeMiddleware(gw.RedirectScheme, r.logger)
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
			newState.accessLog = alm
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

	snap := r.state.Load()
	if snap.accessLog != nil {
		snap.accessLog.Close()
	}

	for _, pool := range snap.pools {
		pool.Close()
	}
}
