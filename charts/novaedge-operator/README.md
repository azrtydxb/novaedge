# NovaEdge Operator Helm Chart

A Helm chart for deploying the NovaEdge Operator, which manages NovaEdge clusters on Kubernetes.

## Introduction

The NovaEdge Operator provides a declarative way to deploy and manage NovaEdge clusters using Kubernetes Custom Resources. It handles:

- Deployment and lifecycle management of NovaEdge Controller
- Deployment and lifecycle management of NovaEdge Agent DaemonSet
- Optional deployment of NovaEdge Web UI
- RBAC configuration for all components
- Health monitoring and automatic recovery
- Rolling upgrades with configurable strategies

## Prerequisites

- Kubernetes 1.25+
- Helm 3.0+
- kubectl configured to access your cluster

## Installation

### Add the NovaEdge Helm repository

```bash
helm repo add novaedge https://piwi3910.github.io/novaedge/charts
helm repo update
```

### Install the operator

```bash
# Create namespace
kubectl create namespace novaedge-system

# Install the operator
helm install novaedge-operator novaedge/novaedge-operator \
  --namespace novaedge-system
```

### Install from source

```bash
# Clone the repository
git clone https://github.com/piwi3910/novaedge.git
cd novaedge

# Install the operator
helm install novaedge-operator charts/novaedge-operator \
  --namespace novaedge-system \
  --create-namespace
```

## Deploying a NovaEdge Cluster

After installing the operator, create a `NovaEdgeCluster` resource:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: NovaEdgeCluster
metadata:
  name: novaedge
  namespace: novaedge-system
spec:
  version: "v0.1.0"

  controller:
    replicas: 1
    leaderElection: true
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "500m"
        memory: "512Mi"

  agent:
    hostNetwork: true
    httpPort: 80
    httpsPort: 443
    vip:
      enabled: true
      mode: L2

  webUI:
    enabled: true
    replicas: 1
    service:
      type: ClusterIP
```

Apply the resource:

```bash
kubectl apply -f novaedgecluster.yaml
```

## Configuration

### Operator Values

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of operator replicas | `1` |
| `image.repository` | Operator image repository | `ghcr.io/piwi3910/novaedge/novaedge-operator` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `image.tag` | Image tag | Chart appVersion |
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.name` | Service account name | `""` |
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `128Mi` |
| `resources.requests.cpu` | CPU request | `10m` |
| `resources.requests.memory` | Memory request | `64Mi` |
| `leaderElection.enabled` | Enable leader election | `true` |
| `metrics.enabled` | Enable Prometheus metrics | `true` |
| `metrics.port` | Metrics port | `8080` |
| `metrics.serviceMonitor.enabled` | Create ServiceMonitor | `false` |
| `rbac.create` | Create RBAC resources | `true` |
| `crds.install` | Install CRDs | `true` |
| `crds.keep` | Keep CRDs on uninstall | `true` |

### NovaEdgeCluster CRD

The `NovaEdgeCluster` CRD supports the following configuration:

#### Spec Fields

| Field | Description |
|-------|-------------|
| `version` | NovaEdge version to deploy (required) |
| `imageRepository` | Container image repository |
| `imagePullPolicy` | Image pull policy |
| `controller` | Controller configuration |
| `agent` | Agent DaemonSet configuration |
| `webUI` | Web UI configuration (optional) |
| `tls` | Internal TLS configuration |
| `observability` | Metrics, tracing, logging configuration |

#### Controller Configuration

| Field | Description | Default |
|-------|-------------|---------|
| `replicas` | Number of controller replicas | `1` |
| `leaderElection` | Enable leader election | `true` |
| `grpcPort` | gRPC config server port | `9090` |
| `metricsPort` | Prometheus metrics port | `8080` |
| `healthPort` | Health probe port | `8081` |
| `resources` | Resource requirements | - |
| `nodeSelector` | Node selector | - |
| `tolerations` | Pod tolerations | - |
| `affinity` | Pod affinity | - |

#### Agent Configuration

| Field | Description | Default |
|-------|-------------|---------|
| `hostNetwork` | Enable host networking | `true` |
| `httpPort` | HTTP traffic port | `80` |
| `httpsPort` | HTTPS traffic port | `443` |
| `metricsPort` | Prometheus metrics port | `9090` |
| `healthPort` | Health probe port | `8080` |
| `vip.enabled` | Enable VIP management | `true` |
| `vip.mode` | VIP mode (L2, BGP, OSPF) | `L2` |
| `updateStrategy` | DaemonSet update strategy | - |

#### Web UI Configuration

| Field | Description | Default |
|-------|-------------|---------|
| `enabled` | Enable web UI | `true` |
| `replicas` | Number of replicas | `1` |
| `port` | Web UI port | `9080` |
| `readOnly` | Read-only mode | `false` |
| `service.type` | Service type | `ClusterIP` |
| `ingress.enabled` | Enable ingress | `false` |
| `prometheusEndpoint` | Prometheus URL for metrics | - |

## Monitoring

### Prometheus Metrics

Enable ServiceMonitor for Prometheus Operator:

```bash
helm upgrade novaedge-operator novaedge/novaedge-operator \
  --set metrics.serviceMonitor.enabled=true \
  --set metrics.serviceMonitor.labels.release=prometheus
```

### Checking Cluster Status

```bash
# Get cluster status
kubectl get novaedgeclusters -n novaedge-system

# Describe cluster details
kubectl describe novaedgecluster novaedge -n novaedge-system

# Check component status
kubectl get pods -n novaedge-system -l app.kubernetes.io/managed-by=novaedge-operator
```

## Upgrading

### Upgrade the operator

```bash
helm upgrade novaedge-operator novaedge/novaedge-operator \
  --namespace novaedge-system
```

### Upgrade a NovaEdge cluster

Update the `version` field in your NovaEdgeCluster resource:

```yaml
spec:
  version: "v0.2.0"
```

The operator will perform a rolling upgrade of all components.

## Uninstalling

### Remove NovaEdge clusters

```bash
kubectl delete novaedgeclusters --all -n novaedge-system
```

### Uninstall the operator

```bash
helm uninstall novaedge-operator -n novaedge-system
```

### Remove CRDs (optional)

```bash
kubectl delete crd novaedgeclusters.novaedge.io
```

## Troubleshooting

### Check operator logs

```bash
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-operator
```

### Check controller logs

```bash
kubectl logs -n novaedge-system -l app.kubernetes.io/component=controller
```

### Check agent logs

```bash
kubectl logs -n novaedge-system -l app.kubernetes.io/component=agent
```

### Common issues

1. **Operator not starting**: Check RBAC permissions and image availability
2. **Components not ready**: Check resource constraints and node availability
3. **VIP not working**: Ensure agents have NET_ADMIN capability and host network access

## License

Apache License 2.0
