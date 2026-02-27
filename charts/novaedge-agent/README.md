# NovaEdge Agent Helm Chart

This chart deploys NovaEdge agents for remote/edge cluster deployments in a hub-spoke multi-cluster architecture.

## Overview

In a hub-spoke deployment:
- **Hub Cluster**: Runs the NovaEdge operator, controller, and web UI
- **Spoke Clusters**: Run only NovaEdge agents that connect back to the hub controller

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Hub Cluster (Management)                         │
│  ┌─────────────────────┐    ┌─────────────────────┐                     │
│  │  NovaEdge Operator  │    │  NovaEdge Controller │◄────────────────┐  │
│  └─────────────────────┘    └──────────┬──────────┘                  │  │
│                                        │                              │  │
│  ┌─────────────────────┐               │ gRPC/mTLS                    │  │
│  │   NovaEdge Web UI   │               │                              │  │
│  └─────────────────────┘               │                              │  │
└────────────────────────────────────────┼──────────────────────────────┘  │
                                         │                                  │
              ┌──────────────────────────┼──────────────────────────┐      │
              │                          │                          │      │
              ▼                          ▼                          ▼      │
┌─────────────────────────┐  ┌─────────────────────────┐  ┌──────────────────┐
│   Edge Cluster 1        │  │   Edge Cluster 2        │  │   Edge Cluster 3 │
│  ┌───────────────────┐  │  │  ┌───────────────────┐  │  │  ┌────────────┐  │
│  │  NovaEdge Agents  │──┼──┼──│  NovaEdge Agents  │──┼──┼──│   Agents   │──┘
│  │  (This Chart)     │  │  │  │  (This Chart)     │  │  │  │            │
│  └───────────────────┘  │  │  └───────────────────┘  │  │  └────────────┘
│       Region: US-West   │  │       Region: EU-West   │  │    Region: Asia
└─────────────────────────┘  └─────────────────────────┘  └──────────────────┘
```

## Prerequisites

- Kubernetes 1.29+
- Helm 3.0+
- Hub cluster with NovaEdge controller running
- Network connectivity from spoke to hub (direct or via tunnel)
- mTLS certificates for secure communication

## Installation

### Step 1: Prepare mTLS Certificates

Generate or obtain certificates for secure agent-controller communication:

```bash
# Option 1: Use cert-manager (recommended)
# Create a Certificate resource that will generate the secrets

# Option 2: Manual certificate generation
# Create CA and client certificates, then create secrets:
kubectl create secret generic novaedge-ca \
  --from-file=ca.crt=path/to/ca.crt \
  -n nova-system

kubectl create secret tls novaedge-agent-cert \
  --cert=path/to/client.crt \
  --key=path/to/client.key \
  -n nova-system
```

### Step 2: Install the Chart

```bash
# Create namespace
kubectl create namespace nova-system

# Install with required values
helm install novaedge-agent ./charts/novaedge-agent \
  --namespace nova-system \
  --set cluster.name=edge-cluster-1 \
  --set cluster.region=us-west \
  --set connection.controllerEndpoint=controller.hub-cluster.example.com:9090 \
  --set tls.caSecretName=novaedge-ca \
  --set tls.clientCertSecretName=novaedge-agent-cert
```

### Step 3: Register in Hub Cluster

Create a `NovaEdgeRemoteCluster` resource in the hub cluster:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeRemoteCluster
metadata:
  name: edge-cluster-1
  namespace: nova-system
spec:
  clusterName: edge-cluster-1
  region: us-west
  connection:
    mode: Direct
    controllerEndpoint: controller.nova-system.svc.cluster.local:9090
    tls:
      enabled: true
  healthCheck:
    enabled: true
    interval: 30s
```

## Configuration

### Required Values

| Parameter | Description |
|-----------|-------------|
| `cluster.name` | Unique name for this remote cluster |
| `connection.controllerEndpoint` | Hub controller gRPC endpoint |

