# CRD Reference

NovaEdge uses Custom Resource Definitions (CRDs) to configure load balancing, routing, and policies.

## CRD Overview

```mermaid
flowchart TB
    subgraph Cluster["Cluster Management"]
        NEC["NovaEdgeCluster<br/>Operator-managed deployment"]
        NERC["NovaEdgeRemoteCluster<br/>Multi-cluster registration"]
        FED["NovaEdgeFederation<br/>Active-active controllers"]
    end

    subgraph DataPlane["Data Plane Configuration"]
        VIP["ProxyVIP<br/>Virtual IP definition"]
        IPPool["ProxyIPPool<br/>IP address pool management"]
        GW["ProxyGateway<br/>Listeners (ports/protocols)"]
        RT["ProxyRoute<br/>Routing rules"]
        BE["ProxyBackend<br/>Upstream services"]
        POL["ProxyPolicy<br/>Rate limit, CORS, JWT, etc."]
    end

    NEC --> GW
    NEC --> VIP
    NERC --> NEC
    FED --> NEC

    IPPool --> VIP
    VIP --> GW
    GW --> RT
    RT --> BE
    RT --> POL
    GW --> POL

    style Cluster fill:#e6f3ff
    style DataPlane fill:#fff5e6
```

## CRD Relationships

```mermaid
flowchart LR
    subgraph Input["Traffic Input"]
        Client((Client))
    end

    subgraph VIPLayer["VIP Layer"]
        VIP["ProxyVIP<br/>192.168.1.100"]
    end

    subgraph GatewayLayer["Gateway Layer"]
        GW["ProxyGateway<br/>:80 HTTP<br/>:443 HTTPS"]
    end

    subgraph RoutingLayer["Routing Layer"]
        RT1["ProxyRoute<br/>api.example.com"]
        RT2["ProxyRoute<br/>web.example.com"]
    end

    subgraph PolicyLayer["Policy Layer"]
        POL1["RateLimit"]
        POL2["JWT Auth"]
        POL3["CORS"]
    end

    subgraph BackendLayer["Backend Layer"]
        BE1["ProxyBackend<br/>api-service"]
        BE2["ProxyBackend<br/>web-service"]
    end

    Client --> VIP
    IPPool --> VIP
    VIP --> GW
    GW --> RT1
    GW --> RT2
    RT1 --> POL1
    RT1 --> POL2
    RT2 --> POL3
    RT1 --> BE1
    RT2 --> BE2

    style VIPLayer fill:#90EE90
    style GatewayLayer fill:#ADD8E6
    style RoutingLayer fill:#FFE4B5
    style PolicyLayer fill:#DDA0DD
    style BackendLayer fill:#F0E68C
```

## NovaEdgeCluster

Defines a complete NovaEdge deployment managed by the operator.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeCluster
metadata:
  name: novaedge
  namespace: novaedge-system
spec:
  # Version of NovaEdge to deploy
  version: "v0.1.0"

  # Image repository (optional)
  imageRepository: ghcr.io/piwi3910/novaedge
  imagePullPolicy: IfNotPresent

  # Controller configuration
  controller:
    replicas: 1
    leaderElection: true
    grpcPort: 9090
    metricsPort: 8080
    healthPort: 8081
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "500m"
        memory: "512Mi"

  # Agent DaemonSet configuration
  agent:
    hostNetwork: true
    httpPort: 80
    httpsPort: 443
    metricsPort: 9090
    healthPort: 8080
    vip:
      enabled: true
      mode: L2  # L2, BGP, or OSPF
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"

  # Web UI configuration (optional)
  webUI:
    enabled: true
    replicas: 1
    port: 9080
    readOnly: false
    service:
      type: ClusterIP
    prometheusEndpoint: "http://prometheus:9090"

  # Observability configuration
  observability:
    metrics:
      enabled: true
      serviceMonitor:
        enabled: true
        interval: "30s"
    tracing:
      enabled: true
      endpoint: "jaeger-collector:4317"
      samplingRate: 10
    logging:
      level: info
      format: json
```

### NovaEdgeCluster Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.version` | string | Yes | NovaEdge version to deploy |
| `spec.imageRepository` | string | No | Container image repository |
| `spec.imagePullPolicy` | string | No | Image pull policy |
| `spec.controller` | object | Yes | Controller deployment configuration |
| `spec.agent` | object | Yes | Agent DaemonSet configuration |
| `spec.webUI` | object | No | Web UI deployment configuration |
| `spec.tls` | object | No | Internal TLS configuration |
| `spec.observability` | object | No | Metrics, tracing, logging config |

### Controller Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `replicas` | int | 1 | Number of controller replicas |
| `leaderElection` | bool | true | Enable leader election for HA |
| `grpcPort` | int | 9090 | gRPC config server port |
| `metricsPort` | int | 8080 | Prometheus metrics port |
| `healthPort` | int | 8081 | Health probe port |
| `resources` | object | - | Resource requirements |
| `nodeSelector` | map | - | Node selector |
| `tolerations` | array | - | Pod tolerations |
| `affinity` | object | - | Pod affinity rules |

