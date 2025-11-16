# Context Propagation Guide

This document outlines the proper use of Go contexts in NovaEdge and identifies files that need context.Background() fixes.

## Table of Contents

- [Context Best Practices](#context-best-practices)
- [When to Use context.Background()](#when-to-use-contextbackground)
- [Files Requiring Fixes](#files-requiring-fixes)
- [Migration Examples](#migration-examples)

## Context Best Practices

### General Rules

1. **Always propagate context** - Pass context as the first parameter to functions that need it
2. **Never use context.Background() in library code** - Only use in main() or top-level operations
3. **Derive child contexts** - Use context.WithCancel, context.WithTimeout, context.WithDeadline
4. **Use request context** - HTTP handlers should use r.Context() for all downstream operations
5. **Store correlation IDs** - Use context values for request tracking

### Function Signatures

```go
// ✅ Good - Context as first parameter
func ProcessRequest(ctx context.Context, req *Request) error

// ❌ Bad - No context
func ProcessRequest(req *Request) error
```

### HTTP Handlers

```go
// ✅ Good - Use request context
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Add correlation ID
    correlationID := uuid.New().String()
    ctx = context.WithValue(ctx, "correlation_id", correlationID)
    r = r.WithContext(ctx)

    // Pass context to downstream operations
    if err := s.router.Route(ctx, r, w); err != nil {
        // Handle error
    }
}

// ❌ Bad - Using context.Background()
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    ctx := context.Background() // Wrong! Use r.Context()
    if err := s.router.Route(ctx, r, w); err != nil {
        // Handle error
    }
}
```

### Long-Running Operations

```go
// ✅ Good - Derive child context from parent
func StartWorker(parentCtx context.Context) {
    ctx, cancel := context.WithCancel(parentCtx)
    defer cancel()

    for {
        select {
        case <-ctx.Done():
            return
        default:
            // Do work
        }
    }
}

// ❌ Bad - Using context.Background()
func StartWorker() {
    ctx := context.Background() // Wrong! Should accept parent context
    // ...
}
```

## When to Use context.Background()

### ✅ Acceptable Uses

1. **main() function** - Top-level context initialization
```go
func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()

    if err := run(ctx); err != nil {
        log.Fatal(err)
    }
}
```

2. **Test setup** - Creating root context for tests
```go
func TestHandler(t *testing.T) {
    ctx := context.Background()
    // Test code
}
```

3. **Independent background tasks** - Tasks with no parent lifecycle
```go
// Only if truly independent, not tied to any request or parent operation
func init() {
    ctx := context.Background()
    go periodicCleanup(ctx)
}
```

### ❌ Unacceptable Uses

1. **HTTP handlers** - Should use r.Context()
2. **Library functions** - Should accept context as parameter
3. **Goroutines spawned from requests** - Should derive from request context
4. **Operations with timeouts** - Should derive from parent context
5. **Health checkers** - Should use parent context for lifecycle management

## Files Requiring Fixes

The following files contain context.Background() calls that should be fixed:

### High Priority - Request Handling

| File | Line(s) | Current Usage | Fix Required |
|------|---------|---------------|--------------|
| `internal/agent/server/http.go` | 125, 139, 297 | context.Background() for shutdown timeouts | Use parent context passed to shutdown function |
| `internal/agent/router/websocket.go` | 165 | context.Background() for WebSocket proxy | Use r.Context() from HTTP request |
| `internal/agent/server/http3.go` | - | context.Background() in Start() | Accept parent context as parameter |

### High Priority - Lifecycle Management

| File | Line(s) | Current Usage | Fix Required |
|------|---------|--------------|--------------|
| `internal/agent/upstream/pool.go` | 123 | context.Background() for health checker | Accept parent context in NewPool() |
| `internal/agent/config/watcher.go` | 260 | context.Background() for status reporting | Use parent context from watcher |
| `internal/agent/vip/l2.go` | - | context.Background() in various places | Accept parent context in functions |
| `internal/agent/vip/bgp.go` | - | context.Background() in BGP operations | Accept parent context in functions |
| `internal/agent/vip/ospf.go` | - | context.Background() in OSPF operations | Accept parent context in functions |

### Medium Priority - Testing

| File | Line(s) | Current Usage | Fix Required |
|------|---------|--------------|--------------|
| `internal/agent/health/checker_test.go` | Multiple | context.Background() in tests | Acceptable for tests, but should derive for sub-operations |
| `internal/agent/server/http3_test.go` | Multiple | context.Background() in tests | Acceptable for tests |
| `internal/agent/vip/l2_test.go` | Multiple | context.Background() in tests | Acceptable for tests |
| `internal/controller/snapshot/builder_test.go` | Multiple | context.Background() in tests | Acceptable for tests |

### Medium Priority - CLI Tools

| File | Line(s) | Current Usage | Fix Required |
|------|---------|--------------|--------------|
| `cmd/novactl/cmd/agents.go` | - | context.Background() for API calls | Derive from command context |
| `cmd/novactl/cmd/logs.go` | - | context.Background() for API calls | Derive from command context |
| `cmd/novactl/cmd/metrics.go` | - | context.Background() for API calls | Derive from command context |
| `cmd/novactl/cmd/debug.go` | - | context.Background() for API calls | Derive from command context |
| `cmd/novactl/cmd/apply.go` | - | context.Background() for API calls | Derive from command context |
| `cmd/novactl/cmd/delete.go` | - | context.Background() for API calls | Derive from command context |
| `cmd/novactl/cmd/describe.go` | - | context.Background() for API calls | Derive from command context |
| `cmd/novactl/cmd/get.go` | - | context.Background() for API calls | Derive from command context |

### Low Priority - Main Functions

| File | Line(s) | Current Usage | Fix Required |
|------|---------|--------------|--------------|
| `cmd/novaedge-agent/main.go` | - | context.Background() in main() | Acceptable - this is top-level |
| `cmd/novaedge-controller/main.go` | - | context.Background() in main() | Acceptable - this is top-level |

## Migration Examples

### Example 1: HTTP Server Shutdown

#### Before
```go
func (s *HTTPServer) ApplyConfig(snapshot *config.Snapshot) error {
    // ...
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    server.Shutdown(ctx)
    cancel()
    // ...
}
```

#### After
```go
func (s *HTTPServer) ApplyConfig(ctx context.Context, snapshot *config.Snapshot) error {
    // ...
    shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    server.Shutdown(shutdownCtx)
    cancel()
    // ...
}
```

### Example 2: Upstream Pool

#### Before
```go
func NewPool(cluster *pb.Cluster, endpoints []*pb.Endpoint, logger *zap.Logger) *Pool {
    // ...
    ctx, cancel := context.WithCancel(context.Background())

    pool := &Pool{
        // ...
        ctx:    ctx,
        cancel: cancel,
    }

    pool.healthChecker.Start(ctx)
    return pool
}
```

#### After
```go
func NewPool(ctx context.Context, cluster *pb.Cluster, endpoints []*pb.Endpoint, logger *zap.Logger) *Pool {
    // ...
    poolCtx, cancel := context.WithCancel(ctx)

    pool := &Pool{
        // ...
        ctx:    poolCtx,
        cancel: cancel,
    }

    pool.healthChecker.Start(poolCtx)
    return pool
}
```

### Example 3: WebSocket Proxy

#### Before
```go
func (p *WebSocketProxy) ProxyWebSocket(w http.ResponseWriter, r *http.Request, backendURL string) error {
    // ...
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    // ...
}
```

#### After
```go
func (p *WebSocketProxy) ProxyWebSocket(w http.ResponseWriter, r *http.Request, backendURL string) error {
    // ...
    ctx, cancel := context.WithCancel(r.Context())
    defer cancel()
    // ...
}
```

### Example 4: Config Watcher Status Reporting

#### Before
```go
func (w *Watcher) reportStatus(client pb.ConfigServiceClient) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    status := &pb.AgentStatus{...}
    _, err := client.ReportStatus(ctx, status)
    // ...
}
```

#### After
```go
func (w *Watcher) reportStatus(client pb.ConfigServiceClient) {
    ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
    defer cancel()

    status := &pb.AgentStatus{...}
    _, err := client.ReportStatus(ctx, status)
    // ...
}
```

### Example 5: CLI Commands

#### Before
```go
func runListAgents(cmd *cobra.Command, args []string) error {
    ctx := context.Background()

    client, err := newClient()
    // ...
    agents, err := client.ListAgents(ctx)
    // ...
}
```

#### After
```go
func runListAgents(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context() // Use command context

    client, err := newClient()
    // ...
    agents, err := client.ListAgents(ctx)
    // ...
}
```

## Migration Checklist

For each file containing context.Background():

- [ ] Identify if the usage is acceptable (main(), tests, etc.)
- [ ] If not acceptable, determine the parent context source
- [ ] Update function signature to accept context if needed
- [ ] Replace context.Background() with parent context
- [ ] Derive child context using WithCancel/WithTimeout if needed
- [ ] Update all call sites to pass context
- [ ] Verify cancellation propagates correctly
- [ ] Test timeout behavior
- [ ] Update tests to verify context propagation

## Testing Context Propagation

### Test Context Cancellation

```go
func TestContextCancellation(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())

    // Start operation
    errCh := make(chan error, 1)
    go func() {
        errCh <- operation(ctx)
    }()

    // Cancel context
    cancel()

    // Verify operation stops
    err := <-errCh
    if err != context.Canceled {
        t.Errorf("expected context.Canceled, got %v", err)
    }
}
```

### Test Context Timeout

```go
func TestContextTimeout(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    err := longRunningOperation(ctx)

    if err != context.DeadlineExceeded {
        t.Errorf("expected context.DeadlineExceeded, got %v", err)
    }
}
```

## Benefits of Proper Context Propagation

1. **Graceful Shutdown** - All operations can be cancelled cleanly
2. **Request Timeout** - Automatic timeout propagation
3. **Resource Cleanup** - Contexts drive cleanup of goroutines and resources
4. **Cancellation Cascades** - Parent cancellation stops all children
5. **Deadline Propagation** - Timeouts apply to entire operation tree
6. **Tracing** - Distributed tracing context flows through system
7. **Better Testing** - Easier to test timeout and cancellation behavior

## Common Pitfalls

1. **Not checking ctx.Done()** - Always check for cancellation in loops
2. **Ignoring cancellation errors** - Handle context.Canceled appropriately
3. **Creating unrelated contexts** - Always derive from parent
4. **Not passing context** - Every operation should accept context
5. **Storing contexts** - Don't store contexts in structs (except for background workers)
6. **Using context for optional parameters** - Use context only for cancellation/deadlines
