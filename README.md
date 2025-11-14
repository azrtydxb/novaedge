# NovaEdge

NovaEdge is a distributed Kubernetes-native load balancer, reverse proxy, and VIP controller written in Go. It serves as a unified replacement for Envoy + MetalLB + NGINX Ingress.

## Features

- **Distributed L7 load balancing** (HTTP/1.1, HTTP/2, WebSockets, gRPC, future HTTP/3)
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

✅ **Phase 1 Complete**: Core CRDs + Controller Skeleton

- All 5 CRD types defined with validation
- Controller manager with reconcilers for each CRD
- CRD manifests generated and ready for installation
- RBAC manifests for controller deployment
- Sample manifests for all resource types
- Dockerfile for controller image

## Getting Started

### Prerequisites

- Go 1.25+
- Kubernetes cluster (1.29+)
- kubectl configured
- make

### Building

```bash
# Build controller
make build-controller

# Build Docker image
make docker-build-controller

# Run tests
make test

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

### Deploying the Controller

```bash
# Deploy controller to cluster
kubectl apply -f config/rbac/
kubectl apply -f config/controller/
```

### Example Usage

```bash
# Apply sample resources
kubectl apply -f config/samples/proxyvip_sample.yaml
kubectl apply -f config/samples/proxygateway_sample.yaml
kubectl apply -f config/samples/proxybackend_sample.yaml
kubectl apply -f config/samples/proxyroute_sample.yaml
kubectl apply -f config/samples/proxypolicy_ratelimit_sample.yaml

# Check status
kubectl get proxyvips
kubectl get proxygateways
kubectl get proxyroutes
kubectl get proxybackends
kubectl get proxypolicies
```

## Development Roadmap

- [x] **Phase 1**: Core CRDs + Controller skeleton
- [ ] **Phase 2**: Config snapshot builder
- [ ] **Phase 3**: Basic HTTP L7 proxy + routing
- [ ] **Phase 4**: L2 VIP mode
- [ ] **Phase 5**: BGP VIP mode
- [ ] **Phase 6**: Filters + LB algorithms
- [ ] **Phase 7**: Health checking + circuit breaking
- [ ] **Phase 8**: Ingress + Gateway API support
- [ ] **Phase 9**: Observability + CLI
- [ ] **Phase 10**: HTTP/2 + WebSockets + gRPC
- [ ] **Phase 11**: HTTP/3 QUIC

## Contributing

See [CLAUDE.md](CLAUDE.md) for development guidelines and best practices when working with Claude Code.

## License

Copyright 2024 NovaEdge Authors. Licensed under the Apache License, Version 2.0.
