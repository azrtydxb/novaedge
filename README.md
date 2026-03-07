<p align="center">
  <img src="novaedge-logo-light.svg" alt="NovaEdge" width="480">
</p>

<p align="center">
  <strong>Kubernetes-Native Network Platform</strong>
</p>

<p align="center">
  <a href="https://github.com/azrtydxb/novaedge/releases"><img src="https://img.shields.io/github/v/release/azrtydxb/novaedge?style=flat-square" alt="Release"></a>
  <a href="https://github.com/azrtydxb/novaedge/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/azrtydxb/novaedge/ci.yml?branch=main&style=flat-square&label=CI" alt="CI"></a>
<a href="https://azrtydxb.github.io/novaedge"><img src="https://img.shields.io/badge/docs-mkdocs-blue?style=flat-square" alt="Documentation"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/azrtydxb/novaedge?style=flat-square" alt="License"></a>
</p>

---

NovaEdge is a unified replacement for Envoy + MetalLB + NGINX Ingress + Cisco SD-WAN, written in Go and Rust. It combines L4/L7 load balancing, VIP management, service mesh, and SD-WAN into a Kubernetes-native platform with a Go control plane and Rust data plane.

## Features

### L7 Load Balancing & Reverse Proxy
- **Protocol support**: HTTP/1.1, HTTP/2 (h2/h2c), HTTP/3 (QUIC), WebSockets, gRPC, Server-Sent Events
- **8 CRD-selectable load balancing policies** (RoundRobin, LeastConn, P2C, EWMA, RingHash, Maglev, SourceHash, Sticky) plus composable wrappers in the Rust dataplane (Locality-aware, Priority failover, Panic mode, Slow-start, Resource-adaptive)
- **Advanced routing**: hostname, path, header, method matching with boolean routing expressions
- **Traffic management**: canary/weighted splitting, traffic mirroring, request retry with configurable policies
- **Response processing**: HTTP caching, gzip/Brotli compression, buffering, custom error pages
- **Composable middleware pipelines** with per-route configuration
- **HTTP-to-HTTPS redirect**, URL rewrite, header modification (request and response)

### L4 Proxying & eBPF/XDP Acceleration
- **TCP and UDP proxying** with connection tracking
- **TLS passthrough** (SNI-based routing without termination)
- **eBPF/XDP acceleration** (enabled by default, auto-detected):
  - **XDP L4 load balancing** — packet rewriting at the NIC driver level, before `sk_buff` allocation
  - **AF_XDP zero-copy** — shared-memory ring buffers for kernel-bypass packet I/O
  - **eBPF mesh redirect** — `SK_LOOKUP` socket redirection replaces nftables/iptables TPROXY rules
  - Automatic fallback to legacy userspace proxy if kernel does not support eBPF

### VIP Management
- **L2 ARP mode**: active-passive VIP ownership with GARP
- **BGP mode**: active-active multi-node ECMP via GoBGP
- **OSPF mode**: active-active L3 routing
- **BFD** (Bidirectional Forwarding Detection) for sub-second failure detection
- **IPv6 dual-stack** support
- **IP address pools** (`ProxyIPPool` CRD) with IPAM allocation

### Security & Authentication
- **TLS termination** with SNI multi-certificate support and TLS 1.3 minimum
- **Frontend mTLS** (mutual TLS client certificate verification)
- **OCSP stapling** for certificate revocation checking
- **PROXY protocol** v1/v2 support
- **Authentication stack**: basic auth, forward auth, OAuth2/OIDC, Keycloak integration
- **JWT validation** with JWKS support
- **Rate limiting**: local (token bucket) and distributed (Redis-backed)
- **CORS**, **IP filtering**, **security headers** (HSTS, CSP, X-Frame-Options)
- **WAF** (Web Application Firewall) powered by Coraza
- **Request size limits** and per-route access logging

