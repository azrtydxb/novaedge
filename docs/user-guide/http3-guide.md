# HTTP/3 and QUIC Guide

This guide explains how to configure and use HTTP/3 with QUIC in NovaEdge.

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Configuration](#configuration)
4. [Testing HTTP/3](#testing-http3)
5. [Performance Tuning](#performance-tuning)
6. [Troubleshooting](#troubleshooting)
7. [Metrics](#metrics)

## Overview

HTTP/3 is the third major version of the Hypertext Transfer Protocol. It uses QUIC (Quick UDP Internet Connections) as its transport protocol instead of TCP. Key benefits include:

- **Faster Connection Establishment**: 0-RTT resumption reduces latency
- **Improved Performance**: No head-of-line blocking at transport layer
- **Better Mobility**: Connection migration across network changes
- **Enhanced Security**: TLS 1.3 is mandatory

### How HTTP/3 Works in NovaEdge

1. Clients initially connect via HTTP/1.1 or HTTP/2
2. NovaEdge sends an `Alt-Svc` header advertising HTTP/3 availability
3. Clients that support HTTP/3 can upgrade to QUIC on subsequent requests
4. HTTP/3 connections use UDP instead of TCP

## Prerequisites

### Requirements

- **TLS 1.3**: HTTP/3 mandates TLS 1.3 or higher
- **UDP Port**: Same port number as HTTPS (typically 443)
- **Firewall Rules**: Allow UDP traffic on the HTTP/3 port
- **Valid TLS Certificates**: Required for QUIC encryption

### Client Support

HTTP/3 is supported by:
- Chrome/Chromium 87+
- Firefox 88+
- Edge 87+
- Safari 14+
- curl 7.66+ (with `--http3` flag)

## Configuration

### Basic HTTP/3 Listener

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: my-gateway
  namespace: default
spec:
  vipRef: default-vip
  listeners:
  - name: http3
    port: 443
    protocol: HTTP3
    hostnames:
    - "*.example.com"
    tls:
      secretRef:
        name: my-tls-cert
        namespace: default
      minVersion: "TLS1.3"  # Required for HTTP/3
    quic:
      maxIdleTimeout: "30s"
      maxBiStreams: 100
      maxUniStreams: 100
      enable0RTT: true
```

### QUIC Configuration Options

| Parameter | Description | Default | Recommended |
|-----------|-------------|---------|-------------|
| `maxIdleTimeout` | Maximum idle time before connection closes | "30s" | "30s" - "120s" |
| `maxBiStreams` | Maximum bidirectional streams per connection | 100 | 100 - 1000 |
| `maxUniStreams` | Maximum unidirectional streams per connection | 100 | 100 - 1000 |
| `enable0RTT` | Enable 0-RTT resumption | true | true |

### Dual-Stack Configuration (HTTP/2 + HTTP/3)

For maximum compatibility, run both HTTP/2 and HTTP/3 on the same port:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: dual-stack-gateway
spec:
  vipRef: default-vip
  listeners:
  # HTTP/3 listener (UDP)
  - name: http3
    port: 443
    protocol: HTTP3
    hostnames:
    - "*.example.com"
    tls:
      secretRef:
        name: my-tls-cert
      minVersion: "TLS1.3"
    quic:
      maxIdleTimeout: "60s"
      maxBiStreams: 500
      maxUniStreams: 500
      enable0RTT: true

  # HTTP/2 fallback listener (TCP)
  - name: https
    port: 443
    protocol: HTTPS
    hostnames:
    - "*.example.com"
    tls:
      secretRef:
        name: my-tls-cert
      minVersion: "TLS1.2"
```

With this configuration:
- Both TCP and UDP traffic work on port 443
- Clients supporting HTTP/3 automatically upgrade
- Legacy clients continue using HTTP/2

### TLS Certificate Requirements

HTTP/3 requires TLS 1.3. Ensure your certificates are compatible:

```bash
# Generate a TLS 1.3 compatible certificate with Let's Encrypt
certbot certonly --standalone \
  -d example.com \
  -d www.example.com

# Create Kubernetes secret
kubectl create secret tls my-tls-cert \
  --cert=/etc/letsencrypt/live/example.com/fullchain.pem \
  --key=/etc/letsencrypt/live/example.com/privkey.pem
```

## Testing HTTP/3

### Using curl

```bash
# Test HTTP/3 connection
curl --http3 -v https://example.com/

# Force HTTP/3 only (fail if HTTP/3 unavailable)
curl --http3-only https://example.com/

# Check which protocol was used
curl -I --http3 https://example.com/ 2>&1 | grep -i "HTTP/3\|alt-svc"
```

### Using Chrome DevTools

1. Open Chrome DevTools (F12)
2. Go to Network tab
3. Right-click column headers → Enable "Protocol"
4. Visit your HTTP/3 enabled site
5. Look for "h3" or "h3-29" in the Protocol column

### Using chrome://net-internals

1. Navigate to `chrome://net-internals/#http3`
2. Check "Active QUIC sessions"
3. View QUIC connection details and statistics

### Using Firefox

1. Type `about:networking#http3` in address bar
2. View HTTP/3 connection information

## Performance Tuning

### Connection Limits

Adjust stream limits based on your workload:

```yaml
quic:
  # For high-concurrency applications
  maxBiStreams: 1000
  maxUniStreams: 1000

  # For low-latency requirements
  maxIdleTimeout: "15s"
  enable0RTT: true
```

### 0-RTT Optimization

0-RTT allows clients to send data on the first packet:

**Pros**:
- Reduces connection latency by one round trip
- Ideal for repeat visitors

**Cons**:
- Vulnerable to replay attacks on non-idempotent requests
- Should only be enabled for GET requests or with anti-replay protection

**Best Practice**:
```yaml
quic:
  enable0RTT: true  # Enable for performance
  # Implement application-level anti-replay protection for POST/PUT/DELETE
```

### Idle Timeout Tuning

```yaml
quic:
  # Short timeout for mobile clients (save battery)
  maxIdleTimeout: "30s"

  # Long timeout for persistent connections
  maxIdleTimeout: "120s"
```

### UDP Buffer Sizing

For high-throughput scenarios, tune UDP buffers at the OS level:

```bash
# Increase UDP receive buffer (Linux)
sysctl -w net.core.rmem_max=2500000
sysctl -w net.core.rmem_default=2500000

# Increase UDP send buffer
sysctl -w net.core.wmem_max=2500000
sysctl -w net.core.wmem_default=2500000

# Make permanent
echo "net.core.rmem_max=2500000" >> /etc/sysctl.conf
echo "net.core.wmem_max=2500000" >> /etc/sysctl.conf
```

## Troubleshooting

### HTTP/3 Not Working

**Check 1: Verify UDP Port is Open**
```bash
# Test UDP connectivity
nc -u -v -w 3 example.com 443

# Check firewall rules (Linux)
sudo iptables -L -n | grep 443
sudo ufw status | grep 443

# Check firewall rules (Kubernetes)
kubectl get svc -o wide  # Ensure UDP is exposed
```

**Check 2: Verify TLS 1.3 Support**
```bash
# Test TLS 1.3
openssl s_client -connect example.com:443 -tls1_3

# Check certificate validity
curl -vI https://example.com/ 2>&1 | grep -i "TLSv1.3"
```

**Check 3: Check Alt-Svc Header**
```bash
# Verify Alt-Svc is advertised
curl -I https://example.com/ | grep -i alt-svc
# Expected: alt-svc: h3=":443"; ma=2592000
```

**Check 4: View NovaEdge Logs**
```bash
# Check agent logs for HTTP/3 listener
kubectl logs -n novaedge-system -l app.kubernetes.io/name=novaedge-agent | grep -i "http3\|quic"

# Look for:
# - "Starting HTTP/3 listener"
# - "HTTP/3 server error" (indicates problems)
```

### Connection Failures

**Symptom**: Clients fall back to HTTP/2

**Diagnosis**:
```bash
# Check QUIC metrics
kubectl port-forward -n novaedge-system svc/novaedge-agent 9091:9091
curl http://localhost:9091/metrics | grep quic
```

**Common Issues**:

1. **UDP Blocked by Firewall**
   ```bash
   # Allow UDP on port 443
   sudo ufw allow 443/udp
   sudo iptables -A INPUT -p udp --dport 443 -j ACCEPT
   ```

2. **Invalid TLS Configuration**
   ```yaml
   # Ensure TLS 1.3
   tls:
     minVersion: "TLS1.3"  # Not TLS1.2
   ```

3. **QUIC Not Enabled on Client**
   ```bash
   # Chrome: Enable QUIC
   chrome://flags/#enable-quic

   # curl: Use --http3 flag
   curl --http3 https://example.com/
   ```

### Performance Issues

**High Latency**:
```yaml
# Reduce idle timeout
quic:
  maxIdleTimeout: "15s"
  enable0RTT: true
```

**Connection Drops**:
```yaml
# Increase stream limits
quic:
  maxBiStreams: 1000
  maxUniStreams: 1000
```

**Packet Loss**:
```bash
# Check QUIC metrics
curl http://localhost:9091/metrics | grep quic_packets

# Look for high ratio of:
# novaedge_quic_connection_errors_total / novaedge_http3_connections_total
```

## Metrics

NovaEdge exposes the following HTTP/3 and QUIC metrics:

### Connection Metrics

```promql
# Total HTTP/3 connections
novaedge_http3_connections_total

# Active QUIC streams
novaedge_quic_streams_active

# Connection errors
rate(novaedge_quic_connection_errors_total[5m])
```

### Performance Metrics

```promql
# HTTP/3 request rate
rate(novaedge_http3_requests_total[5m])

# HTTP/3 vs HTTP/2 ratio
rate(novaedge_http3_requests_total[5m]) / rate(novaedge_http_requests_total[5m])

# 0-RTT success rate
rate(novaedge_quic_0rtt_accepted_total[5m]) /
  (rate(novaedge_quic_0rtt_accepted_total[5m]) + rate(novaedge_quic_0rtt_rejected_total[5m]))
```

### Network Metrics

```promql
# QUIC packet rate
rate(novaedge_quic_packets_received_total[5m])
rate(novaedge_quic_packets_sent_total[5m])

# Packet loss rate
rate(novaedge_quic_connection_errors_total{error_type="packet_loss"}[5m])
```

### Grafana Dashboard Example

```json
{
  "title": "HTTP/3 Performance",
  "panels": [
    {
      "title": "HTTP/3 vs HTTP/2 Traffic",
      "targets": [
        {
          "expr": "rate(novaedge_http3_requests_total[5m])",
          "legendFormat": "HTTP/3"
        },
        {
          "expr": "rate(novaedge_http_requests_total[5m]) - rate(novaedge_http3_requests_total[5m])",
          "legendFormat": "HTTP/2"
        }
      ]
    },
    {
      "title": "0-RTT Success Rate",
      "targets": [
        {
          "expr": "rate(novaedge_quic_0rtt_accepted_total[5m]) / (rate(novaedge_quic_0rtt_accepted_total[5m]) + rate(novaedge_quic_0rtt_rejected_total[5m]))",
          "legendFormat": "Success Rate"
        }
      ]
    }
  ]
}
```

## Best Practices

### Security

1. **Always Use TLS 1.3**
   ```yaml
   tls:
     minVersion: "TLS1.3"
   ```

2. **Implement Anti-Replay for 0-RTT**
   - Use application-level tokens
   - Validate idempotency of requests
   - Consider disabling 0-RTT for sensitive endpoints

3. **Regular Certificate Rotation**
   ```bash
   # Automate with cert-manager
   kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
   ```

### Performance

1. **Enable Both HTTP/2 and HTTP/3**
   - Maximizes client compatibility
   - Allows graceful fallback

2. **Monitor Metrics**
   - Track HTTP/3 adoption rate
   - Watch for connection errors
   - Monitor 0-RTT success rate

3. **Tune Based on Workload**
   - High concurrency: Increase stream limits
   - Low latency: Enable 0-RTT, reduce timeouts
   - Mobile clients: Shorter idle timeouts

### Deployment

1. **Gradual Rollout**
   ```bash
   # Start with canary deployment
   kubectl apply -f http3-canary.yaml

   # Monitor metrics for 24-48 hours

   # Full rollout
   kubectl apply -f http3-production.yaml
   ```

2. **A/B Testing**
   - Route percentage of traffic to HTTP/3
   - Compare latency and success rates
   - Validate against HTTP/2 baseline

3. **Fallback Strategy**
   - Always provide HTTP/2 listener
   - Monitor fallback rate
   - Alert on high fallback percentage

## Examples

### Complete Production Configuration

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: production-gateway
  namespace: prod
spec:
  vipRef: production-vip
  listeners:
  # HTTP/3 for modern clients
  - name: http3
    port: 443
    protocol: HTTP3
    hostnames:
    - "*.example.com"
    tls:
      secretRef:
        name: production-tls
      minVersion: "TLS1.3"
    quic:
      maxIdleTimeout: "60s"
      maxBiStreams: 500
      maxUniStreams: 500
      enable0RTT: true

  # HTTP/2 fallback
  - name: https
    port: 443
    protocol: HTTPS
    hostnames:
    - "*.example.com"
    tls:
      secretRef:
        name: production-tls
      minVersion: "TLS1.2"

  # HTTP redirect to HTTPS
  - name: http
    port: 80
    protocol: HTTP
    hostnames:
    - "*.example.com"
```

## See Also

- [Deployment Guide](deployment-guide.md)
- [TLS Configuration](../README.md#tlshttps-configuration)
- [Performance Tuning](../README.md#performance-considerations)
- [QUIC Specification](https://www.rfc-editor.org/rfc/rfc9000.html)
- [HTTP/3 Specification](https://www.rfc-editor.org/rfc/rfc9114.html)
