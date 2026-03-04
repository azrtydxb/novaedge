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

// WANLinkRole defines the role of a WAN link in multi-link configurations
// +kubebuilder:validation:Enum=primary;backup;loadbalance
type WANLinkRole string

const (
	// WANLinkRolePrimary indicates the link is the preferred primary path.
	WANLinkRolePrimary WANLinkRole = "primary"
	// WANLinkRoleBackup indicates the link is used only when primary links are unavailable.
	WANLinkRoleBackup WANLinkRole = "backup"
	// WANLinkRoleLoadbalance indicates the link participates in active load balancing.
	WANLinkRoleLoadbalance WANLinkRole = "loadbalance"
)

// WANLinkSLA defines the SLA thresholds for a WAN link
type WANLinkSLA struct {
	// MaxLatency is the maximum acceptable one-way latency
	// +optional
	MaxLatency *metav1.Duration `json:"maxLatency,omitempty"`

	// MaxJitter is the maximum acceptable jitter
	// +optional
	MaxJitter *metav1.Duration `json:"maxJitter,omitempty"`

	// MaxPacketLoss is the maximum acceptable packet loss ratio (0.0-1.0)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	MaxPacketLoss *float64 `json:"maxPacketLoss,omitempty"`
}

// WANTunnelEndpoint defines the public endpoint for tunnel establishment
type WANTunnelEndpoint struct {
	// PublicIP is the publicly reachable IP address of the tunnel endpoint
	// +kubebuilder:validation:Required
	PublicIP string `json:"publicIP"`

	// Port is the UDP/TCP port for tunnel traffic
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}

// ProxyWANLinkSpec defines the desired state of ProxyWANLink
type ProxyWANLinkSpec struct {
	// Site is the site name this WAN link belongs to
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Site string `json:"site"`

	// Interface is the network interface name used for this WAN link
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Interface string `json:"interface"`

	// Provider is the ISP or WAN circuit provider name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Provider string `json:"provider"`

	// Bandwidth is the provisioned bandwidth of the link (e.g., "100Mbps", "1Gbps")
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Bandwidth string `json:"bandwidth"`

	// Cost is an administrative cost metric used for path selection (lower is preferred)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=100
	Cost int32 `json:"cost,omitempty"`

	// SLA defines the SLA thresholds for this link
	// +optional
	SLA *WANLinkSLA `json:"sla,omitempty"`

	// TunnelEndpoint is the public endpoint for tunnel establishment
	// +optional
	TunnelEndpoint *WANTunnelEndpoint `json:"tunnelEndpoint,omitempty"`

	// Role defines whether this link is primary, backup, or load-balanced
	// +optional
	// +kubebuilder:default="primary"
	Role WANLinkRole `json:"role,omitempty"`
}

// ProxyWANLinkStatus defines the observed state of ProxyWANLink
type ProxyWANLinkStatus struct {
	// Phase is the current lifecycle phase of the WAN link
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the WAN link's state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// CurrentLatency is the most recently measured latency in milliseconds
	// +optional
	CurrentLatency *float64 `json:"currentLatency,omitempty"`

	// CurrentPacketLoss is the most recently measured packet loss ratio (0.0-1.0)
	// +optional
	CurrentPacketLoss *float64 `json:"currentPacketLoss,omitempty"`

	// Healthy indicates whether the link currently meets its SLA thresholds
	// +optional
	Healthy bool `json:"healthy,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Site",type=string,JSONPath=`.spec.site`
// +kubebuilder:printcolumn:name="Role",type=string,JSONPath=`.spec.role`
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider`
// +kubebuilder:printcolumn:name="Healthy",type=boolean,JSONPath=`.status.healthy`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ProxyWANLink represents a WAN link for SD-WAN multi-link management.
// It tracks link properties, SLA thresholds, and observed quality metrics.
type ProxyWANLink struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyWANLinkSpec   `json:"spec,omitempty"`
	Status ProxyWANLinkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyWANLinkList contains a list of ProxyWANLink
type ProxyWANLinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyWANLink `json:"items"`
}

