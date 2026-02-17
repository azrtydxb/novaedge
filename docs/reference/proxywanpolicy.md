# ProxyWANPolicy CRD Reference

`ProxyWANPolicy` defines application-aware path selection rules for SD-WAN traffic. It matches traffic by host, path, or headers and routes it through the optimal WAN link.

## Metadata

| Field | Value |
|-------|-------|
| API Group | `novaedge.io` |
| API Version | `v1alpha1` |
| Kind | `ProxyWANPolicy` |
| Scope | Namespaced |

## Spec Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `match` | `WANPolicyMatch` | No | -- | Traffic matching criteria. If omitted, matches all traffic. |
| `pathSelection` | `WANPathSelection` | Yes | -- | Path selection behavior for matched traffic. |

### WANPolicyMatch

All match fields are optional. When multiple fields are specified, they are combined with AND logic.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `hosts` | `[]string` | No | Hostnames to match (exact match). |
| `paths` | `[]string` | No | URL path prefixes to match. |
| `headers` | `map[string]string` | No | HTTP headers to match (exact key-value pairs). |

### WANPathSelection

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `strategy` | `string` | Yes | -- | Path selection algorithm. One of: `lowest-latency`, `highest-bandwidth`, `most-reliable`, `lowest-cost`. |
| `failover` | `bool` | No | `true` | Enable automatic failover to another link when the selected link fails SLA. |
| `dscpClass` | `string` | No | -- | DSCP marking to apply to matched traffic (e.g., `"EF"`, `"AF41"`, `"CS1"`). |

### Path Selection Strategies

| Strategy | Selection Criteria | Use Case |
|----------|-------------------|----------|
| `lowest-latency` | Link with the lowest measured one-way latency | VoIP, video conferencing, real-time APIs, gaming |
| `highest-bandwidth` | Link with the highest provisioned bandwidth | Large file transfers, video streaming, backups over fast links |
| `most-reliable` | Link with the lowest measured packet loss | Database replication, financial transactions, critical API calls |
| `lowest-cost` | Link with the lowest administrative cost value | Bulk backups, software updates, non-critical batch processing |

### DSCP Classes

| Class | Per-Hop Behavior | DSCP Value | Typical Use |
|-------|-----------------|------------|-------------|
| `EF` | Expedited Forwarding | 46 | VoIP, real-time media |
| `AF41` | Assured Forwarding 4.1 | 34 | Video conferencing |
| `AF42` | Assured Forwarding 4.2 | 36 | Video conferencing (lower priority) |
| `AF31` | Assured Forwarding 3.1 | 26 | Business-critical applications |
| `AF21` | Assured Forwarding 2.1 | 18 | Transactional data |
| `AF11` | Assured Forwarding 1.1 | 10 | Standard data |
| `CS6` | Class Selector 6 | 48 | Network control traffic |
| `CS5` | Class Selector 5 | 40 | Signaling / broadcast video |
| `CS1` | Class Selector 1 | 8 | Bulk / scavenger traffic |
| `BE` | Best Effort (default) | 0 | Default, unclassified traffic |

## Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | `string` | Current lifecycle phase of the WAN policy. |
| `conditions` | `[]Condition` | Standard Kubernetes conditions representing the policy's state. |
| `selectionCount` | `int64` | Total number of path selections made using this policy. |
| `observedGeneration` | `int64` | Most recent generation observed by the controller. |

## Print Columns

When using `kubectl get proxywanpolicies`, the following columns are displayed:

| Column | Source |
|--------|--------|
| Strategy | `.spec.pathSelection.strategy` |
| DSCP | `.spec.pathSelection.dscpClass` |
| Age | `.metadata.creationTimestamp` |

## Example

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyWANPolicy
metadata:
  name: voice-low-latency
  namespace: novaedge-system
spec:
  match:
    hosts:
      - "voip.example.com"
      - "sip.example.com"
    headers:
      content-type: "application/sdp"
  pathSelection:
    strategy: lowest-latency
    failover: true
    dscpClass: EF
```

## See Also

- [SD-WAN User Guide](../user-guide/sdwan.md) -- Setup walkthrough and operational guidance
- [ProxyWANLink Reference](proxywanlink.md) -- WAN link definition and SLA thresholds
- [Policies User Guide](../user-guide/policies.md) -- General policy configuration
