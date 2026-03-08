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

// Package models provides unified configuration models for the web UI API.
package models

import (
	"time"
)

// Config represents the complete configuration
type Config struct {
	Gateways []Gateway `json:"gateways"`
	Routes   []Route   `json:"routes"`
	Backends []Backend `json:"backends"`
	Policies []Policy  `json:"policies"`
}

// Gateway represents a gateway/listener configuration
type Gateway struct {
	// Metadata
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`

	// Spec
	Listeners []Listener     `json:"listeners"`
	Tracing   *Tracing       `json:"tracing,omitempty"`
	AccessLog *AccessLog     `json:"accessLog,omitempty"`
	Status    *GatewayStatus `json:"status,omitempty"`

	// Resource version for optimistic locking (Kubernetes)
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

// GatewayStatus represents gateway status
type GatewayStatus struct {
	Ready      bool        `json:"ready"`
	Conditions []Condition `json:"conditions,omitempty"`
}

// Condition represents a status condition
type Condition struct {
	Type               string     `json:"type"`
	Status             string     `json:"status"`
	LastTransitionTime *time.Time `json:"lastTransitionTime,omitempty"`
	Reason             string     `json:"reason,omitempty"`
	Message            string     `json:"message,omitempty"`
}

// Listener represents a listener configuration
type Listener struct {
	Name               string   `json:"name"`
	Port               int      `json:"port"`
	Protocol           string   `json:"protocol"` // HTTP, HTTPS, TCP, TLS
	TLS                *TLS     `json:"tls,omitempty"`
	Hostnames          []string `json:"hostnames,omitempty"`
	MaxRequestBodySize int64    `json:"maxRequestBodySize,omitempty"`
}

// TLS represents TLS configuration
type TLS struct {
	Mode           string     `json:"mode,omitempty"` // Terminate, Passthrough
	CertificateRef *SecretRef `json:"certificateRef,omitempty"`
	CertFile       string     `json:"certFile,omitempty"`
	KeyFile        string     `json:"keyFile,omitempty"`
	MinVersion     string     `json:"minVersion,omitempty"`
	CipherSuites   []string   `json:"cipherSuites,omitempty"`
}

// SecretRef references a Kubernetes secret
type SecretRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// Tracing configuration
type Tracing struct {
	Enabled         bool   `json:"enabled"`
	SamplingRate    int    `json:"samplingRate,omitempty"`
	RequestIDHeader string `json:"requestIdHeader,omitempty"`
}

// AccessLog configuration
type AccessLog struct {
	Enabled bool   `json:"enabled"`
	Format  string `json:"format,omitempty"` // json, common, combined
	Path    string `json:"path,omitempty"`
}

// Route represents a routing configuration
type Route struct {
	// Metadata
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`

	// Spec
	GatewayRef  *GatewayRef  `json:"gatewayRef,omitempty"`
	Hostnames   []string     `json:"hostnames,omitempty"`
	Matches     []RouteMatch `json:"matches,omitempty"`
	BackendRefs []BackendRef `json:"backendRefs"`
	Filters     []Filter     `json:"filters,omitempty"`
	Timeout     string       `json:"timeout,omitempty"`
	Policies    []string     `json:"policies,omitempty"`
	Status      *RouteStatus `json:"status,omitempty"`

	// Resource version for optimistic locking
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

// RouteStatus represents route status
type RouteStatus struct {
	Conditions []Condition `json:"conditions,omitempty"`
}

// GatewayRef references a gateway
type GatewayRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// RouteMatch defines routing match conditions
type RouteMatch struct {
	Path    *PathMatch    `json:"path,omitempty"`
	Headers []HeaderMatch `json:"headers,omitempty"`
	Method  string        `json:"method,omitempty"`
}

// PathMatch defines path matching
type PathMatch struct {
	Type  string `json:"type"` // Exact, PathPrefix, RegularExpression
	Value string `json:"value"`
}

// HeaderMatch defines header matching
type HeaderMatch struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"` // Exact, RegularExpression
}

// BackendRef references a backend
type BackendRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Port      int    `json:"port,omitempty"`
	Weight    int    `json:"weight,omitempty"`
}

