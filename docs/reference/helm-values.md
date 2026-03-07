# Helm Values Reference

Complete reference for all configurable values in the NovaEdge Helm charts.

NovaEdge provides three Helm charts:

1. **novaedge-operator** - Deploys the NovaEdge Operator which manages the lifecycle of NovaEdge components (recommended)
2. **novaedge** - Directly deploys NovaEdge components without the operator
3. **novaedge-agent** - Deploys agents in remote/edge clusters that connect back to a hub controller

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
| `image.repository` | Operator image repository | `ghcr.io/azrtydxb/novaedge/novaedge-operator` |
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
  repository: ghcr.io/azrtydxb/novaedge/novaedge-operator
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
| `controller.image.repository` | Controller image repository | `ghcr.io/azrtydxb/novaedge-controller` |
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
| `agent.image.repository` | Agent image repository | `ghcr.io/azrtydxb/novaedge-agent` |
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

## Dataplane

The Rust dataplane handles all L4/L7 traffic and runs as a sidecar alongside the Go agent.

### Basic Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `dataplane.enabled` | Deploy the Rust dataplane sidecar | `true` |

### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `dataplane.image.repository` | Dataplane image repository | `ghcr.io/azrtydxb/novaedge/novaedge-dataplane` |
| `dataplane.image.tag` | Dataplane image tag | `latest` |
| `dataplane.image.pullPolicy` | Image pull policy | `IfNotPresent` |

### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `dataplane.resources.limits.cpu` | CPU limit | `1` |
| `dataplane.resources.limits.memory` | Memory limit | `512Mi` |
| `dataplane.resources.requests.cpu` | CPU request | `100m` |
| `dataplane.resources.requests.memory` | Memory request | `128Mi` |

### Communication

| Parameter | Description | Default |
|-----------|-------------|---------|
| `dataplane.socketPath` | Unix socket for agent-dataplane communication | `/run/novaedge/dataplane.sock` |
| `dataplane.ebpfPath` | Path to eBPF programs | `/opt/novaedge/novaedge-ebpf` |

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
| `webui.image.repository` | Web UI backend image repository | `ghcr.io/azrtydxb/novaedge-novactl` |
| `webui.image.tag` | Web UI backend image tag | Chart appVersion |
| `webui.image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `webui.frontend.image.repository` | Web UI frontend image repository | `ghcr.io/azrtydxb/novaedge-webui` |
| `webui.frontend.image.tag` | Web UI frontend image tag | Chart appVersion |
| `webui.frontend.image.pullPolicy` | Frontend image pull policy | `IfNotPresent` |

### Frontend Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webui.frontend.resources.limits.cpu` | CPU limit | `100m` |
| `webui.frontend.resources.limits.memory` | Memory limit | `128Mi` |
| `webui.frontend.resources.requests.cpu` | CPU request | `25m` |
| `webui.frontend.resources.requests.memory` | Memory request | `32Mi` |

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
| `webui.service.targetPort` | Target port | `8080` |

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

## cert-manager Integration

Certificate management through cert-manager. Configured under `controller.certificates.certManager.*`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `controller.certificates.certManager.enabled` | bool | `false` | Enable cert-manager integration |
| `controller.certificates.certManager.issuerRef` | string | `""` | ClusterIssuer or Issuer name |
| `controller.certificates.certManager.issuerKind` | string | `ClusterIssuer` | Issuer kind (`ClusterIssuer` or `Issuer`) |

When enabled, ProxyCertificate resources can reference cert-manager issuers.

## Vault Integration

Certificate management through HashiCorp Vault PKI. Configured under `controller.certificates.vault.*`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `controller.certificates.vault.enabled` | bool | `false` | Enable Vault integration |
| `controller.certificates.vault.address` | string | `""` | Vault server address |
| `controller.certificates.vault.authMethod` | string | `kubernetes` | Auth method (`kubernetes`, `approle`, `token`) |
| `controller.certificates.vault.role` | string | `""` | Vault PKI role |
| `controller.certificates.vault.mountPath` | string | `pki` | Vault PKI mount path |
| `controller.certificates.vault.credentialsSecretName` | string | `""` | Secret containing Vault token or credentials |


---

