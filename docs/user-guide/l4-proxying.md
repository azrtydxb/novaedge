# Layer 4 TCP/UDP Proxying

NovaEdge supports Layer 4 (L4) proxying for TCP and UDP traffic alongside its Layer 7 HTTP proxying capabilities. This enables proxying of non-HTTP protocols such as databases, message queues, DNS, and custom TCP/UDP services.

## Overview

L4 proxying operates at the transport layer, forwarding raw TCP connections or UDP packets to backend services without inspecting the application-layer payload. NovaEdge provides three L4 proxy modes:

- **TCP Proxy** — Bidirectional TCP connection forwarding with configurable timeouts and connection draining
- **UDP Proxy** — UDP packet forwarding with session affinity based on source IP hash
- **TLS Passthrough** — SNI-based routing without TLS termination, forwarding encrypted traffic directly to backends

## TCP Proxying

### How It Works

1. Client connects to a TCP listener port on the NovaEdge agent
2. NovaEdge selects a backend using round-robin load balancing
3. A connection is established to the backend
4. Data is copied bidirectionally between client and backend
5. When either side closes the connection, both sides are cleaned up

### Configuration (Kubernetes)

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: tcp-gateway
  namespace: default
spec:
  vipRef: my-vip
  listeners:
    - name: mysql
      port: 3306
      protocol: TCP
```

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: mysql-route
  namespace: default
spec:
  rules:
    - backendRefs:
        - name: mysql-backend
```

### Configuration (Standalone)

```yaml
version: "1"
listeners:
  - name: mysql
    port: 3306
    protocol: TCP

l4Listeners:
  - name: mysql-proxy
    port: 3306
    protocol: TCP
    backend: mysql-backend
    tcp:
      connectTimeout: 5s
      idleTimeout: 5m
      drainTimeout: 30s

backends:
  - name: mysql-backend
    endpoints:
      - address: "mysql-primary:3306"
      - address: "mysql-replica:3306"
    lbPolicy: RoundRobin
```

### TCP Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `connectTimeout` | 5s | Timeout for connecting to a backend |
| `idleTimeout` | 5m | Idle timeout before closing a connection |
| `bufferSize` | 32KB | Buffer size for bidirectional copy |
| `drainTimeout` | 30s | Timeout for draining connections during config changes |

## UDP Proxying

### How It Works

1. Client sends a UDP packet to a listener port
2. NovaEdge selects a backend using source IP hash (session affinity)
3. The packet is forwarded to the selected backend
4. Subsequent packets from the same source IP go to the same backend
5. Sessions expire after the configured timeout

### Configuration (Standalone)

```yaml
l4Listeners:
  - name: dns-proxy
    port: 53
    protocol: UDP
    backend: dns-backend
    udp:
      sessionTimeout: 30s
      bufferSize: 65535

backends:
  - name: dns-backend
    endpoints:
      - address: "dns-server-1:53"
      - address: "dns-server-2:53"
    lbPolicy: RoundRobin
```

### UDP Session Affinity

UDP is a connectionless protocol, so NovaEdge maintains "sessions" based on the client's source IP address. All packets from the same source IP are forwarded to the same backend for the duration of the session. Sessions expire after the configured timeout.

### UDP Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `sessionTimeout` | 30s | Idle timeout for UDP sessions |
| `bufferSize` | 65535 | Maximum UDP packet size |

## TLS Passthrough

See [TLS Passthrough](tls.md#tls-passthrough) for TLS passthrough configuration.

## Metrics

NovaEdge exposes the following L4-specific Prometheus metrics:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `novaedge_l4_connections_total` | Counter | protocol, listener, backend | Total L4 connections |
| `novaedge_l4_active_connections` | Gauge | protocol, listener | Currently active connections |
| `novaedge_l4_bytes_sent_total` | Counter | protocol, listener, backend | Bytes sent to clients |
| `novaedge_l4_bytes_received_total` | Counter | protocol, listener, backend | Bytes received from clients |
| `novaedge_l4_connection_duration_seconds` | Histogram | protocol, listener | Connection duration |
| `novaedge_l4_connection_errors_total` | Counter | protocol, listener, error_type | Connection errors |
| `novaedge_l4_udp_sessions_total` | Counter | listener, backend | Total UDP sessions |
| `novaedge_l4_udp_active_sessions` | Gauge | listener | Active UDP sessions |
| `novaedge_l4_tls_passthrough_total` | Counter | listener, sni | TLS passthrough connections |
| `novaedge_l4_sni_routing_errors_total` | Counter | listener, error_type | SNI routing errors |

## Gateway API Support

NovaEdge supports the Gateway API `TCPRoute` and `TLSRoute` resources for L4 routing. See [Gateway API - L4 Routes](../advanced/gateway-api.md#l4-routes) for details.
