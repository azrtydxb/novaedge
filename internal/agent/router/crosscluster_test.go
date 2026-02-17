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

package router

import (
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewCrossClusterTunnelRegistry(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()
	if reg == nil {
		t.Fatal("NewCrossClusterTunnelRegistry returned nil")
	}
	if reg.gateways == nil {
		t.Fatal("gateways map should be initialized")
	}
}

func TestIsRemoteEndpoint(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	tests := []struct {
		name     string
		endpoint *pb.Endpoint
		want     bool
	}{
		{
			name:     "nil endpoint",
			endpoint: nil,
			want:     false,
		},
		{
			name:     "endpoint with no labels",
			endpoint: &pb.Endpoint{Address: "10.0.0.1", Port: 80},
			want:     false,
		},
		{
			name: "endpoint with empty labels",
			endpoint: &pb.Endpoint{
				Address: "10.0.0.1",
				Port:    80,
				Labels:  map[string]string{},
			},
			want: false,
		},
		{
			name: "endpoint with remote=false",
			endpoint: &pb.Endpoint{
				Address: "10.0.0.1",
				Port:    80,
				Labels:  map[string]string{"novaedge.io/remote": "false"},
			},
			want: false,
		},
		{
			name: "endpoint with remote=true",
			endpoint: &pb.Endpoint{
				Address: "10.0.0.1",
				Port:    80,
				Labels:  map[string]string{"novaedge.io/remote": "true"},
			},
			want: true,
		},
		{
			name: "endpoint with remote=true and cluster label",
			endpoint: &pb.Endpoint{
				Address: "10.0.0.1",
				Port:    80,
				Labels: map[string]string{
					"novaedge.io/remote":  "true",
					"novaedge.io/cluster": "us-west-2",
				},
			},
			want: true,
		},
		{
			name: "endpoint with unrelated labels only",
			endpoint: &pb.Endpoint{
				Address: "10.0.0.1",
				Port:    80,
				Labels:  map[string]string{"app": "nginx"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reg.IsRemoteEndpoint(tt.endpoint)
			if got != tt.want {
				t.Errorf("IsRemoteEndpoint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetClusterForEndpoint(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	tests := []struct {
		name     string
		endpoint *pb.Endpoint
		want     string
	}{
		{
			name:     "nil endpoint",
			endpoint: nil,
			want:     "",
		},
		{
			name:     "endpoint with no labels",
			endpoint: &pb.Endpoint{Address: "10.0.0.1", Port: 80},
			want:     "",
		},
		{
			name: "endpoint with cluster label",
			endpoint: &pb.Endpoint{
				Address: "10.0.0.1",
				Port:    80,
				Labels: map[string]string{
					"novaedge.io/remote":  "true",
					"novaedge.io/cluster": "eu-central-1",
				},
			},
			want: "eu-central-1",
		},
		{
			name: "endpoint with empty cluster label",
			endpoint: &pb.Endpoint{
				Address: "10.0.0.1",
				Port:    80,
				Labels:  map[string]string{"novaedge.io/cluster": ""},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reg.GetClusterForEndpoint(tt.endpoint)
			if got != tt.want {
				t.Errorf("GetClusterForEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpdateGatewaysAndGetGateway(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	// No gateways registered: should fail
	_, err := reg.GetGateway("cluster-a")
	if err == nil {
		t.Fatal("Expected error for unregistered cluster, got nil")
	}

	// Register gateways for cluster-a
	gateways := []GatewayAgent{
		{Address: "10.0.1.1:15002", Cluster: "cluster-a", Region: "us-east-1", Zone: "us-east-1a", Healthy: false},
		{Address: "10.0.1.2:15002", Cluster: "cluster-a", Region: "us-east-1", Zone: "us-east-1b", Healthy: true},
		{Address: "10.0.1.3:15002", Cluster: "cluster-a", Region: "us-east-1", Zone: "us-east-1c", Healthy: true},
	}
	reg.UpdateGateways("cluster-a", gateways)

	// Should return first healthy gateway
	gw, err := reg.GetGateway("cluster-a")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if gw.Address != "10.0.1.2:15002" {
		t.Errorf("Expected first healthy gateway 10.0.1.2:15002, got %s", gw.Address)
	}

	// All unhealthy: should fail
	unhealthy := []GatewayAgent{
		{Address: "10.0.1.1:15002", Cluster: "cluster-a", Healthy: false},
		{Address: "10.0.1.2:15002", Cluster: "cluster-a", Healthy: false},
	}
	reg.UpdateGateways("cluster-a", unhealthy)

	_, err = reg.GetGateway("cluster-a")
	if err == nil {
		t.Fatal("Expected error when all gateways unhealthy, got nil")
	}
}

func TestUpdateGatewaysDefensiveCopy(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	gateways := []GatewayAgent{
		{Address: "10.0.1.1:15002", Cluster: "cluster-a", Healthy: true},
	}
	reg.UpdateGateways("cluster-a", gateways)

	// Mutate the original slice: should not affect the registry
	gateways[0].Healthy = false

	gw, err := reg.GetGateway("cluster-a")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !gw.Healthy {
		t.Error("Mutating original slice should not affect stored gateways")
	}
}

func TestRemoveCluster(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	gateways := []GatewayAgent{
		{Address: "10.0.1.1:15002", Cluster: "cluster-a", Healthy: true},
	}
	reg.UpdateGateways("cluster-a", gateways)

	// Should be available
	_, err := reg.GetGateway("cluster-a")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Remove and verify it's gone
	reg.RemoveCluster("cluster-a")
	_, err = reg.GetGateway("cluster-a")
	if err == nil {
		t.Fatal("Expected error after removing cluster, got nil")
	}

	// Removing a non-existent cluster should not panic
	reg.RemoveCluster("cluster-nonexistent")
}

func TestGetGatewayMultipleClusters(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	reg.UpdateGateways("cluster-a", []GatewayAgent{
		{Address: "10.0.1.1:15002", Cluster: "cluster-a", Healthy: true},
	})
	reg.UpdateGateways("cluster-b", []GatewayAgent{
		{Address: "10.0.2.1:15002", Cluster: "cluster-b", Healthy: true},
	})

	gwA, err := reg.GetGateway("cluster-a")
	if err != nil {
		t.Fatalf("Unexpected error for cluster-a: %v", err)
	}
	if gwA.Address != "10.0.1.1:15002" {
		t.Errorf("Wrong gateway for cluster-a: %s", gwA.Address)
	}

	gwB, err := reg.GetGateway("cluster-b")
	if err != nil {
		t.Fatalf("Unexpected error for cluster-b: %v", err)
	}
	if gwB.Address != "10.0.2.1:15002" {
		t.Errorf("Wrong gateway for cluster-b: %s", gwB.Address)
	}
}

func TestGetGatewayEmptySlice(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()
	reg.UpdateGateways("cluster-a", []GatewayAgent{})

	_, err := reg.GetGateway("cluster-a")
	if err == nil {
		t.Fatal("Expected error for cluster with empty gateways, got nil")
	}
}
