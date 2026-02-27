# NovaEdge Helm Chart

A Helm chart for deploying NovaEdge - a Kubernetes-native distributed load balancer, reverse proxy, and VIP controller.

## Prerequisites

- Kubernetes 1.29+
- Helm 3.0+

## Installation

### From Local Directory

```bash
# Clone the repository
git clone https://github.com/piwi3910/novaedge.git
cd novaedge

# Install with default values
helm install novaedge ./charts/novaedge \
  --namespace nova-system \
  --create-namespace

# Install with custom values
helm install novaedge ./charts/novaedge \
  --namespace nova-system \
  --create-namespace \
  -f my-values.yaml
```

### Upgrading

```bash
helm upgrade novaedge ./charts/novaedge \
  --namespace nova-system
```

### Uninstalling

```bash
helm uninstall novaedge --namespace nova-system

# CRDs are preserved by default, to remove them:
kubectl delete crds proxybackends.novaedge.io proxygateways.novaedge.io \
  proxypolicies.novaedge.io proxyroutes.novaedge.io proxyvips.novaedge.io
```

## Configuration

### Global Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `global.imagePullSecrets` | Image pull secrets for private registries | `[]` |
| `global.namespace` | Override the deployment namespace | `""` |

### Controller

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.enabled` | Deploy the controller | `true` |
| `controller.replicaCount` | Number of controller replicas | `3` |
| `controller.image.repository` | Controller image repository | `ghcr.io/piwi3910/novaedge-controller` |
| `controller.image.tag` | Controller image tag | Chart appVersion |
| `controller.image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `controller.leaderElection.enabled` | Enable leader election for HA | `true` |
| `controller.metrics.enabled` | Enable Prometheus metrics | `true` |
| `controller.metrics.port` | Metrics port | `8080` |
| `controller.metrics.serviceMonitor.enabled` | Create ServiceMonitor | `false` |
| `controller.grpc.port` | gRPC server port for agents | `8082` |
| `controller.resources` | Resource limits and requests | See values.yaml |
| `controller.podDisruptionBudget.enabled` | Enable PDB | `true` |
| `controller.autoscaling.enabled` | Enable HPA | `false` |

### Agent (DaemonSet)

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.enabled` | Deploy the agent DaemonSet | `true` |
| `agent.image.repository` | Agent image repository | `ghcr.io/piwi3910/novaedge-agent` |
| `agent.image.tag` | Agent image tag | Chart appVersion |
| `agent.image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `agent.ports.http` | HTTP proxy port | `80` |
| `agent.ports.https` | HTTPS proxy port | `443` |
| `agent.metrics.enabled` | Enable Prometheus metrics | `true` |
| `agent.metrics.port` | Metrics port | `9090` |
| `agent.healthProbe.port` | Health probe port | `9091` |
| `agent.resources` | Resource limits and requests | See values.yaml |
| `agent.tolerations` | Node tolerations | Control plane tolerations |
| `agent.updateStrategy` | DaemonSet update strategy | `RollingUpdate` |
| `agent.podDisruptionBudget.enabled` | Enable PDB | `true` |

### Web UI

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webui.enabled` | Deploy the web UI | `true` |
| `webui.replicaCount` | Number of web UI replicas | `1` |
| `webui.image.repository` | Web UI image repository | `ghcr.io/piwi3910/novaedge-novactl` |
| `webui.image.tag` | Web UI image tag | Chart appVersion |
| `webui.service.type` | Service type | `ClusterIP` |
| `webui.service.port` | Service port | `80` |
| `webui.ingress.enabled` | Enable Ingress | `false` |
| `webui.ingress.className` | Ingress class name | `""` |
| `webui.ingress.hosts` | Ingress hosts | See values.yaml |
| `webui.prometheus.endpoint` | Prometheus endpoint for metrics | `""` |
| `webui.resources` | Resource limits and requests | See values.yaml |

### CRDs

| Parameter | Description | Default |
|-----------|-------------|---------|
| `crds.install` | Install CRDs with the chart | `true` |
| `crds.keep` | Keep CRDs on chart uninstall | `true` |

### RBAC

| Parameter | Description | Default |
|-----------|-------------|---------|
| `rbac.create` | Create RBAC resources | `true` |

## Examples

### Minimal Installation

```bash
helm install novaedge ./charts/novaedge \
  --namespace nova-system \
  --create-namespace
```

### Production Setup with Monitoring

```yaml
# production-values.yaml
controller:
  replicaCount: 3
  metrics:
    serviceMonitor:
      enabled: true
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 5

agent:
  metrics:
    serviceMonitor:
      enabled: true

webui:
  enabled: true
  ingress:
    enabled: true
    className: nginx
    hosts:
      - host: novaedge.example.com
        paths:
          - path: /
            pathType: Prefix
    tls:
      - secretName: novaedge-tls
        hosts:
          - novaedge.example.com
  prometheus:
    endpoint: http://prometheus.monitoring.svc.cluster.local:9090
```

```bash
helm install novaedge ./charts/novaedge \
  --namespace nova-system \
  --create-namespace \
  -f production-values.yaml
```

### Development Setup (Single Node)

```yaml
# dev-values.yaml
controller:
  replicaCount: 1
  podDisruptionBudget:
    enabled: false

agent:
  podDisruptionBudget:
    enabled: false

webui:
  enabled: true
```

```bash
helm install novaedge ./charts/novaedge \
  --namespace nova-system \
  --create-namespace \
  -f dev-values.yaml
```

### Without Web UI

```bash
helm install novaedge ./charts/novaedge \
  --namespace nova-system \
  --create-namespace \
  --set webui.enabled=false
```

## Components Deployed

The chart deploys the following components:

1. **Controller (Deployment)**
   - Watches CRDs and Kubernetes resources
   - Builds configuration and pushes to agents
   - Handles leader election for HA

2. **Agent (DaemonSet)**
   - Runs on every node (or selected nodes)
   - Handles L7 load balancing and routing
   - Manages VIPs (L2 ARP, BGP, OSPF)

3. **Web UI (Deployment)** - Optional
   - Dashboard for configuration management
   - Metrics visualization
   - Real-time monitoring

4. **CRDs**
   - ProxyVIP
   - ProxyGateway
   - ProxyRoute
   - ProxyBackend
   - ProxyPolicy

## Troubleshooting

### Check Component Status

```bash
# Controller
kubectl get pods -n nova-system -l app.kubernetes.io/component=controller
kubectl logs -n nova-system -l app.kubernetes.io/component=controller

# Agent
kubectl get pods -n nova-system -l app.kubernetes.io/component=agent
kubectl logs -n nova-system -l app.kubernetes.io/component=agent

# Web UI
kubectl get pods -n nova-system -l app.kubernetes.io/component=webui
```

### Verify CRDs

```bash
kubectl get crds | grep novaedge.io
```

### Check Helm Release

```bash
helm status novaedge -n nova-system
helm get values novaedge -n nova-system
```

## License

Apache 2.0

## Links

- [Documentation](https://piwi3910.github.io/novaedge/)
- [GitHub Repository](https://github.com/piwi3910/novaedge)
- [Issue Tracker](https://github.com/piwi3910/novaedge/issues)
