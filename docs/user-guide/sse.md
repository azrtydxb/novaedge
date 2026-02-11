# Server-Sent Events (SSE) Support

NovaEdge provides first-class support for Server-Sent Events (SSE), a lightweight protocol for server-to-client streaming over HTTP.

## Overview

SSE allows servers to push data to clients over a long-lived HTTP connection. NovaEdge handles SSE connections with:

- Automatic SSE request detection via `Accept: text/event-stream` header
- Configurable idle timeouts (longer than regular HTTP requests)
- Heartbeat injection to keep connections alive through intermediate proxies
- Connection tracking with dedicated SSE metrics
- Graceful draining during configuration changes

## Configuration

### Kubernetes (ProxyGateway)

```yaml
apiVersion: novaedge.piwi3910.com/v1alpha1
kind: ProxyGateway
metadata:
  name: sse-gateway
spec:
  vipRef: my-vip
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        secretRef:
          name: tls-secret
  sse:
    idleTimeout: "10m"        # Max idle time for SSE connections (default: 5m)
    heartbeatInterval: "30s"  # Keepalive comment interval (default: 30s)
    maxConnections: 1000      # Max concurrent SSE connections (0 = unlimited)
```

### Standalone Mode

```yaml
version: "1"
global:
  logLevel: info
  metricsPort: 9090
  healthPort: 9091

listeners:
  - name: https
    port: 443
    protocol: HTTPS
    tls:
      certFile: /etc/novaedge/tls.crt
      keyFile: /etc/novaedge/tls.key
    sse:
      idleTimeout: "10m"
      heartbeatInterval: "30s"
      maxConnections: 1000
```

## How It Works

### Detection

NovaEdge detects SSE requests by checking the `Accept: text/event-stream` header. When detected:

1. Response buffering is disabled
2. SSE-specific headers are set:
   - `Content-Type: text/event-stream`
   - `Cache-Control: no-cache`
   - `Connection: keep-alive`
   - `X-Accel-Buffering: no`
3. The connection uses SSE-specific timeouts instead of standard request timeouts

### Heartbeat

To prevent intermediate proxies (nginx, cloud load balancers) from closing idle connections, NovaEdge injects SSE comment lines as heartbeats:

```
:keepalive

```

These comment lines are ignored by SSE clients per the specification.

### Graceful Draining

During configuration changes or shutdown, SSE connections are gracefully drained:

1. New SSE connections are rejected with HTTP 503
2. Existing connections continue until they naturally close or the drain timeout expires

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `novaedge_sse_active_connections` | Gauge | Current number of active SSE connections |
| `novaedge_sse_connection_duration_seconds` | Histogram | Duration of SSE connections |
| `novaedge_sse_heartbeats_sent_total` | Counter | Total heartbeat comments sent |

## Best Practices

1. **Set appropriate idle timeouts**: SSE connections should have longer timeouts than regular requests. The default 5-minute idle timeout works for most use cases.
2. **Monitor connection counts**: Use the `novaedge_sse_active_connections` metric to track resource usage.
3. **Use heartbeats**: Keep the default 30-second heartbeat interval to prevent proxy timeouts.
4. **Set max connections**: In production, set `maxConnections` to prevent resource exhaustion.
