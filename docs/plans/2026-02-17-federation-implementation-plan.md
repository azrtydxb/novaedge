# Federation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete the federation subsystem with three progressive modes (hub-spoke, mesh, unified), cross-cluster data plane traffic via mesh tunnel extension, optional WireGuard/SSH/WebSocket tunnels, and Region > Zone > Cluster location-aware routing.

**Architecture:** Build in layers: (1) wire existing unconnected code, (2) complete anti-entropy and resource application, (3) add cross-cluster endpoint merging and data plane tunneling, (4) enhance locality-aware routing, (5) add optional network tunnels, (6) update all documentation. Each layer builds on the previous. The federation sync protocol already works -- the main gap is the data plane.

**Tech Stack:** Go, gRPC/protobuf, Kubernetes controller-runtime, HTTP/2 CONNECT tunnels, mTLS/SPIFFE, WireGuard (`wireguard-go`), Prometheus metrics, Helm charts, mkdocs

---

### Task 1: Create GitHub Issue and Worktree

**Step 1: Create the issue**

The issue already exists: #398. Use it.

**Step 2: Create worktree**

Run:
```bash
git worktree add ../novaedge-worktrees/issue-398-federation-implementation -b issue-398-federation-implementation origin/main
cd ../novaedge-worktrees/issue-398-federation-implementation
git config user.name "Pascal Watteel"
git config user.email "pascal@watteel.com"
```

---

### Task 2: Wire Split-Brain Detector into Manager

**Files:**
- Modify: `internal/controller/federation/manager.go` (lines 33-50, 185-230)
- Modify: `internal/controller/federation/splitbrain.go` (if adapter code needed)
- Test: `internal/controller/federation/manager_test.go`

**Step 1: Read the existing code**

Read `manager.go` `Start()` method (line 185) and `splitbrain.go` `NewSplitBrainDetector()` constructor to understand the interfaces.

**Step 2: Add SplitBrainDetector field to Manager**

In `manager.go`, add to the `Manager` struct:
```go
splitBrain *SplitBrainDetector
```

**Step 3: Create and start detector in Manager.Start()**

After the server and peer clients are created (around line 228), add:
```go
if m.config.SplitBrain.Enabled {
    m.splitBrain = NewSplitBrainDetector(SplitBrainConfig{
        PartitionTimeout:    m.config.SplitBrain.PartitionTimeout,
        QuorumMode:          m.config.SplitBrain.QuorumMode,
        TotalControllers:    len(m.config.Peers) + 1,
        QuorumRequired:      m.config.SplitBrain.QuorumRequired,
        FencingEnabled:      m.config.SplitBrain.FencingEnabled,
        HealingGracePeriod:  m.config.SplitBrain.HealingGracePeriod,
        AutoResolveOnHeal:   m.config.SplitBrain.AutoResolveOnHeal,
    }, m.logger)
    m.splitBrain.Start(derivedCtx)
}
```

**Step 4: Feed peer health data to detector**

In `runHealthChecker()` (line 428), after each `Ping()` result, call:
```go
if m.splitBrain != nil {
    if err != nil {
        m.splitBrain.RecordPeerUnreachable(peerName)
    } else {
        m.splitBrain.RecordPeerReachable(peerName)
    }
}
```

**Step 5: Expose fencing status**

Add method to Manager:
```go
func (m *Manager) AreWritesFenced() bool {
    if m.splitBrain == nil {
        return false
    }
    return m.splitBrain.AreWritesFenced()
}
```

**Step 6: Test, format, build**

Run:
```bash
go test ./internal/controller/federation/... -v
gofmt -s -w internal/controller/federation/manager.go
go build ./cmd/novaedge-controller
```

**Step 7: Commit**
```bash
git add internal/controller/federation/manager.go
git commit -m "[Feature] Wire split-brain detector into federation manager

Create and start SplitBrainDetector in Manager.Start() when enabled.
Feed peer health check results to the detector. Expose AreWritesFenced()
for downstream consumers.

Resolves #398"
```

---

### Task 3: Wire Snapshot Enhancer and Metrics Collector

**Files:**
- Modify: `internal/controller/snapshot/builder.go` (line 177, BuildSnapshot)
- Modify: `internal/controller/federation/manager.go`
- Modify: `internal/controller/federation/metrics.go`

**Step 1: Add federation manager reference to snapshot Builder**

In `builder.go`, the `Builder` struct needs a way to access the federation manager. Add:
```go
type Builder struct {
    // ... existing fields
    federationManager FederationStateProvider // interface for federation metadata
}
```

Define the interface in a new file or in builder.go:
```go
type FederationStateProvider interface {
    GetFederationID() string
    GetLocalMemberName() string
    GetVectorClock() map[string]int64
    IsActive() bool
}
```

**Step 2: Call SnapshotEnhancer in BuildSnapshot()**

After the snapshot is assembled (around line 251 in `BuildSnapshot()`), before returning:
```go
if b.federationManager != nil && b.federationManager.IsActive() {
    enhancer := federation.NewSnapshotEnhancer(
        b.federationManager.GetFederationID(),
        b.federationManager.GetLocalMemberName(),
        b.federationManager.GetVectorClock(),
    )
    enhancer.EnhanceSnapshot(snapshot)
}
```

**Step 3: Implement FederationStateProvider on Manager**

In `manager.go`, add the interface methods:
```go
func (m *Manager) GetFederationID() string { return m.config.FederationID }
func (m *Manager) GetLocalMemberName() string { return m.config.LocalMember.Name }
func (m *Manager) GetVectorClock() map[string]int64 { return m.server.vectorClock.ToMap() }
func (m *Manager) IsActive() bool { m.mu.RLock(); defer m.mu.RUnlock(); return m.started }
```

**Step 4: Start metrics collector in Manager.Start()**