// Filter defines request/response filters
type Filter struct {
	Type           string          `json:"type"` // RequestHeaderModifier, ResponseHeaderModifier, URLRewrite, RequestRedirect
	RequestHeader  *HeaderModifier `json:"requestHeader,omitempty"`
	ResponseHeader *HeaderModifier `json:"responseHeader,omitempty"`
	URLRewrite     *URLRewrite     `json:"urlRewrite,omitempty"`
	Redirect       *Redirect       `json:"redirect,omitempty"`
}

// HeaderModifier modifies headers
type HeaderModifier struct {
	Set    []HeaderValue `json:"set,omitempty"`
	Add    []HeaderValue `json:"add,omitempty"`
	Remove []string      `json:"remove,omitempty"`
}

// HeaderValue represents a header key-value pair
type HeaderValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// URLRewrite defines URL rewrite rules
type URLRewrite struct {
	Hostname string        `json:"hostname,omitempty"`
	Path     *PathModifier `json:"path,omitempty"`
}

// PathModifier modifies the path
type PathModifier struct {
	Type               string `json:"type"` // ReplaceFullPath, ReplacePrefixMatch
	ReplaceFullPath    string `json:"replaceFullPath,omitempty"`
	ReplacePrefixMatch string `json:"replacePrefixMatch,omitempty"`
}

// Redirect defines redirect behavior
type Redirect struct {
	Scheme     string        `json:"scheme,omitempty"`
	Hostname   string        `json:"hostname,omitempty"`
	Port       int           `json:"port,omitempty"`
	Path       *PathModifier `json:"path,omitempty"`
	StatusCode int           `json:"statusCode,omitempty"`
}

// Backend represents an upstream backend configuration
type Backend struct {
	// Metadata
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`

	// Spec
	Endpoints      []Endpoint      `json:"endpoints"`
	LBPolicy       string          `json:"lbPolicy"` // RoundRobin, P2C, EWMA, RingHash, Maglev
	HealthCheck    *HealthCheck    `json:"healthCheck,omitempty"`
	CircuitBreaker *CircuitBreaker `json:"circuitBreaker,omitempty"`
	ConnectionPool *ConnectionPool `json:"connectionPool,omitempty"`
	TLS            *BackendTLS     `json:"tls,omitempty"`
	Status         *BackendStatus  `json:"status,omitempty"`

	// Resource version for optimistic locking
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

// BackendStatus represents backend status
type BackendStatus struct {
	HealthyEndpoints int         `json:"healthyEndpoints"`
	TotalEndpoints   int         `json:"totalEndpoints"`
	Conditions       []Condition `json:"conditions,omitempty"`
}

// Endpoint represents a backend endpoint
type Endpoint struct {
	Address string `json:"address"` // host:port
	Weight  int    `json:"weight,omitempty"`
}

// HealthCheck defines health check configuration
type HealthCheck struct {
	Protocol           string `json:"protocol"` // HTTP, TCP
	Path               string `json:"path,omitempty"`
	Port               int    `json:"port,omitempty"`
	Interval           string `json:"interval"`
	Timeout            string `json:"timeout"`
	HealthyThreshold   int    `json:"healthyThreshold"`
	UnhealthyThreshold int    `json:"unhealthyThreshold"`
}

// CircuitBreaker defines circuit breaker configuration
type CircuitBreaker struct {
	MaxConnections     int    `json:"maxConnections"`
	MaxPendingRequests int    `json:"maxPendingRequests"`
	MaxRequests        int    `json:"maxRequests"`
	MaxRetries         int    `json:"maxRetries"`
	ConsecutiveErrors  int    `json:"consecutiveErrors"`
	Interval           string `json:"interval"`
	BaseEjectionTime   string `json:"baseEjectionTime"`
	MaxEjectionPercent int    `json:"maxEjectionPercent"`
}

// ConnectionPool defines connection pool settings
type ConnectionPool struct {
	MaxConnections        int    `json:"maxConnections"`
	MaxIdleConnections    int    `json:"maxIdleConnections"`
	IdleTimeout           string `json:"idleTimeout"`
	MaxConnectionLifetime string `json:"maxConnectionLifetime"`
}

// BackendTLS defines TLS settings for backend connections
type BackendTLS struct {
	Enabled            bool   `json:"enabled"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify,omitempty"`
	CAFile             string `json:"caFile,omitempty"`
	CertFile           string `json:"certFile,omitempty"`
	KeyFile            string `json:"keyFile,omitempty"`
	ServerName         string `json:"serverName,omitempty"`
}

