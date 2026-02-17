# Federation Implementation Design

**Date:** 2026-02-17
**Status:** Approved

## Problem

The federation subsystem has a working control plane (gRPC sync, vector clocks, conflict resolution, split-brain detection) but lacks the data plane: cross-cluster traffic cannot flow, remote endpoints are not merged, and several implemented components are not wired together. The operator cannot apply incoming federated resources to the local Kubernetes API.

## Goals

1. Complete the federation control plane (wire existing code, finish anti-entropy, resource application)
2. Enable cross-cluster data plane traffic via mesh tunnel extension
3. Support three progressive federation modes: Remote Management, Mesh Federation, Unified Cluster
4. Provide optional network tunnels (WireGuard/SSH/WebSocket) for NAT traversal
5. Location-aware routing with Region > Zone > Cluster hierarchy
6. Full documentation, diagrams, examples, Helm chart updates

## Federation Modes

Three progressive modes configured via `NovaEdgeFederation` CRD:

### Remote Management (`hub-spoke`)
- Hub controller pushes ConfigSnapshots to spoke cluster agents
- Spoke agents report health/status back
- CRD resources synced from hub to spokes (one-way)
- Existing functionality: gRPC config stream, RemoteAgentTracker

### Mesh Federation (`mesh`)
- All of Remote Management, plus:
- Bidirectional CRD sync between peers (existing federation sync)
- Cross-cluster endpoint merging: each controller learns remote cluster endpoints via federation sync and includes them in ConfigSnapshots with cluster/region/zone labels
- Agents tunnel cross-cluster traffic via mTLS HTTP/2 CONNECT to remote agents

### Unified Cluster (`unified`)
- All of Mesh Federation, plus:
- All clusters share a single service namespace
- Location-aware routing: Region > Zone > Cluster hierarchy
- Automatic failover: local backends unhealthy -> overflow to remote clusters based on priority/weight from RemoteClusterRouting

## Architecture

### Cross-Cluster Endpoint Merging

1. **Endpoint exchange via federation sync.** Controllers package local endpoint data as `ServiceEndpoints` resource changes (service name, namespace, cluster, region, zone, endpoint list). These flow to peers via the existing `SyncStream`.

2. **New proto message: `ServiceEndpoints`.** Rides inside existing `ResourceChange.resource_data` with `resource_type: "ServiceEndpoints"`.

3. **Snapshot builder merges remote endpoints.** When building a ConfigSnapshot:
   - Resolves local endpoints from EndpointSlices (existing)
   - Queries federation manager for remote endpoints matching the same service
   - Merges into `Cluster.Endpoints` with labels: `novaedge.io/cluster`, `novaedge.io/region`, `novaedge.io/zone`
   - Applies `RemoteClusterRouting` filters (priority, weight, endpoint selector)

4. **Endpoint labels populated.** Fix existing gap: `resolveServiceEndpoints()` populates `Labels` map from EndpointSlice zone field and node topology labels.

### Cross-Cluster Traffic Path

```
Agent (DC-1)                              Agent (DC-2)
┌─────────────┐                          ┌─────────────┐
│ LB selects  │                          │             │
│ remote EP   │──── mTLS HTTP/2 ────────>│ Tunnel      │
│ (dc-2 label)│     CONNECT tunnel       │ Server      │
│             │     (port 15002)         │   │         │
│             │                          │   v         │
│             │                          │ Backend Pod │
└─────────────┘                          └─────────────┘
       │ (optional)                            │ (optional)
       └──── WireGuard/SSH/WebSocket ──────────┘
             (NAT traversal layer)
```

1. Agent receives ConfigSnapshot with remote endpoints marked `novaedge.io/cluster=dc-2`.
2. `CrossClusterTunnelRegistry` maps cluster names to gateway agent tunnel endpoints.
3. When LB selects a remote endpoint, forwarding path detects the foreign cluster label.
4. Instead of dialing directly, sends HTTP/2 CONNECT through the existing mTLS `TunnelPool` to a gateway agent in the remote cluster.
5. Remote agent receives CONNECT, dials the local backend pod, proxies bidirectionally.

### Locality-Aware Routing (Region > Zone > Cluster)

Enhanced `LocalityLB` with three-tier hierarchy:

| Tier | Condition | Behavior |
|------|-----------|----------|
| 1 | Same zone, same cluster | Highest priority, always preferred |
| 2 | Different zone, same region | Used when local zone healthy ratio < threshold |
| 3 | Different region | Last resort, used when same-region healthy ratio < threshold |

`MinHealthyPercent` threshold (default 70%) controls overflow at each level. The existing `PriorityLB` handles cluster-level failover by assigning remote endpoints higher priority values based on `RemoteClusterRouting.Priority`.

### Trust Federation

- Cross-cluster mTLS uses federation TLS credentials (configured per-peer in `NovaEdgeFederation` CRD)
- SPIFFE IDs extended: `spiffe://FEDERATION_ID/cluster/CLUSTER_NAME/agent/NODE`
- Cross-cluster authorization policies can match on cluster identity

### Optional Network Tunnels

For NAT/firewall traversal, configured per `NovaEdgeRemoteCluster` CRD with `spec.connection.mode: Tunnel`:

**WireGuard:**
- Agent creates WireGuard interface (kernel module or `wireguard-go` userspace)
- Private key from `PrivateKeySecretRef`, peer public key and endpoint from CRD
- `AllowedIPs` routes remote cluster CIDRs through WireGuard
- mTLS tunnel connects through WireGuard transparently

