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
	// VIPModeL2ARP uses ARP to announce the VIP (active-passive).
	VIPModeL2ARP VIPMode = "L2ARP"
	// VIPModeBGP mode uses BGP to announce the VIP (active-active ECMP)
	VIPModeBGP VIPMode = "BGP"
	// VIPModeOSPF mode uses OSPF to announce the VIP (active-active L3 routing)
	VIPModeOSPF VIPMode = "OSPF"
)

// AddressFamily defines the IP address family for a VIP
// +kubebuilder:validation:Enum=ipv4;ipv6;dual
type AddressFamily string

const (
	// AddressFamilyIPv4 is IPv4 only
	AddressFamilyIPv4 AddressFamily = "ipv4"
	// AddressFamilyIPv6 is IPv6 only
	AddressFamilyIPv6 AddressFamily = "ipv6"
	// AddressFamilyDual is dual-stack (both IPv4 and IPv6)
	AddressFamilyDual AddressFamily = "dual"
)

// HealthPolicy defines health requirements for VIP ownership
type HealthPolicy struct {
	// MinHealthyNodes is the minimum number of healthy nodes required
	// +kubebuilder:validation:Minimum=1
	// +optional
	MinHealthyNodes int32 `json:"minHealthyNodes,omitempty"`
}

// BFDConfig defines BFD (Bidirectional Forwarding Detection) settings
type BFDConfig struct {
	// Enabled enables BFD for this VIP's BGP sessions
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// DetectMultiplier is the detection time multiplier (default 3)
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=255
	// +kubebuilder:default=3
	DetectMultiplier int32 `json:"detectMultiplier,omitempty"`

	// DesiredMinTxInterval is the desired minimum transmit interval (e.g., "300ms")
	// +optional
	// +kubebuilder:default="300ms"
	DesiredMinTxInterval string `json:"desiredMinTxInterval,omitempty"`

	// RequiredMinRxInterval is the required minimum receive interval (e.g., "300ms")
	// +optional
	// +kubebuilder:default="300ms"
	RequiredMinRxInterval string `json:"requiredMinRxInterval,omitempty"`

	// EchoMode enables BFD echo mode for sub-millisecond detection
	// +optional
	EchoMode bool `json:"echoMode,omitempty"`
}

// BGPConfig defines BGP configuration for BGP mode VIPs
type BGPConfig struct {
	// LocalAS is the local BGP AS number
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	LocalAS uint32 `json:"localAS"`

	// RouterID is the BGP router ID (usually an IP address)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}$`
	RouterID string `json:"routerID"`

	// Peers lists BGP peer configurations
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Peers []BGPPeer `json:"peers"`

	// Communities are BGP communities to attach to announced routes
	// +optional
	Communities []string `json:"communities,omitempty"`

	// LocalPreference for iBGP routes
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	LocalPreference *uint32 `json:"localPreference,omitempty"`
}

// BGPPeer defines a BGP peer configuration
type BGPPeer struct {
	// Address is the peer IP address
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}$`
	Address string `json:"address"`

	// AS is the peer AS number
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	AS uint32 `json:"as"`

	// Port is the BGP port (default 179)
	// +optional
	// +kubebuilder:default=179
	Port uint16 `json:"port,omitempty"`
}

// OSPFConfig defines OSPF configuration for OSPF mode VIPs
type OSPFConfig struct {
	// RouterID is the OSPF router ID
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}$`
	RouterID string `json:"routerID"`

	// AreaID is the OSPF area ID
	// +kubebuilder:validation:Required
	AreaID uint32 `json:"areaID"`

	// Cost is the OSPF route cost metric (default 10)
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Cost uint32 `json:"cost,omitempty"`

	// HelloInterval is the OSPF hello interval in seconds (default 10)
	// +optional
	// +kubebuilder:default=10
	HelloInterval uint32 `json:"helloInterval,omitempty"`

	// DeadInterval is the OSPF dead interval in seconds (default 40)
	// +optional
	// +kubebuilder:default=40
	DeadInterval uint32 `json:"deadInterval,omitempty"`

	// AuthType is the authentication type (none, simple, md5)
	// +optional
	// +kubebuilder:validation:Enum=none;simple;md5
	// +kubebuilder:default="none"
	AuthType string `json:"authType,omitempty"`

	// AuthKey is the authentication key
	// +optional
	AuthKey string `json:"authKey,omitempty"`

	// GracefulRestart enables OSPF graceful restart
	// +optional
	GracefulRestart bool `json:"gracefulRestart,omitempty"`
}

