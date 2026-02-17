# NovaEdge WASM Plugins

Built-in WebAssembly plugins for NovaEdge. Each plugin is a standalone TinyGo program that compiles to a `.wasm` module.

## Plugins

| Plugin | Issue | Phase | Description |
|--------|-------|-------|-------------|
| `geoip` | #413 | request | GeoIP header enrichment from IP-to-location mappings |
| `requestid` | #414 | both | Request ID generation and propagation |
| `bodyrewrite` | #415 | response | Response body string replacement rules |
| `customauth` | #416 | request | API key and HMAC signature authentication |
| `schemavalidation` | #417 | request | Request header/content-type/method validation |
| `piimask` | #418 | response | PII detection and redaction in response headers |
| `graphqlprotect` | #419 | request | GraphQL depth/alias/introspection protection |
| `abtesting` | #420 | both | A/B testing with deterministic experiment bucketing |
| `businessmetrics` | #421 | both | Business metric extraction for Prometheus |
| `canaryanalysis` | #422 | both | Canary traffic splitting and success tracking |
| `multitenant` | #423 | request | Multi-tenant routing and isolation |
| `cacheinvalidation` | #424 | both | Surrogate-key based cache purge handling |
| `protocoltransform` | #425 | both | SOAP/XML to REST/JSON transformation hints |

## Building

Requires [TinyGo](https://tinygo.org/getting-started/install/) 0.34+.

```bash
# Build all plugins
make all

# Build a single plugin
make geoip

# List available plugins
make list

# Check TinyGo installation
make check
```

Output `.wasm` files are written to `../../bin/plugins/`.

## Documentation

See [WASM Plugin System](../../docs/advanced/wasm-plugins.md) for full documentation including configuration reference and deployment guide.
