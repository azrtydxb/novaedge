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
	"crypto/tls"
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
	ebpfhealth "github.com/piwi3910/novaedge/internal/agent/ebpf/health"
	ebpfratelimit "github.com/piwi3910/novaedge/internal/agent/ebpf/ratelimit"
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

	// ExtProc middleware for external processing
	extProc *ExtProcMiddleware
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

	// clusterLBPolicies tracks the LB policy per cluster so we can detect
	// policy changes and force LB recreation. Only accessed under mu.
	clusterLBPolicies map[string]pb.LoadBalancingPolicy

	// cacheConfig is set once during construction and never changes.
	cacheConfig CacheConfig

	// tunnelRegistry maps remote cluster names to their gateway agents.
	// When non-nil, the router checks selected endpoints for the remote label
	// and forwards through cross-cluster tunnels instead of direct connections.
	tunnelRegistry *CrossClusterTunnelRegistry

	// tunnelTLSConfig is the TLS configuration used for mTLS connections
	// to remote cluster gateway agents. Required when tunnelRegistry is set.
	tunnelTLSConfig *tls.Config

	// ebpfHealthMon is the optional eBPF passive health signal monitor.
	// When non-nil, newly created upstream pool health checkers are configured
	// to use eBPF-observed traffic signals as the primary health indicator.
	// Only accessed under mu in ApplyConfig.
	ebpfHealthMon *ebpfhealth.HealthMonitor

	// ebpfRateLimiter is the optional eBPF per-IP rate limiter. When non-nil,
	// newly created policy rate limiters are configured to offload per-source-IP
	// rate limiting to BPF maps. Only accessed under mu in ApplyConfig.
	ebpfRateLimiter *ebpfratelimit.RateLimiter
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
		logger:            logger,
		endpointVersions:  make(map[string]uint64),
		clusterLBPolicies: make(map[string]pb.LoadBalancingPolicy),
		cacheConfig:       cacheConfig,
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

// SetTunnelRegistry configures cross-cluster tunnel forwarding. When set, the
// router will check selected endpoints for the novaedge.io/remote label and
// forward matching requests through mTLS tunnels to remote cluster gateways.
func (r *Router) SetTunnelRegistry(registry *CrossClusterTunnelRegistry, tlsConfig *tls.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tunnelRegistry = registry
	r.tunnelTLSConfig = tlsConfig
}

// TunnelRegistry returns the cross-cluster tunnel registry, or nil if not configured.
func (r *Router) TunnelRegistry() *CrossClusterTunnelRegistry {
	return r.tunnelRegistry
}

// SetEBPFHealthMonitor sets the eBPF passive health signal monitor. When set,
// newly created upstream pool health checkers are configured to use eBPF
// passive health signals. Existing pools are updated on the next ApplyConfig.
func (r *Router) SetEBPFHealthMonitor(monitor *ebpfhealth.HealthMonitor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ebpfHealthMon = monitor
}

// SetEBPFRateLimiter sets the eBPF per-IP rate limiter. When set, newly
// created policy rate limiters are configured to use the eBPF fast path
// for per-source-IP rate limiting. Existing limiters are updated on the
// next ApplyConfig.
func (r *Router) SetEBPFRateLimiter(rl *ebpfratelimit.RateLimiter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ebpfRateLimiter = rl
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

	// Build routing table from snapshot routes
	newState.routes, newState.routeIndexes = r.buildRoutes(ctx, snapshot)

	// Create upstream pools and load balancers for each cluster
	newLoadBalancers, newHashBasedLBs := r.buildPoolsAndBalancers(ctx, snapshot, prev, newState)
	newState.loadBalancers = newLoadBalancers
	newState.hashBasedLBs = newHashBasedLBs

	// Create sticky session wrappers for clusters with session affinity
	newState.stickyWrappers = r.buildStickyWrappers(snapshot, newLoadBalancers)

	// Atomically publish the new state so ServeHTTP picks it up without locking.
	r.state.Store(newState)

	r.logger.Info("Router configuration applied",
		zap.Int("hostnames", len(newState.routes)),
		zap.Int("pools", len(newState.pools)),
	)

	return nil
}

