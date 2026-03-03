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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LoadBalancingPolicy defines the load balancing algorithm
// +kubebuilder:validation:Enum=RoundRobin;P2C;EWMA;RingHash;Maglev;LeastConn;SourceHash;Sticky
type LoadBalancingPolicy string

const (
	// LBPolicyRoundRobin distributes requests in round-robin fashion
	LBPolicyRoundRobin LoadBalancingPolicy = "RoundRobin"
	// LBPolicyP2C uses Power of Two Choices algorithm
	LBPolicyP2C LoadBalancingPolicy = "P2C"
	// LBPolicyEWMA uses Exponentially Weighted Moving Average (latency-aware)
	LBPolicyEWMA LoadBalancingPolicy = "EWMA"
	// LBPolicyRingHash uses consistent hashing with ring
	LBPolicyRingHash LoadBalancingPolicy = "RingHash"
	// LBPolicyMaglev uses Maglev consistent hashing
	LBPolicyMaglev LoadBalancingPolicy = "Maglev"
	// LBPolicyLeastConn uses Least Connections algorithm
	LBPolicyLeastConn LoadBalancingPolicy = "LeastConn"
	// LBPolicySourceHash uses source IP hash for consistent routing
	LBPolicySourceHash LoadBalancingPolicy = "SourceHash"
	// LBPolicySticky uses cookie-based sticky sessions
	LBPolicySticky LoadBalancingPolicy = "Sticky"
)

