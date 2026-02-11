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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProtocolType defines the application protocol
// +kubebuilder:validation:Enum=HTTP;HTTPS;TCP;TLS;UDP
type ProtocolType string

const (
	// ProtocolTypeHTTP is plain HTTP
	ProtocolTypeHTTP ProtocolType = "HTTP"
	// ProtocolTypeHTTPS is HTTP over TLS
	ProtocolTypeHTTPS ProtocolType = "HTTPS"
	// ProtocolTypeHTTP3 is HTTP/3 over QUIC
	ProtocolTypeHTTP3 ProtocolType = "HTTP3"
	// ProtocolTypeTCP is plain TCP
	ProtocolTypeTCP ProtocolType = "TCP"
	// ProtocolTypeTLS is TLS-encrypted TCP
	ProtocolTypeTLS ProtocolType = "TLS"
	// ProtocolTypeUDP is plain UDP
	ProtocolTypeUDP ProtocolType = "UDP"
)

// TLSConfig defines TLS configuration for a listener
type TLSConfig struct {
	// SecretRef references a Kubernetes Secret containing TLS certificate and key
	// One of SecretRef, CertificateRef, ACME, or SelfSigned must be specified
	// +optional
	SecretRef *corev1.SecretReference `json:"secretRef,omitempty"`

	// CertificateRef references a ProxyCertificate resource
	// +optional
	CertificateRef *ObjectReference `json:"certificateRef,omitempty"`

	// ACME configures automatic certificate provisioning via ACME
	// This creates a ProxyCertificate automatically
	// +optional
	ACME *InlineACMEConfig `json:"acme,omitempty"`

	// SelfSigned generates a self-signed certificate
	// +optional
	SelfSigned *InlineSelfSignedConfig `json:"selfSigned,omitempty"`

	// MinVersion is the minimum TLS version (default: TLS 1.2)
	// +optional
	// +kubebuilder:validation:Enum=TLS1.2;TLS1.3
	MinVersion string `json:"minVersion,omitempty"`

	// CipherSuites is a list of allowed cipher suites
	// +optional
	CipherSuites []string `json:"cipherSuites,omitempty"`
}

// InlineACMEConfig configures inline ACME certificate provisioning
type InlineACMEConfig struct {
	// Email is the email address for ACME registration
	// +kubebuilder:validation:Required
	Email string `json:"email"`

	// ChallengeType specifies the ACME challenge type
	// +optional
	// +kubebuilder:default="http-01"
	// +kubebuilder:validation:Enum=http-01;dns-01;tls-alpn-01
	ChallengeType string `json:"challengeType,omitempty"`

	// DNSProvider for DNS-01 challenges (e.g., cloudflare, route53)
	// +optional
	DNSProvider string `json:"dnsProvider,omitempty"`

	// Server is the ACME server URL (default: Let's Encrypt production)
	// +optional
	Server string `json:"server,omitempty"`
}

// InlineSelfSignedConfig configures inline self-signed certificate generation
type InlineSelfSignedConfig struct {
	// Validity is the certificate validity duration (default: 8760h = 1 year)
	// +optional
	// +kubebuilder:default="8760h"
	Validity string `json:"validity,omitempty"`

	// Organization is the organization name in the certificate
	// +optional
	Organization string `json:"organization,omitempty"`
}

// QUICConfig defines QUIC-specific configuration for HTTP/3
type QUICConfig struct {
	// MaxIdleTimeout is the maximum idle timeout for QUIC connections
	// +optional
	// +kubebuilder:default="30s"
	MaxIdleTimeout string `json:"maxIdleTimeout,omitempty"`

	// MaxBiStreams is the maximum number of concurrent bidirectional streams
	// +optional
	// +kubebuilder:default=100
	MaxBiStreams int64 `json:"maxBiStreams,omitempty"`

	// MaxUniStreams is the maximum number of concurrent unidirectional streams
	// +optional
	// +kubebuilder:default=100
	MaxUniStreams int64 `json:"maxUniStreams,omitempty"`

	// Enable0RTT enables 0-RTT resumption (reduces connection establishment latency)
	// +optional
	// +kubebuilder:default=true
	Enable0RTT bool `json:"enable0RTT,omitempty"`
}