### Key Values

| Parameter | Description | Default |
|-----------|-------------|---------|
| `cluster.region` | Geographic region | `""` |
| `cluster.zone` | Availability zone | `""` |
| `connection.mode` | Connection mode (Direct/Tunnel) | `Direct` |
| `tls.enabled` | Enable mTLS | `true` |
| `tls.caSecretName` | CA certificate secret name | `novaedge-ca` |
| `tls.clientCertSecretName` | Client cert secret name | `novaedge-agent-cert` |
| `vip.enabled` | Enable VIP management | `true` |
| `vip.mode` | VIP mode (L2/BGP/OSPF) | `L2` |

### Full Values Reference

See [values.yaml](values.yaml) for all available configuration options.

## Connection Modes

### Direct Mode

Agents connect directly to the hub controller via gRPC. Requires:
- Network connectivity from spoke to hub on the gRPC port
- DNS resolution or direct IP access
- Firewall rules allowing the connection

```yaml
connection:
  mode: Direct
  controllerEndpoint: novaedge-controller.example.com:9090
```

### Tunnel Mode

For environments where direct connectivity isn't available (NAT, firewalls):

```yaml
connection:
  mode: Tunnel
  controllerEndpoint: via-tunnel
tunnel:
  type: WireGuard
  relayEndpoint: relay.example.com:51820
  wireGuard:
    publicKey: "hub-public-key"
    endpoint: "relay.example.com:51820"
    allowedIPs:
      - 10.0.0.0/8
```

## Examples

### Basic Edge Cluster

```yaml
cluster:
  name: edge-west-1
  region: us-west-2

connection:
  controllerEndpoint: novaedge.hub.example.com:9090

tls:
  enabled: true
  caSecretName: novaedge-ca
  clientCertSecretName: novaedge-agent-cert

vip:
  enabled: true
  mode: L2
  interface: eth0
```

### Edge Cluster with BGP

```yaml
cluster:
  name: edge-dc-1
  region: datacenter-1

connection:
  controllerEndpoint: 10.0.1.100:9090

vip:
  enabled: true
  mode: BGP
  bgp:
    asn: 65001
    routerID: 10.0.2.1
    peers:
      - address: 10.0.1.1
        asn: 65000
        port: 179
```

### High-Resource Edge

```yaml
cluster:
  name: edge-high-traffic

resources:
  limits:
    cpu: 4000m
    memory: 4Gi
  requests:
    cpu: 2000m
    memory: 2Gi

nodeSelector:
  node-role.kubernetes.io/edge: "true"
```

## Troubleshooting

### Agent Not Connecting

1. Check agent logs:
   ```bash
   kubectl logs -n nova-system -l app.kubernetes.io/name=novaedge-agent
   ```

2. Verify network connectivity:
   ```bash
   kubectl exec -n nova-system <agent-pod> -- nc -zv <controller-endpoint> 9090
   ```

3. Check TLS certificates:
   ```bash
   kubectl get secrets -n nova-system
   kubectl describe secret novaedge-agent-cert -n nova-system
   ```

### Certificate Issues

1. Verify CA matches between hub and spoke:
   ```bash
   # On spoke cluster
   kubectl get secret novaedge-ca -n nova-system -o jsonpath='{.data.ca\.crt}' | base64 -d | openssl x509 -text -noout
   ```

2. Check certificate expiration:
   ```bash
   kubectl get secret novaedge-agent-cert -n nova-system -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -enddate -noout
   ```

## Uninstallation

```bash
helm uninstall novaedge-agent -n nova-system
kubectl delete namespace nova-system
```

## See Also

- [Multi-Cluster Guide](../../docs/user-guide/multi-cluster.md)
- [Operator Guide](../../docs/user-guide/operator.md)
- [NovaEdgeRemoteCluster CRD Reference](../../docs/reference/crd-reference.md)
