# Response Caching

NovaEdge provides built-in HTTP response caching with LRU eviction, Cache-Control header support, conditional requests, and an admin purge API.

## Overview

When caching is enabled, NovaEdge:

1. Checks incoming GET/HEAD requests against the in-memory cache
2. On **cache hit**: returns the cached response with `X-Cache: HIT`
3. On **cache miss**: forwards to the backend, caches the response (if eligible), and returns it with `X-Cache: MISS`

## Configuration

### Gateway-Level (CRD)

Enable caching on a `ProxyGateway`:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: web-gateway
spec:
  vipRef: web-vip
  cache:
    enabled: true
    maxSize: "256Mi"      # Maximum cache memory
    defaultTTL: "5m"      # Default TTL when no Cache-Control
    maxTTL: "1h"          # Maximum allowed TTL
    maxEntrySize: "1Mi"   # Maximum single response size
  listeners:
    - name: http
      port: 80
      protocol: HTTP
```

### Standalone Mode

```yaml
global:
  cache:
    enabled: true
    maxSize: "256Mi"
    defaultTTL: "5m"
    maxTTL: "1h"
    maxEntrySize: "1Mi"
```

## Cache Key

The cache key is computed from:

```
method + "|" + host + "|" + path + query + vary_headers
```

For example: `GET|example.com|/api/v1/users?page=1`

Vary header values are included to support content negotiation.

## Cache-Control Support

NovaEdge respects standard HTTP caching headers:

### Request Headers

| Header | Behavior |
|--------|----------|
| `Cache-Control: no-cache` | Bypass cache, forward to backend |
| `Cache-Control: no-store` | Bypass cache, forward to backend |

### Response Headers

| Header | Behavior |
|--------|----------|
| `Cache-Control: no-store` | Response is not cached |
| `Cache-Control: no-cache` | Response is not cached |
| `Cache-Control: private` | Response is not cached (shared proxy) |
| `Cache-Control: public` | Response is cacheable |
| `Cache-Control: max-age=N` | TTL is N seconds |
| `Cache-Control: s-maxage=N` | TTL is N seconds (takes precedence over max-age) |
| `Expires: <date>` | TTL from Expires header (lowest priority) |

### TTL Priority

1. `s-maxage` (shared cache directive)
2. `max-age`
3. `Expires` header
4. Default TTL from configuration

## Conditional Requests

NovaEdge supports conditional request handling for bandwidth efficiency:

- **ETag / If-None-Match**: Returns `304 Not Modified` if the ETag matches
- **If-Modified-Since**: Returns `304 Not Modified` if the resource hasn't changed

If the backend doesn't provide an ETag, NovaEdge generates one from the response body hash.

## What Is NOT Cached

- Non-GET/HEAD requests (POST, PUT, DELETE, etc.)
- Responses with `Set-Cookie` headers
- Responses with `Cache-Control: no-store` or `private`
- Responses larger than `maxEntrySize`
- Error responses (4xx, 5xx)

## Cache Purge API

NovaEdge provides an admin endpoint for cache management:

### Purge by Pattern

```bash
# Purge all entries
curl -X DELETE "http://agent:8082/_novaedge/cache?pattern=*"

# Purge entries matching a prefix
curl -X DELETE "http://agent:8082/_novaedge/cache?pattern=GET|example.com|/api/*"
```

### View Cache Statistics

```bash
curl "http://agent:8082/_novaedge/cache"
```

Response:
```json
{
  "entries": 1234,
  "memoryUsed": 52428800,
  "maxMemory": 268435456,
  "hitCount": 50000,
  "missCount": 10000,
  "evictionCount": 500
}
```

### Using novactl

```bash
# Purge all cache entries
novactl cache purge --agent-addr localhost:8082

# Purge with pattern
novactl cache purge --pattern "/api/*" --agent-addr localhost:8082

# View cache stats
novactl cache stats --agent-addr localhost:8082
```

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `novaedge_cache_hit_total` | Counter | Total cache hits |
| `novaedge_cache_miss_total` | Counter | Total cache misses |
| `novaedge_cache_eviction_total` | Counter | Total LRU evictions |
| `novaedge_cache_size_bytes` | Gauge | Current cache memory usage |

## LRU Eviction

The cache uses Least Recently Used (LRU) eviction:

- When the cache exceeds `maxSize` memory, the least recently accessed entries are evicted
- When the number of entries exceeds the internal limit (10,000), oldest entries are evicted
- A background cleanup goroutine removes expired entries every minute

## Configuration Reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | false | Enable response caching |
| `maxSize` | string | 256Mi | Maximum total cache memory |
| `defaultTTL` | string | 5m | Default TTL when no Cache-Control headers |
| `maxTTL` | string | 1h | Maximum allowed TTL |
| `maxEntrySize` | string | 1Mi | Maximum size of a single cached response |
