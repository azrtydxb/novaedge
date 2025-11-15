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

	// gRPC handler for gRPC-specific request processing
	grpcHandler *grpchandler.GRPCHandler
}

// RouteEntry represents a single route rule
type RouteEntry struct {
	Route       *pb.Route
	Rule        *pb.RouteRule
	PathMatcher PathMatcher
	Policies    []policyMiddleware
}

// policyMiddleware wraps a policy handler
type policyMiddleware struct {
	name    string
	handler func(http.Handler) http.Handler
}

// PathMatcher matches request paths
type PathMatcher interface {
	Match(path string) bool
}

// ExactMatcher matches exact paths
type ExactMatcher struct {
	Path string
}

func (m *ExactMatcher) Match(path string) bool {
	return path == m.Path
}

// PrefixMatcher matches path prefixes
type PrefixMatcher struct {
	Prefix string
}

func (m *PrefixMatcher) Match(path string) bool {
	return strings.HasPrefix(path, m.Prefix)
}

// RegexMatcher matches paths with regex
type RegexMatcher struct {
	Pattern *regexp.Regexp
}

func (m *RegexMatcher) Match(path string) bool {
	return m.Pattern.MatchString(path)
}

// NewRouter creates a new router
func NewRouter(logger *zap.Logger) *Router {
	return &Router{
		logger:        logger,
		routes:        make(map[string][]*RouteEntry),
		pools:         make(map[string]*upstream.Pool),
		loadBalancers: make(map[string]lb.LoadBalancer),
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

	// Build routing table
	for _, route := range snapshot.Routes {
		for _, hostname := range route.Hostnames {
			for _, rule := range route.Rules {
				entry := &RouteEntry{
					Route:       route,
					Rule:        rule,
					PathMatcher: createPathMatcher(rule),
					Policies:    r.createPolicyMiddleware(route, snapshot),
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

		// Create load balancer
		r.loadBalancers[clusterKey] = lb.NewRoundRobin(endpointList.Endpoints)
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
	for _, match := range entry.Rule.Matches {
		if r.matchCondition(match, req, entry.PathMatcher) {
			return true
		}
	}

	return false
}

// matchCondition checks if a request matches a specific match condition
func (r *Router) matchCondition(match *pb.RouteMatch, req *http.Request, pathMatcher PathMatcher) bool {
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
	for _, headerMatch := range match.Headers {
		headerValue := req.Header.Get(headerMatch.Name)
		if !r.matchHeader(headerMatch, headerValue) {
			return false
		}
	}

	return true
}

// matchHeader checks if a header value matches
func (r *Router) matchHeader(match *pb.HeaderMatch, value string) bool {
	switch match.Type {
	case pb.HeaderMatchType_HEADER_EXACT:
		return value == match.Value
	case pb.HeaderMatchType_HEADER_REGULAR_EXPRESSION:
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

	// Get backend reference
	backendRef := entry.Rule.BackendRef
	if backendRef == nil {
		http.Error(w, "No backend configured", http.StatusInternalServerError)
		return
	}

	clusterKey := fmt.Sprintf("%s/%s", backendRef.Namespace, backendRef.Name)

	// Get load balancer and pool
	loadBalancer, ok := r.loadBalancers[clusterKey]
	if !ok {
		r.logger.Error("No load balancer for cluster", zap.String("cluster", clusterKey))
		http.Error(w, "Backend not available", http.StatusServiceUnavailable)
		return
	}

	pool, ok := r.pools[clusterKey]
	if !ok {
		r.logger.Error("No pool for cluster", zap.String("cluster", clusterKey))
		http.Error(w, "Backend not available", http.StatusServiceUnavailable)
		return
	}

	// Select endpoint using load balancer
	endpoint := loadBalancer.Select()
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

// applyFilters applies route filters to request/response
// createPathMatcher creates a path matcher from a route rule
func createPathMatcher(rule *pb.RouteRule) PathMatcher {
	if len(rule.Matches) == 0 {
		return nil
	}

	// Use the first match's path (simplified for now)
	match := rule.Matches[0]
	if match.Path == nil {
		return nil
	}

	switch match.Path.Type {
	case pb.PathMatchType_EXACT:
		return &ExactMatcher{Path: match.Path.Value}
	case pb.PathMatchType_PATH_PREFIX:
		return &PrefixMatcher{Prefix: match.Path.Value}
	case pb.PathMatchType_REGULAR_EXPRESSION:
		if regex, err := regexp.Compile(match.Path.Value); err == nil {
			return &RegexMatcher{Pattern: regex}
		}
		return nil
	default:
		return nil
	}
}
