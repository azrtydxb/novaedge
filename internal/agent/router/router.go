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
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/config"
	grpchandler "github.com/piwi3910/novaedge/internal/agent/grpc"
	"github.com/piwi3910/novaedge/internal/agent/lb"
	"github.com/piwi3910/novaedge/internal/agent/metrics"
	"github.com/piwi3910/novaedge/internal/agent/policy"
	"github.com/piwi3910/novaedge/internal/agent/protocol"
	"github.com/piwi3910/novaedge/internal/agent/upstream"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// responseWriterWithStatus wraps http.ResponseWriter to capture status code
type responseWriterWithStatus struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriterWithStatus) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriterWithStatus) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

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

	// gRPC handler for gRPC-specific request processing
	grpcHandler *grpchandler.GRPCHandler
}

// RouteEntry represents a single route rule
type RouteEntry struct {
	Route         *pb.Route
	Rule          *pb.RouteRule
	PathMatcher   PathMatcher
	Policies      []policyMiddleware
	HeaderRegexes map[int]*regexp.Regexp // Cached compiled header regex patterns (index -> regex)
}

// policyMiddleware wraps a policy handler
type policyMiddleware struct {
	name    string
	handler func(http.Handler) http.Handler
}

// NewRouter creates a new router
func NewRouter(logger *zap.Logger) *Router {
	return &Router{
		logger:        logger,
		routes:        make(map[string][]*RouteEntry),
		pools:         make(map[string]*upstream.Pool),
		loadBalancers: make(map[string]lb.LoadBalancer),
		hashBasedLBs:  make(map[string]interface{}),
		grpcHandler:   grpchandler.NewGRPCHandler(logger),
	}
}