// buildRoutes constructs the routing table and radix-tree indexes from the snapshot.
func (r *Router) buildRoutes(ctx context.Context, snapshot *config.Snapshot) (map[string][]*RouteEntry, map[string]*routeIndex) {
	newRoutes := make(map[string][]*RouteEntry)

	for _, route := range snapshot.Routes {
		// Routes without hostnames act as catch-all: use "" as the key
		hostnames := route.Hostnames
		if len(hostnames) == 0 {
			hostnames = []string{""}
		}
		for _, hostname := range hostnames {
			for _, rule := range route.Rules {
				entry := r.buildRouteEntry(ctx, route, rule, snapshot)
				newRoutes[hostname] = append(newRoutes[hostname], entry)
			}
		}
	}

	// Sort routes by specificity so the most specific matches are tried first.
	newRouteIndexes := make(map[string]*routeIndex, len(newRoutes))
	for hostname := range newRoutes {
		sortRoutesBySpecificity(newRoutes[hostname])
		newRouteIndexes[hostname] = newRouteIndex(newRoutes[hostname])
	}

	return newRoutes, newRouteIndexes
}

// buildRouteEntry creates a single RouteEntry from a route and rule, including
// pre-compiled matchers, filters, expressions, and backend cluster keys.
func (r *Router) buildRouteEntry(ctx context.Context, route *pb.Route, rule *pb.RouteRule, snapshot *config.Snapshot) *RouteEntry {
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

	// Pre-compute cluster keys for all backend refs
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

	return entry
}

// buildPoolsAndBalancers creates upstream pools and load balancers for each cluster.
// It reuses existing pools when possible and preserves LB state (EWMA latency
// data, connection counts, Maglev ring positions) for unchanged endpoints by
// calling UpdateEndpoints on existing LB instances rather than recreating them.
// Stale pools are closed and their metrics cleaned up.
func (r *Router) buildPoolsAndBalancers(ctx context.Context, snapshot *config.Snapshot, prev *routerState, newState *routerState) (map[string]lb.LoadBalancer, map[string]interface{}) {
	newPools := make(map[string]*upstream.Pool)
	newLoadBalancers := make(map[string]lb.LoadBalancer)
	newHashBasedLBs := make(map[string]interface{})

	for _, cluster := range snapshot.Clusters {
		clusterKey := cluster.Namespace + "/" + cluster.Name

		endpointList := snapshot.Endpoints[clusterKey]
		if endpointList == nil {
			r.logger.Warn("No endpoints for cluster", zap.String("cluster", clusterKey))
			continue
		}

		// Create or reuse pool
		if existingPool, ok := prev.pools[clusterKey]; ok {
			existingPool.UpdateEndpoints(endpointList.Endpoints)
			newPools[clusterKey] = existingPool
		} else {
			pool := upstream.NewPool(ctx, cluster, endpointList.Endpoints, r.logger)
			// Attach eBPF passive health monitor if available
			if r.ebpfHealthMon != nil {
				pool.SetEBPFHealthMonitor(r.ebpfHealthMon)
			}
			newPools[clusterKey] = pool
		}

		// Check if endpoints or LB policy changed by computing hash
		endpointHash := hashEndpointList(endpointList.Endpoints, cluster.LbPolicy)
		previousHash, exists := r.endpointVersions[clusterKey]

		if !exists {
			// New cluster: create load balancer from scratch
			r.createLoadBalancer(cluster, clusterKey, endpointList.Endpoints, newLoadBalancers, newHashBasedLBs)
			r.endpointVersions[clusterKey] = endpointHash
			r.clusterLBPolicies[clusterKey] = cluster.LbPolicy
		} else if previousHash != endpointHash {
			// Endpoints or LB policy changed: try to update existing LB in-place
			// to preserve accumulated state (EWMA latency, connection counts, etc.)
			lbPolicyChanged := r.hasLBPolicyChanged(clusterKey, cluster.LbPolicy)
			if lbPolicyChanged {
				// LB policy changed: must recreate the load balancer
				r.createLoadBalancer(cluster, clusterKey, endpointList.Endpoints, newLoadBalancers, newHashBasedLBs)
				r.logger.Info("LB policy changed, recreating load balancer",
					zap.String("cluster", clusterKey),
				)
			} else {
				// Only endpoints changed: update existing LB in-place to preserve state
				r.updateExistingLoadBalancer(clusterKey, endpointList.Endpoints, prev, newLoadBalancers, newHashBasedLBs)
			}
			r.endpointVersions[clusterKey] = endpointHash
			r.clusterLBPolicies[clusterKey] = cluster.LbPolicy
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
			metrics.CleanupClusterMetrics(key)
			delete(r.endpointVersions, key)
			delete(r.clusterLBPolicies, key)
		}
	}

	newState.pools = newPools
	return newLoadBalancers, newHashBasedLBs
}