// ServiceReference references a Kubernetes Service
type ServiceReference struct {
	// Name is the name of the Service
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`

	// Namespace is the namespace of the Service (defaults to backend namespace)
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Namespace *string `json:"namespace,omitempty"`

	// Port is the port number on the Service
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}

// CircuitBreaker defines circuit breaker configuration
type CircuitBreaker struct {
	// MaxConnections is the maximum number of connections to the backend
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100000
	MaxConnections *int32 `json:"maxConnections,omitempty"`

	// MaxPendingRequests is the maximum number of pending requests
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100000
	MaxPendingRequests *int32 `json:"maxPendingRequests,omitempty"`

	// MaxRequests is the maximum number of parallel requests
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100000
	MaxRequests *int32 `json:"maxRequests,omitempty"`

	// MaxRetries is the maximum number of parallel retries
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	MaxRetries *int32 `json:"maxRetries,omitempty"`

	// ConsecutiveFailures is the number of consecutive failures before opening the circuit
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=5
	ConsecutiveFailures *int32 `json:"consecutiveFailures,omitempty"`

	// Interval is the time window for counting failures
	// +optional
	// +kubebuilder:default="10s"
	Interval metav1.Duration `json:"interval,omitempty"`

	// BaseEjectionTime is how long a host is ejected for
	// +optional
	// +kubebuilder:default="30s"
	BaseEjectionTime metav1.Duration `json:"baseEjectionTime,omitempty"`
}

// RetryPolicy defines retry behavior for failed requests
type RetryPolicy struct {
	// NumRetries is the number of retry attempts
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=1
	NumRetries *int32 `json:"numRetries,omitempty"`

	// PerTryTimeout is the timeout for each retry attempt
	// +optional
	// +kubebuilder:default="2s"
	PerTryTimeout metav1.Duration `json:"perTryTimeout,omitempty"`

	// RetryOn specifies conditions that trigger a retry (comma-separated)
	// Valid values: 5xx, gateway-error, connect-failure, retriable-4xx, refused-stream, reset
	// +optional
	// +kubebuilder:default="5xx,gateway-error,connect-failure"
	RetryOn string `json:"retryOn,omitempty"`

	// RetryBackoff configures exponential backoff for retries
	// +optional
	RetryBackoff *RetryBackoff `json:"retryBackoff,omitempty"`
}

// RetryBackoff defines exponential backoff parameters for retries
type RetryBackoff struct {
	// BaseInterval is the initial backoff interval
	// +optional
	// +kubebuilder:default="25ms"
	BaseInterval metav1.Duration `json:"baseInterval,omitempty"`

	// MaxInterval is the maximum backoff interval
	// +optional
	// +kubebuilder:default="250ms"
	MaxInterval metav1.Duration `json:"maxInterval,omitempty"`
}

// SessionAffinityConfig defines session affinity (sticky sessions) configuration
type SessionAffinityConfig struct {
	// Type specifies the session affinity type
	// +kubebuilder:validation:Enum=Cookie;Header;SourceIP
	// +kubebuilder:default="Cookie"
	Type string `json:"type,omitempty"`

	// CookieName is the name of the cookie for cookie-based affinity
	// +optional
	// +kubebuilder:default="NOVAEDGE_AFFINITY"
	CookieName string `json:"cookieName,omitempty"`

	// CookieTTL is the TTL for the affinity cookie
	// +optional
	// +kubebuilder:default="0s"
	CookieTTL metav1.Duration `json:"cookieTTL,omitempty"`

	// CookiePath is the path for the affinity cookie
	// +optional
	// +kubebuilder:default="/"
	CookiePath string `json:"cookiePath,omitempty"`

	// HeaderName is the header name for header-based affinity
	// +optional
	HeaderName string `json:"headerName,omitempty"`

	// Secure sets the Secure flag on the affinity cookie
	// +optional
	Secure bool `json:"secure,omitempty"`

	// SameSite sets the SameSite attribute on the affinity cookie
	// +optional
	// +kubebuilder:validation:Enum=Strict;Lax;None
	SameSite string `json:"sameSite,omitempty"`
}

// ConnectionPool defines connection pool configuration
type ConnectionPool struct {
	// MaxIdleConns is the maximum number of idle connections across all hosts
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10000
	// +kubebuilder:default=100
	MaxIdleConns *int32 `json:"maxIdleConns,omitempty"`

	// MaxIdleConnsPerHost is the maximum number of idle connections per host
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=10
	MaxIdleConnsPerHost *int32 `json:"maxIdleConnsPerHost,omitempty"`

	// MaxConnsPerHost is the maximum number of connections per host (0 = unlimited)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10000
	MaxConnsPerHost *int32 `json:"maxConnsPerHost,omitempty"`

	// IdleConnTimeout is the maximum time an idle connection is kept
	// +optional
	// +kubebuilder:default="90s"
	IdleConnTimeout metav1.Duration `json:"idleConnTimeout,omitempty"`
}

// HealthCheck defines active health check configuration
type HealthCheck struct {
	// Interval is the time between health checks
	// +optional
	// +kubebuilder:default="10s"
	Interval metav1.Duration `json:"interval,omitempty"`

	// Timeout is the time to wait for a health check response
	// +optional
	// +kubebuilder:default="5s"
	Timeout metav1.Duration `json:"timeout,omitempty"`

	// HealthyThreshold is the number of successful health checks required
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=2
	HealthyThreshold *int32 `json:"healthyThreshold,omitempty"`

	// UnhealthyThreshold is the number of failed health checks before marking unhealthy
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3
	UnhealthyThreshold *int32 `json:"unhealthyThreshold,omitempty"`

	// HTTPPath is the HTTP path for health checks (for HTTP backends)
	// +optional
	HTTPPath *string `json:"httpPath,omitempty"`
}

// UpstreamProxyProtocolConfig defines PROXY protocol settings for backend connections.
// When enabled, the proxy sends a PROXY protocol header to backends containing
// the real client IP and port.
type UpstreamProxyProtocolConfig struct {
	// Enabled enables sending PROXY protocol headers to this backend
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Version is the PROXY protocol version to send (1 or 2, default: 1)
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=2
	// +kubebuilder:default=1
	Version int32 `json:"version,omitempty"`
}

// ProxyBackendSpec defines the desired state of ProxyBackend
type ProxyBackendSpec struct {
	// ServiceRef references a Kubernetes Service
	// +optional
	ServiceRef *ServiceReference `json:"serviceRef,omitempty"`

	// LBPolicy defines the load balancing algorithm
	// +optional
	// +kubebuilder:default=RoundRobin
	LBPolicy LoadBalancingPolicy `json:"lbPolicy,omitempty"`

	// ConnectTimeout is the timeout for establishing connections
	// +optional
	// +kubebuilder:default="2s"
	ConnectTimeout metav1.Duration `json:"connectTimeout,omitempty"`

	// IdleTimeout is the timeout for idle connections
	// +optional
	// +kubebuilder:default="60s"
	IdleTimeout metav1.Duration `json:"idleTimeout,omitempty"`

	// CircuitBreaker defines circuit breaker settings
	// +optional
	CircuitBreaker *CircuitBreaker `json:"circuitBreaker,omitempty"`

	// HealthCheck defines active health check configuration
	// +optional
	HealthCheck *HealthCheck `json:"healthCheck,omitempty"`

	// TLS enables TLS for connections to this backend
	// +optional
	TLS *BackendTLSConfig `json:"tls,omitempty"`

	// ConnectionPool defines connection pool configuration
	// +optional
	ConnectionPool *ConnectionPool `json:"connectionPool,omitempty"`

	// RetryPolicy defines retry behavior for failed requests
	// +optional
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`

	// SessionAffinity defines sticky session configuration
	// +optional
	SessionAffinity *SessionAffinityConfig `json:"sessionAffinity,omitempty"`

	// UpstreamProxyProtocol configures sending PROXY protocol headers to this backend.
	// This allows backends to see the real client IP when behind the proxy.
	// +optional
	UpstreamProxyProtocol *UpstreamProxyProtocolConfig `json:"upstreamProxyProtocol,omitempty"`

	// Protocol specifies the backend protocol (HTTP, HTTPS, gRPC, gRPCS, HTTP2)
	// +optional
	// +kubebuilder:validation:Enum=HTTP;HTTPS;gRPC;gRPCS;HTTP2
	// +kubebuilder:default="HTTP"
	Protocol string `json:"protocol,omitempty"`

	// SlowStart configures gradual traffic ramp-up for new/recovering endpoints
	// +optional
	SlowStart *SlowStartConfig `json:"slowStart,omitempty"`

	// OutlierDetection configures per-endpoint outlier detection and auto-ejection
	// +optional
	OutlierDetection *OutlierDetectionConfig `json:"outlierDetection,omitempty"`
}