### Agent Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `hostNetwork` | bool | true | Enable host networking |
| `httpPort` | int | 80 | HTTP traffic port |
| `httpsPort` | int | 443 | HTTPS traffic port |
| `metricsPort` | int | 9090 | Prometheus metrics port |
| `healthPort` | int | 8080 | Health probe port |
| `vip.enabled` | bool | true | Enable VIP management |
| `vip.mode` | string | L2 | VIP mode: L2, BGP, OSPF |
| `vip.interface` | string | - | Network interface (L2 mode) |
| `vip.bgp` | object | - | BGP configuration |
| `updateStrategy` | object | - | DaemonSet update strategy |

### NovaEdgeCluster Status

```yaml
status:
  phase: Running
  observedGeneration: 1
  version: v0.1.0
  conditions:
    - type: Ready
      status: "True"
      reason: AllComponentsReady
    - type: ControllerReady
      status: "True"
    - type: AgentReady
      status: "True"
  controller:
    ready: true
    replicas: 1
    readyReplicas: 1
  agent:
    ready: true
    replicas: 3
    readyReplicas: 3
```

---

## NovaEdgeRemoteCluster

Represents a remote/edge cluster in a hub-spoke multi-cluster deployment. Created in the hub cluster to register and monitor remote clusters.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeRemoteCluster
metadata:
  name: edge-west-1
  namespace: novaedge-system
spec:
  # Unique cluster identifier
  clusterName: edge-west-1

  # Geographic location
  region: us-west
  zone: us-west-2a

  # Additional labels
  labels:
    environment: production
    tier: edge

  # Connection to hub controller
  connection:
    mode: Direct  # Direct or Tunnel
    controllerEndpoint: controller.novaedge-system.svc.cluster.local:9090
    reconnectInterval: 30s
    timeout: 10s
    tls:
      enabled: true
      caSecretRef:
        name: novaedge-ca
        namespace: novaedge-system
      serverName: novaedge-controller

  # Agent configuration for this cluster
  agent:
    version: "v0.1.0"  # Override version
    nodeSelector:
      node-role.kubernetes.io/edge: "true"
    vip:
      enabled: true
      mode: L2

  # Routing configuration
  routing:
    enabled: true
    priority: 100
    weight: 100
    localPreference: true
    allowCrossClusterTraffic: true

  # Health check configuration
  healthCheck:
    enabled: true
    interval: 30s
    timeout: 10s
    healthyThreshold: 2
    unhealthyThreshold: 3
    failoverEnabled: true
```

### NovaEdgeRemoteCluster Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.clusterName` | string | Yes | Unique identifier for the remote cluster |
| `spec.region` | string | No | Geographic region |
| `spec.zone` | string | No | Availability zone |
| `spec.labels` | map | No | Additional labels for the cluster |
| `spec.connection` | object | Yes | Connection configuration |
| `spec.agent` | object | No | Agent configuration override |
| `spec.routing` | object | No | Routing configuration |
| `spec.healthCheck` | object | No | Health check configuration |
| `spec.paused` | bool | No | Suspend reconciliation |

### Connection Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `mode` | string | Direct | Connection mode: `Direct` or `Tunnel` |
| `controllerEndpoint` | string | Required | Hub controller gRPC endpoint |
| `reconnectInterval` | duration | 30s | Reconnection interval |
| `timeout` | duration | 10s | Connection timeout |
| `tls.enabled` | bool | true | Enable mTLS |
| `tls.caSecretRef` | object | - | CA certificate secret reference |
| `tls.clientCertSecretRef` | object | - | Client certificate secret reference |
| `tls.serverName` | string | - | Expected server name for TLS |
| `tls.insecureSkipVerify` | bool | false | Skip TLS verification |
| `tunnel` | object | - | Tunnel configuration (when mode=Tunnel) |

### Routing Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | true | Enable routing to/from this cluster |
| `priority` | int | 100 | Routing priority (lower = higher priority) |
| `weight` | int | 100 | Traffic weight for weighted routing |
| `localPreference` | bool | true | Prefer local backends within cluster |
| `allowCrossClusterTraffic` | bool | true | Allow cross-cluster traffic routing |
| `endpoints` | object | - | Endpoint selection filters |

### Health Check Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | true | Enable health checking |
| `interval` | duration | 30s | Health check interval |
| `timeout` | duration | 10s | Health check timeout |
| `healthyThreshold` | int | 2 | Consecutive successes for healthy |
| `unhealthyThreshold` | int | 3 | Consecutive failures for unhealthy |
| `failoverEnabled` | bool | true | Enable automatic failover |

### NovaEdgeRemoteCluster Status

