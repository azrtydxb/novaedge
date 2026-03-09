# Component Details

Deep dive into each NovaEdge component, their responsibilities, and configuration options.

## Operator

The NovaEdge Operator manages the lifecycle of NovaEdge deployments using the `NovaEdgeCluster` CRD.

### Responsibilities

```mermaid
flowchart LR
    subgraph Operator["NovaEdge Operator"]
        R["Reconciler"]
    end

    NEC[("NovaEdgeCluster")] --> R

    R -->|"creates"| D1["Controller Deployment"]
    R -->|"creates"| D2["Agent DaemonSet"]
    R -->|"creates"| D3["Web UI Deployment"]
    R -->|"creates"| D4["RBAC Resources"]
    R -->|"creates"| D5["Services"]
    R -->|"creates"| D6["ConfigMaps"]
```

| Responsibility | Description |
|----------------|-------------|
| **Deployment** | Creates controller, agent, and web UI workloads |
| **Configuration** | Manages ConfigMaps and Secrets |
| **RBAC** | Creates ServiceAccounts, Roles, and RoleBindings |
| **Upgrades** | Rolling updates when version changes |
| **Status** | Reports component health in cluster status |

### Reconciliation Loop

```mermaid
sequenceDiagram
    participant K as Kubernetes API
    participant O as Operator
    participant R as Resources

    K->>O: NovaEdgeCluster event
    O->>O: Validate spec
    O->>R: Ensure RBAC
    O->>R: Create/Update Controller
    O->>R: Create/Update Agent
    O->>R: Create/Update Web UI
    O->>K: Update status

    loop Every 30s
        O->>R: Check health
        O->>K: Update conditions
    end
```

### Configuration

The Operator is configured via the `NovaEdgeCluster` CRD:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeCluster
metadata:
  name: novaedge
  namespace: nova-system
spec:
  version: "v0.1.0"
  controller:
    replicas: 3
    leaderElection: true
  agent:
    hostNetwork: true
    vip:
      enabled: true
      mode: BGP
  webUI:
    enabled: true
```

## Controller

The Controller is the control plane component that watches Kubernetes resources and distributes configuration to Agents.

### Internal Architecture

```mermaid
flowchart TB
    subgraph Controller["Controller"]
        subgraph Watchers["Resource Watchers"]
            W1["Gateway Watcher"]
            W2["Route Watcher"]
            W3["Backend Watcher"]
            W4["Policy Watcher"]
            W5["VIP Watcher"]
            W6["EndpointSlice Watcher"]
            W7["Secret Watcher"]
        end

        subgraph Core["Core"]
            SB["Snapshot Builder"]
            VS["Version Manager"]
            LE["Leader Election"]
        end

        subgraph Distribution["Distribution"]
            GS["gRPC Server"]
            SM["Session Manager"]
        end
    end

    W1 & W2 & W3 & W4 & W5 & W6 & W7 --> SB
    SB --> VS
    VS --> GS
    LE --> SB
    GS --> SM
```

### Responsibilities

| Component | Purpose |
|-----------|---------|
| **Resource Watchers** | Watch CRDs, EndpointSlices, Secrets via informers |
| **Snapshot Builder** | Build versioned ConfigSnapshots |
| **Version Manager** | Track snapshot versions, detect changes |
| **gRPC Server** | Stream snapshots to Agents |
| **Session Manager** | Track connected Agents |
| **Leader Election** | Ensure single active controller |

### ConfigSnapshot Structure

```mermaid
classDiagram
    class ConfigSnapshot {
        +string version
        +[]Gateway gateways
        +[]Route routes
        +[]Backend backends
        +[]Policy policies
        +[]VIP vips
        +[]Certificate certificates
    }

    class Gateway {
        +string name
        +[]Listener listeners
        +string vipRef
    }

    class Route {
        +string name
        +[]Match matches
        +BackendRef backendRef
        +[]Filter filters
    }

    class Backend {
        +string name
        +[]Endpoint endpoints
        +string lbPolicy
        +HealthCheck healthCheck
    }

    ConfigSnapshot --> Gateway
    ConfigSnapshot --> Route
    ConfigSnapshot --> Backend
```

### Configuration

```yaml
# Controller command-line flags
--grpc-port=9090        # gRPC server port (CRD default: 9090, Helm chart default: 8082)
--metrics-port=8080     # Prometheus metrics port
--health-port=8081      # Health probe port
--log-level=info        # Log level
--leader-election=true  # Enable leader election
```

## Agent (Config Agent)

The Go Agent is a **config-only agent** that manages VIPs, receives configuration from the controller, and pushes it to the Rust Dataplane. It does NOT handle user traffic.

### Internal Architecture

```mermaid
flowchart TB
    subgraph Agent["Go Agent (Config Agent)"]
        subgraph Config["Configuration"]
            GC["gRPC Client<br/>(from controller)"]
            CM["Config Manager"]
            TRANSLATOR["Config Translator"]
        end

        subgraph VIPMgmt["VIP Management"]
            VIP["VIP Manager<br/>(L2/BGP/OSPF/BFD)"]
        end

        subgraph NovaNetComm["NovaNet Communication"]
            NOVANET["NovaNet gRPC Client"]
        end

        subgraph DataplanePush["Dataplane Communication"]
            GRPC_PUSH["gRPC Push to Rust Dataplane"]
        end

        subgraph Observability["Observability"]
            MET["Metrics"]
            HEALTH["Health Probes"]
            ADMIN["Admin API"]
        end
    end

    GC --> CM
    CM --> TRANSLATOR
    TRANSLATOR --> GRPC_PUSH
    CM --> VIP
    CM --> NOVANET
