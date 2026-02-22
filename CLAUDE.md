# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Critical Rules

- **NEVER create version tags (`git tag`) or push tags after merging PRs unless the user explicitly asks for a new release/version.** Tagging triggers the release workflow which builds and publishes Docker images and GitHub releases. Only tag when specifically instructed.
- **ALWAYS use git worktrees for all code changes.** Never commit directly to `main`. The workflow is:
  1. Create an isolated git worktree for each task (use `EnterWorktree` or the `superpowers:using-git-worktrees` skill)
  2. Work on a feature branch inside the worktree
  3. Create a PR via `gh pr create` when done — never push directly to `main`
  4. Merge via PR after review

## Project Overview

NovaEdge is a distributed Kubernetes-native load balancer, reverse proxy, VIP controller, and SD-WAN gateway written in Go. It serves as a unified replacement for Envoy + MetalLB + NGINX Ingress + Cisco SD-WAN, providing:

- Distributed L4/L7 load balancing (HTTP/1.1, HTTP/2, HTTP/3 QUIC, WebSockets, gRPC, SSE, TCP/UDP)
- Reverse proxy with policies and middleware (auth, rate-limit, WAF, rewrites, compression, caching)
- Ingress Controller (compatible with Kubernetes Ingress and Gateway API)
- Distributed VIP management (L2 ARP, BGP, OSPF modes with BFD and IPv6)
- SD-WAN with WireGuard tunnels, multi-WAN link management, SLA-based path selection, STUN NAT traversal, and DSCP QoS
- Certificate management (ACME, cert-manager, HashiCorp Vault)
- Service mesh with transparent mTLS (TPROXY + SPIFFE)
- WASM plugin system for extensibility
- Multi-cluster federation with hub-spoke topology
- Kubernetes-native control plane using 12 CRDs

## Architecture

The system consists of four major components:

1. **Operator**: Manages NovaEdge lifecycle via `NovaEdgeCluster` CRD
2. **Kubernetes Controller (Control-Plane)**: Runs as a Deployment, watches CRDs/Ingress/Gateway API, builds routing configuration, and pushes ConfigSnapshots to node agents via gRPC
3. **Node Agent (Data Plane)**: Runs as a DaemonSet with hostNetwork, handles L4/L7 load balancing, VIP management (ARP/BGP/OSPF), and executes routing/filtering/policy logic
4. **CRDs** (12 types):
   - Core: `ProxyGateway`, `ProxyRoute`, `ProxyBackend`, `ProxyPolicy`, `ProxyVIP`
   - Certificate & IP: `ProxyCertificate`, `ProxyIPPool`
   - SD-WAN: `ProxyWANLink`, `ProxyWANPolicy`
   - Cluster management: `NovaEdgeCluster`, `NovaEdgeFederation`, `NovaEdgeRemoteCluster`

## Repository Structure

