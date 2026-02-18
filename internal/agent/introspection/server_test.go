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

package introspection

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// mockStateProvider implements StateProvider for testing
type mockStateProvider struct {
	snapshot *pb.ConfigSnapshot
}

func (m *mockStateProvider) GetCurrentSnapshot() *pb.ConfigSnapshot {
	return m.snapshot
}

func TestNewServer(t *testing.T) {
	provider := &mockStateProvider{}
	logger := zap.NewNop()

	server := NewServer(provider, logger)
	assert.NotNil(t, server)
	assert.Equal(t, provider, server.provider)
	assert.Equal(t, logger, server.logger)
}

func TestServer_GetAgentConfig_NilSnapshot(t *testing.T) {
	provider := &mockStateProvider{snapshot: nil}
	server := NewServer(provider, zap.NewNop())

	resp, err := server.GetAgentConfig(context.Background(), &pb.GetConfigRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "", resp.Version)
	assert.Equal(t, int32(0), resp.GatewayCount)
}

func TestServer_GetAgentConfig_WithSnapshot(t *testing.T) {
	snapshot := &pb.ConfigSnapshot{
		Version: "v123",
		Gateways: []*pb.Gateway{
			{Name: "gateway1"},
			{Name: "gateway2"},
		},
		Routes: []*pb.Route{
			{Name: "route1"},
		},
		Clusters: []*pb.Cluster{
			{Name: "cluster1"},
			{Name: "cluster2"},
			{Name: "cluster3"},
		},
		Endpoints: map[string]*pb.EndpointList{
			"cluster1": {
				Endpoints: []*pb.Endpoint{
					{Address: "10.0.0.1", Port: 8080},
					{Address: "10.0.0.2", Port: 8080},
				},
			},
			"cluster2": {
				Endpoints: []*pb.Endpoint{
					{Address: "10.0.0.3", Port: 8080},
				},
			},
		},
		VipAssignments: []*pb.VIPAssignment{
			{VipName: "vip1"},
			{VipName: "vip2"},
		},
		Policies: []*pb.Policy{
			{Name: "policy1"},
		},
	}

	provider := &mockStateProvider{snapshot: snapshot}
	server := NewServer(provider, zap.NewNop())

	resp, err := server.GetAgentConfig(context.Background(), &pb.GetConfigRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "v123", resp.Version)
	assert.Equal(t, int32(2), resp.GatewayCount)
	assert.Equal(t, int32(1), resp.RouteCount)
	assert.Equal(t, int32(3), resp.ClusterCount)
	assert.Equal(t, int32(3), resp.EndpointCount) // 2 + 1 endpoints
	assert.Equal(t, int32(2), resp.VipCount)
	assert.Equal(t, int32(1), resp.PolicyCount)
}

func TestServer_GetBackendHealth_NilSnapshot(t *testing.T) {
	provider := &mockStateProvider{snapshot: nil}
	server := NewServer(provider, zap.NewNop())

	resp, err := server.GetBackendHealth(context.Background(), &pb.GetBackendHealthRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.Backends)
}

func TestServer_GetBackendHealth_WithSnapshot(t *testing.T) {
	snapshot := &pb.ConfigSnapshot{
		Clusters: []*pb.Cluster{
			{
				Name:      "cluster1",
				Namespace: "default",
				LbPolicy:  pb.LoadBalancingPolicy_ROUND_ROBIN,
			},
		},
		Endpoints: map[string]*pb.EndpointList{
			"cluster1": {
				Endpoints: []*pb.Endpoint{
					{Address: "10.0.0.1", Port: 8080, Ready: true},
					{Address: "10.0.0.2", Port: 8080, Ready: false},
				},
			},
		},
	}

	provider := &mockStateProvider{snapshot: snapshot}
	server := NewServer(provider, zap.NewNop())

	resp, err := server.GetBackendHealth(context.Background(), &pb.GetBackendHealthRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	require.Len(t, resp.Backends, 1)

	backend := resp.Backends[0]
	assert.Equal(t, "cluster1", backend.ClusterName)
	assert.Equal(t, "default", backend.Namespace)
	assert.Equal(t, "ROUND_ROBIN", backend.LbPolicy)
	require.Len(t, backend.Endpoints, 2)

	assert.Equal(t, "10.0.0.1", backend.Endpoints[0].Address)
	assert.Equal(t, int32(8080), backend.Endpoints[0].Port)
	assert.True(t, backend.Endpoints[0].Healthy)

	assert.Equal(t, "10.0.0.2", backend.Endpoints[1].Address)
	assert.False(t, backend.Endpoints[1].Healthy)
}

func TestServer_GetVIPs_NilSnapshot(t *testing.T) {
	provider := &mockStateProvider{snapshot: nil}
	server := NewServer(provider, zap.NewNop())

	resp, err := server.GetVIPs(context.Background(), &pb.GetVIPsRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.Vips)
}

func TestServer_GetVIPs_WithSnapshot(t *testing.T) {
	snapshot := &pb.ConfigSnapshot{
		VipAssignments: []*pb.VIPAssignment{
			{
				VipName:  "vip1",
				Address:  "192.168.1.100",
				Mode:     pb.VIPMode_L2_ARP,
				IsActive: true,
				Ports:    []int32{80, 443},
			},
			{
				VipName:  "vip2",
				Address:  "192.168.1.101",
				Mode:     pb.VIPMode_BGP,
				IsActive: false,
				Ports:    []int32{8080},
			},
		},
	}

	provider := &mockStateProvider{snapshot: snapshot}
	server := NewServer(provider, zap.NewNop())

	resp, err := server.GetVIPs(context.Background(), &pb.GetVIPsRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	require.Len(t, resp.Vips, 2)

	vip1 := resp.Vips[0]
	assert.Equal(t, "vip1", vip1.Name)
	assert.Equal(t, "192.168.1.100", vip1.Address)
	assert.Equal(t, "L2_ARP", vip1.Mode)
	assert.True(t, vip1.IsActive)
	assert.Equal(t, []int32{80, 443}, vip1.Ports)

	vip2 := resp.Vips[1]
	assert.Equal(t, "vip2", vip2.Name)
	assert.Equal(t, "192.168.1.101", vip2.Address)
	assert.False(t, vip2.IsActive)
}

func TestSafeInt32(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int32
	}{
		{
			name:     "zero",
			input:    0,
			expected: 0,
		},
		{
			name:     "small value",
			input:    100,
			expected: 100,
		},
		{
			name:     "max int32",
			input:    math.MaxInt32,
			expected: math.MaxInt32,
		},
		{
			name:     "overflow clamped to max",
			input:    math.MaxInt32 + 1,
			expected: math.MaxInt32,
		},
		{
			name:     "large overflow",
			input:    math.MaxInt64,
			expected: math.MaxInt32,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safeInt32(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
