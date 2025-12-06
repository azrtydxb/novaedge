# Quick Start

Get NovaEdge up and running in your Kubernetes cluster in minutes.

## Prerequisites

- Kubernetes cluster (1.29+)
- kubectl configured
- Go 1.25+ (for building from source)

## Installation

### Step 1: Install CRDs

```bash
# Clone the repository
git clone https://github.com/piwi3910/novaedge.git
cd novaedge

# Install CRDs
make install-crds

# Verify CRDs are installed
kubectl get crds | grep novaedge.io
```

### Step 2: Deploy Controller

```bash
# Create namespace
kubectl apply -f config/controller/namespace.yaml

# Deploy RBAC and controller
kubectl apply -f config/rbac/
kubectl apply -f config/controller/deployment.yaml

# Verify controller is running
kubectl get pods -n novaedge-system -l app.kubernetes.io/name=novaedge-controller
```

### Step 3: Deploy Agents

```bash
# Deploy agent DaemonSet
kubectl apply -f config/agent/

# Verify agents are running on all nodes
kubectl get pods -n novaedge-system -l app.kubernetes.io/name=novaedge-agent
```

## Your First Gateway

### Step 1: Create a VIP

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyVIP
metadata:
  name: my-vip
spec:
  address: 192.168.1.100/32
  mode: L2
  interface: eth0
```

### Step 2: Create a Gateway

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: my-gateway
spec:
  vipRef: my-vip
  listeners:
  - name: http
    port: 80
    protocol: HTTP
    hostnames:
    - "*.example.com"
```

### Step 3: Create a Backend

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: my-backend
spec:
  serviceRef:
    name: my-service
    port: 8080
  lbPolicy: RoundRobin
  healthCheck:
    interval: 10s
    httpHealthCheck:
      path: /health
```

### Step 4: Create a Route

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: my-route
spec:
  parentRefs:
  - name: my-gateway
  hostnames:
  - app.example.com
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /
    backendRef:
      name: my-backend
```

### Step 5: Test

```bash
# Get the VIP address
kubectl get proxyvip my-vip -o jsonpath='{.spec.address}'

# Test the route
curl -H "Host: app.example.com" http://192.168.1.100/
```

## Using the CLI

NovaEdge includes `novactl`, a kubectl-style CLI:

```bash
# Build the CLI
make build-novactl

# List resources
./novactl get gateways
./novactl get routes
./novactl get backends

# Describe a resource
./novactl describe gateway my-gateway

# Check overall status
./novactl status
```

## Next Steps

- [Deployment Guide](../user-guide/deployment-guide.md) - Production deployment
- [Gateway API](../user-guide/gateway-api.md) - Use standard Gateway API resources
- [novactl Reference](../reference/novactl-reference.md) - Full CLI documentation
