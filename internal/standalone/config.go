/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package standalone provides configuration loading for standalone (non-Kubernetes) mode.
package standalone

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	errVersionIsRequired            = errors.New("version is required")
	errAtLeastOneListenerIsRequired = errors.New("at least one listener is required")
	errCertificate                  = errors.New("certificate[")
	errListener                     = errors.New("listener[")
	errBackend                      = errors.New("backend[")
	errRoute                        = errors.New("route[")
)

// protocolTLS is the TLS protocol identifier used in listener and L4 configuration.
const protocolTLS = "TLS"

// Config represents the complete standalone configuration
type Config struct {
	// Version of the config format
	Version string `yaml:"version"`

	// Global settings
	Global GlobalConfig `yaml:"global"`

	// Certificates define managed TLS certificates
	Certificates []CertificateConfig `yaml:"certificates,omitempty"`

	// Listeners define ports and protocols
	Listeners []ListenerConfig `yaml:"listeners"`

	// Routes define traffic routing rules
	Routes []RouteConfig `yaml:"routes"`

	// Backends define upstream services
	Backends []BackendConfig `yaml:"backends"`

	// L4Listeners define Layer 4 TCP/UDP/TLS passthrough listeners (optional)
	L4Listeners []L4ListenerStandaloneConfig `yaml:"l4Listeners,omitempty"`

	// Policies define rate limiting, CORS, etc. (optional)
	Policies []PolicyConfig `yaml:"policies,omitempty"`

	// ErrorPages define custom error page templates
	ErrorPages *ErrorPagesConfig `yaml:"errorPages,omitempty"`

	// RedirectScheme defines HTTP to HTTPS redirect
	RedirectScheme *RedirectSchemeStandaloneConfig `yaml:"redirectScheme,omitempty"`

	// Management defines management plane configuration
	Management *ManagementConfig `yaml:"management,omitempty"`
}

// GlobalConfig contains global settings
type GlobalConfig struct {
	// LogLevel: debug, info, warn, error
	LogLevel string `yaml:"logLevel"`

	// MetricsPort for Prometheus metrics
	MetricsPort int `yaml:"metricsPort"`

	// HealthPort for health checks
	HealthPort int `yaml:"healthPort"`

	// AccessLog settings
	AccessLog AccessLogConfig `yaml:"accessLog"`

	// Tracing settings
	Tracing TracingConfig `yaml:"tracing"`

	// Compression settings for response compression
	Compression *CompressionConfig `yaml:"compression,omitempty"`

	// Cache configures response caching
	Cache *CacheConfigStandalone `yaml:"cache,omitempty"`
}

// CompressionConfig defines response compression for standalone mode
type CompressionConfig struct {
	Enabled      bool     `yaml:"enabled"`
	MinSize      string   `yaml:"minSize,omitempty"`      // e.g., "1024"
	Level        int      `yaml:"level,omitempty"`        // Compression level
	Algorithms   []string `yaml:"algorithms,omitempty"`   // ["gzip", "br"]
	ExcludeTypes []string `yaml:"excludeTypes,omitempty"` // ["image/*", "video/*"]
}

// AccessLogConfig defines access logging
type AccessLogConfig struct {
	Enabled           bool    `yaml:"enabled"`
	Format            string  `yaml:"format"`   // clf, json, custom
	Template          string  `yaml:"template"` // custom format template
	Path              string  `yaml:"path"`     // file path or "stdout"
	Output            string  `yaml:"output"`   // stdout, file, both
	MaxSize           string  `yaml:"maxSize"`  // e.g., "100Mi"
	MaxBackups        int     `yaml:"maxBackups"`
	FilterStatusCodes []int   `yaml:"filterStatusCodes"` // only log these codes
	SampleRate        float64 `yaml:"sampleRate"`        // 0.0-1.0
}

// ErrorPagesConfig defines custom error pages for standalone mode
type ErrorPagesConfig struct {
	Enabled     bool           `yaml:"enabled"`
	Pages       map[int]string `yaml:"pages"`       // status code -> HTML template
	DefaultPage string         `yaml:"defaultPage"` // fallback HTML template
}

// RedirectSchemeStandaloneConfig defines HTTP to HTTPS redirect for standalone mode
type RedirectSchemeStandaloneConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Scheme     string   `yaml:"scheme"`     // target scheme (default: "https")
	Port       int      `yaml:"port"`       // target port (default: 443)
	StatusCode int      `yaml:"statusCode"` // 301 or 302
	Exclusions []string `yaml:"exclusions"` // paths to skip
}

// TracingConfig defines request tracing
type TracingConfig struct {
	Enabled         bool   `yaml:"enabled"`
	SamplingRate    int    `yaml:"samplingRate"` // 0-100
	RequestIDHeader string `yaml:"requestIdHeader"`
}

