<p align="center">
  <img src="assets/novaedge-logo-light.svg" alt="NovaEdge" width="480">
</p>

# NovaEdge

**Kubernetes-Native Network Platform**

NovaEdge replaces Envoy + MetalLB + NGINX Ingress + Cisco SD-WAN with a single, integrated solution designed for modern Kubernetes deployments.

## Why NovaEdge?

```mermaid
flowchart LR
    subgraph Before["Traditional Stack"]
        NGINX["NGINX Ingress<br/>(L7 Routing)"]
        Envoy["Envoy<br/>(Policies)"]
        MetalLB["MetalLB<br/>(VIPs)"]
        SDWAN["WireGuard + Scripts<br/>(SD-WAN)"]
    end

    subgraph After["NovaEdge"]
        NE["NovaEdge<br/>(All-in-One)"]
    end

    Before -.->|"replaces"| After

    style Before fill:#FFE4B5
    style After fill:#90EE90
```

| Feature | Traditional | NovaEdge |
|---------|-------------|----------|
| L7 Load Balancing | NGINX/Envoy | Built-in (12 algorithms) |
| L4 TCP/UDP Proxying | HAProxy/Envoy | Built-in |
| VIP Management | MetalLB | Built-in (L2/BGP/OSPF + BFD) |
| Rate Limiting | Envoy/Kong | Built-in (local + Redis) |
| Authentication | OAuth2 Proxy/Kong | Built-in (Basic/Forward/OIDC) |
| WAF | ModSecurity/Kong | Built-in (Coraza) |
| TLS/ACME | cert-manager/Traefik | Built-in + cert-manager support |
| WASM Plugins | Envoy | Built-in (Wazero) |
| Service Mesh | Istio/Linkerd | Built-in (TPROXY, no sidecars) |
| SD-WAN | Cisco Viptela/WireGuard scripts | Built-in (WireGuard + path selection) |
| Control-Plane VIP | kube-vip | Built-in (L2/BGP/BFD) |
| Components to manage | 4+ | 1 |

[Full comparison: What NovaEdge Replaces](comparison.md){ .md-button }

## Key Features

- **L7 Load Balancing** - HTTP/1.1, HTTP/2, HTTP/3 (QUIC), WebSockets, gRPC, SSE
- **L4 Proxying** - TCP/UDP proxying with TLS passthrough
- **VIP Management** - L2 ARP, BGP, OSPF modes with BFD and IPv6 dual-stack
- **Security** - mTLS, OCSP stapling, PROXY protocol, WAF, authentication stack
- **Certificate Management** - ACME, cert-manager, HashiCorp Vault integration
- **Policy Enforcement** - Rate limiting, JWT auth, CORS, IP filtering, security headers
- **Extensibility** - WASM plugins, composable middleware pipelines
- **Gateway API** - Native support for Kubernetes Gateway API (HTTP, gRPC, TCP, TLS routes)
- **SD-WAN** - WireGuard tunnels, multi-WAN link management, SLA-based path selection, STUN NAT traversal, DSCP QoS
- **Multi-Cluster** - Hub-spoke federation with split-brain detection
- **Observability** - OpenTelemetry tracing, Prometheus metrics, structured logging, Web UI

## Quick Start

Get running in 2 minutes:

```bash
# Install the operator
helm install novaedge-operator ./charts/novaedge-operator \
  --namespace nova-system --create-namespace

# Deploy NovaEdge
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

[Full Quick Start Guide](getting-started/quickstart.md){ .md-button .md-button--primary }

## Architecture at a Glance

```mermaid
flowchart TB
    subgraph Cluster["Kubernetes Cluster"]
        subgraph Control["Control Plane"]
            OP["Operator"]
            CTRL["Controller"]
        end

        subgraph Data["Data Plane"]
            A1["Agent"]
            A2["Agent"]
            A3["Agent"]
        end

        CRD[(CRDs)]
        SVC["Services"]
    end

    Client((Client)) --> VIP{{"VIP"}}
    VIP --> A1 & A2 & A3

    OP --> CTRL
    CTRL -->|"watches"| CRD
    CTRL -->|"configures"| A1 & A2 & A3
    A1 & A2 & A3 --> SVC
