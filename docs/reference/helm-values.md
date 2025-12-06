# Helm Values Reference

Complete reference for all configurable values in the NovaEdge Helm chart.

## Global Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `global.imagePullSecrets` | List of image pull secrets for private registries | `[]` |
| `global.namespace` | Override the deployment namespace | `""` |

```yaml
global:
  imagePullSecrets:
    - name: my-registry-secret
  namespace: custom-namespace
```

---

## Controller

The controller watches Kubernetes resources and pushes configuration to agents.

### Basic Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.enabled` | Deploy the controller | `true` |
| `controller.replicaCount` | Number of controller replicas | `3` |

### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.image.repository` | Controller image repository | `ghcr.io/piwi3910/novaedge-controller` |
| `controller.image.tag` | Controller image tag | Chart appVersion |
| `controller.image.pullPolicy` | Image pull policy | `IfNotPresent` |

### Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.serviceAccount.create` | Create service account | `true` |
| `controller.serviceAccount.name` | Service account name (auto-generated if empty) | `""` |
| `controller.serviceAccount.annotations` | Service account annotations | `{}` |

### High Availability

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.leaderElection.enabled` | Enable leader election | `true` |

### Metrics

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.metrics.enabled` | Enable Prometheus metrics | `true` |
| `controller.metrics.port` | Metrics port | `8080` |
| `controller.metrics.serviceMonitor.enabled` | Create ServiceMonitor for Prometheus Operator | `false` |
| `controller.metrics.serviceMonitor.interval` | Scrape interval | `30s` |
| `controller.metrics.serviceMonitor.scrapeTimeout` | Scrape timeout | `10s` |

### Probes

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.healthProbe.port` | Health probe port | `8081` |

### gRPC

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.grpc.port` | gRPC server port for agent communication | `8082` |

### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.resources.limits.cpu` | CPU limit | `500m` |
| `controller.resources.limits.memory` | Memory limit | `512Mi` |
| `controller.resources.requests.cpu` | CPU request | `100m` |
| `controller.resources.requests.memory` | Memory request | `128Mi` |

### Scheduling

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.nodeSelector` | Node selector | `{}` |
| `controller.tolerations` | Tolerations | `[]` |
| `controller.affinity` | Affinity rules | `{}` |

### Pod Disruption Budget

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.podDisruptionBudget.enabled` | Enable PDB | `true` |
| `controller.podDisruptionBudget.minAvailable` | Minimum available pods | `1` |

### Autoscaling

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controller.autoscaling.enabled` | Enable HPA | `false` |
| `controller.autoscaling.minReplicas` | Minimum replicas | `2` |
| `controller.autoscaling.maxReplicas` | Maximum replicas | `5` |
| `controller.autoscaling.targetCPUUtilizationPercentage` | Target CPU utilization | `80` |

---

## Agent

The agent runs as a DaemonSet on each node, handling L7 load balancing and VIP management.

### Basic Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.enabled` | Deploy the agent DaemonSet | `true` |

### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.image.repository` | Agent image repository | `ghcr.io/piwi3910/novaedge-agent` |
| `agent.image.tag` | Agent image tag | Chart appVersion |
| `agent.image.pullPolicy` | Image pull policy | `IfNotPresent` |

### Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.serviceAccount.create` | Create service account | `true` |
| `agent.serviceAccount.name` | Service account name (auto-generated if empty) | `""` |
| `agent.serviceAccount.annotations` | Service account annotations | `{}` |

### Scheduling

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.nodeSelector` | Node selector for agent pods | `{}` |
| `agent.tolerations` | Tolerations (includes control-plane by default) | See below |

Default tolerations allow agents to run on control-plane nodes:
```yaml
tolerations:
  - key: node-role.kubernetes.io/control-plane
    operator: Exists
    effect: NoSchedule
  - key: node-role.kubernetes.io/master
    operator: Exists
    effect: NoSchedule
```

### Ports

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.ports.http` | HTTP proxy port | `80` |
| `agent.ports.https` | HTTPS proxy port | `443` |

### Metrics

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.metrics.enabled` | Enable Prometheus metrics | `true` |
| `agent.metrics.port` | Metrics port | `9090` |
| `agent.metrics.serviceMonitor.enabled` | Create ServiceMonitor | `false` |
| `agent.metrics.serviceMonitor.interval` | Scrape interval | `30s` |
| `agent.metrics.serviceMonitor.scrapeTimeout` | Scrape timeout | `10s` |

### Probes

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.healthProbe.port` | Health probe port | `9091` |

### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.resources.limits.cpu` | CPU limit | `2000m` |
| `agent.resources.limits.memory` | Memory limit | `1Gi` |
| `agent.resources.requests.cpu` | CPU request | `500m` |
| `agent.resources.requests.memory` | Memory request | `256Mi` |

### Update Strategy

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.updateStrategy.type` | Update strategy type | `RollingUpdate` |
| `agent.updateStrategy.rollingUpdate.maxUnavailable` | Max unavailable during update | `1` |

### Pod Disruption Budget

| Parameter | Description | Default |
|-----------|-------------|---------|
| `agent.podDisruptionBudget.enabled` | Enable PDB | `true` |
| `agent.podDisruptionBudget.maxUnavailable` | Maximum unavailable pods | `1` |

---

## Web UI

The web UI provides a dashboard for configuration management and monitoring.

### Basic Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webui.enabled` | Deploy the web UI | `true` |
| `webui.replicaCount` | Number of web UI replicas | `1` |

### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webui.image.repository` | Web UI image repository | `ghcr.io/piwi3910/novaedge-novactl` |
| `webui.image.tag` | Web UI image tag | Chart appVersion |
| `webui.image.pullPolicy` | Image pull policy | `IfNotPresent` |

### Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webui.serviceAccount.create` | Create service account | `true` |
| `webui.serviceAccount.name` | Service account name | `""` |
| `webui.serviceAccount.annotations` | Service account annotations | `{}` |

### Service

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webui.service.type` | Service type | `ClusterIP` |
| `webui.service.port` | Service port | `80` |
| `webui.service.targetPort` | Target port | `9080` |

### Ingress

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webui.ingress.enabled` | Enable Ingress | `false` |
| `webui.ingress.className` | Ingress class name | `""` |
| `webui.ingress.annotations` | Ingress annotations | `{}` |
| `webui.ingress.hosts` | Ingress hosts configuration | See below |
| `webui.ingress.tls` | TLS configuration | `[]` |

Default hosts:
```yaml
hosts:
  - host: novaedge.local
    paths:
      - path: /
        pathType: Prefix
```

### Prometheus Integration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webui.prometheus.endpoint` | Prometheus endpoint URL | `""` |

Example:
```yaml
webui:
  prometheus:
    endpoint: http://prometheus.monitoring.svc.cluster.local:9090
```

### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webui.resources.limits.cpu` | CPU limit | `200m` |
| `webui.resources.limits.memory` | Memory limit | `256Mi` |
| `webui.resources.requests.cpu` | CPU request | `50m` |
| `webui.resources.requests.memory` | Memory request | `64Mi` |

### Scheduling

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webui.nodeSelector` | Node selector | `{}` |
| `webui.tolerations` | Tolerations | `[]` |
| `webui.affinity` | Affinity rules | `{}` |

---

## CRDs

| Parameter | Description | Default |
|-----------|-------------|---------|
| `crds.install` | Install CRDs with the chart | `true` |
| `crds.keep` | Keep CRDs when uninstalling chart | `true` |

---

## RBAC

| Parameter | Description | Default |
|-----------|-------------|---------|
| `rbac.create` | Create RBAC resources | `true` |

---

## Example Configurations

### Minimal Development

```yaml
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

### Production with Monitoring

```yaml
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
  resources:
    limits:
      cpu: 4000m
      memory: 2Gi
    requests:
      cpu: 1000m
      memory: 512Mi

webui:
  enabled: true
  replicaCount: 2
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

### Agent on Specific Nodes

```yaml
agent:
  nodeSelector:
    novaedge.io/role: edge
  tolerations:
    - key: novaedge.io/dedicated
      operator: Equal
      value: edge
      effect: NoSchedule
```

### Private Registry

```yaml
global:
  imagePullSecrets:
    - name: my-registry-secret

controller:
  image:
    repository: my-registry.example.com/novaedge-controller
    tag: v1.0.0

agent:
  image:
    repository: my-registry.example.com/novaedge-agent
    tag: v1.0.0

webui:
  image:
    repository: my-registry.example.com/novaedge-novactl
    tag: v1.0.0
```
