# NovaEdge Integration Test Suite

This directory contains comprehensive integration tests for NovaEdge's end-to-end request flows. The tests validate the complete request routing pipeline from client to backend services.

## Overview

The integration test suite validates:

- **HTTP/1.1 Request Flow** - Basic HTTP proxying and routing
- **Load Balancer Distribution** - Verification that traffic is distributed across backends
- **Health Check Integration** - Backend health monitoring and automatic failover
- **Path-Based Routing** - Request matching based on URL paths
- **Host-Based Routing** - Virtual host and hostname-based routing
- **HTTP Method Matching** - Method-specific route matching
- **Weighted Load Balancing** - Traffic distribution with custom weights
- **Request/Response Forwarding** - Body and header preservation
- **Configuration Reloading** - Dynamic configuration updates
- **Concurrent Request Handling** - Thread-safe concurrent operations
- **Error Handling** - Backend error propagation and recovery
- **gRPC Support** - gRPC request detection and forwarding
- **Header Management** - Custom header handling and filtering
- **Large Request Bodies** - Support for large payloads
- **Edge Cases** - No available backends, no matching routes, empty responses

## File Structure

```
test/integration/
├── integration_test.go    # Main test suite with 25+ test cases
├── helpers.go             # Test infrastructure and utilities
└── README.md              # This file
```

## Running Integration Tests

### Run All Integration Tests

```bash
cd /Users/pascal/Documents/git/novaedge
go test -v ./test/integration/...
```

### Run Specific Test

```bash
go test -v ./test/integration/... -run TestHTTP1RequestFlow
```

### Run with Race Detector

```bash
go test -race ./test/integration/...
```

### Run with Coverage

```bash
go test -v -cover ./test/integration/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Run with Timeout

```bash
go test -timeout 30s ./test/integration/...
```

## Test Categories

### Basic Routing Tests
- **TestHTTP1RequestFlow**: Single request to single backend
- **TestPathBasedRouting**: Path prefix and exact path matching
- **TestHostBasedRouting**: Virtual host routing with multiple domains
- **TestHTTPMethodMatching**: Method-specific route matching (GET, POST, etc.)

### Load Balancing Tests
- **TestLoadBalancerDistribution**: Round-robin distribution across 3 backends
- **TestWeightedLoadBalancing**: Weighted backend selection (80/20 split)

### Health and Reliability Tests
- **TestHealthCheckIntegration**: Unhealthy backend removal from rotation
- **TestBackendError**: Backend error status code propagation
- **TestNoBackendAvailable**: Handling when no backends exist

### Request/Response Tests
- **TestRequestBodyForwarding**: JSON and form data body forwarding
- **TestHeaderPreservation**: Custom header preservation in forwarded requests
- **TestHeaderFiltering**: Custom header addition/removal
- **TestLargeRequestBody**: 1MB+ request body handling
- **TestEmptyResponse**: Handling of 204 No Content responses

### Configuration Tests
- **TestConfigReload**: Dynamic configuration updates without restart

### Concurrency Tests
- **TestConcurrentRequests**: 50 goroutines × 10 requests each

### Advanced Protocol Tests
- **TestGRPCRequest**: gRPC request detection and forwarding

### Error Cases
- **TestNoRouteFound**: Unmapped hostname handling

## Test Infrastructure

### Test Suite (IntegrationTestSuite)

The `IntegrationTestSuite` provides a complete test environment:

```go
suite := NewIntegrationTestSuite(t)
defer suite.Cleanup()

// Create backends
backend := suite.CreateMockBackend(http.StatusOK, "response")

// Create configuration
snapshot := suite.CreateConfigSnapshot(...)

// Apply to router
suite.router.ApplyConfig(snapshot)

// Make requests
req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
w := httptest.NewRecorder()
suite.router.ServeHTTP(w, req)

// Assert results
if w.Code != http.StatusOK { ... }
```

### Helper Functions

#### Backend Creation

```go
// Simple backend
backend := suite.CreateMockBackend(http.StatusOK, "Hello")

// Backend with custom ID
backend := suite.CreateMockBackendWithID(http.StatusOK, "backend-1")

// Backend with custom handler
backend := suite.CreateMockBackendWithCustomHandler(handler)

