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
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/lb"
	"github.com/piwi3910/novaedge/internal/agent/metrics"
	"github.com/piwi3910/novaedge/internal/agent/protocol"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// handleRoute handles a matched route
func (r *Router) handleRoute(entry *RouteEntry, w http.ResponseWriter, req *http.Request) {
	// Wrap response writer with response filter if needed
	responseWriter := w
	if entry.ResponseFilter != nil && entry.ResponseFilter.HasModifications() {
		responseWriter = NewResponseHeaderWriter(w, entry.ResponseFilter)
	}

	// Pipeline state is already injected into the context by ServeHTTP

	// Create the final handler that forwards to backend (with optional retry)
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.forwardToBackend(entry, w, req)
	})

	// Apply response buffering middleware (wraps backend, buffers before sending to client)
	if entry.Buffering != nil && entry.Buffering.ResponseBuffering {
		respBuf := NewResponseBufferingMiddleware(entry.Buffering)
		handler = respBuf.Wrap(handler)
	}

	// Apply compression middleware (wraps backend response, compresses before client)
	if r.compressionConfig != nil && r.compressionConfig.Enabled {
		comp := NewCompressionMiddleware(r.compressionConfig)
		handler = comp.Wrap(handler)
	}

	// Apply cache middleware: cache executes BEFORE backend forwarding (cache hit skips backend)
	if r.cache != nil && r.cache.config.Enabled {
		handler = r.cache.Middleware(handler)
	}

	// Apply request buffering middleware (buffers request body before forwarding)
	if entry.Buffering != nil && entry.Buffering.RequestBuffering {
		reqBuf := NewRequestBufferingMiddleware(entry.Buffering)
		handler = reqBuf.Wrap(handler)
	}

	// Apply per-route limits middleware (size limits and timeouts before forwarding)
	if entry.Limits != nil {
		limits := NewRequestLimitsMiddleware(entry.Limits)
		handler = limits.Wrap(handler)
	}

	// If a composable pipeline is configured, use it
	if entry.Pipeline != nil && entry.Pipeline.Len() > 0 {
		handler = entry.Pipeline.Wrap(handler)
	}

	// Apply policy middleware in reverse order (last policy wraps first)
	for i := len(entry.Policies) - 1; i >= 0; i-- {
		handler = entry.Policies[i].handler(handler)
	}

	// Wrap with error page interceptor if configured
	if r.errorPages != nil && r.errorPages.IsEnabled() {
		handler = r.errorPages.Wrap(handler)
	}
	// Execute the handler chain: policies -> limits -> req buffering -> cache -> compression -> resp buffering -> error pages -> pipeline -> backend
	handler.ServeHTTP(responseWriter, req)
}

