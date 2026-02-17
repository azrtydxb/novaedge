# SD-WAN Implementation Design

## Goal

Transform NovaEdge from a multi-cluster federation platform into a full SD-WAN solution by implementing the data plane (WireGuard tunnels with overlay routing), WAN intelligence (link quality probing and application-aware path selection), and polish features (STUN NAT traversal, DSCP marking, topology dashboard).

## Current State

NovaEdge already has ~60% of SD-WAN capabilities built:

| Component | Lines | Status |
|-----------|-------|--------|
| Federation control plane sync | ~4,750 | Complete |
| Split-brain detection (quorum) | ~890 | Complete |
| Anti-entropy (Merkle trees) | ~555 | Complete |
| Agent failover (state machine) | ~975 | Complete |
| BGP/OSPF/BFD dynamic routing | ~1,834 | Complete (VIP only) |
| RemoteCluster CRD with TunnelConfig | Full types | Complete |
| WireGuard tunnel implementation | ~300 | Broken (CLI shelling, missing private key, no overlay IP) |
| mTLS everywhere | Throughout | Complete |

**Confirmed gaps:** No wgctrl kernel API, no overlay CIDR routing, no WAN link probing, no path selection engine, no multi-WAN link management, no STUN, no DSCP marking, no topology dashboard.

## Architecture

**Layered architecture** with two packages:

1. **Transport layer** (`internal/agent/tunnel/`) — enhanced existing package for WireGuard data plane, overlay routing, STUN discovery
2. **Intelligence layer** (`internal/agent/sdwan/`) — new package for WAN probing, path selection, link management, DSCP marking

Two new CRDs: `ProxyWANLink` (WAN uplink definition) and `ProxyWANPolicy` (application-to-path mapping).

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| WireGuard API | `wgctrl` kernel API | Reliable, no CLI dependency, proper error handling |
| CRD design | New `ProxyWANLink` + `ProxyWANPolicy` CRDs | Clean separation from RemoteCluster, independent lifecycle |
| NAT traversal | STUN discovery + WireGuard keepalive | Handles most NAT types without relay server |
| QoS | DSCP marking only | Lightweight, no tc/iptables dependency |
| Architecture | Layered (tunnel/ + sdwan/) | Clean separation of transport and intelligence |

---

## Phase 1: Data Plane (~1,700 lines)

### 1.1 WireGuard Rewrite (`internal/agent/tunnel/wireguard.go`)

Replace CLI-shelling implementation with `wgctrl` kernel API:

- **Dependency**: `golang.zx2c4.com/wireguard/wgctrl` + `golang.zx2c4.com/wireguard/wgctrl/wgtypes`
- **Private key**: Load from Kubernetes Secret via `PrivateKeySecretRef`; generate keypair if none exists
- **Interface creation**: `netlink.LinkAdd(&netlink.Wireguard{})` instead of `exec.Command("ip", "link", "add")`
- **Peer configuration**: `wgctrl.Client.ConfigureDevice()` instead of `exec.Command("wg", "set")`
- **Overlay IP**: Assign from site's overlay CIDR via `netlink.AddrAdd` on the WireGuard interface
- **Interface up**: `netlink.LinkSetUp()` instead of `exec.Command("ip", "link", "set", "up")`

**Connect flow:**
```
RemoteCluster CRD (with overlayCIDR + TunnelConfig)
  → Agent reads config
  → tunnel.Manager.AddTunnel()
  → wireguard.connect():
      1. netlink.LinkAdd (create wg interface)
      2. wgctrl.ConfigureDevice (private key + peer public key + endpoint + allowed IPs)
      3. netlink.AddrAdd (assign overlay IP from CRD overlayCIDR)
      4. netlink.LinkSetUp (bring up)
      5. overlay.InstallRoutes (remote CIDRs via wg interface)
```

### 1.2 Overlay Routing (`internal/agent/tunnel/overlay.go`)

