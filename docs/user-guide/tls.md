# TLS Configuration

Configure TLS termination, passthrough, and mutual TLS (mTLS).

## Overview

```mermaid
flowchart LR
    subgraph TLS["TLS Modes"]
        T["Terminate"]
        P["Passthrough"]
        M["mTLS"]
    end

    Client((Client)) -->|"HTTPS"| T
    T -->|"HTTP"| Backend1((Backend))

    Client2((Client)) -->|"TLS"| P
    P -->|"TLS"| Backend2((Backend))

    Client3((Client)) -->|"mTLS"| M
    M -->|"mTLS"| Backend3((Backend))
```

## TLS Termination

Terminate TLS at the gateway and forward plain HTTP to backends.

### Using Kubernetes Secrets

```yaml
# Create TLS secret
kubectl create secret tls example-tls \
  --cert=cert.pem \
  --key=key.pem \
  -n default

# Gateway configuration
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: https-gateway
spec:
  vipRef: main-vip
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      hostnames:
        - "*.example.com"
      tls:
        mode: Terminate
        certificateRefs:
          - name: example-tls
            namespace: default
```

### Multiple Certificates (SNI)

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: multi-cert-gateway
spec:
  vipRef: main-vip
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        mode: Terminate
        certificateRefs:
          - name: api-tls        # For api.example.com
          - name: web-tls        # For www.example.com
          - name: admin-tls      # For admin.example.com
```

NovaEdge automatically selects the correct certificate based on SNI.

### TLS Versions

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: tls-config-gateway
spec:
  vipRef: main-vip
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        mode: Terminate
        minVersion: "TLS1.2"  # Minimum TLS version
        maxVersion: "TLS1.3"  # Maximum TLS version
        cipherSuites:
          - TLS_AES_128_GCM_SHA256
          - TLS_AES_256_GCM_SHA384
          - TLS_CHACHA20_POLY1305_SHA256
        certificateRefs:
          - name: example-tls
```

### TLS Options

| Field | Default | Description |
|-------|---------|-------------|
| `mode` | Terminate | TLS mode (Terminate, Passthrough) |
| `minVersion` | TLS1.2 | Minimum TLS version |
| `maxVersion` | TLS1.3 | Maximum TLS version |
| `cipherSuites` | [] | Allowed cipher suites |
| `certificateRefs` | [] | Certificate secrets |

## TLS Passthrough

Pass encrypted traffic directly to backends without termination. NovaEdge reads the SNI (Server Name Indication) from the TLS ClientHello message without decrypting the connection, then routes the connection to the appropriate backend based on the hostname.

### How It Works

1. Client initiates a TLS connection to the listener port
2. NovaEdge reads the TLS ClientHello message and extracts the SNI hostname
3. The SNI hostname is matched against configured routes (exact match, then wildcard)
4. The entire TLS connection (including the ClientHello) is forwarded to the selected backend
5. The TLS handshake completes directly between client and backend (end-to-end encryption)

```mermaid
sequenceDiagram
    participant C as Client
    participant G as Gateway
    participant B as Backend

    C->>G: TLS ClientHello (SNI: api.example.com)
    G->>G: Extract SNI (no decryption)
    G->>G: Match route by hostname
    G->>B: Forward full TLS connection
    B->>C: TLS handshake continues
    Note over C,B: End-to-end encryption preserved
```

### Kubernetes Configuration

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: passthrough-gateway
spec:
  vipRef: main-vip
  listeners:
    - name: tls
      port: 443
      protocol: TLS
      hostnames:
        - "api.example.com"
        - "app.example.com"
        - "*.internal.example.com"
```

### Route Configuration

TLS passthrough routes use the `ProxyRoute` resource with hostname-based matching:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: api-tls-route
spec:
  hostnames:
    - "api.example.com"
  rules:
    - backendRefs:
        - name: api-backend
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: app-tls-route
spec:
  hostnames:
    - "app.example.com"
  rules:
    - backendRefs:
        - name: app-backend
```

### Hostname Matching

TLS passthrough supports two types of hostname matching:

| Match Type | Example | Matches |
|------------|---------|---------|
| Exact | `api.example.com` | Only `api.example.com` |
| Wildcard | `*.example.com` | `foo.example.com`, `bar.example.com` (not `example.com`) |

Exact matches take priority over wildcard matches.

### Gateway API TLSRoute

NovaEdge also supports the Gateway API `TLSRoute` resource for TLS passthrough:

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TLSRoute
metadata:
  name: api-tlsroute
spec:
  parentRefs:
    - name: tls-gateway
      sectionName: tls
  hostnames:
    - "api.example.com"
  rules:
    - backendRefs:
        - name: api-service
          port: 443
