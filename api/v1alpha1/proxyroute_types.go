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

// PathMatchType specifies the semantics of how HTTP paths should be compared
// +kubebuilder:validation:Enum=Exact;PathPrefix;RegularExpression
type PathMatchType string

const (
	// PathMatchExact matches the URL path exactly
	PathMatchExact PathMatchType = "Exact"
	// PathMatchPathPrefix matches based on a URL path prefix
	PathMatchPathPrefix PathMatchType = "PathPrefix"
	// PathMatchRegularExpression matches based on a regular expression
	PathMatchRegularExpression PathMatchType = "RegularExpression"
)

// HeaderMatchType specifies the semantics of how HTTP header values should be compared
// +kubebuilder:validation:Enum=Exact;RegularExpression
type HeaderMatchType string

const (
	// HeaderMatchExact matches the header value exactly
	HeaderMatchExact HeaderMatchType = "Exact"
	// HeaderMatchRegularExpression matches based on a regular expression
	HeaderMatchRegularExpression HeaderMatchType = "RegularExpression"
)

// HTTPPathMatch describes how to match the path of an HTTP request
type HTTPPathMatch struct {
	// Type specifies how to match against the path value
	// +kubebuilder:validation:Required
	Type PathMatchType `json:"type"`

	// Value is the path to match
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
}

// HTTPHeaderMatch describes how to match an HTTP header
type HTTPHeaderMatch struct {
	// Type specifies how to match against the header value
	// +optional
	// +kubebuilder:default=Exact
	Type HeaderMatchType `json:"type,omitempty"`

	// Name is the name of the HTTP header
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Value is the value of the header
	// +kubebuilder:validation:Required
	Value string `json:"value"`
}

// HTTPQueryParamMatch describes how to match a query parameter
type HTTPQueryParamMatch struct {
	// Type specifies how to match against the query parameter value
	// +optional
	// +kubebuilder:default=Exact
	Type HeaderMatchType `json:"type,omitempty"`

	// Name is the name of the query parameter
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Value is the value of the query parameter
	// +kubebuilder:validation:Required
	Value string `json:"value"`
}

// HTTPRouteMatch defines a match for an HTTP request
type HTTPRouteMatch struct {
	// Path specifies a HTTP request path matcher
	// +optional
	Path *HTTPPathMatch `json:"path,omitempty"`

	// Headers specifies HTTP request header matchers
	// +optional
	// +kubebuilder:validation:MaxItems=16
	Headers []HTTPHeaderMatch `json:"headers,omitempty"`

	// QueryParams specifies HTTP query parameter matchers
	// +optional
	// +kubebuilder:validation:MaxItems=16
	QueryParams []HTTPQueryParamMatch `json:"queryParams,omitempty"`

	// Method matches the HTTP method
	// +optional
	// +kubebuilder:validation:Enum=GET;HEAD;POST;PUT;PATCH;DELETE;CONNECT;OPTIONS;TRACE
	Method *string `json:"method,omitempty"`
}

// HTTPRouteFilterType identifies a type of filter
// +kubebuilder:validation:Enum=AddHeader;RemoveHeader;RequestRedirect;URLRewrite;RequestMirror;ResponseAddHeader;ResponseRemoveHeader;ResponseSetHeader
type HTTPRouteFilterType string

const (
	// HTTPRouteFilterAddHeader adds HTTP request headers
	HTTPRouteFilterAddHeader HTTPRouteFilterType = "AddHeader"
	// HTTPRouteFilterRemoveHeader removes HTTP request headers
	HTTPRouteFilterRemoveHeader HTTPRouteFilterType = "RemoveHeader"
	// HTTPRouteFilterRequestRedirect redirects the request
	HTTPRouteFilterRequestRedirect HTTPRouteFilterType = "RequestRedirect"
	// HTTPRouteFilterURLRewrite rewrites the request URL
	HTTPRouteFilterURLRewrite HTTPRouteFilterType = "URLRewrite"
	// HTTPRouteFilterRequestMirror mirrors the request to another backend
	HTTPRouteFilterRequestMirror HTTPRouteFilterType = "RequestMirror"
	// HTTPRouteFilterResponseAddHeader adds HTTP response headers
	HTTPRouteFilterResponseAddHeader HTTPRouteFilterType = "ResponseAddHeader"
	// HTTPRouteFilterResponseRemoveHeader removes HTTP response headers
	HTTPRouteFilterResponseRemoveHeader HTTPRouteFilterType = "ResponseRemoveHeader"
	// HTTPRouteFilterResponseSetHeader sets HTTP response headers (replaces existing)
	HTTPRouteFilterResponseSetHeader HTTPRouteFilterType = "ResponseSetHeader"
)

