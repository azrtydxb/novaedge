# Changelog

All notable changes to NovaEdge are documented in this file.

## [1.0.1] - 2026-02-16

Security hardening and performance optimization release with 21 fixes across the data plane, control plane, and policy engine.

### Security Fixes

- **OIDC CSRF protection**: Bind OAuth state parameter to session cookie to prevent session fixation attacks; validate redirect URLs are same-origin to prevent open redirect phishing (#322)
- **JWT algorithm enforcement**: Require explicit `AllowedAlgorithms` configuration; explicitly reject `alg: none` tokens (#323)
- **SSRF protection**: Block outbound HTTP requests to private IP ranges in forward auth, JWKS, and OCSP responder paths (#324)
- **X-Forwarded-For bypass**: Use `RemoteAddr` for observability rate limiting instead of trusting XFF headers (#320)
- **PROXY protocol hardening**: Default to deny when no trusted CIDRs configured; validate protocol field and port ranges (#321)
- **WebSocket security**: Reject upgrades when no allowed origins configured (fail secure); add 10MB message size limit (#326)
- **L4 proxy limits**: Add TCP max connection limit (10,000) and UDP max session limit (10,000) to prevent resource exhaustion (#327)
- **WAF improvements**: Warn on responses exceeding inspection buffer; add yaml/csv/graphql MIME types; detect ambiguous Content-Length + Transfer-Encoding (#328)
- **gRPC size limits**: Enforce 16MB max message size on all gRPC connections (#329)
- **ReDoS prevention**: Validate user-supplied regex patterns with 500-character length limit (#325)
- **Minor hardening**: Constant-time basic auth validation, CRLF header sanitization, minimum OIDC session secret length, mesh authorization default-allow logging (#331)

### Performance Improvements

- **LB hot path**: Eliminate `RWMutex` contention in LeastConn, EWMA, and P2C `Select()` using atomic endpoint snapshots (#332)
- **RingHash**: Copy-on-write ring rebuild with atomic swap; reduce virtual nodes from 150 to 100 (#333)
- **Compression pooling**: Reuse gzip and brotli compressor writers via `sync.Pool` (#330)
- **Cache store**: Shard locks across 16 buckets; FNV hash-based cache keys (#334)
- **Config snapshots**: Skip unchanged snapshot sends using content-based version tracking (#336)
- **Health checks**: Bounded worker pool (10 goroutines) with HTTP connection reuse (#335)
- **Connection pool**: Add 60-second default connect timeout fallback (#337)
- **VIP manager**: Perform network operations outside lock; rate-limit GARPs to 10/sec; increase periodic interval to 60s (#338)
- **Metrics**: TTL-based endpoint cardinality cleanup; cached time-based sampling bucket (#339)
- **Controller reconciliation**: Generation-based event filtering across all 14 controllers (#340)

### Features

- **Nightly changelog**: Auto-generate categorized changelog in nightly release notes (#298)

## [1.0.0] - 2026-02-16

NovaEdge v1.0.0 is the first stable release. It delivers a unified Kubernetes-native load balancer, reverse proxy, VIP controller, and service mesh that replaces Envoy + MetalLB + NGINX Ingress in a single binary.

### Core Architecture

- **Controller** (control plane): Watches 10 CRDs, Ingress, and Gateway API resources; builds per-node config snapshots; pushes to agents via gRPC
- **Agent** (data plane): Runs as DaemonSet with hostNetwork; handles L4/L7 proxying, VIP management, policy enforcement, and service mesh
- **Operator**: Manages NovaEdge lifecycle via `NovaEdgeCluster` CRD
- **Standalone mode**: File-based configuration for non-Kubernetes deployments
- **novactl CLI**: Resource management, config generation, and embedded Web UI

### VIP Management

- L2 ARP mode with active node election and GARP announcements
- BGP mode with GoBGP for active-active ECMP load balancing
- OSPF mode for distributed VIP announcements via LSA
- BFD (Bidirectional Forwarding Detection) for sub-second failure detection (RFC 5880/5881)
- IPv6 dual-stack support for VIP addresses
- IP address pool allocation via ProxyIPPool CRD with IPAM
- Control-plane VIP mode for kube-apiserver HA (L2/BGP/BFD)
- BGP/BFD session self-healing after neighbor recovery
- Hot-reload of all VIP configuration without pod restarts

### Load Balancing (12 algorithms)

- Round Robin, Least Connections, P2C (Power of Two Choices)
- EWMA (latency-aware), Ring Hash, Maglev (consistent hashing)
- Cookie-based session affinity (sticky sessions)
- Locality/zone-aware routing
- Priority-based failover with configurable groups
- Panic mode (bypass health checks when too many endpoints unhealthy)
- Slow-start ramp-up for new/recovering backends
- Statistical outlier detection for upstream endpoints

### Protocol Support

- HTTP/1.1, HTTP/2 with ALPN negotiation
- HTTP/3 QUIC with 0-RTT connection resumption
- WebSocket upgrade forwarding
- Server-Sent Events (SSE) with heartbeat support
- gRPC routing and gRPC-Web bridge
- TCP/UDP L4 proxying with TLS passthrough
- PROXY protocol v1/v2 (frontend and upstream)
- Redis protocol-aware proxying

### Routing & Matching

- Path matching: Exact, PathPrefix, RegularExpression
- Header matching (exact and regex)
- Query parameter matching
- HTTP method matching
- Hostname/SNI matching (including wildcards)
- Boolean routing expressions for complex match logic
- Pre-compiled route expressions for high performance

### Request/Response Filters

- URL rewrite (path and host)
- Request redirect (301/302/307/308)
- Add/remove/set request headers
- Add/remove/set response headers
- Request mirroring with configurable percentage
- Traffic splitting with weighted backends (canary deployments)

### Security & Authentication

- TLS 1.3 minimum enforcement with AEAD-only cipher suites
- Frontend mTLS with client certificate validation and CN patterns
- Backend mTLS for upstream encryption
- SNI-based multi-certificate routing
- OCSP stapling for certificate revocation checking
- Basic authentication with htpasswd/bcrypt
- Forward authentication (subrequest to external auth service)
- OAuth2/OIDC with Keycloak integration
- JWT validation with JWKS, blacklisting, and revocation support
- ECDSA and EdDSA signing algorithm support
- IP allow/deny lists with CIDR support
- Security headers policy (HSTS, CSP, X-Frame-Options, etc.)
- X-Forwarded-For spoofing protection

### Web Application Firewall (WAF)

- Coraza WAF engine with OWASP CRS-style rules
- Prevention and detection modes with configurable paranoia levels
- Request body inspection with size limits
- Response body inspection
- Anomaly scoring with configurable threshold
- WAF metrics, caching, and audit logging
- Configurable fail-closed mode

### Certificate Management

- ACME client (Let's Encrypt) with HTTP-01, DNS-01, TLS-ALPN-01 challenges
- DNS-01 providers: Cloudflare, Route53, Google DNS
- cert-manager integration via annotations
- HashiCorp Vault PKI backend
- Self-signed certificate generation
- Automatic certificate renewal with configurable renew-before
- Certificate expiry metrics

### Middleware Pipeline

- Response caching with configurable TTL and max size
- Gzip and Brotli compression with min-size threshold
- Request/response buffering
- Request retry with configurable policies and retry budgets
- Request hedging for tail latency reduction
- Request timeouts and body size limits
- HTTP-to-HTTPS redirect
- Custom error pages
- Fault injection for chaos engineering
- Overload management for resource exhaustion prevention

### Service Mesh

- Transparent mTLS via TPROXY iptables interception
- SPIFFE workload identity (`spiffe://cluster.local/agent/<node>`)
- Embedded mesh certificate authority (ECDSA P-256, 24h lifetime)
- Automatic certificate request and renewal at 80% lifetime
- HTTP/2 mTLS tunnel between agents (ports 15001/15002)
- Mesh authorization policies (L7 allow/deny by namespace, path, SPIFFE ID)
- SPIFFE/SPIRE integration for external identity providers

### Ingress Controller

- Full Kubernetes Ingress API compatibility
- 40+ annotation support for feature configuration
- Automatic LB policy detection for BGP/OSPF VIPs (Maglev auto-select)
- Gateway API support (GatewayClass, Gateway, HTTPRoute, GRPCRoute, TCPRoute, TLSRoute)
- Gateway API conformance testing and status reporting
- Service LoadBalancer controller (kube-vip replacement)

### WASM Plugin System

- Wazero runtime for WebAssembly plugins
- Request and response processing phases
- Resource limits and execution timeouts
- Plugin metrics

### Multi-Cluster Federation

- Hub-spoke topology with federated control plane
- Cross-cluster service discovery
- Agent-assisted quorum for split-brain prevention
- Remote cluster registration via CRDs

### Observability

- 378+ Prometheus metrics (request, backend, VIP, BGP, BFD, circuit breaker, pool, health check, WAF, rate limit, cache, mirror, SSE, HTTP/3, TLS, WASM)
- Bounded status-class labels for low metrics cardinality
- OpenTelemetry distributed tracing with OTLP export
- Trace verbosity control (minimal vs detailed)
- Structured logging with zap
- JSON and CLF access log formats
- Admin/debug HTTP API on agent
- Runtime API for live operational changes
- External processing via gRPC (ExtProc)

### Operations

- Graceful connection draining on config reload
- Atomic config snapshot swaps
- Connection pooling with circuit breaking and outlier detection
- Distributed circuit breaker with resource limits
- Expression-based circuit breaker triggers
- Distributed rate limiting via Redis (sliding window)
- Per-route rate limiting with local token bucket
- InsecureSkipVerify audit metrics and logging

### Helm Charts

- `novaedge` (main chart), `novaedge-agent`, `novaedge-operator`
- Kubernetes NetworkPolicies for all charts
- CRD definitions for 10 resource types
- Kustomize overlays (dev, production)
- 51 sample configurations

### CI/CD

- golangci-lint with 16 strict linters
- CodeQL security analysis
- Unit tests, integration tests, E2E test suite (81 tests across 13 groups)
- Multi-arch release builds (linux/amd64, linux/arm64)
- Docker images published to GHCR
- Documentation freshness enforcement
- Nightly builds

### Documentation

- Full documentation site with MkDocs
- Architecture overview with Mermaid diagrams
- CRD reference for all 10 types
- CLI reference for novactl
- Helm values reference
- 8 use-case guides (edge LB, API gateway, multi-cluster, HA control plane, service mesh, gaming/IoT, regulatory compliance, bare-metal migration)
- Tool comparison page (vs Envoy/Istio, MetalLB, NGINX Ingress, HAProxy, Traefik)
- k3s control plane HA deployment guide
- 51 sample configurations
