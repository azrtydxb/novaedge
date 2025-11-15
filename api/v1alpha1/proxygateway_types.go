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
// +kubebuilder:validation:Enum=HTTP;HTTPS;TCP;TLS
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
)

// TLSConfig defines TLS configuration for a listener
type TLSConfig struct {
	// SecretRef references a Kubernetes Secret containing TLS certificate and key
	// +kubebuilder:validation:Required
	SecretRef corev1.SecretReference `json:"secretRef"`

	// MinVersion is the minimum TLS version (default: TLS 1.2)
	// +optional
	// +kubebuilder:validation:Enum=TLS1.2;TLS1.3
	MinVersion string `json:"minVersion,omitempty"`

	// CipherSuites is a list of allowed cipher suites
	// +optional
	CipherSuites []string `json:"cipherSuites,omitempty"`
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
}

// ProxyGatewaySpec defines the desired state of ProxyGateway
type ProxyGatewaySpec struct {
	// VIPRef references the ProxyVIP to use for this gateway
	// +kubebuilder:validation:Required
	VIPRef string `json:"vipRef"`

	// IngressClassName is the ingress class name for Ingress resource compatibility
	// +optional
	IngressClassName string `json:"ingressClassName,omitempty"`

	// Listeners define the ports and protocols this gateway accepts
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Listeners []Listener `json:"listeners"`
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
