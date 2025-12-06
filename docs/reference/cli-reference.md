# novactl CLI Reference

`novactl` is the command-line interface for managing NovaEdge resources. It provides a kubectl-style interface for interacting with ProxyGateways, ProxyRoutes, ProxyBackends, ProxyVIPs, and ProxyPolicies.

## Installation

```bash
# Build from source
cd novaedge
make build-novactl

# The binary will be in the project root
./novactl version

# Optional: Install to system path
sudo cp novactl /usr/local/bin/
novactl version
```

## Global Flags

```
--kubeconfig string   Path to kubeconfig file (default: $KUBECONFIG or ~/.kube/config)
--context string      Kubernetes context to use
--namespace string    Kubernetes namespace (default: "default")
-o, --output string   Output format: table, json, yaml (default: "table")
-h, --help           Help for any command
```

## Commands

### novactl version

Display version information for novactl.

```bash
novactl version
```

Output:
```
novactl version: v1.0.0
Kubernetes version: v1.29.0
```

### novactl get

List resources of a specific type.

**Syntax:**
```bash
novactl get <resource-type> [name] [flags]
```

**Resource Types:**
- `clusters` or `cluster` or `cl` (NovaEdgeCluster)
- `gateways` or `gateway` or `gw`
- `routes` or `route` or `rt`
- `backends` or `backend` or `be`
- `vips` or `vip`
- `policies` or `policy` or `pol`

**Examples:**

```bash
# List all NovaEdge clusters
novactl get clusters
novactl get clusters -A

# List all gateways in current namespace
novactl get gateways

# List all gateways in all namespaces
novactl get gateways --all-namespaces
novactl get gateways -A

# List gateways in specific namespace
novactl get gateways -n production

# Get specific gateway
novactl get gateway main-gateway

# Output as JSON
novactl get gateways -o json

# Output as YAML
novactl get gateways -o yaml

# List all resource types
novactl get clusters
novactl get routes
novactl get backends
novactl get vips
novactl get policies
```

**Table Output Format:**

NovaEdgeClusters:
```
NAMESPACE        NAME       VERSION   PHASE     CONTROLLER   AGENTS   AGE
novaedge-system  novaedge   v0.1.0    Running   1/1          3/3      5d
```

Gateways:
```
NAMESPACE   NAME            VIP          LISTENERS   READY   AGE
default     main-gateway    default-vip  2           True    5d
```

Routes:
```
NAMESPACE   NAME         HOSTNAMES           RULES   READY   AGE
default     echo-route   echo.example.com    1       True    5d
```

Backends:
```
NAMESPACE   NAME           SERVICE   LB POLICY    ENDPOINTS   READY   AGE
default     echo-backend   echo:80   RoundRobin   3           True    5d
```

VIPs:
```
NAMESPACE   NAME         VIP             MODE   READY   AGE
default     default-vip  192.168.1.100   L2     True    5d
```

Policies:
```
NAMESPACE   NAME               TYPE        TARGET                  AGE
default     rate-limit-policy  RateLimit   ProxyRoute/echo-route   5d
```

### novactl describe

Show detailed information about a specific resource.

**Syntax:**
```bash
novactl describe <resource-type> <name> [flags]
```

**Examples:**

```bash
# Describe a NovaEdge cluster
novactl describe cluster novaedge -n novaedge-system

# Describe a gateway
novactl describe gateway main-gateway

# Describe in specific namespace
novactl describe gateway main-gateway -n production

# Describe route
novactl describe route echo-route

# Describe backend
novactl describe backend echo-backend

# Describe VIP
novactl describe vip default-vip

# Describe policy
novactl describe policy rate-limit-policy
```

**Output Example:**

```
Name:         main-gateway
Namespace:    default
Labels:       app=web
Annotations:  <none>
API Version:  novaedge.io/v1alpha1
Kind:         ProxyGateway

Spec:
  VIP Ref:  default-vip
  Listeners:
    Name:      http
    Port:      80
    Protocol:  HTTP
    Hostnames:
      *.example.com
    Name:      https
    Port:      443
    Protocol:  HTTPS
    Hostnames:
      *.example.com
    TLS:
      Secret Ref:
        Name:       example-tls
        Namespace:  default
      Min Version:  TLS1.2

Status:
  Conditions:
    Type:                  Ready
    Status:                True
    Last Transition Time:  2024-11-15T10:30:00Z
    Reason:                Valid
    Message:               Gateway configuration is valid
  Observed Generation:     5

Events:  <none>
```