```
novaedge/
├── cmd/                              # Main applications (5 binaries)
│   ├── novaedge-controller/          # Controller entrypoint
│   ├── novaedge-agent/               # Node agent entrypoint
│   ├── novaedge-standalone/          # Standalone mode (no Kubernetes)
│   ├── novaedge-operator/            # Operator entrypoint
│   └── novactl/                      # CLI tool (JSON API backend)
├── web/                              # React frontend (standalone, served by nginx)
│   ├── src/                          # React source code
│   ├── nginx.conf                    # nginx configuration for SPA + API proxy
│   ├── docker-entrypoint.sh          # Container entrypoint (env var substitution)
│   ├── vite.config.ts                # Vite build config (outputs to dist/)
│   └── package.json                  # Node.js dependencies
├── internal/                         # Private application code
│   ├── controller/                   # Controller logic
│   │   ├── certmanager/              # cert-manager integration
│   │   ├── vault/                    # HashiCorp Vault integration
│   │   ├── federation/               # Multi-cluster federation
│   │   ├── ipam/                     # IP address management
│   │   ├── meshca/                   # Mesh certificate authority
│   │   └── snapshot/                 # Config snapshot builder
│   ├── agent/                        # Agent implementation
│   │   ├── vip/                      # VIP management (L2/BGP/OSPF/BFD)
│   │   ├── lb/                       # Load balancing algorithms (12 types)
│   │   ├── router/                   # Request routing, middleware, caching, compression
│   │   ├── policy/                   # Policy enforcement (rate-limit, auth, CORS, JWT, WAF, mTLS)
│   │   ├── server/                   # HTTP/HTTPS/HTTP3 servers, mTLS, OCSP, PROXY protocol
│   │   ├── l4/                       # L4 TCP/UDP proxying, TLS passthrough
│   │   ├── wasm/                     # WASM plugin runtime (Wazero)
│   │   ├── upstream/                 # Connection pooling
│   │   ├── health/                   # Health checking and circuit breaking
│   │   ├── config/                   # Config snapshot handling
│   │   ├── metrics/                  # Prometheus metrics
│   │   ├── grpc/                     # gRPC handler
│   │   ├── protocol/                 # Protocol detection
│   │   ├── mesh/                     # Service mesh (mTLS, TPROXY, tunnel, authz, certs)
│   │   ├── cpvip/                    # Control-plane VIP management
│   │   ├── filters/                  # Request/response filter chains
│   │   └── websocket/               # WebSocket upgrade handling
│   ├── acme/                         # ACME client (Let's Encrypt)
│   │   └── dns/                      # DNS-01 providers (Cloudflare, Route53, Google)
│   ├── standalone/                   # Standalone mode config
│   └── proto/                        # Protobuf definitions
├── api/                              # CRD API definitions
│   └── v1alpha1/                     # API version (10 CRD types)
├── charts/                           # Helm charts
│   ├── novaedge/                     # Main chart
│   ├── novaedge-agent/               # Agent chart
│   └── novaedge-operator/            # Operator chart
├── config/                           # Kubernetes manifests
│   ├── crd/                          # CRD definitions (10 CRDs)
│   ├── samples/                      # Example resources (51 samples)
│   ├── rbac/                         # RBAC manifests
│   ├── controller/                   # Controller deployment
│   ├── agent/                        # Agent DaemonSet
│   └── kustomize/                    # Kustomize overlays (dev, production)
├── docs/                             # Documentation site
├── test/                             # Integration tests
└── Makefile                          # Build automation
```

## Development Commands

### Build Commands
```bash
# Build all 5 binaries
make build-all

# Build individually
make build-controller
make build-agent
make build-standalone
make build-operator
make build-novactl

# Build web UI frontend (requires Node.js)
make build-webui

# Build Docker images (all 6: controller, agent, novactl, standalone, operator, webui)
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

# Run conformance tests (requires running cluster)
make test-conformance

# Run tests with coverage
make test-coverage

# Run specific package tests
go test -v ./internal/agent/lb/
```

### Linting and Code Quality
```bash
# Run linter (16 strict linters via golangci-lint)
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
- **LeastConn**: Route to endpoint with fewest active connections
- **P2C (Power of Two Choices)**: Pick best of two random endpoints
- **EWMA**: Latency-aware using exponentially weighted moving average
- **Ring Hash / Maglev**: Consistent hashing for session affinity
- **Sticky**: Cookie-based session affinity
- **Locality-aware**: Prefer endpoints in the same zone/region
- **Priority failover**: Route to highest-priority endpoint group
- **Panic mode**: Bypass health checks when too many endpoints are unhealthy
- **Slow-start**: Gradually ramp traffic to newly added endpoints
- **Resource-adaptive**: Weight endpoints by CPU/memory usage
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

### BFD
- Bidirectional Forwarding Detection for sub-second failure detection
- Works alongside L2/BGP/OSPF modes

### IPv6 Dual-Stack
- Full IPv6 support for VIP addresses
- IP address pools via ProxyIPPool CRD with IPAM allocation

## Service Mesh

The service mesh provides transparent mTLS between Kubernetes services:

- **`internal/agent/mesh/`**: TPROXY-based traffic interception, HTTP/2 mTLS tunnel, TLS provider, authorization engine, certificate requester
- **`internal/controller/meshca/`**: Embedded certificate authority issuing SPIFFE workload certificates (ECDSA P-256, 24h lifetime)
- Services opt-in via `novaedge.io/mesh: "enabled"` annotation
- SPIFFE identity format: `spiffe://cluster.local/agent/<node>`
- Agent cert requester generates CSR, calls `RequestMeshCertificate` RPC, auto-renews at 80% lifetime
- TPROXY iptables rules intercept ClusterIP traffic to port 15001, tunnel via mTLS on port 15002

## Control-Plane VIP

- **`internal/agent/cpvip/`**: Manages a dedicated VIP for controller high availability
- Health checks controller via `/livez` endpoint with ServiceAccount token auth
- Supports L2/BGP/BFD modes for CP VIP announcement
- Separate from data-plane VIP management in `internal/agent/vip/`

