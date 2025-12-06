# CRD Reference

NovaEdge uses Custom Resource Definitions (CRDs) to configure load balancing, routing, and policies.

## ProxyVIP

Defines a Virtual IP address for the load balancer.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyVIP
metadata:
  name: my-vip
spec:
  # VIP address with CIDR notation
  address: 192.168.1.100/32

  # VIP mode: L2, BGP, or OSPF
  mode: L2

  # Network interface for L2 mode
  interface: eth0

  # BGP configuration (for BGP mode)
  bgp:
    asn: 65000
    peers:
      - address: 192.168.1.1
        asn: 65001
        password: secret

  # OSPF configuration (for OSPF mode)
  ospf:
    area: 0.0.0.0
    helloInterval: 10s
    deadInterval: 40s
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.address` | string | Yes | VIP address with CIDR notation |
| `spec.mode` | string | Yes | VIP mode: `L2`, `BGP`, or `OSPF` |
| `spec.interface` | string | L2 only | Network interface for ARP |
| `spec.bgp` | object | BGP only | BGP configuration |
| `spec.ospf` | object | OSPF only | OSPF configuration |

---

## ProxyGateway

Defines listeners and binds them to a VIP.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: my-gateway
spec:
  # Reference to ProxyVIP
  vipRef: my-vip

  listeners:
    - name: http
      port: 80
      protocol: HTTP
      hostnames:
        - "*.example.com"

    - name: https
      port: 443
      protocol: HTTPS
      hostnames:
        - "*.example.com"
      tls:
        secretRef:
          name: my-tls-secret
          namespace: default
        minVersion: "TLS1.2"
        cipherSuites:
          - TLS_AES_128_GCM_SHA256
          - TLS_AES_256_GCM_SHA384

    - name: http3
      port: 443
      protocol: HTTP3
      hostnames:
        - "*.example.com"
      tls:
        secretRef:
          name: my-tls-secret
        minVersion: "TLS1.3"
      quic:
        maxIdleTimeout: "30s"
        maxBiStreams: 100
        enable0RTT: true
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.vipRef` | string | Yes | Reference to ProxyVIP name |
| `spec.listeners` | array | Yes | List of listener configurations |
| `spec.listeners[].name` | string | Yes | Unique listener name |
| `spec.listeners[].port` | int32 | Yes | Port number |
| `spec.listeners[].protocol` | string | Yes | Protocol: `HTTP`, `HTTPS`, `HTTP3`, `TCP`, `TLS` |
| `spec.listeners[].hostnames` | array | No | Hostnames to match (wildcards supported) |
| `spec.listeners[].tls` | object | HTTPS/HTTP3 | TLS configuration |
| `spec.listeners[].quic` | object | HTTP3 only | QUIC configuration |

---

## ProxyRoute

Defines routing rules for traffic.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: my-route
spec:
  # Reference to parent gateway(s)
  parentRefs:
    - name: my-gateway
      namespace: default

  # Hostnames to match
  hostnames:
    - "api.example.com"

  # Routing rules
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /api/v1
          headers:
            - name: X-API-Version
              value: v1
          method: GET

      # Backend reference
      backendRef:
        name: api-backend
        weight: 100

      # Request filters
      filters:
        - type: RequestHeaderModifier
          requestHeaderModifier:
            add:
              - name: X-Request-ID
                value: "${request_id}"
            set:
              - name: X-Forwarded-Proto
                value: https
            remove:
              - X-Legacy-Header

        - type: URLRewrite
          urlRewrite:
            path:
              type: ReplacePrefixMatch
              replacePrefixMatch: /v1

      # Response filters
      responseFilters:
        - type: ResponseHeaderModifier
          responseHeaderModifier:
            add:
              - name: X-Served-By
                value: novaedge
            remove:
              - Server

      # Policy references
      policyRefs:
        - name: rate-limit-policy