// ListenerConfig defines a listener
type ListenerConfig struct {
	Name     string `yaml:"name"`
	Port     int    `yaml:"port"`
	Protocol string `yaml:"protocol"` // HTTP, HTTPS, TCP, TLS

	// HTTP3 enables HTTP/3 (QUIC) support
	HTTP3 *HTTP3ListenerConfig `yaml:"http3,omitempty"`

	// SSE configures Server-Sent Events support
	SSE *SSEListenerConfig `yaml:"sse,omitempty"`

	// TLS configuration (for HTTPS/TLS)
	TLS *TLSConfig `yaml:"tls,omitempty"`

	// Hostnames to accept (for HTTP/HTTPS)
	Hostnames []string `yaml:"hostnames,omitempty"`

	// MaxRequestBodySize in bytes (0 = unlimited)
	MaxRequestBodySize int64 `yaml:"maxRequestBodySize,omitempty"`

	// ClientAuth configures mTLS client certificate authentication
	ClientAuth *ClientAuthListenerConfig `yaml:"clientAuth,omitempty"`

	// OCSPStapling enables OCSP stapling for this listener
	OCSPStapling bool `yaml:"ocspStapling,omitempty"`

	// ProxyProtocol configures PROXY protocol parsing for this listener
	ProxyProtocol *ProxyProtocolListenerConfig `yaml:"proxyProtocol,omitempty"`
}

// ClientAuthListenerConfig defines mTLS client auth for standalone mode
type ClientAuthListenerConfig struct {
	// Mode: none, optional, require
	Mode string `yaml:"mode"`

	// CAFile is the path to the CA certificate file for client cert verification
	CAFile string `yaml:"caFile,omitempty"`

	// RequiredCNPatterns are regex patterns the client cert CN must match
	RequiredCNPatterns []string `yaml:"requiredCNPatterns,omitempty"`

	// RequiredSANs are SANs the client cert must contain
	RequiredSANs []string `yaml:"requiredSANs,omitempty"`
}

// ProxyProtocolListenerConfig defines PROXY protocol for standalone mode
type ProxyProtocolListenerConfig struct {
	// Enabled enables PROXY protocol parsing
	Enabled bool `yaml:"enabled"`

	// Version: 0 (both), 1 (v1 only), 2 (v2 only)
	Version int `yaml:"version,omitempty"`

	// TrustedCIDRs are source CIDRs from which PROXY headers are trusted
	TrustedCIDRs []string `yaml:"trustedCIDRs,omitempty"`
}

// TLSConfig defines TLS settings
type TLSConfig struct {
	// CertFile path to PEM-encoded certificate (for inline TLS config)
	CertFile string `yaml:"certFile,omitempty"`

	// KeyFile path to PEM-encoded private key (for inline TLS config)
	KeyFile string `yaml:"keyFile,omitempty"`

	// Certificate references a certificate by name (from certificates section)
	// This is an alternative to CertFile/KeyFile
	Certificate string `yaml:"certificate,omitempty"`

	// MinVersion: TLS1.2, TLS1.3 (default: TLS1.2)
	MinVersion string `yaml:"minVersion,omitempty"`

	// CipherSuites is an optional list of allowed cipher suites
	CipherSuites []string `yaml:"cipherSuites,omitempty"`
}

// RouteMirrorConfig configures traffic mirroring for a route
type RouteMirrorConfig struct {
	// Backend to mirror to
	Backend string `yaml:"backend"`
	// Percentage of requests to mirror (0-100)
	Percentage int `yaml:"percentage,omitempty"`
}

// CacheConfigStandalone configures response caching in standalone mode
type CacheConfigStandalone struct {
	// Enabled enables response caching
	Enabled bool `yaml:"enabled"`
	// MaxSize is the maximum cache memory (e.g., "256Mi")
	MaxSize string `yaml:"maxSize,omitempty"`
	// DefaultTTL is the default time-to-live (e.g., "5m")
	DefaultTTL string `yaml:"defaultTTL,omitempty"`
	// MaxTTL is the maximum TTL (e.g., "1h")
	MaxTTL string `yaml:"maxTTL,omitempty"`
	// MaxEntrySize is the maximum entry size (e.g., "1Mi")
	MaxEntrySize string `yaml:"maxEntrySize,omitempty"`
}

// RouteConfig defines a routing rule
type RouteConfig struct {
	Name string `yaml:"name"`

	// Match conditions
	Match RouteMatch `yaml:"match"`

	// Backend to route to
	Backends []RouteBackendRef `yaml:"backends"`

	// Filters to apply
	Filters []RouteFilter `yaml:"filters,omitempty"`

	// Mirror configures traffic mirroring for this route
	Mirror *RouteMirrorConfig `yaml:"mirror,omitempty"`

	// Timeout for requests
	Timeout string `yaml:"timeout,omitempty"`

	// Policies to apply (by name)
	Policies []string `yaml:"policies,omitempty"`

	// Limits defines per-route request size limits and timeouts
	Limits *RouteLimits `yaml:"limits,omitempty"`

	// Buffering defines request/response buffering settings
	Buffering *BufferingConfig `yaml:"buffering,omitempty"`

	// Retry configuration
	Retry *RetryPolicyConfig `yaml:"retry,omitempty"`

	// Pipeline defines a composable middleware pipeline
	Pipeline *PipelineConfig `yaml:"pipeline,omitempty"`

	// Expression is a boolean routing expression for advanced matching
	Expression string `yaml:"expression,omitempty"`
}