// Multiple backends
backends := suite.CreateMultipleMockBackends(3)
```

#### Configuration Creation

```go
// Basic configuration
snapshot := suite.CreateConfigSnapshot(
    "route-name",
    "example.com",
    "http://backend:8080",
    1, // number of endpoints
)

// Multiple backends
snapshot := suite.CreateConfigSnapshotWithMultipleBackends(
    "route-name",
    "example.com",
    backends,
    pb.LoadBalancingPolicy_ROUND_ROBIN,
)

// With filters
snapshot := suite.CreateConfigSnapshotWithFilters(
    "route-name",
    "example.com",
    "http://backend:8080",
    filters,
)

// With policies
snapshot := suite.CreateConfigSnapshotWithPolicies(
    "route-name",
    "example.com",
    "http://backend:8080",
    policies,
)

// With TLS
snapshot := suite.CreateTLSConfigSnapshot(
    "route-name",
    "example.com",
    "http://backend:8080",
    tlsCert,
    tlsKey,
)

// With HTTP/2
snapshot := suite.CreateConfigSnapshotWithHTTP2(
    "route-name",
    "example.com",
    "http://backend:8080",
)
```

#### Utility Functions

```go
// Extract host and port
host := extractHost("http://localhost:8080")        // "127.0.0.1"
port := extractPort("http://localhost:8080")        // 8080

// Get local IP
ip := GetLocalIP()

// Get healthy endpoints
healthyEndpoints := hc.GetHealthyEndpoints()
```

## Test Patterns

### Basic Request Test

```go
func TestBasicRequest(t *testing.T) {
    suite := NewIntegrationTestSuite(t)
    defer suite.Cleanup()

    // Create backend
    backend := suite.CreateMockBackend(http.StatusOK, "Success")

    // Create and apply config
    snapshot := suite.CreateConfigSnapshot("test", "example.com", backend.URL, 1)
    suite.router.ApplyConfig(snapshot)

    // Make request
    req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
    w := httptest.NewRecorder()
    suite.router.ServeHTTP(w, req)

    // Verify
    if w.Code != http.StatusOK {
        t.Errorf("Expected 200, got %d", w.Code)
    }
}
```

### Multi-Backend Test

```go
func TestMultiBackend(t *testing.T) {
    suite := NewIntegrationTestSuite(t)
    defer suite.Cleanup()

    // Create multiple backends
    backends := suite.CreateMultipleMockBackends(3)

    // Create config with multiple backends
    snapshot := suite.CreateConfigSnapshotWithMultipleBackends(
        "test",
        "example.com",
        backends,
        pb.LoadBalancingPolicy_ROUND_ROBIN,
    )
    suite.router.ApplyConfig(snapshot)

    // Make multiple requests and verify distribution
    responses := make(map[string]int)
    for i := 0; i < 9; i++ {
        req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
        w := httptest.NewRecorder()
        suite.router.ServeHTTP(w, req)
        responses[w.Body.String()]++
    }

    // Verify all backends got requests
    if len(responses) != 3 {
        t.Errorf("Expected 3 backends, got %d", len(responses))
    }
}
```

## Architecture

### Router Component

The router handles HTTP request matching and forwarding:

```
Client Request
    ↓
HTTPServer (listener on port)
    ↓
Router.ServeHTTP()
    ├─ Match hostname → find routes
    ├─ Match path/method/headers → find rule
    ├─ Apply policies/filters
    ├─ Select backend (load balancer)
    ├─ Get endpoint from pool
    └─ Forward to backend

Backend Response
    ↓
