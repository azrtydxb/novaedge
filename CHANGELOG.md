# Changelog

All notable changes to NovaEdge are documented in this file.

## [1.2.0] - 2026-02-17

Major feature release: multi-cluster federation with cross-cluster routing and tunneling.

### Features

- **Multi-cluster federation** with three operating modes (#401):
  - **Hub-spoke**: Central hub pushes configuration to spoke clusters (one-way sync)
  - **Mesh**: Full bidirectional sync with endpoint merging across all members
  - **Unified**: Shared service namespace with locality-aware routing across clusters
- **Cross-cluster endpoint merging**: Controllers exchange ServiceEndpoints via federation sync; snapshot builder merges local and remote endpoints with cluster/region/zone labels (#401)
- **Cross-cluster data plane routing**: Reuse existing mTLS HTTP/2 CONNECT tunnel (port 15002) for forwarding requests to remote cluster endpoints transparently (#401)
- **Location-aware routing**: Three-tier locality hierarchy (Region > Zone > Cluster) with configurable MinHealthyPercent overflow thresholds (default 70%) (#401)
- **Network tunnels**: Optional WireGuard, SSH, and WebSocket tunnels for cross-cluster connectivity when direct pod-to-pod routing is unavailable (#401)
- **Federated SPIFFE identities**: Extended format `spiffe://FEDERATION_ID/cluster/CLUSTER_NAME/agent/NODE` for cross-cluster mTLS authentication (#401)
- **Anti-entropy reconciliation**: Merkle tree comparison with push/pull/bidirectional repair modes and drift reporting (#401)
- **Split-brain detection**: Fully implemented state machine wired into federation manager with configurable quorum, partition timeout, and auto-heal (#401)
- **Config hot reload**: Detect CRD spec changes via generation comparison; dynamically add/remove peers and update TLS credentials without restarts (#401)
- **Remote cluster cleanup**: Automatically delete federated resources (by `novaedge.io/federation-origin` label) and tear down tunnels when a NovaEdgeRemoteCluster is removed (#401)
- **Federation mode CRD field**: Added `spec.mode` field (hub-spoke/mesh/unified) with kubebuilder validation to NovaEdgeFederation CRD (#401)

### Documentation

- **Federation user guide**: Comprehensive guide covering all three modes, CRD configuration, sync behavior, anti-entropy, and split-brain handling (`docs/user-guide/federation.md`) (#401)
- **Cross-cluster routing guide**: Architecture diagrams, endpoint merging flow, locality routing tiers, and configuration examples (`docs/user-guide/cross-cluster-routing.md`) (#401)
- **Federation tunnels guide**: Setup instructions for WireGuard, SSH, and WebSocket tunnels with security considerations (`docs/user-guide/federation-tunnels.md`) (#401)
- **Federation design document**: Architecture decision record for the federation implementation (`docs/plans/`) (#401)
- **Example CRDs**: 5 sample configurations for hub-spoke, mesh, unified, direct remote cluster, and WireGuard tunnel remote cluster (`config/samples/federation/`) (#401)

### Fixes

- **Fail-closed middleware**: IP filter and OIDC middleware now fail closed on configuration errors instead of silently allowing traffic (#387)
- **CORS wildcard credentials**: Prevent setting `Access-Control-Allow-Origin: *` when credentials are enabled, as browsers reject this combination (#388)
- **IP filter data race**: Fix concurrent read/write race on `trustedProxyCIDRs` slice in IP filter middleware (#389)
- **Bounded io.ReadAll**: Add size limits to all `io.ReadAll` calls in agent hot paths to prevent memory exhaustion from large request/response bodies (#390)
- **Lock-free router**: Replace Router RWMutex with `atomic.Pointer` for lock-free request handling, eliminating contention under high concurrency (#391)
- **Rate limiter memory bounds**: Add bounded map size to rate limiter to prevent unbounded memory growth from unique client IPs (#392)
- **Context-aware retries**: Replace `time.Sleep` with context-aware `select` in retry loops to respect cancellation and deadlines (#393)
- **Context propagation**: Replace `context.Background()` with caller-provided context in VIP, health check, router, and server paths (#394)
- **Health checker TLS**: Health checker now verifies TLS certificates by default instead of skipping verification (#395)
- **TODO no-ops replaced**: Replace silent TODO no-ops with explicit not-implemented log warnings so missing functionality is visible (#396)

### Improvements

- **Router decomposition**: Decompose large router functions into focused helpers for better readability and testability (#399)
- **Test coverage**: Add unit tests for SSRF protection, WAF cache, and WAF rate limiting (#400)
- **Code quality cleanup**: Minor cleanup across agent packages — unused variables, consistent error wrapping, simplified control flow (#397)

### Helm

- Updated Helm chart values with `federation.mode`, `crossCluster`, and `tunnel` configuration sections (#401)
- Synced all CRD manifests between `config/crd/` and `charts/novaedge/crds/` (#401)

## [1.0.3] - 2026-02-16

Web Admin GUI architecture overhaul and monitoring release.

### Features

- **Standalone Web UI container**: Split React frontend into a standalone nginx container (`novaedge-webui`), decoupling it from the novactl binary. The webui pod now runs a two-container sidecar pattern: nginx serves the SPA and proxies `/api/` requests to the novactl API backend (#356)
- **Comprehensive Web Admin GUI expansion**: 19 fully functional pages covering all NovaEdge resources — dashboard, gateways, routes, backends, VIPs, certificates, IP pools, policies, mesh, federation, agents, config management, metrics, logs, traces, WAF events, and clusters (#350)
- **Prometheus/Grafana monitoring**: 5 pre-built Grafana dashboards for traffic overview, backend health, VIP management, policy enforcement, and mesh observability; plus Prometheus scrape configs and ServiceMonitor resources (#348)

### Fixes

- **WebUI page fixes**: Fix broken agents page, dashboard metrics, mesh policies, logs viewer, and traces page (#352)
- **Static asset rebuild**: Rebuild frontend assets to match source code fixes (#354)
- **Lint and test fixes**: Fix 11 lint issues (goconst, gosec, unparam) and pre-existing SSRF-related test failures by making HTTP clients injectable (#356)

## [1.0.2] - 2026-02-16

Helm chart coverage and operator feature completeness release.

### Features

- **Comprehensive Helm chart coverage**: All 3 charts (novaedge, novaedge-agent, novaedge-operator) updated to expose every configurable feature — VIP modes, all 12 LB algorithms, connection pooling, circuit breaking, TLS/HTTP3, all policies, middleware, mesh, CP VIP, WASM, L4, federation, webhooks, PDB, HPA, ServiceMonitor (#344)

### Fixes

- **Operator CLI flags**: Add 9 missing CLI flags to the operator binary that the Helm chart referenced (`--log-level`, `--log-format`, `--webhook-port`, `--controller-image`, `--agent-image`, `--novactl-image`, `--leader-elect-lease-duration`, `--leader-elect-renew-deadline`, `--leader-elect-retry-period`) (#346)
- **Federation controller registration**: Wire up `NovaEdgeFederationReconciler` which existed in code but was never registered in `main.go` (#346)
- **Webhook registration**: Register `FederationValidator` and `FederationDefaulter` admission webhooks when webhook port is configured (#346)
- **Managed image overrides**: Add `--controller-image`/`--agent-image`/`--novactl-image` override support to `NovaEdgeClusterReconciler` (#346)

### Chores

- Remove tracked binaries and PLAN.md from repository, update .gitignore (#342)

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