// RouteLimits defines per-route request limits for standalone mode
type RouteLimits struct {
	MaxRequestBodySize string `yaml:"maxRequestBodySize,omitempty"` // e.g., "10Mi"
	RequestTimeout     string `yaml:"requestTimeout,omitempty"`     // e.g., "30s"
	IdleTimeout        string `yaml:"idleTimeout,omitempty"`        // e.g., "60s"
}

// BufferingConfig defines buffering settings for standalone mode
type BufferingConfig struct {
	Request  bool   `yaml:"request,omitempty"`
	Response bool   `yaml:"response,omitempty"`
	MaxSize  string `yaml:"maxSize,omitempty"` // e.g., "50Mi"
}

// RetryPolicyConfig defines retry behavior for a route
type RetryPolicyConfig struct {
	MaxRetries    int      `yaml:"maxRetries"`
	PerTryTimeout string   `yaml:"perTryTimeout,omitempty"`
	RetryOn       []string `yaml:"retryOn,omitempty"`
	RetryBudget   float64  `yaml:"retryBudget,omitempty"`
	BackoffBase   string   `yaml:"backoffBase,omitempty"`
	RetryMethods  []string `yaml:"retryMethods,omitempty"`
}

// PipelineConfig defines a middleware pipeline for standalone mode
type PipelineConfig struct {
	Middleware []MiddlewareRefConfig `yaml:"middleware,omitempty"`
}

// MiddlewareRefConfig references a middleware in the pipeline
type MiddlewareRefConfig struct {
	Type     string            `yaml:"type"` // builtin, wasm
	Name     string            `yaml:"name"`
	Priority int               `yaml:"priority,omitempty"`
	Config   map[string]string `yaml:"config,omitempty"`
}

// RouteMatch defines matching conditions
type RouteMatch struct {
	// Hostnames to match
	Hostnames []string `yaml:"hostnames,omitempty"`

	// Path matching
	Path *PathMatch `yaml:"path,omitempty"`

	// Header matching
	Headers []HeaderMatch `yaml:"headers,omitempty"`

	// Method matching
	Method string `yaml:"method,omitempty"`
}

// PathMatch defines path matching
type PathMatch struct {
	Type  string `yaml:"type"` // Exact, PathPrefix, RegularExpression
	Value string `yaml:"value"`
}

// HeaderMatch defines header matching
type HeaderMatch struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
	Type  string `yaml:"type,omitempty"` // Exact, RegularExpression
}

// RouteBackendRef references a backend with optional weight
type RouteBackendRef struct {
	Name   string `yaml:"name"`
	Weight int    `yaml:"weight,omitempty"`
}

// RouteFilter defines request/response modifications
type RouteFilter struct {
	Type        string            `yaml:"type"` // AddHeader, RemoveHeader, URLRewrite, RequestRedirect
	Add         map[string]string `yaml:"add,omitempty"`
	Remove      []string          `yaml:"remove,omitempty"`
	RewritePath string            `yaml:"rewritePath,omitempty"`
	RedirectURL string            `yaml:"redirectURL,omitempty"`
}

// BackendConfig defines an upstream service
type BackendConfig struct {
	Name string `yaml:"name"`

	// Endpoints (static list of backend addresses)
	Endpoints []EndpointConfig `yaml:"endpoints"`

	// Load balancing policy
	LBPolicy string `yaml:"lbPolicy"` // RoundRobin, LeastConnections, Random, P2C, RingHash, Maglev

	// Health check configuration
	HealthCheck *HealthCheckConfig `yaml:"healthCheck,omitempty"`

	// Circuit breaker configuration
	CircuitBreaker *CircuitBreakerConfig `yaml:"circuitBreaker,omitempty"`

	// Connection pool settings
	ConnectionPool *ConnectionPoolConfig `yaml:"connectionPool,omitempty"`

	// TLS settings for backend connections
	TLS *BackendTLSConfig `yaml:"tls,omitempty"`

	// Session affinity configuration
	SessionAffinity *SessionAffinityStandaloneConfig `yaml:"sessionAffinity,omitempty"`

	// UpstreamProxyProtocol enables sending PROXY protocol to this backend
	UpstreamProxyProtocol *UpstreamProxyProtocolBackendConfig `yaml:"upstreamProxyProtocol,omitempty"`
}

// UpstreamProxyProtocolBackendConfig defines upstream PROXY protocol for standalone mode
type UpstreamProxyProtocolBackendConfig struct {
	// Enabled enables sending PROXY protocol headers to backends
	Enabled bool `yaml:"enabled"`

	// Version: 1 or 2 (default: 1)
	Version int `yaml:"version,omitempty"`
}

// EndpointConfig defines a backend endpoint
type EndpointConfig struct {
	Address string `yaml:"address"` // host:port
	Weight  int    `yaml:"weight,omitempty"`
}