// Listener defines a port and protocol to listen on
type Listener struct {
	// Name is a unique name for this listener
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// Port is the network port to listen on
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`

	// Protocol is the application protocol
	// +kubebuilder:validation:Required
	Protocol ProtocolType `json:"protocol"`

	// TLS contains TLS configuration (required for HTTPS/TLS/HTTP3 protocols)
	// Use TLSCertificates for SNI support with multiple certificates
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`

	// TLSCertificates provides SNI support with multiple TLS certificates per listener
	// Key is the hostname, value is the TLS configuration for that hostname
	// Supports wildcard hostnames (e.g., "*.example.com")
	// +optional
	TLSCertificates map[string]TLSConfig `json:"tlsCertificates,omitempty"`

	// QUIC contains QUIC-specific configuration (optional for HTTP3 protocol)
	// +optional
	QUIC *QUICConfig `json:"quic,omitempty"`

	// Hostnames is a list of hostnames this listener accepts (for HTTP/HTTPS/HTTP3)
	// +optional
	Hostnames []string `json:"hostnames,omitempty"`

	// SSLRedirect enables automatic redirect from HTTP to HTTPS
	// +optional
	SSLRedirect bool `json:"sslRedirect,omitempty"`

	// MaxRequestBodySize is the maximum allowed request body size in bytes (0 = unlimited)
	// +optional
	MaxRequestBodySize int64 `json:"maxRequestBodySize,omitempty"`

	// AllowedSourceRanges is a list of CIDR ranges that are allowed to access this listener
	// If empty, all sources are allowed
	// +optional
	AllowedSourceRanges []string `json:"allowedSourceRanges,omitempty"`
}

// TracingConfig defines request tracing configuration
type TracingConfig struct {
	// Enabled enables request tracing
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// SamplingRate is the percentage of requests to trace (0-100)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=100
	SamplingRate *int32 `json:"samplingRate,omitempty"`

	// RequestIDHeader is the header name for the request ID
	// +optional
	// +kubebuilder:default="X-Request-ID"
	RequestIDHeader string `json:"requestIdHeader,omitempty"`

	// PropagateRequestID propagates the request ID to upstream backends
	// +optional
	// +kubebuilder:default=true
	PropagateRequestID bool `json:"propagateRequestId,omitempty"`

	// GenerateRequestID generates a request ID if not present
	// +optional
	// +kubebuilder:default=true
	GenerateRequestID bool `json:"generateRequestId,omitempty"`
}

// AccessLogConfig defines access logging configuration
type AccessLogConfig struct {
	// Enabled enables access logging
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Format specifies the log format (json, clf, custom)
	// +optional
	// +kubebuilder:validation:Enum=json;clf;custom
	// +kubebuilder:default="json"
	Format string `json:"format,omitempty"`

	// Template defines a custom log format template (for format=custom)
	// +optional
	Template string `json:"template,omitempty"`

	// Output defines where logs are written (stdout, file, both)
	// +optional
	// +kubebuilder:validation:Enum=stdout;file;both
	// +kubebuilder:default="stdout"
	Output string `json:"output,omitempty"`

	// FilePath is the path to the log file (required when output is file or both)
	// +optional
	FilePath string `json:"filePath,omitempty"`

	// MaxSize is the maximum size of a log file before rotation (e.g., "100Mi")
	// +optional
	MaxSize string `json:"maxSize,omitempty"`

	// MaxBackups is the maximum number of rotated log files to retain
	// +optional
	MaxBackups *int32 `json:"maxBackups,omitempty"`

	// IncludeHeaders includes specified request/response headers in logs
	// +optional
	IncludeHeaders []string `json:"includeHeaders,omitempty"`

	// ExcludePaths excludes specified paths from logging (e.g., health checks)
	// +optional
	ExcludePaths []string `json:"excludePaths,omitempty"`

	// FilterStatusCodes limits logging to specific HTTP status codes
	// +optional
	FilterStatusCodes []int32 `json:"filterStatusCodes,omitempty"`

	// SampleRate defines the fraction of requests to log (0.0-1.0, default 1.0)
	// +optional
	// +kubebuilder:validation:Minimum=0
}

// CustomErrorPage defines a custom error page configuration
type CustomErrorPage struct {
	// Codes is a list of HTTP status codes this page applies to
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Codes []int32 `json:"codes"`

	// Path is the path to serve for these error codes
	// +optional
	Path string `json:"path,omitempty"`

	// Body is the response body to return for these error codes
	// +optional
	Body string `json:"body,omitempty"`

	// ContentType is the content type of the error response
	// +optional
	// +kubebuilder:default="text/html"
	ContentType string `json:"contentType,omitempty"`
}

// CompressionConfig defines response compression settings
type CompressionConfig struct {
	// Enabled enables response compression
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// MinSize is the minimum response body size in bytes before compression triggers
	// Responses smaller than this are sent uncompressed
	// +optional
	// +kubebuilder:default="1024"
	MinSize string `json:"minSize,omitempty"`

	// Level is the compression level (1-9 for gzip, 0-11 for brotli)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=11
	// +kubebuilder:default=6
	Level int32 `json:"level,omitempty"`

	// Algorithms is the list of supported compression algorithms
	// +optional
	// +kubebuilder:default={"gzip","br"}
	Algorithms []string `json:"algorithms,omitempty"`

	// ExcludeTypes is a list of content type patterns to skip compression
	// Supports glob-style wildcards (e.g., "image/*", "video/*")
	// +optional
	ExcludeTypes []string `json:"excludeTypes,omitempty"`
}

