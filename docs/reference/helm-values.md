# Helm Values Reference

Complete reference for all configurable values in the NovaEdge Helm charts.

NovaEdge provides two Helm charts:

1. **novaedge-operator** - Deploys the NovaEdge Operator which manages the lifecycle of NovaEdge components (recommended)
2. **novaedge** - Directly deploys NovaEdge components without the operator

---

## NovaEdge Operator Chart

The operator chart (`charts/novaedge-operator`) deploys the NovaEdge Operator, which then manages the controller, agents, and web UI through the `NovaEdgeCluster` CRD.

### Basic Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of operator replicas | `1` |
| `nameOverride` | Override the chart name | `""` |
| `fullnameOverride` | Override the full name | `""` |

### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Operator image repository | `ghcr.io/piwi3910/novaedge/novaedge-operator` |
| `image.tag` | Image tag (defaults to chart appVersion) | `""` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `imagePullSecrets` | List of image pull secrets | `[]` |

### Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create a service account | `true` |
| `serviceAccount.annotations` | Service account annotations | `{}` |
| `serviceAccount.name` | Service account name | `""` |

### Security

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podSecurityContext.runAsNonRoot` | Run as non-root user | `true` |
| `podSecurityContext.seccompProfile.type` | Seccomp profile | `RuntimeDefault` |
| `securityContext.allowPrivilegeEscalation` | Allow privilege escalation | `false` |
| `securityContext.capabilities.drop` | Dropped capabilities | `["ALL"]` |
| `securityContext.readOnlyRootFilesystem` | Read-only root filesystem | `true` |

### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `128Mi` |
| `resources.requests.cpu` | CPU request | `10m` |
| `resources.requests.memory` | Memory request | `64Mi` |

### Probes

| Parameter | Description | Default |
|-----------|-------------|---------|
| `livenessProbe.httpGet.path` | Liveness probe path | `/healthz` |
| `livenessProbe.httpGet.port` | Liveness probe port | `8081` |
| `livenessProbe.initialDelaySeconds` | Initial delay | `15` |
| `livenessProbe.periodSeconds` | Period seconds | `20` |
| `readinessProbe.httpGet.path` | Readiness probe path | `/readyz` |
| `readinessProbe.httpGet.port` | Readiness probe port | `8081` |
| `readinessProbe.initialDelaySeconds` | Initial delay | `5` |
| `readinessProbe.periodSeconds` | Period seconds | `10` |

### Leader Election

| Parameter | Description | Default |
|-----------|-------------|---------|
| `leaderElection.enabled` | Enable leader election | `true` |

### Metrics

| Parameter | Description | Default |
|-----------|-------------|---------|
| `metrics.enabled` | Enable Prometheus metrics | `true` |
| `metrics.port` | Metrics service port | `8080` |
| `metrics.serviceMonitor.enabled` | Create ServiceMonitor | `false` |
| `metrics.serviceMonitor.interval` | Scrape interval | `30s` |
| `metrics.serviceMonitor.labels` | Additional ServiceMonitor labels | `{}` |

### Health

| Parameter | Description | Default |
|-----------|-------------|---------|
| `health.port` | Health probe port | `8081` |

### Scheduling

| Parameter | Description | Default |
|-----------|-------------|---------|
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Tolerations | `[]` |
| `affinity` | Affinity rules | `{}` |
| `podAnnotations` | Pod annotations | `{}` |
| `podLabels` | Pod labels | `{}` |

### CRDs

| Parameter | Description | Default |
|-----------|-------------|---------|
| `crds.install` | Install CRDs with the chart | `true` |
| `crds.keep` | Keep CRDs on uninstall | `true` |

### RBAC

| Parameter | Description | Default |
|-----------|-------------|---------|
| `rbac.create` | Create RBAC resources | `true` |

### Additional Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `extraEnv` | Additional environment variables | `[]` |
| `extraArgs` | Additional command-line arguments | `[]` |
| `extraVolumes` | Additional volumes | `[]` |
| `extraVolumeMounts` | Additional volume mounts | `[]` |

### Operator Chart Example

```yaml
# Production operator configuration
replicaCount: 1

image:
  repository: ghcr.io/piwi3910/novaedge/novaedge-operator
  tag: v0.1.0

resources:
  limits:
    cpu: 500m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi

metrics:
  enabled: true
  serviceMonitor:
    enabled: true
    interval: 30s

