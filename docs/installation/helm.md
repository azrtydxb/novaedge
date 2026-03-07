# Helm Installation

Detailed guide for installing NovaEdge using Helm charts.

## Available Charts

| Chart | Purpose |
|-------|---------|
| `novaedge-operator` | Deploy the NovaEdge Operator |
| `novaedge` | Deploy NovaEdge directly (without operator) |
| `novaedge-agent` | Deploy agent-only (for hub-spoke) |

## Operator Chart

The recommended approach for production deployments.

### Installation

```bash
helm install novaedge-operator ./charts/novaedge-operator \
  --namespace nova-system \
  --create-namespace
```

### Configuration

```yaml
# values.yaml for novaedge-operator
replicaCount: 1

image:
  repository: ghcr.io/azrtydxb/novaedge-operator
  tag: "v0.1.0"
  pullPolicy: IfNotPresent

resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi

# Create default NovaEdgeCluster on install
createDefaultCluster: false
defaultCluster:
  version: "v0.1.0"
  controller:
    replicas: 1
  agent:
    vip:
      enabled: true
      mode: L2
```

### After Installation

Create a `NovaEdgeCluster` to deploy components:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeCluster
metadata:
  name: novaedge
  namespace: nova-system
spec:
  version: "v0.1.0"
  controller:
    replicas: 1
  agent:
    hostNetwork: true
    vip:
      enabled: true
      mode: L2
```

## NovaEdge Chart (Direct)

Deploy NovaEdge components directly without the operator.

### Installation

```bash
helm install novaedge ./charts/novaedge \
  --namespace nova-system \
  --create-namespace
```

### Key Values

```yaml
# values.yaml for novaedge
controller:
  replicas: 1
  image:
    repository: ghcr.io/azrtydxb/novaedge-controller
    tag: "v0.1.0"
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
  leaderElection: true

agent:
  image:
    repository: ghcr.io/azrtydxb/novaedge-agent
    tag: "v0.1.0"
  hostNetwork: true
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
  vip:
    enabled: true
    mode: L2  # L2, BGP, or OSPF
  # For BGP mode
  bgp:
    asn: 65000
    peers: []

webUI:
  enabled: false
  replicas: 1
  service:
    type: ClusterIP

metrics:
  enabled: true
  serviceMonitor:
    enabled: false

tracing:
  enabled: false
  endpoint: ""
```

### Common Configurations

#### Production with HA

```bash
helm install novaedge ./charts/novaedge \
  --namespace nova-system \
  --create-namespace \
  --set controller.replicas=3 \
  --set controller.resources.requests.cpu=500m \
  --set controller.resources.requests.memory=512Mi \
  --set agent.resources.requests.cpu=500m \
  --set agent.resources.requests.memory=512Mi
```

#### With BGP

```bash
helm install novaedge ./charts/novaedge \
  --namespace nova-system \
  --create-namespace \
  --set agent.vip.mode=BGP \
  --set agent.bgp.asn=65000 \
  --set "agent.bgp.peers[0].address=10.0.0.254" \
  --set "agent.bgp.peers[0].asn=65001"
```

#### With Observability

```bash
helm install novaedge ./charts/novaedge \
  --namespace nova-system \
  --create-namespace \
  --set metrics.enabled=true \
  --set metrics.serviceMonitor.enabled=true \
  --set tracing.enabled=true \
  --set tracing.endpoint=jaeger:4317
```

#### With Web UI

```bash
helm install novaedge ./charts/novaedge \
  --namespace nova-system \
  --create-namespace \
  --set webUI.enabled=true \
  --set webUI.service.type=LoadBalancer
```

## Agent-Only Chart

For hub-spoke multi-cluster deployments.

### Installation

```bash
helm install novaedge-agent ./charts/novaedge-agent \
  --namespace nova-system \
  --create-namespace \
  --set controller.address=hub-controller.example.com:9090
```

### Configuration

```yaml
# values.yaml for novaedge-agent
controller:
  address: "hub-controller:9090"
  tls:
    enabled: true
    secretName: agent-tls-secret

image:
  repository: ghcr.io/azrtydxb/novaedge-agent
  tag: "v0.1.0"