// HealthCheckConfig defines health checking
type HealthCheckConfig struct {
	Protocol string `yaml:"protocol"` // HTTP, TCP

	Path     string `yaml:"path,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`

	HealthyThreshold   int `yaml:"healthyThreshold"`
	UnhealthyThreshold int `yaml:"unhealthyThreshold"`
}

// CircuitBreakerConfig defines circuit breaker settings
type CircuitBreakerConfig struct {
	MaxConnections     int    `yaml:"maxConnections"`
	MaxPendingRequests int    `yaml:"maxPendingRequests"`
	MaxRequests        int    `yaml:"maxRequests"`
	MaxRetries         int    `yaml:"maxRetries"`
	ConsecutiveErrors  int    `yaml:"consecutiveErrors"`
	Interval           string `yaml:"interval"`
	BaseEjectionTime   string `yaml:"baseEjectionTime"`
	MaxEjectionPercent int    `yaml:"maxEjectionPercent"`
}

// ConnectionPoolConfig defines connection pool settings
type ConnectionPoolConfig struct {
	MaxConnections        int    `yaml:"maxConnections"`
	MaxIdleConnections    int    `yaml:"maxIdleConnections"`
	IdleTimeout           string `yaml:"idleTimeout"`
	MaxConnectionLifetime string `yaml:"maxConnectionLifetime"`
}

// BackendTLSConfig defines TLS for backend connections
type BackendTLSConfig struct {
	// Enabled enables TLS for backend connections
	Enabled bool `yaml:"enabled"`

	// Mode: verify (default), skip-verify, mtls
	Mode string `yaml:"mode,omitempty"`

	// CAFile path to CA certificate for verification
	CAFile string `yaml:"caFile,omitempty"`

	// CertFile path to client certificate (for mTLS)
	CertFile string `yaml:"certFile,omitempty"`

	// KeyFile path to client private key (for mTLS)
	KeyFile string `yaml:"keyFile,omitempty"`

	// ServerName for SNI
	ServerName string `yaml:"serverName,omitempty"`

	// SelfSigned auto-generates a self-signed client certificate
	SelfSigned bool `yaml:"selfSigned,omitempty"`

	// InsecureSkipVerify skips certificate verification (deprecated: use mode: skip-verify)
	InsecureSkipVerify bool `yaml:"insecureSkipVerify,omitempty"`
}

// SessionAffinityStandaloneConfig defines session affinity in standalone mode
type SessionAffinityStandaloneConfig struct {
	// Type: Cookie, Header, SourceIP
	Type string `yaml:"type"`

	// CookieName for cookie-based affinity
	CookieName string `yaml:"cookieName,omitempty"`

	// CookieTTL as a duration string (e.g. "30m")
	CookieTTL string `yaml:"cookieTTL,omitempty"`

	// CookiePath for the affinity cookie
	CookiePath string `yaml:"cookiePath,omitempty"`

	// Secure flag on the cookie
	Secure bool `yaml:"secure,omitempty"`

	// SameSite attribute: Strict, Lax, None
	SameSite string `yaml:"sameSite,omitempty"`
}

// PolicyConfig defines a policy (rate limit, CORS, etc.)
type PolicyConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"` // RateLimit, CORS, IPFilter, JWT, WASMPlugin, BasicAuth, ForwardAuth, OIDC

	// Rate limiting
	RateLimit *RateLimitPolicy `yaml:"rateLimit,omitempty"`

	// CORS
	CORS *CORSPolicy `yaml:"cors,omitempty"`

	// IP filtering
	IPFilter *IPFilterPolicy `yaml:"ipFilter,omitempty"`

	// JWT validation
	JWT *JWTPolicy `yaml:"jwt,omitempty"`

	// Distributed rate limiting via Redis
	DistributedRateLimit *DistributedRateLimitPolicy `yaml:"distributedRateLimit,omitempty"`

	// WAF (Web Application Firewall)
	WAF *WAFPolicy `yaml:"waf,omitempty"`

	// WASM plugin
	WASMPlugin *WASMPluginPolicy `yaml:"wasmPlugin,omitempty"`

	// Basic Auth
	BasicAuth *BasicAuthPolicy `yaml:"basicAuth,omitempty"`

	// Forward Auth
	ForwardAuth *ForwardAuthPolicy `yaml:"forwardAuth,omitempty"`

	// OIDC
	OIDC *OIDCPolicy `yaml:"oidc,omitempty"`
}

// DistributedRateLimitPolicy defines distributed rate limiting
type DistributedRateLimitPolicy struct {
	RequestsPerSecond int                   `yaml:"requestsPerSecond"`
	BurstSize         int                   `yaml:"burstSize"`
	Algorithm         string                `yaml:"algorithm,omitempty"` // fixed-window, sliding-window, token-bucket
	Key               string                `yaml:"key,omitempty"`
	Redis             RedisConnectionConfig `yaml:"redis"`
}

// RedisConnectionConfig defines Redis connection settings
type RedisConnectionConfig struct {
	Address     string `yaml:"address"`
	Password    string `yaml:"password,omitempty"` //nolint:gosec // G117: struct field name for Redis config, not a hardcoded credential
	TLS         bool   `yaml:"tls,omitempty"`
	Database    int    `yaml:"database,omitempty"`
	ClusterMode bool   `yaml:"clusterMode,omitempty"`
}

