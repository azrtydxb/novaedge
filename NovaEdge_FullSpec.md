# NovaEdge: Distributed Kubernetes-Native Load Balancer, Reverse Proxy & VIP Controller  
Version: 0.1  
Author: AI Architectural Draft  
Target: AI Coder Implementation  

---

# 1. Overview

NovaEdge is a **single unified system** that provides:

- **Distributed L7 load balancing** (HTTP/1.1, HTTP/2, WebSockets, gRPC, future HTTP/3)  
- **Reverse proxy with filters** (auth, rate-limit, rewrites, etc.)  
- **Ingress Controller** compatible with:  
  - Kubernetes Ingress  
  - Gateway API  
  - NovaEdge CRDs  
- **Distributed VIP management**, replacing MetalLB with:  
  - **L2 ARP mode** (active–passive VIP ownership)  
  - **BGP mode** (active–active multi-node ECMP)  
  - **OSPF mode** (active–active L3 routing)  
- **Node-level edge agents** binding to hostNetwork and exposing 80/443  
- **Kubernetes-native control-plane** using CRDs and watchers  
- **Dynamic config distribution** to agents via gRPC streaming  
- **Health checks, circuit breaking, outlier detection**  
- **High availability**, multi-node awareness, and distributed traffic routing  
- **Observability** (OpenTelemetry, Prometheus, logs, tracing)

NovaEdge is designed to be a **full replacement for Envoy + MetalLB + NGINX Ingress**, in a single cohesive Go codebase.

---

# 2. Architecture

NovaEdge consists of three major components:

## 2.1 Kubernetes Controller (Control-Plane)

Runs as a Deployment inside the cluster.

Responsibilities:
- Watch:
  - NovaEdge CRDs  
  - Kubernetes Ingress  
  - Gateway API (Gateway, HTTPRoute)  
  - Services + EndpointSlices  
  - Nodes (to determine LB agent placement)
- Build **desired-state routing graph**
- Build versioned **ConfigSnapshot**
- Push snapshots to node agents with gRPC streaming
- VIP logic:
  - Elect active node for L2
  - Instruct nodes to announce/unannounce BGP/OSPF routes
- Store state only in Kubernetes; no external DB required

## 2.2 Node Agent (Data Plane + Network Plane)

Runs as a DaemonSet with hostNetwork.

Responsibilities:
- Bind to :80/:443 on hostNetwork  
- Handle L7 load balancing (HTTP/H2/etc.)  
- Execute all routing/filtering logic  
- Manage upstream connection pools  
- Handle L2 ARP VIP mode  
- Handle BGP/OSPF VIP announcements  
- Report stats back to controller  
- Swap runtime config atomically when ConfigSnapshots arrive  

## 2.3 CRDs

NovaEdge exposes the following CRDs:

- `ProxyGateway` – describes listeners and TLS  
- `ProxyRoute` – describes routing rules  
- `ProxyBackend` – upstream objects  
- `ProxyPolicy` – authentication / rate-limit / etc.  
- `ProxyVIP` – describes external IP behaviour (L2/BGP/OSPF)

---

# 3. NovaEdge CRDs

## 3.1 ProxyVIP CRD

Describes the external IP and how NovaEdge exposes it through the node agents.

### YAML Example

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyVIP
metadata:
  name: public-vip
spec:
  address: 203.0.113.10/32
  mode: BGP     # L2ARP | BGP | OSPF
  ports:
    - 80
    - 443
  nodeSelector:
    matchLabels:
      novaedge-role: edge
  healthPolicy:
    minHealthyNodes: 2
```

### Fields

| Field | Description |
|-------|-------------|
| `address` | The VIP as CIDR, usually /32 |
| `mode` | L2ARP / BGP / OSPF |
| `ports` | Ports to bind on hostNetwork |
| `nodeSelector` | Which nodes can host this VIP |
| `healthPolicy` | Node health minimums |

---

## 3.2 ProxyGateway

Defines listeners, TLS, ingress class, hostnames.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: external-gateway
spec:
  vipRef: public-vip
  ingressClassName: novaedge
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        secretRef:
          name: tls-cert
      hostnames:
        - example.com
        - api.example.com
```

---

## 3.3 ProxyRoute

Routing rules similar to Gateway API HTTPRoute.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: example-route
spec:
  hostnames:
    - api.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /v1
      backendRef:
        name: api-backend
      filters:
        - type: AddHeader
          add:
            X-Version: "v1"
```

---

## 3.4 ProxyBackend

Mapping to Kubernetes Services or external endpoints.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: api-backend
spec:
  serviceRef:
    name: api-svc
    port: 8080
  lbPolicy: EWMA
  connectTimeout: 2s
  idleTimeout: 60s
  circuitBreaker:
    maxConnections: 1000
    maxPendingRequests: 200
```

---

# 4. Config Snapshot Model

Sent by controller → node agent.

Includes:
- All gateways assigned to this node
- All routes
- All backends + endpoints
- All filters
- Which VIPs this node should own/announce
- Ports to bind
- TLS certs or secret references

