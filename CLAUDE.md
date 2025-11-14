# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

NovaEdge is a distributed Kubernetes-native load balancer, reverse proxy, and VIP controller written in Go. It serves as a unified replacement for Envoy + MetalLB + NGINX Ingress, providing:

- Distributed L7 load balancing (HTTP/1.1, HTTP/2, WebSockets, gRPC, future HTTP/3)
- Reverse proxy with filters (auth, rate-limit, rewrites)
- Ingress Controller (compatible with Kubernetes Ingress and Gateway API)
- Distributed VIP management (L2 ARP, BGP, OSPF modes)
- Kubernetes-native control plane using CRDs

## Architecture

The system consists of three major components:

1. **Kubernetes Controller (Control-Plane)**: Runs as a Deployment, watches CRDs/Ingress/Gateway API, builds routing configuration, and pushes ConfigSnapshots to node agents via gRPC
2. **Node Agent (Data Plane)**: Runs as a DaemonSet with hostNetwork, handles L7 load balancing, VIP management (ARP/BGP/OSPF), and executes routing/filtering logic
3. **CRDs**: `ProxyGateway`, `ProxyRoute`, `ProxyBackend`, `ProxyPolicy`, `ProxyVIP`

## Repository Structure

```
novaedge/
├── cmd/                          # Main applications
│   ├── novaedge-controller/      # Controller entrypoint
│   └── novaedge-agent/          # Node agent entrypoint
├── internal/                     # Private application code
│   ├── controller/               # Controller logic (watchers, reconcilers)
│   ├── agent/                    # Agent implementation
│   │   ├── vip/                 # VIP management (L2/BGP/OSPF)
│   │   ├── lb/                  # Load balancing algorithms
│   │   ├── router/              # Request routing
│   │   ├── filters/             # Filter chain (auth, rate-limit, etc.)
│   │   ├── upstream/            # Connection pooling
│   │   ├── health/              # Health checking
│   │   └── config/              # Config snapshot handling
│   └── proto/                   # Protobuf definitions
├── api/                         # CRD API definitions
│   └── v1alpha1/                # API version
├── config/                      # Kubernetes manifests
│   ├── crd/                     # CRD definitions
│   ├── samples/                 # Example resources
│   └── rbac/                    # RBAC manifests
└── Makefile                     # Build automation
```

## Development Commands

### Build Commands
```bash
# Build controller
make build-controller

# Build agent
make build-agent

# Build both
make build-all

# Build Docker images
make docker-build

# Generate CRDs
make generate-crds

# Generate protobuf
make generate-proto
```

### Testing Commands
```bash
# Run all tests
make test

# Run unit tests only
go test ./internal/...

# Run integration tests
make test-integration

# Run tests with coverage
make test-coverage

# Run specific package tests
go test -v ./internal/agent/lb/
```

### Linting and Code Quality
```bash
# Run linter
make lint

# Format code
make fmt

# Run go vet
make vet

# Run all checks (fmt, vet, lint)
make check
```

### Deployment Commands
```bash
# Install CRDs
make install-crds

# Deploy to Kubernetes
make deploy

# Undeploy from Kubernetes
make undeploy

# Deploy samples
kubectl apply -f config/samples/
```

## Go Development Standards

### Code Organization
- Use standard Go project layout
- Keep `internal/` packages private to the project
- Place shared types in `api/` packages
- Use `cmd/` for application entrypoints

### Kubernetes Client-Go Patterns
- Use informers with shared informer factories for watching resources
- Implement reconciliation loops with exponential backoff
- Use workqueues for decoupling watch events from processing
- Implement leader election for controller high availability
- Cache resources using listers, not direct API calls

### Error Handling
- Wrap errors with context using `fmt.Errorf("context: %w", err)`
- Return errors up the stack, handle at appropriate levels
- Use structured logging (zap) for error context
- Implement retry logic with exponential backoff for transient failures

### Concurrency
- Use channels for goroutine communication
- Implement context cancellation for graceful shutdown
- Protect shared state with mutexes
- Use errgroup for managing related goroutines

### Networking Code
- All VIP operations (L2 ARP/BGP/OSPF) must handle node failures gracefully
- Connection pools must implement circuit breaking and outlier detection
- Health checks must use both active probing and passive failure detection
- TLS termination must support SNI and certificate rotation

## CRD Development