### novactl create

Create resources from file or stdin.

**Syntax:**
```bash
novactl create -f <file> [flags]
```

**Examples:**

```bash
# Create from file
novactl create -f gateway.yaml

# Create from multiple files
novactl create -f gateway.yaml -f route.yaml

# Create from directory
novactl create -f ./manifests/

# Create from stdin
cat gateway.yaml | novactl create -f -

# Create in specific namespace
novactl create -f gateway.yaml -n production
```

### novactl apply

Apply configuration from file (create or update).

**Syntax:**
```bash
novactl apply -f <file> [flags]
```

**Examples:**

```bash
# Apply configuration
novactl apply -f gateway.yaml

# Apply multiple files
novactl apply -f gateway.yaml -f route.yaml

# Apply from directory
novactl apply -f ./manifests/

# Apply with server-side apply
novactl apply -f gateway.yaml --server-side
```

### novactl delete

Delete resources.

**Syntax:**
```bash
novactl delete <resource-type> <name> [flags]
novactl delete -f <file> [flags]
```

**Examples:**

```bash
# Delete by name
novactl delete gateway main-gateway

# Delete from file
novactl delete -f gateway.yaml

# Delete all gateways in namespace
novactl delete gateways --all

# Delete in specific namespace
novactl delete gateway main-gateway -n production

# Force delete (skip finalizers)
novactl delete gateway main-gateway --force --grace-period=0
```

### novactl edit

Edit a resource using default editor.

**Syntax:**
```bash
novactl edit <resource-type> <name> [flags]
```

**Examples:**

```bash
# Edit gateway
novactl edit gateway main-gateway

# Edit in specific namespace
novactl edit gateway main-gateway -n production

# Use specific editor
EDITOR=vim novactl edit gateway main-gateway
```

### novactl patch

Update fields of a resource.

**Syntax:**
```bash
novactl patch <resource-type> <name> -p <patch> [flags]
```

**Examples:**

```bash
# Patch with JSON
novactl patch gateway main-gateway -p '{"spec":{"vipRef":"new-vip"}}'

# Patch with YAML
novactl patch gateway main-gateway --type=merge -p '
spec:
  vipRef: new-vip
'

# Strategic merge patch (default)
novactl patch gateway main-gateway --type=strategic -p '{"spec":{"listeners":[{"name":"http","port":8080}]}}'

# JSON patch
novactl patch gateway main-gateway --type=json -p '[{"op":"replace","path":"/spec/vipRef","value":"new-vip"}]'
```

### novactl logs

View logs from NovaEdge components.

**Syntax:**
```bash
novactl logs <component> [flags]
```

**Components:**
- `controller`
- `agent`

**Examples:**

```bash
# View controller logs
novactl logs controller

# View agent logs (all agents)
novactl logs agent

# View agent logs from specific node
novactl logs agent --node=worker-1

# Follow logs
novactl logs controller -f

# Show last 100 lines
novactl logs controller --tail=100

# Show logs since timestamp
novactl logs controller --since=1h

# Show timestamps
novactl logs controller --timestamps
```

### novactl status

Show overall status of NovaEdge deployment.

**Syntax:**
```bash
novactl status [flags]
```

**Example:**

```bash
novactl status
```

**Output:**

```
NovaEdge Status Report

Operator:
  Installed:   Yes
  Version:     v0.1.0

Cluster:
  Name:        novaedge
  Namespace:   novaedge-system
  Phase:       Running
  Version:     v0.1.0

Controller:
  Replicas:    1/1 Ready
  Version:     v1.0.0
  Status:      Running
  Last Sync:   2024-11-15T10:30:00Z

Agents:
  Total Nodes: 3
  Ready:       3
  Version:     v1.0.0

  Node            Status    VIPs    Active Connections
  ----            ------    ----    ------------------
  control-plane   Ready     1       145
  worker-1        Ready     0       203
  worker-2        Ready     0       198

Web UI:
  Enabled:     Yes
  Replicas:    1/1 Ready
  URL:         http://novaedge-webui.novaedge-system:9080

Resources:
  NovaEdgeClusters:  1
  ProxyGateways:     5
  ProxyRoutes:       12
  ProxyBackends:     8
  ProxyVIPs:         2
  ProxyPolicies:     6

Health:
  Operator:    ✓ Healthy
  Controller:  ✓ Healthy
  Agents:      ✓ All Ready
  VIPs:        ✓ All Active
  Backends:    ⚠ 1 Degraded
```

