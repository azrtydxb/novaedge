# NovaEdge TLS/ACME & Management Plane Protection Implementation Plan

> **Status: COMPLETED** - This plan has been fully implemented across PRs #120-#124.
> All sprints below have been delivered. This document is retained as historical reference.

## Overview

This plan implements comprehensive TLS/ACME support and management plane protection for NovaEdge, enabling the system to protect itself using its own reverse proxy capabilities.

## Architecture Goals

1. **Self-Protecting Management Plane**: Web UI, Prometheus metrics, and API endpoints run behind NovaEdge's own ingress/reverse proxy
2. **Certificate Management**: Support for manual certs, ACME (Let's Encrypt), cert-manager, and self-signed certificates
3. **Backend TLS Options**: Manual certs, self-signed, or skip verification for upstream connections
4. **Unified Security**: Apply rate limiting, JWT auth, IP filtering to management endpoints

---

## Phase 1: ACME Client Library Integration

### 1.1 Add Dependencies
- Add `github.com/go-acme/lego/v4` for ACME protocol support
- Supports Let's Encrypt, ZeroSSL, and custom ACME servers
- HTTP-01, DNS-01, TLS-ALPN-01 challenge types

### 1.2 Create ACME Package (`internal/acme/`)

**Files to create:**
```
internal/acme/
├── client.go          # ACME client wrapper
├── provider.go        # Certificate provider interface
├── http_challenge.go  # HTTP-01 challenge handler
├── dns_challenge.go   # DNS-01 challenge provider abstraction
├── storage.go         # Certificate storage (file/secret)
├── renewal.go         # Auto-renewal logic
└── metrics.go         # Certificate metrics
```

**Key Types:**
```go
// ACMEConfig for configuration
type ACMEConfig struct {
    Email           string
    Server          string   // ACME server URL (default: Let's Encrypt)
    KeyType         string   // RSA2048, RSA4096, EC256, EC384
    Storage         StorageConfig
    RenewalDays     int      // Days before expiry to renew (default: 30)
    ChallengeType   string   // http-01, dns-01, tls-alpn-01
    DNSProvider     string   // For DNS-01: cloudflare, route53, etc.
    DNSCredentials  map[string]string
}

// CertificateProvider interface
type CertificateProvider interface {
    GetCertificate(domain string) (*tls.Certificate, error)
    RequestCertificate(domains []string) error
    RenewCertificate(domain string) error
    GetExpiryTime(domain string) time.Time
}
```

---

## Phase 2: CRD Extensions for Certificate Management

### 2.1 New CRD: ProxyCertificate (`api/v1alpha1/proxycertificate_types.go`)

```go
type ProxyCertificate struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   ProxyCertificateSpec   `json:"spec,omitempty"`
    Status ProxyCertificateStatus `json:"status,omitempty"`
}

type ProxyCertificateSpec struct {
    // Domains to include in certificate
    Domains []string `json:"domains"`

    // Issuer configuration
    Issuer CertificateIssuer `json:"issuer"`

    // Secret name to store certificate (auto-generated if empty)
    SecretName string `json:"secretName,omitempty"`

    // Duration before expiry to renew
    RenewBefore metav1.Duration `json:"renewBefore,omitempty"`
}

type CertificateIssuer struct {
    // Type: acme, manual, self-signed, cert-manager
    Type string `json:"type"`

    // ACME configuration
    ACME *ACMEIssuerConfig `json:"acme,omitempty"`

    // Manual certificate reference
    Manual *ManualIssuerConfig `json:"manual,omitempty"`

    // Cert-manager issuer reference
    CertManager *CertManagerIssuerConfig `json:"certManager,omitempty"`
}

type ACMEIssuerConfig struct {
    // ACME server URL
    Server string `json:"server,omitempty"` // Default: Let's Encrypt

    // Email for registration
    Email string `json:"email"`

    // Challenge type: http-01, dns-01, tls-alpn-01
    ChallengeType string `json:"challengeType"`

    // DNS provider for dns-01
    DNSProvider string `json:"dnsProvider,omitempty"`

    // Secret containing DNS provider credentials
    DNSCredentialsRef *corev1.SecretReference `json:"dnsCredentialsRef,omitempty"`

    // Secret containing ACME account key (auto-created if empty)
    AccountKeyRef *corev1.SecretReference `json:"accountKeyRef,omitempty"`
}

type ManualIssuerConfig struct {
    // Secret containing tls.crt and tls.key
    SecretRef corev1.SecretReference `json:"secretRef"`
}

type CertManagerIssuerConfig struct {
    // Reference to cert-manager Issuer or ClusterIssuer
    IssuerRef ObjectReference `json:"issuerRef"`
}

type ProxyCertificateStatus struct {
    // Current state: Pending, Ready, Renewing, Failed
    State string `json:"state"`

    // Certificate details
    NotBefore *metav1.Time `json:"notBefore,omitempty"`
    NotAfter  *metav1.Time `json:"notAfter,omitempty"`

    // Renewal status
    LastRenewalTime *metav1.Time `json:"lastRenewalTime,omitempty"`
    NextRenewalTime *metav1.Time `json:"nextRenewalTime,omitempty"`

    // Secret name where cert is stored
    SecretName string `json:"secretName,omitempty"`

    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

### 2.2 Update ProxyGateway TLS Config

```go
type TLSConfig struct {
    // Existing fields
    SecretRef    *corev1.SecretReference `json:"secretRef,omitempty"`
    MinVersion   string                  `json:"minVersion,omitempty"`
    CipherSuites []string                `json:"cipherSuites,omitempty"`

    // NEW: Certificate reference (alternative to SecretRef)
    CertificateRef *ObjectReference `json:"certificateRef,omitempty"`

    // NEW: ACME inline config (creates ProxyCertificate automatically)
    ACME *InlineACMEConfig `json:"acme,omitempty"`
}

type InlineACMEConfig struct {
    Email         string `json:"email"`
    ChallengeType string `json:"challengeType,omitempty"` // Default: http-01
    DNSProvider   string `json:"dnsProvider,omitempty"`
}
```

### 2.3 Update ProxyBackend TLS Config

```go
type BackendTLSConfig struct {
    // Enable TLS to backend
    Enabled bool `json:"enabled,omitempty"`

    // TLS mode: verify, skip-verify, mtls
    Mode string `json:"mode,omitempty"` // Default: verify

    // CA certificate for verification
    CASecretRef *corev1.SecretReference `json:"caSecretRef,omitempty"`

    // Client certificate for mTLS
    ClientCertRef *corev1.SecretReference `json:"clientCertRef,omitempty"`

    // Use self-signed certificate (auto-generated)
    SelfSigned bool `json:"selfSigned,omitempty"`

    // Server name for SNI
    ServerName string `json:"serverName,omitempty"`
}
```

---

## Phase 3: Certificate Controller

### 3.1 Create Certificate Controller (`internal/controller/certificate_controller.go`)

**Responsibilities:**
- Watch ProxyCertificate resources
- Provision certificates via ACME or other issuers
- Store certificates in Kubernetes Secrets
- Monitor expiry and trigger renewals
- Handle HTTP-01 challenges via agent coordination

**Key Functions:**
```go
func (r *CertificateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch ProxyCertificate
    // 2. Check if certificate exists and is valid
    // 3. If missing/expiring, request new certificate
    // 4. Store in Secret
    // 5. Update status
    // 6. Schedule next reconcile based on expiry
}

func (r *CertificateReconciler) provisionACME(ctx context.Context, cert *v1alpha1.ProxyCertificate) error {
    // 1. Get/create ACME account
    // 2. Request certificate
    // 3. Complete challenge
    // 4. Store certificate
}
```

### 3.2 HTTP-01 Challenge Handler

The controller needs to coordinate with agents to serve HTTP-01 challenges:

```go
// Challenge route automatically added to agents
// Path: /.well-known/acme-challenge/{token}
// Response: {keyAuth}
```

**Implementation Options:**
1. **Controller-side**: Controller handles challenges directly (requires separate port 80 listener)
2. **Agent-side**: Push challenge tokens to agents via ConfigSnapshot (preferred)

---

## Phase 4: Standalone Mode ACME Support

### 4.1 Update Standalone Config Schema

```yaml
version: "1.0"

# NEW: Certificate management
certificates:
  - name: api-cert
    domains:
      - api.example.com
      - "*.api.example.com"
    issuer:
      type: acme
      acme:
        email: admin@example.com
        server: https://acme-v02.api.letsencrypt.org/directory
        challengeType: http-01
        # For DNS-01
        # dnsProvider: cloudflare
        # dnsCredentials:
        #   CF_API_EMAIL: xxx
        #   CF_API_KEY: xxx
    renewBefore: 720h  # 30 days

  - name: web-cert
    domains:
      - www.example.com
    issuer:
      type: manual
      manual:
        certFile: /etc/novaedge/certs/web.crt
        keyFile: /etc/novaedge/certs/web.key

  - name: internal-cert
    domains:
      - internal.local
    issuer:
      type: self-signed
      selfSigned:
        validity: 8760h  # 1 year

# Listener references certificate by name
listeners:
  - name: https
    port: 443
    protocol: HTTPS
    certificate: api-cert  # Reference to certificate above
    hostnames:
      - api.example.com

# Backend TLS configuration
backends:
  - name: upstream-api
    endpoints:
      - address: backend:8443
    tls:
      enabled: true
      mode: verify  # verify, skip-verify, mtls
      caFile: /etc/novaedge/ca.crt
      # For mTLS
      # clientCertFile: /etc/novaedge/client.crt
      # clientKeyFile: /etc/novaedge/client.key
```

### 4.2 Standalone ACME Manager

```go
// internal/standalone/acme_manager.go
type ACMEManager struct {
    config     *StandaloneConfig
    client     *acme.Client
    storage    *FileStorage
    certCache  map[string]*tls.Certificate
    renewTimer *time.Timer
    logger     *zap.Logger
}

func (m *ACMEManager) Start(ctx context.Context) error {
    // 1. Load existing certificates
    // 2. Check for expiring certs
    // 3. Request/renew as needed
    // 4. Start renewal timer
}

func (m *ACMEManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
    // SNI-based certificate selection
}
```

---

## Phase 5: Management Plane Protection

### 5.1 Architecture

```
                    ┌─────────────────────────────────────────┐
                    │           NovaEdge Agent                │
                    │  ┌─────────────────────────────────┐   │
   Internet ───────>│  │      Listener (443/HTTPS)       │   │
                    │  │  - TLS termination              │   │
                    │  │  - Rate limiting                │   │
                    │  │  - JWT auth                     │   │
                    │  │  - IP filtering                 │   │
                    │  └──────────────┬──────────────────┘   │
                    │                 │                       │
                    │    ┌────────────┴────────────┐         │
                    │    │         Routes          │         │
                    │    └────────────┬────────────┘         │
                    │                 │                       │
                    │  ┌──────────────┼──────────────┐       │
                    │  ▼              ▼              ▼       │
                    │ ┌────┐      ┌────────┐    ┌────────┐   │
                    │ │Web │      │Metrics │    │  API   │   │
                    │ │ UI │      │ /prom  │    │ /api   │   │
                    │ └────┘      └────────┘    └────────┘   │
                    │   │              │            │         │
                    └───┼──────────────┼────────────┼─────────┘
                        │              │            │
                        ▼              ▼            ▼
                    ┌─────────────────────────────────────────┐
                    │     Management Plane (localhost)        │
                    │  Web UI :9080  Prometheus :9090         │
                    └─────────────────────────────────────────┘
```

### 5.2 Management Plane Gateway Configuration

**Kubernetes Mode** - Create internal ProxyGateway:

```yaml
apiVersion: proxy.novaedge.io/v1alpha1
kind: ProxyGateway
metadata:
  name: novaedge-management
  namespace: novaedge-system
spec:
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        acme:
          email: admin@example.com
          challengeType: http-01
      hostnames:
        - "management.novaedge.io"

---
apiVersion: proxy.novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: management-webui
  namespace: novaedge-system
spec:
  parentRefs:
    - name: novaedge-management
  hostnames:
    - "management.novaedge.io"
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: novaedge-webui
          port: 9080
      policyRefs:
        - name: management-security

---
apiVersion: proxy.novaedge.io/v1alpha1
kind: ProxyRoute
metadata:
  name: management-metrics
  namespace: novaedge-system
spec:
  parentRefs:
    - name: novaedge-management
  hostnames:
    - "management.novaedge.io"
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /prometheus
      backendRefs:
        - name: prometheus
          port: 9090
      policyRefs:
        - name: management-security
      filters:
        - type: URLRewrite
          urlRewrite:
            path:
              type: ReplacePrefixMatch
              replacePrefixMatch: /

---
apiVersion: proxy.novaedge.io/v1alpha1
kind: ProxyPolicy
metadata:
  name: management-security
  namespace: novaedge-system
spec:
  rateLimit:
    requestsPerSecond: 100
    burst: 200
    key: client_ip
  jwt:
    issuer: https://auth.example.com
    audience: ["novaedge-management"]
    jwksUri: https://auth.example.com/.well-known/jwks.json
  ipFilter:
    allowList:
      - 10.0.0.0/8
      - 192.168.0.0/16
```

**Standalone Mode** - Config section:

```yaml
# Management plane protection
management:
  enabled: true
  # Listen externally, proxy to internal services
  listener:
    port: 443
    protocol: HTTPS
    certificate: management-cert

  # Security policies for management endpoints
  security:
    rateLimit:
      requestsPerSecond: 100
      burstSize: 200
    jwt:
      enabled: true
      issuer: https://auth.example.com
      jwksUri: https://auth.example.com/.well-known/jwks.json
    ipFilter:
      allowList:
        - 10.0.0.0/8
        - 192.168.0.0/16

  # Internal services to protect
  routes:
    - path: /
      backend: localhost:9080  # Web UI
    - path: /prometheus
      backend: localhost:9090  # Prometheus
      stripPrefix: true
    - path: /api/v1/metrics
      backend: localhost:9090
```

### 5.3 Update Web UI Server

**Add TLS Support:**

```go
// cmd/novactl/cmd/web.go - New flags
var (
    tlsCert     string
    tlsKey      string
    tlsAuto     bool   // Auto-generate self-signed
    acmeEmail   string
    acmeServer  string
    behindProxy bool   // Run behind NovaEdge proxy
)

func init() {
    webCmd.Flags().StringVar(&tlsCert, "tls-cert", "", "TLS certificate file")
    webCmd.Flags().StringVar(&tlsKey, "tls-key", "", "TLS key file")
    webCmd.Flags().BoolVar(&tlsAuto, "tls-auto", false, "Auto-generate self-signed cert")
    webCmd.Flags().StringVar(&acmeEmail, "acme-email", "", "ACME email for Let's Encrypt")
    webCmd.Flags().StringVar(&acmeServer, "acme-server", "", "ACME server URL")
    webCmd.Flags().BoolVar(&behindProxy, "behind-proxy", false, "Run behind NovaEdge proxy")
}
```

**Server TLS Modes:**

```go
// cmd/novactl/pkg/webui/server.go
func (s *Server) Start(ctx context.Context) error {
    if s.behindProxy {
        // Listen only on localhost, proxy handles TLS
        return s.httpServer.ListenAndServe()
    }

    if s.tlsCert != "" && s.tlsKey != "" {
        // Manual certificate
        return s.httpServer.ListenAndServeTLS(s.tlsCert, s.tlsKey)
    }

    if s.acmeEmail != "" {
        // ACME/Let's Encrypt
        return s.startWithACME(ctx)
    }

    if s.tlsAuto {
        // Self-signed certificate
        return s.startWithSelfSigned(ctx)
    }

    // Plain HTTP (development only)
    return s.httpServer.ListenAndServe()
}
```

---

## Phase 6: Missing Reverse Proxy Features

### 6.1 Response Header Modification

```go
// internal/agent/router/filter.go
type ResponseHeaderFilter struct {
    Add    map[string]string
    Set    map[string]string
    Remove []string
}

func (f *ResponseHeaderFilter) ModifyResponse(resp *http.Response) error {
    for k, v := range f.Set {
        resp.Header.Set(k, v)
    }
    for k, v := range f.Add {
        resp.Header.Add(k, v)
    }
    for _, k := range f.Remove {
        resp.Header.Del(k)
    }
    return nil
}
```

### 6.2 Security Headers Filter (HSTS, etc.)

```go
// internal/agent/policy/security_headers.go
type SecurityHeadersPolicy struct {
    // HSTS
    HSTS *HSTSConfig `json:"hsts,omitempty"`

    // Content Security Policy
    CSP string `json:"csp,omitempty"`

    // X-Frame-Options
    XFrameOptions string `json:"xFrameOptions,omitempty"`

    // X-Content-Type-Options
    XContentTypeOptions bool `json:"xContentTypeOptions,omitempty"`

    // X-XSS-Protection
    XXSSProtection string `json:"xxssProtection,omitempty"`

    // Referrer-Policy
    ReferrerPolicy string `json:"referrerPolicy,omitempty"`
}

type HSTSConfig struct {
    MaxAge            int  `json:"maxAge"`            // Seconds
    IncludeSubdomains bool `json:"includeSubdomains"`
    Preload           bool `json:"preload"`
}
```

### 6.3 Request Body Size Limit

```go
// Already exists in Listener, but add enforcement
type RequestLimits struct {
    MaxBodySize     int64         `json:"maxBodySize"`     // Bytes
    MaxHeaderSize   int           `json:"maxHeaderSize"`   // Bytes
    ReadTimeout     time.Duration `json:"readTimeout"`
    WriteTimeout    time.Duration `json:"writeTimeout"`
    IdleTimeout     time.Duration `json:"idleTimeout"`
}
```

### 6.4 Compression Support

```go
// internal/agent/router/compression.go
type CompressionConfig struct {
    Enabled    bool     `json:"enabled"`
    MinSize    int      `json:"minSize"`    // Minimum size to compress
    Level      int      `json:"level"`      // Compression level
    Types      []string `json:"types"`      // MIME types to compress
    Algorithms []string `json:"algorithms"` // gzip, br, deflate
}
```

### 6.5 Buffering Configuration

```go
type BufferingConfig struct {
    // Request buffering
    RequestBuffering  bool  `json:"requestBuffering"`
    RequestBufferSize int64 `json:"requestBufferSize"`

    // Response buffering
    ResponseBuffering  bool  `json:"responseBuffering"`
    ResponseBufferSize int64 `json:"responseBufferSize"`
}
```

---

## Phase 7: Certificate Hot-Reload

### 7.1 Certificate Watcher

```go
// internal/agent/server/cert_watcher.go
type CertificateWatcher struct {
    certPath    string
    keyPath     string
    certificate atomic.Value // *tls.Certificate
    lastModTime time.Time
    ticker      *time.Ticker
    logger      *zap.Logger
}

func (w *CertificateWatcher) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
    cert := w.certificate.Load().(*tls.Certificate)
    return cert, nil
}