```yaml
status:
  phase: Connected
  observedGeneration: 1
  conditions:
    - type: Ready
      status: "True"
      reason: Connected
    - type: AgentsHealthy
      status: "True"
  connection:
    connected: true
    activeConnections: 3
    lastConnected: "2024-01-15T10:30:00Z"
    latency: "15ms"
  agents:
    total: 3
    ready: 3
    healthy: 3
    nodes:
      - name: edge-node-1
        ready: true
        ip: 10.1.0.10
        vips: ["192.168.1.100"]
      - name: edge-node-2
        ready: true
        ip: 10.1.0.11
  lastHeartbeat: "2024-01-15T12:00:00Z"
  lastConfigSync: "2024-01-15T11:55:00Z"
  version: v0.1.0
```

### Remote Cluster Phases

| Phase | Description |
|-------|-------------|
| `Pending` | Remote cluster is pending connection |
| `Connecting` | Connection is being established |
| `Connected` | Remote cluster is connected and healthy |
| `Degraded` | Some agents are unhealthy |
| `Disconnected` | No active connections from remote cluster |
| `Failed` | Remote cluster configuration failed |

---

## NovaEdgeFederation

Configures active-active federation between multiple NovaEdge controllers for multi-datacenter deployments with state synchronization and split-brain protection.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeFederation
metadata:
  name: production-federation
  namespace: novaedge-system
spec:
  # Unique federation identifier
  federationID: prod-fed-01

  # This controller's identity
  localMember:
    name: controller-dc1
    region: us-west
    zone: us-west-2a
    endpoint: controller-dc1.novaedge.example.com:9090

  # Peer controllers
  members:
    - name: controller-dc2
      endpoint: controller-dc2.novaedge.example.com:9090
      region: us-east
      zone: us-east-1a
      tls:
        enabled: true
        caSecretRef:
          name: novaedge-federation-ca
        clientCertSecretRef:
          name: novaedge-federation-client-cert
      priority: 100

  # Synchronization settings
  sync:
    interval: 5s
    timeout: 30s
    batchSize: 100
    compression: true

  # Conflict resolution
  conflictResolution:
    strategy: LastWriterWins  # LastWriterWins, Merge, Manual
    vectorClocks: true
    tombstoneTTL: 24h

  # Health checking
  healthCheck:
    interval: 10s
    timeout: 5s
    failureThreshold: 3
    successThreshold: 1

  # Split-brain detection and protection
  splitBrain:
    enabled: true
    partitionTimeout: 30s
    quorumMode: AgentAssisted  # Controllers or AgentAssisted
    quorumRequired: true
    fencingEnabled: true
    healingGracePeriod: 5s
    autoResolveOnHeal: true
    agentQuorum:
      controllerWeight: 10
      agentWeight: 1
      minAgentsForQuorum: 1
```

### NovaEdgeFederation Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.federationID` | string | Yes | Unique federation identifier |
| `spec.localMember` | object | Yes | This controller's identity |
| `spec.members` | array | No | Peer controller configurations |
| `spec.sync` | object | No | Synchronization settings |
| `spec.conflictResolution` | object | No | Conflict resolution settings |
| `spec.healthCheck` | object | No | Health check configuration |
| `spec.splitBrain` | object | No | Split-brain detection settings |
| `spec.paused` | bool | No | Suspend federation sync |

### Split-Brain Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | true | Enable split-brain detection |
| `partitionTimeout` | duration | 30s | Time before confirming partition |
| `quorumMode` | string | Controllers | `Controllers` or `AgentAssisted` |
| `quorumRequired` | bool | false | Require quorum for writes |
| `fencingEnabled` | bool | false | Block writes during partition |
| `healingGracePeriod` | duration | 5s | Grace period after partition heals |
| `autoResolveOnHeal` | bool | true | Auto-resolve conflicts on heal |

### Agent-Assisted Quorum Configuration

For 2-datacenter deployments, agent-assisted quorum enables split-brain prevention:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `agentQuorum.controllerWeight` | int | 10 | Voting weight per controller |
| `agentQuorum.agentWeight` | int | 1 | Voting weight per agent |
| `agentQuorum.minAgentsForQuorum` | int | 1 | Minimum agents required |

### NovaEdgeFederation Status

```yaml
status:
  phase: Healthy
  observedGeneration: 1
  conditions:
    - type: Ready
      status: "True"
      reason: AllPeersHealthy
    - type: Synced
      status: "True"
  members:
    - name: controller-dc2
      healthy: true
      lastSeen: "2024-01-15T12:00:00Z"
      lastSyncTime: "2024-01-15T11:59:55Z"
      syncLag: 5s
      vectorClock:
        controller-dc1: 150
        controller-dc2: 148
      agentCount: 5
  lastSyncTime: "2024-01-15T11:59:55Z"
  syncLag: 5s
  localVectorClock:
    controller-dc1: 150
    controller-dc2: 148
  conflictsPending: 0
  splitBrain:
    partitionState: Healthy
    haveQuorum: true
    writesFenced: false
    reachablePeers:
      - controller-dc2
    agentQuorumStatus:
      totalAgents: 10
      reachableAgents: 10
      ourVotes: 20
      totalVotes: 30
      quorumThreshold: 16
```