// Policy represents a policy configuration
type Policy struct {
	// Metadata
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`

	// Spec
	Type      string           `json:"type"` // RateLimit, CORS, IPFilter, JWT
	TargetRef *TargetRef       `json:"targetRef,omitempty"`
	RateLimit *RateLimitConfig `json:"rateLimit,omitempty"`
	CORS      *CORSConfig      `json:"cors,omitempty"`
	IPFilter  *IPFilterConfig  `json:"ipFilter,omitempty"`
	JWT       *JWTConfig       `json:"jwt,omitempty"`
	Status    *PolicyStatus    `json:"status,omitempty"`

	// Resource version for optimistic locking
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

// PolicyStatus represents policy status
type PolicyStatus struct {
	Conditions []Condition `json:"conditions,omitempty"`
}

// TargetRef references a target resource
type TargetRef struct {
	Group     string `json:"group,omitempty"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// RateLimitConfig defines rate limiting
type RateLimitConfig struct {
	RequestsPerSecond int    `json:"requestsPerSecond"`
	BurstSize         int    `json:"burstSize"`
	Key               string `json:"key,omitempty"` // client_ip, header:X-User-ID
}

// CORSConfig defines CORS settings
type CORSConfig struct {
	AllowOrigins     []string `json:"allowOrigins"`
	AllowMethods     []string `json:"allowMethods"`
	AllowHeaders     []string `json:"allowHeaders"`
	ExposeHeaders    []string `json:"exposeHeaders,omitempty"`
	MaxAge           int      `json:"maxAge,omitempty"`
	AllowCredentials bool     `json:"allowCredentials,omitempty"`
}

// IPFilterConfig defines IP filtering
type IPFilterConfig struct {
	AllowList []string `json:"allowList,omitempty"`
	DenyList  []string `json:"denyList,omitempty"`
}

// JWTConfig defines JWT validation
type JWTConfig struct {
	Issuer            string   `json:"issuer"`
	Audience          []string `json:"audience,omitempty"`
	JWKSURI           string   `json:"jwksUri,omitempty"`
	SecretKey         string   `json:"secretKey,omitempty"`
	AllowedAlgorithms []string `json:"allowedAlgorithms,omitempty"`
}

// ImportResult represents the result of an import operation
type ImportResult struct {
	Created []ResourceRef `json:"created"`
	Updated []ResourceRef `json:"updated"`
	Skipped []ResourceRef `json:"skipped"`
	Errors  []ImportError `json:"errors,omitempty"`
	DryRun  bool          `json:"dryRun"`
}

// ResourceRef references a resource
type ResourceRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// ImportError represents an import error
type ImportError struct {
	Resource ResourceRef `json:"resource"`
	Error    string      `json:"error"`
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationResult represents validation results
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

// Certificate represents a ProxyCertificate configuration
type Certificate struct {
	Name            string            `json:"name" yaml:"name"`
	Namespace       string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels          map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	Spec            CertificateSpec   `json:"spec" yaml:"spec"`
	Status          CertificateStatus `json:"status,omitempty" yaml:"status,omitempty"`
	ResourceVersion string            `json:"resourceVersion,omitempty" yaml:"resourceVersion,omitempty"`
}

// CertificateSpec defines the desired state of a certificate
type CertificateSpec struct {
	Domains     []string    `json:"domains" yaml:"domains"`
	Issuer      IssuerRef   `json:"issuer" yaml:"issuer"`
	SecretName  string      `json:"secretName,omitempty" yaml:"secretName,omitempty"`
	KeyType     string      `json:"keyType,omitempty" yaml:"keyType,omitempty"`
	RenewBefore string      `json:"renewBefore,omitempty" yaml:"renewBefore,omitempty"`
	MustStaple  bool        `json:"mustStaple,omitempty" yaml:"mustStaple,omitempty"`
	ACME        *ACMEConfig `json:"acme,omitempty" yaml:"acme,omitempty"`
}

// IssuerRef references a certificate issuer
type IssuerRef struct {
	Type string `json:"type" yaml:"type"`
}

// ACMEConfig defines ACME-specific configuration
type ACMEConfig struct {
	Server        string `json:"server,omitempty" yaml:"server,omitempty"`
	Email         string `json:"email,omitempty" yaml:"email,omitempty"`
	ChallengeType string `json:"challengeType,omitempty" yaml:"challengeType,omitempty"`
	DNSProvider   string `json:"dnsProvider,omitempty" yaml:"dnsProvider,omitempty"`
}

// CertificateStatus defines the observed state of a certificate
type CertificateStatus struct {
	State           string      `json:"state,omitempty" yaml:"state,omitempty"`
	SecretName      string      `json:"secretName,omitempty" yaml:"secretName,omitempty"`
	NotBefore       *time.Time  `json:"notBefore,omitempty" yaml:"notBefore,omitempty"`
	NotAfter        *time.Time  `json:"notAfter,omitempty" yaml:"notAfter,omitempty"`
	LastRenewalTime *time.Time  `json:"lastRenewalTime,omitempty" yaml:"lastRenewalTime,omitempty"`
	SerialNumber    string      `json:"serialNumber,omitempty" yaml:"serialNumber,omitempty"`
	Issuer          string      `json:"issuer,omitempty" yaml:"issuer,omitempty"`
	Message         string      `json:"message,omitempty" yaml:"message,omitempty"`
	Conditions      []Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// IPPool represents a ProxyIPPool configuration
type IPPool struct {
	Name            string            `json:"name" yaml:"name"`
	Namespace       string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels          map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	Spec            IPPoolSpec        `json:"spec" yaml:"spec"`
	Status          IPPoolStatus      `json:"status,omitempty" yaml:"status,omitempty"`
	ResourceVersion string            `json:"resourceVersion,omitempty" yaml:"resourceVersion,omitempty"`
}

// IPPoolSpec defines the desired state of an IP pool
type IPPoolSpec struct {
	CIDRs      []string `json:"cidrs,omitempty" yaml:"cidrs,omitempty"`
	Addresses  []string `json:"addresses,omitempty" yaml:"addresses,omitempty"`
	AutoAssign bool     `json:"autoAssign,omitempty" yaml:"autoAssign,omitempty"`
}

// IPPoolStatus defines the observed state of an IP pool
type IPPoolStatus struct {
	Allocated   int32          `json:"allocated,omitempty" yaml:"allocated,omitempty"`
	Available   int32          `json:"available,omitempty" yaml:"available,omitempty"`
	Allocations []IPAllocation `json:"allocations,omitempty" yaml:"allocations,omitempty"`
	Conditions  []Condition    `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// IPAllocation tracks a single IP allocation from a pool
type IPAllocation struct {
	Address string `json:"address" yaml:"address"`
	Owner   string `json:"owner" yaml:"owner"`
}

// NovaEdgeClusterModel represents a NovaEdgeCluster configuration
type NovaEdgeClusterModel struct {
	Name            string                 `json:"name" yaml:"name"`
	Namespace       string                 `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels          map[string]string      `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations     map[string]string      `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	Spec            map[string]interface{} `json:"spec" yaml:"spec"`
	Status          map[string]interface{} `json:"status,omitempty" yaml:"status,omitempty"`
	ResourceVersion string                 `json:"resourceVersion,omitempty" yaml:"resourceVersion,omitempty"`
}

// FederationModel represents a NovaEdgeFederation configuration
type FederationModel struct {
	Name            string                 `json:"name" yaml:"name"`
	Namespace       string                 `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels          map[string]string      `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations     map[string]string      `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	Spec            map[string]interface{} `json:"spec" yaml:"spec"`
	Status          map[string]interface{} `json:"status,omitempty" yaml:"status,omitempty"`
	ResourceVersion string                 `json:"resourceVersion,omitempty" yaml:"resourceVersion,omitempty"`
}

// RemoteClusterModel represents a NovaEdgeRemoteCluster configuration
type RemoteClusterModel struct {
	Name            string                 `json:"name" yaml:"name"`
	Namespace       string                 `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels          map[string]string      `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations     map[string]string      `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	Spec            map[string]interface{} `json:"spec" yaml:"spec"`
	Status          map[string]interface{} `json:"status,omitempty" yaml:"status,omitempty"`
	ResourceVersion string                 `json:"resourceVersion,omitempty" yaml:"resourceVersion,omitempty"`
}