In `manager.go` `Start()`, after the server starts:
```go
metricsCollector := NewMetricsCollector(m)
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-derivedCtx.Done():
            return
        case <-ticker.C:
            metricsCollector.Collect()
        }
    }
}()
```

**Step 5: Test, format, build, commit**

Run:
```bash
go test ./internal/controller/snapshot/... -v
go test ./internal/controller/federation/... -v
gofmt -s -w internal/controller/snapshot/builder.go internal/controller/federation/manager.go
go build ./cmd/novaedge-controller
git add internal/controller/snapshot/builder.go internal/controller/federation/manager.go internal/controller/federation/metrics.go
git commit -m "[Feature] Wire snapshot enhancer and metrics collector

Populate FederationMetadata in ConfigSnapshots when federation is active.
Start periodic metrics collection in federation manager.

Resolves #398"
```

---

### Task 4: Remove Dead connectToPeer and Populate Endpoint Labels

**Files:**
- Modify: `internal/controller/federation/server.go` (lines ~1018-1025)
- Modify: `internal/controller/snapshot/builder.go` (resolveServiceEndpoints, line 940)

**Step 1: Remove connectToPeer from server.go**

Delete the `connectToPeer` method entirely. Search for any callers -- if any exist, they need to be updated to use the Manager's PeerClient path instead.

**Step 2: Populate endpoint labels in resolveServiceEndpoints**

In `builder.go` `resolveServiceEndpoints()`, when creating `pb.Endpoint` objects (around line 1005):

```go
ep := &pb.Endpoint{
    Address: addr,
    Port:    matchedPort,
    Ready:   *endpoint.Conditions.Ready,
    Labels:  make(map[string]string),
}
// Add zone from EndpointSlice
if epSlice.Labels["topology.kubernetes.io/zone"] != "" {
    ep.Labels["topology.kubernetes.io/zone"] = epSlice.Labels["topology.kubernetes.io/zone"]
}
```

Also look up the node to get region:
```go
if endpoint.NodeName != nil {
    node := &corev1.Node{}
    if err := b.client.Get(ctx, types.NamespacedName{Name: *endpoint.NodeName}, node); err == nil {
        if region, ok := node.Labels["topology.kubernetes.io/region"]; ok {
            ep.Labels["topology.kubernetes.io/region"] = region
        }
    }
}
```

Cache node lookups to avoid repeated API calls for the same node.

**Step 3: Test, format, build, commit**

Run:
```bash
go test ./internal/controller/snapshot/... -v
go test ./internal/controller/federation/... -v
gofmt -s -w internal/controller/federation/server.go internal/controller/snapshot/builder.go
go build ./cmd/novaedge-controller
git add internal/controller/federation/server.go internal/controller/snapshot/builder.go
git commit -m "[Feature] Remove dead connectToPeer, populate endpoint labels

Remove unused Server.connectToPeer() stub. Populate endpoint labels
with topology.kubernetes.io/zone and topology.kubernetes.io/region
from EndpointSlice and Node objects for locality-aware routing.

Resolves #398"
```

---

### Task 5: Complete Anti-Entropy Peer Comparison

**Files:**
- Modify: `internal/controller/federation/antientropy.go` (lines 439-555)
- Modify: `internal/controller/federation/manager.go` (Start method)
- Modify: `internal/controller/federation/client.go` (if new RPC methods needed)
- Test: `internal/controller/federation/antientropy_test.go`

**Step 1: Wire AntiEntropyManager into Manager.Start()**

In `manager.go` `Start()`, after server and health checker:
```go
m.antiEntropy = NewAntiEntropyManager(m.server, m.config.AntiEntropyInterval, m.logger)
m.antiEntropy.Start(derivedCtx)
```

Add field to Manager struct: `antiEntropy *AntiEntropyManager`

**Step 2: Complete compareWithPeer() - Concurrent case**

When vector clocks are concurrent (case 0 in the switch at line 483):
```go
case 0: // Concurrent - use merkle tree comparison
    m.rebuildLocalTree()
    localRoot := m.tree.GetRoot()
    // Request peer's resource hashes via full sync
    peerClient := m.getPeerClient(peerName)
    if peerClient == nil {
        m.logger.Warn("No client for peer", zap.String("peer", peerName))
        return
    }
    peerResources, err := peerClient.RequestFullSync(m.server.config.FederationID, m.server.config.LocalMember.Name, nil, nil)
    if err != nil {
        m.logger.Error("Failed to request peer resources for anti-entropy", zap.Error(err))
        return
    }
    // Compare and apply missing/divergent resources
    m.reconcileWithPeerResources(peerName, peerResources)
```

**Step 3: Complete compareWithPeer() - We're ahead case**

When we're ahead (case 1): push our changes to peer. Use the existing `PeerClient.SendChange()` to push resources the peer is missing based on vector clock comparison.

**Step 4: Complete compareWithPeer() - Peer ahead case**

When peer is ahead (case -1): request full sync from peer and apply:
```go
peerResources, err := peerClient.RequestFullSync(...)
if err != nil { return }
m.reconcileWithPeerResources(peerName, peerResources)
```

**Step 5: Implement reconcileWithPeerResources()**

New method that compares peer resources with local resources and applies differences:
```go
func (m *AntiEntropyManager) reconcileWithPeerResources(peerName string, peerResources []*pb.ResourceChange) {
    for _, res := range peerResources {
        localRes, exists := m.server.resources.Load(ResourceKey{Type: res.ResourceType, Namespace: res.Namespace, Name: res.Name})
        if !exists || localRes.Version < res.ResourceVersion {
            m.server.applyResourceChange(res)
        }
    }
}
```

**Step 6: Implement GetDriftReports()**

Cache drift reports from each comparison cycle, return them from `GetDriftReports()`.

**Step 7: Add getPeerClient() helper to AntiEntropyManager**

The `AntiEntropyManager` needs access to peer clients. Pass the Manager's client map or a lookup function during construction.

