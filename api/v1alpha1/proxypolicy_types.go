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

// PolicyType defines the type of policy
// +kubebuilder:validation:Enum=RateLimit;JWT;IPAllowList;IPDenyList;CORS
type PolicyType string

const (
	// PolicyTypeRateLimit applies rate limiting
	PolicyTypeRateLimit PolicyType = "RateLimit"
	// PolicyTypeJWT applies JWT authentication
	PolicyTypeJWT PolicyType = "JWT"
	// PolicyTypeIPAllowList allows only specific IPs
	PolicyTypeIPAllowList PolicyType = "IPAllowList"
	// PolicyTypeIPDenyList denies specific IPs
	PolicyTypeIPDenyList PolicyType = "IPDenyList"
	// PolicyTypeCORS applies CORS headers
	PolicyTypeCORS PolicyType = "CORS"
)

// RateLimitConfig defines rate limiting configuration
type RateLimitConfig struct {
	// RequestsPerSecond is the maximum number of requests per second
	// +kubebuilder:validation:Minimum=1
	RequestsPerSecond int32 `json:"requestsPerSecond"`

	// Burst is the maximum burst size
	// +optional
	// +kubebuilder:validation:Minimum=1
	Burst *int32 `json:"burst,omitempty"`

	// Key determines what to rate limit by (e.g., source IP, header value)
	// +optional
	// +kubebuilder:default="source-ip"
	Key string `json:"key,omitempty"`
}

// JWTConfig defines JWT authentication configuration
type JWTConfig struct {
	// Issuer is the expected JWT issuer
	// +kubebuilder:validation:Required
	Issuer string `json:"issuer"`

	// Audience is the expected JWT audience
	// +optional
	Audience []string `json:"audience,omitempty"`

	// JWKSUri is the URL to fetch JWKS for verification
	// +kubebuilder:validation:Required
	JWKSUri string `json:"jwksUri"`

	// HeaderName is the header containing the JWT token
	// +optional
	// +kubebuilder:default="Authorization"
	HeaderName string `json:"headerName,omitempty"`

	// HeaderPrefix is the prefix before the token in the header
	// +optional
	// +kubebuilder:default="Bearer "
	HeaderPrefix string `json:"headerPrefix,omitempty"`
}

// IPListConfig defines IP allow/deny list configuration
type IPListConfig struct {
	// CIDRs is a list of CIDR blocks
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	CIDRs []string `json:"cidrs"`

	// SourceHeader specifies an HTTP header to extract the client IP from
	// (e.g., X-Forwarded-For, X-Real-IP)
	// +optional
	SourceHeader *string `json:"sourceHeader,omitempty"`
}

// CORSConfig defines CORS policy configuration
type CORSConfig struct {
	// AllowOrigins is a list of allowed origins
	// +optional
	AllowOrigins []string `json:"allowOrigins,omitempty"`

	// AllowMethods is a list of allowed HTTP methods
	// +optional
	AllowMethods []string `json:"allowMethods,omitempty"`

	// AllowHeaders is a list of allowed headers
	// +optional
	AllowHeaders []string `json:"allowHeaders,omitempty"`

	// ExposeHeaders is a list of headers to expose
	// +optional
	ExposeHeaders []string `json:"exposeHeaders,omitempty"`

	// MaxAge is how long the response to a preflight request can be cached
	// +optional
	MaxAge *metav1.Duration `json:"maxAge,omitempty"`

	// AllowCredentials indicates whether credentials are allowed
	// +optional
	AllowCredentials bool `json:"allowCredentials,omitempty"`
}

// TargetRef identifies the resource(s) this policy applies to
type TargetRef struct {
	// Kind is the kind of resource (e.g., ProxyGateway, ProxyRoute)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=ProxyGateway;ProxyRoute;ProxyBackend
	Kind string `json:"kind"`

	// Name is the name of the resource
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the resource (defaults to policy namespace)
	// +optional
	Namespace *string `json:"namespace,omitempty"`
}

// ProxyPolicySpec defines the desired state of ProxyPolicy
type ProxyPolicySpec struct {
	// Type is the type of policy
	// +kubebuilder:validation:Required
	Type PolicyType `json:"type"`

	// TargetRef identifies the resource this policy applies to
	// +kubebuilder:validation:Required
	TargetRef TargetRef `json:"targetRef"`

	// RateLimit configuration (for RateLimit type)
	// +optional
	RateLimit *RateLimitConfig `json:"rateLimit,omitempty"`

	// JWT configuration (for JWT type)
	// +optional
	JWT *JWTConfig `json:"jwt,omitempty"`

	// IPList configuration (for IPAllowList/IPDenyList types)
	// +optional
	IPList *IPListConfig `json:"ipList,omitempty"`

	// CORS configuration (for CORS type)
	// +optional
	CORS *CORSConfig `json:"cors,omitempty"`
}

// ProxyPolicyStatus defines the observed state of ProxyPolicy
type ProxyPolicyStatus struct {
	// Conditions represent the latest available observations of the policy's state
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
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Target Kind",type=string,JSONPath=`.spec.targetRef.kind`
// +kubebuilder:printcolumn:name="Target Name",type=string,JSONPath=`.spec.targetRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProxyPolicy defines authentication, rate-limiting, and other policies
type ProxyPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyPolicySpec   `json:"spec,omitempty"`
	Status ProxyPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyPolicyList contains a list of ProxyPolicy
type ProxyPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProxyPolicy{}, &ProxyPolicyList{})
}
