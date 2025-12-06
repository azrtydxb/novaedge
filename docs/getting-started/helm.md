# Helm Chart Installation

Deploy NovaEdge to Kubernetes using Helm for simplified installation and configuration management.

## Prerequisites

- Kubernetes 1.29+
- Helm 3.0+
- kubectl configured with cluster access

## Quick Install

```bash
# Clone the repository
git clone https://github.com/piwi3910/novaedge.git
cd novaedge

# Install NovaEdge
helm install novaedge ./charts/novaedge \
  --namespace novaedge-system \
  --create-namespace

# Verify installation
kubectl get pods -n novaedge-system
```

## Installation Options

### Default Installation

Deploys controller, agents, and web UI with default settings:

```bash
helm install novaedge ./charts/novaedge \
  --namespace novaedge-system \
  --create-namespace
```

### Custom Values

Create a `values.yaml` file with your customizations:

```yaml
# my-values.yaml
controller:
  replicaCount: 3
  metrics:
    serviceMonitor:
      enabled: true

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
```

```bash
helm install novaedge ./charts/novaedge \
  --namespace novaedge-system \
  --create-namespace \
  -f my-values.yaml
```

### Without Web UI

```bash
helm install novaedge ./charts/novaedge \
  --namespace novaedge-system \
  --create-namespace \
  --set webui.enabled=false
```

### Development Mode

For single-node development clusters:

```bash
helm install novaedge ./charts/novaedge \
  --namespace novaedge-system \
  --create-namespace \
  --set controller.replicaCount=1 \
  --set controller.podDisruptionBudget.enabled=false \
  --set agent.podDisruptionBudget.enabled=false
```

## Upgrading

```bash
# Update values and upgrade
helm upgrade novaedge ./charts/novaedge \
  --namespace novaedge-system \
  -f my-values.yaml

# Or upgrade with inline values
helm upgrade novaedge ./charts/novaedge \
  --namespace novaedge-system \
  --set controller.replicaCount=5
```

## Uninstalling

```bash
# Uninstall the release
helm uninstall novaedge --namespace novaedge-system

# Delete namespace (optional)
kubectl delete namespace novaedge-system

# CRDs are preserved by default. To remove them:
kubectl delete crds \
  proxybackends.novaedge.io \
  proxygateways.novaedge.io \
  proxypolicies.novaedge.io \
  proxyroutes.novaedge.io \
  proxyvips.novaedge.io
```

## What Gets Deployed

| Component | Type | Description |
|-----------|------|-------------|
| Controller | Deployment | Watches CRDs, builds config, pushes to agents |
| Agent | DaemonSet | L7 load balancing, VIP management |
| Web UI | Deployment | Dashboard for configuration (optional) |
| CRDs | CustomResourceDefinitions | ProxyVIP, ProxyGateway, ProxyRoute, ProxyBackend, ProxyPolicy |
| RBAC | ClusterRole/Binding | Permissions for controller and agents |
| Services | ClusterIP | Internal communication |

## Production Configuration

For production deployments, consider:

```yaml
# production-values.yaml
controller:
  replicaCount: 3
  resources:
    limits:
      cpu: 1000m
      memory: 1Gi
    requests:
      cpu: 200m
      memory: 256Mi
  metrics:
    serviceMonitor:
      enabled: true
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 5
    targetCPUUtilizationPercentage: 70
  podDisruptionBudget:
    enabled: true
    minAvailable: 1

agent:
  resources:
    limits:
      cpu: 4000m
      memory: 2Gi
    requests:
      cpu: 1000m
      memory: 512Mi
  metrics:
    serviceMonitor:
      enabled: true
  podDisruptionBudget:
    enabled: true
    maxUnavailable: 1

webui:
  enabled: true
  replicaCount: 2
  ingress:
    enabled: true
    className: nginx
    annotations:
      cert-manager.io/cluster-issuer: letsencrypt-prod
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

## Troubleshooting

### Check Installation Status

```bash
# Helm release status
helm status novaedge -n novaedge-system

# View deployed values
helm get values novaedge -n novaedge-system

# List all releases
helm list -n novaedge-system
```

### Check Pods

```bash
# All pods
kubectl get pods -n novaedge-system

# Controller logs
kubectl logs -n novaedge-system -l app.kubernetes.io/component=controller

# Agent logs
kubectl logs -n novaedge-system -l app.kubernetes.io/component=agent

# Web UI logs
kubectl logs -n novaedge-system -l app.kubernetes.io/component=webui
```

### Verify CRDs

```bash
kubectl get crds | grep novaedge.io
```

### Common Issues

**Pods pending due to resource constraints:**
```bash
# Reduce resource requests
helm upgrade novaedge ./charts/novaedge \
  --namespace novaedge-system \
  --set controller.resources.requests.cpu=50m \
  --set controller.resources.requests.memory=64Mi
```

**Agent not running on specific nodes:**
```bash
# Check tolerations and node selectors
kubectl describe daemonset -n novaedge-system novaedge-agent
```

**CRDs already exist:**
```bash
# Skip CRD installation
helm upgrade novaedge ./charts/novaedge \
  --namespace novaedge-system \
  --set crds.install=false
```

## Next Steps

- [Configure your first gateway](../user-guide/deployment-guide.md#step-6-configure-vip)
- [Explore Helm values reference](../reference/helm-values.md)
- [Set up monitoring](../user-guide/deployment-guide.md#observability-integration)
