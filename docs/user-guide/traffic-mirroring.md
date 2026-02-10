# Traffic Mirroring (Shadow Traffic)

Traffic mirroring allows you to send a copy of live traffic to a secondary backend for testing, debugging, or observability purposes without affecting the original request flow.

## Overview

When traffic mirroring is enabled on a route rule, NovaEdge:

1. Forwards the original request to the primary backend as normal
2. Asynchronously clones the request and sends it to the mirror backend (fire-and-forget)
3. Discards the mirror response — the original request is never affected by the mirror

Mirror requests include an `X-Mirror: true` header so the mirror backend can distinguish them from real traffic.

## Configuration

### Per-Route Mirror (CRD)

Add a `mirror` block to any route rule in a `ProxyRoute`:

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
            value: /api/v1
      backendRefs:
        - name: api-backend
          weight: 1
      mirror:
        backendRef:
          name: api-shadow-backend
        percentage: 50  # Mirror 50% of requests
```

### Per-Filter Mirror

You can also use the `RequestMirror` filter type in route filters:

```yaml
filters:
  - type: RequestMirror
    mirrorBackend:
      name: shadow-backend
    mirrorPercent: 100
```

### Standalone Mode

In standalone YAML configuration:

```yaml
routes:
  - name: api-route
    match:
      hostnames:
        - api.example.com
      path:
        type: PathPrefix
        value: /api
    backends:
      - name: api-backend
    mirror:
      backend: shadow-backend
      percentage: 25
```

## Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `backendRef.name` | string | required | Name of the ProxyBackend to mirror to |
| `backendRef.namespace` | string | route namespace | Namespace of the mirror backend |
| `percentage` | int (0-100) | 100 | Percentage of requests to mirror |

## Behavior

- **Asynchronous**: Mirror requests run in a separate goroutine and never block the original request
- **Fire-and-forget**: Mirror responses are discarded; errors are logged but do not affect the client
- **Body cloning**: Request bodies are buffered and sent to both primary and mirror backends
- **Context respect**: If the original request's context is cancelled before the mirror starts, the mirror is skipped
- **Timeout**: Mirror requests have a 10-second timeout to prevent goroutine leaks

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `novaedge_mirror_requests_total` | Counter | Total mirrored requests sent |
| `novaedge_mirror_errors_total` | Counter | Total mirror request errors |
| `novaedge_mirror_latency_seconds` | Histogram | Mirror request latency |

## Use Cases

- **Shadow testing**: Route production traffic to a new service version to verify behavior
- **Performance benchmarking**: Compare response times between current and new backends
- **Debug logging**: Send traffic copies to a debug service that logs request details
- **Data validation**: Verify a new backend produces equivalent responses

## Best Practices

1. Start with a low mirror percentage (e.g., 10%) and increase gradually
2. Ensure the mirror backend can handle the additional load
3. Monitor mirror error rates to detect backend issues
4. Use the `X-Mirror: true` header in mirror backends to skip side effects (e.g., database writes)