After WireGuard interface is up, install overlay routes:

- `netlink.RouteAdd`: `<remote-site-CIDR> dev <wg-interface>`
- Feed overlay prefixes into existing BGP speaker (`internal/agent/vip/bgp.go`) so other nodes on the same site learn routes
- On tunnel teardown: `netlink.RouteDel` to clean up
- Idempotent: check for existing routes before adding

### 1.3 Tunnel Health via BFD

Run BFD sessions (existing `internal/agent/vip/bfd.go`) over the WireGuard interface:
- Sub-second failure detection
- On BFD failure: mark tunnel unhealthy, trigger path failover
- No new BFD code needed — configure BFD peer with WG overlay IP

### 1.4 CRD Extension

Add to `NovaEdgeRemoteClusterSpec`:
```go
// OverlayCIDR is the overlay network CIDR for this remote cluster (e.g., "10.200.1.0/24")
OverlayCIDR string `json:"overlayCIDR,omitempty"`
```

---

## Phase 2: WAN Intelligence (~1,800 lines)

### 2.1 New CRDs

#### ProxyWANLink (`api/v1alpha1/proxywanlink_types.go`)

```go
type ProxyWANLinkSpec struct {
    // Site identifies which site this WAN link belongs to
    Site string `json:"site"`
    // Interface is the physical network interface name
    Interface string `json:"interface"`
    // Provider is a human-readable ISP/provider name
    Provider string `json:"provider,omitempty"`
    // Bandwidth is the advertised link bandwidth (e.g., "1Gbps")
    Bandwidth string `json:"bandwidth,omitempty"`
    // Cost is the relative cost of this link (lower = preferred)
    Cost int32 `json:"cost,omitempty"`
    // SLA defines quality thresholds for this link
    SLA *WANLinkSLA `json:"sla,omitempty"`
    // TunnelEndpoint defines the WireGuard endpoint on this link
    TunnelEndpoint *WANTunnelEndpoint `json:"tunnelEndpoint,omitempty"`
    // Role defines the link's role: primary, backup, or loadbalance
    Role WANLinkRole `json:"role,omitempty"`
}

type WANLinkSLA struct {
    MaxLatency    *metav1.Duration `json:"maxLatency,omitempty"`
    MaxJitter     *metav1.Duration `json:"maxJitter,omitempty"`
    MaxPacketLoss *float64         `json:"maxPacketLoss,omitempty"` // 0.0-100.0 percent
}

type WANTunnelEndpoint struct {
    PublicIP string `json:"publicIP,omitempty"`
    Port     int32  `json:"port,omitempty"`
}

type WANLinkRole string
const (
    WANLinkRolePrimary     WANLinkRole = "primary"
    WANLinkRoleBackup      WANLinkRole = "backup"
    WANLinkRoleLoadBalance WANLinkRole = "loadbalance"
)
```

#### ProxyWANPolicy (`api/v1alpha1/proxywanpolicy_types.go`)

```go
type ProxyWANPolicySpec struct {
    // Match defines which traffic this policy applies to
    Match WANPolicyMatch `json:"match"`
    // PathSelection defines how to choose the WAN path
    PathSelection WANPathSelection `json:"pathSelection"`
}

type WANPolicyMatch struct {
    Hosts   []string          `json:"hosts,omitempty"`
    Paths   []string          `json:"paths,omitempty"`
    Headers map[string]string `json:"headers,omitempty"`
}

type WANPathSelection struct {
    Strategy  WANStrategy `json:"strategy"`
    Failover  bool        `json:"failover,omitempty"`
    DSCPClass string      `json:"dscpClass,omitempty"` // EF, AF41, AF21, CS1, BE
}

type WANStrategy string
const (
    WANStrategyLowestLatency    WANStrategy = "lowest-latency"
    WANStrategyHighestBandwidth WANStrategy = "highest-bandwidth"
    WANStrategyMostReliable     WANStrategy = "most-reliable"
    WANStrategyLowestCost       WANStrategy = "lowest-cost"
)
```