```

**Components:**

- **Operator** - Manages NovaEdge lifecycle via `NovaEdgeCluster` CRD
- **Controller** - Watches CRDs, builds config, distributes to agents via gRPC
- **Agents** - Per-node DaemonSet handling traffic routing and VIP management

[Learn more about the architecture](architecture/overview.md)

## What NovaEdge Replaces

- [Full Comparison](comparison.md) - Tool-by-tool replacement guide with feature matrix

## Use Cases

Hands-on guides for common deployment scenarios, each with architecture diagrams and complete configurations:

- [API Gateway](use-cases/api-gateway.md) - Replace Kong/Ambassador with NovaEdge
- [Ingress Controller](use-cases/ingress-controller.md) - Replace NGINX Ingress Controller
- [Bare-Metal Load Balancer](use-cases/bare-metal-lb.md) - Replace MetalLB for bare-metal clusters
- [Gateway API](use-cases/gateway-api.md) - Use Kubernetes Gateway API with NovaEdge
- [Service Mesh](use-cases/service-mesh.md) - Replace Istio/Linkerd with TPROXY-based mesh
- [TLS & Certificate Management](use-cases/tls-management.md) - ACME, cert-manager, Vault integration
- [WAF & Security Stack](use-cases/waf-security.md) - Replace ModSecurity with defense-in-depth
- [Multi-Cluster Federation](use-cases/multi-cluster.md) - Hub-spoke federation across clusters

## Documentation

### Getting Started
- [Quick Start](getting-started/quickstart.md) - Deploy in 5 minutes
- [Installation](installation/kubernetes.md) - Detailed installation options
- [Helm Installation](installation/helm.md) - Deploy with Helm charts
- [Standalone Mode](installation/standalone.md) - Run without Kubernetes
- [Operator Installation](installation/operator.md) - Lifecycle management via operator

### Architecture
- [Architecture Overview](architecture/overview.md) - System design and components
- [Component Details](architecture/components.md) - Deep dive into each component
- [Federation Architecture](architecture/federation.md) - Multi-cluster federation design

### User Guide

#### Routing & Traffic
- [Routing](user-guide/routing.md) - Configure routes and traffic matching
- [Load Balancing](user-guide/load-balancing.md) - 12 algorithms and session affinity
- [L4 Proxying](user-guide/l4-proxying.md) - TCP/UDP proxying and TLS passthrough
- [Middleware Pipelines](user-guide/middleware-pipelines.md) - Composable middleware chains
- [Response Caching](user-guide/response-caching.md) - HTTP response caching
- [Traffic Mirroring](user-guide/traffic-mirroring.md) - Shadow traffic for testing
- [Retry](user-guide/retry.md) - Request retry configuration
- [Error Pages](user-guide/error-pages.md) - Custom error page handling
- [SSE](user-guide/sse.md) - Server-Sent Events support

#### Networking
- [PROXY Protocol](user-guide/proxy-protocol.md) - PROXY protocol v1/v2 support

#### Service Mesh
- [Service Mesh](user-guide/service-mesh.md) - TPROXY-based mesh with mTLS and authorization

#### Security & Authentication
- [TLS](user-guide/tls.md) - TLS termination, mTLS, OCSP stapling, ACME challenges
- [Authentication](user-guide/authentication.md) - Basic auth, forward auth, OIDC, JWT
- [Keycloak Integration](user-guide/keycloak.md) - Keycloak OIDC provider setup
- [Policies](user-guide/policies.md) - Rate limiting, CORS, JWT, IP filtering, security headers
- [WAF](user-guide/waf.md) - Web Application Firewall (Coraza)

#### Certificate Management
- [cert-manager Integration](user-guide/cert-manager.md) - Kubernetes-native certificate lifecycle
- [HashiCorp Vault](user-guide/vault.md) - Vault PKI and KV integration

#### Health & Monitoring
- [Health Checks](user-guide/health-checks.md) - Active and passive health checking

### Advanced Topics
- [Multi-Cluster Federation](advanced/multi-cluster.md) - Hub-spoke federation
- [Federation Setup](advanced/federation-setup.md) - Step-by-step federation configuration
- [HTTP/3 & QUIC](advanced/http3-quic.md) - Next-gen protocol support
- [Gateway API](advanced/gateway-api.md) - Kubernetes Gateway API integration
- [WASM Plugins](advanced/wasm-plugins.md) - Extend NovaEdge with WebAssembly plugins

### Operations
- [Observability](operations/observability.md) - Metrics, tracing, and logging
- [Web UI](operations/web-ui.md) - Dashboard for monitoring and management
- [Access Logging](operations/access-logging.md) - Per-route access log configuration
- [Troubleshooting](operations/troubleshooting.md) - Common issues and solutions

### Reference
- [CRD Reference](reference/crd-reference.md) - Complete CRD specifications
- [CLI Reference](reference/cli-reference.md) - novactl command reference
- [Helm Values](reference/helm-values.md) - Chart configuration options

### Development
- [Contributing](development/contributing.md) - How to contribute
- [Development Guide](development/development-guide.md) - Building from source

## License

Apache License 2.0
