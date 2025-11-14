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

// VIPMode defines the mode of VIP exposure
// +kubebuilder:validation:Enum=L2ARP;BGP;OSPF
type VIPMode string

const (
	// L2ARP mode uses ARP to announce the VIP (active-passive)
	VIPModeL2ARP VIPMode = "L2ARP"
	// VIPModeBGP mode uses BGP to announce the VIP (active-active ECMP)
	VIPModeBGP VIPMode = "BGP"
	// VIPModeOSPF mode uses OSPF to announce the VIP (active-active L3 routing)
	VIPModeOSPF VIPMode = "OSPF"
)

// HealthPolicy defines health requirements for VIP ownership
type HealthPolicy struct {
	// MinHealthyNodes is the minimum number of healthy nodes required
	// +kubebuilder:validation:Minimum=1
	// +optional
	MinHealthyNodes int32 `json:"minHealthyNodes,omitempty"`
}

// ProxyVIPSpec defines the desired state of ProxyVIP
type ProxyVIPSpec struct {
	// Address is the VIP as CIDR notation, usually /32 for a single IP
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}$`
	Address string `json:"address"`

	// Mode determines how the VIP is exposed (L2ARP, BGP, or OSPF)
	// +kubebuilder:validation:Required
	Mode VIPMode `json:"mode"`

	// Ports lists the ports to bind on hostNetwork
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Ports []int32 `json:"ports"`

	// NodeSelector selects which nodes can host this VIP
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// HealthPolicy defines node health requirements
	// +optional
	HealthPolicy *HealthPolicy `json:"healthPolicy,omitempty"`
}

// ProxyVIPStatus defines the observed state of ProxyVIP
type ProxyVIPStatus struct {
	// ActiveNode is the node currently owning the VIP (for L2ARP mode)
	// +optional
	ActiveNode string `json:"activeNode,omitempty"`

	// AnnouncingNodes lists nodes currently announcing this VIP (for BGP/OSPF mode)
	// +optional
	AnnouncingNodes []string `json:"announcingNodes,omitempty"`

	// Conditions represent the latest available observations of the VIP's state
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
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=`.spec.address`
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.mode`
// +kubebuilder:printcolumn:name="Active Node",type=string,JSONPath=`.status.activeNode`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProxyVIP describes the external IP and how NovaEdge exposes it through node agents
type ProxyVIP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyVIPSpec   `json:"spec,omitempty"`
	Status ProxyVIPStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyVIPList contains a list of ProxyVIP
type ProxyVIPList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyVIP `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProxyVIP{}, &ProxyVIPList{})
}