## NovaEdge Agent Chart (Standalone)

The `novaedge-agent` chart is designed for deploying agents in remote/edge clusters that connect to a central hub controller. This enables hub-spoke multi-cluster architectures.

### Cluster Identification

| Parameter | Description | Default |
|-----------|-------------|---------|
| `cluster.name` | Unique name for this remote cluster (required) | `""` |
| `cluster.region` | Geographic region | `""` |
| `cluster.zone` | Availability zone | `""` |
| `cluster.labels` | Additional cluster labels | `{}` |

### Controller Connection

| Parameter | Description | Default |
|-----------|-------------|---------|
| `connection.mode` | Connection mode: `Direct` or `Tunnel` | `Direct` |
| `connection.controllerEndpoint` | Hub controller endpoint (required) | `""` |
| `connection.reconnectInterval` | Reconnect interval when disconnected | `30s` |
| `connection.timeout` | Connection timeout | `10s` |

**Examples:**
- Direct mode: `controller.nova-system.svc.hub-cluster:9090`
- External: `novaedge-controller.example.com:9090`

### mTLS Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `tls.enabled` | Enable mTLS (highly recommended) | `true` |
| `tls.caSecretName` | CA certificate secret for validating controller | `novaedge-ca` |
| `tls.clientCertSecretName` | Client certificate secret for agent auth | `novaedge-agent-cert` |
| `tls.serverName` | Expected server name for TLS verification | `""` |
| `tls.insecureSkipVerify` | Skip TLS verification (NOT for production) | `false` |

### Tunnel Configuration

Used when `connection.mode=Tunnel` for NAT traversal or secure connectivity.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `tunnel.type` | Tunnel type: `WireGuard`, `SSH`, or `WebSocket` | `WireGuard` |
| `tunnel.relayEndpoint` | Relay endpoint for tunnel | `""` |
| `tunnel.wireGuard.privateKeySecretName` | Secret with WireGuard private key | `""` |
| `tunnel.wireGuard.publicKey` | Hub's WireGuard public key | `""` |
| `tunnel.wireGuard.endpoint` | Hub's WireGuard endpoint | `""` |
| `tunnel.wireGuard.allowedIPs` | Allowed IP ranges for tunnel | `[10.0.0.0/8]` |
| `tunnel.wireGuard.persistentKeepalive` | Keepalive interval (seconds) | `25` |

### VIP Management

| Parameter | Description | Default |
|-----------|-------------|---------|
| `vip.l2.enabled` | Enable L2 ARP mode VIP management | `true` |
| `vip.l2.interface` | Network interface for VIP binding | `eth0` |
| `vip.bgp.enabled` | Enable BGP mode VIP announcement | `false` |
| `vip.bgp.asn` | Local BGP AS number | `65000` |
| `vip.bgp.routerID` | BGP router ID (auto-detected if empty) | `""` |
| `vip.bgp.peers` | List of BGP peer configurations | `[]` |
| `vip.ospf.enabled` | Enable OSPF mode VIP announcement | `false` |
| `vip.ospf.routerID` | OSPF router ID | `""` |
| `vip.ospf.areaID` | OSPF area ID | `0.0.0.0` |
| `vip.bfd.enabled` | Enable BFD for failure detection | `false` |
| `vip.bfd.minTxInterval` | BFD transmit interval | `300ms` |
| `vip.bfd.minRxInterval` | BFD receive interval | `300ms` |
| `vip.bfd.multiplier` | Detection multiplier | `3` |

### Server Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `server.tls.enabled` | Enable TLS termination | `true` |
| `server.tls.minVersion` | Minimum TLS version | `TLS1.2` |
| `server.mtls.enabled` | Enable mTLS for client authentication | `false` |
| `server.mtls.mode` | mTLS mode: `require` or `optional` | `require` |
| `server.ocsp.enabled` | Enable OCSP stapling | `false` |
| `server.proxyProtocol.enabled` | Accept PROXY protocol headers | `false` |

### HTTP/3 QUIC Support

| Parameter | Description | Default |
|-----------|-------------|---------|
| `http3.enabled` | Enable HTTP/3 QUIC support | `false` |
| `http3.port` | HTTP/3 UDP port | `443` |
| `http3.maxStreamBufferSize` | Max stream buffer size | `1048576` (1MB) |
| `http3.maxIdleTimeout` | Max idle timeout | `30s` |