```

## Rust Dataplane (Traffic Handler)

The Rust Dataplane is the **actual data plane** that handles all L4/L7 traffic. It runs as a DaemonSet sidecar alongside the Go Agent.

### Internal Architecture

```mermaid
flowchart TB
    subgraph Dataplane["Rust Dataplane (Traffic Handler)"]
        subgraph ConfigRecv["Configuration"]
            GRPC_RECV["gRPC Server<br/>(from Go Agent)"]
            CFG["Config Store"]
        end

        subgraph Traffic["Traffic Handling"]
            RT["Router"]
            POL["Policy Engine"]
            LB["Load Balancer"]
        end

        subgraph Backend["Backend Management"]
            HC["Health Checker"]
            POOL["Connection Pool"]
            CB["Circuit Breaker"]
        end
    end

    GRPC_RECV --> CFG
    CFG --> RT & POL & LB & HC

    Traffic --> Backend
```

### Subcomponents

#### VIP Manager (Go Agent)

Manages virtual IP addresses on the node (part of the Go config agent):

```mermaid
flowchart LR
    subgraph VIPManager["VIP Manager"]
        L2["L2 ARP Handler"]
        BGP["BGP Speaker"]
        OSPF["OSPF Handler"]
        NI["Network Interface"]
    end

    Config((Config)) --> L2 & BGP & OSPF
    L2 --> NI
    BGP --> Router[["External Router"]]
    OSPF --> Router
```

| Mode | Implementation |
|------|----------------|
| L2 ARP | Bind VIP to interface, send GARP |
| BGP | Announce VIP via GoBGP |
| OSPF | Advertise via OSPF LSAs |

#### Router (Rust Dataplane)

Matches incoming requests to routes (part of the Rust dataplane):

```mermaid
flowchart LR
    Req((Request)) --> HM["Host Matcher"]
    HM --> PM["Path Matcher"]
    PM --> MM["Method Matcher"]
    MM --> HeaderM["Header Matcher"]
    HeaderM --> Route["Selected Route"]
```

Matching order:
1. Hostname (exact > suffix > prefix > wildcard)
2. Path (exact > prefix > regex)
3. Method
4. Headers

#### Policy Engine (Rust Dataplane)

Applies policies to requests (part of the Rust dataplane):

```mermaid
flowchart LR
    Req((Request)) --> RL["Rate Limiter"]
    RL --> JWT["JWT Validator"]
    JWT --> CORS["CORS Handler"]
    CORS --> IP["IP Filter"]
    IP --> Pass((Pass/Reject))
```

| Policy | Function |
|--------|----------|
| Rate Limit | Token bucket per key (client IP, header) |
| JWT | Validate tokens against JWKS |
| CORS | Handle preflight, set headers |
| IP Filter | Allow/deny by CIDR |

#### Load Balancer (Rust Dataplane)

Selects backend endpoints (part of the Rust dataplane):

```mermaid
flowchart TB
    subgraph LoadBalancer["Load Balancer"]
        RR["Round Robin"]
        P2C["Power of Two Choices"]
        EWMA["EWMA (Latency-aware)"]
        RH["Ring Hash"]
        MG["Maglev"]
    end

    Policy((Policy)) --> RR & P2C & EWMA & RH & MG
    RR & P2C & EWMA & RH & MG --> EP["Selected Endpoint"]
```

| Algorithm | Use Case |
|-----------|----------|
| Round Robin | Equal distribution |
| P2C | Low latency |
| EWMA | Latency-aware |
| Ring Hash | Session affinity |
| Maglev | High-performance consistent hashing |

#### Health Checker (Rust Dataplane)

Monitors backend health (part of the Rust dataplane):

```mermaid
sequenceDiagram
    participant HC as Health Checker
    participant EP as Endpoint
    participant LB as Load Balancer

    loop Every interval
        HC->>EP: Health probe (HTTP/TCP/gRPC)
        alt Healthy
            EP-->>HC: Success
            HC->>LB: Mark healthy
        else Unhealthy
            EP-->>HC: Failure
            HC->>LB: Mark unhealthy
        end
    end
```

Supports:
- HTTP health checks
- TCP connection checks
- gRPC health protocol
- Passive failure detection

#### Connection Pool (Rust Dataplane)

Manages backend connections (part of the Rust dataplane):

```mermaid
flowchart TB
    subgraph ConnectionPool["Connection Pool"]
        POOL["Pool Manager"]
        HTTP11["HTTP/1.1 Pool"]
        HTTP2["HTTP/2 Pool"]
        HTTP3["HTTP/3 Pool"]
    end

    POOL --> HTTP11 & HTTP2 & HTTP3
    HTTP11 & HTTP2 & HTTP3 --> Backend((Backend))
