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
- `gateways` or `gateway` or `gw`
- `routes` or `route` or `rt`
- `backends` or `backend` or `be`
- `vips` or `vip`
- `policies` or `policy` or `pol`

**Examples:**

```bash
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
novactl get routes
novactl get backends
novactl get vips
novactl get policies
```

**Table Output Format:**

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

Resources:
  ProxyGateways:  5
  ProxyRoutes:    12
  ProxyBackends:  8
  ProxyVIPs:      2
  ProxyPolicies:  6

Health:
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
novactl get gateways,routes,backends,vips,policies
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

- [Deployment Guide](deployment-guide.md)
- [Gateway API Documentation](gateway-api.md)
- [NovaEdge Architecture](../NovaEdge_FullSpec.md)
- [kubectl Reference](https://kubernetes.io/docs/reference/kubectl/)
