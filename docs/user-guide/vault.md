# HashiCorp Vault Integration

NovaEdge provides optional integration with [HashiCorp Vault](https://www.vaultproject.io/) for TLS certificate provisioning via the PKI secrets engine and secrets management via the KV engine.

## Overview

The Vault integration enables:

- **PKI Engine**: Request TLS certificates from Vault PKI, with automatic renewal
- **KV Engine**: Resolve policy credentials (OIDC secrets, API keys) from Vault at snapshot build time
- **Multiple Auth Methods**: Kubernetes, AppRole, and Token authentication

## Prerequisites

- HashiCorp Vault server (v1.12+)
- PKI secrets engine configured (for certificate management)
- KV secrets engine configured (for secrets management)
- Appropriate Vault policies for NovaEdge

## Enabling the Integration

The integration is controlled by the `--enable-vault` flag:

| Value | Behavior |
|-------|----------|
| `false` (default) | Disable Vault integration |
| `auto` | Attempt connection; disable if unreachable |
| `true` | Require Vault; fail startup if unreachable |

### Controller Flags

```bash
novaedge-controller \
  --enable-vault=true \
  --vault-addr=https://vault.example.com:8200 \
  --vault-auth-method=kubernetes \
  --vault-role=novaedge
```

## Authentication Methods

### Kubernetes Auth (Recommended)

Uses the pod's service account token to authenticate with Vault:

```yaml
# Controller deployment with Vault config
containers:
  - name: novaedge-controller
    args:
      - --enable-vault=true
      - --vault-addr=https://vault.internal:8200
      - --vault-auth-method=kubernetes
      - --vault-role=novaedge
```

Vault setup:

```bash
vault auth enable kubernetes
vault write auth/kubernetes/config \
  kubernetes_host="https://kubernetes.default.svc"
vault write auth/kubernetes/role/novaedge \
  bound_service_account_names=novaedge-controller \
  bound_service_account_namespaces=novaedge-system \
  policies=novaedge-policy \
  ttl=1h
```

### AppRole Auth

```bash
novaedge-controller \
  --enable-vault=true \
  --vault-addr=https://vault.example.com:8200 \
  --vault-auth-method=approle
```

Set `VAULT_APPROLE_ROLE_ID` and `VAULT_APPROLE_SECRET_ID` environment variables.

### Token Auth

For development environments:

```bash
VAULT_TOKEN=hvs.xxx novaedge-controller \
  --enable-vault=true \
  --vault-addr=https://vault.example.com:8200 \
  --vault-auth-method=token
```

## PKI Certificate Management

### ProxyGateway with Vault PKI

Reference Vault PKI directly in your gateway listener:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: api-gateway
spec:
  vipRef: "external-vip"
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      hostnames:
        - api.example.com
      tls:
        vaultCertRef:
          path: pki-int
          role: web-server
          ttl: 720h
          cacheSecretName: api-gateway-vault-tls
```

The controller will:
1. Request a certificate from Vault PKI at `pki-int/issue/web-server`
2. Store the certificate in K8s Secret `api-gateway-vault-tls`
3. Auto-renew before TTL expiry

### ProxyCertificate with Vault PKI

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyCertificate
metadata:
  name: api-cert
spec:
  domains:
    - api.example.com
  issuer:
    type: vault-pki
    vaultPKI:
      path: pki-int
      role: web-server
      ttl: 720h
  secretName: api-tls
```

### Vault PKI Setup

```bash
# Enable PKI engine
vault secrets enable -path=pki-int pki

# Configure CA
vault write pki-int/config/ca pem_bundle=@ca-bundle.pem

# Create role
vault write pki-int/roles/web-server \
  allowed_domains="example.com" \
  allow_subdomains=true \
  max_ttl=8760h
```

## KV Secrets for Policies

### Vault Secret References in Policies

Policy credentials can reference Vault KV paths:

```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: jwt-auth
spec:
  type: JWT
  targetRef:
    kind: ProxyRoute
    name: api-route
  jwt:
    issuer: "https://auth.example.com"
    jwksUri: "https://auth.example.com/.well-known/jwks.json"
    vaultSecretRef:
      path: "secret/data/novaedge/jwt"
      key: "signing_key"
      engine: "kv-v2"
      refreshInterval: "5m"
```

The controller resolves Vault secrets at snapshot build time and periodically refreshes them.

## Vault Policy

```hcl
# novaedge-policy.hcl
path "pki-int/issue/web-server" {
  capabilities = ["create", "update"]
}

path "pki-int/revoke" {
  capabilities = ["create", "update"]
}

path "secret/data/novaedge/*" {
  capabilities = ["read"]
}

path "sys/health" {
  capabilities = ["read"]
}
```

## Health Checks

The Vault health check is integrated into the controller's health endpoint. When Vault is enabled, `/healthz` will also check Vault connectivity.

## Automatic Renewal

- **Tokens**: Automatically re-authenticated before expiry
- **PKI Certificates**: Renewed when approaching TTL expiry (default: 24h before)
- **KV Secrets**: Refreshed at the configured interval (default: 5m)
