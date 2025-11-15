# NovaEdge Deployment Guide

This guide walks through deploying NovaEdge to a Kubernetes cluster, from initial setup to serving traffic.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Quick Start](#quick-start)
3. [Step-by-Step Deployment](#step-by-step-deployment)
4. [Configuration](#configuration)
5. [Verification](#verification)
6. [Troubleshooting](#troubleshooting)

## Prerequisites

### Required

- **Kubernetes cluster**: Version 1.29 or higher
  - For testing: kind, k3s, or minikube
  - For production: EKS, GKE, AKS, or on-premises
- **kubectl**: Configured to access your cluster
- **Container runtime**: Docker or containerd

### Optional

- **Helm**: For simplified deployment (coming soon)
- **OpenTelemetry Collector**: For distributed tracing
- **Prometheus**: For metrics collection
- **Grafana**: For metrics visualization

### Cluster Requirements

NovaEdge agents run with `hostNetwork: true` and require the following:

- **Node privileges**: Agents need `NET_ADMIN`, `NET_RAW`, and `NET_BIND_SERVICE` capabilities
- **Port availability**: Ports 80, 443 must be available on nodes (not bound by other services)
- **BGP/OSPF**: If using BGP or OSPF VIP modes, ensure network allows the required protocols

## Quick Start

For testing in a local cluster (kind/k3s/minikube):

```bash
# 1. Clone the repository
git clone https://github.com/piwi3910/novaedge.git
cd novaedge

# 2. Build Docker images
make docker-build

# 3. Load images into cluster (kind example)
kind load docker-image novaedge-controller:latest
kind load docker-image novaedge-agent:latest

# 4. Install CRDs
make install-crds

# 5. Deploy everything
kubectl apply -f config/controller/namespace.yaml
kubectl apply -f config/rbac/
kubectl apply -f config/controller/deployment.yaml
kubectl apply -f config/agent/

# 6. Verify
kubectl get pods -n novaedge-system
```

## Step-by-Step Deployment

### Step 1: Build Container Images

```bash
# Build all binaries
make build-all

# Build Docker images
make docker-build

# Or build individually
make docker-build-controller
make docker-build-agent

# Tag for your registry (if pushing to remote)
docker tag novaedge-controller:latest your-registry/novaedge-controller:v1.0.0
docker tag novaedge-agent:latest your-registry/novaedge-agent:v1.0.0
docker push your-registry/novaedge-controller:v1.0.0
docker push your-registry/novaedge-agent:v1.0.0
```

### Step 2: Install Custom Resource Definitions (CRDs)

```bash
# Install all CRDs
make install-crds

# Or install manually
kubectl apply -f config/crd/

# Verify CRDs are installed
kubectl get crds | grep novaedge.io
```

Expected output:
```
proxybackends.novaedge.io
proxygateways.novaedge.io
proxypolicies.novaedge.io
proxyroutes.novaedge.io
proxyvips.novaedge.io
```

### Step 3: Create Namespace

```bash
kubectl apply -f config/controller/namespace.yaml

# Or create manually
kubectl create namespace novaedge-system
```

### Step 4: Deploy Controller

```bash
# Create RBAC resources
kubectl apply -f config/rbac/service_account.yaml
kubectl apply -f config/rbac/role.yaml
kubectl apply -f config/rbac/role_binding.yaml
kubectl apply -f config/rbac/leader_election_role.yaml
kubectl apply -f config/rbac/leader_election_role_binding.yaml

# Deploy controller
kubectl apply -f config/controller/deployment.yaml

# Verify controller is running
kubectl get pods -n novaedge-system -l app.kubernetes.io/name=novaedge-controller
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-controller --tail=50
```

### Step 5: Deploy Agents (DaemonSet)

```bash
# Create agent RBAC
kubectl apply -f config/agent/serviceaccount.yaml
kubectl apply -f config/agent/clusterrole.yaml
kubectl apply -f config/agent/clusterrolebinding.yaml

# Deploy agent DaemonSet
kubectl apply -f config/agent/daemonset.yaml

# Verify agents are running on all nodes
kubectl get pods -n novaedge-system -l app.kubernetes.io/name=novaedge-agent -o wide
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-agent --tail=50
```

You should see one agent pod per node in your cluster.

### Step 6: Configure VIP

Create a ProxyVIP resource to define how traffic reaches your cluster:

```yaml
# vip.yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyVIP
metadata:
  name: default-vip
  namespace: default
spec:
  vip: "192.168.1.100"
  mode: L2
  interface: eth0
```

```bash
kubectl apply -f vip.yaml
kubectl get proxyvip default-vip -o yaml
```

**VIP Modes:**

- **L2 (ARP)**: Single active node owns VIP, automatic failover
  - Use when: Direct L2 connectivity, simple setup
  - Requires: `interface` field

- **BGP**: All nodes announce VIP via BGP, ECMP load distribution
  - Use when: L3 routed network, active-active needed
  - Requires: `bgpConfig` with AS number and router IP

- **OSPF**: All nodes announce via OSPF, L3 routing
  - Use when: OSPF-based infrastructure
  - Requires: `ospfConfig` with area ID and router ID

### Step 7: Deploy Sample Application

```bash
# Create a sample backend service
kubectl create deployment echo --image=ealen/echo-server:latest
kubectl expose deployment echo --port=80 --target-port=80

# Create ProxyBackend
kubectl apply -f - <<EOF
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: echo-backend
  namespace: default
spec:
  serviceRef:
    name: echo
    namespace: default
    port: 80
  lbPolicy: RoundRobin
  healthCheck:
    interval: 10s
    timeout: 5s
    healthyThreshold: 2
    unhealthyThreshold: 3
    httpHealthCheck:
      path: /health
      expectedStatuses: [200]
EOF
```

### Step 8: Create Gateway and Route

```bash
# Create ProxyGateway
kubectl apply -f - <<EOF
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: main-gateway
  namespace: default
spec:
  vipRef: default-vip
  listeners:
  - name: http
    port: 80
    protocol: HTTP
    hostnames:
    - "*.example.com"
EOF

# Create ProxyRoute
kubectl apply -f - <<EOF
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: echo-route
  namespace: default
spec:
  hostnames:
  - "echo.example.com"
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: "/"
    backendRef:
      name: echo-backend
EOF
```

### Step 9: Test Traffic

```bash
# Test from within cluster
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -H "Host: echo.example.com" http://192.168.1.100/

# Or from outside cluster (if VIP is accessible)
curl -H "Host: echo.example.com" http://192.168.1.100/
```

## Configuration

### Using Kubernetes Ingress

NovaEdge supports standard Kubernetes Ingress resources:

```yaml
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  name: novaedge
spec:
  controller: novaedge.io/ingress-controller
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: echo-ingress
  annotations:
    novaedge.io/vip-ref: default-vip
    novaedge.io/load-balancing: RoundRobin
spec:
  ingressClassName: novaedge
  rules:
  - host: echo.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: echo
            port:
              number: 80
```

### Using Gateway API

NovaEdge implements the Kubernetes Gateway API:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: novaedge
spec:
  controllerName: novaedge.io/gateway-controller
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: example-gateway
  namespace: default
spec:
  gatewayClassName: novaedge
  listeners:
  - name: http
    protocol: HTTP
    port: 80
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: echo-route
spec:
  parentRefs:
  - name: example-gateway
  hostnames:
  - "echo.example.com"
  rules:
  - backendRefs:
    - name: echo
      port: 80
```

### Policy Configuration

Apply policies to routes for traffic management:

```yaml
# Rate Limiting
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: rate-limit-policy
spec:
  targetRef:
    kind: ProxyRoute
    name: echo-route
  rateLimit:
    requestsPerSecond: 100
    burst: 50
---
# CORS
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: cors-policy
spec:
  targetRef:
    kind: ProxyRoute
    name: echo-route
  cors:
    allowOrigins: ["*"]
    allowMethods: ["GET", "POST", "PUT", "DELETE"]
    allowHeaders: ["Content-Type", "Authorization"]
    maxAgeSeconds: 3600
---
# JWT Validation
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: jwt-policy
spec:
  targetRef:
    kind: ProxyRoute
    name: echo-route
  jwt:
    issuer: "https://auth.example.com"
    audience: ["api.example.com"]
    jwksUri: "https://auth.example.com/.well-known/jwks.json"
```

### TLS/HTTPS Configuration

```yaml
# Create TLS secret
kubectl create secret tls example-tls \
  --cert=path/to/cert.pem \
  --key=path/to/key.pem

# Configure HTTPS listener
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: main-gateway
spec:
  vipRef: default-vip
  listeners:
  - name: https
    port: 443
    protocol: HTTPS
    hostnames:
    - "*.example.com"
    tls:
      secretRef:
        name: example-tls
        namespace: default
      minVersion: "TLS1.2"
```

## Verification

### Check Component Health

```bash
# Controller health
kubectl get pods -n novaedge-system -l app.kubernetes.io/name=novaedge-controller
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-controller | grep -i error

# Agent health
kubectl get pods -n novaedge-system -l app.kubernetes.io/name=novaedge-agent
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-agent | grep -i error

# Check agent health endpoints
kubectl exec -n novaedge-system -it <agent-pod-name> -- curl localhost:8082/healthz
kubectl exec -n novaedge-system -it <agent-pod-name> -- curl localhost:8082/ready
```

### Check Resource Status

```bash
# Using kubectl
kubectl get proxyvips
kubectl get proxygateways
kubectl get proxyroutes
kubectl get proxybackends
kubectl get proxypolicies

# Get detailed status
kubectl describe proxygateway main-gateway
kubectl describe proxyroute echo-route

# Using novactl (if built)
./novactl get gateways
./novactl get routes
./novactl describe gateway main-gateway
```

### Check Metrics

```bash
# Controller metrics (if Prometheus configured)
kubectl port-forward -n novaedge-system svc/novaedge-controller 8080:8080
curl http://localhost:8080/metrics

# Agent metrics
kubectl port-forward -n novaedge-system <agent-pod> 9091:9091
curl http://localhost:9091/metrics
```

### Verify Traffic Flow

```bash
# Check if VIP is responding
curl -v http://<vip-address>/

# Check specific host
curl -v -H "Host: echo.example.com" http://<vip-address>/

# Check HTTPS (if configured)
curl -v -k https://<vip-address>/

# Check from within cluster
kubectl run -it --rm test --image=curlimages/curl --restart=Never -- \
  curl -v http://<vip-address>/
```

## Troubleshooting

### Controller Issues

**Controller pod not starting:**
```bash
# Check pod status
kubectl describe pod -n novaedge-system <controller-pod-name>

# Check logs
kubectl logs -n novaedge-system <controller-pod-name>

# Common issues:
# - CRDs not installed: make install-crds
# - RBAC permissions: verify service account and roles
# - Image pull errors: check image name and registry access
```

**Controller reconciliation errors:**
```bash
# Check controller logs for specific errors
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-controller | grep -i error

# Common issues:
# - Invalid resource specs: kubectl describe <resource>
# - Missing referenced resources: check VIPRef, BackendRef, etc.
```

### Agent Issues

**Agent pods not starting:**
```bash
# Check DaemonSet status
kubectl get daemonset -n novaedge-system novaedge-agent

# Check pod events
kubectl describe pod -n novaedge-system <agent-pod-name>

# Common issues:
# - hostNetwork conflicts: ensure ports 80, 443 are free
# - Insufficient privileges: check securityContext in DaemonSet
# - Node selector mismatch: verify nodeSelector/tolerations
```

**VIP not working:**
```bash
# Check VIP assignment
kubectl get proxyvip -A

# Check agent logs for VIP errors
kubectl logs -n novaedge-system <agent-pod-name> | grep -i vip

# L2 mode: Verify interface exists on node
kubectl exec -n novaedge-system <agent-pod-name> -- ip addr show

# BGP mode: Check BGP peering
kubectl logs -n novaedge-system <agent-pod-name> | grep -i bgp

# OSPF mode: Check OSPF neighbors
kubectl logs -n novaedge-system <agent-pod-name> | grep -i ospf
```

**Traffic not routing:**
```bash
# Check if route exists
kubectl get proxyroute
kubectl describe proxyroute <route-name>

# Check backend health
kubectl get proxybackend
kubectl describe proxybackend <backend-name>

# Check agent received configuration
kubectl logs -n novaedge-system <agent-pod-name> | grep "Applying config"

# Check routing logs
kubectl logs -n novaedge-system <agent-pod-name> | grep -i "routing\|request"
```

### Configuration Issues

**Backend endpoints not found:**
```bash
# Verify service exists
kubectl get svc <service-name>

# Check EndpointSlices
kubectl get endpointslices -l kubernetes.io/service-name=<service-name>

# Verify backend configuration
kubectl describe proxybackend <backend-name>
```

**TLS certificate issues:**
```bash
# Verify secret exists
kubectl get secret <tls-secret-name>

# Check secret format
kubectl get secret <tls-secret-name> -o yaml

# Verify certificate
kubectl get secret <tls-secret-name> -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout
```

**Policy not applied:**
```bash
# Check policy exists
kubectl get proxypolicy <policy-name>

# Verify targetRef
kubectl describe proxypolicy <policy-name>

# Check agent logs for policy application
kubectl logs -n novaedge-system <agent-pod-name> | grep -i policy
```

### Performance Issues

**High latency:**
```bash
# Check backend health
kubectl describe proxybackend <backend-name>

# Check circuit breaker status in logs
kubectl logs -n novaedge-system <agent-pod-name> | grep -i "circuit\|health"

# Verify load balancing algorithm
kubectl get proxybackend <backend-name> -o jsonpath='{.spec.lbPolicy}'

# Check connection pool settings
kubectl describe proxybackend <backend-name>
```

**High CPU/memory on agents:**
```bash
# Check resource usage
kubectl top pods -n novaedge-system

# Check active connections
kubectl logs -n novaedge-system <agent-pod-name> | grep "active connections"

# Check for config update loops
kubectl logs -n novaedge-system <agent-pod-name> | grep "Applying config" | tail -20
```

## Advanced Configuration

### Multi-Cluster Setup

For multi-cluster deployments, deploy NovaEdge controller and agents to each cluster with cluster-specific VIP configurations.

### High Availability

- **Controller**: Deploy with multiple replicas, leader election is automatic
- **Agents**: DaemonSet ensures one per node, VIP failover is automatic
- **Database**: Consider external etcd for controller state (not yet implemented)

### Observability Integration

**OpenTelemetry Tracing:**
```yaml
# Configure in agent DaemonSet
env:
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: "http://otel-collector:4317"
- name: OTEL_TRACE_SAMPLE_RATE
  value: "0.1"
```

**Prometheus:**
```yaml
# ServiceMonitor for controller
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: novaedge-controller
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: novaedge-controller
  endpoints:
  - port: metrics
    interval: 30s
```

## Next Steps

- Review [Gateway API documentation](gateway-api.md)
- Explore sample configurations in `config/samples/`
- Set up monitoring and alerting
- Configure backup and disaster recovery
- Test failover scenarios
- Tune performance settings