leaderElection:
  enabled: true

nodeSelector:
  node-role.kubernetes.io/control-plane: ""

tolerations:
  - key: node-role.kubernetes.io/control-plane
    operator: Exists
    effect: NoSchedule
```

---

## NovaEdge Chart (Direct Deployment)

The main chart (`charts/novaedge`) directly deploys NovaEdge components without using the operator.

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

## Federation (Multi-Controller)

Federation settings enable active-active multi-controller deployments with automatic state synchronization and split-brain protection.

### Basic Federation Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `federation.enabled` | Enable federation mode | `false` |
| `federation.federationID` | Unique federation ID (same across all controllers) | `""` |
| `federation.paused` | Temporarily pause federation sync | `false` |

### Local Member Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `federation.localMember.name` | Unique name for this controller | `""` |
| `federation.localMember.region` | Geographic region | `""` |
| `federation.localMember.zone` | Availability zone | `""` |
| `federation.localMember.endpoint` | gRPC endpoint for federation | `""` |

### Peer Members

| Parameter | Description | Default |
|-----------|-------------|---------|
| `federation.members` | List of peer controllers | `[]` |

Each member in the list supports:

| Field | Description |
|-------|-------------|
| `name` | Unique peer name |
| `endpoint` | gRPC endpoint (host:port) |
| `region` | Geographic region |
| `tls.enabled` | Enable mTLS |
| `tls.caSecretRef.name` | CA certificate secret |
| `tls.clientCertSecretRef.name` | Client certificate secret |

### Synchronization Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `federation.sync.interval` | Sync interval | `5s` |
| `federation.sync.timeout` | Sync timeout | `30s` |
| `federation.sync.batchSize` | Max resources per batch | `100` |
| `federation.sync.compression` | Enable compression | `true` |

### Conflict Resolution

| Parameter | Description | Default |
|-----------|-------------|---------|
| `federation.conflictResolution.strategy` | Strategy: `LastWriterWins`, `Merge`, `Manual` | `LastWriterWins` |
| `federation.conflictResolution.vectorClocks` | Enable vector clocks | `true` |
| `federation.conflictResolution.tombstoneTTL` | Deletion record TTL | `24h` |

### Health Check

| Parameter | Description | Default |
|-----------|-------------|---------|
| `federation.healthCheck.interval` | Peer health check interval | `10s` |
| `federation.healthCheck.timeout` | Health check timeout | `5s` |
| `federation.healthCheck.failureThreshold` | Failures before unhealthy | `3` |
| `federation.healthCheck.successThreshold` | Successes before healthy | `1` |

### Split-Brain Detection

| Parameter | Description | Default |
|-----------|-------------|---------|
| `federation.splitBrain.enabled` | Enable split-brain detection | `true` |
| `federation.splitBrain.partitionTimeout` | Time before declaring partition | `30s` |
| `federation.splitBrain.quorumMode` | `Controllers` or `AgentAssisted` | `Controllers` |
| `federation.splitBrain.quorumRequired` | Require quorum for writes | `false` |
| `federation.splitBrain.fencingEnabled` | Block writes during partition | `false` |
| `federation.splitBrain.healingGracePeriod` | Grace period after partition heals | `5s` |
| `federation.splitBrain.autoResolveOnHeal` | Auto-resolve conflicts on heal | `true` |

### Agent-Assisted Quorum

For 2-datacenter deployments, use agent-assisted quorum:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `federation.splitBrain.agentQuorum.controllerWeight` | Voting weight per controller | `10` |
| `federation.splitBrain.agentQuorum.agentWeight` | Voting weight per agent | `1` |
| `federation.splitBrain.agentQuorum.minAgentsForQuorum` | Minimum agents required | `1` |

---

## NovaEdge Agent Chart

The standalone agent chart (`charts/novaedge-agent`) deploys agents in remote/edge clusters that connect back to a hub controller.

### Cluster Identification

| Parameter | Description | Default |
|-----------|-------------|---------|
| `cluster.name` | Unique cluster name (required) | `""` |
| `cluster.region` | Geographic region | `""` |
| `cluster.zone` | Availability zone | `""` |
| `cluster.labels` | Additional cluster labels | `{}` |

### Controller Connection

| Parameter | Description | Default |
|-----------|-------------|---------|
| `connection.mode` | `Direct` or `Tunnel` | `Direct` |
| `connection.controllerEndpoint` | Hub controller endpoint | `""` |
| `connection.reconnectInterval` | Reconnect interval | `30s` |
| `connection.timeout` | Connection timeout | `10s` |

### TLS Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `tls.enabled` | Enable mTLS | `true` |
| `tls.caSecretName` | CA certificate secret | `novaedge-ca` |
| `tls.clientCertSecretName` | Client certificate secret | `novaedge-agent-cert` |
| `tls.serverName` | Expected server name | `""` |
| `tls.insecureSkipVerify` | Skip TLS verification | `false` |

### VIP Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `vip.enabled` | Enable VIP management | `true` |
| `vip.mode` | VIP mode: `L2`, `BGP`, `OSPF` | `L2` |
| `vip.interface` | Network interface for L2 | `""` |
| `vip.bgp.asn` | BGP ASN | `0` |
| `vip.bgp.routerID` | BGP router ID | `""` |
| `vip.bgp.peers` | BGP peer list | `[]` |

### Controller Reachability (Agent-Assisted Quorum)

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controllerReachability.enabled` | Enable controller probing | `false` |
| `controllerReachability.probeInterval` | Probe interval | `10s` |
| `controllerReachability.probeTimeout` | Probe timeout | `5s` |
| `controllerReachability.controllerEndpoints` | All controllers to probe | `[]` |

