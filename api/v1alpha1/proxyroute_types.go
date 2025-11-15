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

// HTTPRouteMatch defines a match for an HTTP request
type HTTPRouteMatch struct {
	// Path specifies a HTTP request path matcher
	// +optional
	Path *HTTPPathMatch `json:"path,omitempty"`

	// Headers specifies HTTP request header matchers
	// +optional
	// +kubebuilder:validation:MaxItems=16
	Headers []HTTPHeaderMatch `json:"headers,omitempty"`

	// Method matches the HTTP method
	// +optional
	// +kubebuilder:validation:Enum=GET;HEAD;POST;PUT;PATCH;DELETE;CONNECT;OPTIONS;TRACE
	Method *string `json:"method,omitempty"`
}

// HTTPRouteFilterType identifies a type of filter
// +kubebuilder:validation:Enum=AddHeader;RemoveHeader;RequestRedirect;URLRewrite
type HTTPRouteFilterType string

const (
	// HTTPRouteFilterAddHeader adds HTTP headers
	HTTPRouteFilterAddHeader HTTPRouteFilterType = "AddHeader"
	// HTTPRouteFilterRemoveHeader removes HTTP headers
	HTTPRouteFilterRemoveHeader HTTPRouteFilterType = "RemoveHeader"
	// HTTPRouteFilterRequestRedirect redirects the request
	HTTPRouteFilterRequestRedirect HTTPRouteFilterType = "RequestRedirect"
	// HTTPRouteFilterURLRewrite rewrites the request URL
	HTTPRouteFilterURLRewrite HTTPRouteFilterType = "URLRewrite"
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