// BackendTLSMode defines the TLS verification mode for backend connections
// +kubebuilder:validation:Enum=verify;skip-verify;mtls
type BackendTLSMode string

const (
	// BackendTLSModeVerify verifies the backend certificate (default)
	BackendTLSModeVerify BackendTLSMode = "verify"
	// BackendTLSModeSkipVerify skips certificate verification
	BackendTLSModeSkipVerify BackendTLSMode = "skip-verify"
	// BackendTLSModeMTLS enables mutual TLS with client certificate
	BackendTLSModeMTLS BackendTLSMode = "mtls"
)

// BackendTLSConfig defines TLS settings for backend connections
type BackendTLSConfig struct {
	// Enabled indicates whether to use TLS
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// Mode specifies the TLS verification mode
	// +optional
	// +kubebuilder:default="verify"
	Mode BackendTLSMode `json:"mode,omitempty"`

	// InsecureSkipVerify is deprecated, use Mode: skip-verify instead
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// CACertSecretRef references a Secret containing CA certificates for verification
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	CACertSecretRef *string `json:"caCertSecretRef,omitempty"`

	// ClientCertSecretRef references a Secret containing client certificate for mTLS
	// The Secret should contain tls.crt and tls.key
	// +optional
	ClientCertSecretRef *string `json:"clientCertSecretRef,omitempty"`

	// SelfSigned generates a self-signed client certificate for mTLS
	// +optional
	SelfSigned bool `json:"selfSigned,omitempty"`

	// ServerName overrides the server name for SNI and certificate verification
	// +optional
	ServerName string `json:"serverName,omitempty"`
}

// ProxyBackendStatus defines the observed state of ProxyBackend
type ProxyBackendStatus struct {
	// Conditions represent the latest available observations of the backend's state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// EndpointCount is the number of healthy endpoints
	// +optional
	EndpointCount int32 `json:"endpointCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Service",type=string,JSONPath=`.spec.serviceRef.name`
// +kubebuilder:printcolumn:name="LB Policy",type=string,JSONPath=`.spec.lbPolicy`
// +kubebuilder:printcolumn:name="Endpoints",type=integer,JSONPath=`.status.endpointCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProxyBackend maps to Kubernetes Services or external endpoints
type ProxyBackend struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyBackendSpec   `json:"spec,omitempty"`
	Status ProxyBackendStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyBackendList contains a list of ProxyBackend
type ProxyBackendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyBackend `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProxyBackend{}, &ProxyBackendList{})
}

// SlowStartConfig configures gradual traffic ramp-up for new/recovering endpoints
type SlowStartConfig struct {
	// Window is the duration over which traffic ramps from 0% to 100%
	// +kubebuilder:validation:Required
	Window metav1.Duration `json:"window"`

	// Aggression controls the ramp curve shape (1.0 = linear, >1.0 = concave, <1.0 = convex)
	// +optional
	// +kubebuilder:default="1.0"
	Aggression string `json:"aggression,omitempty"`
}

// OutlierDetectionConfig configures per-endpoint outlier detection and auto-ejection
type OutlierDetectionConfig struct {
	// Interval between detection sweeps
	// +optional
	// +kubebuilder:default="10s"
	Interval metav1.Duration `json:"interval,omitempty"`

	// Consecutive5xxThreshold ejects after this many consecutive server errors
	// +optional
	// +kubebuilder:default=5
	// +kubebuilder:validation:Minimum=1
	Consecutive5xxThreshold *int32 `json:"consecutive5xxThreshold,omitempty"`

	// BaseEjectionDuration is the initial ejection time (increases with exponential backoff)
	// +optional
	// +kubebuilder:default="30s"
	BaseEjectionDuration metav1.Duration `json:"baseEjectionDuration,omitempty"`

	// MaxEjectionPercent limits the maximum percentage of endpoints that can be ejected
	// +optional
	// +kubebuilder:default=50
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	MaxEjectionPercent *int32 `json:"maxEjectionPercent,omitempty"`

	// SuccessRateMinHosts is the minimum cluster size for success rate analysis
	// +optional
	// +kubebuilder:default=5
	SuccessRateMinHosts *int32 `json:"successRateMinHosts,omitempty"`

	// SuccessRateMinRequests is the minimum requests per host for success rate analysis
	// +optional
	// +kubebuilder:default=100
	SuccessRateMinRequests *int32 `json:"successRateMinRequests,omitempty"`

	// SuccessRateStdevFactor is the number of standard deviations below mean for ejection
	// +optional
	// +kubebuilder:default="1.9"
	SuccessRateStdevFactor string `json:"successRateStdevFactor,omitempty"`
}