### Code Generation
After modifying CRD types in `api/v1alpha1/`:
```bash
# Generate deepcopy, clientset, informers, listers
make generate

# Generate and update CRD manifests
make manifests
```

### CRD Design Principles
- Use `metav1.Condition` for status tracking
- Implement validation using kubebuilder markers
- Use references (`ObjectReference`) for linking resources
- Support both declarative and imperative workflows
- Include observability fields in status (observedGeneration, etc.)

## Load Balancing Algorithms

When implementing LB algorithms in `internal/agent/lb/`:
- **Round Robin**: Simple rotation through endpoints
- **P2C (Power of Two Choices)**: Pick best of two random endpoints
- **EWMA**: Latency-aware using exponentially weighted moving average
- **Ring Hash / Maglev**: Consistent hashing for session affinity
- All algorithms must handle endpoint addition/removal without disruption

## VIP Management

### L2 ARP Mode
- Single active node owns VIP at a time
- Node agent binds VIP to interface and sends GARPs
- Controller handles failover by reassigning VIP

### BGP Mode
- All healthy nodes announce VIP via BGP
- Uses GoBGP library for BGP peering
- Router performs ECMP across nodes

### OSPF Mode
- Similar to BGP using OSPF LSA advertisements
- Active-active with L3 routing

## Configuration Snapshot Model

The controller pushes versioned `ConfigSnapshot` to agents containing:
- Gateways assigned to the node
- Routes with matching rules
- Backends with endpoints from EndpointSlices
- Filters and policies
- VIP assignments for this node
- TLS certificates

Agents must atomically swap runtime config when receiving new snapshots.

## Observability

### Metrics (Prometheus)
- Controller: reconciliation_duration, watch_events, config_pushes
- Agent: request_count, request_duration, upstream_rtt, active_connections, vip_failovers

### Logging
- Use structured logging with zap
- Include correlation IDs for request tracing
- Log level: INFO for normal operation, DEBUG for troubleshooting

### Tracing
- Export OpenTelemetry traces for request flows
- Trace from ingress through routing to upstream response

## Testing Strategy

### Unit Tests
- Test each package in isolation
- Mock Kubernetes client interfaces
- Test LB algorithm distribution and fairness
- Test routing logic with various match conditions

### Integration Tests
- Use envtest for controller testing with fake API server
- Test full reconciliation loops
- Verify CRD validation and defaulting
- Test agent config updates

### E2E Tests
- Deploy to kind/k3s cluster
- Test actual traffic flow through agents
- Verify VIP failover scenarios
- Test integration with real Ingress/Gateway API resources

## Security Considerations

- Node agents run with `hostNetwork: true` and `privileged: true` for network operations
- TLS certificates loaded from Kubernetes Secrets
- Support mTLS between proxy and backends
- Implement JWT verification filter for authentication
- Rate limiting using token bucket algorithm

## Common Pitfalls

### Controller Development
- Always use informers, not direct GET calls in hot paths
- Implement proper error handling in reconciliation loops
- Use rate-limited workqueues to prevent API server overload
- Handle resource deletion with finalizers when needed

### Agent Development
- Atomic config swaps are critical to avoid request failures
- Connection pools must be drained gracefully on config changes
- VIP binding/unbinding must be idempotent
- Health check failures must not cause cascading failures

### Kubernetes Integration
- EndpointSlices can be large - use pagination and filtering
- Node labels can change - watch for updates
- Services can have multiple ports - map correctly to backends
- Gateway API and Ingress have different semantics - translate carefully

## Implementation Phases

Development follows this roadmap (see NovaEdge_FullSpec.md for details):

1. Core CRDs + Controller skeleton
2. Config snapshot builder
3. Basic HTTP L7 proxy + routing
4. L2 VIP mode
5. BGP VIP mode
6. Filters + LB algorithms
7. Health checking + circuit breaking
8. Ingress + Gateway API support
9. Observability + CLI
10. HTTP/2 + WebSockets + gRPC
11. HTTP/3 QUIC

## Dependencies

Key Go libraries used:
- `k8s.io/client-go`: Kubernetes client
- `sigs.k8s.io/controller-runtime`: Controller framework
- `github.com/osrg/gobgp`: BGP implementation
- `google.golang.org/grpc`: Config distribution
- `go.uber.org/zap`: Structured logging
- `github.com/prometheus/client_golang`: Metrics
- `go.opentelemetry.io/otel`: Tracing
