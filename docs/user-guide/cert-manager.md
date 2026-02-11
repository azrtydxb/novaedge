# cert-manager Integration

NovaEdge provides optional integration with [cert-manager](https://cert-manager.io/) for automatic TLS certificate lifecycle management.

## Overview

When cert-manager is installed in your cluster, NovaEdge can automatically create `Certificate` resources from ProxyGateway annotations. This enables fully automated TLS certificate provisioning and renewal without manual certificate management.

## Prerequisites

- cert-manager v1.x installed in the cluster
- At least one `Issuer` or `ClusterIssuer` configured

## Enabling the Integration

The integration is controlled by the `--enable-cert-manager` flag on the NovaEdge controller:

| Value | Behavior |
|-------|----------|
| `auto` (default) | Auto-detect cert-manager CRDs; enable if found |
| `true` | Require cert-manager; fail startup if not found |
| `false` | Disable entirely |

```yaml
# In the controller deployment
containers:
  - name: novaedge-controller
    args:
      - --enable-cert-manager=auto
```

## Usage

### Annotation-Based Certificate Creation

Add cert-manager annotations to your ProxyGateway to trigger automatic Certificate creation:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: web-gateway
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
spec:
  vipRef: "external-vip"
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      hostnames:
        - "example.com"
        - "www.example.com"
      tls:
        secretRef:
          name: web-gateway-tls
```

NovaEdge will automatically create:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: web-gateway-tls
  ownerReferences:
    - apiVersion: novaedge.io/v1alpha1
      kind: ProxyGateway
      name: web-gateway
spec:
  dnsNames:
    - example.com
    - www.example.com
  secretName: web-gateway-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
    group: cert-manager.io
```

### Supported Annotations

| Annotation | Description |
|------------|-------------|
| `cert-manager.io/cluster-issuer` | Name of a ClusterIssuer to use |
| `cert-manager.io/issuer` | Name of a namespaced Issuer to use |

### ProxyCertificate with cert-manager

You can also reference cert-manager directly in ProxyCertificate:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyCertificate
metadata:
  name: api-cert
spec:
  domains:
    - api.example.com
  issuer:
    type: cert-manager
    certManager:
      issuerRef:
        name: letsencrypt-prod
        kind: ClusterIssuer
        group: cert-manager.io
  secretName: api-tls
```

## Certificate Lifecycle

1. **Creation**: When a ProxyGateway with cert-manager annotations is created/updated
2. **Monitoring**: NovaEdge watches Certificate status for Ready condition
3. **Rotation**: cert-manager handles automatic renewal; NovaEdge detects Secret updates and triggers config snapshot rebuild
4. **Cleanup**: When the ProxyGateway is deleted, the Certificate CR is garbage-collected via ownerReferences

## Troubleshooting

### Check Certificate Status

```bash
# Using kubectl
kubectl get certificates -l app.kubernetes.io/managed-by=novaedge

# Using novactl
novactl describe gateway web-gateway
```

### Common Issues

- **Certificate stuck in "Pending"**: Check cert-manager logs and Issuer status
- **Certificate not created**: Verify annotations are correct and controller has cert-manager detection enabled
- **TLS not working after certificate Ready**: Ensure the secretName matches the listener's TLS secretRef