### Load Balancing

| Parameter | Description | Default |
|-----------|-------------|---------|
| `loadBalancing.defaultAlgorithm` | Default algorithm | `RoundRobin` |
| `loadBalancing.ewma.decayFactor` | EWMA decay factor | `0.9` |
| `loadBalancing.sticky.enabled` | Enable sticky sessions | `false` |
| `loadBalancing.sticky.cookieName` | Sticky session cookie name | `novaedge-session` |
| `loadBalancing.locality.enabled` | Enable locality-aware routing | `false` |
| `loadBalancing.panic.threshold` | Panic mode threshold | `0.5` (50%) |

**Available algorithms:** `RoundRobin`, `LeastConn`, `P2C`, `EWMA`, `RingHash`, `Maglev`, `Sticky`, `LocalityAware`, `PriorityFailover`, `PanicMode`, `SlowStart`, `ResourceAdaptive`

### Connection Pool

| Parameter | Description | Default |
|-----------|-------------|---------|
| `connectionPool.maxConnections` | Max connections per backend | `1024` |
| `connectionPool.maxIdleConnections` | Max idle connections | `100` |
| `connectionPool.idleTimeout` | Idle connection timeout | `90s` |
| `connectionPool.circuitBreaker.enabled` | Enable circuit breaker | `true` |
| `connectionPool.circuitBreaker.threshold` | Failure threshold | `5` |
| `connectionPool.circuitBreaker.timeout` | Breaker timeout | `30s` |

### Health Checking

| Parameter | Description | Default |
|-----------|-------------|---------|
| `healthChecking.interval` | Health check interval | `10s` |
| `healthChecking.timeout` | Health check timeout | `5s` |
| `healthChecking.unhealthyThreshold` | Unhealthy threshold | `3` |
| `healthChecking.healthyThreshold` | Healthy threshold | `2` |
| `healthChecking.outlierDetection.enabled` | Enable outlier detection | `true` |
| `healthChecking.outlierDetection.consecutiveErrors` | Consecutive errors for ejection | `5` |
| `healthChecking.outlierDetection.interval` | Detection interval | `10s` |

### eBPF/XDP Acceleration

eBPF/XDP features are enabled by default and auto-detected at runtime. If the kernel does not support a feature, the agent transparently falls back to the legacy path.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ebpf.bpffsMount` | Mount `/sys/fs/bpf` for BPF map pinning | `true` |
| `ebpf.xdpInterface` | NIC for XDP/AF_XDP attachment (enables L4 LB and zero-copy acceleration when set) | `""` |
| `ebpf.forceLegacyLb` | Force legacy userspace L4 proxy instead of XDP/AF_XDP | `false` |
| `ebpf.forceLegacyMesh` | Force legacy nftables/iptables mesh interception instead of eBPF `SK_LOOKUP` | `false` |

Example:

```yaml
ebpf:
  bpffsMount: true
  xdpInterface: eth0    # Set to your NIC to enable XDP/AF_XDP
  forceLegacyLb: false
  forceLegacyMesh: false