### Certificate Management
- **ACME** (Let's Encrypt) with HTTP-01, DNS-01, and TLS-ALPN-01 challenges
- **DNS providers**: Cloudflare, Route53, Google Cloud DNS
- **cert-manager integration** for Kubernetes-native certificate lifecycle
- **HashiCorp Vault** integration (PKI secrets engine, KV store)
- **Self-signed certificate** generation
- **Certificate hot-reload** without downtime

### Extensibility
- **WASM plugin system** (via Wazero runtime) for custom request/response processing
- **Composable middleware pipelines** with boolean expressions for conditional routing
- **12 Custom Resource Definitions** for declarative configuration

### Kubernetes Integration
- **Ingress API v1** support with automatic translation
- **Gateway API v1** support (Gateway, HTTPRoute, GRPCRoute, TCPRoute, TLSRoute)
- **Operator** (`NovaEdgeCluster` CRD) for lifecycle management
- **Multi-cluster federation** with hub-spoke topology and split-brain detection
- **Helm charts** for controller, agent, and operator deployment

### Observability
- **OpenTelemetry** distributed tracing
- **Prometheus** metrics with cardinality limiting
- **Structured logging** (zap) with correlation IDs
- **Web UI dashboard** (via novactl)
- **Per-route access logging**
- **CLI tool** (novactl) with trace query, metrics query, and agent inspection

### Service Mesh
- **Transparent mTLS** between services via TPROXY interception (eBPF `SK_LOOKUP` auto-detected, nftables/iptables fallback)
- **Embedded mesh CA** with SPIFFE identity (ECDSA P-256 workload certificates)
- **HTTP/2 mTLS tunnel** for encrypted service-to-service communication
- **Mesh authorization engine** with service-level access policies
- **Automatic certificate rotation** with configurable renewal threshold

### SD-WAN
- **Multi-link WAN management** with primary, backup, and load-balanced roles
- **Application-aware path selection** with SLA-based routing strategies (lowest-latency, highest-bandwidth, most-reliable, lowest-cost)
- **WireGuard tunnels** with wgctrl kernel API and STUN NAT traversal
- **Real-time link quality probing** -- latency, jitter, packet loss with EWMA smoothing
- **Automatic failover** with hysteresis to prevent link flip-flop
- **DSCP marking** for QoS enforcement
- **2 new CRDs**: `ProxyWANLink`, `ProxyWANPolicy`

### Control-Plane VIP
- **Dedicated VIP for controller** high availability
- **Health-check based failover** using Kubernetes `/livez` endpoint
- **BGP/BFD mode support** for CP VIP announcement

### Health & Resilience
- **Active and passive health checking**
- **Circuit breaking** with configurable thresholds
- **Graceful shutdown** with in-flight request tracking
- **Connection pooling** with per-cluster limits

## Architecture

NovaEdge consists of five major components:

1. **Operator**: Manages NovaEdge lifecycle via `NovaEdgeCluster` CRD
2. **Controller (Control-Plane)**: Runs as a Deployment, watches CRDs and Kubernetes resources, builds routing configuration, and pushes ConfigSnapshots to node agents via gRPC
3. **Node Agent (Config Agent)**: Runs as a DaemonSet with hostNetwork, receives configuration from the controller, manages VIP binding (ARP/BGP/OSPF/BFD), iptables/nftables rules, eBPF program lifecycle, and pushes config to the Rust dataplane via gRPC. Does NOT handle user traffic.
4. **Rust Dataplane (Traffic Handler)**: Runs as a DaemonSet sidecar alongside the Go agent, binds ports 80/443, handles ALL L4/L7 traffic (HTTP/HTTPS/HTTP3/TCP/UDP/WebSocket/gRPC), executes routing, middleware, policies, load balancing, and connection pooling. Optionally accelerated by eBPF XDP.
5. **CRDs**: 12 Custom Resource Definitions:
   - Core: `ProxyGateway`, `ProxyRoute`, `ProxyBackend`, `ProxyPolicy`, `ProxyVIP`
   - Certificate & IP: `ProxyCertificate`, `ProxyIPPool`
   - SD-WAN: `ProxyWANLink`, `ProxyWANPolicy`
   - Cluster management: `NovaEdgeCluster`, `NovaEdgeFederation`, `NovaEdgeRemoteCluster`

See the [documentation site](docs/index.md) for detailed architecture and specifications.

## Getting Started

### Prerequisites

- Go 1.25+
- Kubernetes cluster (1.29+)
- kubectl configured
- make

### Building

```bash
# Build all 5 binaries (controller, agent, standalone, operator, novactl)
make build-all

# Or build individually
make build-controller
make build-agent
make build-standalone
make build-operator
make build-novactl

# Build Docker images
make docker-build

# Run tests
make test

# Run tests with coverage
make test-coverage

# Run linter
make lint
```

### Deploying with Helm

```bash
# Install the operator
helm install novaedge-operator ./charts/novaedge-operator \
  --namespace nova-system --create-namespace

# Deploy NovaEdge via operator
kubectl apply -f - <<EOF
apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeCluster
metadata:
  name: novaedge
  namespace: nova-system
spec:
  version: "v0.1.0"
  agent:
    vip:
      enabled: true
      mode: L2
EOF

# Verify
kubectl get pods -n nova-system
```

### Deploying Manually

```bash
# 1. Install CRDs and create namespace
make install-crds
kubectl apply -f config/controller/namespace.yaml

# 2. Deploy controller
kubectl apply -f config/rbac/
kubectl apply -f config/controller/deployment.yaml

# 3. Deploy agents (DaemonSet)
kubectl apply -f config/agent/serviceaccount.yaml
kubectl apply -f config/agent/clusterrole.yaml
kubectl apply -f config/agent/clusterrolebinding.yaml
kubectl apply -f config/agent/daemonset.yaml

# 4. Verify deployment
kubectl get pods -n nova-system
kubectl logs -n nova-system -l app.kubernetes.io/name=novaedge-controller
kubectl logs -n nova-system -l app.kubernetes.io/name=novaedge-agent
```

### Example Usage

```bash
# Apply sample resources (NovaEdge CRDs)
kubectl apply -f config/samples/proxyvip_sample.yaml
kubectl apply -f config/samples/proxygateway_sample.yaml
kubectl apply -f config/samples/proxybackend_sample.yaml
kubectl apply -f config/samples/proxyroute_sample.yaml
kubectl apply -f config/samples/proxypolicy_ratelimit_sample.yaml
kubectl apply -f config/samples/proxypolicy_cors_sample.yaml
kubectl apply -f config/samples/proxypolicy_jwt_sample.yaml
kubectl apply -f config/samples/proxycertificate_acme.yaml

# Or use standard Kubernetes Ingress
kubectl apply -f config/samples/ingress_sample.yaml

# Or use Gateway API
kubectl apply -f config/samples/gatewayclass.yaml
kubectl apply -f config/samples/gateway_example.yaml
kubectl apply -f config/samples/httproute_example.yaml

# Check status with kubectl
kubectl get proxyvips
kubectl get proxygateways
kubectl get proxyroutes
kubectl get proxybackends
kubectl get proxypolicies
kubectl get proxycertificates
kubectl get proxyippools

# Or use the novactl CLI tool
./novactl get gateways
./novactl get routes
./novactl get backends
./novactl get vips
./novactl get policies
./novactl describe gateway my-gateway

# Advanced novactl features
./novactl agent config worker-1              # Query agent configuration
./novactl trace list --limit 20              # List recent traces
./novactl metrics query 'rate(requests[5m])' # Execute PromQL
./novactl metrics dashboard                  # Show metrics dashboard

# SD-WAN management
./novactl sdwan status                       # Show SD-WAN status summary
./novactl sdwan links                        # List WAN links with quality data
./novactl sdwan links -A                     # List WAN links in all namespaces
./novactl sdwan topology                     # Show overlay network topology
```

## Testing & Quality

### Test Suite
- **1345+ test functions** across unit, integration, and controller tests
- **85%+ unit coverage** for critical components (router, health, VIP, load balancing, mesh, policies)

### Running Tests
```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run integration tests
go test -v ./test/integration/...

# Run specific component tests
go test -v ./internal/agent/router/...
go test -v ./internal/agent/health/...
go test -v ./internal/controller/...

# Run benchmarks
go test -bench=. -benchmem ./internal/agent/...
```

### Code Quality
- **Zero linting errors** with 16 strict linters enabled (golangci-lint)
- **CI pipeline**: gofmt, golangci-lint, go vet, govulncheck, unit tests, build all 5 binaries
- **Standardized error handling** with custom error types
- **Interface abstractions** for improved testability

## Documentation

Comprehensive documentation is available in the [docs/](docs/index.md) directory covering:

- **Architecture**: system design, component deep-dives, federation model
- **User Guides**: routing, load balancing, VIP management, TLS, authentication, WASM plugins, L4 proxying, WAF, and more
- **Operations**: observability, Web UI dashboard, troubleshooting, access logging
- **Reference**: CRD specifications, CLI reference, Helm values
- **Development**: contributing guide, development setup

## Contributing

See [docs/development/contributing.md](docs/development/contributing.md) for contribution guidelines.

See [CLAUDE.md](CLAUDE.md) for development guidelines when working with Claude Code.

## License

Copyright 2024 NovaEdge Authors. Licensed under the Apache License, Version 2.0.