### 2.2 Link Quality Prober (`internal/agent/sdwan/prober.go`)

Goroutine per WAN link measuring quality:

- **Probes**: UDP echo packets every 1s to remote tunnel endpoint
- **Latency**: EWMA-smoothed RTT (alpha=0.3)
- **Jitter**: Standard deviation of last 30 samples
- **Packet loss**: Percentage of probes with no response within 2s timeout
- **Composite score**: `score = (1 - loss) * (1 / (latency_ms * (1 + jitter_ms/latency_ms)))` — higher is better
- **Health**: Link is healthy if all SLA thresholds from ProxyWANLink are met

```go
type LinkQuality struct {
    LinkName    string
    RemoteSite  string
    LatencyMs   float64
    JitterMs    float64
    PacketLoss  float64  // 0.0-1.0
    Score       float64
    LastUpdated time.Time
    Healthy     bool
}
```

### 2.3 Path Selection Engine (`internal/agent/sdwan/pathselect.go`)

Selects best WAN link for traffic:

- **Input**: Matched `ProxyWANPolicy` + available links with quality data
- **Strategies**: lowest-latency, highest-bandwidth, most-reliable, lowest-cost
- **Failover**: If selected link drops below SLA, switch to next-best
- **Hysteresis**: Require better link to be above threshold for 10s before switching back (prevent flip-flop)
- **Output**: Target link name + tunnel interface for routing

### 2.4 Link Manager (`internal/agent/sdwan/linkmanager.go`)

Watches ProxyWANLink CRDs and manages lifecycle:

- Create/destroy tunnels per link via `tunnel.NetworkTunnelManager`
- Start/stop probing per link
- Track link states: active, degraded (above SLA but quality declining), down
- Coordinate active-active (ECMP) vs active-passive (failover) based on role

### 2.5 Prometheus Metrics (`internal/agent/sdwan/metrics.go`)

- `novaedge_sdwan_link_latency_ms{link, remote_site}` — gauge
- `novaedge_sdwan_link_jitter_ms{link, remote_site}` — gauge
- `novaedge_sdwan_link_packet_loss_ratio{link, remote_site}` — gauge
- `novaedge_sdwan_link_score{link, remote_site}` — gauge
- `novaedge_sdwan_link_healthy{link, remote_site}` — gauge (0 or 1)
- `novaedge_sdwan_path_selections_total{strategy, link}` — counter
- `novaedge_sdwan_failovers_total{from_link, to_link}` — counter

---

## Phase 3: Polish (~1,000 lines)

### 3.1 STUN Discovery (`internal/agent/tunnel/stun.go`)

Before WireGuard tunnel setup, discover public endpoint:

- **Library**: `github.com/pion/stun/v3`
- **STUN servers**: Configurable, default `stun.l.google.com:19302`
- **Cache**: 5-minute TTL, re-discover on tunnel reconnect
- **Fallback**: Use static endpoint from CRD if STUN fails
- **Integration**: Called by `tunnel.Manager.AddTunnel()` before configuring WireGuard peer

### 3.2 DSCP Marking (`internal/agent/sdwan/dscp.go`)

Mark outgoing tunnel packets with DSCP values:

- Set IP TOS byte via `syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_TOS, dscpValue)`
- Applied at the proxy dialer level when dialing upstream through a tunnel
- Supported classes: EF (46), AF41 (34), AF21 (18), CS1 (8), BE (0)
- No tc/iptables dependency — pure Go socket option

### 3.3 WebUI Topology Dashboard

New SD-WAN page in the existing React web UI:

**Components:**
- Topology map: Sites as nodes, WAN links as edges with color-coded quality (green/yellow/red)
- Link quality table: Real-time latency/jitter/loss per link with SLA threshold indicators
- Path selection log: Recent path decisions with matched policy and selected link
- Tunnel status: WireGuard handshake state per remote cluster