**Step 8: Test, format, build, commit**

Run:
```bash
go test ./internal/controller/federation/... -v -run TestAntiEntropy
gofmt -s -w internal/controller/federation/antientropy.go internal/controller/federation/manager.go
go build ./cmd/novaedge-controller
git add internal/controller/federation/antientropy.go internal/controller/federation/manager.go
git commit -m "[Feature] Complete anti-entropy peer comparison with merkle tree

Wire AntiEntropyManager into federation Manager. Complete
compareWithPeer() for all three cases: concurrent (full merkle
comparison), ahead (push to peer), behind (pull from peer).
Implement resource reconciliation and drift reporting.

Resolves #398"
```

---

### Task 6: Implement Resource Application (OnResourceChange)

**Files:**
- Modify: `internal/operator/controller/novaedgefederation_controller.go` (line ~188)
- Create: `internal/operator/controller/federation_applier.go`
- Test: `internal/operator/controller/federation_applier_test.go`

**Step 1: Create FederationResourceApplier**

New file `federation_applier.go`:
```go
type FederationResourceApplier struct {
    client    client.Client
    scheme    *runtime.Scheme
    logger    *zap.Logger
}

func NewFederationResourceApplier(c client.Client, scheme *runtime.Scheme, logger *zap.Logger) *FederationResourceApplier

func (a *FederationResourceApplier) Apply(ctx context.Context, key federation.ResourceKey, changeType federation.ChangeType, data []byte) error
```

**Step 2: Implement Apply() for CRD types**

Based on `key.Type`, unmarshal `data` and create/update/delete:
- `"ProxyGateway"` -> unmarshal to `novaedgev1alpha1.ProxyGateway`, apply with `novaedge.io/federation-origin` label
- `"ProxyRoute"` -> same pattern
- `"ProxyBackend"` -> same pattern
- `"ProxyPolicy"` -> same pattern
- `"ProxyVIP"` -> same pattern
- `"ConfigMap"` -> unmarshal to `corev1.ConfigMap`, apply with origin label
- `"Secret"` -> unmarshal to `corev1.Secret`, apply with origin label
- `"ServiceEndpoints"` -> store in manager's endpoint cache (do NOT write to K8s API)

For create/update, use `controllerutil.CreateOrUpdate()` with the origin label. For delete, delete the object if it has the federation-origin label (refuse to delete resources not originated from federation).

**Step 3: Add origin label to prevent sync loops**

All resources created by the applier get:
```go
labels["novaedge.io/federation-origin"] = originMember
```

The federation controller's reconcile watches should skip resources with this label to prevent infinite sync loops.

**Step 4: Wire into operator controller**

In `novaedgefederation_controller.go`, replace the TODO warning at line ~188:
```go
applier := NewFederationResourceApplier(r.Client, r.Scheme, logger)
manager.OnResourceChange = func(key federation.ResourceKey, changeType federation.ChangeType, data []byte) {
    if err := applier.Apply(ctx, key, changeType, data); err != nil {
        logger.Error("Failed to apply federated resource", zap.Error(err))
    }
}
```

**Step 5: Write tests**

Test `Apply()` with mock client for each resource type (create, update, delete). Test origin label is set. Test that delete refuses to remove non-federated resources.

**Step 6: Format, build, commit**

```bash
gofmt -s -w internal/operator/controller/federation_applier.go
go test ./internal/operator/controller/... -v
go build ./cmd/novaedge-operator
git add internal/operator/controller/federation_applier.go internal/operator/controller/federation_applier_test.go internal/operator/controller/novaedgefederation_controller.go
git commit -m "[Feature] Implement federation resource application

Create FederationResourceApplier that writes incoming federated
resources (CRDs, ConfigMaps, Secrets) to the local Kubernetes API.
Resources are labeled with novaedge.io/federation-origin to prevent
sync loops. ServiceEndpoints are cached in memory for snapshot builder.

Resolves #398"
```

---

### Task 7: Config Hot Reload

**Files:**
- Modify: `internal/operator/controller/novaedgefederation_controller.go` (line ~152)
- Modify: `internal/controller/federation/manager.go`

**Step 1: Add config comparison to reconciler**

In the reconciler, when the manager already exists (line ~152), compare the current CRD spec with the running config:
```go
newConfig := crdToConfig(fed)
if !m.config.Equal(newConfig) {
    m.UpdateConfig(newConfig)
}
```

**Step 2: Implement Config.Equal() on federation Config**

Compare all fields. Return false if any differ.

**Step 3: Implement Manager.UpdateConfig()**

```go
func (m *Manager) UpdateConfig(newConfig *Config) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    // Update simple config values in-place
    m.server.UpdateSyncConfig(newConfig.SyncInterval, newConfig.SyncTimeout, newConfig.BatchSize)

    // Handle peer changes
    oldPeers := m.config.PeerSet()
    newPeers := newConfig.PeerSet()

    // Add new peers
    for name, peer := range newPeers {
        if _, exists := oldPeers[name]; !exists {
            client := NewPeerClient(peer, ...)
            m.clients[name] = client
            go m.maintainPeerConnection(name, client)
        }
    }

    // Remove old peers
    for name := range oldPeers {
        if _, exists := newPeers[name]; !exists {
            if c, ok := m.clients[name]; ok {
                c.Disconnect()
                delete(m.clients, name)
            }
        }
    }

    // Update TLS for changed peers
    for name, newPeer := range newPeers {
        if oldPeer, exists := oldPeers[name]; exists && !oldPeer.TLSEqual(newPeer) {
            m.clients[name].UpdateTLS(newPeer.TLS)
        }
    }

    m.config = newConfig
    return nil
}
```

**Step 4: Test, format, build, commit**

