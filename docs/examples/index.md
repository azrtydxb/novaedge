# Examples

Ready-to-use configuration examples for common NovaEdge use cases.

## Quick Reference

| Example | Description |
|---------|-------------|
| [Basic HTTP Load Balancer](#basic-http-load-balancer) | Simple HTTP routing |
| [HTTPS with TLS](#https-with-tls-termination) | TLS termination |
| [API Gateway](#api-gateway-with-rate-limiting) | Rate limiting and JWT |
| [Blue-Green Deployment](#blue-green-deployment) | Traffic splitting |
| [Multi-Tenant](#multi-tenant-setup) | Namespace isolation |
| [WebSocket](#websocket-support) | WebSocket routing |
| [gRPC](#grpc-load-balancing) | gRPC routing |
| [SD-WAN Samples](#sd-wan-samples) | WAN link and path selection configs |

## Sample Configuration Files

The following sample YAML files are available in the `config/samples/` directory:

### SD-WAN Samples

| File | Description |
|------|-------------|
| `proxywanlink_primary_sample.yaml` | Primary WAN link with SLA thresholds (latency, jitter, packet loss) |
| `proxywanlink_backup_sample.yaml` | Backup WAN link with higher cost and relaxed SLA |
| `proxywanpolicy_voice_sample.yaml` | Voice traffic policy with lowest-latency strategy and EF DSCP marking |
| `proxywanpolicy_bulk_sample.yaml` | Bulk transfer policy with lowest-cost strategy |

## Basic HTTP Load Balancer

Simple HTTP load balancing across multiple backends.

```yaml
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyVIP
metadata:
  name: web-vip
spec:
  address: 192.168.1.100/32
  mode: L2
  interface: eth0
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: web-gateway
spec:
  vipRef: web-vip
  listeners:
    - name: http
      port: 80
      protocol: HTTP
      hostnames:
        - "*.example.com"
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: web-backend
spec:
  serviceRef:
    name: web-service
    port: 8080
  lbPolicy: RoundRobin
  healthCheck:
    interval: 10s
    httpHealthCheck:
      path: /health
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: web-route
spec:
  parentRefs:
    - name: web-gateway
  hostnames:
    - www.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRef:
        name: web-backend
```

## HTTPS with TLS Termination

HTTPS with automatic HTTP to HTTPS redirect.

```yaml
---
# Create TLS secret first:
# kubectl create secret tls example-tls --cert=cert.pem --key=key.pem
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: https-gateway
spec:
  vipRef: web-vip
  listeners:
    - name: http
      port: 80
      protocol: HTTP
    - name: https
      port: 443
      protocol: HTTPS
      hostnames:
        - "*.example.com"
      tls:
        mode: Terminate
        certificateRefs:
          - name: example-tls
---
# HTTP to HTTPS redirect
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: https-redirect
spec:
  parentRefs:
    - name: https-gateway
      sectionName: http
  rules:
    - filters:
        - type: RequestRedirect
          requestRedirect:
            scheme: https
            statusCode: 301
---
# Main route on HTTPS
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: secure-route
spec:
  parentRefs:
    - name: https-gateway
      sectionName: https
  hostnames:
    - www.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRef:
        name: web-backend
```

## API Gateway with Rate Limiting

API gateway with JWT auth and rate limiting.

```yaml
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: api-gateway
spec:
  vipRef: api-vip
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      hostnames:
        - "api.example.com"
      tls:
        mode: Terminate
        certificateRefs:
          - name: api-tls
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: api-backend
spec:
  serviceRef:
    name: api-service
    port: 8080
  lbPolicy: P2C
  healthCheck:
    interval: 5s
    httpHealthCheck:
      path: /healthz
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: api-route
spec:
  parentRefs:
    - name: api-gateway
  hostnames:
    - api.example.com
  policyRefs:
    - name: api-rate-limit
    - name: api-jwt-auth
    - name: api-cors
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /v1
      backendRef:
        name: api-backend
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: api-rate-limit
spec:
  targetRef:
    kind: ProxyRoute
    name: api-route
  rateLimit:
    requestsPerSecond: 100
    burstSize: 150
    key: "header:X-API-Key"
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: api-jwt-auth
spec:
  targetRef:
    kind: ProxyRoute
    name: api-route
  jwt:
    issuer: "https://auth.example.com"
    audience:
      - "api.example.com"
    jwksUri: "https://auth.example.com/.well-known/jwks.json"
    claimsToHeaders:
      - claim: sub
        header: X-User-ID
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: api-cors
spec:
  targetRef:
    kind: ProxyRoute
    name: api-route
  cors:
    allowOrigins:
      - "https://app.example.com"
    allowMethods:
      - GET
      - POST
      - PUT
      - DELETE
    allowHeaders:
      - Authorization
      - Content-Type
    maxAge: 86400
```

## Blue-Green Deployment

Traffic splitting for canary releases.

```yaml
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: app-v1
spec:
  serviceRef:
    name: app-v1
    port: 8080
  lbPolicy: RoundRobin
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: app-v2
spec:
  serviceRef:
    name: app-v2
    port: 8080
  lbPolicy: RoundRobin
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: canary-route
spec:
  parentRefs:
    - name: app-gateway
  hostnames:
    - app.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: app-v1
          weight: 90    # 90% to v1
        - name: app-v2
          weight: 10    # 10% to v2 (canary)
```

## Multi-Tenant Setup

Namespace isolation for multi-tenant applications.

```yaml
---
# Tenant A namespace
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: tenant-a-route
  namespace: tenant-a
spec:
  parentRefs:
    - name: shared-gateway
      namespace: nova-system
  hostnames:
    - tenant-a.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRef:
        name: tenant-a-backend
        namespace: tenant-a
---
# Tenant B namespace
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: tenant-b-route
  namespace: tenant-b
spec:
  parentRefs:
    - name: shared-gateway
      namespace: nova-system
  hostnames:
    - tenant-b.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRef:
        name: tenant-b-backend
        namespace: tenant-b
```

## WebSocket Support

WebSocket routing with connection upgrade.

```yaml
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: ws-gateway
spec:
  vipRef: ws-vip
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        mode: Terminate
        certificateRefs:
          - name: ws-tls
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: ws-backend
spec:
  serviceRef:
    name: websocket-service
    port: 8080
  lbPolicy: RingHash    # Session affinity for WebSockets
  hashPolicy:
    type: Header
    headerName: X-Session-ID
  timeout:
    idle: 3600s         # Long idle timeout for WebSockets
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: ws-route
spec:
  parentRefs:
    - name: ws-gateway
  hostnames:
    - ws.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /ws
      backendRef:
        name: ws-backend
```

## gRPC Load Balancing

gRPC service routing.

```yaml
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: grpc-gateway
spec:
  vipRef: grpc-vip
  listeners:
    - name: grpc
      port: 443
      protocol: HTTPS
      tls:
        mode: Terminate
        certificateRefs:
          - name: grpc-tls
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: grpc-backend
spec:
  serviceRef:
    name: grpc-service
    port: 9090
  lbPolicy: RoundRobin
  healthCheck:
    protocol: GRPC
    interval: 10s
    grpcHealthCheck:
      serviceName: "grpc.health.v1.Health"
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: grpc-route
spec:
  parentRefs:
    - name: grpc-gateway
  hostnames:
    - grpc.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRef:
        name: grpc-backend
```

## BGP Mode with ECMP

Active-active load balancing with BGP.

```yaml
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyVIP
metadata:
  name: bgp-vip
spec:
  address: 10.0.100.1/32
  mode: BGP
  bgp:
    asn: 65000
    routerID: "10.0.0.1"
    peers:
      - address: "10.0.0.254"
        asn: 65001
        port: 179
      - address: "10.0.0.253"
        asn: 65001
        port: 179
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: bgp-gateway
spec:
  vipRef: bgp-vip
  listeners:
    - name: http
      port: 80
      protocol: HTTP
```

## Standalone Mode Config

Complete standalone configuration file.

```yaml
# /etc/novaedge/config.yaml
version: "1.0"

listeners:
  - name: http
    port: 80
    protocol: HTTP
  - name: https
    port: 443
    protocol: HTTPS
    tls:
      certFile: /etc/novaedge/certs/server.crt
      keyFile: /etc/novaedge/certs/server.key

routes:
  - name: api-route
    match:
      hostnames:
        - "api.example.com"
      path:
        type: PathPrefix
        value: /api
    backends:
      - name: api-backend
    policies:
      - rate-limit
      - cors

  - name: web-route
    match:
      hostnames:
        - "www.example.com"
      path:
        type: PathPrefix
        value: /
    backends:
      - name: web-backend

backends:
  - name: api-backend
    endpoints:
      - address: api1:8080
      - address: api2:8080
    lbPolicy: P2C
    healthCheck:
      protocol: HTTP
      path: /health
      interval: 5s

  - name: web-backend
    endpoints:
      - address: web1:8080
      - address: web2:8080
    lbPolicy: RoundRobin
    healthCheck:
      protocol: HTTP
      path: /
      interval: 10s

policies:
  - name: rate-limit
    type: RateLimit
    rateLimit:
      requestsPerSecond: 100
      burstSize: 150
      key: client_ip

  - name: cors
    type: CORS
    cors:
      allowOrigins:
        - "https://app.example.com"
      allowMethods:
        - GET
        - POST
      maxAge: 86400

vips:
  - name: main-vip
    address: 192.168.1.100/32
    mode: L2
    interface: eth0
```

## Next Steps

- [Quick Start](../getting-started/quickstart.md) - Deploy your first gateway
- [Routing Guide](../user-guide/routing.md) - Advanced routing
- [CRD Reference](../reference/crd-reference.md) - Complete CRD specs