**SSH:**
- Agent establishes SSH tunnel (port forwarding) to remote cluster bastion
- Maps local port to remote agent tunnel port (15002)
- mTLS tunnel connects to `localhost:mapped-port`

**WebSocket:**
- For environments where only HTTPS/443 is allowed outbound
- Agent establishes WebSocket to remote cluster gateway
- Upgrades to bidirectional byte stream, mTLS runs over it

Managed by `NetworkTunnelManager` with reconnection, health checks, exponential backoff.

### Anti-Entropy Completion

- Wire `AntiEntropyManager` into `Manager.Start()`
- Complete `compareWithPeer()`: request peer merkle root, compare, drill down to divergent subtrees, push/pull specific resources
- Complete `findNode()` tree traversal
- `GetDriftReports()` returns cached results
- Runs on configurable interval (default 5 min) as consistency safety net

### Resource Application

When federated resource changes arrive from peers:
1. Determine resource type from `ResourceChange.resource_type`
2. NovaEdge CRDs (ProxyGateway, ProxyRoute, ProxyBackend, ProxyPolicy, ProxyVIP): unmarshal, create/update/delete via K8s API with `novaedge.io/federation-origin` label
3. ConfigMaps/Secrets: same approach with namespace mapping
4. `ServiceEndpoints`: store in federation manager endpoint cache (consumed by snapshot builder, not written to K8s)
5. Origin labels prevent sync loops: controller skips reconciling federation-originated resources

### Config Hot Reload

When `NovaEdgeFederation` CRD spec changes:
- Reconciler detects `spec.generation != status.observedGeneration`
- Peer changes: add new `PeerClient`s, disconnect removed peers, update TLS
- Interval/threshold changes: update in-place
- Federation ID change: full manager restart

### Wiring Existing Code

- `SplitBrainDetector`: created in `Manager.Start()`, fed peer health data
- `SnapshotEnhancer`: called from snapshot builder when federation active
- `MetricsCollector`: periodic goroutine in Manager, registered with Prometheus
- Remove dead `Server.connectToPeer()` stub

### Remote Cluster Cleanup

On `NovaEdgeRemoteCluster` deletion:
- Unregister from registry (existing)
- Delete resources labeled `novaedge.io/federation-origin=CLUSTER_NAME`
- Tear down network tunnel if active

## Documentation Requirements

| Area | Updates Required |
|------|-----------------|
| Architecture docs | Federation architecture diagrams, cross-cluster traffic flow, tunnel topology |
| User guide | Federation setup guide, multi-cluster deployment walkthrough, location-aware routing config |
| CRD reference | Updated field descriptions for NovaEdgeFederation, NovaEdgeRemoteCluster |
| Examples | `config/samples/` - federation CRD examples for all 3 modes |
| Helm charts | Federation values, tunnel config, cross-cluster TLS secret templates |
| Operator docs | Operator lifecycle for federation, hot reload behavior |
| mkdocs.yml | New nav entries for federation pages |
| Diagrams | Mermaid diagrams for architecture, endpoint merging, tunnel connectivity, anti-entropy |

## Trade-offs

- **Pro:** Three progressive modes let users adopt incrementally
- **Pro:** Builds on existing mesh tunnel infrastructure (no new transport protocol)
- **Pro:** Automatic endpoint merging requires zero app-side changes
- **Pro:** Location-aware routing handles multi-DC transparently
- **Con:** WireGuard/SSH/WebSocket adds optional dependencies
- **Con:** Significant implementation scope (~18 tasks)
- **Con:** Cross-cluster mTLS adds latency (acceptable for geo-distributed workloads)

## Files Changed

### New Files
- `internal/agent/tunnel/manager.go` - NetworkTunnelManager (WireGuard/SSH/WebSocket)
- `internal/agent/tunnel/wireguard.go` - WireGuard tunnel implementation
- `internal/agent/tunnel/ssh.go` - SSH tunnel implementation
- `internal/agent/tunnel/websocket.go` - WebSocket tunnel implementation
- `internal/agent/router/crosscluster.go` - CrossClusterTunnelRegistry, remote endpoint detection
- `docs/user-guide/federation.md` - Federation user guide
- `docs/user-guide/cross-cluster-routing.md` - Cross-cluster routing guide
- `config/samples/federation/` - Example CRDs for all 3 modes

### Modified Files
- `api/proto/config.proto` - ServiceEndpoints message
- `internal/controller/snapshot/builder.go` - Endpoint label population, remote endpoint merging
- `internal/controller/federation/manager.go` - Wire split-brain, metrics, anti-entropy
- `internal/controller/federation/antientropy.go` - Complete peer comparison
- `internal/controller/federation/server.go` - Remove connectToPeer, handle ServiceEndpoints
- `internal/controller/federation/snapshot_integration.go` - Wire into builder
- `internal/agent/lb/locality.go` - Region > Zone > Cluster hierarchy
- `internal/agent/router/forwarding.go` - Cross-cluster tunnel routing
- `internal/agent/mesh/tls.go` - Cross-cluster SPIFFE IDs
- `internal/agent/mesh/tunnel.go` - Cross-cluster tunnel connections
- `internal/operator/controller/novaedgefederation_controller.go` - Resource application, hot reload
- `internal/operator/controller/novaedgeremotecluster_controller.go` - Cleanup implementation
- `charts/novaedge/` - Federation Helm values
- `mkdocs.yml` - New nav entries
