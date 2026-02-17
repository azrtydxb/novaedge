# Gateway API Conformance

NovaEdge implements the [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io/) specification (v1.4.0, standard channel). This document tracks the conformance status for each supported feature.

## Conformance Profiles

NovaEdge targets conformance for the following profiles:

| Profile | Status |
|---------|--------|
| GatewayHTTP | Supported |
| GatewayGRPC | Supported |
| GatewayTLS | Supported |

## Supported Resources

| Resource | Status |
|----------|--------|
| GatewayClass | Supported |
| Gateway | Supported |
| HTTPRoute | Supported |
| GRPCRoute | Supported |
| TLSRoute | Supported |
| ReferenceGrant | Planned |
| TCPRoute | Planned |
| UDPRoute | Planned |

## Core Features

These features are required for basic Gateway API conformance.

| Feature | Status | Notes |
|---------|--------|-------|
| Gateway | Supported | Full GatewayClass and Gateway lifecycle |
| HTTPRoute | Supported | Path, header, and host matching |

## Extended Features

These features provide additional functionality beyond the core specification.

### HTTPRoute Extended Features

| Feature | Status | Notes |
|---------|--------|-------|
| HTTPRouteHostRewrite | Supported | Rewrite the Host header on upstream requests |
| HTTPRoutePathRewrite | Supported | Rewrite request path before forwarding |
| HTTPRoutePathRedirect | Supported | Redirect clients to a different path |
| HTTPRouteSchemeRedirect | Supported | Redirect HTTP to HTTPS and vice versa |
| HTTPRoutePortRedirect | Supported | Redirect to a different port |
| HTTPRouteResponseHeaderModification | Supported | Add, set, or remove response headers |
| HTTPRouteRequestMirror | Supported | Mirror traffic to a secondary backend |
| HTTPRouteRequestMultipleMirrors | Supported | Mirror traffic to multiple backends |
| HTTPRouteQueryParamMatching | Supported | Match routes by query parameters |
| HTTPRouteMethodMatching | Supported | Match routes by HTTP method |
| HTTPRouteRequestTimeout | Supported | Per-request timeout |
| HTTPRouteBackendTimeout | Supported | Per-backend timeout |
| HTTPRouteBackendProtocolH2C | Supported | Cleartext HTTP/2 to backends |
| HTTPRouteBackendProtocolWebSocket | Supported | WebSocket upgrade support |
| HTTPRouteParentRefPort | Supported | Attach routes to specific listener ports |
| HTTPRouteBackendRequestHeaderModification | Planned | Modify headers per-backend |
| HTTPRouteRequestPercentageMirror | Planned | Mirror a percentage of traffic |
| HTTPRouteCORS | Planned | CORS header management |
| HTTPRouteDestinationPortMatching | Planned | Match by destination port |

### Gateway Extended Features

| Feature | Status | Notes |
|---------|--------|-------|
| GatewayPort8080 | Supported | Listen on non-standard ports |
| GatewayHTTPListenerIsolation | Supported | Isolate listeners within a Gateway |
| GatewayStaticAddresses | Planned | Request specific addresses |
| GatewayInfrastructurePropagation | Planned | Propagate labels/annotations to infrastructure |

### Route Type Features

| Feature | Status | Notes |
|---------|--------|-------|
| GRPCRoute | Supported | gRPC routing with method matching |
| TLSRoute | Supported | TLS passthrough and termination routing |
| Mesh | Supported | Service mesh with transparent mTLS |

## Running Conformance Tests

To run the Gateway API conformance test suite against a NovaEdge deployment:

```bash
# Deploy NovaEdge to a Kubernetes cluster
make deploy

# Run the conformance tests
go test -v -tags conformance ./test/conformance/ -args -gateway-class=novaedge

# Run with debug logging
go test -v -tags conformance ./test/conformance/ -args -gateway-class=novaedge -debug

# Skip cleanup (useful for debugging failures)
go test -v -tags conformance ./test/conformance/ -args -gateway-class=novaedge -cleanup=false
```

## Conformance Report

After running the conformance tests, a report is generated that includes:

- The list of tested profiles (HTTP, gRPC, TLS)
- Core and extended feature results per profile
- Pass/fail status for each individual test

The report format follows the [Gateway API conformance report specification](https://gateway-api.sigs.k8s.io/concepts/conformance/#conformance-profiles).
