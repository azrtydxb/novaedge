# Middleware Pipelines

NovaEdge supports composable middleware pipelines that allow you to define an ordered chain of middleware for each route. Pipelines support both built-in middleware and WASM plugins in the same chain.

## Overview

A middleware pipeline is an ordered list of middleware entries that execute in priority order before the request reaches the backend. Each middleware can:

- **Modify** request/response headers
- **Short-circuit** the chain (e.g., return 401 for auth failures)
- **Communicate** with other middleware via pipeline state

## Pipeline Phases

Middleware can execute at different phases:

| Phase | Description |
|-------|-------------|
| `pre-route` | Before route matching |
| `post-route` | After route matching, before backend |
| `pre-backend` | Just before sending to backend |
| `post-backend` | After backend response |

## Configuration

### Per-Route Pipeline

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: api-route
spec:
  hostnames: ["api.example.com"]
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /api
      backendRefs:
        - name: api-backend
  pipeline:
    middleware:
      - type: builtin
        name: rate-limit
        priority: 10
        config:
          requests_per_second: "100"
      - type: builtin
        name: cors
        priority: 20
      - type: wasm
        name: custom-auth
        priority: 30
      - type: builtin
        name: jwt
        priority: 40
```

### Execution Order

Middleware executes in ascending priority order (lower number = earlier execution):

```
Request → rate-limit (10) → cors (20) → custom-auth (30) → jwt (40) → Backend
```

If any middleware short-circuits (e.g., rate limiter returns 429), subsequent middleware and the backend are not called.

## Middleware Types

### Built-in (`type: builtin`)

| Name | Description |
|------|-------------|
| `rate-limit` | Rate limiting per source IP or header |
| `cors` | CORS header management |
| `jwt` | JWT token validation |
| `ip-allow` | IP allowlist filtering |
| `ip-deny` | IP denylist filtering |
| `security-headers` | Security response headers |

### WASM (`type: wasm`)

Custom middleware implemented as WASM plugins. See [WASM Plugin Development Guide](../advanced/wasm-plugins.md).

## Pipeline State

Middleware in the same pipeline can communicate through pipeline state:

- A middleware can set values: `state.Set("user_id", "123")`
- Downstream middleware can read values: `state.Get("user_id")`

This is useful for passing authentication context from an auth middleware to downstream handlers.

## Error Handling

- If a middleware **panics**, the pipeline recovers and returns HTTP 500
- If a WASM plugin **errors**, the pipeline continues (fail-open by default)
- If a middleware **short-circuits**, no further middleware or backend is called

## Standalone Mode

In standalone mode (non-Kubernetes), configure pipelines in YAML:

```yaml
routes:
  - name: api
    match:
      hostnames: ["api.example.com"]
      path:
        type: PathPrefix
        value: /api
    backends:
      - name: api-backend
    pipeline:
      middleware:
        - type: builtin
          name: rate-limit
          priority: 10
          config:
            requests_per_second: "100"
        - type: wasm
          name: custom-auth
          priority: 20
```