// ApplyConfig applies a new configuration to the router
func (r *Router) ApplyConfig(snapshot *config.Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Info("Applying router configuration",
		zap.Int("routes", len(snapshot.Routes)),
		zap.Int("clusters", len(snapshot.Clusters)),
	)

	// Clear existing configuration
	r.routes = make(map[string][]*RouteEntry)
	r.loadBalancers = make(map[string]lb.LoadBalancer)
	r.hashBasedLBs = make(map[string]interface{})

	// Build routing table
	for _, route := range snapshot.Routes {
		for _, hostname := range route.Hostnames {
			for _, rule := range route.Rules {
				entry := &RouteEntry{
					Route:         route,
					Rule:          rule,
					PathMatcher:   createPathMatcher(rule),
					Policies:      r.createPolicyMiddleware(route, snapshot),
					HeaderRegexes: compileHeaderRegexes(rule),
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
			pool := upstream.NewPool(cluster, endpointList.Endpoints, r.logger)
			newPools[clusterKey] = pool
		}

		// Create load balancer based on cluster policy
		switch cluster.LbPolicy {
		case pb.LoadBalancingPolicy_P2C:
			r.loadBalancers[clusterKey] = lb.NewP2C(endpointList.Endpoints)
			r.logger.Debug("Created P2C load balancer", zap.String("cluster", clusterKey))

		case pb.LoadBalancingPolicy_EWMA:
			r.loadBalancers[clusterKey] = lb.NewEWMA(endpointList.Endpoints)
			r.logger.Debug("Created EWMA load balancer", zap.String("cluster", clusterKey))

		case pb.LoadBalancingPolicy_RING_HASH:
			// RingHash uses consistent hashing - store separately
			r.hashBasedLBs[clusterKey] = lb.NewRingHash(endpointList.Endpoints)
			r.logger.Debug("Created RingHash load balancer", zap.String("cluster", clusterKey))

		case pb.LoadBalancingPolicy_MAGLEV:
			// Maglev uses consistent hashing - store separately
			r.hashBasedLBs[clusterKey] = lb.NewMaglev(endpointList.Endpoints)
			r.logger.Debug("Created Maglev load balancer", zap.String("cluster", clusterKey))

		case pb.LoadBalancingPolicy_ROUND_ROBIN, pb.LoadBalancingPolicy_LB_POLICY_UNSPECIFIED:
			r.loadBalancers[clusterKey] = lb.NewRoundRobin(endpointList.Endpoints)
			r.logger.Debug("Created RoundRobin load balancer", zap.String("cluster", clusterKey))

		default:
			// Fallback to round robin for unknown policies
			r.loadBalancers[clusterKey] = lb.NewRoundRobin(endpointList.Endpoints)
			r.logger.Warn("Unknown LB policy, using RoundRobin",
				zap.String("cluster", clusterKey),
				zap.Int32("policy", int32(cluster.LbPolicy)),
			)
		}
	}

	// Close pools that are no longer needed
	for key, pool := range r.pools {
		if _, needed := newPools[key]; !needed {
			r.logger.Info("Closing unused pool", zap.String("cluster", key))
			pool.Close()
		}
	}

	r.pools = newPools

	r.logger.Info("Router configuration applied",
		zap.Int("hostnames", len(r.routes)),
		zap.Int("pools", len(r.pools)),
	)

	return nil
}

// ServeHTTP routes incoming HTTP requests
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Track request start time and in-flight requests
	startTime := time.Now()
	metrics.HTTPRequestsInFlight.Inc()
	defer metrics.HTTPRequestsInFlight.Dec()

	// Wrap response writer to capture status code
	wrappedWriter := &responseWriterWithStatus{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	// Defer metrics recording
	defer func() {
		duration := time.Since(startTime).Seconds()
		statusCode := strconv.Itoa(wrappedWriter.statusCode)
		// We'll set cluster in handleRoute, for now use "unknown"
		metrics.RecordHTTPRequest(req.Method, statusCode, "unknown", duration)
	}()

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Extract hostname (without port)
	hostname := req.Host
	if idx := strings.Index(hostname, ":"); idx != -1 {
		hostname = hostname[:idx]
	}

	// Find matching route
	routes, ok := r.routes[hostname]
	if !ok {
		r.logger.Warn("No route for hostname", zap.String("hostname", hostname))
		http.Error(wrappedWriter, "No route found", http.StatusNotFound)
		return
	}

	// Match against route rules
	for _, entry := range routes {
		if r.matchRoute(entry, req) {
			r.handleRoute(entry, wrappedWriter, req)
			return
		}
	}

	// No matching rule
	r.logger.Warn("No matching rule for request",
		zap.String("hostname", hostname),
		zap.String("path", req.URL.Path),
	)
	http.Error(wrappedWriter, "No matching route rule", http.StatusNotFound)
}

// matchRoute checks if a request matches a route entry
func (r *Router) matchRoute(entry *RouteEntry, req *http.Request) bool {
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

// handleRoute handles a matched route
func (r *Router) handleRoute(entry *RouteEntry, w http.ResponseWriter, req *http.Request) {
	// Create the final handler that forwards to backend
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.forwardToBackend(entry, w, req)
	})

	// Apply policy middleware in reverse order (last policy wraps first)
	for i := len(entry.Policies) - 1; i >= 0; i-- {
		handler = entry.Policies[i].handler(handler)
	}

	// Execute the handler chain (policies + backend forwarding)
	handler.ServeHTTP(w, req)
}

// forwardToBackend forwards the request to the backend
func (r *Router) forwardToBackend(entry *RouteEntry, w http.ResponseWriter, req *http.Request) {
	// Check if this is a gRPC request
	isGRPC := protocol.IsGRPCRequest(req)
	if isGRPC {
		r.logger.Debug("Detected gRPC request",
			zap.String("path", req.URL.Path),
			zap.String("content-type", req.Header.Get("Content-Type")),
		)

		// Validate gRPC request
		if err := r.grpcHandler.ValidateGRPCRequest(req); err != nil {
			r.logger.Error("Invalid gRPC request", zap.Error(err))
			http.Error(w, "Invalid gRPC request", http.StatusBadRequest)
			return
		}

		// Prepare gRPC request for backend
		req = r.grpcHandler.PrepareGRPCRequest(req)
	}

	// Apply route filters first (header modifications, redirects, rewrites)
	modifiedReq, shouldContinue := applyFilters(entry.Rule.Filters, w, req)
	if !shouldContinue {
		// Filter handled the response (e.g., redirect)
		return
	}
	req = modifiedReq

	// Select backend using weighted selection
	backendRef := selectWeightedBackend(entry.Rule.BackendRefs)
	if backendRef == nil {
		http.Error(w, "No backend configured", http.StatusInternalServerError)
		return
	}

	clusterKey := fmt.Sprintf("%s/%s", backendRef.Namespace, backendRef.Name)

	// Get pool
	pool, ok := r.pools[clusterKey]
	if !ok {
		r.logger.Error("No pool for cluster", zap.String("cluster", clusterKey))
		http.Error(w, "Backend not available", http.StatusServiceUnavailable)
		return
	}

	// Select endpoint using appropriate load balancer
	var endpoint *pb.Endpoint

	// Check if this cluster uses hash-based load balancing
	if hashLB, ok := r.hashBasedLBs[clusterKey]; ok {
		// Use client IP as the hash key for consistent hashing
		// Extract client IP from request
		clientIP := req.RemoteAddr
		if idx := strings.LastIndex(clientIP, ":"); idx != -1 {
			clientIP = clientIP[:idx]
		}

		// Type assert to the specific hash-based LB type
		switch hashBalancer := hashLB.(type) {
		case *lb.RingHash:
			endpoint = hashBalancer.Select(clientIP)
		case *lb.Maglev:
			endpoint = hashBalancer.Select(clientIP)
		default:
			r.logger.Error("Unknown hash-based load balancer type", zap.String("cluster", clusterKey))
			http.Error(w, "Backend configuration error", http.StatusInternalServerError)
			return
		}
	} else {
		// Use standard load balancer
		loadBalancer, ok := r.loadBalancers[clusterKey]
		if !ok {
			r.logger.Error("No load balancer for cluster", zap.String("cluster", clusterKey))
			http.Error(w, "Backend not available", http.StatusServiceUnavailable)
			return
		}
		endpoint = loadBalancer.Select()
	}

	if endpoint == nil {
		r.logger.Error("No healthy endpoint available", zap.String("cluster", clusterKey))
		http.Error(w, "No healthy backend", http.StatusServiceUnavailable)
		return
	}

	// Track backend request timing
	backendStart := time.Now()
	endpointKey := fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)

	// Forward request to backend
	if err := pool.Forward(endpoint, req, w); err != nil {
		// Record failure for passive health checking
		pool.RecordFailure(endpoint)

		// Record backend failure metrics
		backendDuration := time.Since(backendStart).Seconds()
		metrics.RecordBackendRequest(clusterKey, endpointKey, "failure", backendDuration)

		r.logger.Error("Failed to forward request",
			zap.String("cluster", clusterKey),
			zap.String("endpoint", endpointKey),
			zap.Bool("grpc", isGRPC),
			zap.Error(err),
		)
		http.Error(w, "Backend error", http.StatusBadGateway)
	} else {
		// Record success for passive health checking
		pool.RecordSuccess(endpoint)

		// Record backend success metrics
		backendDuration := time.Since(backendStart).Seconds()
		metrics.RecordBackendRequest(clusterKey, endpointKey, "success", backendDuration)

		if isGRPC {
			r.logger.Debug("Successfully forwarded gRPC request",
				zap.String("cluster", clusterKey),
				zap.String("endpoint", endpointKey),
				zap.Duration("duration", time.Since(backendStart)),
			)
		}
	}
}

