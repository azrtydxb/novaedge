# NovaEdge

NovaEdge is a distributed Kubernetes-native load balancer, reverse proxy, and VIP controller written in Go. It serves as a unified replacement for Envoy + MetalLB + NGINX Ingress.

## Features

- **Distributed L7 load balancing** (HTTP/1.1, HTTP/2, HTTP/3, WebSockets, gRPC)
- **Reverse proxy with filters** (auth, rate-limit, rewrites, CORS)
- **Ingress Controller** compatible with Kubernetes Ingress and Gateway API
- **Distributed VIP management**:
  - L2 ARP mode (active-passive VIP ownership)
  - BGP mode (active-active multi-node ECMP)
  - OSPF mode (active-active L3 routing)
- **Node-level edge agents** binding to hostNetwork
- **Kubernetes-native control plane** using CRDs
- **Health checks, circuit breaking, outlier detection**
- **High availability** and multi-node awareness
- **Observability** (OpenTelemetry, Prometheus, structured logging)

## Architecture

NovaEdge consists of three major components:

1. **Kubernetes Controller (Control-Plane)**: Runs as a Deployment, watches CRDs and Kubernetes resources, builds routing configuration, and pushes ConfigSnapshots to node agents via gRPC
2. **Node Agent (Data Plane)**: Runs as a DaemonSet with hostNetwork, handles L7 load balancing, VIP management, and executes routing/filtering logic
3. **CRDs**: Custom Resource Definitions for `ProxyVIP`, `ProxyGateway`, `ProxyRoute`, `ProxyBackend`, `ProxyPolicy`

See [NovaEdge_FullSpec.md](NovaEdge_FullSpec.md) for detailed architecture and specifications.

## Current Status

✅ **Phases 1-10 Complete**: Production-Ready System

NovaEdge is now **production-ready** and capable of replacing Envoy + MetalLB + NGINX Ingress in Kubernetes clusters.

**Completed Features:**
- ✅ All 5 CRD types with full validation and status tracking
- ✅ Complete controller with reconcilers for all CRDs
- ✅ Config snapshot builder with versioning and gRPC distribution
- ✅ Full HTTP/1.1, HTTP/2 (h2/h2c), and HTTP/3 (QUIC) support
- ✅ WebSocket proxying with bidirectional streaming
- ✅ gRPC support with metadata forwarding
- ✅ All 3 VIP modes: L2 ARP, BGP, and OSPF
- ✅ 5 load balancing algorithms: RoundRobin, P2C, EWMA, RingHash, Maglev
- ✅ Advanced filters: header modification, URL rewrite, redirects
- ✅ Policy enforcement: rate limiting, CORS, IP filtering, JWT validation
- ✅ Health checking (active & passive) and circuit breaking
- ✅ TLS/SSL termination with SNI support
- ✅ Ingress API v1 support with automatic translation
- ✅ Gateway API v1 support (Gateway + HTTPRoute)
- ✅ OpenTelemetry distributed tracing
- ✅ Prometheus metrics and structured logging
- ✅ CLI tool (novactl) for resource management
- ✅ Complete deployment manifests and RBAC

## Getting Started

### Prerequisites

- Go 1.25+
- Kubernetes cluster (1.29+)
- kubectl configured
- make

### Building

```bash
# Build all components (controller, agent, novactl)
make build-all

# Or build individually
make build-controller
make build-agent
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

### Installing CRDs

```bash
# Install CRDs to your cluster
make install-crds

# Verify CRDs are installed
kubectl get crds | grep novaedge.io
```

### Deploying to Kubernetes

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
kubectl get pods -n novaedge-system
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-controller
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-agent
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

# Or use the novactl CLI tool
./novactl get gateways
./novactl get routes
./novactl get backends
./novactl get vips
./novactl get policies
./novactl describe gateway my-gateway
```

## Development Roadmap

- [x] **Phase 1**: Core CRDs + Controller skeleton
- [x] **Phase 2**: Config snapshot builder
- [x] **Phase 3**: Basic HTTP L7 proxy + routing
- [x] **Phase 4**: L2 VIP mode
- [x] **Phase 5**: BGP VIP mode
- [x] **Phase 6**: Filters + LB algorithms
- [x] **Phase 7**: Health checking + circuit breaking
- [x] **Phase 8**: Ingress + Gateway API support
- [x] **Phase 9**: Observability + CLI
- [x] **Phase 10**: Policy enforcement and traffic management
- [x] **Phase 11**: HTTP/3 QUIC

## Contributing

See [CLAUDE.md](CLAUDE.md) for development guidelines and best practices when working with Claude Code.

## License

Copyright 2024 NovaEdge Authors. Licensed under the Apache License, Version 2.0.