// WAFPolicy defines WAF configuration
type WAFPolicy struct {
	Enabled                bool     `yaml:"enabled"`
	Mode                   string   `yaml:"mode,omitempty"` // detection, prevention
	ParanoiaLevel          int      `yaml:"paranoiaLevel,omitempty"`
	AnomalyThreshold       int      `yaml:"anomalyThreshold,omitempty"`
	RulesFile              string   `yaml:"rulesFile,omitempty"`
	RuleExclusions         []string `yaml:"ruleExclusions,omitempty"`
	CustomRules            []string `yaml:"customRules,omitempty"`
	MaxBodySize            int64    `yaml:"maxBodySize,omitempty"`
	ResponseBodyInspection bool     `yaml:"responseBodyInspection,omitempty"`
	MaxResponseBodySize    int64    `yaml:"maxResponseBodySize,omitempty"`
}

// WASMPluginPolicy defines WASM plugin configuration for standalone mode
type WASMPluginPolicy struct {
	// Source is a file path to the WASM binary
	Source string `yaml:"source"`
	// Config is key-value configuration for the plugin
	Config map[string]string `yaml:"config,omitempty"`
	// Phase: request, response, both
	Phase string `yaml:"phase,omitempty"`
	// Priority determines execution order (lower = earlier)
	Priority int `yaml:"priority,omitempty"`
}

// RateLimitPolicy defines rate limiting
type RateLimitPolicy struct {
	RequestsPerSecond int    `yaml:"requestsPerSecond"`
	BurstSize         int    `yaml:"burstSize"`
	Key               string `yaml:"key,omitempty"` // client_ip, header:X-User-ID, etc.
}

// CORSPolicy defines CORS settings
type CORSPolicy struct {
	AllowOrigins     []string `yaml:"allowOrigins"`
	AllowMethods     []string `yaml:"allowMethods"`
	AllowHeaders     []string `yaml:"allowHeaders"`
	ExposeHeaders    []string `yaml:"exposeHeaders,omitempty"`
	MaxAge           int      `yaml:"maxAge,omitempty"`
	AllowCredentials bool     `yaml:"allowCredentials,omitempty"`
}

// IPFilterPolicy defines IP filtering
type IPFilterPolicy struct {
	AllowList []string `yaml:"allowList,omitempty"`
	DenyList  []string `yaml:"denyList,omitempty"`
}

// JWTPolicy defines JWT validation
type JWTPolicy struct {
	Issuer            string   `yaml:"issuer"`
	Audience          []string `yaml:"audience,omitempty"`
	JWKSURI           string   `yaml:"jwksUri,omitempty"`
	SecretKey         string   `yaml:"secretKey,omitempty"`
	AllowedAlgorithms []string `yaml:"allowedAlgorithms,omitempty"`
}

// L4ListenerStandaloneConfig defines a Layer 4 listener in standalone mode
type L4ListenerStandaloneConfig struct {
	// Name identifies this listener
	Name string `yaml:"name"`
	// Port to listen on
	Port int `yaml:"port"`
	// Protocol: TCP, UDP, or TLS (passthrough)
	Protocol string `yaml:"protocol"`
	// Backend name for TCP/UDP listeners
	Backend string `yaml:"backend,omitempty"`
	// TCPConfig holds TCP-specific settings
	TCP *L4TCPStandaloneConfig `yaml:"tcp,omitempty"`
	// UDPConfig holds UDP-specific settings
	UDP *L4UDPStandaloneConfig `yaml:"udp,omitempty"`
	// TLSRoutes maps SNI hostnames to backends for TLS passthrough
	TLSRoutes []L4TLSRouteStandaloneConfig `yaml:"tlsRoutes,omitempty"`
	// DefaultTLSBackend is the fallback for unmatched SNI
	DefaultTLSBackend string `yaml:"defaultTlsBackend,omitempty"`
}

// L4TCPStandaloneConfig holds TCP-specific configuration
type L4TCPStandaloneConfig struct {
	ConnectTimeout string `yaml:"connectTimeout,omitempty"`
	IdleTimeout    string `yaml:"idleTimeout,omitempty"`
	BufferSize     int    `yaml:"bufferSize,omitempty"`
	DrainTimeout   string `yaml:"drainTimeout,omitempty"`
}

// L4UDPStandaloneConfig holds UDP-specific configuration
type L4UDPStandaloneConfig struct {
	SessionTimeout string `yaml:"sessionTimeout,omitempty"`
	BufferSize     int    `yaml:"bufferSize,omitempty"`
}

// L4TLSRouteStandaloneConfig maps an SNI hostname to a backend for TLS passthrough
type L4TLSRouteStandaloneConfig struct {
	Hostname string `yaml:"hostname"`
	Backend  string `yaml:"backend"`
}