## Policy & Middleware Architecture

Policies and middleware are spread across two packages:

- **`internal/agent/policy/`**: Security and access control policies
  - Rate limiting (local token bucket + distributed Redis)
  - Authentication: basic auth, forward auth, OAuth2/OIDC, Keycloak
  - JWT validation, CORS, IP filtering, security headers
  - mTLS enforcement, WAF (Coraza)

- **`internal/agent/router/`**: Traffic processing middleware
  - Composable middleware pipelines with boolean routing expressions
  - Response caching, gzip/Brotli compression, buffering
  - Traffic mirroring, traffic splitting/canary
  - Custom error pages, access logging, request retry
  - SSE support, HTTP-to-HTTPS redirect, URL rewrite

## Configuration Snapshot Model

The controller pushes versioned `ConfigSnapshot` to agents containing:
- Gateways assigned to the node
- Routes with matching rules
- Backends with endpoints from EndpointSlices
- Filters and policies
- VIP assignments for this node
- TLS certificates
- L4 route configurations
- Authentication configurations
- Internal services (mesh-enabled) with endpoints
- Mesh authorization policies

Agents must atomically swap runtime config when receiving new snapshots.

## Observability

### Metrics (Prometheus)
- Controller: reconciliation_duration, watch_events, config_pushes
- Agent: request_count, request_duration, upstream_rtt, active_connections, vip_failovers
- Certificate: expiry_seconds, renewals_total, acme_challenges_total

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
- TLS 1.3 minimum with secure AEAD cipher suites
- TLS certificates loaded from Kubernetes Secrets, ACME, cert-manager, or Vault
- Frontend mTLS with client certificate verification
- OCSP stapling for certificate revocation checking
- Support mTLS between proxy and backends
- Authentication: basic auth, forward auth, OAuth2/OIDC, JWT with JWKS
- Rate limiting using token bucket (local) and Redis (distributed)
- WAF via Coraza engine
- Request size limits and security headers policy

## Documentation Freshness

**Every PR that changes behavior must include corresponding documentation updates.** This is enforced by CI and code review.

### What Requires Doc Updates
- New features or CRDs → user guide page or section, examples, CRD reference
- Changed API fields, flags, or config options → update relevant reference docs
- New LB algorithms, policies, or middleware → update user guide + examples
- Architecture changes → update architecture docs and diagrams
- New CLI commands or flags → update CLI reference
- Helm chart changes → update Helm values reference
- Changed deployment requirements → update installation guides

### Documentation Structure
- `docs/comparison.md` - Tool replacement guide (update when adding new replacement capabilities)
- `docs/use-cases/` - Use-case guides with architecture diagrams and complete YAML configs
- `docs/user-guide/` - Feature reference docs
- `docs/reference/` - CRD, CLI, and Helm reference
- `docs/examples/index.md` - Quick-reference config examples
- `mkdocs.yml` - Navigation (must include any new pages)

### CI Enforcement
- `mkdocs build --strict` runs on every PR that touches `docs/`, `mkdocs.yml`, `*.go`, `charts/`, or `config/`
- Broken internal links, missing nav entries, and malformed Mermaid diagrams will fail the build
- The Claude code review also checks for documentation completeness

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

## Dependencies

Key Go libraries used:
- `k8s.io/client-go`: Kubernetes client
- `sigs.k8s.io/controller-runtime`: Controller framework
- `sigs.k8s.io/gateway-api`: Gateway API types
- `github.com/osrg/gobgp/v3`: BGP implementation
- `google.golang.org/grpc`: Config distribution
- `go.uber.org/zap`: Structured logging
- `github.com/prometheus/client_golang`: Metrics
- `go.opentelemetry.io/otel`: Tracing
- `github.com/quic-go/quic-go`: HTTP/3 QUIC support
- `github.com/tetratelabs/wazero`: WASM plugin runtime
- `github.com/corazawaf/coraza/v3`: WAF engine
- `github.com/go-acme/lego/v4`: ACME protocol (Let's Encrypt)
- `github.com/redis/go-redis/v9`: Distributed rate limiting
- `github.com/coreos/go-oidc/v3`: OAuth2/OIDC authentication
- `github.com/golang-jwt/jwt/v5`: JWT parsing and validation
- `github.com/gorilla/websocket`: WebSocket support
- `github.com/andybalholm/brotli`: Brotli compression
- `github.com/spf13/cobra`: CLI framework