```bash
go test ./internal/controller/federation/... -v
go test ./internal/operator/controller/... -v
gofmt -s -w internal/controller/federation/manager.go internal/operator/controller/novaedgefederation_controller.go
go build ./cmd/novaedge-operator && go build ./cmd/novaedge-controller
git add internal/controller/federation/manager.go internal/operator/controller/novaedgefederation_controller.go
git commit -m "[Feature] Implement federation config hot reload

Detect NovaEdgeFederation CRD spec changes and update the running
manager in-place. Peers are added/removed dynamically, TLS credentials
are updated, and sync intervals are adjusted without restart.

Resolves #398"
```

---

### Task 8: ServiceEndpoints Sync via Federation

**Files:**
- Modify: `api/proto/config.proto` (add ServiceEndpoints message)
- Run: `make generate-proto`
- Modify: `internal/controller/federation/server.go` (handle ServiceEndpoints resource type)
- Modify: `internal/controller/federation/manager.go` (endpoint cache + publish)
- Create: `internal/controller/federation/endpoints.go` (endpoint cache)
- Modify: `internal/controller/snapshot/builder.go` (publish local endpoints)

**Step 1: Add ServiceEndpoints proto message**

In `api/proto/config.proto`, add:
```protobuf
message ServiceEndpoints {
  string service_name = 1;
  string namespace = 2;
  string cluster_name = 3;
  string region = 4;
  string zone = 5;
  repeated Endpoint endpoints = 6;
}
```

Run `make generate-proto` to regenerate Go code.

**Step 2: Create endpoint cache**

New file `internal/controller/federation/endpoints.go`:
```go
type RemoteEndpointCache struct {
    mu        sync.RWMutex
    endpoints map[string]map[string]*pb.ServiceEndpoints // cluster -> "namespace/service" -> endpoints
}

func NewRemoteEndpointCache() *RemoteEndpointCache
func (c *RemoteEndpointCache) Update(cluster string, endpoints *pb.ServiceEndpoints)
func (c *RemoteEndpointCache) Delete(cluster, namespace, serviceName string)
func (c *RemoteEndpointCache) GetForService(namespace, serviceName string) []*pb.ServiceEndpoints
func (c *RemoteEndpointCache) GetAll() map[string][]*pb.ServiceEndpoints
```

**Step 3: Handle ServiceEndpoints in federation server**

When `ResourceChange.resource_type == "ServiceEndpoints"`, unmarshal `resource_data` as `pb.ServiceEndpoints` and store in the `RemoteEndpointCache`.

**Step 4: Publish local endpoints to federation peers**

In the snapshot builder or a separate goroutine, when local endpoints change, publish them as `ResourceChange` messages with `resource_type: "ServiceEndpoints"` to the federation manager's `RecordChange()`.

**Step 5: Expose cache via Manager interface**

Add to the `FederationStateProvider` interface:
```go
GetRemoteEndpoints(namespace, serviceName string) []*pb.ServiceEndpoints
```

**Step 6: Test, format, build, commit**

```bash
make generate-proto
go test ./internal/controller/federation/... -v
gofmt -s -w internal/controller/federation/endpoints.go internal/controller/federation/server.go internal/controller/federation/manager.go
go build ./cmd/novaedge-controller
git add api/proto/config.proto internal/proto/gen/ internal/controller/federation/endpoints.go internal/controller/federation/server.go internal/controller/federation/manager.go
git commit -m "[Feature] Add ServiceEndpoints sync via federation

Add ServiceEndpoints proto message for cross-cluster endpoint exchange.
Create RemoteEndpointCache to store endpoints from remote clusters.
Publish local endpoints to federation peers as resource changes.

Resolves #398"
```

---

### Task 9: Cross-Cluster Endpoint Merging in Snapshot Builder

**Files:**
- Modify: `internal/controller/snapshot/builder.go` (buildClusters, line 564)

**Step 1: Merge remote endpoints in buildClusters()**

After resolving local endpoints for each cluster (line 649), query the federation manager for remote endpoints matching the same service:

```go
if b.federationManager != nil && b.federationManager.IsActive() {
    remoteEPs := b.federationManager.GetRemoteEndpoints(backend.Namespace, backend.Spec.ServiceRef.Name)
    for _, reps := range remoteEPs {
        for _, ep := range reps.Endpoints {
            // Add cluster/region/zone labels
            if ep.Labels == nil {
                ep.Labels = make(map[string]string)
            }
            ep.Labels["novaedge.io/cluster"] = reps.ClusterName
            ep.Labels["novaedge.io/region"] = reps.Region
            ep.Labels["novaedge.io/zone"] = reps.Zone
            ep.Labels["novaedge.io/remote"] = "true"
            endpoints = append(endpoints, ep)
        }
    }
}
```

**Step 2: Apply RemoteClusterRouting filters**

If `RemoteClusterRouting` is configured for a cluster, apply priority/weight/endpoint selector filtering before merging.

**Step 3: Test with mock federation manager**

Write tests that verify:
- Local-only endpoints when federation is inactive
- Merged endpoints when federation provides remote endpoints
- Labels are correctly set on remote endpoints
- Routing filters are applied

**Step 4: Format, build, commit**

```bash
gofmt -s -w internal/controller/snapshot/builder.go
go test ./internal/controller/snapshot/... -v
go build ./cmd/novaedge-controller
git add internal/controller/snapshot/builder.go
git commit -m "[Feature] Merge remote cluster endpoints into ConfigSnapshot

Query federation manager for remote endpoints matching local services.
Merge into ConfigSnapshot with cluster/region/zone labels. Apply
RemoteClusterRouting filters for priority and endpoint selection.

Resolves #398"
```

---

### Task 10: Enhance Locality-Aware LB (Region > Zone > Cluster)

**Files:**
- Modify: `internal/agent/lb/locality.go`
- Test: `internal/agent/lb/locality_test.go`

**Step 1: Extend LocalityConfig**