// BasicAuthPolicy defines HTTP Basic Authentication for standalone mode
type BasicAuthPolicy struct {
	// Realm for WWW-Authenticate header
	Realm string `yaml:"realm,omitempty"`

	// HtpasswdFile path to htpasswd file
	HtpasswdFile string `yaml:"htpasswdFile,omitempty"`

	// Users inline username:hash map (alternative to htpasswdFile)
	Users map[string]string `yaml:"users,omitempty"`

	// StripAuth removes Authorization header before forwarding
	StripAuth bool `yaml:"stripAuth,omitempty"`
}

// ForwardAuthPolicy defines external auth delegation for standalone mode
type ForwardAuthPolicy struct {
	// Address of the external auth service
	Address string `yaml:"address"`

	// AuthHeaders to forward to auth service
	AuthHeaders []string `yaml:"authHeaders,omitempty"`

	// ResponseHeaders to copy from auth response
	ResponseHeaders []string `yaml:"responseHeaders,omitempty"`

	// Timeout for auth subrequest (e.g., "5s")
	Timeout string `yaml:"timeout,omitempty"`

	// CacheTTL for caching auth decisions (e.g., "5m")
	CacheTTL string `yaml:"cacheTTL,omitempty"`
}

// OIDCPolicy defines OAuth2/OIDC authentication for standalone mode
type OIDCPolicy struct {
	// Provider type: generic, keycloak
	Provider string `yaml:"provider,omitempty"`

	// IssuerURL for OIDC discovery
	IssuerURL string `yaml:"issuerURL,omitempty"`

	// ClientID for OAuth2
	ClientID string `yaml:"clientID"`

	// ClientSecret for OAuth2
	ClientSecret string `yaml:"clientSecret"` //nolint:gosec // G117: struct field name for OAuth2 config, not a hardcoded credential

	// RedirectURL for OAuth2 callback
	RedirectURL string `yaml:"redirectURL"`

	// Scopes to request
	Scopes []string `yaml:"scopes,omitempty"`

	// SessionSecret for encrypting session cookies (base64, 32 bytes)
	SessionSecret string `yaml:"sessionSecret"` //nolint:gosec // G117: struct field name for session config, not a hardcoded credential

	// ForwardHeaders to set from user info claims
	ForwardHeaders []string `yaml:"forwardHeaders,omitempty"`

	// Keycloak-specific config
	Keycloak *KeycloakPolicy `yaml:"keycloak,omitempty"`

	// Authorization config
	Authorization *AuthorizationPolicy `yaml:"authorization,omitempty"`
}

// KeycloakPolicy defines Keycloak-specific settings for standalone mode
type KeycloakPolicy struct {
	ServerURL  string `yaml:"serverURL"`
	Realm      string `yaml:"realm"`
	RoleClaim  string `yaml:"roleClaim,omitempty"`
	GroupClaim string `yaml:"groupClaim,omitempty"`
}

// AuthorizationPolicy defines role-based access control for standalone mode
type AuthorizationPolicy struct {
	RequiredRoles  []string `yaml:"requiredRoles,omitempty"`
	RequiredGroups []string `yaml:"requiredGroups,omitempty"`
	Mode           string   `yaml:"mode,omitempty"` // any, all
}

// CertificateConfig defines a managed TLS certificate
type CertificateConfig struct {
	// Name is the unique identifier for this certificate
	Name string `yaml:"name"`

	// Domains to include in the certificate (first is primary/CN)
	Domains []string `yaml:"domains"`

	// Issuer configuration
	Issuer CertificateIssuerConfig `yaml:"issuer"`

	// RenewBefore specifies how long before expiry to renew
	RenewBefore string `yaml:"renewBefore,omitempty"` // Default: 720h (30 days)
}

// CertificateIssuerConfig defines certificate issuer settings
type CertificateIssuerConfig struct {
	// Type: acme, manual, self-signed
	Type string `yaml:"type"`

	// ACME configuration (for type: acme)
	ACME *ACMEIssuerConfig `yaml:"acme,omitempty"`

	// Manual certificate files (for type: manual)
	Manual *ManualIssuerConfig `yaml:"manual,omitempty"`

	// Self-signed configuration (for type: self-signed)
	SelfSigned *SelfSignedIssuerConfig `yaml:"selfSigned,omitempty"`
}

// ACMEIssuerConfig defines ACME certificate provisioning
type ACMEIssuerConfig struct {
	// Email for ACME registration
	Email string `yaml:"email"`

	// Server is the ACME server URL (default: Let's Encrypt production)
	Server string `yaml:"server,omitempty"`

	// ChallengeType: http-01, dns-01, tls-alpn-01 (default: http-01)
	ChallengeType string `yaml:"challengeType,omitempty"`

	// DNSProvider for dns-01 challenges (e.g., cloudflare, route53, googledns)
	DNSProvider string `yaml:"dnsProvider,omitempty"`

	// DNSCredentials for dns-01 challenges
	DNSCredentials map[string]string `yaml:"dnsCredentials,omitempty"`

	// DNS01 configures DNS-01 specific options
	DNS01 *DNS01ChallengeConfig `yaml:"dns01,omitempty"`

	// TLSALPN01 configures TLS-ALPN-01 specific options
	TLSALPN01 *TLSALPN01ChallengeConfig `yaml:"tlsAlpn01,omitempty"`

	// AcceptTOS indicates acceptance of ACME Terms of Service
	AcceptTOS bool `yaml:"acceptTOS,omitempty"`

	// KeyType: RSA2048, RSA4096, EC256, EC384 (default: EC256)
	KeyType string `yaml:"keyType,omitempty"`
}