### Federation Phases

| Phase | Description |
|-------|-------------|
| `Initializing` | Federation is starting up |
| `Syncing` | Initial sync in progress |
| `Healthy` | All members healthy and in sync |
| `Degraded` | Some members unhealthy or out of sync |
| `Partitioned` | Network partition detected |

### Partition States

| State | Description |
|-------|-------------|
| `Healthy` | All peers reachable |
| `Suspected` | Some peers not responding |
| `Confirmed` | Partition confirmed, fencing may be active |
| `Healing` | Partition healing, reconciliation in progress |

---

## ProxyVIP

Defines a Virtual IP address for the load balancer. Supports IPv4, IPv6, and dual-stack configurations with optional BFD and IP pool allocation.

```mermaid
flowchart TB
    subgraph VIPModes["VIP Operating Modes"]
        subgraph L2Mode["L2 ARP/NDP Mode"]
            L2["Single active node<br/>GARP (IPv4) / NDP (IPv6)"]
        end
        subgraph BGPMode["BGP Mode"]
            BGP["All nodes announce<br/>ECMP routing<br/>Optional BFD"]
        end
        subgraph OSPFMode["OSPF/OSPFv3 Mode"]
            OSPF["LSA announcements<br/>L3 active-active<br/>Graceful restart"]
        end
    end

    style L2Mode fill:#90EE90
    style BGPMode fill:#FFE4B5
    style OSPFMode fill:#ADD8E6
```

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyVIP
metadata:
  name: my-vip
spec:
  address: 10.200.0.100/32
  ipv6Address: "2001:db8::100/128"
  mode: BGP
  addressFamily: dual
  ports:
    - 80
    - 443
  bgpConfig:
    localAS: 65000
    routerID: "10.0.0.1"
    peers:
      - address: "10.0.0.254"
        as: 65001
  ospfConfig:
    routerID: "10.0.0.1"
    areaID: 0
    cost: 10
    helloInterval: 10
    deadInterval: 40
    authType: md5
    authKey: "mySecretKey"
    gracefulRestart: true
  bfd:
    enabled: true
    detectMultiplier: 3
    desiredMinTxInterval: "300ms"
    requiredMinRxInterval: "300ms"
    echoMode: false
  poolRef:
    name: main-vip-pool
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/loadbalancer: "true"
```

### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.address` | string | No* | IPv4 VIP address with CIDR (e.g., `10.200.0.100/32`) |
| `spec.ipv6Address` | string | No* | IPv6 VIP address with CIDR (e.g., `2001:db8::100/128`) |
| `spec.mode` | string | Yes | VIP mode: `L2ARP`, `BGP`, or `OSPF` |
| `spec.addressFamily` | string | No | Address family: `ipv4` (default), `ipv6`, or `dual` |
| `spec.ports` | []int32 | Yes | Ports to bind on hostNetwork |
| `spec.bgpConfig` | object | BGP only | BGP configuration (see below) |
| `spec.ospfConfig` | object | OSPF only | OSPF configuration (see below) |
| `spec.bfd` | object | No | BFD configuration for sub-second failover |
| `spec.poolRef` | object | No | Reference to a ProxyIPPool for auto-allocation |
| `spec.nodeSelector` | LabelSelector | No | Node selector for VIP placement |
| `spec.healthPolicy` | object | No | Node health requirements |

*At least one of `address`, `ipv6Address`, or `poolRef` must be specified.

### BGP Config Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bgpConfig.localAS` | int32 | Yes | Local BGP AS number (1-4294967295) |
| `bgpConfig.routerID` | string | Yes | BGP router ID |
| `bgpConfig.peers` | []object | Yes | BGP peer list (min 1) |
| `bgpConfig.peers[].address` | string | Yes | Peer IP address (IPv4 or IPv6) |
| `bgpConfig.peers[].as` | int32 | Yes | Peer AS number |
| `bgpConfig.peers[].port` | int | No | Peer BGP port (default: 179) |
| `bgpConfig.communities` | []string | No | BGP communities to attach |
| `bgpConfig.localPreference` | int32 | No | Local preference for iBGP |

### OSPF Config Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ospfConfig.routerID` | string | Yes | OSPF router ID |
| `ospfConfig.areaID` | int32 | No | OSPF area ID (default: 0 = backbone) |
| `ospfConfig.cost` | int32 | No | Route metric cost (default: 10) |
| `ospfConfig.helloInterval` | int32 | No | Hello interval in seconds (default: 10) |
| `ospfConfig.deadInterval` | int32 | No | Dead interval in seconds (default: 40) |
| `ospfConfig.authType` | string | No | Authentication: `none`, `plaintext`, `md5` |
| `ospfConfig.authKey` | string | No | Authentication key |
| `ospfConfig.gracefulRestart` | bool | No | Enable graceful restart (default: false) |