```go
type LocalityConfig struct {
    Enabled           bool
    LocalZone         string
    LocalRegion       string
    LocalCluster      string
    MinHealthyPercent float64
}
```

**Step 2: Implement three-tier selection in Select()**

```go
func (l *LocalityLB) Select() *pb.Endpoint {
    // Tier 1: Same zone, same cluster
    if localZoneEPs healthy >= threshold -> use localZoneLB

    // Tier 2: Same region (any zone, any cluster)
    if sameRegionEPs healthy >= threshold -> use sameRegionLB

    // Tier 3: All endpoints (cross-region)
    return allLB.Select()
}
```

**Step 3: Update UpdateEndpoints() to group by region and cluster**

Extend the grouping logic to maintain three endpoint sets: local-zone, same-region, all.

**Step 4: Write comprehensive tests**

Test cases:
- All local: stays in zone
- Local unhealthy: overflows to same-region
- Region unhealthy: overflows to cross-region
- Mixed clusters: prefers local cluster within same zone
- No remote endpoints: works exactly as before (backward compatible)

**Step 5: Format, build, commit**

```bash
gofmt -s -w internal/agent/lb/locality.go
go test ./internal/agent/lb/... -v
go build ./cmd/novaedge-agent
git add internal/agent/lb/locality.go internal/agent/lb/locality_test.go
git commit -m "[Feature] Enhance locality LB with Region > Zone > Cluster hierarchy

Extend LocalityLB from zone-only to three-tier selection:
same-zone > same-region > cross-region. Each tier overflows to the
next when healthy endpoint ratio drops below MinHealthyPercent.

Resolves #398"
```

---

### Task 11: Cross-Cluster Tunnel Registry and Forwarding

**Files:**
- Create: `internal/agent/router/crosscluster.go`
- Modify: `internal/agent/router/forwarding.go` (line ~188-196)
- Modify: `internal/agent/router/router.go` (routerState struct)
- Test: `internal/agent/router/crosscluster_test.go`

**Step 1: Create CrossClusterTunnelRegistry**

```go
type CrossClusterTunnelRegistry struct {
    mu       sync.RWMutex
    gateways map[string][]GatewayAgent // clusterName -> gateway agents
}

type GatewayAgent struct {
    Address   string // tunnel endpoint address (host:port)
    Cluster   string
    Region    string
    Zone      string
    Healthy   bool
    Latency   time.Duration
}

func (r *CrossClusterTunnelRegistry) GetGateway(clusterName string) (*GatewayAgent, error)
func (r *CrossClusterTunnelRegistry) UpdateGateways(clusterName string, gateways []GatewayAgent)
func (r *CrossClusterTunnelRegistry) IsRemoteEndpoint(ep *pb.Endpoint) bool
```

**Step 2: Add tunnel registry to routerState**

In `router.go`, add to the `routerState` struct or the `Router` struct:
```go
tunnelRegistry *CrossClusterTunnelRegistry
tunnelPool     *mesh.TunnelPool // reuse mesh tunnel pool for cross-cluster
```

**Step 3: Intercept remote endpoints in forwardToBackend()**

In `forwarding.go`, between `selectEndpoint()` and `executeForward()` (around line 188-196):

```go
endpoint, lbType := r.selectEndpoint(ctx, snap, clusterKey, req, w, backendSpan, detailed, tracer)
if endpoint == nil {
    return
}

// Check if this is a cross-cluster endpoint
if r.tunnelRegistry != nil && r.tunnelRegistry.IsRemoteEndpoint(endpoint) {
    r.forwardViaTunnel(ctx, snap, entry, endpoint, w, req, backendSpan)
    return
}

r.executeForward(ctx, snap, entry, pool, endpoint, clusterKey, isGRPC, backendSpan, w, req)
```

**Step 4: Implement forwardViaTunnel()**

```go
func (r *Router) forwardViaTunnel(ctx context.Context, snap *routerState, entry *RouteEntry, endpoint *pb.Endpoint, w http.ResponseWriter, req *http.Request, span trace.Span) {
    clusterName := endpoint.GetLabels()["novaedge.io/cluster"]
    gateway, err := r.tunnelRegistry.GetGateway(clusterName)
    if err != nil {
        http.Error(w, "No gateway for remote cluster", http.StatusBadGateway)
        return
    }

    backendAddr := fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)
    conn, err := r.tunnelPool.DialVia(ctx, gateway.Address, backendAddr, r.localCluster, "")
    if err != nil {
        http.Error(w, "Failed to tunnel to remote cluster", http.StatusBadGateway)
        return
    }
    defer conn.Close()

    // Proxy the request through the tunnel connection
    // Use httputil.ReverseProxy or manual request/response copying
}
```

**Step 5: Write tests**

Test `IsRemoteEndpoint()`, `GetGateway()`, and the tunnel forwarding path with a mock tunnel server.

**Step 6: Format, build, commit**

```bash
gofmt -s -w internal/agent/router/crosscluster.go internal/agent/router/forwarding.go
go test ./internal/agent/router/... -v
go build ./cmd/novaedge-agent
git add internal/agent/router/crosscluster.go internal/agent/router/crosscluster_test.go internal/agent/router/forwarding.go internal/agent/router/router.go
git commit -m "[Feature] Add cross-cluster tunnel registry and forwarding

Create CrossClusterTunnelRegistry to map remote clusters to gateway
agents. Intercept remote endpoint selection in forwardToBackend() and
route through mTLS HTTP/2 CONNECT tunnel to the remote cluster agent.

Resolves #398"
```

---

### Task 12: Cross-Cluster SPIFFE Identity

**Files:**
- Modify: `internal/agent/mesh/cert.go`
- Modify: `internal/agent/mesh/tls.go`
- Modify: `internal/agent/mesh/tunnel.go` (authorization)
- Test: `internal/agent/mesh/cert_test.go`

**Step 1: Extend SPIFFE ID format**