```

The agent container requires `CAP_BPF`, `CAP_NET_ADMIN`, and `CAP_SYS_ADMIN` capabilities for eBPF program loading. These are included in the default security context.

### L4 Proxy (TCP/UDP)

| Parameter | Description | Default |
|-----------|-------------|---------|
| `l4Proxy.enabled` | Enable L4 TCP/UDP proxying | `true` |
| `l4Proxy.tcp.enabled` | Enable TCP proxying | `true` |
| `l4Proxy.udp.enabled` | Enable UDP proxying | `true` |
| `l4Proxy.tlsPassthrough.enabled` | Enable TLS passthrough (SNI routing) | `false` |

### Policy Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `policy.rateLimit.enabled` | Enable rate limiting | `true` |
| `policy.rateLimit.distributedMode` | Distributed rate limiting via Redis | `false` |
| `policy.rateLimit.redisEndpoint` | Redis endpoint for distributed limiting | `""` |
| `policy.auth.enabled` | Enable authentication | `false` |
| `policy.jwt.enabled` | Enable JWT validation | `false` |
| `policy.cors.enabled` | Enable CORS handling | `false` |
| `policy.waf.enabled` | Enable Web Application Firewall | `false` |
| `policy.waf.ruleSet` | WAF rule set: `OWASP-CRS` | `OWASP-CRS` |
| `policy.ipFilter.enabled` | Enable IP filtering | `false` |
| `policy.mtls.enabled` | Enable mTLS enforcement | `false` |

### Middleware

| Parameter | Description | Default |
|-----------|-------------|---------|
| `middleware.caching.enabled` | Enable HTTP response caching | `false` |
| `middleware.caching.maxSize` | Max cache size | `100MB` |
| `middleware.compression.enabled` | Enable compression (gzip/brotli) | `true` |
| `middleware.compression.level` | Compression level (1-9) | `6` |
| `middleware.retry.enabled` | Enable request retry | `true` |
| `middleware.retry.maxAttempts` | Max retry attempts | `3` |
| `middleware.hedging.enabled` | Enable request hedging | `false` |
| `middleware.hedging.delay` | Hedging delay | `100ms` |
| `middleware.mirroring.enabled` | Enable traffic mirroring | `false` |

### WebSocket & SSE

| Parameter | Description | Default |
|-----------|-------------|---------|
| `websocket.enabled` | Enable WebSocket support | `true` |
| `websocket.maxFrameSize` | Max WebSocket frame size | `1048576` (1MB) |
| `sse.enabled` | Enable Server-Sent Events | `true` |
| `sse.maxMessageSize` | Max SSE message size | `65536` (64KB) |

### gRPC Support

| Parameter | Description | Default |
|-----------|-------------|---------|
| `grpc.enabled` | Enable gRPC proxying | `true` |
| `grpc.maxMessageSize` | Max gRPC message size | `4194304` (4MB) |
| `grpc.transcoding.enabled` | Enable gRPC-JSON transcoding | `false` |

### Service Mesh

| Parameter | Description | Default |
|-----------|-------------|---------|
| `mesh.enabled` | Enable service mesh (transparent mTLS) | `false` |
| `mesh.mode` | Mesh mode: `tproxy` or `iptables` | `tproxy` |
| `mesh.spiffe.enabled` | Enable SPIFFE workload identity | `true` |
| `mesh.authz.enabled` | Enable authorization policies | `false` |

### Control Plane VIP

| Parameter | Description | Default |
|-----------|-------------|---------|
| `controlPlaneVIP.enabled` | Enable dedicated VIP for controller HA | `false` |
| `controlPlaneVIP.address` | VIP address for controller | `""` |
| `controlPlaneVIP.healthCheck.enabled` | Enable controller health checking | `true` |
| `controlPlaneVIP.healthCheck.endpoint` | Controller health endpoint | `/livez` |

### WASM Plugin Support

| Parameter | Description | Default |
|-----------|-------------|---------|
| `wasm.enabled` | Enable WASM plugin system | `false` |
| `wasm.pluginDir` | Directory for WASM plugins | `/var/lib/novaedge/wasm` |
| `wasm.maxMemoryPages` | Max WASM memory pages | `512` |

### Example: Remote Agent Configuration

```yaml
cluster:
  name: edge-cluster-west
  region: us-west-2
  zone: us-west-2a

connection:
  mode: Direct
  controllerEndpoint: novaedge-controller.hub.example.com:9090

tls:
  enabled: true
  caSecretName: novaedge-ca
  clientCertSecretName: edge-west-agent-cert

vip:
  l2:
    enabled: true
    interface: eth0
  bgp:
    enabled: true
    asn: 65001
    peers:
      - address: 192.168.1.1
        asn: 65000
        password: bgp-secret

loadBalancing:
  defaultAlgorithm: EWMA
  ewma:
    decayFactor: 0.9
  locality:
    enabled: true

policy:
  rateLimit:
    enabled: true
    distributedMode: true
    redisEndpoint: redis.edge-cluster:6379
  waf:
    enabled: true
    ruleSet: OWASP-CRS

middleware:
  caching:
    enabled: true
    maxSize: 500MB
  compression:
    enabled: true
    level: 6
```

