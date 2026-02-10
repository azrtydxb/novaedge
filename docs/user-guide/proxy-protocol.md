# PROXY Protocol Support

NovaEdge supports the PROXY protocol (v1 and v2) for preserving real client IP addresses when traffic passes through intermediate load balancers or proxies.

## Overview

When NovaEdge sits behind a layer 4 load balancer (such as AWS NLB, HAProxy, or cloud load balancers), the original client IP is lost because the upstream proxy replaces it with its own IP. The PROXY protocol solves this by prepending a header to the connection that contains the original client information.

NovaEdge supports:

- **Receiving** PROXY protocol headers from upstream load balancers (listener-side)
- **Sending** PROXY protocol headers to backend services (upstream-side)

## Listener Configuration (Receiving)

### Kubernetes CRD

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: external-gateway
spec:
  vipRef: external-vip
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        secretRef:
          name: tls-cert
      proxyProtocol:
        enabled: true
        version: 0        # 0 = accept both v1 and v2
        trustedCIDRs:
          - "10.0.0.0/8"  # Only trust PROXY headers from internal IPs
          - "172.16.0.0/12"
```

### Standalone Mode

```yaml
listeners:
  - name: https
    port: 443
    protocol: HTTPS
    tls:
      certFile: /etc/tls/cert.pem
      keyFile: /etc/tls/key.pem
    proxyProtocol:
      enabled: true
      version: 0
      trustedCIDRs:
        - "10.0.0.0/8"
```

### Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable PROXY protocol parsing |
| `version` | int | `0` | Protocol version: `0` (both), `1` (v1 only), `2` (v2 only) |
| `trustedCIDRs` | []string | `[]` (all) | Source CIDRs from which PROXY headers are accepted |

### Security: Trusted CIDRs

Always configure `trustedCIDRs` in production. Without it, any client can send a PROXY protocol header to spoof their IP address. Only trust the IP addresses of your upstream load balancers.

## Backend Configuration (Sending)

To forward real client IPs to backends that support PROXY protocol:

### Kubernetes CRD

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyBackend
metadata:
  name: backend-with-proxy
spec:
  serviceRef:
    name: my-service
    port: 8080
  upstreamProxyProtocol:
    enabled: true
    version: 1    # Send v1 headers to backends
```

### Standalone Mode

```yaml
backends:
  - name: backend-with-proxy
    endpoints:
      - address: "10.0.1.1:8080"
    upstreamProxyProtocol:
      enabled: true
      version: 1
```

## Protocol Versions

### PROXY Protocol v1 (Text)

Human-readable text format. Example:
```
PROXY TCP4 192.168.1.100 10.0.0.1 12345 80\r\n
```

**Pros:** Simple, easy to debug
**Cons:** Slightly larger header, IPv6 addresses are longer

### PROXY Protocol v2 (Binary)

Binary format with a 12-byte signature. More compact and extensible.

**Pros:** Compact, supports TLV extensions
**Cons:** Not human-readable

### Recommendation

- Use **v1** for compatibility with most backends
- Use **v2** when connecting to modern proxies/backends that support it
- Use **version: 0** on listeners to accept both formats

## Architecture

```
Client (1.2.3.4)
    |
    v
Cloud LB (10.0.0.1) -- adds PROXY header: "src=1.2.3.4:54321"
    |
    v
NovaEdge Listener (PROXY protocol enabled)
    | parses header, sets RemoteAddr = 1.2.3.4:54321
    v
Backend (optional: receives PROXY header with real client IP)
```
