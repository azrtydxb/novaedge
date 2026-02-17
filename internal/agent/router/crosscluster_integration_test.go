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

// TestCrossClusterIsRemoteEndpointIdentification verifies that the tunnel
// registry correctly distinguishes between local and remote endpoints based
// on the novaedge.io/remote label.
func TestCrossClusterIsRemoteEndpointIdentification(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	tests := []struct {
		name     string
		endpoint *pb.Endpoint
		isRemote bool
	}{
		{
			name: "local endpoint without any labels",
			endpoint: &pb.Endpoint{
				Address: "10.0.0.1",
				Port:    80,
			},
			isRemote: false,
		},
		{
			name: "local endpoint with unrelated labels",
			endpoint: &pb.Endpoint{
				Address: "10.0.0.2",
				Port:    80,
				Labels:  map[string]string{"app": "web"},
			},
			isRemote: false,
		},
		{
			name: "remote endpoint from us-west cluster",
			endpoint: &pb.Endpoint{
				Address: "10.1.0.1",
				Port:    8080,
				Labels: map[string]string{
					"novaedge.io/remote":  "true",
					"novaedge.io/cluster": "us-west-2",
					"novaedge.io/region":  "us-west-2",
					"novaedge.io/zone":    "us-west-2a",
				},
			},
			isRemote: true,
		},
		{
			name: "remote endpoint from eu-central cluster",
			endpoint: &pb.Endpoint{
				Address: "172.16.0.5",
				Port:    9090,
				Labels: map[string]string{
					"novaedge.io/remote":  "true",
					"novaedge.io/cluster": "eu-central-1",
				},
			},
			isRemote: true,
		},
		{
			name: "endpoint with remote label set to false",
			endpoint: &pb.Endpoint{
				Address: "10.0.0.3",
				Port:    80,
				Labels:  map[string]string{"novaedge.io/remote": "false"},
			},
			isRemote: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reg.IsRemoteEndpoint(tt.endpoint)
			if got != tt.isRemote {
				t.Errorf("IsRemoteEndpoint() = %v, want %v", got, tt.isRemote)
			}
		})
	}
}

// TestCrossClusterGetGatewayReturnsCorrectGateway verifies that the tunnel
// registry returns the correct healthy gateway for a given cluster name.
func TestCrossClusterGetGatewayReturnsCorrectGateway(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	// Register gateways for two clusters
	reg.UpdateGateways("us-west-2", []GatewayAgent{
		{Address: "gw1.us-west.example.com:15002", Cluster: "us-west-2", Region: "us-west-2", Zone: "us-west-2a", Healthy: true},
		{Address: "gw2.us-west.example.com:15002", Cluster: "us-west-2", Region: "us-west-2", Zone: "us-west-2b", Healthy: true},
	})
	reg.UpdateGateways("eu-central-1", []GatewayAgent{
		{Address: "gw1.eu.example.com:15002", Cluster: "eu-central-1", Region: "eu-central-1", Zone: "eu-central-1a", Healthy: false},
		{Address: "gw2.eu.example.com:15002", Cluster: "eu-central-1", Region: "eu-central-1", Zone: "eu-central-1b", Healthy: true},
	})

	// Test getting gateway for us-west-2
	gw, err := reg.GetGateway("us-west-2")
	if err != nil {
		t.Fatalf("GetGateway(us-west-2) error: %v", err)
	}
	if gw.Address != "gw1.us-west.example.com:15002" {
		t.Errorf("expected first healthy gateway, got %q", gw.Address)
	}
	if gw.Cluster != "us-west-2" {
		t.Errorf("gateway Cluster = %q, want us-west-2", gw.Cluster)
	}

	// Test getting gateway for eu-central-1: first is unhealthy, second is healthy
	gw, err = reg.GetGateway("eu-central-1")
	if err != nil {
		t.Fatalf("GetGateway(eu-central-1) error: %v", err)
	}
	if gw.Address != "gw2.eu.example.com:15002" {
		t.Errorf("expected second gateway (first is unhealthy), got %q", gw.Address)
	}

	// Test getting gateway for a cluster that does not exist
	_, err = reg.GetGateway("ap-southeast-1")
	if err == nil {
		t.Error("expected error for unregistered cluster, got nil")
	}
}

