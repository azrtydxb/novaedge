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

// WANStrategy defines the path selection strategy for SD-WAN traffic
// +kubebuilder:validation:Enum=lowest-latency;highest-bandwidth;most-reliable;lowest-cost
type WANStrategy string

const (
	// WANStrategyLowestLatency selects the link with the lowest measured latency.
	WANStrategyLowestLatency WANStrategy = "lowest-latency"
	// WANStrategyHighestBandwidth selects the link with the highest provisioned bandwidth.
	WANStrategyHighestBandwidth WANStrategy = "highest-bandwidth"
	// WANStrategyMostReliable selects the link with the lowest packet loss.
	WANStrategyMostReliable WANStrategy = "most-reliable"
	// WANStrategyLowestCost selects the link with the lowest administrative cost.
	WANStrategyLowestCost WANStrategy = "lowest-cost"
)

// WANPolicyMatch defines traffic matching criteria for a WAN policy
type WANPolicyMatch struct {
	// Hosts is a list of hostnames to match
	// +optional
	Hosts []string `json:"hosts,omitempty"`

	// Paths is a list of URL path prefixes to match
	// +optional
	Paths []string `json:"paths,omitempty"`

	// Headers is a map of header name to value for matching
	// +optional
	Headers map[string]string `json:"headers,omitempty"`
}

// WANPathSelection defines path selection behavior for matched traffic
type WANPathSelection struct {
	// Strategy is the algorithm used to select the optimal WAN path
	// +kubebuilder:validation:Required
	Strategy WANStrategy `json:"strategy"`

	// Failover enables automatic failover to another link when the selected link fails SLA
	// +optional
	// +kubebuilder:default=true
	Failover bool `json:"failover,omitempty"`

	// DSCPClass is the DSCP marking to apply to traffic matching this policy (e.g., "EF", "AF41", "CS6")
	// +optional
	DSCPClass string `json:"dscpClass,omitempty"`
}

// ProxyWANPolicySpec defines the desired state of ProxyWANPolicy
type ProxyWANPolicySpec struct {
	// Match defines the traffic matching criteria
	// +optional
	Match WANPolicyMatch `json:"match,omitempty"`

	// PathSelection defines how the optimal WAN path is selected for matched traffic
	// +kubebuilder:validation:Required
	PathSelection WANPathSelection `json:"pathSelection"`
}

// ProxyWANPolicyStatus defines the observed state of ProxyWANPolicy
type ProxyWANPolicyStatus struct {
	// Phase is the current lifecycle phase of the WAN policy
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the policy's state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// SelectionCount is the total number of path selections made using this policy
	// +optional
	SelectionCount int64 `json:"selectionCount,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Strategy",type=string,JSONPath=`.spec.pathSelection.strategy`
// +kubebuilder:printcolumn:name="DSCP",type=string,JSONPath=`.spec.pathSelection.dscpClass`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ProxyWANPolicy defines application-aware path selection rules for SD-WAN traffic.
// It matches traffic by host, path, or headers and routes it through the optimal WAN link.
type ProxyWANPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyWANPolicySpec   `json:"spec,omitempty"`
	Status ProxyWANPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyWANPolicyList contains a list of ProxyWANPolicy
type ProxyWANPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyWANPolicy `json:"items"`
}