Proto sketch:

```proto
message ConfigSnapshot {
  string version = 1;
  repeated Gateway gateways = 2;
  repeated Route routes = 3;
  repeated Cluster clusters = 4;
  map<string, EndpointList> endpoints = 5;
  repeated VIPAssignment vipAssignments = 6;
}
```

---

# 5. VIP Exposure Logic

## 5.1 L2 ARP Mode

One node at a time owns a VIP.

Node agent:
- `ip addr add 203.0.113.10/32 dev eth0`
- Answer ARP for that IP
- Send GARPs
- Bind proxyd listeners

Failover:
- Controller moves VIP to another node after heartbeat loss
- New node sends GARPs

## 5.2 BGP Mode (Active–Active)

All healthy nodes advertise VIP via GoBGP.

Node agent:
- Adds VIP to loopback
- Announces `/32` route via BGP peers
- Withdraws route if unhealthy or told by controller

Routers ECMP across nodes.

## 5.3 OSPF Mode

Similar to BGP but using LSA advertisements.

---

# 6. Node Agent Internal Architecture

## 6.1 Components

- VIP Manager (L2ARP/BGP/OSPF)
- Listener Manager (bind ports)
- HTTP Server Engine  
- Router  
- Filter Chain  
- Load Balancer  
- Upstream Pool  
- Health Checker (active + passive)  
- Config Watcher (gRPC)  
- Stats Reporter  

## 6.2 Listener flow

```
TCP/80 or TCP/443 or QUIC/UDP/443
        ↓
   TLS termination
        ↓
   HTTP parser (H1/H2/H3)
        ↓
   Router (host/path/header)
        ↓
   Filters (auth, rate-limit, rewrite)
        ↓
   LB algorithm
        ↓
   Upstream connection pool
        ↓
   Backend service
```

---

# 7. Load Balancing Algorithms

- **Round Robin**
- **P2C (Power of Two Choices)**
- **EWMA (Latency-Aware)**
- **Ring Hash**
- **Maglev Hash**
- Consistent hash by:
  - Cookie
  - Header
  - Source IP

---

# 8. Health Checks & Circuit Breaking

## Active checks:
- HTTP GET /health
- TCP connect
- Interval + jitter
- Success/fail thresholds

## Passive checks:
- Track upstream failures
- Outlier ejection

## Circuit breaker:
- Max connections
- Max pending requests
- Failure budget

---

# 9. Security

- TLS termination with Kubernetes Secret support  
- mTLS between proxy → backend  
- JWT verification filter  
- Rate-limit token bucket  
- IP allow/deny lists  

---

# 10. Observability

- Prometheus metrics:
  - request_count
  - request_duration
  - upstream_rtt
  - open_connections
  - vip_failovers
  - bgp_announces / withdraws

- Structured logs with zap  
- OpenTelemetry trace export  

---

# 11. Ingress + Gateway API Integration

NovaEdge registers as:
- IngressClass controller: `novaedge.io/ingress-controller`
- GatewayClass controller: `novaedge.io/gateway-controller`

Ingress → Route translation:
- host → hostnames
- path → match.path
- backend → ProxyBackend
- annotations → filters / LB policy / timeouts

---

# 12. Kubernetes Deployment

## 12.1 Controller Deployment

- 3 replicas
- leader election enabled
- RBAC for CRDs + Services + EndpointSlices + Nodes

## 12.2 Node Agent DaemonSet

- hostNetwork: true  
- privileged: true (for ARP/BGP/OSPF and ip addr operations)  
- mounts /etc/novaedge/config for certs  

---

# 13. CLI Tool (novactl)

Commands:
- `novactl gateways`
- `novactl routes`
- `novactl vip list`
- `novactl vip status`
- `novactl backends`
- `novactl topology graph`
- `novactl stats --node <name>`

---

# 14. Full Repository Layout

```
novaedge/
├── cmd/
│   ├── novaedge-controller/
│   └── novaedge-agent/
├── internal/
│   ├── controller/
│   ├── agent/
│   │   ├── vip/
│   │   ├── lb/
│   │   ├── router/
│   │   ├── filters/
│   │   ├── upstream/
│   │   ├── health/
│   │   └── config/
│   └── proto/
├── api/
│   └── v1alpha1/
├── config/
│   ├── crd/
│   ├── samples/
│   └── rbac/
└── Makefile
```

---

# 15. Implementation Roadmap (AI Coder)

### Phase 1 – Core CRDs + Controller skeleton  
### Phase 2 – Config snapshot builder  
### Phase 3 – Basic HTTP L7 proxy + routing  
### Phase 4 – L2 VIP mode  
### Phase 5 – BGP VIP mode  
### Phase 6 – Filters + LB algorithms  
### Phase 7 – Health checking + circuit breaking  
### Phase 8 – Ingress + Gateway API support  
### Phase 9 – Observability + CLI  
### Phase 10 – HTTP/2 + WebSockets + gRPC  
### Phase 11 – HTTP/3 QUIC

---

# END OF DOCUMENT