Current: `spiffe://cluster.local/agent/NODE`
New: `spiffe://FEDERATION_ID/cluster/CLUSTER_NAME/agent/NODE`

In `cert.go`, update the CSR generation to use the federation-aware SPIFFE ID when federation is active.

**Step 2: Update tunnel authorization**

In `tunnel.go` `handleConnect()`, the SPIFFE ID verification needs to accept cross-cluster identities. Parse the SPIFFE ID to extract cluster name and validate it against known federated clusters.

**Step 3: Update TLS config**

In `tls.go`, ensure the TLS verifier accepts certificates with cross-cluster SPIFFE IDs when federation is active.

**Step 4: Test, format, build, commit**

```bash
go test ./internal/agent/mesh/... -v
gofmt -s -w internal/agent/mesh/cert.go internal/agent/mesh/tls.go internal/agent/mesh/tunnel.go
go build ./cmd/novaedge-agent
git add internal/agent/mesh/
git commit -m "[Feature] Extend SPIFFE identity for cross-cluster federation

Update SPIFFE ID format to include federation ID and cluster name.
Update tunnel authorization to accept cross-cluster identities.

Resolves #398"
```

---

### Task 13: Network Tunnel Manager (WireGuard/SSH/WebSocket)

**Files:**
- Create: `internal/agent/tunnel/manager.go`
- Create: `internal/agent/tunnel/wireguard.go`
- Create: `internal/agent/tunnel/ssh.go`
- Create: `internal/agent/tunnel/websocket.go`
- Create: `internal/agent/tunnel/tunnel.go` (interface)
- Test: `internal/agent/tunnel/manager_test.go`

**Step 1: Define tunnel interface**

```go
// internal/agent/tunnel/tunnel.go
package tunnel

type Tunnel interface {
    Start(ctx context.Context) error
    Stop() error
    IsHealthy() bool
    LocalAddr() string // local endpoint to use for dialing through this tunnel
}

type Config struct {
    Type           string // "wireguard", "ssh", "websocket"
    WireGuard      *WireGuardConfig
    SSH            *SSHConfig
    WebSocket      *WebSocketConfig
}
```

**Step 2: Implement NetworkTunnelManager**

```go
// internal/agent/tunnel/manager.go
type NetworkTunnelManager struct {
    mu      sync.RWMutex
    tunnels map[string]Tunnel // clusterName -> tunnel
    logger  *zap.Logger
}

func NewNetworkTunnelManager(logger *zap.Logger) *NetworkTunnelManager
func (m *NetworkTunnelManager) AddTunnel(clusterName string, config Config) error
func (m *NetworkTunnelManager) RemoveTunnel(clusterName string) error
func (m *NetworkTunnelManager) GetTunnel(clusterName string) (Tunnel, bool)
func (m *NetworkTunnelManager) HealthCheck() map[string]bool
```

**Step 3: Implement WireGuard tunnel**

```go
// internal/agent/tunnel/wireguard.go
type WireGuardTunnel struct {
    config     WireGuardConfig
    device     *device.Device // from wireguard-go
    logger     *zap.Logger
    localAddr  string
}

type WireGuardConfig struct {
    PrivateKey         string
    PeerPublicKey      string
    PeerEndpoint       string
    AllowedIPs         []string
    PersistentKeepalive int
}
```

Use `golang.zx2c4.com/wireguard` (wireguard-go) for userspace WireGuard. Create a TUN device, configure peer, set allowed IPs.

**Step 4: Implement SSH tunnel**

```go
// internal/agent/tunnel/ssh.go
type SSHTunnel struct {
    config    SSHConfig
    client    *ssh.Client
    listener  net.Listener // local port forward
    logger    *zap.Logger
    localAddr string
}

type SSHConfig struct {
    Host           string
    Port           int
    User           string
    PrivateKeyRef  string
    RemoteAddr     string // remote agent tunnel address
}
```

Use `golang.org/x/crypto/ssh` for SSH client. Establish connection, set up local port forwarding to remote agent's tunnel port (15002).

**Step 5: Implement WebSocket tunnel**

```go
// internal/agent/tunnel/websocket.go
type WebSocketTunnel struct {
    config    WebSocketConfig
    conn      *websocket.Conn
    listener  net.Listener
    logger    *zap.Logger
    localAddr string
}

type WebSocketConfig struct {
    URL       string
    TLS       *tls.Config
    Headers   map[string]string
}
```

Use `github.com/gorilla/websocket` (already a dependency). Dial WebSocket, bridge to local TCP listener.

**Step 6: Wire into agent startup**

In the agent startup (or via ConfigSnapshot), when `RemoteClusterConnection.Mode == "Tunnel"`, create the appropriate tunnel via `NetworkTunnelManager` and register the local tunnel address in the `CrossClusterTunnelRegistry`.

**Step 7: Add reconnection with exponential backoff**

Each tunnel implementation includes a reconnection loop:
```go
func (t *WireGuardTunnel) maintainConnection(ctx context.Context) {
    backoff := time.Second
    for {
        if err := t.connect(); err != nil {
            t.logger.Warn("Tunnel connection failed, retrying", zap.Duration("backoff", backoff), zap.Error(err))
            select {
            case <-ctx.Done():
                return
            case <-time.After(backoff):
            }
            backoff = min(backoff*2, 30*time.Second)
            continue
        }
        backoff = time.Second
        // Wait for disconnect
        select {
        case <-ctx.Done():
            return
        case <-t.disconnected:
        }
    }
}
```

**Step 8: Test, format, build, commit**

```bash
gofmt -s -w internal/agent/tunnel/
go test ./internal/agent/tunnel/... -v
go build ./cmd/novaedge-agent
git add internal/agent/tunnel/
git commit -m "[Feature] Add NetworkTunnelManager with WireGuard/SSH/WebSocket

Implement optional network tunnel layer for NAT/firewall traversal.
WireGuard uses wireguard-go userspace, SSH uses port forwarding,
WebSocket bridges to local TCP. All include reconnection with
exponential backoff and health checking.

Resolves #398"
```