// HTTPHeader represents an HTTP header name and value
type HTTPHeader struct {
	// Name is the name of the header
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Value is the value of the header
	// +kubebuilder:validation:Required
	Value string `json:"value"`
}

// HTTPRouteFilter defines processing steps that must be completed during the request or response lifecycle
type HTTPRouteFilter struct {
	// Type identifies the type of filter to apply
	// +kubebuilder:validation:Required
	Type HTTPRouteFilterType `json:"type"`

	// Add contains headers to add (for AddHeader type)
	// +optional
	Add []HTTPHeader `json:"add,omitempty"`

	// Remove contains header names to remove (for RemoveHeader type)
	// +optional
	Remove []string `json:"remove,omitempty"`

	// RedirectURL is the URL to redirect to (for RequestRedirect type)
	// +optional
	RedirectURL *string `json:"redirectURL,omitempty"`

	// RewritePath is the path to rewrite to (for URLRewrite type)
	// +optional
	RewritePath *string `json:"rewritePath,omitempty"`

	// MirrorBackend specifies the backend to mirror requests to (for RequestMirror type)
	// +optional
	MirrorBackend *BackendRef `json:"mirrorBackend,omitempty"`

	// MirrorPercent specifies the percentage of requests to mirror (0-100)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=100
	MirrorPercent *int32 `json:"mirrorPercent,omitempty"`

	// ResponseAdd contains headers to add to the response (for ResponseAddHeader type)
	// +optional
	ResponseAdd []HTTPHeader `json:"responseAdd,omitempty"`

	// ResponseRemove contains header names to remove from the response (for ResponseRemoveHeader type)
	// +optional
	ResponseRemove []string `json:"responseRemove,omitempty"`

	// ResponseSet contains headers to set on the response (for ResponseSetHeader type)
	// These headers will replace any existing values
	// +optional
	ResponseSet []HTTPHeader `json:"responseSet,omitempty"`
}

// BackendRef references a backend for routing
type BackendRef struct {
	// Name is the name of the ProxyBackend
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the ProxyBackend (defaults to route namespace)
	// +optional
	Namespace *string `json:"namespace,omitempty"`

	// Weight defines the proportion of requests sent to this backend
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	Weight *int32 `json:"weight,omitempty"`
}

// RouteLimits defines per-route request size limits and timeouts
type RouteLimits struct {
	// MaxRequestBodySize is the maximum request body size (e.g., "10Mi", "1048576")
	// Requests exceeding this limit receive a 413 Payload Too Large response
	// +optional
	MaxRequestBodySize string `json:"maxRequestBodySize,omitempty"`

	// RequestTimeout is the total request timeout (e.g., "30s", "1m")
	// If the upstream does not respond in time, a 504 Gateway Timeout is returned
	// +optional
	RequestTimeout string `json:"requestTimeout,omitempty"`

	// IdleTimeout is the connection idle timeout (e.g., "60s")
	// Connections with no data received within this time are closed
	// +optional
	IdleTimeout string `json:"idleTimeout,omitempty"`
}

// RouteBufferingConfig defines request and response buffering settings
type RouteBufferingConfig struct {
	// Request enables buffering the entire request body before forwarding
	// This is required for retry support since the body must be re-readable
	// +optional
	Request bool `json:"request,omitempty"`

	// Response enables buffering the entire response body before sending to client
	// This is useful for response transformations
	// +optional
	Response bool `json:"response,omitempty"`

	// MaxSize is the maximum buffer size (e.g., "50Mi", "52428800")
	// Bodies exceeding this receive 413 for requests or stream-through for responses
	// +optional
	MaxSize string `json:"maxSize,omitempty"`
}

// RouteMirrorConfig configures traffic mirroring for a route rule
type RouteMirrorConfig struct {
	// BackendRef references the mirror backend
	// +kubebuilder:validation:Required
	BackendRef BackendRef `json:"backendRef"`

	// Percentage is the percentage of requests to mirror (0-100)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=100
	Percentage int32 `json:"percentage,omitempty"`
}