hostNetwork: true

resources:
  requests:
    cpu: 200m
    memory: 256Mi

vip:
  enabled: true
  mode: L2

clusterName: "spoke-cluster-1"
```

## Values Reference

### Controller Values

| Value | Default | Description |
|-------|---------|-------------|
| `controller.replicas` | 1 | Number of controller replicas |
| `controller.image.repository` | ghcr.io/azrtydxb/novaedge-controller | Image repository |
| `controller.image.tag` | v0.1.0 | Image tag |
| `controller.leaderElection` | true | Enable leader election |
| `controller.grpcPort` | 9090 | gRPC server port |
| `controller.metricsPort` | 8080 | Metrics port |
| `controller.resources` | {} | Resource requirements |
| `controller.nodeSelector` | {} | Node selector |
| `controller.tolerations` | [] | Pod tolerations |
| `controller.affinity` | {} | Pod affinity |

### Agent Values

| Value | Default | Description |
|-------|---------|-------------|
| `agent.image.repository` | ghcr.io/azrtydxb/novaedge-agent | Image repository |
| `agent.image.tag` | v0.1.0 | Image tag |
| `agent.hostNetwork` | true | Use host networking |
| `agent.httpPort` | 80 | HTTP traffic port |
| `agent.httpsPort` | 443 | HTTPS traffic port |
| `agent.metricsPort` | 9090 | Metrics port |
| `agent.vip.enabled` | true | Enable VIP management |
| `agent.vip.mode` | L2 | VIP mode (L2/BGP/OSPF) |
| `agent.vip.interface` | "" | Network interface |
| `agent.bgp.asn` | 65000 | Local BGP ASN |
| `agent.bgp.routerID` | "" | BGP router ID |
| `agent.bgp.peers` | [] | BGP peer list |
| `agent.resources` | {} | Resource requirements |
| `agent.nodeSelector` | {} | Node selector |
| `agent.tolerations` | [] | Pod tolerations |
| `agent.updateStrategy` | RollingUpdate | Update strategy |

### Web UI Values

| Value | Default | Description |
|-------|---------|-------------|
| `webUI.enabled` | false | Enable Web UI |
| `webUI.replicas` | 1 | Number of replicas |
| `webUI.port` | 9080 | Web UI port |
| `webUI.readOnly` | false | Read-only mode |
| `webUI.service.type` | ClusterIP | Service type |
| `webUI.ingress.enabled` | false | Enable ingress |
| `webUI.ingress.host` | "" | Ingress hostname |

### Observability Values

| Value | Default | Description |
|-------|---------|-------------|
| `metrics.enabled` | true | Enable Prometheus metrics |
| `metrics.serviceMonitor.enabled` | false | Create ServiceMonitor |
| `metrics.serviceMonitor.interval` | 30s | Scrape interval |
| `tracing.enabled` | false | Enable tracing |
| `tracing.endpoint` | "" | OTLP endpoint |
| `tracing.samplingRate` | 10 | Sampling rate (0-100) |

## Upgrading

```bash
# Update values and upgrade
helm upgrade novaedge ./charts/novaedge \
  --namespace nova-system \
  --set controller.image.tag=v0.2.0 \
  --set agent.image.tag=v0.2.0

# Upgrade with values file
helm upgrade novaedge ./charts/novaedge \
  --namespace nova-system \
  -f production-values.yaml
```

## Uninstallation

```bash
# Remove NovaEdge resources first
kubectl delete proxyvips,proxygateways,proxyroutes,proxybackends,proxypolicies --all -A

# Uninstall chart
helm uninstall novaedge -n nova-system

# Remove CRDs (optional)
kubectl delete crd proxyvips.novaedge.io proxygateways.novaedge.io \
  proxyroutes.novaedge.io proxybackends.novaedge.io proxypolicies.novaedge.io
```

## Next Steps

- [Standalone Mode](standalone.md) - Non-Kubernetes deployment
- [Quick Start](../getting-started/quickstart.md) - Create your first gateway
- [Helm Values Reference](../reference/helm-values.md) - Complete values reference