### BFD Config Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bfd.enabled` | bool | No | Enable BFD (default: false) |
| `bfd.detectMultiplier` | int32 | No | Missed packets threshold (default: 3, range: 1-255) |
| `bfd.desiredMinTxInterval` | string | No | Min TX interval (default: "300ms") |
| `bfd.requiredMinRxInterval` | string | No | Min RX interval (default: "300ms") |
| `bfd.echoMode` | bool | No | Enable echo mode (default: false) |

### Pool Ref Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `poolRef.name` | string | Yes | Name of the ProxyIPPool resource |

### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `status.activeNode` | string | Node currently owning the VIP (L2ARP mode) |
| `status.announcingNodes` | []string | Nodes announcing this VIP (BGP/OSPF) |
| `status.allocatedAddress` | string | Address allocated from pool |
| `status.allocatedIpv6Address` | string | IPv6 address allocated from pool |
| `status.bfdSessionState` | string | Current BFD session state |
| `status.observedGeneration` | int64 | Most recent generation observed |
| `status.conditions` | []Condition | Standard Kubernetes conditions |

---

## ProxyIPPool

Defines an IP address pool for automatic VIP address allocation.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyIPPool
metadata:
  name: main-vip-pool
spec:
  cidrs:
    - "10.200.0.0/24"
  addresses:
    - "10.200.1.100/32"
    - "10.200.1.200/32"
  autoAssign: true
```

### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.cidrs` | []string | No* | CIDR ranges for the pool |
| `spec.addresses` | []string | No* | Explicit addresses (CIDR notation) |
| `spec.autoAssign` | bool | No | Allow automatic allocation (default: false) |

*At least one of `cidrs` or `addresses` must be specified.

### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `status.allocated` | int32 | Number of allocated addresses |
| `status.available` | int32 | Number of available addresses |
| `status.allocations` | []IPAllocation | Current allocation details |
| `status.allocations[].address` | string | Allocated IP address |
| `status.allocations[].vipRef` | string | Name of the VIP using this address |
| `status.allocations[].allocatedAt` | Time | When the address was allocated |
| `status.conditions` | []Condition | Standard Kubernetes conditions |

---

## ProxyGateway

Defines listeners and binds them to a VIP.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: my-gateway
spec:
  # Reference to ProxyVIP
  vipRef: my-vip

  listeners:
    - name: http
      port: 80
      protocol: HTTP
      hostnames:
        - "*.example.com"

    - name: https
      port: 443
      protocol: HTTPS
      hostnames:
        - "*.example.com"
      tls:
        secretRef:
          name: my-tls-secret
          namespace: default
        minVersion: "TLS1.2"
        cipherSuites:
          - TLS_AES_128_GCM_SHA256
          - TLS_AES_256_GCM_SHA384

    - name: http3
      port: 443
      protocol: HTTP3
      hostnames:
        - "*.example.com"
      tls:
        secretRef:
          name: my-tls-secret
        minVersion: "TLS1.3"
      quic:
        maxIdleTimeout: "30s"
        maxBiStreams: 100
        enable0RTT: true
```

### L4 Listener Examples

TCP and UDP listeners enable Layer 4 proxying for non-HTTP protocols:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: l4-gateway
spec:
  vipRef: my-vip
  listeners:
    # TCP listener for database traffic
    - name: mysql
      port: 3306
      protocol: TCP

    # UDP listener for DNS traffic
    - name: dns
      port: 53
      protocol: UDP

    # TLS passthrough listener (SNI-based routing)
    - name: tls-passthrough
      port: 8443
      protocol: TLS
      hostnames:
        - "api.example.com"
        - "*.internal.example.com"
```

For L4 proxying details, see [Layer 4 TCP/UDP Proxying](../user-guide/l4-proxying.md).

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.vipRef` | string | Yes | Reference to ProxyVIP name |
| `spec.listeners` | array | Yes | List of listener configurations |
| `spec.listeners[].name` | string | Yes | Unique listener name |
| `spec.listeners[].port` | int32 | Yes | Port number |
| `spec.listeners[].protocol` | string | Yes | Protocol: `HTTP`, `HTTPS`, `HTTP3`, `TCP`, `TLS`, `UDP` |
| `spec.listeners[].hostnames` | array | No | Hostnames to match (wildcards supported) |
| `spec.listeners[].tls` | object | HTTPS/HTTP3 | TLS configuration |
| `spec.listeners[].quic` | object | HTTP3 only | QUIC configuration |

---

## ProxyRoute

Defines routing rules for traffic.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: my-route
spec:
  # Reference to parent gateway(s)
  parentRefs:
    - name: my-gateway
      namespace: default

  # Hostnames to match
  hostnames:
    - "api.example.com"

  # Routing rules
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /api/v1
          headers:
            - name: X-API-Version
              value: v1
          method: GET

      # Backend reference
      backendRef:
        name: api-backend
        weight: 100

      # Request filters
      filters:
        - type: RequestHeaderModifier
          requestHeaderModifier:
            add:
              - name: X-Request-ID
                value: "${request_id}"
            set:
              - name: X-Forwarded-Proto
                value: https
            remove:
              - X-Legacy-Header

        - type: URLRewrite
          urlRewrite:
            path:
              type: ReplacePrefixMatch
              replacePrefixMatch: /v1

      # Response filters
      responseFilters:
        - type: ResponseHeaderModifier
          responseHeaderModifier:
            add:
              - name: X-Served-By
                value: novaedge
            remove:
              - Server

      # Policy references
      policyRefs:
        - name: rate-limit-policy
```