// ProxyVIPSpec defines the desired state of ProxyVIP
type ProxyVIPSpec struct {
	// Address is the VIP as CIDR notation, usually /32 for a single IP (IPv4 or IPv6)
	// +optional
	Address string `json:"address,omitempty"`

	// IPv6Address is the IPv6 VIP address in CIDR notation (for dual-stack mode)
	// +optional
	IPv6Address string `json:"ipv6Address,omitempty"`

	// Mode determines how the VIP is exposed (L2ARP, BGP, or OSPF)
	// +kubebuilder:validation:Required
	Mode VIPMode `json:"mode"`

	// AddressFamily specifies the IP address family (ipv4, ipv6, or dual)
	// +optional
	// +kubebuilder:default="ipv4"
	AddressFamily AddressFamily `json:"addressFamily,omitempty"`

	// Ports lists the ports to bind on hostNetwork
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Ports []int32 `json:"ports"`

	// NodeSelector selects which nodes can host this VIP
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// Tolerations lists node label keys from the cluster-wide VipNodeExclusions that
	// this VIP is permitted to run on. Works analogously to Kubernetes pod tolerations:
	// a VIP with a matching toleration may be scheduled on otherwise-excluded nodes.
	// +optional
	Tolerations []string `json:"tolerations,omitempty"`

	// HealthPolicy defines node health requirements
	// +optional
	HealthPolicy *HealthPolicy `json:"healthPolicy,omitempty"`

	// BGPConfig defines BGP configuration for BGP mode VIPs
	// Required when Mode is BGP
	// +optional
	BGPConfig *BGPConfig `json:"bgpConfig,omitempty"`

	// OSPFConfig defines OSPF configuration for OSPF mode VIPs
	// Required when Mode is OSPF
	// +optional
	OSPFConfig *OSPFConfig `json:"ospfConfig,omitempty"`

	// BFD defines BFD configuration for fast failure detection
	// Used with BGP mode VIPs
	// +optional
	BFD *BFDConfig `json:"bfd,omitempty"`

	// PoolRef references a ProxyIPPool for automatic address allocation
	// When set, Address may be left empty and will be allocated from the pool
	// +optional
	PoolRef *LocalObjectReference `json:"poolRef,omitempty"`
}

// ProxyVIPStatus defines the observed state of ProxyVIP
type ProxyVIPStatus struct {
	// ActiveNode is the node currently owning the VIP (for L2ARP mode)
	// +optional
	ActiveNode string `json:"activeNode,omitempty"`

	// AnnouncingNodes lists nodes currently announcing this VIP (for BGP/OSPF mode)
	// +optional
	AnnouncingNodes []string `json:"announcingNodes,omitempty"`

	// AllocatedAddress is the address allocated from the IP pool (if using pool)
	// +optional
	AllocatedAddress string `json:"allocatedAddress,omitempty"`

	// AllocatedIPv6Address is the IPv6 address allocated from the IP pool (if dual-stack)
	// +optional
	AllocatedIPv6Address string `json:"allocatedIPv6Address,omitempty"`

	// BFDSessionState is the current BFD session state (if BFD is enabled)
	// +optional
	BFDSessionState string `json:"bfdSessionState,omitempty"`

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
// +kubebuilder:printcolumn:name="Family",type=string,JSONPath=`.spec.addressFamily`
// +kubebuilder:printcolumn:name="BFD",type=boolean,JSONPath=`.spec.bfd.enabled`
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