func (w *CertificateWatcher) Start(ctx context.Context) {
    w.ticker = time.NewTicker(30 * time.Second)
    for {
        select {
        case <-ctx.Done():
            return
        case <-w.ticker.C:
            w.checkAndReload()
        }
    }
}
```

### 7.2 Dynamic TLS Config

```go
// GetCertificate callback for dynamic certificate selection
tlsConfig := &tls.Config{
    GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
        return certManager.GetCertificate(hello.ServerName)
    },
    MinVersion: tls.VersionTLS12,
}
```

---

## Phase 8: Cert-Manager Integration

### 8.1 Compatibility Layer

For users who prefer cert-manager, support referencing cert-manager Certificates:

```yaml
apiVersion: proxy.novaedge.io/v1alpha1
kind: ProxyGateway
spec:
  listeners:
    - name: https
      port: 443
      protocol: HTTPS
      tls:
        # Reference cert-manager Certificate's Secret
        secretRef:
          name: my-cert-manager-secret
          namespace: default
```

### 8.2 Automatic Secret Detection

The controller can detect cert-manager managed Secrets by annotations:
```go
if secret.Annotations["cert-manager.io/certificate-name"] != "" {
    // This is a cert-manager managed certificate
    // Don't try to manage it ourselves
}
```

---

## Phase 9: Metrics & Observability

### 9.1 Certificate Metrics

```go
// internal/acme/metrics.go
var (
    certificateExpirySeconds = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "novaedge_certificate_expiry_seconds",
            Help: "Seconds until certificate expires",
        },
        []string{"domain", "issuer"},
    )

    certificateRenewalsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "novaedge_certificate_renewals_total",
            Help: "Total certificate renewals",
        },
        []string{"domain", "issuer", "status"},
    )

    acmeChallengesTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "novaedge_acme_challenges_total",
            Help: "Total ACME challenges",
        },
        []string{"type", "status"},
    )
)
```

### 9.2 Dashboard Updates

Add to web UI dashboard:
- Certificate expiry status
- Renewal history
- ACME challenge status
- TLS handshake metrics

---

## Implementation Order

### Sprint 1: Foundation (Week 1-2) - COMPLETED
1. ~~Add lego/v4 dependency~~
2. ~~Create `internal/acme/` package with client wrapper~~
3. ~~Implement file-based certificate storage~~
4. ~~Add basic ACME provisioning~~

### Sprint 2: Standalone ACME (Week 3-4) - COMPLETED
1. ~~Update standalone config schema for certificates~~
2. ~~Implement standalone ACME manager~~
3. ~~Add certificate hot-reload~~
4. ~~Test with Let's Encrypt staging~~

### Sprint 3: Kubernetes CRDs (Week 5-6) - COMPLETED
1. ~~Create ProxyCertificate CRD~~
2. ~~Update ProxyGateway TLS config~~
3. ~~Build certificate controller~~
4. ~~Implement HTTP-01 challenge coordination~~

### Sprint 4: Backend TLS (Week 7-8) - COMPLETED
1. ~~Implement backend TLS modes (verify, skip-verify, mtls)~~
2. ~~Add self-signed certificate generation~~
3. ~~Update connection pool for TLS options~~
4. ~~Test various backend scenarios~~

### Sprint 5: Management Plane (Week 9-10) - COMPLETED
1. ~~Add TLS flags to web UI server~~
2. ~~Implement behind-proxy mode~~
3. ~~Create management gateway configurations~~
4. ~~Add security policy examples~~

### Sprint 6: Missing Features (Week 11-12) - COMPLETED
1. ~~Response header modification~~
2. ~~Security headers policy (HSTS)~~
3. ~~Compression support~~
4. ~~Request/response buffering~~

### Sprint 7: Polish (Week 13-14) - COMPLETED
1. ~~Certificate metrics~~
2. ~~Dashboard certificate UI~~
3. ~~Documentation~~
4. ~~Integration tests~~

---

## Files to Create/Modify

### New Files
```
internal/acme/
├── client.go
├── provider.go
├── http_challenge.go
├── dns_challenge.go
├── storage.go
├── renewal.go
├── selfsigned.go
└── metrics.go