### Match Types

| Path Type | Description | Example |
|-----------|-------------|---------|
| `Exact` | Exact path match | `/api` matches only `/api` |
| `PathPrefix` | Prefix match | `/api` matches `/api`, `/api/v1`, etc. |
| `RegularExpression` | Regex match | `/api/v[0-9]+` |

### Filter Types

| Filter Type | Description |
|-------------|-------------|
| `RequestHeaderModifier` | Add, set, or remove request headers |
| `ResponseHeaderModifier` | Add, set, or remove response headers |
| `URLRewrite` | Rewrite URL path or hostname |
| `RequestRedirect` | Redirect to different URL |

---

## ProxyBackend

Defines backend services and load balancing.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: my-backend
spec:
  # Reference to Kubernetes Service
  serviceRef:
    name: my-service
    port: 8080

  # Or static endpoints
  endpoints:
    - address: 10.0.0.1
      port: 8080
    - address: 10.0.0.2
      port: 8080

  # Load balancing policy
  lbPolicy: RoundRobin  # RoundRobin, P2C, EWMA, LeastConn, RingHash, Maglev

  # Health check configuration
  healthCheck:
    interval: 10s
    timeout: 5s
    healthyThreshold: 2
    unhealthyThreshold: 3
    httpHealthCheck:
      path: /health
      expectedStatuses:
        - 200
        - 204

  # Circuit breaker configuration
  circuitBreaker:
    maxConnections: 1000
    maxPendingRequests: 100
    maxRetries: 3
    consecutiveErrors: 5
    ejectionTime: 30s

  # Session affinity (sticky sessions)
  sessionAffinity:
    type: Cookie           # Cookie, Header, SourceIP
    cookieName: NOVAEDGE_AFFINITY
    cookieTTL: 30m
    cookiePath: /
    secure: true
    sameSite: Lax          # Strict, Lax, None

  # Connection pool configuration
  connectionPool:
    maxIdleConns: 100
    maxIdleConnsPerHost: 10
    idleConnTimeoutMs: 90000
```

### Load Balancing Policies

```mermaid
flowchart LR
    subgraph Algorithms["Load Balancing Algorithms"]
        RR["RoundRobin<br/>Equal distribution"]
        P2C["P2C<br/>Best of 2 random"]
        EWMA["EWMA<br/>Latency-aware"]
        LC["LeastConn<br/>Connection-aware"]
        RH["RingHash<br/>Consistent hashing"]
        MAG["Maglev<br/>Google's algorithm"]
    end

    subgraph UseCase["Best For"]
        UC1["General purpose"]
        UC2["Low latency"]
        UC3["Latency-sensitive"]
        UC4["Session affinity"]
        UC5["High performance"]
    end

    RR --> UC1
    P2C --> UC2
    EWMA --> UC3
    RH --> UC4
    MAG --> UC5

    style Algorithms fill:#e6f3ff
```

| Policy | Description |
|--------|-------------|
| `RoundRobin` | Equal distribution across endpoints |
| `P2C` | Power of Two Choices - pick best of 2 random |
| `EWMA` | Exponentially Weighted Moving Average latency |
| `RingHash` | Consistent hashing for session affinity |
| `Maglev` | Google's Maglev consistent hashing |

---

## ProxyPolicy

Defines policies for rate limiting, security, and more.

```mermaid
flowchart TB
    subgraph PolicyTypes["Available Policy Types"]
        RL["RateLimit<br/>Token bucket<br/>Per-client/global"]
        CORS["CORS<br/>Cross-origin<br/>requests"]
        JWT["JWT Validation<br/>JWKS support<br/>Token verification"]
        IP["IP Filter<br/>Allow/deny lists<br/>CIDR support"]
        SEC["Security Headers<br/>HSTS, CSP<br/>XSS protection"]
    end

    subgraph Targets["Policy Targets"]
        GW["ProxyGateway<br/>(applies to all routes)"]
        RT["ProxyRoute<br/>(specific routes)"]
    end

    RL --> RT
    CORS --> RT
    JWT --> RT
    IP --> GW
    IP --> RT
    SEC --> GW

    style PolicyTypes fill:#DDA0DD
    style Targets fill:#fff5e6