**API endpoints** (novactl JSON API backend):
- `GET /api/v1/sdwan/links` — all WAN links with quality data
- `GET /api/v1/sdwan/topology` — site topology graph (nodes + edges)
- `GET /api/v1/sdwan/policies` — active WAN policies with match counts
- `GET /api/v1/sdwan/events` — recent path selection/failover events

---

## File Summary

### New Files
| File | Package | Purpose | Est. Lines |
|------|---------|---------|------------|
| `internal/agent/tunnel/overlay.go` | tunnel | Overlay route installation via netlink | ~200 |
| `internal/agent/tunnel/stun.go` | tunnel | STUN endpoint discovery | ~150 |
| `internal/agent/sdwan/sdwan.go` | sdwan | SDWANManager orchestrator | ~200 |
| `internal/agent/sdwan/prober.go` | sdwan | WAN link quality probing | ~400 |
| `internal/agent/sdwan/pathselect.go` | sdwan | SLA-based path selection | ~300 |
| `internal/agent/sdwan/linkmanager.go` | sdwan | Multi-WAN link lifecycle | ~350 |
| `internal/agent/sdwan/dscp.go` | sdwan | DSCP packet marking | ~100 |
| `internal/agent/sdwan/metrics.go` | sdwan | Prometheus SD-WAN metrics | ~100 |
| `api/v1alpha1/proxywanlink_types.go` | v1alpha1 | ProxyWANLink CRD types | ~150 |
| `api/v1alpha1/proxywanpolicy_types.go` | v1alpha1 | ProxyWANPolicy CRD types | ~120 |
| `web/src/pages/SDWANTopology.tsx` | web | Topology dashboard page | ~400 |
| Tests (`*_test.go`) | various | Unit tests for all new code | ~800 |

### Modified Files
| File | Change |
|------|--------|
| `internal/agent/tunnel/wireguard.go` | Rewrite: wgctrl kernel API |
| `internal/agent/tunnel/manager.go` | Add overlay CIDR awareness |
| `api/v1alpha1/novaedgeremotecluster_types.go` | Add `OverlayCIDR` field |
| `cmd/novactl/main.go` | Add SD-WAN API endpoints |
| `web/src/App.tsx` | Add SD-WAN route |
| `go.mod` / `go.sum` | Add wgctrl, pion/stun dependencies |

### New Dependencies
- `golang.zx2c4.com/wireguard/wgctrl` — WireGuard kernel API
- `github.com/pion/stun/v3` — STUN client for NAT traversal

### Documentation
- `docs/user-guide/sdwan.md` — SD-WAN feature overview and setup guide
- `docs/reference/proxywanlink.md` — ProxyWANLink CRD reference
- `docs/reference/proxywanpolicy.md` — ProxyWANPolicy CRD reference
- `config/samples/` — Sample WAN link and policy YAMLs
- Update `docs/advanced/federation.md` — reference SD-WAN tunnel setup
- Update `mkdocs.yml` — add new pages to navigation

---

## Testing Strategy

- **Unit tests**: Every new file gets `_test.go` with table-driven tests
- **WireGuard**: Interface over `wgctrl.Client` for mocking; test key generation, peer config, overlay IP assignment
- **Prober**: Mock UDP listener; verify EWMA smoothing, jitter calculation, score formula
- **Path selection**: Table-driven tests for each strategy; test failover with hysteresis
- **Link manager**: Test lifecycle (add/remove/degrade) with mock tunnel manager
- **STUN**: Mock STUN server; test discovery, caching, fallback
- **DSCP**: Verify TOS byte setting on mock socket
- **Integration**: Test overlay route installation with netlink mocks

## Estimated Total
~4,500 lines of new/rewritten Go code + ~400 lines React/TypeScript + ~800 lines tests + documentation
