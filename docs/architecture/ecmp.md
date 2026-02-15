# ECMP Architecture with BGP/OSPF VIPs

## Overview

When NovaEdge VIPs are configured in BGP or OSPF mode, all healthy nodes
announce the same VIP address via routing protocols. Upstream routers see
multiple equal-cost paths and distribute incoming traffic across nodes using
Equal-Cost Multi-Path (ECMP) routing.

This creates a distributed, active-active load balancing topology where any
ECMP node can receive any client request.

## How ECMP Works

```
                    ┌──────────────┐
                    │   Client     │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │   Router     │
                    │  (ECMP LB)   │
                    └──┬───┬───┬───┘
                  ┌────┘   │   └────┐
           ┌──────▼──┐ ┌───▼───┐ ┌──▼──────┐
           │ Node A  │ │Node B │ │ Node C  │
           │ BGP VIP │ │BGP VIP│ │ BGP VIP │
           └────┬────┘ └───┬───┘ └────┬────┘
                │          │          │
           ┌────▼──────────▼──────────▼────┐
           │       Backend Endpoints       │
           │   (shared across all nodes)   │
           └───────────────────────────────┘
```

1. Each NovaEdge agent announces the VIP address via BGP/OSPF.
2. The upstream router learns equal-cost routes to the VIP.
3. The router hashes incoming packets (typically 5-tuple) and selects a
   next-hop node.
4. The selected node applies its LB algorithm to pick a backend endpoint.

## Why Hash-Based LB Is Required

With ECMP, the **router** selects the node but the **node** selects the backend.
If different nodes use non-deterministic algorithms (round-robin, least-conn,
P2C, EWMA), the same client may hit different backends depending on which node
the router selects — breaking session affinity and causing inconsistent behavior.

**Hash-based algorithms** (Maglev, RingHash) solve this because:

- Every node builds the same lookup table from the same endpoint list.
- Given the same hash key (e.g., source IP, header value), every node picks
  the same backend.
- Clients get consistent routing regardless of which ECMP node handles
  the request.

### Algorithm Compatibility Matrix

| Algorithm    | ECMP Compatible | Reason                                     |
|-------------|----------------|--------------------------------------------|
| Maglev      | Yes            | Deterministic hash, minimal disruption      |
| RingHash    | Yes            | Consistent hash ring, same endpoints = same mapping |
| RoundRobin  | No             | Per-node counter, different node = different backend |
| P2C         | No             | Random selection of two candidates          |
| EWMA        | No             | Per-node latency observations differ        |
| LeastConn   | No             | Per-node connection counts differ           |

## Automatic LB Policy Enforcement

NovaEdge enforces hash-based LB for ECMP VIPs at two levels:

### Snapshot-Time Validation (Hard Enforcement)

When building a ConfigSnapshot for a node with BGP/OSPF VIP assignments:

- **Unspecified policy** → auto-promoted to Maglev
- **Maglev or RingHash** → allowed as-is
- **Any other policy** → cluster is skipped (not included in snapshot), error logged

### ProxyVIP Status Condition (Visibility)

BGP/OSPF VIPs report a `LBPolicyValid` condition:

```yaml
status:
  conditions:
  - type: LBPolicyValid
    status: "False"
    reason: IncompatibleLBPolicy
    message: "Backends with non-hash LB will be excluded from ECMP routing: default/my-backend. Use Maglev or RingHash."
```

Use `kubectl describe proxyvip <name>` to check compatibility.

## Sticky Sessions with ECMP

Cookie-based sticky sessions work correctly with ECMP + hash-based LB:

1. First request: node sets a sticky cookie containing the backend endpoint
   address (e.g., `10.0.0.5:8080`).
2. Subsequent requests: any ECMP node reads the cookie and routes directly
   to the encoded backend, bypassing the hash lookup.

Since the cookie contains the actual endpoint address (not a node-local
index), it works regardless of which node handles the request.

## Shutdown and Connection Draining

When an agent shuts down, it must avoid dropping in-flight requests:

1. **VIP release**: Agent withdraws the BGP/OSPF route announcement.
2. **Drain period**: Agent waits for a configurable period (default: 3s)
   to let upstream routers remove the withdrawn route from their ECMP
   next-hop set.
3. **Server shutdown**: HTTP/L4 servers stop accepting new connections and
   drain existing ones.

Configure the drain period via:

```
--shutdown-drain-period=3s
```

The drain period is bounded by the overall shutdown timeout (10s). Set to
`0` to disable (not recommended for BGP/OSPF deployments).

## Configuration Examples

### BGP VIP with Maglev (Recommended)

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyVIP
metadata:
  name: web-vip
spec:
  address: 203.0.113.10/32
  mode: BGP
  bgpConfig:
    localAS: 65001
    peers:
    - address: 10.0.0.1
      as: 65000
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: web-backend
spec:
  lbPolicy: Maglev
  serviceRef:
    name: web-service
    port: 80
```

### OSPF VIP with RingHash

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyVIP
metadata:
  name: api-vip
spec:
  address: 203.0.113.20/32
  mode: OSPF
  ospfConfig:
    areaID: "0.0.0.0"
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: api-backend
spec:
  lbPolicy: RingHash
  serviceRef:
    name: api-service
    port: 8080
```

## Failure Scenarios

### Node Failure

When a node fails, BFD (if enabled) detects the failure in sub-second
time and the router removes that node from the ECMP set. Remaining nodes
continue serving with the same hash-to-backend mapping. Maglev ensures
minimal disruption — only flows that were assigned to the failed node are
redistributed.

### Backend Endpoint Removal

When an endpoint is removed, the controller pushes a new ConfigSnapshot.
All ECMP nodes update their hash tables simultaneously. Maglev minimizes
the number of reassigned flows — only those that mapped to the removed
endpoint shift to a new backend.

### Split-Brain / Stale Routes

If a node's BGP session flaps, the router may temporarily have stale
routes. BFD reduces the detection window. During the stale period, the
affected node may receive traffic but still has a valid hash table, so
routing remains consistent.
