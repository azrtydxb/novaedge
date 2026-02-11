# Gateway API Conformance Tests

This directory contains the Gateway API conformance test suite for NovaEdge.

## Prerequisites

- A running Kubernetes cluster (kind, k3s, or any conformant cluster)
- NovaEdge deployed with the GatewayClass `novaedge` installed
- Gateway API CRDs installed (v1.4.0+)
- `KUBECONFIG` set or `~/.kube/config` available

## Install Gateway API CRDs

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml
```

## Install NovaEdge GatewayClass

```bash
kubectl apply -f config/samples/gatewayclass.yaml
```

## Running the Tests

### Full Conformance Suite

```bash
# From the repository root
make test-conformance
```

Or directly:

```bash
go test -v -tags conformance ./test/conformance/ -args \
  -gateway-class=novaedge \
  -debug=false \
  -cleanup=true
```

### Run a Specific Test

```bash
go test -v -tags conformance -run TestConformance/HTTPRouteHeaderMatching ./test/conformance/ -args \
  -gateway-class=novaedge
```

### Using novactl

```bash
novactl conformance --gateway-class novaedge
```

## Conformance Profile

NovaEdge targets the **GATEWAY-HTTP** conformance profile.

### Core Features (supported)

| Feature | Status |
|---------|--------|
| Gateway | Supported |
| HTTPRoute | Supported |
| HTTPRouteHostRewrite | Supported |
| HTTPRoutePathRewrite | Supported |
| HTTPRoutePathRedirect | Supported |
| HTTPRouteSchemeRedirect | Supported |
| HTTPRouteRequestHeaderModification | Supported |
| HTTPRouteResponseHeaderModification | Supported |
| HTTPRouteRequestMirror | Supported |

### Extended Features (supported)

| Feature | Status |
|---------|--------|
| GatewayPort8080 | Supported |
| GatewayHTTPListenerIsolation | Supported |

### Planned Features

| Feature | Status |
|---------|--------|
| TLSRoute | Planned |
| TCPRoute | Planned |
| UDPRoute | Planned |
| GRPCRoute | Planned |
| ReferenceGrant | Planned |
| GatewayStaticAddresses | Planned |

## Interpreting Results

The conformance test suite outputs results per conformance profile:

- **Core**: Tests for core features that all implementations must support
- **Extended**: Tests for optional features that implementations may support

A "Succeeded" result means all tests in that category passed.
A "Failed" result means one or more tests did not pass.

## Troubleshooting

### GatewayClass Not Accepted

Ensure the NovaEdge controller is running and the GatewayClass has been accepted:

```bash
kubectl get gatewayclass novaedge -o yaml
```

The `status.conditions` should contain an `Accepted: True` condition.

### Tests Timing Out

Increase the timeout configuration or check that NovaEdge agents are running:

```bash
kubectl get pods -n novaedge-system
```

### Debug Mode

Enable debug logging for more detailed output:

```bash
go test -v -tags conformance ./test/conformance/ -args \
  -gateway-class=novaedge \
  -debug=true
```
