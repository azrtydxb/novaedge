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
// +kubebuilder:validation:Enum=RoundRobin;P2C;EWMA;RingHash;Maglev
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
)

// ServiceReference references a Kubernetes Service
type ServiceReference struct {
	// Name is the name of the Service
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the Service (defaults to backend namespace)
	// +optional
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
	MaxConnections *int32 `json:"maxConnections,omitempty"`

	// MaxPendingRequests is the maximum number of pending requests
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxPendingRequests *int32 `json:"maxPendingRequests,omitempty"`

	// MaxRequests is the maximum number of parallel requests
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxRequests *int32 `json:"maxRequests,omitempty"`

	// MaxRetries is the maximum number of parallel retries
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxRetries *int32 `json:"maxRetries,omitempty"`
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
}

// BackendTLSConfig defines TLS settings for backend connections
type BackendTLSConfig struct {
	// Enabled indicates whether to use TLS
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// InsecureSkipVerify skips certificate validation (not recommended for production)
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// CACertSecretRef references a Secret containing CA certificates
	// +optional
	CACertSecretRef *string `json:"caCertSecretRef,omitempty"`
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
