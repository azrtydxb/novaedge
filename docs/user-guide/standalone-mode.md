# Standalone Mode

Run NovaEdge as a standalone load balancer without Kubernetes. This mode is ideal for:

- Docker-based deployments
- Bare-metal servers
- Development and testing
- Edge locations without Kubernetes

## Quick Start

### Using Docker Compose

```bash
# From repository root
make standalone-up

# View logs
make standalone-logs

# Stop
make standalone-down
```

### With Monitoring (Prometheus + Grafana)

```bash
make standalone-monitoring
```

Access:

- Prometheus: http://localhost:9091
- Grafana: http://localhost:3000 (admin/admin)

### Running Directly

```bash
# Build the binary
make build-standalone

# Run with a config file
./bin/novaedge-standalone --config=/path/to/config.yaml
```

## Configuration

Edit `config.yaml` to customize your load balancer.

### Listeners

Define ports and protocols:

```yaml
listeners:
  - name: http
    port: 80
    protocol: HTTP
  - name: https
    port: 443
    protocol: HTTPS
    tls:
      certFile: /path/to/cert.crt
      keyFile: /path/to/key.pem
```

### Routes

Configure traffic routing:

```yaml
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
```

### Backends

Define upstream services:

```yaml
backends:
  - name: api-backend
    endpoints:
      - address: server1:8080
      - address: server2:8080
    lbPolicy: RoundRobin  # or P2C, EWMA, RingHash, Maglev
    healthCheck:
      protocol: HTTP
      path: /health
      interval: 10s
```

### Policies

Add rate limiting, CORS, security headers, and more:

```yaml
policies:
  - name: rate-limit
    type: RateLimit
    rateLimit:
      requestsPerSecond: 100
      burstSize: 150
      key: client_ip

  - name: security-headers
    type: SecurityHeaders
    securityHeaders:
      hsts:
        enabled: true
        maxAgeSeconds: 31536000
        includeSubdomains: true
      xFrameOptions: DENY
      xContentTypeOptions: true
      referrerPolicy: strict-origin-when-cross-origin
```

## TLS Configuration

### Self-Signed Certificates

```bash
# Generate self-signed cert for development
openssl req -x509 -newkey rsa:4096 \
  -keyout certs/server.key \
  -out certs/server.crt \
  -days 365 -nodes
```

### Configuration

```yaml
listeners:
  - name: https
    port: 443
    protocol: HTTPS
    tls:
      certFile: /etc/novaedge/certs/server.crt
      keyFile: /etc/novaedge/certs/server.key
      minVersion: "TLS1.3"
```

## VIP Management (Bare Metal)

For bare-metal deployments, configure VIPs for high availability:

```yaml
vips:
  - name: main-vip
    address: 192.168.1.100/32
    mode: L2       # L2 ARP, BGP, or OSPF
    interface: eth0
```

**Note:** VIP management requires root privileges and host network access.

## Hot Reload

Configuration changes are automatically detected and applied. Simply edit `config.yaml` and the load balancer will reload within 30 seconds.

## Metrics

Prometheus metrics available at `http://localhost:9090/metrics`:

| Metric | Description |
|--------|-------------|
| `novaedge_requests_total` | Total request count |
| `novaedge_request_duration_seconds` | Request latency histogram |
| `novaedge_upstream_health` | Backend health status |
| `novaedge_active_connections` | Current active connections |

## Health Checks

| Endpoint | Description |
|----------|-------------|
| `http://localhost:8080/healthz` | Liveness probe |
| `http://localhost:8080/ready` | Readiness probe |

## Command Line Options

```
--config              Path to configuration file (default: /etc/novaedge/config.yaml)
--node-name           Node identifier (default: hostname)
--metrics-port        Prometheus metrics port (default: 9090)
--health-probe-port   Health probe port (default: 8080)
--log-level           Log level: debug, info, warn, error (default: info)
```

## Complete Example

```yaml
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
  - name: web-route
    match:
      hostnames:
        - "www.example.com"
      path:
        type: PathPrefix
        value: /
    backends:
      - name: web-backend
    policies:
      - rate-limit
      - security-headers

  - name: api-route
    match:
      hostnames:
        - "api.example.com"
      path:
        type: PathPrefix
        value: /api
    backends:
      - name: api-backend

backends:
  - name: web-backend
    endpoints:
      - address: web1:8080
      - address: web2:8080
    lbPolicy: RoundRobin
    healthCheck:
      protocol: HTTP
      path: /health
      interval: 10s

  - name: api-backend
    endpoints:
      - address: api1:8080
      - address: api2:8080
    lbPolicy: P2C
    healthCheck:
      protocol: HTTP
      path: /healthz
      interval: 5s

policies:
  - name: rate-limit
    type: RateLimit
    rateLimit:
      requestsPerSecond: 100
      burstSize: 200
      key: client_ip

  - name: security-headers
    type: SecurityHeaders
    securityHeaders:
      hsts:
        enabled: true
        maxAgeSeconds: 31536000
      xFrameOptions: DENY
      xContentTypeOptions: true
```

## Differences from Kubernetes Mode

| Feature | Standalone | Kubernetes |
|---------|-----------|------------|
| Config Source | YAML file | CRDs + Secrets |
| Service Discovery | Static endpoints | EndpointSlices |
| TLS Certs | File paths | Kubernetes Secrets |
| VIP Management | Full support | Via DaemonSet |
| Scaling | Manual | Horizontal Pod Autoscaler |

## Docker Compose Example

```yaml
version: '3.8'

services:
  novaedge:
    image: novaedge:latest
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./config.yaml:/etc/novaedge/config.yaml
      - ./certs:/etc/novaedge/certs
    restart: unless-stopped

  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9091:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    restart: unless-stopped

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    restart: unless-stopped
```
