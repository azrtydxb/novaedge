# NovaEdge Logging Standards

This document defines logging standards and best practices for NovaEdge components.

## Table of Contents

- [Log Levels](#log-levels)
- [Structured Logging](#structured-logging)
- [Field Naming Conventions](#field-naming-conventions)
- [Correlation IDs](#correlation-ids)
- [Component-Specific Guidelines](#component-specific-guidelines)
- [Examples](#examples)

## Log Levels

Use appropriate log levels based on the severity and purpose of the message:

### DEBUG
- Detailed diagnostic information for troubleshooting
- Function entry/exit points
- Internal state changes
- Variable values during processing
- **Not logged in production by default**

```go
logger.Debug("Processing request",
    zap.String("method", r.Method),
    zap.String("path", r.URL.Path),
    zap.String("correlation_id", correlationID),
)
```

### INFO
- Normal operational messages
- Significant state changes
- Configuration updates
- Successful operations
- Request/response summaries

```go
logger.Info("Configuration applied successfully",
    zap.String("version", snapshot.Version),
    zap.Int("gateways", len(snapshot.Gateways)),
    zap.Int("routes", len(snapshot.Routes)),
)
```

### WARN
- Potentially harmful situations
- Recoverable errors
- Degraded functionality
- Fallback to defaults
- Retryable failures

```go
logger.Warn("Failed to connect to backend, retrying",
    zap.String("backend", backendURL),
    zap.Error(err),
    zap.Duration("retry_delay", backoff),
)
```

### ERROR
- Error conditions that require attention
- Unrecoverable failures
- Resource exhaustion
- Configuration errors
- Security violations

```go
logger.Error("Failed to apply configuration",
    zap.String("version", snapshot.Version),
    zap.Error(err),
)
```

## Structured Logging

Always use structured logging with zap fields. Never use string concatenation or formatting in messages.

### ❌ Bad
```go
logger.Info(fmt.Sprintf("Request from %s completed in %v", r.RemoteAddr, duration))
logger.Error("Error: " + err.Error())
```

### ✅ Good
```go
logger.Info("Request completed",
    zap.String("remote_addr", r.RemoteAddr),
    zap.Duration("duration", duration),
)
logger.Error("Request failed",
    zap.Error(err),
)
```

## Field Naming Conventions

Use consistent field names across all log statements:

### Standard Fields

| Field Name | Type | Description | Example |
|------------|------|-------------|---------|
| `correlation_id` | string | Request correlation ID | `"req-123abc"` |
| `trace_id` | string | Distributed trace ID | `"trace-xyz789"` |
| `cluster` | string | Cluster name | `"default/backend-cluster"` |
| `endpoint` | string | Endpoint address | `"10.0.1.5:8080"` |
| `backend` | string | Backend URL | `"http://10.0.1.5:8080"` |
| `vip` | string | VIP name | `"frontend-vip"` |
| `gateway` | string | Gateway name | `"default/api-gateway"` |
| `route` | string | Route name | `"default/api-route"` |
| `listener` | string | Listener name | `"https-listener"` |
| `method` | string | HTTP method | `"GET"` |
| `path` | string | URL path | `"/api/v1/users"` |
| `host` | string | HTTP Host header | `"api.example.com"` |
| `remote_addr` | string | Client address | `"192.168.1.100:54321"` |
| `status` | int | HTTP status code | `200` |
| `duration` | duration | Operation duration | `time.Duration` |
| `latency` | float64 | Latency in seconds | `0.123` |
| `error` | error | Error object | Use `zap.Error(err)` |
| `version` | string | Configuration version | `"v1.2.3"` |
| `node` | string | Node name | `"worker-01"` |
| `port` | int32 | Port number | `8080` |
| `protocol` | string | Protocol name | `"HTTP/2"` |

### Naming Rules

1. **Use snake_case** for field names (not camelCase)
2. **Be consistent** - always use the same field name for the same concept
3. **Be specific** - use `remote_addr` not `addr`, `backend_url` not `url`
4. **Avoid abbreviations** unless widely understood (e.g., `vip`, `tls`, `http`)
5. **Use singular forms** for single values, plural for arrays/counts

### ❌ Bad - Inconsistent field names
```go
logger.Info("Request started", zap.String("clusterName", cluster))
logger.Info("Request completed", zap.String("cluster", cluster))
```

### ✅ Good - Consistent field names
```go
logger.Info("Request started", zap.String("cluster", cluster))
logger.Info("Request completed", zap.String("cluster", cluster))
```

## Correlation IDs

Use correlation IDs to track requests through the system:

### Generating Correlation IDs

```go
import (
    "github.com/google/uuid"
)

// Generate a correlation ID for each request
correlationID := uuid.New().String()

// Add to request context
ctx := context.WithValue(r.Context(), "correlation_id", correlationID)
r = r.WithContext(ctx)

// Log with correlation ID
logger.Info("Request started",
    zap.String("correlation_id", correlationID),
    zap.String("method", r.Method),
    zap.String("path", r.URL.Path),
)
```

### Propagating Correlation IDs

```go
// Extract correlation ID from context
func getCorrelationID(ctx context.Context) string {
    if id, ok := ctx.Value("correlation_id").(string); ok {
        return id
    }
    return ""
}

// Use in all logs for the request
logger.Debug("Routing request",
    zap.String("correlation_id", getCorrelationID(r.Context())),
    zap.String("route", routeName),
)
```

### HTTP Headers

Add correlation ID to response headers for client tracking:

```go
w.Header().Set("X-Correlation-ID", correlationID)
```

## Component-Specific Guidelines

### Controller

```go
logger.Info("Reconciling gateway",
    zap.String("gateway", fmt.Sprintf("%s/%s", gw.Namespace, gw.Name)),
    zap.String("version", gw.ResourceVersion),
)

logger.Error("Failed to build configuration snapshot",
    zap.String("node", nodeName),
    zap.Error(err),
)
```

### Agent - Router

```go
logger.Debug("Routing request",
    zap.String("correlation_id", correlationID),
    zap.String("method", r.Method),
    zap.String("host", r.Host),
    zap.String("path", r.URL.Path),
    zap.String("route", routeName),
)

logger.Info("Request completed",
    zap.String("correlation_id", correlationID),
    zap.String("method", r.Method),
    zap.String("path", r.URL.Path),
    zap.Int("status", status),
    zap.Duration("duration", duration),
)
```

### Agent - Health Checker

```go
logger.Debug("Health check started",
    zap.String("cluster", clusterKey),
    zap.String("endpoint", endpointKey),
)

logger.Warn("Endpoint became unhealthy",
    zap.String("cluster", clusterKey),
    zap.String("endpoint", endpointKey),
    zap.Uint32("consecutive_failures", failures),
    zap.Error(lastError),
)

logger.Info("Endpoint became healthy",
    zap.String("cluster", clusterKey),
    zap.String("endpoint", endpointKey),
    zap.Uint32("consecutive_successes", successes),
)
```

### Agent - VIP Manager

```go
logger.Info("VIP acquired",
    zap.String("vip", vipName),
    zap.String("address", vipAddr),
    zap.String("mode", mode),
)

logger.Warn("VIP failover initiated",
    zap.String("vip", vipName),
    zap.String("from_node", oldNode),
    zap.String("to_node", newNode),
    zap.String("reason", reason),
)
```

### Agent - Upstream Pool

```go
logger.Debug("Forwarding request to backend",
    zap.String("correlation_id", correlationID),
    zap.String("cluster", clusterKey),
    zap.String("endpoint", endpointKey),
    zap.String("backend", backendURL),
)

logger.Error("Backend connection failed",
    zap.String("correlation_id", correlationID),
    zap.String("cluster", clusterKey),
    zap.String("endpoint", endpointKey),
    zap.Error(err),
)
```

## Examples

### Successful Operation
```go
logger.Info("Configuration applied successfully",
    zap.String("version", snapshot.Version),
    zap.Int("gateways", len(snapshot.Gateways)),
    zap.Int("routes", len(snapshot.Routes)),
    zap.Int("clusters", len(snapshot.Clusters)),
    zap.Int("vips", len(snapshot.VipAssignments)),
)
```

### Error with Context
```go
logger.Error("Failed to forward request",
    zap.String("correlation_id", correlationID),
    zap.String("method", r.Method),
    zap.String("path", r.URL.Path),
    zap.String("cluster", clusterName),
    zap.String("endpoint", endpointAddr),
    zap.Int("attempt", retryCount),
    zap.Error(err),
)
```

### Performance Metric
```go
logger.Debug("Request latency",
    zap.String("correlation_id", correlationID),
    zap.String("cluster", clusterName),
    zap.String("endpoint", endpointAddr),
    zap.Duration("total_duration", totalDuration),
    zap.Duration("upstream_duration", upstreamDuration),
    zap.Duration("filter_duration", filterDuration),
)
```

### State Change
```go
logger.Info("Circuit breaker state changed",
    zap.String("cluster", clusterName),
    zap.String("endpoint", endpointAddr),
    zap.String("old_state", oldState),
    zap.String("new_state", newState),
    zap.Uint32("failure_count", failureCount),
)
```

## Best Practices

1. **Always include correlation IDs** for request-scoped logs
2. **Log entry and exit** of significant operations at DEBUG level
3. **Log state changes** at INFO level
4. **Include error context** - don't just log the error, log what you were doing
5. **Use appropriate types** - zap.Duration for durations, zap.Error for errors
6. **Avoid sensitive data** - never log passwords, tokens, or API keys
7. **Be concise** - log messages should be brief and descriptive
8. **Use past tense** for completed actions, present tense for ongoing actions
9. **Include metrics** - duration, count, size when relevant
10. **Test log output** - verify logs are readable and contain useful information

## Performance Considerations

1. **Use DEBUG wisely** - excessive DEBUG logging can impact performance
2. **Defer expensive operations** - use lazy evaluation for expensive field values
3. **Sample high-frequency logs** - consider sampling for very frequent events
4. **Use log levels** - control verbosity in production vs development

## Migration Checklist

When updating existing code to follow these standards:

- [ ] Replace string concatenation with structured fields
- [ ] Use consistent field names (check table above)
- [ ] Add correlation IDs to request-scoped logs
- [ ] Use appropriate log levels
- [ ] Include relevant context (cluster, endpoint, etc.)
- [ ] Remove sensitive data from logs
- [ ] Use zap types (zap.Error, zap.Duration, etc.)
- [ ] Verify logs are useful for debugging and monitoring