```

Features:
- Connection reuse
- Keep-alive management
- Automatic protocol detection
- Connection limits

#### SD-WAN Engine

Manages multi-link WAN connectivity and application-aware path selection:

```mermaid
flowchart TB
    subgraph SDWANEngine["SD-WAN Engine"]
        LM["WAN Link Manager"]
        PRB["SLA Prober"]
        PS["Path Selection Engine"]
        STUN["STUN Discoverer"]
        DSCP["DSCP Marker"]
    end

    Config((Config)) --> LM
    LM --> PRB
    PRB -->|"latency, jitter, loss"| PS
    PS -->|"select link"| DSCP
    STUN --> LM
```

| Component | Purpose |
|-----------|---------|
| WAN Link Manager | Tracks WAN link state (up/down/degraded) and manages WireGuard tunnels |
| SLA Prober | Measures latency, jitter, and packet loss with EWMA smoothing |
| Path Selection Engine | Selects optimal WAN path using 4 strategies (lowest-latency, highest-bandwidth, most-reliable, lowest-cost) |
| STUN Discoverer | Discovers public endpoints for NAT traversal in tunnel establishment |
| DSCP Marker | Applies DSCP markings for QoS enforcement on outbound traffic |

#### eBPF Acceleration (via NovaNet)

eBPF acceleration services (SOCKMAP bypass, mesh redirect, rate limiting, health monitoring) are provided by [NovaNet](https://github.com/azrtydxb/novanet), the Nova CNI component. NovaEdge no longer loads or manages eBPF programs directly. Instead, the Go agent communicates with NovaNet via a gRPC client over a Unix domain socket at `/run/novanet/ebpf-services.sock`.

```mermaid
flowchart LR
    subgraph Agent["Go Agent"]
        GC["NovaNet gRPC Client"]
    end

    subgraph NovaNet["NovaNet"]
        SVC["eBPF Services API"]
        SOCKMAP["SOCKMAP Bypass"]
        SKLOOKUP["Mesh Redirect"]
        RL["Rate Limiting"]
    end

    GC -->|"Unix socket"| SVC
    SVC --> SOCKMAP & SKLOOKUP & RL
```

If NovaNet is not available, NovaEdge continues to operate without eBPF acceleration (graceful degradation). See [eBPF Acceleration (NovaNet)](../user-guide/ebpf-acceleration.md) for details.

### Configuration

```yaml
# Agent command-line flags
--controller-addr=controller:9090  # Controller address
--node-name=$NODE_NAME             # Node name (from downward API)
--http-port=80                     # HTTP traffic port
--https-port=443                   # HTTPS traffic port
--metrics-port=9090                # Prometheus metrics port
--health-port=8080                 # Health probe port
--log-level=info                   # Log level
--novanet-socket=/run/novanet/ebpf-services.sock  # NovaNet eBPF services socket
```

## Web UI

Optional dashboard for monitoring and management.

### Features

```mermaid
flowchart LR
    subgraph WebUI["Web UI"]
        DASH["Dashboard"]
        RES["Resource Browser"]
        MET["Metrics Viewer"]
        LOGS["Log Viewer"]
    end

    DASH --> API["NovaEdge API"]
    MET --> PROM["Prometheus"]
    LOGS --> K8S["Kubernetes Logs"]
```

| Feature | Description |
|---------|-------------|
| Dashboard | Overview of cluster health |
| Resource Browser | View/edit CRDs |
| Metrics Viewer | Prometheus integration |
| Topology | Visual service map |

### Security

- Authentication via Kubernetes ServiceAccount
- RBAC-based authorization
- Read-only mode available
- TLS support

## Inter-Component Communication

```mermaid
sequenceDiagram
    participant OP as Operator
    participant K as Kubernetes
    participant CTRL as Controller
    participant A as Agent
    participant UI as Web UI

    OP->>K: Watch NovaEdgeCluster
    OP->>K: Create Controller
    OP->>K: Create Agent DaemonSet
    OP->>K: Create Web UI

    CTRL->>K: Watch CRDs
    A->>CTRL: Connect (gRPC)
    CTRL->>A: Stream ConfigSnapshot

    UI->>K: Query CRDs
    UI->>CTRL: Get status
```

## Resource Requirements

| Component | CPU Request | Memory Request | Notes |
|-----------|------------|----------------|-------|
| Operator | 100m | 128Mi | Single instance |
| Controller | 200m | 256Mi | Per replica |
| Agent | 200m | 256Mi | Per node |
| Web UI | 100m | 128Mi | Optional |

## Next Steps

- [Installation](../installation/kubernetes.md) - Deploy NovaEdge
- [Routing](../user-guide/routing.md) - Configure routes
- [Load Balancing](../user-guide/load-balancing.md) - LB algorithms