// TestCrossClusterEndToEndForwardingPath verifies the full forwarding path:
// an endpoint is identified as remote, its cluster label is extracted, and the
// registry returns the correct gateway for forwarding.
func TestCrossClusterEndToEndForwardingPath(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	// Register gateways
	reg.UpdateGateways("cluster-b", []GatewayAgent{
		{Address: "10.0.5.1:15002", Cluster: "cluster-b", Region: "us-east-1", Zone: "us-east-1a", Healthy: true},
	})

	// Simulate an endpoint that came from federation merging (with remote labels)
	remoteEndpoint := &pb.Endpoint{
		Address: "10.1.0.50",
		Port:    8080,
		Ready:   true,
		Labels: map[string]string{
			"novaedge.io/remote":  "true",
			"novaedge.io/cluster": "cluster-b",
			"novaedge.io/region":  "us-east-1",
			"novaedge.io/zone":    "us-east-1a",
		},
	}

	// Step 1: Identify as remote
	if !reg.IsRemoteEndpoint(remoteEndpoint) {
		t.Fatal("endpoint should be identified as remote")
	}

	// Step 2: Extract cluster name
	clusterName := reg.GetClusterForEndpoint(remoteEndpoint)
	if clusterName != "cluster-b" {
		t.Fatalf("expected cluster cluster-b, got %q", clusterName)
	}

	// Step 3: Get gateway for forwarding
	gw, err := reg.GetGateway(clusterName)
	if err != nil {
		t.Fatalf("GetGateway(%q) error: %v", clusterName, err)
	}
	if gw.Address != "10.0.5.1:15002" {
		t.Errorf("expected gateway 10.0.5.1:15002, got %q", gw.Address)
	}

	// Step 4: Verify the backend address that would be used in the tunnel
	expectedBackend := formatEndpointKey(remoteEndpoint.Address, remoteEndpoint.Port)
	if expectedBackend != "10.1.0.50:8080" {
		t.Errorf("expected backend address 10.1.0.50:8080, got %q", expectedBackend)
	}
}

// TestCrossClusterLocalEndpointBypass verifies that local endpoints are
// not treated as remote and the tunnel path is not triggered.
func TestCrossClusterLocalEndpointBypass(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	localEndpoint := &pb.Endpoint{
		Address: "10.0.0.1",
		Port:    80,
		Ready:   true,
		Labels: map[string]string{
			"app": "web",
		},
	}

	if reg.IsRemoteEndpoint(localEndpoint) {
		t.Error("local endpoint should not be identified as remote")
	}

	// Cluster label should be empty for local endpoints
	clusterName := reg.GetClusterForEndpoint(localEndpoint)
	if clusterName != "" {
		t.Errorf("expected empty cluster for local endpoint, got %q", clusterName)
	}
}

// TestCrossClusterGatewayFailover verifies that when gateways are updated
// (e.g., one becomes unhealthy), the registry correctly returns the next
// healthy gateway.
func TestCrossClusterGatewayFailover(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	// Initially register two healthy gateways
	reg.UpdateGateways("cluster-c", []GatewayAgent{
		{Address: "gw1:15002", Cluster: "cluster-c", Healthy: true},
		{Address: "gw2:15002", Cluster: "cluster-c", Healthy: true},
	})

	gw, err := reg.GetGateway("cluster-c")
	if err != nil {
		t.Fatalf("GetGateway error: %v", err)
	}
	if gw.Address != "gw1:15002" {
		t.Errorf("expected gw1, got %q", gw.Address)
	}

	// Update: gw1 becomes unhealthy
	reg.UpdateGateways("cluster-c", []GatewayAgent{
		{Address: "gw1:15002", Cluster: "cluster-c", Healthy: false},
		{Address: "gw2:15002", Cluster: "cluster-c", Healthy: true},
	})

	gw, err = reg.GetGateway("cluster-c")
	if err != nil {
		t.Fatalf("GetGateway error after failover: %v", err)
	}
	if gw.Address != "gw2:15002" {
		t.Errorf("expected gw2 after failover, got %q", gw.Address)
	}

	// Update: both become unhealthy
	reg.UpdateGateways("cluster-c", []GatewayAgent{
		{Address: "gw1:15002", Cluster: "cluster-c", Healthy: false},
		{Address: "gw2:15002", Cluster: "cluster-c", Healthy: false},
	})

	_, err = reg.GetGateway("cluster-c")
	if err == nil {
		t.Error("expected error when all gateways are unhealthy")
	}
}

// TestCrossClusterRemoveCluster verifies that removing a cluster from the
// registry makes its gateways unavailable.
func TestCrossClusterRemoveCluster(t *testing.T) {
	reg := NewCrossClusterTunnelRegistry()

	reg.UpdateGateways("cluster-d", []GatewayAgent{
		{Address: "gw1:15002", Cluster: "cluster-d", Healthy: true},
	})

	// Should be available
	_, err := reg.GetGateway("cluster-d")
	if err != nil {
		t.Fatalf("GetGateway error: %v", err)
	}

	// Remove and verify unavailable
	reg.RemoveCluster("cluster-d")
	_, err = reg.GetGateway("cluster-d")
	if err == nil {
		t.Error("expected error after removing cluster")
	}
}