// RedirectSchemeConfig defines HTTP to HTTPS redirect configuration
type RedirectSchemeConfig struct {
	// Enabled enables scheme redirection
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Scheme is the target scheme (default: "https")
	// +optional
	// +kubebuilder:default="https"
	Scheme string `json:"scheme,omitempty"`

	// Port is the target port (default: 443)
	// +optional
	// +kubebuilder:default=443
	Port int32 `json:"port,omitempty"`

	// StatusCode is the HTTP redirect status code (301 or 302, default: 301)
	// +optional
	// +kubebuilder:validation:Enum=301;302
	// +kubebuilder:default=301
	StatusCode int32 `json:"statusCode,omitempty"`

	// Exclusions is a list of path prefixes to exclude from redirection
	// +optional
	Exclusions []string `json:"exclusions,omitempty"`
}

// ProxyGatewaySpec defines the desired state of ProxyGateway
type ProxyGatewaySpec struct {
	// VIPRef references the ProxyVIP to use for this gateway
	// +kubebuilder:validation:Required
	VIPRef string `json:"vipRef"`

	// IngressClassName is the ingress class name for Ingress resource compatibility
	// +optional
	IngressClassName string `json:"ingressClassName,omitempty"`

	// LoadBalancerClass specifies the load balancer class this gateway belongs to.
	// Controllers only reconcile gateways matching their configured class.
	// Default: "novaedge.io/proxy"
	// +optional
	LoadBalancerClass string `json:"loadBalancerClass,omitempty"`

	// Cache configures response caching for this gateway
	// +optional
	Cache *GatewayCacheConfig `json:"cache,omitempty"`

	// Listeners define the ports and protocols this gateway accepts
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Listeners []Listener `json:"listeners"`

	// Tracing defines request tracing configuration
	// +optional
	Tracing *TracingConfig `json:"tracing,omitempty"`

	// AccessLog defines access logging configuration
	// +optional
	AccessLog *AccessLogConfig `json:"accessLog,omitempty"`

	// CustomErrorPages defines custom error pages
	// +optional
	CustomErrorPages []CustomErrorPage `json:"customErrorPages,omitempty"`

	// Compression defines response compression configuration
	// +optional
	Compression *CompressionConfig `json:"compression,omitempty"`

	// RedirectScheme defines HTTP to HTTPS redirect configuration
	// +optional
	RedirectScheme *RedirectSchemeConfig `json:"redirectScheme,omitempty"`
}

// GatewayCacheConfig configures HTTP response caching for the gateway
type GatewayCacheConfig struct {
	// Enabled enables response caching
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// MaxSize is the maximum cache memory (e.g., "256Mi")
	// +optional
	MaxSize string `json:"maxSize,omitempty"`

	// DefaultTTL is the default time-to-live for cached responses (e.g., "5m")
	// +optional
	DefaultTTL string `json:"defaultTTL,omitempty"`

	// MaxTTL is the maximum allowed TTL for cached responses (e.g., "1h")
	// +optional
	MaxTTL string `json:"maxTTL,omitempty"`

	// MaxEntrySize is the maximum size of a single cached response (e.g., "1Mi")
	// +optional
	MaxEntrySize string `json:"maxEntrySize,omitempty"`
}

// ProxyGatewayStatus defines the observed state of ProxyGateway
type ProxyGatewayStatus struct {
	// Conditions represent the latest available observations of the gateway's state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ListenerStatus contains status for each listener
	// +optional
	ListenerStatus []ListenerStatus `json:"listenerStatus,omitempty"`
}

// ListenerStatus contains status for a single listener
type ListenerStatus struct {
	// Name matches the listener name
	Name string `json:"name"`

	// Ready indicates if the listener is ready to accept traffic
	Ready bool `json:"ready"`

	// Reason provides detail about the listener status
	// +optional
	Reason string `json:"reason,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="VIP Ref",type=string,JSONPath=`.spec.vipRef`
// +kubebuilder:printcolumn:name="Ingress Class",type=string,JSONPath=`.spec.ingressClassName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProxyGateway defines listeners, TLS configuration, and ingress class
type ProxyGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyGatewaySpec   `json:"spec,omitempty"`
	Status ProxyGatewayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyGatewayList contains a list of ProxyGateway
type ProxyGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyGateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProxyGateway{}, &ProxyGatewayList{})
}
