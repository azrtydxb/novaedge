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

package server

import (
	"testing"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/config"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestStructuralHash_SameConfigSameHash(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	s := NewHTTPServer(logger)

	snapshot := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Gateways: []*pb.Gateway{
				{
					Name:      "gw1",
					Namespace: "default",
					VipRef:    "vip-1",
					Listeners: []*pb.Listener{
						{Name: "http", Port: 80, Protocol: pb.Protocol_HTTP},
					},
				},
			},
			Routes: []*pb.Route{
				{
					Name:      "route1",
					Namespace: "default",
					Hostnames: []string{"example.com"},
					Rules: []*pb.RouteRule{
						{
							Matches: []*pb.RouteMatch{
								{Path: &pb.PathMatch{Type: pb.PathMatchType_PATH_PREFIX, Value: "/"}},
							},
							BackendRefs: []*pb.BackendRef{
								{Namespace: "default", Name: "backend1", Weight: 100},
							},
						},
					},
				},
			},
			Clusters: []*pb.Cluster{
				{Name: "backend1", Namespace: "default", LbPolicy: pb.LoadBalancingPolicy_ROUND_ROBIN},
			},
			Endpoints: map[string]*pb.EndpointList{
				"default/backend1": {
					Endpoints: []*pb.Endpoint{
						{Address: "10.0.0.1", Port: 8080, Ready: true},
					},
				},
			},
			Policies: []*pb.Policy{
				{Name: "policy1", Namespace: "default"},
			},
		},
	}

	hash1 := s.structuralHash(snapshot)
	hash2 := s.structuralHash(snapshot)

	if hash1 != hash2 {
		t.Errorf("Same snapshot should produce same structural hash: %s != %s", hash1, hash2)
	}

	if hash1 == "" {
		t.Error("Structural hash should not be empty")
	}
}

func TestStructuralHash_EndpointChangeDoesNotAffectHash(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	s := NewHTTPServer(logger)

	baseConfig := &pb.ConfigSnapshot{
		Version: "v1",
		Gateways: []*pb.Gateway{
			{
				Name:      "gw1",
				Namespace: "default",
				VipRef:    "vip-1",
				Listeners: []*pb.Listener{
					{Name: "http", Port: 80, Protocol: pb.Protocol_HTTP},
				},
			},
		},
		Routes: []*pb.Route{
			{
				Name:      "route1",
				Namespace: "default",
				Hostnames: []string{"example.com"},
				Rules: []*pb.RouteRule{
					{
						Matches: []*pb.RouteMatch{
							{Path: &pb.PathMatch{Type: pb.PathMatchType_PATH_PREFIX, Value: "/"}},
						},
						BackendRefs: []*pb.BackendRef{
							{Namespace: "default", Name: "backend1", Weight: 100},
						},
					},
				},
			},
		},
		Clusters: []*pb.Cluster{
			{Name: "backend1", Namespace: "default", LbPolicy: pb.LoadBalancingPolicy_ROUND_ROBIN},
		},
		Policies: []*pb.Policy{
			{Name: "policy1", Namespace: "default"},
		},
	}

	// Snapshot with endpoints A
	snapshot1 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version:  "v1",
			Gateways: baseConfig.Gateways,
			Routes:   baseConfig.Routes,
			Clusters: baseConfig.Clusters,
			Policies: baseConfig.Policies,
			Endpoints: map[string]*pb.EndpointList{
				"default/backend1": {
					Endpoints: []*pb.Endpoint{
						{Address: "10.0.0.1", Port: 8080, Ready: true},
						{Address: "10.0.0.2", Port: 8080, Ready: true},
					},
				},
			},
		},
	}

	// Snapshot with endpoints B (different endpoints, same structure)
	snapshot2 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version:  "v2",
			Gateways: baseConfig.Gateways,
			Routes:   baseConfig.Routes,
			Clusters: baseConfig.Clusters,
			Policies: baseConfig.Policies,
			Endpoints: map[string]*pb.EndpointList{
				"default/backend1": {
					Endpoints: []*pb.Endpoint{
						{Address: "10.0.0.1", Port: 8080, Ready: true},
						{Address: "10.0.0.3", Port: 8080, Ready: true}, // Different endpoint
					},
				},
			},
		},
	}

	hash1 := s.structuralHash(snapshot1)
	hash2 := s.structuralHash(snapshot2)

	if hash1 != hash2 {
		t.Errorf("Structural hash should be the same when only endpoints differ: %s != %s", hash1, hash2)
	}
}