```

See [Gateway API - L4 Routes](../advanced/gateway-api.md#l4-routes) for details.

### Standalone Configuration

```yaml
l4Listeners:
  - name: tls-passthrough
    port: 443
    protocol: TLS
    tlsRoutes:
      - hostname: "api.example.com"
        backend: api-backend
      - hostname: "*.internal.example.com"
        backend: internal-backend
```

### TLS Passthrough Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `novaedge_l4_tls_passthrough_total` | Counter | TLS passthrough connections by SNI |
| `novaedge_l4_sni_routing_errors_total` | Counter | SNI routing errors |
| `novaedge_l4_connections_total` | Counter | Total L4 connections |
| `novaedge_l4_active_connections` | Gauge | Currently active connections |

For comprehensive L4 proxying documentation including TCP and UDP, see [Layer 4 TCP/UDP Proxying](l4-proxying.md).

## Backend TLS (Upstream TLS)

Encrypt traffic between NovaEdge and backends.

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: secure-backend
spec:
  serviceRef:
    name: api-service
    port: 8443
  tls:
    enabled: true
    serverName: "api.internal.example.com"
    insecureSkipVerify: false
    caSecretRef:
      name: backend-ca
```

### Backend TLS Options

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | false | Enable TLS to backend |
| `serverName` | - | SNI server name |
| `insecureSkipVerify` | false | Skip certificate verification |
| `caSecretRef` | - | CA certificate secret |

## Mutual TLS (mTLS)

Require client certificates for authentication.

### Client Certificate Validation

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: mtls-gateway
spec:
  vipRef: main-vip
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        mode: Terminate
        certificateRefs:
          - name: server-tls
        clientValidation:
          mode: Require  # Require, Request, or Optional
          caSecretRef:
            name: client-ca
```

### Client Validation Modes

| Mode | Description |
|------|-------------|
| Require | Reject if no valid client cert |
| Request | Request cert, allow if missing |
| Optional | Accept any cert or none |

### Forward Client Certificate

Forward client certificate info to backends:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: mtls-forward-gateway
spec:
  vipRef: main-vip
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        mode: Terminate
        certificateRefs:
          - name: server-tls
        clientValidation:
          mode: Require
          caSecretRef:
            name: client-ca
          forwardClientCertificate:
            enabled: true
            header: X-Client-Certificate
            sanitize: true
```

## Certificate Management

### Generate Self-Signed Certificate

```bash
# Generate CA
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 1024 -out ca.crt \
  -subj "/CN=NovaEdge CA"

# Generate server certificate
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr \
  -subj "/CN=*.example.com"
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out server.crt -days 365 -sha256

# Create Kubernetes secret
kubectl create secret tls example-tls \
  --cert=server.crt \
  --key=server.key
```

### Using cert-manager

```yaml
# Certificate request
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-tls
spec:
  secretName: example-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
    - "*.example.com"
    - "example.com"
```

NovaEdge automatically reloads certificates when secrets are updated.

## HTTP to HTTPS Redirect

Redirect HTTP traffic to HTTPS:

```yaml
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: redirect-gateway
spec:
  vipRef: main-vip
  listeners:
    - name: http
      port: 80
      protocol: HTTP
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        mode: Terminate
        certificateRefs:
          - name: example-tls
---
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: https-redirect
spec:
  parentRefs:
    - name: redirect-gateway
      sectionName: http  # HTTP listener only
  rules:
    - filters:
        - type: RequestRedirect
          requestRedirect:
            scheme: https
            statusCode: 301
```

## TLS Metrics

| Metric | Description |
|--------|-------------|
| `novaedge_tls_handshakes_total` | TLS handshakes |
| `novaedge_tls_handshake_errors_total` | TLS handshake errors |
| `novaedge_tls_version` | TLS version used |
| `novaedge_mtls_client_auth_total` | mTLS authentications |
| `novaedge_mtls_client_auth_failed_total` | Failed mTLS auths |

## Troubleshooting

### Certificate Issues

```bash
# Verify secret contains correct data
kubectl get secret example-tls -o yaml

# Check certificate validity
kubectl get secret example-tls -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -dates

# Check certificate chain
openssl s_client -connect example.com:443 -servername example.com
```

### TLS Version Issues

```bash
# Test specific TLS version
openssl s_client -connect example.com:443 -tls1_2
openssl s_client -connect example.com:443 -tls1_3
```

### mTLS Issues

```bash
# Test with client certificate
openssl s_client -connect example.com:443 \
  -cert client.crt \
  -key client.key \
  -CAfile ca.crt
```

## Next Steps

- [Health Checks](health-checks.md) - Backend health checking
- [Policies](policies.md) - Security policies
- [Observability](../operations/observability.md) - TLS metrics
