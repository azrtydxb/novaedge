# novactl - NovaEdge CLI Tool

`novactl` is a command-line tool for managing, debugging, and monitoring NovaEdge resources in a Kubernetes cluster.

## Installation

Build the CLI tool using the Makefile:

```bash
make build-novactl
```

The binary will be created at `bin/novactl`. Optionally, copy it to your PATH:

```bash
sudo cp bin/novactl /usr/local/bin/
```

## Usage

```bash
novactl [command] [flags]
```

### Global Flags

- `--kubeconfig`: Path to kubeconfig file (default: `~/.kube/config`)
- `-n, --namespace`: Kubernetes namespace (default: `default`)

## Commands

### Resource Management

#### Get Resources

List NovaEdge resources:

```bash
# List all gateways
novactl get gateways

# List routes in a specific namespace
novactl get routes -n production

# Get backends with JSON output
novactl get backends -o json

# Get policies with YAML output
novactl get policies -o yaml

# List VIPs
novactl get vips
```

**Supported resource types:**
- `gateways` (alias: `gateway`, `gw`) - ProxyGateway resources
- `routes` (alias: `route`, `rt`) - ProxyRoute resources
- `backends` (alias: `backend`, `be`) - ProxyBackend resources
- `policies` (alias: `policy`, `pol`) - ProxyPolicy resources
- `vips` (alias: `vip`) - ProxyVIP resources

**Output formats:**
- `table` (default) - Human-readable table format
- `json` - JSON output
- `yaml` - YAML output
- `wide` - Extended table format

#### Describe Resources

Show detailed information about a specific resource:

```bash
# Describe a gateway
novactl describe gateway external-gateway

# Describe a route in production namespace
novactl describe route api-route -n production

# Describe a backend
novactl describe backend api-backend
```

#### Delete Resources

Delete a specific resource:

```bash
# Delete a gateway
novactl delete gateway old-gateway

# Delete a route
novactl delete route deprecated-route -n production
```

#### Apply Resources

Create or update resources from YAML files:

```bash
# Apply a single resource
novactl apply -f gateway.yaml

# Apply resources from samples directory
novactl apply -f config/samples/gateway-sample.yaml
```

### Agent Management

#### List Agents

View all NovaEdge agents running on cluster nodes:

```bash
novactl agents list
```

Example output:
```
NODE       STATUS    RESTARTS  AGE
worker-1   Running   0         2d
worker-2   Running   1         2d
worker-3   Running   0         1d
```

#### Describe Agent

Show detailed information about an agent on a specific node:

```bash
novactl agents describe worker-1
```

Example output:
```
Agent on Node: worker-1
Pod Name: novaedge-agent-abc123
Namespace: novaedge-system
Status: Running
Pod IP: 10.244.1.5
Host IP: 192.168.1.10
Start Time: 2025-11-13 10:00:00

Container Status:
  Ready: true
  Restart Count: 0
  Image: novaedge-agent:latest
  State: Running (started 2025-11-13 10:00:05)
```

#### Show Agent Configuration

View the current configuration snapshot for an agent:

```bash
novactl agents config worker-1
```

*Note: This feature requires implementing a gRPC client for the agent's API.*

### Debugging

#### Debug Routes

Show routing information for a specific hostname:

```bash
novactl debug routes api.example.com
```

Example output:
```
Routes matching hostname 'api.example.com':

Name: api-route
Namespace: default
Hostnames: [api.example.com]
Matches:
  - Path: /api (PathPrefix)
  - Method: GET
Backends:
  - api-backend (weight: 100)

---

Name: api-v2-route
Namespace: default
Hostnames: [api.example.com]
Matches:
  - Path: /v2 (PathPrefix)
Backends:
  - api-v2-backend (weight: 80)
  - api-v1-backend (weight: 20)
```

#### Debug Backends

Show detailed backend information including endpoints and health status:

```bash
novactl debug backends api-backend
```

Example output:
```
Backend: api-backend
Namespace: default
Service: api-service:8080

Health Check:
  Path: /health
  Interval: 10s
  Timeout: 5s

Endpoints (3):
ADDRESS         PORT  HEALTHY  LAST CHECK
10.244.1.10     8080  Yes      2025-11-15 14:00:00
10.244.2.11     8080  Yes      2025-11-15 14:00:01
10.244.3.12     8080  No       2025-11-15 13:59:55
```

#### Debug Trace

View distributed trace information for a request:

```bash
novactl debug trace abc123
```