// hasLBPolicyChanged checks whether the LB policy has changed for a cluster
// by comparing against the previously stored policy.
func (r *Router) hasLBPolicyChanged(clusterKey string, newPolicy pb.LoadBalancingPolicy) bool {
	prevPolicy, exists := r.clusterLBPolicies[clusterKey]
	if !exists {
		return true // New cluster, treat as changed
	}
	return prevPolicy != newPolicy
}

// updateExistingLoadBalancer updates the endpoints on an existing LB instance
// without destroying accumulated state (EWMA latency data, connection counts,
// Maglev ring positions for unchanged endpoints, etc.).
func (r *Router) updateExistingLoadBalancer(clusterKey string, endpoints []*pb.Endpoint, prev *routerState, newLoadBalancers map[string]lb.LoadBalancer, newHashBasedLBs map[string]interface{}) {
	// Try standard load balancers first
	if existingLB, ok := prev.loadBalancers[clusterKey]; ok {
		existingLB.UpdateEndpoints(endpoints)
		newLoadBalancers[clusterKey] = existingLB
		r.logger.Debug("Updated existing load balancer endpoints in-place",
			zap.String("cluster", clusterKey),
			zap.Int("endpoints", len(endpoints)),
		)
		return
	}

	// Try hash-based load balancers (Maglev, RingHash).
	// These have Select(key string) instead of Select(), so they do NOT
	// satisfy the lb.LoadBalancer interface. We must match each concrete
	// type explicitly to call UpdateEndpoints.
	if existingHashLB, ok := prev.hashBasedLBs[clusterKey]; ok {
		switch hlb := existingHashLB.(type) {
		case *lb.Maglev:
			hlb.UpdateEndpoints(endpoints)
		case *lb.RingHash:
			hlb.UpdateEndpoints(endpoints)
		case lb.LoadBalancer:
			hlb.UpdateEndpoints(endpoints)
		default:
			r.logger.Warn("Hash-based LB does not support UpdateEndpoints, skipping in-place update",
				zap.String("cluster", clusterKey),
			)
		}
		newHashBasedLBs[clusterKey] = existingHashLB
		r.logger.Debug("Updated existing hash-based load balancer endpoints in-place",
			zap.String("cluster", clusterKey),
			zap.Int("endpoints", len(endpoints)),
		)
		return
	}

	// No existing LB found (shouldn't happen since we checked hash existence)
	r.logger.Warn("No existing load balancer found for in-place update, this is unexpected",
		zap.String("cluster", clusterKey),
	)
}

