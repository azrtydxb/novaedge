# Development Guide

This guide covers development best practices, coding standards, and internal patterns for contributing to NovaEdge.

## Table of Contents

- [Logging Standards](#logging-standards)
- [Context Propagation](#context-propagation)
- [Error Handling](#error-handling)
- [Performance Optimizations](#performance-optimizations)
- [Testing Guidelines](#testing-guidelines)

---

## Logging Standards

NovaEdge uses structured logging with [zap](https://github.com/uber-go/zap) for high-performance, machine-parseable logs.

### Log Levels

| Level | Usage | Example |
|-------|-------|---------|
| **DEBUG** | Detailed diagnostic information, function entry/exit, variable values | `Processing request method=GET path=/api` |
| **INFO** | Normal operational messages, state changes, successful operations | `Configuration applied version=v1.2.3` |
| **WARN** | Potentially harmful situations, recoverable errors, fallbacks | `Failed to connect to backend, retrying` |
| **ERROR** | Error conditions requiring attention, unrecoverable failures | `Failed to apply configuration` |

### Structured Logging Rules

Always use structured fields - never string concatenation:

```go
// Good
logger.Info("Request completed",
    zap.String("method", r.Method),
    zap.String("path", r.URL.Path),
    zap.Duration("duration", duration),
)

// Bad - never do this
logger.Info(fmt.Sprintf("Request from %s completed in %v", r.RemoteAddr, duration))
```

### Field Naming Conventions

Use consistent `snake_case` field names:

| Field | Type | Description |
|-------|------|-------------|
| `correlation_id` | string | Request correlation ID |
| `cluster` | string | Cluster name |
| `endpoint` | string | Endpoint address |
| `method` | string | HTTP method |
| `path` | string | URL path |
| `status` | int | HTTP status code |
| `duration` | duration | Operation duration |
| `error` | error | Error object (use `zap.Error()`) |

### Correlation IDs

Always include correlation IDs for request-scoped logs:

```go
correlationID := uuid.New().String()
ctx := context.WithValue(r.Context(), "correlation_id", correlationID)

logger.Info("Request started",
    zap.String("correlation_id", correlationID),
    zap.String("method", r.Method),
)
```

---

## Context Propagation

Proper context propagation enables graceful shutdown, timeout handling, and distributed tracing.

### Best Practices

1. **Always propagate context** - Pass context as the first parameter
2. **Never use context.Background() in library code** - Only in `main()` or tests
3. **Derive child contexts** - Use `context.WithCancel`, `context.WithTimeout`
4. **Use request context** - HTTP handlers should use `r.Context()`

### Function Signatures

```go
// Good - Context as first parameter
func ProcessRequest(ctx context.Context, req *Request) error

// Bad - No context
func ProcessRequest(req *Request) error
```

### HTTP Handlers

```go
// Good - Use request context
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Derive child context with timeout
    opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    // Pass context to downstream operations
    result, err := s.service.Process(opCtx, r)
}
```

### Acceptable Uses of context.Background()

1. **main() function** - Top-level context initialization
2. **Test setup** - Creating root context for tests
3. **Independent background tasks** - Tasks with no parent lifecycle

---

## Error Handling

NovaEdge uses structured error handling with rich context for debugging.

### Custom Error Types

Located in `internal/pkg/errors/errors.go`:

```go
// Network errors
err := pkgerrors.NewNetworkError("connection timeout").
    WithField("host", "backend.example.com").
    WithField("port", 8080)

// Configuration errors
err := pkgerrors.NewConfigError("invalid gateway spec").
    WithField("gateway", "my-gateway")

// Validation errors
err := pkgerrors.NewValidationError("hostname", "required", "hostname cannot be empty")
```

### Error Wrapping

Always wrap errors with context:

```go
if err != nil {
    return fmt.Errorf("failed to connect to backend %s: %w", backendURL, err)
}
```

### Error Checking

Use `errors.Is()` and `errors.As()`:

```go
if errors.Is(err, pkgerrors.ErrConnectionTimeout) {
    // Handle timeout
}

var validationErr *pkgerrors.ValidationError
if errors.As(err, &validationErr) {
    log.Error("Validation failed", "field", validationErr.Field)
}
```

---

## Performance Optimizations

### Connection Pool Configuration

Configure connection pools per cluster in `internal/agent/upstream/pool.go`:

```go
type ConnectionPool struct {
    MaxIdleConns         int32   // Maximum total idle connections (default: 100)
    MaxIdleConnsPerHost  int32   // Maximum idle per host (default: 10)
    MaxConnsPerHost      int32   // Maximum total per host (0 = unlimited)
    IdleConnTimeoutMs    int64   // Idle timeout in ms (default: 90000)
}
```

### Load Balancer State Caching

LB state is cached and only recreated when endpoints change:

```go
// Hash-based change detection
endpointHash := hashEndpointList(endpoints)
if previousHash != endpointHash {
    r.loadBalancers[clusterKey] = lb.NewRoundRobin(endpoints)
    r.endpointVersions[clusterKey] = endpointHash
}
```

**Impact**: ~90% faster config updates when endpoints unchanged.

### Metrics Cardinality Reduction

Prevent Prometheus metric explosion:

```go
ConfigureMetrics(MetricsConfig{
    EnableSampling:         true,
    SampleRate:             10,    // 10% sampling
    MaxEndpointCardinality: 100,   // Max 100 endpoints per cluster
})
```

### Memory Pools

Use `sync.Pool` for frequently allocated objects:

```go
var responseWriterPool = sync.Pool{
    New: func() interface{} {
        return &responseWriterWithStatus{statusCode: http.StatusOK}
    },
}

// Get from pool
rw := responseWriterPool.Get().(*responseWriterWithStatus)
defer responseWriterPool.Put(rw)
```

**Impact**: ~40% reduction in allocations per request.

### Benchmarks

Run performance benchmarks:

```bash
# All benchmarks
make bench

# Specific benchmarks
go test -bench=BenchmarkRouteMatching -benchmem ./internal/agent/router/
```

---

## Testing Guidelines

### Test Organization

```
internal/
├── agent/
│   ├── router/
│   │   ├── router.go
│   │   └── router_test.go      # Unit tests
│   └── ...
└── controller/
    ├── controller.go
    └── controller_test.go
test/
└── integration/                 # Integration tests
    └── ...
```

### Running Tests

```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run specific package tests
go test -v ./internal/agent/router/...

# Run integration tests
go test -v ./test/integration/...
```

### Unit Test Patterns

```go
func TestRouteMatching(t *testing.T) {
    tests := []struct {
        name     string
        path     string
        expected bool
    }{
        {"exact match", "/api/v1", true},
        {"prefix match", "/api/v1/users", true},
        {"no match", "/other", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := router.Match(tt.path)
            if result != tt.expected {
                t.Errorf("got %v, want %v", result, tt.expected)
            }
        })
    }
}
```

### Testing Context Cancellation

```go
func TestContextCancellation(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())

    errCh := make(chan error, 1)
    go func() {
        errCh <- operation(ctx)
    }()

    cancel()

    err := <-errCh
    if err != context.Canceled {
        t.Errorf("expected context.Canceled, got %v", err)
    }
}
```

### Mocking with Interfaces

Use the interfaces in `internal/agent/interfaces.go` for mocking:

```go
// Production
var forwarder Forwarder = upstream.NewPool(ctx, cluster, endpoints, logger)

// Test
var forwarder Forwarder = &MockForwarder{
    ForwardFunc: func(ctx context.Context, req *http.Request) (*http.Response, error) {
        return &http.Response{StatusCode: 200}, nil
    },
}
```

---

## TLS Configuration

### Hardened Defaults

All TLS configurations use secure defaults from `internal/pkg/tlsutil/tls.go`:

- **Minimum TLS Version**: TLS 1.3 (TLS 1.2 for compatibility)
- **Cipher Suites**: AEAD ciphers only (AES-GCM, ChaCha20-Poly1305)
- **Certificate Validation**: Proper CA verification

### Creating TLS Configs

```go
// Server TLS
config, err := tlsutil.CreateServerTLSConfig(certPEM, keyPEM)

// Client TLS with mTLS
config, err := tlsutil.CreateClientTLSConfigWithMTLS(caCertPEM, clientCertPEM, clientKeyPEM, serverName)

// Backend TLS
config, err := tlsutil.CreateBackendTLSConfig(caCertPEM, serverName, skipVerify)
```

### SNI Support

```go
sniConfig := &tlsutil.SNIConfig{
    DefaultCert:  defaultCert,
    Certificates: map[string]*tls.Certificate{
        "api.example.com": apiCert,
        "*.example.com":   wildcardCert,
    },
    MinVersion: tls.VersionTLS13,
}
config, err := tlsutil.CreateServerTLSConfigWithSNI(sniConfig)
```

---

## Configuration Validation

Use the validator in `internal/agent/config/validation.go`:

```go
validator := config.NewValidator()

if err := validator.ValidateSnapshot(snapshot); err != nil {
    var validationErr *pkgerrors.ValidationError
    if errors.As(err, &validationErr) {
        log.Error("Validation failed",
            "field", validationErr.Field,
            "rule", validationErr.Rule,
            "message", validationErr.Message,
        )
    }
    return err
}
```

---

## Code Quality Checklist

Before submitting code:

- [ ] All tests pass (`make test`)
- [ ] No linting errors (`make lint`)
- [ ] Structured logging with consistent field names
- [ ] Context propagated through all call chains
- [ ] Errors wrapped with context
- [ ] No `context.Background()` in library code
- [ ] Interface abstractions for testability
- [ ] Benchmarks for performance-critical code
- [ ] Documentation updated if API changed

---

## Related Documentation

- [CLAUDE.md](../CLAUDE.md) - Development guidelines for Claude Code
- [Deployment Guide](deployment-guide.md) - Production deployment
- [NovaEdge Architecture](../NovaEdge_FullSpec.md) - System design
