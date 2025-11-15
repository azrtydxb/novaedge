# Router.go Tracing Instrumentation Guide

The router.go file requires the following OpenTelemetry tracing instrumentation. These changes enable distributed tracing across the entire request path from ingress through the proxy to backend services.

## 1. Add OpenTelemetry Imports

Add the following imports to the import section of `/Users/pascal/Documents/git/novaedge/internal/agent/router/router.go`:

```go
"go.opentelemetry.io/otel"
"go.opentelemetry.io/otel/attribute"
"go.opentelemetry.io/otel/codes"
"go.opentelemetry.io/otel/propagation"
"go.opentelemetry.io/otel/trace"
```

## 2. Add Tracer Field to Router Struct

Add a tracer field to the Router struct (around line 88):

```go
type Router struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// ... existing fields ...

	// Tracer for distributed tracing
	tracer trace.Tracer
}
```

## 3. Initialize Tracer in NewRouter

Update the NewRouter function to initialize the tracer (around line 146):

```go
func NewRouter(logger *zap.Logger) *Router {
	return &Router{
		logger:        logger,
		routes:        make(map[string][]*RouteEntry),
		pools:         make(map[string]*upstream.Pool),
		loadBalancers: make(map[string]lb.LoadBalancer),
		grpcHandler:   grpchandler.NewGRPCHandler(logger),
		wsProxy:       server.NewWebSocketProxy(logger),
		tracer:        otel.Tracer("novaedge/router"),  // ADD THIS LINE
	}
}
```

## 4. Instrument ServeHTTP Method

Replace the beginning of the ServeHTTP method (starting around line 225) with:

```go
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Extract trace context from incoming request headers
	ctx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))

	// Start tracing span for this request
	ctx, span := r.tracer.Start(ctx, "http.request",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.url", req.URL.String()),
			attribute.String("http.host", req.Host),
			attribute.String("http.scheme", req.URL.Scheme),
			attribute.String("http.target", req.URL.Path),
		),
	)
	defer span.End()

	// Update request with trace context
	req = req.WithContext(ctx)

	// Track request start time and in-flight requests
	startTime := time.Now()
	metrics.HTTPRequestsInFlight.Inc()
	defer metrics.HTTPRequestsInFlight.Dec()

	// Wrap response writer to capture status code
	wrappedWriter := &responseWriterWithStatus{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	// Defer metrics and span finalization
	defer func() {
		duration := time.Since(startTime).Seconds()
		statusCode := strconv.Itoa(wrappedWriter.statusCode)

		// Record span status and attributes
		span.SetAttributes(
			attribute.Int("http.status_code", wrappedWriter.statusCode),
			attribute.Float64("http.duration_seconds", duration),
		)

		// Set span status based on HTTP status code
		if wrappedWriter.statusCode >= 400 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", wrappedWriter.statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}

		// Record metrics
		metrics.RecordHTTPRequest(req.Method, statusCode, "unknown", duration)
	}()

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Extract hostname (without port)
	hostname := req.Host
	if idx := strings.Index(hostname, ":"); idx != -1 {
		hostname = hostname[:idx]
	}

	span.SetAttributes(attribute.String("http.hostname", hostname))

	// Find matching route
	routes, ok := r.routes[hostname]
	if !ok {
		r.logger.Warn("No route for hostname", zap.String("hostname", hostname))
		span.SetAttributes(attribute.String("error.type", "no_route_found"))
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
	span.SetAttributes(attribute.String("error.type", "no_matching_rule"))
	http.Error(wrappedWriter, "No matching route rule", http.StatusNotFound)
}
```

## 5. Instrument forwardToBackend Method

In the `forwardToBackend` method, add span creation and instrumentation for backend requests. Find the section that starts with `// Track backend request timing` (around line 464) and add the span creation just before it:

```go
// Create child span for backend request
ctx := req.Context()
_, backendSpan := r.tracer.Start(ctx, "backend.forward",
	trace.WithSpanKind(trace.SpanKindClient),
	trace.WithAttributes(
		attribute.String("backend.cluster", clusterKey),
		attribute.String("backend.endpoint", fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)),
		attribute.String("backend.address", endpoint.Address),
		attribute.Int("backend.port", int(endpoint.Port)),
	),
)
defer backendSpan.End()

// Track backend request timing
backendStart := time.Now()
endpointKey := fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)
```

Then in the error handling section (after `metrics.RecordBackendRequest(clusterKey, endpointKey, "failure", backendDuration)`), add:

```go
// Update span with error
backendSpan.RecordError(err)
backendSpan.SetStatus(codes.Error, "Backend forward failed")
backendSpan.SetAttributes(
	attribute.String("error.message", err.Error()),
	attribute.Float64("backend.duration_seconds", backendDuration),
)
```

And in the success section (after `metrics.RecordBackendRequest(clusterKey, endpointKey, "success", backendDuration)`), add:

```go
// Update span with success
backendSpan.SetStatus(codes.Ok, "")
backendSpan.SetAttributes(
	attribute.Float64("backend.duration_seconds", backendDuration),
)
```

## Summary

These changes enable:
1. **Trace context extraction** from incoming requests
2. **Span creation** for each HTTP request with appropriate attributes
3. **Span propagation** through the request lifecycle
4. **Backend span creation** as child spans for upstream calls
5. **Status recording** for both successful and failed requests
6. **Duration tracking** at both the proxy and backend levels

The traces will flow from client → NovaEdge proxy → backend service, enabling end-to-end request tracing.
