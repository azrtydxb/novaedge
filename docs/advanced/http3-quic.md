# HTTP/3 (QUIC) Support

NovaEdge provides full HTTP/3 support over QUIC, offering reduced latency, improved reliability on lossy networks, and built-in encryption.

## Overview

HTTP/3 uses QUIC as its transport layer instead of TCP+TLS, providing:

- **0-RTT Connection Resumption**: Returning clients can send data immediately without waiting for a handshake
- **Multiplexed Streams**: No head-of-line blocking across streams
- **Connection Migration**: Clients can seamlessly switch networks (e.g., WiFi to cellular)
- **Built-in Encryption**: QUIC mandates TLS 1.3

## Configuration

### Kubernetes (ProxyGateway)

```yaml
apiVersion: novaedge.piwi3910.com/v1alpha1
kind: ProxyGateway
metadata:
  name: http3-gateway
spec:
  vipRef: external-vip
  listeners:
    # HTTPS listener (HTTP/1.1 + HTTP/2) with Alt-Svc advertising
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        secretRef:
          name: tls-cert
    # HTTP/3 listener on the same port (UDP)
    - name: http3
      port: 443
      protocol: HTTP3
      tls:
        secretRef:
          name: tls-cert
      quic:
        maxIdleTimeout: "30s"
        maxBiStreams: 100
        maxUniStreams: 100
        enable0RTT: true
  http3:
    enabled: true
    port: 443
    zeroRTT: true
    maxIdleTimeout: "30s"
    altSvcMaxAge: 2592000
```

### Standalone Mode

```yaml
listeners:
  - name: https
    port: 443
    protocol: HTTPS
    tls:
      certFile: /etc/novaedge/tls.crt
      keyFile: /etc/novaedge/tls.key
    http3:
      enabled: true
      zeroRTT: true
      maxIdleTimeout: "30s"
```

## Alt-Svc Header Advertisement

When HTTP/3 is enabled, NovaEdge automatically adds the `Alt-Svc` header to HTTP/1.1 and HTTP/2 responses:

```
Alt-Svc: h3=":443"; ma=2592000
```

This tells clients that HTTP/3 is available on the same port. Clients that support HTTP/3 will upgrade automatically on subsequent requests.

## 0-RTT (Zero Round Trip Time)

0-RTT allows returning clients to send application data in the first flight of QUIC packets, before the handshake completes. This reduces latency by one round trip.

**Security Consideration**: 0-RTT data can be replayed by an attacker. Only enable 0-RTT for endpoints that handle idempotent requests (GET, HEAD) or have application-level replay protection.

## Connection Coalescing

HTTP/3 supports connection coalescing: multiple hostnames can share the same QUIC connection if they resolve to the same IP and use a certificate that covers all hostnames. NovaEdge supports this through wildcard TLS certificates and SNI configuration.

## QUIC Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `maxIdleTimeout` | `30s` | Maximum idle timeout before closing the connection |
| `maxBiStreams` | `100` | Maximum concurrent bidirectional streams |
| `maxUniStreams` | `100` | Maximum concurrent unidirectional streams |
| `enable0RTT` | `true` | Enable 0-RTT connection resumption |

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `novaedge_http3_quic_connections` | Gauge | Active QUIC connections |
| `novaedge_http3_quic_streams` | Gauge | Active QUIC streams |
| `novaedge_http3_quic_handshake_duration_seconds` | Histogram | QUIC handshake duration |
| `novaedge_http3_quic_0rtt_attempts_total` | Counter | 0-RTT connection attempts |
| `novaedge_http3_quic_0rtt_successes_total` | Counter | Successful 0-RTT connections |
| `novaedge_http3_requests_total` | Counter | HTTP/3 requests by method and status |

## Graceful Shutdown

When shutting down, NovaEdge:

1. Stops accepting new QUIC connections
2. Sends QUIC CONNECTION_CLOSE frames to all active connections
3. Waits for in-flight requests to complete (up to the configured timeout)
4. Forces closure of remaining connections

## Testing HTTP/3

Use `curl` with HTTP/3 support:

```bash
curl --http3 https://example.com/
```

Or use the `quiche-client` tool:

```bash
quiche-client https://example.com/
```