// RetryConfig defines automatic request retry behavior for failed backend requests
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=3
	MaxRetries int32 `json:"maxRetries,omitempty"`

	// PerTryTimeout is the timeout for each retry attempt
	// +optional
	PerTryTimeout *metav1.Duration `json:"perTryTimeout,omitempty"`

	// RetryOn specifies conditions that trigger a retry
	// Valid values: "5xx", "connection-failure", "reset", "refused-stream"
	// +optional
	RetryOn []string `json:"retryOn,omitempty"`

	// RetryBudget is the percentage of requests that can be retried (0.0-1.0)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	RetryBudget *float64 `json:"retryBudget,omitempty"`

	// BackoffBase is the base interval for exponential backoff between retries
	// +optional
	BackoffBase *metav1.Duration `json:"backoffBase,omitempty"`

	// RetryMethods specifies which HTTP methods are eligible for retry
	// Default: GET, HEAD, OPTIONS
	// +optional
	RetryMethods []string `json:"retryMethods,omitempty"`
}

// HTTPRouteRule defines semantics for matching an HTTP request and routing it
type HTTPRouteRule struct {
	// Matches define conditions used for matching the rule against incoming requests
	// +optional
	// +kubebuilder:validation:MaxItems=8
	Matches []HTTPRouteMatch `json:"matches,omitempty"`

	// Filters define processing steps that must be completed during the request or response lifecycle
	// +optional
	// +kubebuilder:validation:MaxItems=16
	Filters []HTTPRouteFilter `json:"filters,omitempty"`

	// BackendRefs references the backends to route to with optional weights
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	BackendRefs []BackendRef `json:"backendRefs"`

	// Mirror configures traffic mirroring for this route rule
	// +optional
	Mirror *RouteMirrorConfig `json:"mirror,omitempty"`

	// Timeout is the request timeout for this rule
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// MaxRequestBodySize is the maximum request body size for this rule (0 = unlimited)
	// Overrides the listener-level setting for this specific route
	// +optional
	MaxRequestBodySize *int64 `json:"maxRequestBodySize,omitempty"`

	// Limits defines per-route request size limits and timeouts
	// +optional
	Limits *RouteLimits `json:"limits,omitempty"`

	// Buffering defines request and response buffering settings
	// +optional
	Buffering *RouteBufferingConfig `json:"buffering,omitempty"`

	// Retry defines automatic retry behavior for failed requests to backends
	// +optional
	Retry *RetryConfig `json:"retry,omitempty"`

	// FaultInjection configures fault injection for chaos engineering
	// +optional
	FaultInjection *FaultInjectionConfig `json:"faultInjection,omitempty"`

	// BodyTransform configures JSON body transformation for this rule
	// +optional
	BodyTransform *BodyTransformConfig `json:"bodyTransform,omitempty"`
}

// MiddlewarePipelineConfig defines a composable middleware pipeline for a route
type MiddlewarePipelineConfig struct {
	// Middleware is an ordered list of middleware references
	// +optional
	// +kubebuilder:validation:MaxItems=32
	Middleware []MiddlewareRefConfig `json:"middleware,omitempty"`
}

// MiddlewareRefConfig references a middleware to include in the pipeline
type MiddlewareRefConfig struct {
	// Type is the middleware type: builtin or wasm
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=builtin;wasm
	Type string `json:"type"`

	// Name identifies the middleware (builtin name or WASM plugin name)
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Priority determines execution order (lower = earlier)
	// +optional
	// +kubebuilder:default=100
	Priority int `json:"priority,omitempty"`

	// Config holds optional key-value configuration for the middleware
	// +optional
	Config map[string]string `json:"config,omitempty"`
}

// ProxyRouteSpec defines the desired state of ProxyRoute
type ProxyRouteSpec struct {
	// Hostnames defines the hostnames for which this route applies
	// +optional
	Hostnames []string `json:"hostnames,omitempty"`

	// Rules are a list of HTTP routing rules
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	Rules []HTTPRouteRule `json:"rules"`

	// AccessLog defines per-route access logging configuration
	// Overrides gateway-level access log settings for this route
	// +optional
	AccessLog *RouteAccessLogConfig `json:"accessLog,omitempty"`

	// Pipeline defines a composable middleware pipeline for this route
	// +optional
	Pipeline *MiddlewarePipelineConfig `json:"pipeline,omitempty"`

	// Expression is a boolean routing expression for advanced matching
	// Syntax: (header:X-Env == "staging") AND (path prefix "/api" OR path prefix "/v2")
	// +optional
	Expression string `json:"expression,omitempty"`
}

