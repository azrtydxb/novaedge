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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestObjectReference(t *testing.T) {
	t.Run("create object reference", func(t *testing.T) {
		ref := ObjectReference{
			Name:      "test-name",
			Namespace: "test-namespace",
			Group:     "test-group",
			Kind:      "test-kind",
		}
		assert.Equal(t, "test-name", ref.Name)
		assert.Equal(t, "test-namespace", ref.Namespace)
		assert.Equal(t, "test-group", ref.Group)
		assert.Equal(t, "test-kind", ref.Kind)
	})
}

func TestLocalObjectReference(t *testing.T) {
	t.Run("create local object reference", func(t *testing.T) {
		ref := LocalObjectReference{
			Name: "test-name",
		}
		assert.Equal(t, "test-name", ref.Name)
	})
}

func TestLoadBalancingPolicy(t *testing.T) {
	tests := []struct {
		name     string
		policy   LoadBalancingPolicy
		expected string
	}{
		{"RoundRobin", LBPolicyRoundRobin, "RoundRobin"},
		{"P2C", LBPolicyP2C, "P2C"},
		{"EWMA", LBPolicyEWMA, "EWMA"},
		{"RingHash", LBPolicyRingHash, "RingHash"},
		{"Maglev", LBPolicyMaglev, "Maglev"},
		{"LeastConn", LBPolicyLeastConn, "LeastConn"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, LoadBalancingPolicy(tt.expected), tt.policy)
		})
	}
}

func TestServiceReference(t *testing.T) {
	namespace := "test-namespace"
	ref := ServiceReference{
		Name:      "test-service",
		Namespace: &namespace,
		Port:      8080,
	}
	assert.Equal(t, "test-service", ref.Name)
	assert.Equal(t, "test-namespace", *ref.Namespace)
	assert.Equal(t, int32(8080), ref.Port)
}

func TestCircuitBreaker(t *testing.T) {
	maxConn := int32(100)
	maxPending := int32(50)
	cb := CircuitBreaker{
		MaxConnections:     &maxConn,
		MaxPendingRequests: &maxPending,
	}
	assert.Equal(t, int32(100), *cb.MaxConnections)
	assert.Equal(t, int32(50), *cb.MaxPendingRequests)
}

func TestPathMatchType(t *testing.T) {
	tests := []struct {
		name     string
		match    PathMatchType
		expected string
	}{
		{"Exact", PathMatchExact, "Exact"},
		{"PathPrefix", PathMatchPathPrefix, "PathPrefix"},
		{"RegularExpression", PathMatchRegularExpression, "RegularExpression"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, PathMatchType(tt.expected), tt.match)
		})
	}
}

func TestHeaderMatchType(t *testing.T) {
	tests := []struct {
		name     string
		match    HeaderMatchType
		expected string
	}{
		{"Exact", HeaderMatchExact, "Exact"},
		{"RegularExpression", HeaderMatchRegularExpression, "RegularExpression"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, HeaderMatchType(tt.expected), tt.match)
		})
	}
}

func TestVIPMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     VIPMode
		expected string
	}{
		{"L2ARP", VIPModeL2ARP, "L2ARP"},
		{"BGP", VIPModeBGP, "BGP"},
		{"OSPF", VIPModeOSPF, "OSPF"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, VIPMode(tt.expected), tt.mode)
		})
	}
}

func TestAddressFamily(t *testing.T) {
	tests := []struct {
		name     string
		family   AddressFamily
		expected string
	}{
		{"IPv4", AddressFamilyIPv4, "ipv4"},
		{"IPv6", AddressFamilyIPv6, "ipv6"},
		{"Dual", AddressFamilyDual, "dual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, AddressFamily(tt.expected), tt.family)
		})
	}
}

func TestConflictResolutionStrategy(t *testing.T) {
	tests := []struct {
		name     string
		strategy ConflictResolutionStrategy
		expected string
	}{
		{"LastWriterWins", ConflictResolutionLastWriterWins, "LastWriterWins"},
		{"Merge", ConflictResolutionMerge, "Merge"},
		{"Manual", ConflictResolutionManual, "Manual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, ConflictResolutionStrategy(tt.expected), tt.strategy)
		})
	}
}

func TestFederationMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     FederationMode
		expected string
	}{
		{"HubSpoke", FederationModeHubSpoke, "hub-spoke"},
		{"Mesh", FederationModeMesh, "mesh"},
		{"Unified", FederationModeUnified, "unified"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, FederationMode(tt.expected), tt.mode)
		})
	}
}
