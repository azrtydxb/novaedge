# Federation Tunnels

When clusters cannot communicate directly -- due to NAT gateways, firewalls, or restrictive network policies -- NovaEdge provides optional network tunnels to carry federation and agent traffic between clusters. Three tunnel types are supported: WireGuard, SSH, and WebSocket.

## When Tunnels Are Needed

Tunnels are required when the standard gRPC connectivity between controllers (port 9443) or between agents and controllers (port 9090) is blocked or unreliable:

| Scenario | Tunnel Needed? | Recommended Tunnel |
|----------|---------------|-------------------|
| Clusters in same VPC / VPN | No | Direct connectivity |
| Clusters in peered VPCs (same cloud) | No | Direct connectivity |
| Clusters behind NAT gateway | Yes | WireGuard |
| On-premises cluster behind corporate firewall | Yes | WireGuard or SSH |
| Clusters separated by strict egress-only firewall (HTTPS only) | Yes | WebSocket |
| Different cloud providers without VPN | Yes | WireGuard |
| Air-gapped with bastion host | Yes | SSH |

## Choosing a Tunnel Type

| Feature | WireGuard | SSH | WebSocket |
|---------|-----------|-----|-----------|
| **Protocol** | UDP (kernel-level) | TCP | TCP over HTTPS |
| **Performance** | Best (kernel-space crypto) | Good | Moderate (double encryption with TLS) |
| **NAT traversal** | Yes (keepalive) | Yes (port forwarding) | Yes (HTTP/HTTPS proxy compatible) |
| **Firewall friendliness** | Requires UDP port open | Requires TCP port open | Works through HTTPS-only firewalls |
| **Setup complexity** | Moderate (key exchange) | Low (standard SSH) | Low (standard HTTP) |
| **Reconnection** | Automatic | Automatic with retry | Automatic with retry |
| **Use case** | Primary tunnel for most deployments | Bastion/jump host environments | Corporate proxies, HTTPS-only egress |

## WireGuard Tunnel

WireGuard provides the best performance because encryption happens in kernel space. It uses a single UDP port and handles NAT traversal with persistent keepalives.

### Key Generation

Generate a WireGuard key pair for each cluster:

```bash
# On the remote cluster (or any machine)
wg genkey | tee privatekey | wg pubkey > publickey

# View the keys
cat privatekey   # Keep this secret
cat publickey    # Share this with the hub
```

### Store the Private Key

Create a Kubernetes secret in the remote cluster containing the private key:

```bash
kubectl -n nova-system create secret generic wg-private-key \
  --from-file=privateKey=./privatekey
```

### Configure the Remote Cluster

In the `NovaEdgeRemoteCluster` resource on the hub, specify the WireGuard tunnel:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeRemoteCluster
metadata:
  name: edge-onprem-dc1
  namespace: nova-system
spec:
  clusterName: edge-onprem-dc1
  region: us-west-2
  zone: datacenter-1

  connection:
    mode: Tunnel
    # Use the tunnel-internal address (WireGuard interface IP)
    controllerEndpoint: "10.200.0.1:9090"

    # mTLS still secures gRPC inside the tunnel (defense in depth)
    tls:
      enabled: true
      caSecretRef:
        name: remote-cluster-ca
        namespace: nova-system
      clientCertSecretRef:
        name: remote-agent-cert
        namespace: nova-system
      serverName: novaedge-hub.internal

    tunnel:
      type: WireGuard
      wireGuard:
        # Secret containing this cluster's WireGuard private key
        privateKeySecretRef:
          name: wg-private-key
          key: privateKey

        # The hub cluster's WireGuard public key
        publicKey: "aB1cD2eF3gH4iJ5kL6mN7oP8qR9sT0uV1wX2yZ3A="

        # Hub's WireGuard endpoint (must be publicly reachable on UDP)
        endpoint: "wg-hub.example.com:51820"

        # IP ranges routed through the tunnel
        allowedIPs:
          - "10.200.0.0/24"    # WireGuard tunnel network
          - "10.96.0.0/12"     # Hub cluster service CIDR
          - "10.244.0.0/16"    # Hub cluster pod CIDR

        # Keepalive for NAT traversal (seconds)
        persistentKeepalive: 25

    reconnectInterval: 15s
    timeout: 15s