### novactl validate

Validate resource definitions without applying them.

**Syntax:**
```bash
novactl validate -f <file> [flags]
```

**Examples:**

```bash
# Validate single file
novactl validate -f gateway.yaml

# Validate multiple files
novactl validate -f gateway.yaml -f route.yaml

# Validate directory
novactl validate -f ./manifests/

# Validate from stdin
cat gateway.yaml | novactl validate -f -
```

**Output:**

```
✓ gateway.yaml: Valid ProxyGateway (main-gateway)
✗ route.yaml: Invalid ProxyRoute (test-route)
  - spec.rules[0].backendRef.name: Required field missing
  - spec.hostnames: At least one hostname required

Validation Summary:
  Total: 2
  Valid: 1
  Invalid: 1
```

### novactl trace

Query distributed traces from the OpenTelemetry backend (Jaeger, Tempo, etc.).

**Syntax:**
```bash
novactl trace <subcommand> [flags]
```

**Global Trace Flags:**
```
--trace-endpoint string   OpenTelemetry trace backend endpoint (default: http://localhost:16686)
```

**Subcommands:**

#### novactl trace list

List recent traces from the tracing backend.

```bash
# List last 20 traces from the past hour
novactl trace list

# List last 50 traces from the past 24 hours
novactl trace list --limit 50 --lookback 24h

# List traces from custom endpoint
novactl trace list --trace-endpoint http://jaeger:16686
```

**Flags:**
```
--limit int        Maximum number of traces to return (default: 20)
--lookback string  How far back to search (e.g., 1h, 24h, 7d) (default: 1h)
```

**Output:**
```
Recent Traces (last 1h):

TRACE ID          OPERATION                  SERVICE         DURATION   SPANS   START TIME
abc123def456      HTTP GET /api/users        novaedge-agent  45.32ms    3       14:30:05
def456ghi789      HTTP POST /api/orders      novaedge-agent  123.45ms   5       14:29:58
...

Use 'novactl trace get <trace-id>' to view details
```

#### novactl trace get

Get details of a specific trace by ID.

```bash
# Get trace details
novactl trace get abc123def456

# Get trace from custom endpoint
novactl trace get abc123def456 --trace-endpoint http://jaeger:16686
```

**Output:**
```
Trace: abc123def456789
Operation: HTTP GET /api/users
Service: novaedge-agent
Duration: 45.32ms
Start Time: 2024-11-15 14:30:05.123
Spans: 3

Span Tree:

├─ HTTP GET [novaedge-agent] 45.32ms
│  http.method: GET
│  http.status_code: 200
│  ├─ backend_forward [novaedge-agent] 42.15ms
│  │  novaedge.backend.cluster: default/api-backend
│  │  novaedge.endpoint: 10.0.1.5:8080
│  │  ├─ upstream_request [api-service] 40.02ms
```

#### novactl trace search

Search for traces matching specific criteria.

```bash
# Search for traces from novaedge-agent service
novactl trace search --service novaedge-agent

# Search for specific operation
novactl trace search --service novaedge-agent --operation "HTTP GET /api/users"

# Search with time range
novactl trace search --service novaedge-agent --start "2024-11-15T10:00:00Z" --end "2024-11-15T12:00:00Z"

# Search for slow traces (duration > 1s)
novactl trace search --min-duration 1s

# Search with tags
novactl trace search --tag http.method=GET --tag http.status_code=500
```

**Flags:**
```
--service string       Service name to filter by
--operation string     Operation name to filter by
--start string         Start time (RFC3339 format)
--end string           End time (RFC3339 format)
--min-duration string  Minimum duration (e.g., 100ms, 1s)
--max-duration string  Maximum duration (e.g., 5s)
--tag stringArray      Tags to filter by (format: key=value)
--limit int            Maximum traces to return (default: 20)
```

#### novactl trace services

List all services that have sent traces.

```bash
novactl trace services
```

**Output:**
```
Services (3):

  novaedge-agent
  api-service
  database-service
```

### novactl config

View and modify novactl configuration.

**Syntax:**
```bash
novactl config <subcommand> [flags]
```