// createPolicyMiddleware creates policy middleware for a route
func (r *Router) createPolicyMiddleware(route *pb.Route, snapshot *config.Snapshot) []policyMiddleware {
	var middlewares []policyMiddleware

	// Find policies attached to this route
	routeRef := fmt.Sprintf("%s/%s", route.Namespace, route.Name)

	for _, policyProto := range snapshot.Policies {
		// Check if policy targets this route
		if policyProto.TargetRef == nil {
			continue
		}

		targetRef := fmt.Sprintf("%s/%s", policyProto.TargetRef.Namespace, policyProto.TargetRef.Name)
		if targetRef != routeRef {
			continue
		}

		// Create middleware based on policy type
		switch policyProto.Type {
		case pb.PolicyType_RATE_LIMIT:
			if policyProto.RateLimit != nil {
				limiter := policy.NewRateLimiter(policyProto.RateLimit)
				middlewares = append(middlewares, policyMiddleware{
					name:    fmt.Sprintf("rate-limit-%s", policyProto.Name),
					handler: policy.HandleRateLimit(limiter),
				})
			}

		case pb.PolicyType_CORS:
			if policyProto.Cors != nil {
				cors := policy.NewCORS(policyProto.Cors)
				middlewares = append(middlewares, policyMiddleware{
					name:    fmt.Sprintf("cors-%s", policyProto.Name),
					handler: policy.HandleCORS(cors),
				})
			}

		case pb.PolicyType_IP_ALLOW_LIST:
			if policyProto.IpList != nil {
				filter, err := policy.NewIPAllowListFilter(policyProto.IpList.Cidrs)
				if err == nil {
					middlewares = append(middlewares, policyMiddleware{
						name:    fmt.Sprintf("ip-allow-%s", policyProto.Name),
						handler: policy.HandleIPFilter(filter),
					})
				}
			}

		case pb.PolicyType_IP_DENY_LIST:
			if policyProto.IpList != nil {
				filter, err := policy.NewIPDenyListFilter(policyProto.IpList.Cidrs)
				if err == nil {
					middlewares = append(middlewares, policyMiddleware{
						name:    fmt.Sprintf("ip-deny-%s", policyProto.Name),
						handler: policy.HandleIPFilter(filter),
					})
				}
			}

		case pb.PolicyType_JWT:
			if policyProto.Jwt != nil {
				validator, err := policy.NewJWTValidator(policyProto.Jwt)
				if err == nil {
					middlewares = append(middlewares, policyMiddleware{
						name:    fmt.Sprintf("jwt-%s", policyProto.Name),
						handler: policy.HandleJWT(validator),
					})
				} else {
					r.logger.Error("Failed to create JWT validator",
						zap.String("policy", policyProto.Name),
						zap.Error(err),
					)
				}
			}
		}
	}

	return middlewares
}