---

### Task 14: Remote Cluster Cleanup

**Files:**
- Modify: `internal/operator/controller/novaedgeremotecluster_controller.go` (line ~372)

**Step 1: Implement cleanupRemoteCluster()**

Replace the TODO with:
```go
func (r *NovaEdgeRemoteClusterReconciler) cleanupRemoteCluster(ctx context.Context, rc *novaedgev1alpha1.NovaEdgeRemoteCluster) error {
    logger := log.FromContext(ctx)

    // 1. Unregister from registry (existing)
    r.Registry.Unregister(rc.Spec.ClusterName)

    // 2. Delete resources originated from this cluster
    clusterName := rc.Spec.ClusterName
    labelSelector := client.MatchingLabels{"novaedge.io/federation-origin": clusterName}

    // Delete federated ProxyGateways
    gatewayList := &novaedgev1alpha1.ProxyGatewayList{}
    if err := r.List(ctx, gatewayList, labelSelector); err == nil {
        for i := range gatewayList.Items {
            if err := r.Delete(ctx, &gatewayList.Items[i]); err != nil {
                logger.Error("Failed to delete federated gateway", zap.Error(err))
            }
        }
    }
    // Repeat for ProxyRoute, ProxyBackend, ProxyPolicy, ProxyVIP, ConfigMaps, Secrets

    // 3. Tear down network tunnel if active
    if r.TunnelManager != nil {
        r.TunnelManager.RemoveTunnel(clusterName)
    }

    logger.Info("Cleaned up remote cluster resources", "cluster", clusterName)
    return nil
}
```

**Step 2: Test, format, build, commit**

```bash
gofmt -s -w internal/operator/controller/novaedgeremotecluster_controller.go
go test ./internal/operator/controller/... -v
go build ./cmd/novaedge-operator
git add internal/operator/controller/novaedgeremotecluster_controller.go
git commit -m "[Feature] Implement remote cluster cleanup on deletion

Delete all resources labeled with novaedge.io/federation-origin
matching the cluster name. Tear down network tunnel if active.
Unregister from in-memory registry.

Resolves #398"
```

---

### Task 15: Federation Mode Field in CRD

**Files:**
- Modify: `api/v1alpha1/novaedgefederation_types.go`
- Run: `make manifests` to regenerate CRDs
- Modify: `internal/controller/federation/manager.go` (respect mode)

**Step 1: Add Mode field to NovaEdgeFederationSpec**

```go
// FederationMode defines the federation operating mode
// +kubebuilder:validation:Enum=hub-spoke;mesh;unified
type FederationMode string

const (
    FederationModeHubSpoke FederationMode = "hub-spoke"
    FederationModeMesh     FederationMode = "mesh"
    FederationModeUnified  FederationMode = "unified"
)

// In NovaEdgeFederationSpec:
// +kubebuilder:default=mesh
Mode FederationMode `json:"mode,omitempty"`
```

**Step 2: Respect mode in Manager**

- `hub-spoke`: one-way sync (hub pushes, spokes receive), no endpoint merging
- `mesh`: bidirectional sync + endpoint merging
- `unified`: bidirectional sync + endpoint merging + shared service namespace + locality routing enforced

**Step 3: Regenerate CRDs, test, commit**

```bash
make manifests
make generate
gofmt -s -w api/v1alpha1/novaedgefederation_types.go internal/controller/federation/manager.go
go build ./cmd/novaedge-controller && go build ./cmd/novaedge-operator
git add api/ config/crd/ internal/controller/federation/manager.go
git commit -m "[Feature] Add federation mode (hub-spoke/mesh/unified)

Add Mode field to NovaEdgeFederation CRD spec. hub-spoke enables
one-way config push, mesh adds bidirectional sync with endpoint
merging, unified adds shared service namespace with location-aware
routing.

Resolves #398"
```

---

### Task 16: Update Helm Charts

**Files:**
- Modify: `charts/novaedge/values.yaml`
- Modify: `charts/novaedge/templates/` (federation-related templates)

**Step 1: Add cross-cluster and tunnel values**

In `values.yaml`, extend the federation section:
```yaml
federation:
  # ... existing fields
  mode: mesh  # hub-spoke, mesh, unified
  crossCluster:
    endpointMerging: true
    localityRouting:
      enabled: true
      localRegion: ""
      localZone: ""
      minHealthyPercent: 0.7
  tunnel:
    enabled: false
    type: ""  # wireguard, ssh, websocket
    wireguard:
      privateKeySecretRef: ""
      peerPublicKey: ""
      peerEndpoint: ""
      allowedIPs: []
      persistentKeepalive: 25
    ssh:
      host: ""
      port: 22
      user: ""
      privateKeySecretRef: ""
      remoteAddr: ""
    websocket:
      url: ""
      tlsSecretRef: ""
```

**Step 2: Update templates**

Update the NovaEdgeFederation CRD template and any related Deployment/DaemonSet templates to include the new configuration.

**Step 3: Run Helm lint, commit**

```bash
helm lint charts/novaedge
git add charts/
git commit -m "[Chore] Update Helm charts with federation cross-cluster config

Add mode, cross-cluster endpoint merging, locality routing, and
optional tunnel configuration to federation Helm values.

Resolves #398"
```

---

### Task 17: Example CRDs and Samples

**Files:**
- Create: `config/samples/federation/hub-spoke-example.yaml`
- Create: `config/samples/federation/mesh-federation-example.yaml`
- Create: `config/samples/federation/unified-cluster-example.yaml`
- Create: `config/samples/federation/remote-cluster-wireguard.yaml`
- Create: `config/samples/federation/remote-cluster-direct.yaml`

**Step 1: Create hub-spoke example**