// DNS01ChallengeConfig configures DNS-01 ACME challenges for standalone mode.
type DNS01ChallengeConfig struct {
	// Provider specifies the DNS provider (cloudflare, route53, googledns)
	Provider string `yaml:"provider"`

	// Credentials for the DNS provider
	Credentials map[string]string `yaml:"credentials"`

	// PropagationTimeout is the max time to wait for DNS propagation (default: 120s)
	PropagationTimeout string `yaml:"propagationTimeout,omitempty"`

	// PollingInterval is the time between DNS propagation checks (default: 5s)
	PollingInterval string `yaml:"pollingInterval,omitempty"`
}

// TLSALPN01ChallengeConfig configures TLS-ALPN-01 ACME challenges for standalone mode.
type TLSALPN01ChallengeConfig struct {
	// Port to listen on for TLS-ALPN-01 challenges (default: 443)
	Port int `yaml:"port,omitempty"`
}

// ManualIssuerConfig defines manual certificate files
type ManualIssuerConfig struct {
	// CertFile path to PEM-encoded certificate
	CertFile string `yaml:"certFile"`

	// KeyFile path to PEM-encoded private key
	KeyFile string `yaml:"keyFile"`

	// CAFile optional path to CA certificate for chain
	CAFile string `yaml:"caFile,omitempty"`
}

// SelfSignedIssuerConfig defines self-signed certificate generation
type SelfSignedIssuerConfig struct {
	// Validity duration for the certificate (default: 8760h = 1 year)
	Validity string `yaml:"validity,omitempty"`

	// Organization name in the certificate
	Organization string `yaml:"organization,omitempty"`

	// KeyType: RSA2048, RSA4096, EC256, EC384 (default: EC256)
	KeyType string `yaml:"keyType,omitempty"`
}

// ManagementConfig defines management plane configuration
type ManagementConfig struct {
	// Enabled enables management plane protection
	Enabled bool `yaml:"enabled"`

	// Listener configuration for management endpoint
	Listener ManagementListenerConfig `yaml:"listener"`

	// Security policies for management endpoints
	Security ManagementSecurityConfig `yaml:"security,omitempty"`

	// Routes for internal management services
	Routes []ManagementRouteConfig `yaml:"routes,omitempty"`
}

// ManagementListenerConfig defines the management listener
type ManagementListenerConfig struct {
	// Port for management HTTPS endpoint
	Port int `yaml:"port"`

	// Protocol: HTTP, HTTPS (default: HTTPS)
	Protocol string `yaml:"protocol,omitempty"`

	// Certificate name to use (from certificates section)
	Certificate string `yaml:"certificate,omitempty"`

	// Hostnames to accept
	Hostnames []string `yaml:"hostnames,omitempty"`
}

// ManagementSecurityConfig defines security for management endpoints
type ManagementSecurityConfig struct {
	// RateLimit configuration
	RateLimit *RateLimitPolicy `yaml:"rateLimit,omitempty"`

	// JWT authentication
	JWT *JWTPolicy `yaml:"jwt,omitempty"`

	// IP filtering
	IPFilter *IPFilterPolicy `yaml:"ipFilter,omitempty"`

	// BasicAuth configuration
	BasicAuth *BasicAuthConfig `yaml:"basicAuth,omitempty"`
}

// BasicAuthConfig defines basic authentication
type BasicAuthConfig struct {
	// Enabled enables basic authentication
	Enabled bool `yaml:"enabled"`

	// Users is a map of username to bcrypt password hash
	Users map[string]string `yaml:"users,omitempty"`

	// Realm for WWW-Authenticate header
	Realm string `yaml:"realm,omitempty"`
}