// compileHeaderRegexes pre-compiles all header regex patterns for a route rule
// This prevents regex compilation on every request (performance optimization)
func compileHeaderRegexes(rule *pb.RouteRule) map[int]*regexp.Regexp {
	regexes := make(map[int]*regexp.Regexp)

	for matchIdx, match := range rule.Matches {
		for headerIdx, header := range match.Headers {
			if header.Type == pb.HeaderMatchType_HEADER_REGULAR_EXPRESSION {
				if regex, err := regexp.Compile(header.Value); err == nil {
					// Store with a unique key combining match and header index
					key := matchIdx*1000 + headerIdx
					regexes[key] = regex
				}
			}
		}
	}

	return regexes
}

// selectWeightedBackend selects a backend from multiple backends based on their weights
// Uses weighted random selection algorithm
func selectWeightedBackend(backends []*pb.BackendRef) *pb.BackendRef {
	if len(backends) == 0 {
		return nil
	}

	// If only one backend, return it directly
	if len(backends) == 1 {
		return backends[0]
	}

	// Calculate total weight
	totalWeight := int32(0)
	for _, backend := range backends {
		weight := backend.Weight
		if weight <= 0 {
			weight = 1 // Default weight
		}
		totalWeight += weight
	}

	// Generate random number between 0 and totalWeight
	randVal := rand.Int31n(totalWeight)

	// Select backend based on weight
	currentWeight := int32(0)
	for _, backend := range backends {
		weight := backend.Weight
		if weight <= 0 {
			weight = 1 // Default weight
		}
		currentWeight += weight
		if randVal < currentWeight {
			return backend
		}
	}

	// Fallback to first backend (should never reach here)
	return backends[0]
}