```

### Rate Limiting

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: rate-limit
spec:
  targetRef:
    kind: ProxyRoute
    name: my-route

  rateLimit:
    requestsPerSecond: 100
    burstSize: 150
    key: client_ip  # client_ip, header:<name>, cookie:<name>
```

### CORS

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: cors-policy
spec:
  targetRef:
    kind: ProxyRoute
    name: my-route

  cors:
    allowOrigins:
      - "https://example.com"
      - "https://*.example.com"
    allowMethods:
      - GET
      - POST
      - PUT
      - DELETE
    allowHeaders:
      - Authorization
      - Content-Type
    exposeHeaders:
      - X-Request-ID
    maxAge: 86400
    allowCredentials: true
```

### JWT Validation

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: jwt-policy
spec:
  targetRef:
    kind: ProxyRoute
    name: my-route

  jwt:
    issuer: https://auth.example.com
    audience:
      - api.example.com
    jwksUri: https://auth.example.com/.well-known/jwks.json
    headerName: Authorization
    headerPrefix: "Bearer "
```

### IP Filtering

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: ip-filter
spec:
  targetRef:
    kind: ProxyRoute
    name: my-route

  ipFilter:
    allowList:
      - 10.0.0.0/8
      - 192.168.0.0/16
    denyList:
      - 10.0.0.5/32
```

### Security Headers

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: security-headers
spec:
  targetRef:
    kind: ProxyRoute
    name: my-route

  securityHeaders:
    hsts:
      enabled: true
      maxAgeSeconds: 31536000
      includeSubdomains: true
      preload: true
    xFrameOptions: DENY
    xContentTypeOptions: true
    referrerPolicy: strict-origin-when-cross-origin
    contentSecurityPolicy: "default-src 'self'"
```

---

## Status Conditions

All NovaEdge CRDs include status conditions:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: Valid
      message: Configuration is valid and applied
      lastTransitionTime: "2024-01-15T10:30:00Z"
  observedGeneration: 5
```

### Common Condition Types

| Type | Description |
|------|-------------|
| `Ready` | Resource is ready and configuration applied |
| `Accepted` | Resource was accepted by controller |
| `Programmed` | Configuration pushed to agents |
| `Degraded` | Resource is partially working |

---

## Labels and Annotations

### Common Labels

```yaml
metadata:
  labels:
    app.kubernetes.io/name: my-app
    app.kubernetes.io/component: frontend
    novaedge.io/gateway: my-gateway
```

### Annotations

```yaml
metadata:
  annotations:
    # Custom logging level for this resource
    novaedge.io/log-level: debug

    # Skip validation (use with caution)
    novaedge.io/skip-validation: "true"
```

## CompressionConfig

Response compression configuration, defined on `ProxyGateway.spec.compression`.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | bool | No | `false` | Enable response compression |
| `minSize` | string | No | `"1024"` | Minimum body size (bytes) before compression triggers |
| `level` | int32 | No | `6` | Compression level (gzip: 1-9, brotli: 0-11) |
| `algorithms` | []string | No | `["gzip", "br"]` | Supported compression algorithms in preference order |
| `excludeTypes` | []string | No | `["image/*", ...]` | Content type patterns to skip compression |

## RouteLimits

Per-route request size limits and timeouts, defined on `ProxyRoute.spec.rules[].limits`.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `maxRequestBodySize` | string | No | Gateway default | Maximum request body size (e.g., "10Mi") |
| `requestTimeout` | string | No | No timeout | Total request timeout (e.g., "30s") |
| `idleTimeout` | string | No | No timeout | Connection idle timeout (e.g., "60s") |

## RouteBufferingConfig

Request/response buffering settings, defined on `ProxyRoute.spec.rules[].buffering`.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `request` | bool | No | `false` | Buffer entire request body before forwarding |
| `response` | bool | No | `false` | Buffer entire response body before sending to client |
| `maxSize` | string | No | No limit | Maximum buffer size (e.g., "50Mi") |

## New Fields (v1alpha1)

### ProxyGateway

| Field | Type | Description |
|-------|------|-------------|
| `spec.loadBalancerClass` | string | Load balancer class for multi-controller coexistence. Default: `novaedge.io/proxy` |
| `spec.cache.enabled` | bool | Enable HTTP response caching |
| `spec.cache.maxSize` | string | Maximum cache memory (e.g., `256Mi`) |
| `spec.cache.defaultTTL` | string | Default cache TTL (e.g., `5m`) |
| `spec.cache.maxTTL` | string | Maximum cache TTL (e.g., `1h`) |
| `spec.cache.maxEntrySize` | string | Maximum single entry size (e.g., `1Mi`) |

### ProxyRoute

| Field | Type | Description |
|-------|------|-------------|
| `spec.rules[].mirror.backendRef` | BackendRef | Backend to mirror traffic to |
| `spec.rules[].mirror.percentage` | int (0-100) | Percentage of requests to mirror (default: 100) |

### Controller Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--controller-class` | `novaedge.io/proxy` | loadBalancerClass this controller handles |

