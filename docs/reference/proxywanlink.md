# ProxyWANLink CRD Reference

`ProxyWANLink` represents a WAN link for SD-WAN multi-link management. It tracks link properties, SLA thresholds, and observed quality metrics.

## Metadata

| Field | Value |
|-------|-------|
| API Group | `novaedge.io` |
| API Version | `v1alpha1` |
| Kind | `ProxyWANLink` |
| Scope | Namespaced |

## Spec Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `site` | `string` | Yes | -- | Site name this WAN link belongs to. Must be non-empty. |
| `interface` | `string` | Yes | -- | Network interface name used for this WAN link (e.g., `eth0`). |
| `provider` | `string` | Yes | -- | ISP or WAN circuit provider name. |
| `bandwidth` | `string` | Yes | -- | Provisioned bandwidth of the link (e.g., `"100Mbps"`, `"1Gbps"`). |
| `cost` | `int32` | No | `100` | Administrative cost metric for path selection. Lower values are preferred. Minimum: 0. |
| `role` | `string` | No | `"primary"` | Link role: `primary`, `backup`, or `loadbalance`. |
| `sla` | `WANLinkSLA` | No | -- | SLA thresholds for health evaluation. |
| `tunnelEndpoint` | `WANTunnelEndpoint` | No | -- | Public endpoint for tunnel establishment. |

### WANLinkSLA

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `maxLatency` | `Duration` | No | Maximum acceptable one-way latency (e.g., `50ms`). |
| `maxJitter` | `Duration` | No | Maximum acceptable jitter (e.g., `10ms`). |
| `maxPacketLoss` | `float64` | No | Maximum acceptable packet loss ratio. Range: 0.0 to 1.0 (e.g., `0.01` for 1%). |

### WANTunnelEndpoint

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `publicIP` | `string` | Yes | Publicly reachable IP address of the tunnel endpoint. |
| `port` | `int32` | Yes | UDP/TCP port for tunnel traffic. Range: 1-65535. |

## Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | `string` | Current lifecycle phase of the WAN link. |
| `conditions` | `[]Condition` | Standard Kubernetes conditions representing the link's state. |
| `currentLatency` | `float64` | Most recently measured latency in milliseconds. |
| `currentPacketLoss` | `float64` | Most recently measured packet loss ratio (0.0-1.0). |
| `healthy` | `bool` | Whether the link currently meets its SLA thresholds. |
| `observedGeneration` | `int64` | Most recent generation observed by the controller. |

## Print Columns

When using `kubectl get proxywanlinks`, the following columns are displayed:

| Column | Source |
|--------|--------|
| Site | `.spec.site` |
| Role | `.spec.role` |
| Provider | `.spec.provider` |
| Healthy | `.status.healthy` |
| Age | `.metadata.creationTimestamp` |

## Example

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyWANLink
metadata:
  name: site-a-isp1
  namespace: novaedge-system
spec:
  site: site-a
  interface: eth0
  provider: "Acme ISP"
  bandwidth: "1Gbps"
  cost: 100
  role: primary
  sla:
    maxLatency: 50ms
    maxJitter: 10ms
    maxPacketLoss: 1.0
  tunnelEndpoint:
    publicIP: "203.0.113.1"
    port: 51820
```

## See Also

- [SD-WAN User Guide](../user-guide/sdwan.md) -- Setup walkthrough and operational guidance
- [ProxyWANPolicy Reference](proxywanpolicy.md) -- Application-aware path selection policies
- [Federation Tunnels](../user-guide/federation-tunnels.md) -- Tunnel configuration details
