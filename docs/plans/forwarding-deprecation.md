# Go Forwarding Path Deprecation Plan

**Status**: Planned (pending `--forwarding-plane=rust` validation)
**Tracking Issue**: #702

## Overview

With the introduction of `--forwarding-plane=rust|shadow`, the Go agent can
delegate all forwarding (L4/L7 proxying, load balancing, health checking,
policy enforcement, etc.) to the Rust dataplane daemon. Once the Rust
dataplane is validated in production via shadow mode, the Go forwarding
packages can be removed.

This document lists every Go package and file that becomes dead code when
`--forwarding-plane=rust` is the only supported mode.

## Removal Criteria

All of the following must be true before removing any code below:

1. Shadow mode has been running in production for at least two release cycles.
2. Zero discrepancies are observed in the `ShadowComparator` flow comparison.
3. The Rust dataplane passes the full conformance test suite.
4. The `--forwarding-plane=go` and `--forwarding-plane=shadow` flags are
   removed from `cmd/novaedge-agent/main.go`.

## Packages to Remove

### L4/L7 Forwarding

| Package | Description | Rust Replacement |
|---------|-------------|-----------------|
| `internal/agent/l4/` | TCP/UDP/TLS passthrough proxying | `dataplane/src/proxy/l4/` |
| `internal/agent/lb/` | Load balancing algorithms (12 types) | `dataplane/src/proxy/lb/` |
| `internal/agent/router/` | L7 HTTP routing, middleware, caching | `dataplane/src/proxy/l7/` |
| `internal/agent/server/` | HTTP/HTTPS/HTTP3 server, TLS, PROXY protocol | `dataplane/src/proxy/l7/` |
| `internal/agent/upstream/` | Connection pooling and reverse proxy transport | `dataplane/src/proxy/connection_pool/` |

### Policy and Security

| Package | Description | Rust Replacement |
|---------|-------------|-----------------|
| `internal/agent/policy/` | Rate limiting, auth, JWT, CORS, WAF, IP filter | `dataplane/src/middleware/` |

### Health and Resilience

| Package | Description | Rust Replacement |
|---------|-------------|-----------------|
| `internal/agent/health/` | Health checking, circuit breaking, outlier detection | `dataplane/src/proxy/health/` |
| `internal/agent/overload/` | Adaptive load shedding | Rust runtime resource management |

### Protocol Detection and Plugins

| Package | Description | Rust Replacement |
|---------|-------------|-----------------|
| `internal/agent/protocol/` | HTTP/WebSocket/gRPC protocol detection | Built into Rust proxy layer |
| `internal/agent/wasm/` | WASM plugin runtime (Wazero) | Rust WASM runtime (wasmtime) |

### eBPF (Go user-space helpers)

| Package | Description | Rust Replacement |
|---------|-------------|-----------------|
| `internal/agent/xdplb/` | XDP load balancing helpers | `dataplane/src/ebpf/` |
| `internal/agent/afxdp/` | AF_XDP socket management | `dataplane/src/ebpf/` |
| `internal/agent/ebpf/` | eBPF program loader, conntrack, maglev | `dataplane/src/ebpf/` |
| `internal/agent/ebpfmesh/` | eBPF mesh intercept programs | `dataplane/src/ebpf/` |
| `internal/agent/tunnel/` | WireGuard/mesh tunnels | `dataplane/src/proxy/` |

## Packages That Stay in Go

These packages remain in the Go agent even after full Rust forwarding:

| Package | Reason |
|---------|--------|
| `internal/agent/config/` | Config snapshot streaming from controller (gRPC client) |
| `internal/agent/grpc/` | gRPC handler for controller communication |
| `internal/agent/vip/` | VIP management (netlink, ARP, BGP, OSPF) -- kernel ops |
| `internal/agent/cpvip/` | Control-plane VIP for HA |
| `internal/agent/metrics/` | Prometheus metrics exporter |
| `internal/agent/introspection/` | Admin/debug API |
| `internal/agent/kernel/` | Kernel interface detection |
| `internal/agent/mesh/` | Service mesh cert requester and TPROXY setup |
| `internal/agent/sdwan/` | SD-WAN manager (WireGuard, link monitoring) |
| `internal/dataplane/` | Dataplane gRPC client and config translator |

## Files to Modify in cmd/novaedge-agent/main.go

When removing the Go forwarding path:

1. Remove `--forwarding-plane` and `--dataplane-socket` flags (default to Rust).
2. Remove the Go forwarding code block in the config reconciliation callback.
3. Remove shadow mode branching.
4. Remove imports for deprecated packages.
5. Simplify startup to always create the dataplane client.

## Estimated Removal Timeline

- **Phase 8**: Shadow mode production validation (2-4 weeks).
- **Phase 9**: Default `--forwarding-plane=rust`, deprecate `go` mode.
- **Phase 10**: Remove Go forwarding packages.