---

## RedirectSchemeConfig

Configures automatic HTTP to HTTPS redirection on a ProxyGateway.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable scheme redirection |
| `scheme` | string | `"https"` | Target scheme |
| `port` | int32 | `443` | Target port |
| `statusCode` | int32 | `301` | HTTP redirect status code (301 or 302) |
| `exclusions` | []string | `[]` | Paths to exclude from redirection |

## AccessLogConfig (Enhanced)

Configures access logging on a ProxyGateway or ProxyRoute.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true` | Enable access logging |
| `format` | string | `"json"` | Log format: `json`, `clf`, or `custom` |
| `template` | string | `""` | Custom Go template (for format=custom) |
| `output` | string | `"stdout"` | Output destination: `stdout`, `file`, or `both` |
| `filePath` | string | `""` | Log file path (required for file/both output) |
| `maxSize` | string | `"100Mi"` | Maximum log file size before rotation |
| `maxBackups` | int32 | `5` | Number of rotated files to keep |
| `filterStatusCodes` | []int32 | `[]` | Status codes to log (empty = all) |
| `sampleRate` | float64 | `1.0` | Fraction of requests to log (0.0-1.0) |

## CustomErrorPage

Defines custom error pages on a ProxyGateway.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `codes` | []int32 | required | HTTP status codes this page applies to |
| `path` | string | `""` | Path to serve for these error codes |
| `body` | string | `""` | HTML template body (supports template variables) |
| `contentType` | string | `"text/html"` | Response Content-Type header |

### Template Variables

| Variable | Description |
|----------|-------------|
| `{{.StatusCode}}` | HTTP status code (e.g., 404) |
| `{{.StatusText}}` | HTTP status text (e.g., "Not Found") |
| `{{.RequestID}}` | X-Request-ID header value |
| `{{.Timestamp}}` | UTC timestamp in RFC3339 format |

---

## ClientAuthConfig

Client certificate authentication settings for a listener.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `mode` | string | No | `none` | Client auth mode: `none`, `optional`, `require` |
| `caCertRef` | SecretReference | No | - | Secret containing CA cert for client verification (must have `ca.crt` key) |
| `requiredCNPatterns` | []string | No | - | Regex patterns the client cert CN must match |
| `requiredSANs` | []string | No | - | SANs the client cert must contain |

## ProxyProtocolConfig

PROXY protocol configuration for a listener.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | bool | No | `false` | Enable PROXY protocol parsing |
| `version` | int32 | No | `0` | Version: `0` (both), `1` (v1 only), `2` (v2 only) |
| `trustedCIDRs` | []string | No | `[]` (all) | Source CIDRs from which PROXY headers are accepted |

## UpstreamProxyProtocolConfig

PROXY protocol configuration for backend connections.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | bool | No | `false` | Enable sending PROXY protocol headers to backends |
| `version` | int32 | No | `1` | Version: `1` or `2` |

---

## Gateway API Resources

NovaEdge supports standard Kubernetes Gateway API resources in addition to its custom CRDs.

### GatewayClass

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: novaedge
spec:
  controllerName: novaedge.io/gateway-controller
  description: "NovaEdge Gateway Controller"
```

**Status Conditions:**

| Condition | Description |
|-----------|-------------|
| Accepted | `True` when the controller recognizes the GatewayClass |
| SupportedVersion | `True` when the Gateway API version is supported |

### Gateway

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: example-gateway
  namespace: default
spec:
  gatewayClassName: novaedge
  listeners:
  - name: http
    protocol: HTTP
    port: 80
  - name: https
    protocol: HTTPS
    port: 443
    tls:
      mode: Terminate
      certificateRefs:
      - kind: Secret
        name: tls-secret
```

**Status Conditions:**

| Condition | Description |
|-----------|-------------|
| Accepted | Gateway accepted by the controller |
| Programmed | Gateway ready to accept traffic |

**Listener Status Conditions:**

| Condition | Description |
|-----------|-------------|
| Accepted | Listener configuration is valid |
| Programmed | Listener is ready |
| ResolvedRefs | All TLS references resolved |
| Conflicted | Listener has port/hostname conflicts |

### HTTPRoute

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: example-route
  namespace: default
spec:
  parentRefs:
  - name: example-gateway
  hostnames:
  - "example.com"
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /api
    backendRefs:
    - name: api-service
      port: 8080
```

**Parent Status Conditions:**

| Condition | Description |
|-----------|-------------|
| Accepted | Route accepted by the parent Gateway |
| ResolvedRefs | All backend references resolved |

For detailed Gateway API documentation, see [Gateway API Guide](../advanced/gateway-api.md).
