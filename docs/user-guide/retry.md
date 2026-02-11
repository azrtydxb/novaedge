# Request Retry Configuration

NovaEdge supports automatic request retry on backend failure, allowing failed requests to be
retried against alternate backend endpoints.

## Overview

When a backend request fails (5xx response, connection failure, timeout), NovaEdge can
automatically retry the request against a different healthy endpoint. This improves
reliability without requiring client-side retry logic.

## Configuration

### Kubernetes CRD (ProxyRoute)

Add retry configuration to individual route rules:

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
            value: /api
      backendRefs:
        - name: api-backend
      retry:
        maxRetries: 3
        perTryTimeout: 2s
        retryOn:
          - "5xx"
          - "connection-failure"
          - "reset"
        retryBudget: 0.2
        backoffBase: 25ms
        retryMethods:
          - GET
          - HEAD
          - OPTIONS
```

### Standalone Mode

```yaml
routes:
  - name: api-route
    match:
      hostnames: ["api.example.com"]
      path:
        type: PathPrefix
        value: /api
    backends:
      - name: api-backend
    retry:
      maxRetries: 3
      perTryTimeout: 2s
      retryOn: ["5xx", "connection-failure"]
      retryBudget: 0.2
      backoffBase: 25ms
      retryMethods: ["GET", "HEAD", "OPTIONS"]
```

## Configuration Reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `maxRetries` | int | 3 | Maximum number of retry attempts |
| `perTryTimeout` | duration | - | Timeout for each retry attempt |
| `retryOn` | string[] | `["5xx", "connection-failure"]` | Conditions that trigger retry |
| `retryBudget` | float | 0.2 | Max percentage of requests retried (0.0-1.0) |
| `backoffBase` | duration | 25ms | Base interval for exponential backoff |
| `retryMethods` | string[] | `["GET", "HEAD", "OPTIONS"]` | HTTP methods eligible for retry |

### Retry Conditions

- `5xx`: Retry on 5xx status codes (500-599)
- `connection-failure`: Retry on connection refused or timeout
- `reset`: Retry on connection reset
- `refused-stream`: Retry on HTTP/2 refused stream

## Behavior Details

### Endpoint Exclusion

When a retry occurs, the failed endpoint is excluded from the next attempt. NovaEdge
selects a different healthy endpoint for each retry, maximizing the chance of success.

### Retry Budget

The retry budget prevents retry storms. It limits the percentage of total requests that
can be retried per cluster within a sliding 10-second window. Default: 20%.

### Exponential Backoff

Retries use exponential backoff: `backoffBase * 2^attempt`. For example, with a 25ms base:
- Attempt 1: 25ms delay
- Attempt 2: 50ms delay
- Attempt 3: 100ms delay

### Idempotency

By default, only idempotent methods (GET, HEAD, OPTIONS) are retried. To retry non-idempotent
methods like POST, explicitly include them in `retryMethods`.

### Headers

The `X-Retry-Count` header is added to upstream requests to indicate the retry attempt number.

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `novaedge_retry_count_total` | Counter | Total retry attempts |
| `novaedge_retry_success_total` | Counter | Requests that succeeded after retry |
| `novaedge_retry_exhausted_total` | Counter | Requests where all retries were exhausted |
