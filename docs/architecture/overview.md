# Architecture Overview

NovaEdge is a distributed system designed to provide L7 load balancing, VIP management, and policy enforcement for Kubernetes environments.

## System Architecture

```mermaid
flowchart TB
    subgraph Kubernetes["Kubernetes Cluster"]
        subgraph ControlPlane["Control Plane"]
            OP["NovaEdge Operator<br/>(Deployment)"]
            CTRL["NovaEdge Controller<br/>(Deployment)"]
        end

        subgraph DataPlane["Data Plane (per node)"]
            A1["Agent<br/>(DaemonSet)"]
            A2["Agent<br/>(DaemonSet)"]
            A3["Agent<br/>(DaemonSet)"]
        end

        subgraph Storage["Configuration"]
            CRD[("CRDs<br/>Gateway, Route<br/>Backend, Policy, VIP")]
            SEC[("Secrets<br/>TLS Certificates")]
        end

        subgraph Workloads["Backend Services"]
            S1["Service A"]
            S2["Service B"]
            S3["Service C"]
        end
    end

    Client((Client)) --> VIP{{"VIP<br/>192.168.1.100"}}

    VIP --> A1
    VIP --> A2
    VIP --> A3

    OP -->|"manages"| CTRL
    OP -->|"manages"| A1 & A2 & A3

    CTRL -->|"watches"| CRD
    CTRL -->|"reads"| SEC
    CTRL -->|"gRPC streaming"| A1 & A2 & A3

    A1 --> S1 & S2
    A2 --> S2 & S3
    A3 --> S1 & S3

    style ControlPlane fill:#e6f3ff
    style DataPlane fill:#90EE90
```

## Core Components

| Component | Type | Purpose |
|-----------|------|---------|
| **Operator** | Deployment | Manages NovaEdge lifecycle via `NovaEdgeCluster` CRD |
| **Controller** | Deployment | Watches CRDs, builds config snapshots, distributes via gRPC |
| **Agent** | DaemonSet | Config agent: receives config from controller, manages VIPs, pushes config to Rust dataplane |
| **Rust Dataplane** | DaemonSet sidecar | Traffic handler: binds ports 80/443, handles all L4/L7 traffic, routing, policies, load balancing |

## Request Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant V as VIP
    participant DP as Rust Dataplane
    participant R as Router
    participant P as Policies
    participant LB as Load Balancer
    participant B as Backend

    C->>V: HTTP Request
    V->>DP: Route to node (port 80/443)
    DP->>R: Match route
    R->>P: Apply policies
    Note over P: Rate limit, JWT, CORS
    P->>LB: Select backend
    Note over LB: RoundRobin, P2C, EWMA
    LB->>B: Forward request
    B-->>LB: Response
    LB-->>P: Apply response policies
    P-->>DP: Return response
    DP-->>C: HTTP Response
```

Note: The Go Agent does not appear in the request flow. It runs alongside the Rust Dataplane as a config agent, managing VIPs and pushing configuration updates via gRPC.

## Configuration Distribution

The Controller builds configuration snapshots and distributes them to Agents via gRPC streaming:

```mermaid
sequenceDiagram
    participant K as Kubernetes API
    participant C as Controller
    participant A1 as Agent (Node 1)
    participant A2 as Agent (Node 2)

    K->>C: CRD Change Event
    C->>C: Build ConfigSnapshot
    C->>A1: Stream ConfigSnapshot
    C->>A2: Stream ConfigSnapshot
    A1->>A1: Atomic config swap
    A2->>A2: Atomic config swap
    A1-->>C: ACK
    A2-->>C: ACK
```

### ConfigSnapshot Contents

Each snapshot contains:

- **Gateways** - Listeners, protocols, TLS config
- **Routes** - Matching rules, filters, backend refs
- **Backends** - Endpoints from EndpointSlices, LB policy
- **Policies** - Rate limits, JWT config, CORS rules
- **VIPs** - VIP assignments for this node
- **Certificates** - TLS certificates from Secrets

## Control Plane Details

### Controller

The Controller runs as a Kubernetes Deployment with leader election:

```mermaid
flowchart LR
    subgraph Controller["Controller Pod"]
        direction TB
        WM["Watch Manager"]
        SB["Snapshot Builder"]
        GS["gRPC Server"]
        LE["Leader Election"]
    end

    CRD[(CRDs)] --> WM
    EP[(EndpointSlices)] --> WM
    SEC[(Secrets)] --> WM

    WM --> SB
    SB --> GS
    LE --> WM
```

**Responsibilities:**

1. Watch CRDs, EndpointSlices, and Secrets
2. Build versioned ConfigSnapshots
3. Stream snapshots to connected Agents
4. Handle leader election for HA

### Operator

The Operator manages the NovaEdge deployment lifecycle:

```mermaid
flowchart TB
    subgraph Operator["Operator"]
        R["Reconciler"]
    end

    NEC[("NovaEdgeCluster<br/>CRD")] --> R

    R --> CTRL["Controller<br/>Deployment"]
    R --> AGENT["Agent<br/>DaemonSet"]
    R --> WEBUI["Web UI<br/>Deployment"]
    R --> RBAC["RBAC<br/>Resources"]
    R --> SVC["Services"]