```

### WireGuard Network Requirements

- The hub must expose UDP port 51820 (or your chosen WireGuard port) publicly
- The remote cluster needs outbound UDP access to the hub's WireGuard endpoint
- `allowedIPs` must include any CIDR ranges the remote cluster needs to reach through the tunnel

## SSH Tunnel

SSH tunnels are useful when you already have bastion hosts or jump servers in your infrastructure. The tunnel forwards the gRPC port through an SSH connection.

### Configuration

```yaml
spec:
  connection:
    mode: Tunnel
    controllerEndpoint: "127.0.0.1:9090"

    tunnel:
      type: SSH
      relayEndpoint: "bastion.example.com:22"

    tls:
      enabled: true
      caSecretRef:
        name: remote-cluster-ca
        namespace: nova-system
      clientCertSecretRef:
        name: remote-agent-cert
        namespace: nova-system
      serverName: novaedge-hub.internal

    reconnectInterval: 30s
    timeout: 10s
```

### SSH Requirements

- The bastion host must be reachable from the remote cluster on TCP port 22 (or configured SSH port)
- SSH key-based authentication must be configured (password authentication is not supported)
- The bastion host must be able to reach the hub controller's gRPC endpoint

## WebSocket Tunnel

WebSocket tunnels carry gRPC traffic over standard HTTPS connections. This is the only option when corporate firewalls restrict all traffic to HTTP/HTTPS (ports 80/443).

### Configuration

```yaml
spec:
  connection:
    mode: Tunnel
    controllerEndpoint: "wss://novaedge-hub.example.com:443/federation/tunnel"

    tunnel:
      type: WebSocket
      relayEndpoint: "wss://novaedge-hub.example.com:443/federation/tunnel"

    tls:
      enabled: true
      caSecretRef:
        name: remote-cluster-ca
        namespace: nova-system
      clientCertSecretRef:
        name: remote-agent-cert
        namespace: nova-system
      serverName: novaedge-hub.example.com

    reconnectInterval: 30s
    timeout: 10s
```

### WebSocket Requirements

- The hub must expose an HTTPS endpoint (port 443) that accepts WebSocket upgrades
- The remote cluster needs outbound HTTPS access to the hub
- If a corporate HTTP proxy is in the path, it must support WebSocket upgrades (most modern proxies do)

!!! note "Double encryption"
    WebSocket tunnels carry mTLS-encrypted gRPC traffic inside a TLS-encrypted WebSocket connection. This adds overhead compared to WireGuard or SSH, but ensures compatibility with HTTPS-only network policies.

## Health Checking and Reconnection

All tunnel types include automatic health checking and reconnection:

1. **Connection monitoring** -- the tunnel manager periodically verifies the tunnel is alive by sending keepalive packets
2. **Automatic reconnection** -- if the tunnel drops, the agent retries at the `reconnectInterval` (default: 30s) with exponential backoff
3. **Health reporting** -- tunnel status is reflected in the `NovaEdgeRemoteCluster` status:

```bash
kubectl -n nova-system get novaedgeremoteclusters edge-onprem-dc1 -o yaml
```

Key status fields:

| Field | Description |
|-------|-------------|
| `status.phase` | `Connected`, `Connecting`, `Disconnected`, `Degraded`, `Failed` |
| `status.connection.connected` | Whether the tunnel is currently up |
| `status.connection.latency` | Round-trip latency through the tunnel |
| `status.connection.error` | Last error message if disconnected |
| `status.connection.lastConnected` | Timestamp of last successful connection |

### Tuning Health Checks for Tunnels

Tunnels add latency and can be less stable than direct connections. Adjust the remote cluster's health check settings accordingly:

```yaml
spec:
  healthCheck:
    enabled: true
    # Less frequent checks for tunnel-connected clusters
    interval: 60s
    # Longer timeout to account for tunnel overhead
    timeout: 15s
    # Require more successes to confirm recovery
    healthyThreshold: 2
    # Be more tolerant of transient failures
    unhealthyThreshold: 5
    failoverEnabled: true
```

## Routing Considerations for Tunneled Clusters

Tunnels add latency, so routing configuration should reflect this:

```yaml
spec:
  routing:
    enabled: true
    # Higher priority number = lower preference (direct clusters get 100, tunneled get 200)
    priority: 200
    # Lower traffic weight reflects higher latency
    weight: 30
    # Strongly prefer local backends to minimize tunnel usage
    localPreference: true
    # Still allow cross-cluster traffic for failover
    allowCrossClusterTraffic: true
```

## Related Guides

- [Federation](federation.md) -- federation setup and modes
- [Cross-Cluster Routing](cross-cluster-routing.md) -- endpoint merging and locality-aware routing