// forwardToBackend forwards the request to the backend
func (r *Router) forwardToBackend(entry *RouteEntry, w http.ResponseWriter, req *http.Request) {
	// Get the parent span from request context
	ctx := req.Context()
	parentSpan := trace.SpanFromContext(ctx)

	// Start a child span for backend forwarding
	tracer := otel.Tracer(tracerName)
	ctx, backendSpan := tracer.Start(ctx, "backend_forward",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer backendSpan.End()

	// Update request context with the new span.
	// NOTE (#129): This WithContext cannot be consolidated further because the
	// backend span is created inside forwardToBackend, after ServeHTTP has
	// already set its context. The span must be injected into the request so
	// that trace propagation carries the per-backend child span to the upstream.
	req = req.WithContext(ctx)

	// Check if this is a gRPC request
	isGRPC := protocol.IsGRPCRequest(req)
	if isGRPC {
		backendSpan.SetAttributes(attribute.Bool("rpc.system", true))
		backendSpan.AddEvent("grpc_request_detected")
		r.logger.Debug("Detected gRPC request",
			zap.String("path", req.URL.Path),
			zap.String("content-type", req.Header.Get("Content-Type")),
		)

		// Validate gRPC request
		if err := r.grpcHandler.ValidateGRPCRequest(req); err != nil {
			backendSpan.SetStatus(codes.Error, "Invalid gRPC request")
			backendSpan.RecordError(err)
			r.logger.Error("Invalid gRPC request", zap.Error(err))
			http.Error(w, "Invalid gRPC request", http.StatusBadRequest)
			return
		}

		// Prepare gRPC request for backend
		req = r.grpcHandler.PrepareGRPCRequest(req)
	}

	// Apply route filters first (header modifications, redirects, rewrites)
	modifiedReq, shouldContinue := applyPrebuiltFilters(entry.Filters, w, req)
	if !shouldContinue {
		// Filter handled the response (e.g., redirect)
		backendSpan.AddEvent("filter_handled_response")
		return
	}
	req = modifiedReq

	// Select backend using weighted selection
	backendRef := selectBackendForRequest(entry.Rule.BackendRefs, req)
	if backendRef == nil {
		backendSpan.SetStatus(codes.Error, "No backend configured")
		http.Error(w, "No backend configured", http.StatusInternalServerError)
		return
	}

	// Fire-and-forget mirror: execute AFTER backend selection but the mirror copy runs in a goroutine.
	// The original request flow is unaffected by the mirror.
	if entry.MirrorConfig != nil {
		// Build interface maps for mirror endpoint selection
		standardLBs := make(map[string]interface{}, len(r.loadBalancers))
		for k, v := range r.loadBalancers {
			standardLBs[k] = v
		}
		r.mirrorRequest(ctx, req, entry.MirrorConfig, r.pools, r.hashBasedLBs, standardLBs)
	}

	clusterKey := entry.BackendClusterKeys[backendRef]
	backendSpan.SetAttributes(
		attribute.String("novaedge.backend.cluster", clusterKey),
		attribute.String("novaedge.backend.namespace", backendRef.Namespace),
		attribute.String("novaedge.backend.name", backendRef.Name),
	)

	// Get pool
	pool, ok := r.pools[clusterKey]
	if !ok {
		backendSpan.SetStatus(codes.Error, "No pool for cluster")
		r.logger.Error("No pool for cluster", zap.String("cluster", clusterKey))
		http.Error(w, "Backend not available", http.StatusServiceUnavailable)
		return
	}

	// Select endpoint using appropriate load balancer
	var endpoint *pb.Endpoint
	var lbType string

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
			lbType = "ring_hash"
		case *lb.Maglev:
			endpoint = hashBalancer.Select(clientIP)
			lbType = "maglev"
		default:
			backendSpan.SetStatus(codes.Error, "Unknown hash-based load balancer type")
			r.logger.Error("Unknown hash-based load balancer type", zap.String("cluster", clusterKey))
			http.Error(w, "Backend configuration error", http.StatusInternalServerError)
			return
		}
	} else if stickyWrapper, hasSW := r.stickyWrappers[clusterKey]; hasSW {
		// Use sticky session wrapper (cookie-based affinity)
		endpoint = stickyWrapper.SelectWithAffinity(req, w)
		lbType = "sticky"
	} else {
		// Use standard load balancer
		loadBalancer, ok := r.loadBalancers[clusterKey]
		if !ok {
			backendSpan.SetStatus(codes.Error, "No load balancer for cluster")
			r.logger.Error("No load balancer for cluster", zap.String("cluster", clusterKey))
			http.Error(w, "Backend not available", http.StatusServiceUnavailable)
			return
		}
		endpoint = loadBalancer.Select()
		lbType = "standard"
	}

	backendSpan.SetAttributes(attribute.String("novaedge.lb.type", lbType))

	if endpoint == nil {
		backendSpan.SetStatus(codes.Error, "No healthy endpoint available")
		r.logger.Error("No healthy endpoint available", zap.String("cluster", clusterKey))
		http.Error(w, "No healthy backend", http.StatusServiceUnavailable)
		return
	}

	// Track backend request timing
	backendStart := time.Now()
	endpointKey := formatEndpointKey(endpoint.Address, endpoint.Port)

	backendSpan.SetAttributes(
		attribute.String("net.peer.name", endpoint.Address),
		attribute.Int("net.peer.port", int(endpoint.Port)),
		attribute.String("novaedge.endpoint", endpointKey),
	)
	backendSpan.AddEvent("forwarding_to_backend", trace.WithAttributes(
		attribute.String("endpoint", endpointKey),
	))

	// Propagate trace context to backend request headers
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Set X-Cache: MISS if caching is enabled and this is a cache miss (no cache header set yet)
	if r.cache != nil && r.cache.config.Enabled && w.Header().Get("X-Cache") == "" {
		w.Header().Set("X-Cache", "MISS")
	}

	// Check for retry configuration on this route rule
	retryPolicy := NewRetryPolicy(entry.Rule.Retry)

	if retryPolicy != nil && !isGRPC {
		// Use retry-aware forwarding
		var loadBalancer lb.LoadBalancer
		if stdLB, ok := r.loadBalancers[clusterKey]; ok {
			loadBalancer = stdLB
		}
		var hashLB interface{}
		if hLB, ok := r.hashBasedLBs[clusterKey]; ok {
			hashLB = hLB
		}

		r.forwardWithRetry(entry, w, req, retryPolicy, pool, clusterKey, loadBalancer, hashLB, backendSpan, r.logger)
	} else {
		// Standard forwarding without retry
		// Forward request to backend
		if err := pool.Forward(endpoint, req, w); err != nil {
			// Record failure for passive health checking
			pool.RecordFailure(endpoint)

			// Record backend failure metrics
			backendDuration := time.Since(backendStart).Seconds()
			metrics.RecordBackendRequest(clusterKey, endpointKey, "failure", backendDuration)

			// Record error on span
			backendSpan.RecordError(err)
			backendSpan.SetStatus(codes.Error, "Backend request failed")
			backendSpan.SetAttributes(
				attribute.Float64("novaedge.backend.duration_seconds", backendDuration),
				attribute.String("novaedge.backend.status", "failure"),
			)

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

			// Record success on span
			backendSpan.SetStatus(codes.Ok, "")
			backendSpan.SetAttributes(
				attribute.Float64("novaedge.backend.duration_seconds", backendDuration),
				attribute.String("novaedge.backend.status", "success"),
			)

			if isGRPC {
				r.logger.Debug("Successfully forwarded gRPC request",
					zap.String("cluster", clusterKey),
					zap.String("endpoint", endpointKey),
					zap.Duration("duration", time.Since(backendStart)),
				)
			}
		}
	}

	// Link to parent span if available (for debugging)
	_ = parentSpan // Use parentSpan to avoid unused variable warning if we need it later
}