```

## Data Plane Details

### Agent

Each node runs two DaemonSet pods: the Go Agent (config agent) and the Rust Dataplane (traffic handler), both with `hostNetwork: true`:

```mermaid
flowchart TB
    subgraph GoAgent["Go Agent (Config Agent)"]
        GC["gRPC Client<br/>(receives config from controller)"]
        VIP["VIP Manager<br/>(L2/BGP/OSPF/BFD)"]
        TRANSLATOR["Config Translator"]
        GRPC_PUSH["gRPC Push to Dataplane"]
    end

    subgraph RustDP["Rust Dataplane (Traffic Handler)"]
        RT["Router"]
        LB["Load Balancer"]
        HC["Health Checker"]
        POL["Policy Engine"]
        POOL["Connection Pool"]

        subgraph eBPF["eBPF/XDP Acceleration"]
            AFXDP["AF_XDP Zero-Copy"]
            SKLOOKUP["SK_LOOKUP Mesh Redirect"]
            SOCKMAP["SOCKMAP Same-Node Bypass"]
        end
    end

    GC --> TRANSLATOR
    TRANSLATOR --> GRPC_PUSH
    GRPC_PUSH -->|"config"| RT & LB & POL & HC
    GC -->|"VIP config"| VIP

    Traffic((Traffic)) --> AFXDP
    AFXDP --> RT
    RT --> POL
    POL --> LB
    LB --> HC
    HC --> POOL
    POOL --> Backend((Backend))

    style eBPF fill:#fff4e6
    style GoAgent fill:#e6f3ff
    style RustDP fill:#90EE90
```

**Agent Responsibilities (Config Agent):**

1. Receive config from controller via gRPC streaming
2. Bind/unbind VIPs on node interface (L2 ARP/BGP/OSPF/BFD)
3. Translate config and push to Rust dataplane via gRPC
4. Manage iptables/nftables rules
5. Manage eBPF program lifecycle

**Rust Dataplane Responsibilities (Traffic Handler):**

1. Bind ports 80/443 and accept all inbound traffic
2. Route incoming requests (hostname, path, header matching)
3. Apply policies (rate limit, JWT, CORS, WAF)
4. Load balance across healthy backends
5. Manage connection pools and circuit breakers
6. Perform active and passive health checks
7. Accelerate traffic via eBPF/XDP (AF_XDP zero-copy, SOCKMAP bypass, sk_lookup mesh redirect)

## VIP Modes

NovaEdge supports three VIP modes for different network topologies:

### L2 ARP Mode (Active/Standby)

```mermaid
flowchart TB
    Client((Client)) --> Switch[["Switch"]]
    Switch --> VIP{{"VIP"}}

    VIP -.->|"GARP"| N1["Node 1<br/>(Active)"]
    N1 -->|"failover"| N2["Node 2<br/>(Standby)"]
    N1 -->|"failover"| N3["Node 3<br/>(Standby)"]

    style N1 fill:#90EE90
    style N2 fill:#FFE4B5
    style N3 fill:#FFE4B5
```

- Single node owns VIP at a time
- Sends Gratuitous ARP to claim VIP
- Controller manages failover

### BGP Mode (Active/Active ECMP)

```mermaid
flowchart TB
    Client((Client)) --> Router[["BGP Router"]]

    Router -->|"ECMP"| N1["Node 1"]
    Router -->|"ECMP"| N2["Node 2"]
    Router -->|"ECMP"| N3["Node 3"]

    N1 -.->|"BGP peer"| Router
    N2 -.->|"BGP peer"| Router
    N3 -.->|"BGP peer"| Router

    style N1 fill:#90EE90
    style N2 fill:#90EE90
    style N3 fill:#90EE90
```

- All healthy nodes announce VIP
- ToR router performs ECMP
- Automatic failover via BGP withdrawal

### OSPF Mode

- Similar to BGP using OSPF LSAs
- Active/Active with L3 routing
- Useful in OSPF-only environments

## CRD Relationships

```mermaid
flowchart TB
    VIP["ProxyVIP<br/>VIP address & mode"]
    GW["ProxyGateway<br/>Listeners & protocols"]
    RT["ProxyRoute<br/>Matching rules"]
    BE["ProxyBackend<br/>Endpoints & LB policy"]
    POL["ProxyPolicy<br/>Rate limit, JWT, CORS"]
    WL["ProxyWANLink<br/>WAN link management"]
    WP["ProxyWANPolicy<br/>Path selection"]

    GW -->|"vipRef"| VIP
    RT -->|"parentRefs"| GW
    RT -->|"backendRef"| BE
    RT -->|"policyRefs"| POL
    BE -->|"serviceRef"| SVC[(Service)]
    WP -->|"selects"| WL

    style VIP fill:#FFD700
    style GW fill:#87CEEB
    style RT fill:#98FB98
    style BE fill:#DDA0DD
    style POL fill:#F0E68C
    style WL fill:#e8f5e9
    style WP fill:#e8f5e9
```

## Scalability

| Component | Scaling Model |
|-----------|---------------|
| Controller | Horizontal (with leader election) |
| Agent | One per node (DaemonSet) |
| Throughput | Linear with node count |

## High Availability

- **Controller**: Multiple replicas with leader election
- **Agent**: Runs on every node, VIP failover between nodes
- **Config**: Cached locally, survives controller restarts

## Next Steps

- [Component Details](components.md) - Deep dive into each component
- [Installation](../installation/kubernetes.md) - Deploy NovaEdge
