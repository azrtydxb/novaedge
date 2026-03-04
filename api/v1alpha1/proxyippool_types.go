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

// IPAllocation tracks a single IP allocation from a pool
type IPAllocation struct {
	// Address is the allocated IP address in CIDR notation
	// +kubebuilder:validation:Required
	Address string `json:"address"`

	// VIPRef is the name of the ProxyVIP that holds this allocation
	// +kubebuilder:validation:Required
	VIPRef string `json:"vipRef"`

	// AllocatedAt is when this address was allocated
	// +optional
	AllocatedAt metav1.Time `json:"allocatedAt,omitempty"`
}

// ProxyIPPoolSpec defines the desired state of ProxyIPPool
type ProxyIPPoolSpec struct {
	// CIDRs defines IP address ranges in CIDR notation (e.g., "10.200.0.0/24")
	// +optional
	CIDRs []string `json:"cidrs,omitempty"`

	// Addresses is an explicit list of IP addresses (e.g., "10.200.0.10/32")
	// +optional
	Addresses []string `json:"addresses,omitempty"`

	// AutoAssign enables automatic IP allocation for VIPs referencing this pool
	// +optional
	// +kubebuilder:default=true
	AutoAssign bool `json:"autoAssign,omitempty"`
}

// ProxyIPPoolStatus defines the observed state of ProxyIPPool
type ProxyIPPoolStatus struct {
	// Allocated is the number of currently allocated addresses
	// +optional
	Allocated int32 `json:"allocated,omitempty"`

	// Available is the number of available addresses in the pool
	// +optional
	Available int32 `json:"available,omitempty"`

	// Allocations lists all current IP allocations from this pool
	// +optional
	Allocations []IPAllocation `json:"allocations,omitempty"`

	// Conditions represent the latest available observations of the pool's state
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
// +kubebuilder:printcolumn:name="CIDRs",type=string,JSONPath=`.spec.cidrs`
// +kubebuilder:printcolumn:name="Auto Assign",type=boolean,JSONPath=`.spec.autoAssign`
// +kubebuilder:printcolumn:name="Allocated",type=integer,JSONPath=`.status.allocated`
// +kubebuilder:printcolumn:name="Available",type=integer,JSONPath=`.status.available`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ProxyIPPool defines a pool of IP addresses for VIP allocation
type ProxyIPPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyIPPoolSpec   `json:"spec,omitempty"`
	Status ProxyIPPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyIPPoolList contains a list of ProxyIPPool
type ProxyIPPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyIPPool `json:"items"`
}