### Failover Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `failover.enabled` | Enable controller failover | `true` |
| `failover.backupControllers` | Backup controller list | `[]` |
| `failover.failoverTimeout` | Time before failover | `30s` |
| `failover.healthCheckInterval` | Primary health check interval | `10s` |
| `failover.persistConfig` | Persist config locally | `true` |
| `failover.persistPath` | Config persistence path | `/var/lib/novaedge/config` |

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

### Federated Multi-Datacenter (3+ Controllers)

```yaml
# DC1 Controller values
federation:
  enabled: true
  federationID: production-cluster
  localMember:
    name: controller-dc1
    region: us-west
    zone: us-west-2a
    endpoint: controller-dc1.novaedge.example.com:9090
  members:
    - name: controller-dc2
      endpoint: controller-dc2.novaedge.example.com:9090
      region: us-east
      tls:
        enabled: true
        caSecretRef:
          name: novaedge-federation-ca
        clientCertSecretRef:
          name: novaedge-federation-client-cert
    - name: controller-dc3
      endpoint: controller-dc3.novaedge.example.com:9090
      region: eu-west
      tls:
        enabled: true
        caSecretRef:
          name: novaedge-federation-ca
        clientCertSecretRef:
          name: novaedge-federation-client-cert
  sync:
    interval: 5s
    compression: true
  splitBrain:
    enabled: true
    quorumMode: Controllers
    quorumRequired: true
    fencingEnabled: true
```

### Agent-Assisted Quorum (2 Datacenters)

```yaml
# DC1 Controller values
federation:
  enabled: true
  federationID: two-dc-cluster
  localMember:
    name: controller-dc1
    region: us-west
    endpoint: controller-dc1.novaedge.example.com:9090
  members:
    - name: controller-dc2
      endpoint: controller-dc2.novaedge.example.com:9090
      region: us-east
      tls:
        enabled: true
        caSecretRef:
          name: novaedge-federation-ca
  splitBrain:
    enabled: true
    quorumMode: AgentAssisted
    quorumRequired: true
    fencingEnabled: true
    agentQuorum:
      controllerWeight: 10
      agentWeight: 1
      minAgentsForQuorum: 1
```

### Remote Agent with Controller Reachability

```yaml
# Remote cluster agent values
cluster:
  name: edge-us-west-1
  region: us-west
  zone: us-west-2a

connection:
  mode: Direct
  controllerEndpoint: controller-dc1.novaedge.example.com:9090

tls:
  enabled: true
  caSecretName: novaedge-ca
  clientCertSecretName: novaedge-agent-cert

# Enable for agent-assisted quorum
controllerReachability:
  enabled: true
  probeInterval: 10s
  probeTimeout: 5s
  controllerEndpoints:
    - name: controller-dc1
      endpoint: controller-dc1.novaedge.example.com:9090
    - name: controller-dc2
      endpoint: controller-dc2.novaedge.example.com:9090

failover:
  enabled: true
  backupControllers:
    - endpoint: controller-dc2.novaedge.example.com:9090
      priority: 100
  persistConfig: true
```
