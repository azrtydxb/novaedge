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
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

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

	// VIPs define virtual IP addresses (optional)
	VIPs []VIPConfig `yaml:"vips,omitempty"`

	// Policies define rate limiting, CORS, etc. (optional)
	Policies []PolicyConfig `yaml:"policies,omitempty"`

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
}

// AccessLogConfig defines access logging
type AccessLogConfig struct {
	Enabled bool   `yaml:"enabled"`
	Format  string `yaml:"format"` // json, common, combined
	Path    string `yaml:"path"`   // file path or "stdout"
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

	// TLS configuration (for HTTPS/TLS)
	TLS *TLSConfig `yaml:"tls,omitempty"`

	// Hostnames to accept (for HTTP/HTTPS)
	Hostnames []string `yaml:"hostnames,omitempty"`

	// MaxRequestBodySize in bytes (0 = unlimited)
	MaxRequestBodySize int64 `yaml:"maxRequestBodySize,omitempty"`
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

// RouteConfig defines a routing rule
type RouteConfig struct {
	Name string `yaml:"name"`

	// Match conditions
	Match RouteMatch `yaml:"match"`

	// Backend to route to
	Backends []RouteBackendRef `yaml:"backends"`

	// Filters to apply
	Filters []RouteFilter `yaml:"filters,omitempty"`

	// Timeout for requests
	Timeout string `yaml:"timeout,omitempty"`

	// Policies to apply (by name)
	Policies []string `yaml:"policies,omitempty"`
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

// VIPConfig defines a virtual IP address
type VIPConfig struct {
	Name    string `yaml:"name"`
	Address string `yaml:"address"` // IP/CIDR format
	Mode    string `yaml:"mode"`    // L2, BGP, OSPF

	// Interface to bind VIP (for L2 mode)
	Interface string `yaml:"interface,omitempty"`

	// BGP configuration
	BGP *BGPConfig `yaml:"bgp,omitempty"`

	// OSPF configuration
	OSPF *OSPFConfig `yaml:"ospf,omitempty"`
}

// BGPConfig defines BGP settings
type BGPConfig struct {
	LocalAS       uint32 `yaml:"localAS"`
	RouterID      string `yaml:"routerID"`
	PeerAS        uint32 `yaml:"peerAS"`
	PeerIP        string `yaml:"peerIP"`
	HoldTime      int    `yaml:"holdTime,omitempty"`
	KeepaliveTime int    `yaml:"keepaliveTime,omitempty"`
}

// OSPFConfig defines OSPF settings
type OSPFConfig struct {
	RouterID  string `yaml:"routerID"`
	Area      string `yaml:"area"`
	Interface string `yaml:"interface"`
}

// PolicyConfig defines a policy (rate limit, CORS, etc.)
type PolicyConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"` // RateLimit, CORS, IPFilter, JWT

	// Rate limiting
	RateLimit *RateLimitPolicy `yaml:"rateLimit,omitempty"`

	// CORS
	CORS *CORSPolicy `yaml:"cors,omitempty"`

	// IP filtering
	IPFilter *IPFilterPolicy `yaml:"ipFilter,omitempty"`

	// JWT validation
	JWT *JWTPolicy `yaml:"jwt,omitempty"`
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
	Issuer    string   `yaml:"issuer"`
	Audience  []string `yaml:"audience,omitempty"`
	JWKSURI   string   `yaml:"jwksUri,omitempty"`
	SecretKey string   `yaml:"secretKey,omitempty"`
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

	// DNSProvider for dns-01 challenges (e.g., cloudflare, route53)
	DNSProvider string `yaml:"dnsProvider,omitempty"`

	// DNSCredentials for dns-01 challenges
	DNSCredentials map[string]string `yaml:"dnsCredentials,omitempty"`

	// AcceptTOS indicates acceptance of ACME Terms of Service
	AcceptTOS bool `yaml:"acceptTOS,omitempty"`

	// KeyType: RSA2048, RSA4096, EC256, EC384 (default: EC256)
	KeyType string `yaml:"keyType,omitempty"`
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

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Version == "" {
		return fmt.Errorf("version is required")
	}

	if len(c.Listeners) == 0 {
		return fmt.Errorf("at least one listener is required")
	}

	// Build certificate name map for validation
	certNames := make(map[string]bool)
	for i, cert := range c.Certificates {
		if cert.Name == "" {
			return fmt.Errorf("certificate[%d]: name is required", i)
		}
		if certNames[cert.Name] {
			return fmt.Errorf("certificate[%d]: duplicate name %s", i, cert.Name)
		}
		certNames[cert.Name] = true

		if len(cert.Domains) == 0 {
			return fmt.Errorf("certificate[%d]: at least one domain is required", i)
		}
		if cert.Issuer.Type == "" {
			return fmt.Errorf("certificate[%d]: issuer type is required", i)
		}

		switch cert.Issuer.Type {
		case "acme":
			if cert.Issuer.ACME == nil {
				return fmt.Errorf("certificate[%d]: ACME configuration required for type acme", i)
			}
			if cert.Issuer.ACME.Email == "" {
				return fmt.Errorf("certificate[%d]: ACME email is required", i)
			}
		case "manual":
			if cert.Issuer.Manual == nil {
				return fmt.Errorf("certificate[%d]: manual configuration required for type manual", i)
			}
			if cert.Issuer.Manual.CertFile == "" || cert.Issuer.Manual.KeyFile == "" {
				return fmt.Errorf("certificate[%d]: certFile and keyFile are required for manual issuer", i)
			}
		case "self-signed":
			// Self-signed config is optional
		default:
			return fmt.Errorf("certificate[%d]: invalid issuer type %s", i, cert.Issuer.Type)
		}
	}

	// Validate listeners
	for i, l := range c.Listeners {
		if l.Name == "" {
			return fmt.Errorf("listener[%d]: name is required", i)
		}
		if l.Port <= 0 || l.Port > 65535 {
			return fmt.Errorf("listener[%d]: invalid port %d", i, l.Port)
		}
		if l.Protocol == "" {
			return fmt.Errorf("listener[%d]: protocol is required", i)
		}
		if l.Protocol == "HTTPS" || l.Protocol == "TLS" {
			if l.TLS == nil {
				return fmt.Errorf("listener[%d]: TLS configuration required for %s", i, l.Protocol)
			}
			// Validate TLS config - either cert/key files or certificate reference
			if l.TLS.Certificate != "" {
				if !certNames[l.TLS.Certificate] {
					return fmt.Errorf("listener[%d]: unknown certificate %s", i, l.TLS.Certificate)
				}
			} else if l.TLS.CertFile == "" || l.TLS.KeyFile == "" {
				return fmt.Errorf("listener[%d]: either certificate reference or certFile/keyFile required", i)
			}
		}
	}

	// Validate backends
	backendNames := make(map[string]bool)
	for i, b := range c.Backends {
		if b.Name == "" {
			return fmt.Errorf("backend[%d]: name is required", i)
		}
		if backendNames[b.Name] {
			return fmt.Errorf("backend[%d]: duplicate name %s", i, b.Name)
		}
		backendNames[b.Name] = true

		if len(b.Endpoints) == 0 {
			return fmt.Errorf("backend[%d]: at least one endpoint is required", i)
		}
	}

	// Validate routes
	for i, r := range c.Routes {
		if r.Name == "" {
			return fmt.Errorf("route[%d]: name is required", i)
		}
		if len(r.Backends) == 0 {
			return fmt.Errorf("route[%d]: at least one backend is required", i)
		}
		for j, br := range r.Backends {
			if !backendNames[br.Name] {
				return fmt.Errorf("route[%d].backends[%d]: unknown backend %s", i, j, br.Name)
			}
		}
	}

	return nil
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