func TestStructuralHash_RouteChangeAffectsHash(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	s := NewHTTPServer(logger)

	snapshot1 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes: []*pb.Route{
				{
					Name:      "route1",
					Namespace: "default",
					Hostnames: []string{"example.com"},
					Rules: []*pb.RouteRule{
						{
							Matches: []*pb.RouteMatch{
								{Path: &pb.PathMatch{Type: pb.PathMatchType_PATH_PREFIX, Value: "/api"}},
							},
						},
					},
				},
			},
			Clusters:  []*pb.Cluster{},
			Endpoints: map[string]*pb.EndpointList{},
			Gateways:  []*pb.Gateway{},
		},
	}

	snapshot2 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes: []*pb.Route{
				{
					Name:      "route1",
					Namespace: "default",
					Hostnames: []string{"example.com"},
					Rules: []*pb.RouteRule{
						{
							Matches: []*pb.RouteMatch{
								{Path: &pb.PathMatch{Type: pb.PathMatchType_PATH_PREFIX, Value: "/v2"}}, // Different path
							},
						},
					},
				},
			},
			Clusters:  []*pb.Cluster{},
			Endpoints: map[string]*pb.EndpointList{},
			Gateways:  []*pb.Gateway{},
		},
	}

	hash1 := s.structuralHash(snapshot1)
	hash2 := s.structuralHash(snapshot2)

	if hash1 == hash2 {
		t.Error("Structural hash should differ when routes change")
	}
}

func TestStructuralHash_GatewayChangeAffectsHash(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	s := NewHTTPServer(logger)

	snapshot1 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes: []*pb.Route{},
			Gateways: []*pb.Gateway{
				{
					Name:      "gw1",
					Namespace: "default",
					Listeners: []*pb.Listener{
						{Name: "http", Port: 80, Protocol: pb.Protocol_HTTP},
					},
				},
			},
			Clusters:  []*pb.Cluster{},
			Endpoints: map[string]*pb.EndpointList{},
		},
	}

	snapshot2 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes: []*pb.Route{},
			Gateways: []*pb.Gateway{
				{
					Name:      "gw1",
					Namespace: "default",
					Listeners: []*pb.Listener{
						{Name: "https", Port: 443, Protocol: pb.Protocol_HTTPS}, // Different port/protocol
					},
				},
			},
			Clusters:  []*pb.Cluster{},
			Endpoints: map[string]*pb.EndpointList{},
		},
	}

	hash1 := s.structuralHash(snapshot1)
	hash2 := s.structuralHash(snapshot2)

	if hash1 == hash2 {
		t.Error("Structural hash should differ when gateway listeners change")
	}
}

func TestStructuralHash_PolicyChangeAffectsHash(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	s := NewHTTPServer(logger)

	snapshot1 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes:    []*pb.Route{},
			Gateways:  []*pb.Gateway{},
			Clusters:  []*pb.Cluster{},
			Endpoints: map[string]*pb.EndpointList{},
			Policies: []*pb.Policy{
				{Name: "policy1", Namespace: "default"},
			},
		},
	}

	snapshot2 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes:    []*pb.Route{},
			Gateways:  []*pb.Gateway{},
			Clusters:  []*pb.Cluster{},
			Endpoints: map[string]*pb.EndpointList{},
			Policies: []*pb.Policy{
				{Name: "policy2", Namespace: "default"}, // Different policy
			},
		},
	}

	hash1 := s.structuralHash(snapshot1)
	hash2 := s.structuralHash(snapshot2)

	if hash1 == hash2 {
		t.Error("Structural hash should differ when policies change")
	}
}

func TestStructuralHash_LBPolicyChangeAffectsHash(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	s := NewHTTPServer(logger)

	snapshot1 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes:   []*pb.Route{},
			Gateways: []*pb.Gateway{},
			Clusters: []*pb.Cluster{
				{Name: "backend1", Namespace: "default", LbPolicy: pb.LoadBalancingPolicy_ROUND_ROBIN},
			},
			Endpoints: map[string]*pb.EndpointList{},
		},
	}

	snapshot2 := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Routes:   []*pb.Route{},
			Gateways: []*pb.Gateway{},
			Clusters: []*pb.Cluster{
				{Name: "backend1", Namespace: "default", LbPolicy: pb.LoadBalancingPolicy_LEAST_CONN}, // Different LB policy
			},
			Endpoints: map[string]*pb.EndpointList{},
		},
	}

	hash1 := s.structuralHash(snapshot1)
	hash2 := s.structuralHash(snapshot2)

	if hash1 == hash2 {
		t.Error("Structural hash should differ when LB policy changes")
	}
}