```

### Match Types

| Path Type | Description | Example |
|-----------|-------------|---------|
| `Exact` | Exact path match | `/api` matches only `/api` |
| `PathPrefix` | Prefix match | `/api` matches `/api`, `/api/v1`, etc. |
| `RegularExpression` | Regex match | `/api/v[0-9]+` |

### Filter Types

| Filter Type | Description |
|-------------|-------------|
| `RequestHeaderModifier` | Add, set, or remove request headers |
| `ResponseHeaderModifier` | Add, set, or remove response headers |
| `URLRewrite` | Rewrite URL path or hostname |
| `RequestRedirect` | Redirect to different URL |

---

## ProxyBackend

Defines backend services and load balancing.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: my-backend
spec:
  # Reference to Kubernetes Service
  serviceRef:
    name: my-service
    port: 8080

  # Or static endpoints
  endpoints:
    - address: 10.0.0.1
      port: 8080
    - address: 10.0.0.2
      port: 8080

  # Load balancing policy
  lbPolicy: RoundRobin  # RoundRobin, P2C, EWMA, RingHash, Maglev

  # Health check configuration
  healthCheck:
    interval: 10s
    timeout: 5s
    healthyThreshold: 2
    unhealthyThreshold: 3
    httpHealthCheck:
      path: /health
      expectedStatuses:
        - 200
        - 204

  # Circuit breaker configuration
  circuitBreaker:
    maxConnections: 1000
    maxPendingRequests: 100
    maxRetries: 3
    consecutiveErrors: 5
    ejectionTime: 30s

  # Connection pool configuration
  connectionPool:
    maxIdleConns: 100
    maxIdleConnsPerHost: 10
    idleConnTimeoutMs: 90000
```

### Load Balancing Policies

| Policy | Description |
|--------|-------------|
| `RoundRobin` | Equal distribution across endpoints |
| `P2C` | Power of Two Choices - pick best of 2 random |
| `EWMA` | Exponentially Weighted Moving Average latency |
| `RingHash` | Consistent hashing for session affinity |
| `Maglev` | Google's Maglev consistent hashing |

---

## ProxyPolicy

Defines policies for rate limiting, security, and more.

### Rate Limiting

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: rate-limit
spec:
  targetRef:
    kind: ProxyRoute
    name: my-route

  rateLimit:
    requestsPerSecond: 100
    burstSize: 150
    key: client_ip  # client_ip, header:<name>, cookie:<name>
```

### CORS

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: cors-policy
spec:
  targetRef:
    kind: ProxyRoute
    name: my-route

  cors:
    allowOrigins:
      - "https://example.com"
      - "https://*.example.com"
    allowMethods:
      - GET
      - POST
      - PUT
      - DELETE
    allowHeaders:
      - Authorization
      - Content-Type
    exposeHeaders:
      - X-Request-ID
    maxAge: 86400
    allowCredentials: true
```

### JWT Validation

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: jwt-policy
spec:
  targetRef:
    kind: ProxyRoute
    name: my-route

  jwt:
    issuer: https://auth.example.com
    audience:
      - api.example.com
    jwksUri: https://auth.example.com/.well-known/jwks.json
    headerName: Authorization
    headerPrefix: "Bearer "
```

### IP Filtering

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: ip-filter
spec:
  targetRef:
    kind: ProxyRoute
    name: my-route

  ipFilter:
    allowList:
      - 10.0.0.0/8
      - 192.168.0.0/16
    denyList:
      - 10.0.0.5/32
```

### Security Headers

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: security-headers
spec:
  targetRef:
    kind: ProxyRoute
    name: my-route

  securityHeaders:
    hsts:
      enabled: true
      maxAgeSeconds: 31536000
      includeSubdomains: true
      preload: true
    xFrameOptions: DENY
    xContentTypeOptions: true
    referrerPolicy: strict-origin-when-cross-origin
    contentSecurityPolicy: "default-src 'self'"
```

---

## Status Conditions

All NovaEdge CRDs include status conditions:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: Valid
      message: Configuration is valid and applied
      lastTransitionTime: "2024-01-15T10:30:00Z"
  observedGeneration: 5
```

### Common Condition Types

| Type | Description |
|------|-------------|
| `Ready` | Resource is ready and configuration applied |
| `Accepted` | Resource was accepted by controller |
| `Programmed` | Configuration pushed to agents |
| `Degraded` | Resource is partially working |

---

## Labels and Annotations

### Common Labels

```yaml
metadata:
  labels:
    app.kubernetes.io/name: my-app
    app.kubernetes.io/component: frontend
    novaedge.io/gateway: my-gateway
```

### Annotations

```yaml
metadata:
  annotations:
    # Custom logging level for this resource
    novaedge.io/log-level: debug

    # Skip validation (use with caution)
    novaedge.io/skip-validation: "true"
```
