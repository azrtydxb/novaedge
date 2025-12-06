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

package standalone

import (
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestConverterToSnapshot(t *testing.T) {
	converter := NewConverter()

	config := &Config{
		Version: "1.0",
		Listeners: []ListenerConfig{
			{
				Name:     "http",
				Port:     80,
				Protocol: "HTTP",
			},
		},
		Routes: []RouteConfig{
			{
				Name: "api-route",
				Match: RouteMatch{
					Path: &PathMatch{
						Type:  "PathPrefix",
						Value: "/api",
					},
				},
				Backends: []RouteBackendRef{
					{Name: "api-backend", Weight: 100},
				},
			},
		},
		Backends: []BackendConfig{
			{
				Name:     "api-backend",
				LBPolicy: "RoundRobin",
				Endpoints: []EndpointConfig{
					{Address: "server1:8080", Weight: 1},
					{Address: "server2:8080", Weight: 2},
				},
			},
		},
	}

	snapshot, err := converter.ToSnapshot(config, "test-node")
	if err != nil {
		t.Fatalf("ToSnapshot failed: %v", err)
	}

	// Verify gateways
	if len(snapshot.Gateways) != 1 {
		t.Errorf("Expected 1 gateway, got %d", len(snapshot.Gateways))
	}
	if snapshot.Gateways[0].Name != "standalone" {
		t.Errorf("Expected gateway name 'standalone', got %s", snapshot.Gateways[0].Name)
	}
	if len(snapshot.Gateways[0].Listeners) != 1 {
		t.Errorf("Expected 1 listener, got %d", len(snapshot.Gateways[0].Listeners))
	}
	if snapshot.Gateways[0].Listeners[0].Port != 80 {
		t.Errorf("Expected listener port 80, got %d", snapshot.Gateways[0].Listeners[0].Port)
	}

	// Verify routes
	if len(snapshot.Routes) != 1 {
		t.Errorf("Expected 1 route, got %d", len(snapshot.Routes))
	}
	if snapshot.Routes[0].Name != "api-route" {
		t.Errorf("Expected route name 'api-route', got %s", snapshot.Routes[0].Name)
	}

	// Verify clusters
	if len(snapshot.Clusters) != 1 {
		t.Errorf("Expected 1 cluster, got %d", len(snapshot.Clusters))
	}
	if snapshot.Clusters[0].Name != "api-backend" {
		t.Errorf("Expected cluster name 'api-backend', got %s", snapshot.Clusters[0].Name)
	}
	if snapshot.Clusters[0].LbPolicy != pb.LoadBalancingPolicy_ROUND_ROBIN {
		t.Errorf("Expected ROUND_ROBIN policy, got %v", snapshot.Clusters[0].LbPolicy)
	}

	// Verify endpoints
	endpointKey := "default/api-backend"
	if endpoints, ok := snapshot.Endpoints[endpointKey]; !ok {
		t.Errorf("Expected endpoints for key %s", endpointKey)
	} else if len(endpoints.Endpoints) != 2 {
		t.Errorf("Expected 2 endpoints, got %d", len(endpoints.Endpoints))
	}

	// Verify version is set
	if snapshot.Version == "" {
		t.Error("Expected version to be set")
	}
}

func TestParseProtocol(t *testing.T) {
	converter := NewConverter()

	tests := []struct {
		input    string
		expected pb.Protocol
	}{
		{"HTTP", pb.Protocol_HTTP},
		{"http", pb.Protocol_HTTP},
		{"HTTPS", pb.Protocol_HTTPS},
		{"HTTP3", pb.Protocol_HTTP3},
		{"TCP", pb.Protocol_TCP},
		{"TLS", pb.Protocol_TLS},
		{"unknown", pb.Protocol_PROTOCOL_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := converter.parseProtocol(tt.input)
			if result != tt.expected {
				t.Errorf("parseProtocol(%s) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseLBPolicy(t *testing.T) {
	converter := NewConverter()

	tests := []struct {
		input    string
		expected pb.LoadBalancingPolicy
	}{
		{"RoundRobin", pb.LoadBalancingPolicy_ROUND_ROBIN},
		{"P2C", pb.LoadBalancingPolicy_P2C},
		{"EWMA", pb.LoadBalancingPolicy_EWMA},
		{"RingHash", pb.LoadBalancingPolicy_RING_HASH},
		{"Maglev", pb.LoadBalancingPolicy_MAGLEV},
		{"unknown", pb.LoadBalancingPolicy_ROUND_ROBIN},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := converter.parseLBPolicy(tt.input)
			if result != tt.expected {
				t.Errorf("parseLBPolicy(%s) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParsePathMatchType(t *testing.T) {
	converter := NewConverter()

	tests := []struct {
		input    string
		expected pb.PathMatchType
	}{
		{"Exact", pb.PathMatchType_EXACT},
		{"PathPrefix", pb.PathMatchType_PATH_PREFIX},
		{"RegularExpression", pb.PathMatchType_REGULAR_EXPRESSION},
		{"unknown", pb.PathMatchType_PATH_PREFIX},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := converter.parsePathMatchType(tt.input)
			if result != tt.expected {
				t.Errorf("parsePathMatchType(%s) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseVIPMode(t *testing.T) {
	converter := NewConverter()

	tests := []struct {
		input    string
		expected pb.VIPMode
	}{
		{"L2", pb.VIPMode_L2_ARP},
		{"BGP", pb.VIPMode_BGP},
		{"OSPF", pb.VIPMode_OSPF},
		{"unknown", pb.VIPMode_L2_ARP},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := converter.parseVIPMode(tt.input)
			if result != tt.expected {
				t.Errorf("parseVIPMode(%s) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseAddressPort(t *testing.T) {
	tests := []struct {
		input        string
		expectedAddr string
		expectedPort int32
	}{
		{"localhost:8080", "localhost", 8080},
		{"192.168.1.1:443", "192.168.1.1", 443},
		{"server", "server", 80},
		{"server:invalid", "server", 0}, // Invalid port parsed as 0
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			addr, port := parseAddressPort(tt.input)
			if addr != tt.expectedAddr {
				t.Errorf("parseAddressPort(%s) address = %s, want %s", tt.input, addr, tt.expectedAddr)
			}
			if port != tt.expectedPort {
				t.Errorf("parseAddressPort(%s) port = %d, want %d", tt.input, port, tt.expectedPort)
			}
		})
	}
}
