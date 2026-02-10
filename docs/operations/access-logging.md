# Access Logging

NovaEdge provides configurable access logging for HTTP requests, supporting
multiple output formats, file rotation, sampling, and status code filtering.

## Overview

Access logging captures detailed information about every HTTP request processed
by the proxy, including client IP, request method, URI, response status, timing,
and more.

## Configuration

### Kubernetes CRD (Per-Route)

Access logging is configured in the `ProxyRoute` spec:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: api-route
spec:
  hostnames:
    - api.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: api-backend
  accessLog:
    enabled: true
    format: json
    output: both
    filePath: /var/log/novaedge/access.log
    maxSize: "100Mi"
    maxBackups: 5
    sampleRate: 1.0
```

### Standalone Mode

In `novaedge.yaml`:

```yaml
global:
  accessLog:
    enabled: true
    format: json          # clf, json, or custom
    output: both          # stdout, file, or both
    path: /var/log/novaedge/access.log
    maxSize: "100Mi"
    maxBackups: 5
    sampleRate: 1.0
    filterStatusCodes: [] # empty = log all
```

## Log Formats

### Common Log Format (CLF)

```
format: clf
```

Output:
```
192.168.1.100 - - [15/Jan/2024:10:30:00 +0000] "GET /api/v1/users HTTP/1.1" 200 1234 "-" "curl/8.0" 0.045 req-abc123
```

Fields: client IP, timestamp, method, URI, protocol, status, body bytes, referer,
user agent, duration (seconds), request ID.

### JSON Format

```
format: json
```

Output:
```json
{
  "client_ip": "192.168.1.100",
  "timestamp": "2024-01-15T10:30:00.000000000Z",
  "method": "GET",
  "uri": "/api/v1/users",
  "protocol": "HTTP/1.1",
  "status_code": 200,
  "body_bytes_sent": 1234,
  "duration_seconds": 0.045,
  "user_agent": "curl/8.0",
  "referer": "",
  "request_id": "req-abc123",
  "host": "api.example.com"
}
```

### Custom Template

```yaml
format: custom
template: "{{.Method}} {{.URI}} {{.StatusCode}} {{.Duration}}"
```

Available template fields:

| Field | Type | Description |
|-------|------|-------------|
| `{{.ClientIP}}` | string | Client IP address |
| `{{.Timestamp}}` | string | Request timestamp (RFC3339Nano) |
| `{{.Method}}` | string | HTTP method |
| `{{.URI}}` | string | Request URI |
| `{{.Protocol}}` | string | HTTP protocol version |
| `{{.StatusCode}}` | int | Response status code |
| `{{.BodyBytesSent}}` | int64 | Response body size in bytes |
| `{{.Duration}}` | float64 | Request duration in seconds |
| `{{.UserAgent}}` | string | User-Agent header |
| `{{.Referer}}` | string | Referer header |
| `{{.RequestID}}` | string | X-Request-ID header |
| `{{.Host}}` | string | Request Host header |
| `{{.UpstreamAddr}}` | string | Backend address |
| `{{.UpstreamResponseTime}}` | float64 | Backend response time |

## Output Destinations

| Value | Description |
|-------|-------------|
| `stdout` | Write to standard output (default) |
| `file` | Write to file only |
| `both` | Write to both stdout and file |

## File Rotation

When writing to files, NovaEdge supports automatic log rotation:

- **maxSize**: Maximum file size before rotation (e.g., `"100Mi"`, `"1Gi"`)
- **maxBackups**: Number of rotated files to keep (default: 5)

Rotated files are named with a numeric suffix: `access.log.1`, `access.log.2`, etc.

## Sampling

For high-traffic routes, you can reduce logging overhead by sampling:

```yaml
sampleRate: 0.1  # Log 10% of requests
```

- `1.0` = log every request (default)
- `0.5` = log ~50% of requests
- `0.01` = log ~1% of requests

## Status Code Filtering

Log only specific status codes:

```yaml
filterStatusCodes:
  - 400
  - 401
  - 403
  - 404
  - 500
  - 502
  - 503
```

When empty (default), all status codes are logged.

## Viewing Access Logs

### Using novactl

```bash
# Stream access logs from a specific node
novactl logs access --node worker-1

# Follow access logs in real-time
novactl logs access --node worker-1 -f

# Show last 200 lines
novactl logs access --node worker-1 --tail 200
```

### Using kubectl

```bash
kubectl logs -l app=novaedge-agent -f
```