internal/controller/
└── certificate_controller.go

internal/agent/policy/
└── security_headers.go

internal/agent/router/
├── response_filter.go
└── compression.go

api/v1alpha1/
└── proxycertificate_types.go

config/crd/
└── proxycertificate.yaml
```

### Modified Files
```
go.mod                                    # Add lego dependency
api/v1alpha1/proxygateway_types.go       # Add certificate ref, inline ACME
api/v1alpha1/proxybackend_types.go       # Backend TLS modes
internal/standalone/config.go             # Certificate config section
internal/standalone/watcher.go            # ACME integration
internal/controller/snapshot/builder.go   # Certificate resolution
internal/agent/upstream/pool.go           # Backend TLS options
cmd/novactl/cmd/web.go                    # TLS flags
cmd/novactl/pkg/webui/server.go           # TLS server modes
```

---

## Testing Strategy

### Unit Tests
- ACME client mocking
- Certificate parsing
- Challenge handlers
- TLS config generation

### Integration Tests
- Let's Encrypt staging environment
- Self-signed certificate generation
- Backend TLS connections
- Certificate renewal

### E2E Tests
- Full ACME flow with Pebble (ACME test server)
- Management plane behind proxy
- Certificate hot-reload
- Multi-domain certificates

---

## Security Considerations

1. **ACME Account Keys**: Store securely in Secrets, never log
2. **Private Keys**: Memory-only handling where possible
3. **DNS Credentials**: Encrypted Secrets, minimal permissions
4. **Rate Limits**: Respect ACME provider rate limits
5. **Fallback**: Keep backup certificates for service continuity
6. **Audit**: Log certificate operations for compliance