*Note: This feature requires implementing trace query from the OpenTelemetry endpoint.*

### Metrics

#### Agent Metrics

View metrics for a specific agent:

```bash
novactl metrics agent worker-1
```

*Note: This feature requires implementing Prometheus query API client.*

#### Backend Metrics

Show health metrics for all backends:

```bash
novactl metrics backends
```

Example output:
```
Backend Health Metrics:

NAME            TOTAL  HEALTHY  UNHEALTHY  HEALTH %
api-backend     3      3        0          100%
auth-backend    2      1        1          50%
db-backend      4      4        0          100%
```

#### VIP Metrics

Display VIP assignments and status:

```bash
novactl metrics vips
```

Example output:
```
VIP Status:

NAME          IP             MODE  ASSIGNED NODE  STATUS   LAST TRANSITION
external-vip  192.168.1.100  L2    worker-1       Active   2025-11-15 10:00:00
internal-vip  192.168.1.101  BGP   worker-2       Active   2025-11-15 10:00:05
```

### Log Streaming

#### Agent Logs

Stream logs from an agent on a specific node:

```bash
# View agent logs
novactl logs agent worker-1

# Follow agent logs
novactl logs agent worker-1 -f

# Show last 100 lines
novactl logs agent worker-1 --tail 100

# Include timestamps
novactl logs agent worker-1 --timestamps
```

#### Controller Logs

Stream logs from the NovaEdge controller:

```bash
# View controller logs
novactl logs controller

# Follow controller logs
novactl logs controller -f

# Show last 50 lines with timestamps
novactl logs controller --tail 50 --timestamps
```

## Examples

### Complete Workflow Example

```bash
# 1. List all gateways
novactl get gateways

# 2. Describe a specific gateway
novactl describe gateway external-gateway

# 3. Check which routes use a hostname
novactl debug routes api.example.com

# 4. Inspect backend health
novactl debug backends api-backend

# 5. View backend health metrics
novactl metrics backends

# 6. Check agent status
novactl agents list

# 7. View agent logs
novactl logs agent worker-1 -f

# 8. Apply a new route
novactl apply -f new-route.yaml

# 9. Verify the route was created
novactl get routes
```

### Troubleshooting Example

```bash
# 1. Check if routes match the expected hostname
novactl debug routes myapp.example.com

# 2. Verify backend endpoints are healthy
novactl debug backends myapp-backend

# 3. Check agent status on all nodes
novactl agents list

# 4. View agent logs for errors
novactl logs agent worker-1 --tail 100

# 5. Check VIP status
novactl metrics vips

# 6. View backend health metrics
novactl metrics backends
```

## Architecture

The `novactl` CLI is structured as follows:

```
cmd/novactl/
├── main.go                 # Entry point
├── cmd/                    # Command implementations
│   ├── root.go            # Root command and global flags
│   ├── get.go             # Resource listing
│   ├── describe.go        # Resource details
│   ├── delete.go          # Resource deletion
│   ├── apply.go           # Resource creation/update
│   ├── agents.go          # Agent management
│   ├── debug.go           # Debugging commands
│   ├── metrics.go         # Metrics viewing
│   └── logs.go            # Log streaming
└── pkg/                    # Shared packages
    ├── client/            # Kubernetes API clients
    ├── printer/           # Output formatting
    └── util/              # Utility functions
```

## Development

### Building

```bash
make build-novactl
```

### Testing

The CLI can be tested against a running Kubernetes cluster with NovaEdge installed:

```bash
# Set your kubeconfig
export KUBECONFIG=~/.kube/config

# Test commands
./bin/novactl get gateways
./bin/novactl agents list
```

## Future Enhancements

The following features are planned or partially implemented:

1. **Agent Config Inspection**: Implement gRPC client to retrieve agent configuration snapshots
2. **Trace Viewing**: Implement OpenTelemetry trace query from OTLP endpoint
3. **Metrics Integration**: Implement Prometheus query API client for real-time metrics
4. **Resource Validation**: Add client-side validation before applying resources
5. **Batch Operations**: Support for applying multiple resources from directories
6. **Interactive Mode**: Interactive terminal UI for resource management
7. **Export/Import**: Export resources to files and import from backups

## Contributing

When adding new commands:

1. Create a new file in `cmd/` for the command
2. Implement the command using cobra patterns
3. Add the command to `root.go`
4. Update this README with usage examples
5. Test the command against a live cluster

## License

NovaEdge is licensed under the Apache 2.0 License.