Shows a hub controller managing two spoke clusters with one-way config push.

**Step 2: Create mesh federation example**

Shows two clusters sharing services bidirectionally with endpoint merging.

**Step 3: Create unified cluster example**

Shows three clusters acting as one with location-aware routing across regions.

**Step 4: Create remote cluster examples**

One with direct connectivity, one with WireGuard tunnel.

**Step 5: Commit**

```bash
git add config/samples/federation/
git commit -m "[Docs] Add federation example CRDs for all modes

Examples for hub-spoke, mesh, and unified federation modes.
Remote cluster examples for direct and WireGuard connectivity.

Resolves #398"
```

---

### Task 18: Documentation and Architecture Diagrams

**Files:**
- Create: `docs/user-guide/federation.md`
- Create: `docs/user-guide/cross-cluster-routing.md`
- Create: `docs/user-guide/federation-tunnels.md`
- Modify: `mkdocs.yml` (add nav entries)
- Modify: `docs/reference/crds.md` (update CRD reference if exists)

**Step 1: Write federation user guide**

Cover:
- Federation overview (three modes explained)
- Setup walkthrough for each mode
- TLS configuration between clusters
- Monitoring federation health
- Troubleshooting

Include Mermaid diagrams:
- Hub-spoke topology
- Mesh federation with bidirectional sync
- Unified cluster with location-aware routing

**Step 2: Write cross-cluster routing guide**

Cover:
- How endpoint merging works
- Locality-aware routing (Region > Zone > Cluster)
- Configuring RemoteClusterRouting (priority, weight, local preference)
- Failover behavior
- Monitoring cross-cluster traffic

Include Mermaid diagrams:
- Cross-cluster traffic flow
- Endpoint merging pipeline
- Locality hierarchy

**Step 3: Write tunnel guide**

Cover:
- When tunnels are needed (NAT/firewall)
- WireGuard setup with key generation
- SSH tunnel configuration
- WebSocket for HTTPS-only environments
- Health checking and reconnection

**Step 4: Update mkdocs.yml**

Add nav entries under the appropriate section.

**Step 5: Update CRD reference**

Add documentation for new/updated fields (Mode, ServiceEndpoints, tunnel config).

**Step 6: Build docs, commit**

```bash
mkdocs build --strict
git add docs/ mkdocs.yml
git commit -m "[Docs] Add federation, cross-cluster routing, and tunnel documentation

Comprehensive user guides with Mermaid architecture diagrams for
federation modes, cross-cluster endpoint merging and routing,
and optional network tunnels (WireGuard/SSH/WebSocket).

Resolves #398"
```

---

### Task 19: Integration Tests

**Files:**
- Create: `internal/controller/federation/integration_test.go`
- Create: `internal/agent/router/crosscluster_integration_test.go`

**Step 1: Federation sync integration test**

Test that two federation managers can:
- Establish peer connection
- Exchange resource changes
- Resolve conflicts with LastWriterWins
- Run anti-entropy and detect drift

**Step 2: Endpoint merging integration test**

Test that:
- Local endpoints are published to federation
- Remote endpoints are received and cached
- Snapshot builder merges local + remote endpoints
- Labels are correctly set

**Step 3: Cross-cluster forwarding test**

Test that:
- Remote endpoint detection works
- Tunnel registry returns correct gateway
- Request is forwarded through tunnel to mock backend

**Step 4: Commit**

```bash
go test ./internal/controller/federation/... -v -run TestIntegration
go test ./internal/agent/router/... -v -run TestCrossCluster
git add internal/controller/federation/integration_test.go internal/agent/router/crosscluster_integration_test.go
git commit -m "[Test] Add federation and cross-cluster integration tests

Integration tests for federation sync, endpoint merging, and
cross-cluster forwarding via tunnel.

Resolves #398"
```

---

### Task 20: Push and Create PR

**Step 1: Push branch**

```bash
git push -u origin issue-398-federation-implementation
```

**Step 2: Create PR**

```bash
gh pr create --title "Implement federation with cross-cluster routing and tunneling" \
  --body "$(cat <<'EOF'
## Summary

Complete implementation of the federation subsystem with three progressive modes:

- **Hub-Spoke:** One-way config push from hub to spoke clusters
- **Mesh Federation:** Bidirectional sync + cross-cluster endpoint merging
- **Unified Cluster:** Shared service namespace + Region > Zone > Cluster location-aware routing

### Key Features
- Wire existing code: split-brain detector, snapshot enhancer, metrics collector
- Complete anti-entropy with merkle tree peer comparison
- Resource application: write federated resources to local K8s API
- Config hot reload: update running manager when CRD spec changes
- ServiceEndpoints sync: exchange endpoint data between clusters
- Cross-cluster endpoint merging in ConfigSnapshot builder
- Enhanced locality LB: Region > Zone > Cluster hierarchy
- Cross-cluster forwarding via mTLS HTTP/2 CONNECT tunnel
- Extended SPIFFE identity for cross-cluster trust
- Optional network tunnels: WireGuard, SSH, WebSocket
- Remote cluster cleanup on deletion
- Federation mode CRD field
- Updated Helm charts, example CRDs, comprehensive documentation

## Test plan
- [ ] Federation sync integration tests pass
- [ ] Cross-cluster endpoint merging tests pass
- [ ] Cross-cluster forwarding tests pass
- [ ] Locality LB hierarchy tests pass
- [ ] All existing tests still pass
- [ ] Helm lint passes
- [ ] mkdocs build --strict passes
- [ ] CI passes all checks

Resolves #398
EOF
)"
```

---

### Task 21: Clean Up Worktree (after PR merge)

**Step 1: Remove worktree**

Run (from main repo):
```bash
cd /Users/pascal/Documents/git/novaedge
git worktree remove ../novaedge-worktrees/issue-398-federation-implementation
git branch -d issue-398-federation-implementation
```
