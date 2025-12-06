# NovaEdge Standalone Mode

Run NovaEdge as a standalone load balancer without Kubernetes. This mode is ideal for:
- Docker-based deployments
- Bare-metal servers
- Development and testing
- Edge locations without Kubernetes

## Quick Start

### Using Docker Compose

1. **Build and start:**
   ```bash
   # From repository root
   make standalone-up
   ```

2. **View logs:**
   ```bash
   make standalone-logs
   ```

3. **Stop:**
   ```bash
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

Edit `config.yaml` to customize your load balancer. The configuration supports:

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
Add rate limiting, CORS, and more:
```yaml
policies:
  - name: rate-limit
    type: RateLimit
    rateLimit:
      requestsPerSecond: 100
      burstSize: 150
      key: client_ip
```

## TLS Configuration

1. Place certificates in `certs/`:
   ```bash
   # Generate self-signed cert for development
   openssl req -x509 -newkey rsa:4096 \
     -keyout certs/server.key \
     -out certs/server.crt \
     -days 365 -nodes
   ```

2. Uncomment HTTPS listener in `config.yaml`

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
- `novaedge_requests_total` - Total request count
- `novaedge_request_duration_seconds` - Request latency histogram
- `novaedge_upstream_health` - Backend health status
- `novaedge_active_connections` - Current active connections

## Health Checks

- Liveness: `http://localhost:8080/healthz`
- Readiness: `http://localhost:8080/ready`

## Command Line Options

```
--config           Path to configuration file (default: /etc/novaedge/config.yaml)
--node-name        Node identifier (default: hostname)
--metrics-port     Prometheus metrics port (default: 9090)
--health-probe-port Health probe port (default: 8080)
--log-level        Log level: debug, info, warn, error (default: info)
```

## Architecture

```
                    +-----------------+
                    |   config.yaml   |
                    +--------+--------+
                             |
                             v
+------------+    +----------+----------+    +------------+
|   Client   |--->|  NovaEdge Standalone|--->|  Backend   |
+------------+    +----------+----------+    +------------+
                             |
                             v
                    +--------+--------+
                    |    Metrics      |
                    | (Prometheus)    |
                    +-----------------+
```

## Differences from Kubernetes Mode

| Feature | Standalone | Kubernetes |
|---------|-----------|------------|
| Config Source | YAML file | CRDs + Secrets |
| Service Discovery | Static endpoints | EndpointSlices |
| TLS Certs | File paths | Kubernetes Secrets |
| VIP Management | Full support | Via DaemonSet |
| Scaling | Manual | Horizontal Pod Autoscaler |