**Subcommands:**
- `view` - Display current configuration
- `set-context` - Set current context
- `use-context` - Switch context

**Examples:**

```bash
# View current configuration
novactl config view

# Set context
novactl config use-context production

# View contexts
novactl config get-contexts
```

## Configuration File

novactl uses `~/.novactl/config` for configuration:

```yaml
currentContext: default
contexts:
- name: default
  cluster: default
  namespace: default
- name: production
  cluster: production-cluster
  namespace: prod
clusters:
- name: default
  kubeconfig: ~/.kube/config
- name: production-cluster
  kubeconfig: ~/.kube/prod-config
```

## Environment Variables

- `KUBECONFIG` - Path to kubeconfig file
- `NOVACTL_NAMESPACE` - Default namespace
- `NOVACTL_OUTPUT` - Default output format (table, json, yaml)
- `NOVACTL_CONTEXT` - Default Kubernetes context

## Examples Workflows

### Deploying a New Application

```bash
# 1. Create backend
cat <<EOF | novactl apply -f -
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: myapp-backend
spec:
  serviceRef:
    name: myapp
    port: 8080
  lbPolicy: RoundRobin
  healthCheck:
    interval: 10s
    httpHealthCheck:
      path: /health
EOF

# 2. Create route
cat <<EOF | novactl apply -f -
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: myapp-route
spec:
  hostnames:
  - myapp.example.com
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: "/"
    backendRef:
      name: myapp-backend
EOF

# 3. Verify
novactl get routes
novactl describe route myapp-route

# 4. Test
curl -H "Host: myapp.example.com" http://<vip-address>/
```

### Updating Configuration

```bash
# Edit route
novactl edit route myapp-route

# Or patch specific field
novactl patch route myapp-route -p '{"spec":{"hostnames":["new.example.com"]}}'

# Verify changes
novactl get route myapp-route -o yaml
```

### Troubleshooting

```bash
# Check overall status
novactl status

# View controller logs
novactl logs controller --tail=100

# View agent logs
novactl logs agent --node=worker-1

# Describe problematic resource
novactl describe backend myapp-backend

# Check all resources
novactl get clusters,gateways,routes,backends,vips,policies
```

### Cleaning Up

```bash
# Delete route
novactl delete route myapp-route

# Delete backend
novactl delete backend myapp-backend

# Delete from file
novactl delete -f myapp-manifests.yaml

# Delete all resources in namespace
novactl delete gateways,routes,backends,policies --all
```

## Comparison with kubectl

novactl is designed to work alongside kubectl:

| Task | kubectl | novactl |
|------|---------|---------|
| List gateways | `kubectl get proxygateways` | `novactl get gateways` |
| Describe gateway | `kubectl describe proxygateway main-gateway` | `novactl describe gateway main-gateway` |
| View logs | `kubectl logs -n novaedge-system -l app=controller` | `novactl logs controller` |
| Overall status | Manual inspection | `novactl status` |
| Validate | `kubectl apply --dry-run=client` | `novactl validate` |

You can use both tools interchangeably. `novactl` provides convenience shortcuts and NovaEdge-specific features, while `kubectl` offers full Kubernetes API access.

## Tips and Best Practices

1. **Use aliases for common commands:**
   ```bash
   alias nvg='novactl get'
   alias nvd='novactl describe'
   alias nvl='novactl logs'
   ```

2. **Set default namespace:**
   ```bash
   export NOVACTL_NAMESPACE=production
   ```

3. **Use output formats for scripting:**
   ```bash
   # Get gateway VIP in JSON
   novactl get gateway main-gateway -o json | jq '.spec.vipRef'

   # List all route hostnames
   novactl get routes -o yaml | grep -A1 hostnames
   ```

4. **Validate before applying:**
   ```bash
   novactl validate -f config.yaml && novactl apply -f config.yaml
   ```

5. **Watch logs during deployment:**
   ```bash
   novactl logs controller -f &
   novactl apply -f new-gateway.yaml
   ```

## See Also

- [Operator Guide](../user-guide/operator.md)
- [Deployment Guide](../user-guide/deployment-guide.md)
- [Gateway API Documentation](../user-guide/gateway-api.md)
- [CRD Reference](crd-reference.md)
- [Helm Values Reference](helm-values.md)
- [kubectl Reference](https://kubernetes.io/docs/reference/kubectl/)