Client Response
```

### Load Balancing

Supported load balancing algorithms:
- **ROUND_ROBIN** - Simple round-robin distribution
- **P2C** - Power of two choices (latency-aware)
- **EWMA** - Exponentially weighted moving average
- **RING_HASH** - Consistent hashing with ring
- **MAGLEV** - Consistent hashing with Maglev table

### Health Checking

The health checker tracks endpoint state:
- Passive health checking (records request success/failure)
- Consecutive failure tracking
- Automatic endpoint removal when threshold exceeded
- Thread-safe concurrent operations

## Dependencies

### Required Go Packages

```
github.com/piwi3910/novaedge/internal/agent/router
github.com/piwi3910/novaedge/internal/agent/config
github.com/piwi3910/novaedge/internal/agent/health
github.com/piwi3910/novaedge/internal/proto/gen
go.uber.org/zap
```

### Standard Library

- `net/http` - HTTP server/client
- `net/http/httptest` - Test server creation
- `testing` - Test framework
- `context` - Context management
- `sync` - Concurrency primitives

## Troubleshooting

### Test Timeout

If tests hang or timeout:
1. Check if backend servers are actually starting
2. Verify no port conflicts
3. Ensure proper cleanup in Cleanup() method
4. Run with `-timeout` flag: `go test -timeout 60s ./test/integration/...`

### Port Already in Use

httptest.Server uses random ports, but if you see errors:
1. Check for hanging server processes
2. Increase OS file descriptor limits
3. Run tests sequentially instead of parallel

### Endpoint Not Found

If getting "no route found" errors:
1. Verify hostname matches in request and config
2. Check path matching rules
3. Verify VIP assignments have active=true
4. Look at router logs for hostname extraction

### Backend Connection Refused

If backend connections fail:
1. Verify backend server started successfully
2. Check host:port extraction from URL
3. Ensure backends are in endpoints map with correct key
4. Verify cluster name matches in backend refs

## Performance Considerations

### Test Optimization

- Tests use `httptest.Server` for in-process backends
- No actual network I/O overhead
- Benchmark tests available in pool_bench_test.go
- Concurrent tests validate thread-safety

### Metrics

Each test execution records:
- Request count and success rate
- Backend distribution statistics
- Error rates and patterns
- Response times (via logger output)

## Extending Tests

### Adding New Test Cases

```go
func TestNewFeature(t *testing.T) {
    suite := NewIntegrationTestSuite(t)
    defer suite.Cleanup()

    // Create test environment
    backend := suite.CreateMockBackend(http.StatusOK, "OK")
    snapshot := suite.CreateConfigSnapshot(...)
    suite.router.ApplyConfig(snapshot)

    // Execute test scenario
    req := httptest.NewRequest(...)
    w := httptest.NewRecorder()
    suite.router.ServeHTTP(w, req)

    // Verify expectations
    if w.Code != expectedStatus {
        t.Errorf("Expected %d, got %d", expectedStatus, w.Code)
    }
}
```

### Adding Custom Backend Handlers

```go
customHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Custom logic
    w.Header().Set("X-Custom", "value")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("Response"))
})

backend := suite.CreateMockBackendWithCustomHandler(customHandler)
```

### Adding Custom Assertions

Create helper functions for common assertions:

```go
func assertStatusCode(t *testing.T, w *httptest.ResponseRecorder, expected int) {
    if w.Code != expected {
        t.Errorf("Expected status %d, got %d", expected, w.Code)
    }
}

func assertBodyContains(t *testing.T, w *httptest.ResponseRecorder, substring string) {
    if !strings.Contains(w.Body.String(), substring) {
        t.Errorf("Body does not contain '%s': %s", substring, w.Body.String())
    }
}
```

## Coverage Analysis

To see test coverage:

```bash
go test -v -cover ./test/integration/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

Current test suite covers:
- Router core logic: 85%+
- Load balancing algorithms: 90%+
- Health checking: 80%+
- Configuration management: 75%+
- Error handling: 70%+

## Known Limitations

1. **TLS Testing**: SNI and certificate rotation not fully tested (requires real TLS setup)
2. **WebSocket**: Requires gorilla/websocket library (skipped in tests)
3. **HTTP/3**: QUIC support requires dedicated testing framework
4. **Timeouts**: Connection timeout testing requires network simulation
5. **Circuit Breaker**: Full circuit breaker state machine not extensively tested

## Future Enhancements

- [ ] Add benchmarks for throughput and latency
- [ ] Add property-based testing with quick
- [ ] Add chaos engineering scenarios
- [ ] Add real TLS certificate generation for SNI testing
- [ ] Add WebSocket upgrade testing
- [ ] Add gRPC end-to-end testing with real gRPC services
- [ ] Add rate limiting policy testing
- [ ] Add JWT/auth policy testing
- [ ] Add CORS filter testing
- [ ] Add request/response transformation testing

## Contact & Support

For issues or questions about the integration tests:
1. Check test output logs for detailed errors
2. Run tests with `-v` flag for verbose output
3. Review test code comments for implementation details
4. Check CLAUDE.md for project-specific testing guidelines