// createLoadBalancer creates the appropriate load balancer for a cluster based on its LB policy.
func (r *Router) createLoadBalancer(cluster *pb.Cluster, clusterKey string, endpoints []*pb.Endpoint, newLoadBalancers map[string]lb.LoadBalancer, newHashBasedLBs map[string]interface{}) {
	r.logger.Debug("Endpoints changed, recreating load balancer",
		zap.String("cluster", clusterKey),
		zap.Bool("new_cluster", true),
	)

	switch cluster.LbPolicy {
	case pb.LoadBalancingPolicy_P2C:
		newLoadBalancers[clusterKey] = lb.NewP2C(endpoints)
		r.logger.Debug("Created P2C load balancer", zap.String("cluster", clusterKey))

	case pb.LoadBalancingPolicy_EWMA:
		newLoadBalancers[clusterKey] = lb.NewEWMA(endpoints)
		r.logger.Debug("Created EWMA load balancer", zap.String("cluster", clusterKey))

	case pb.LoadBalancingPolicy_RING_HASH:
		newHashBasedLBs[clusterKey] = lb.NewRingHash(endpoints)
		r.logger.Debug("Created RingHash load balancer", zap.String("cluster", clusterKey))

	case pb.LoadBalancingPolicy_MAGLEV:
		newHashBasedLBs[clusterKey] = lb.NewMaglev(endpoints)
		r.logger.Debug("Created Maglev load balancer", zap.String("cluster", clusterKey))

	case pb.LoadBalancingPolicy_LEAST_CONN:
		newLoadBalancers[clusterKey] = lb.NewLeastConn(endpoints)
		r.logger.Debug("Created LeastConn load balancer", zap.String("cluster", clusterKey))

	case pb.LoadBalancingPolicy_ROUND_ROBIN, pb.LoadBalancingPolicy_LB_POLICY_UNSPECIFIED:
		newLoadBalancers[clusterKey] = lb.NewRoundRobin(endpoints)
		r.logger.Debug("Created RoundRobin load balancer", zap.String("cluster", clusterKey))

	default:
		newLoadBalancers[clusterKey] = lb.NewRoundRobin(endpoints)
		r.logger.Warn("Unknown LB policy, using RoundRobin",
			zap.String("cluster", clusterKey),
			zap.Int32("policy", int32(cluster.LbPolicy)),
		)
	}

	// Wrap with slow start if configured
	if ss := cluster.GetSlowStart(); ss != nil && ss.WindowMs > 0 {
		ssCfg := lb.SlowStartConfig{
			Window:     time.Duration(ss.WindowMs) * time.Millisecond,
			Aggression: ss.Aggression,
		}
		if ssCfg.Aggression <= 0 {
			ssCfg.Aggression = 1.0
		}
		newLoadBalancers[clusterKey] = lb.NewSlowStartManager(
			newLoadBalancers[clusterKey], ssCfg, endpoints,
		)
	}
}

// buildStickyWrappers creates sticky session wrappers for clusters with cookie-based
// session affinity configured.
func (r *Router) buildStickyWrappers(snapshot *config.Snapshot, newLoadBalancers map[string]lb.LoadBalancer) map[string]*lb.StickyWrapper {
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
	return newStickyWrappers
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
	// Close previous ExtProc middleware if any
	if prev.extProc != nil {
		_ = prev.extProc.Close()
	}
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

	// Configure ExtProc middleware from gateway config
	for _, gateway := range snapshot.Gateways {
		if ep := gateway.GetExtProc(); ep != nil && ep.Enabled && ep.Address != "" {
			cfg := &ExtProcConfig{
				Address:                ep.Address,
				Timeout:                time.Duration(ep.TimeoutMs) * time.Millisecond,
				FailOpen:               ep.FailOpen,
				ProcessRequestHeaders:  ep.ProcessRequestHeaders,
				ProcessRequestBody:     ep.ProcessRequestBody,
				ProcessResponseHeaders: ep.ProcessResponseHeaders,
				ProcessResponseBody:    ep.ProcessResponseBody,
			}
			if cfg.Timeout == 0 {
				cfg.Timeout = DefaultExtProcTimeout
			}
			extProcMw, extProcErr := NewExtProcMiddleware(cfg, r.logger, nil)
			if extProcErr != nil {
				r.logger.Error("failed to create ExtProc middleware", zap.Error(extProcErr))
			} else {
				newState.extProc = extProcMw
				r.logger.Info("ExtProc middleware configured",
					zap.String("gateway", gateway.Name),
					zap.String("address", ep.Address),
				)
			}
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
	if snap.extProc != nil {
		_ = snap.extProc.Close()
	}

	for _, pool := range snap.pools {
		pool.Close()
	}
}