// ProxyRouteStatus defines the observed state of ProxyRoute
type ProxyRouteStatus struct {
	// Conditions represent the latest available observations of the route's state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Hostnames",type=string,JSONPath=`.spec.hostnames`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProxyRoute defines routing rules for HTTP requests
type ProxyRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyRouteSpec   `json:"spec,omitempty"`
	Status ProxyRouteStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyRouteList contains a list of ProxyRoute
type ProxyRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyRoute `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProxyRoute{}, &ProxyRouteList{})
}

// RouteAccessLogConfig defines per-route access logging configuration
type RouteAccessLogConfig struct {
	// Enabled enables access logging for this route
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Format specifies the log format (json, clf, custom)
	// +optional
	// +kubebuilder:validation:Enum=json;clf;custom
	Format string `json:"format,omitempty"`

	// Template defines a custom log format template (for format=custom)
	// +optional
	Template string `json:"template,omitempty"`

	// Output defines where logs are written (stdout, file, both)
	// +optional
	// +kubebuilder:validation:Enum=stdout;file;both
	Output string `json:"output,omitempty"`

	// FilePath is the path to the log file
	// +optional
	FilePath string `json:"filePath,omitempty"`

	// MaxSize is the maximum size of a log file before rotation (e.g., "100Mi")
	// +optional
	MaxSize string `json:"maxSize,omitempty"`

	// MaxBackups is the maximum number of rotated log files to retain
	// +optional
	MaxBackups *int32 `json:"maxBackups,omitempty"`

	// FilterStatusCodes limits logging to specific HTTP status codes
	// +optional
	FilterStatusCodes []int32 `json:"filterStatusCodes,omitempty"`

	// SampleRate defines the percentage of requests to log (0-100)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	SampleRate *int32 `json:"sampleRate,omitempty"`
}

// FaultInjectionConfig configures fault injection for chaos engineering.
type FaultInjectionConfig struct {
	// DelayDuration is the fixed delay to inject (e.g. "500ms", "2s")
	// +optional
	DelayDuration *metav1.Duration `json:"delayDuration,omitempty"`

	// DelayPercent is the percentage of requests to delay (0-100)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	DelayPercent *int32 `json:"delayPercent,omitempty"`

	// AbortStatusCode is the HTTP status code for aborted requests
	// +optional
	// +kubebuilder:validation:Minimum=200
	// +kubebuilder:validation:Maximum=599
	AbortStatusCode *int32 `json:"abortStatusCode,omitempty"`

	// AbortPercent is the percentage of requests to abort (0-100)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	AbortPercent *int32 `json:"abortPercent,omitempty"`

	// HeaderActivation is an optional header that must be present to activate fault injection
	// +optional
	HeaderActivation string `json:"headerActivation,omitempty"`
}

// BodyTransformConfig configures JSON body transformation
type BodyTransformConfig struct {
	// Request transforms to apply to request bodies
	// +optional
	Request []TransformOperation `json:"request,omitempty"`

	// Response transforms to apply to response bodies
	// +optional
	Response []TransformOperation `json:"response,omitempty"`

	// MaxBodySize is the maximum body size for transformation in bytes (default 1MB)
	// +optional
	// +kubebuilder:default=1048576
	MaxBodySize *int64 `json:"maxBodySize,omitempty"`
}

// TransformOperation represents a single JSON Patch operation (RFC 6902)
type TransformOperation struct {
	// Op is the operation: add, remove, replace, move, copy
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=add;remove;replace;move;copy
	Op string `json:"op"`

	// Path is the JSON Pointer (RFC 6901) target path
	// +kubebuilder:validation:Required
	Path string `json:"path"`

	// Value is the JSON-encoded value for add/replace operations
	// +optional
	Value string `json:"value,omitempty"`

	// From is the source path for move/copy operations (JSON Pointer)
	// +optional
	From string `json:"from,omitempty"`
}