// ManagementRouteConfig defines a management route
type ManagementRouteConfig struct {
	// Path prefix to match
	Path string `yaml:"path"`

	// Backend address (e.g., localhost:9080)
	Backend string `yaml:"backend"`

	// StripPrefix removes the path prefix before forwarding
	StripPrefix bool `yaml:"stripPrefix,omitempty"`

	// Description of this route
	Description string `yaml:"description,omitempty"`
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// validateCertificates validates certificate configurations and returns a map of valid cert names.
func (c *Config) validateCertificates() (map[string]bool, error) {
	certNames := make(map[string]bool)
	for i, cert := range c.Certificates {
		if cert.Name == "" {
			return nil, fmt.Errorf("%w: %d]: name is required", errCertificate, i)
		}
		if certNames[cert.Name] {
			return nil, fmt.Errorf("%w: %d]: duplicate name %s", errCertificate, i, cert.Name)
		}
		certNames[cert.Name] = true

		if len(cert.Domains) == 0 {
			return nil, fmt.Errorf("%w: %d]: at least one domain is required", errCertificate, i)
		}
		if err := validateCertIssuer(cert.Issuer, i); err != nil {
			return nil, err
		}
	}
	return certNames, nil
}

// validateCertIssuer validates a certificate issuer configuration.
func validateCertIssuer(issuer CertificateIssuerConfig, idx int) error {
	if issuer.Type == "" {
		return fmt.Errorf("%w: %d]: issuer type is required", errCertificate, idx)
	}
	switch issuer.Type {
	case "acme":
		if issuer.ACME == nil {
			return fmt.Errorf("%w: %d]: ACME configuration required for type acme", errCertificate, idx)
		}
		if issuer.ACME.Email == "" {
			return fmt.Errorf("%w: %d]: ACME email is required", errCertificate, idx)
		}
	case "manual":
		if issuer.Manual == nil {
			return fmt.Errorf("%w: %d]: manual configuration required for type manual", errCertificate, idx)
		}
		if issuer.Manual.CertFile == "" || issuer.Manual.KeyFile == "" {
			return fmt.Errorf("%w: %d]: certFile and keyFile are required for manual issuer", errCertificate, idx)
		}
	case "self-signed":
		// Self-signed config is optional
	default:
		return fmt.Errorf("%w: %d]: invalid issuer type %s", errCertificate, idx, issuer.Type)
	}
	return nil
}

// validateListeners validates listener configurations against the known certificate names.
func (c *Config) validateListeners(certNames map[string]bool) error {
	for i, l := range c.Listeners {
		if l.Name == "" {
			return fmt.Errorf("%w: %d]: name is required", errListener, i)
		}
		if l.Port <= 0 || l.Port > 65535 {
			return fmt.Errorf("%w: %d]: invalid port %d", errListener, i, l.Port)
		}
		if l.Protocol == "" {
			return fmt.Errorf("%w: %d]: protocol is required", errListener, i)
		}
		if l.Protocol == "HTTPS" || l.Protocol == protocolTLS {
			if l.TLS == nil {
				return fmt.Errorf("%w: %d]: TLS configuration required for %s", errListener, i, l.Protocol)
			}
			if l.TLS.Certificate != "" {
				if !certNames[l.TLS.Certificate] {
					return fmt.Errorf("%w: %d]: unknown certificate %s", errListener, i, l.TLS.Certificate)
				}
			} else if l.TLS.CertFile == "" || l.TLS.KeyFile == "" {
				return fmt.Errorf("%w: %d]: either certificate reference or certFile/keyFile required", errListener, i)
			}
		}
	}
	return nil
}

// validateBackends validates backend configurations and returns a map of valid backend names.
func (c *Config) validateBackends() (map[string]bool, error) {
	backendNames := make(map[string]bool)
	for i, b := range c.Backends {
		if b.Name == "" {
			return nil, fmt.Errorf("%w: %d]: name is required", errBackend, i)
		}
		if backendNames[b.Name] {
			return nil, fmt.Errorf("%w: %d]: duplicate name %s", errBackend, i, b.Name)
		}
		backendNames[b.Name] = true

		if len(b.Endpoints) == 0 {
			return nil, fmt.Errorf("%w: %d]: at least one endpoint is required", errBackend, i)
		}
	}
	return backendNames, nil
}

// validateRoutes validates route configurations against the known backend names.
func (c *Config) validateRoutes(backendNames map[string]bool) error {
	for i, r := range c.Routes {
		if r.Name == "" {
			return fmt.Errorf("%w: %d]: name is required", errRoute, i)
		}
		if len(r.Backends) == 0 {
			return fmt.Errorf("%w: %d]: at least one backend is required", errRoute, i)
		}
		for j, br := range r.Backends {
			if !backendNames[br.Name] {
				return fmt.Errorf("%w: %d].backends[%d]: unknown backend %s", errRoute, i, j, br.Name)
			}
		}
	}
	return nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Version == "" {
		return errVersionIsRequired
	}
	if len(c.Listeners) == 0 {
		return errAtLeastOneListenerIsRequired
	}

	certNames, err := c.validateCertificates()
	if err != nil {
		return err
	}
	if err := c.validateListeners(certNames); err != nil {
		return err
	}
	backendNames, err := c.validateBackends()
	if err != nil {
		return err
	}
	return c.validateRoutes(backendNames)
}

// GetTimeout parses and returns the timeout duration
func (r *RouteConfig) GetTimeout() time.Duration {
	if r.Timeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(r.Timeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

// HTTP3ListenerConfig defines HTTP/3 QUIC configuration for standalone mode
type HTTP3ListenerConfig struct {
	Enabled        bool   `yaml:"enabled"`
	ZeroRTT        bool   `yaml:"zeroRTT,omitempty"`
	MaxIdleTimeout string `yaml:"maxIdleTimeout,omitempty"`
}

// SSEListenerConfig defines SSE configuration for standalone mode
type SSEListenerConfig struct {
	IdleTimeout       string `yaml:"idleTimeout,omitempty"`
	HeartbeatInterval string `yaml:"heartbeatInterval,omitempty"`
	MaxConnections    int    `yaml:"maxConnections,omitempty"`
}
