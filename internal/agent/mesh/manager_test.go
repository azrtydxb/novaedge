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

package mesh

import (
	"context"
	"testing"

	"go.uber.org/zap"

	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

func TestServiceTableLookup(t *testing.T) {
	st := NewServiceTable()

	services := []*pb.InternalService{
		{
			Name:      "web",
			Namespace: "app",
			ClusterIp: "10.96.0.10",
			Ports: []*pb.ServicePort{
				{Name: "http", Port: 80, TargetPort: 8080, Protocol: "TCP"},
			},
			Endpoints: []*pb.Endpoint{
				{Address: "10.244.0.5", Port: 8080, Ready: true},
				{Address: "10.244.0.6", Port: 8080, Ready: true},
			},
			LbPolicy:    pb.LoadBalancingPolicy_ROUND_ROBIN,
			MeshEnabled: true,
		},
	}

	st.Update(services)

	if st.ServiceCount() != 1 {
		t.Fatalf("Expected 1 service, got %d", st.ServiceCount())
	}

	// Lookup existing service
	ep, ok := st.Lookup("10.96.0.10", 80)
	if !ok {
		t.Fatal("Expected to find service 10.96.0.10:80")
	}
	if ep.Address != "10.244.0.5" && ep.Address != "10.244.0.6" {
		t.Errorf("Unexpected endpoint address: %s", ep.Address)
	}

	// Lookup non-existent service
	_, ok = st.Lookup("10.96.0.99", 80)
	if ok {
		t.Error("Expected no match for 10.96.0.99:80")
	}
}

func TestServiceTableRoundRobin(t *testing.T) {
	st := NewServiceTable()

	services := []*pb.InternalService{
		{
			ClusterIp: "10.96.0.10",
			Ports:     []*pb.ServicePort{{Port: 80}},
			Endpoints: []*pb.Endpoint{
				{Address: "10.244.0.1", Port: 8080, Ready: true},
				{Address: "10.244.0.2", Port: 8080, Ready: true},
			},
		},
	}
	st.Update(services)

	// Should round-robin between endpoints
	addrs := make(map[string]int)
	for i := 0; i < 10; i++ {
		ep, ok := st.Lookup("10.96.0.10", 80)
		if !ok {
			t.Fatal("lookup failed")
		}
		addrs[ep.Address]++
	}

	if addrs["10.244.0.1"] != 5 || addrs["10.244.0.2"] != 5 {
		t.Errorf("Expected even distribution, got %v", addrs)
	}
}

func TestServiceTableSkipsNotReady(t *testing.T) {
	st := NewServiceTable()

	services := []*pb.InternalService{
		{
			ClusterIp: "10.96.0.10",
			Ports:     []*pb.ServicePort{{Port: 80}},
			Endpoints: []*pb.Endpoint{
				{Address: "10.244.0.1", Port: 8080, Ready: false},
				{Address: "10.244.0.2", Port: 8080, Ready: true},
			},
		},
	}
	st.Update(services)

	for i := 0; i < 5; i++ {
		ep, ok := st.Lookup("10.96.0.10", 80)
		if !ok {
			t.Fatal("lookup failed")
		}
		if ep.Address != "10.244.0.2" {
			t.Errorf("Expected only ready endpoint 10.244.0.2, got %s", ep.Address)
		}
	}
}

func TestServiceTableNoReadyEndpoints(t *testing.T) {
	st := NewServiceTable()

	services := []*pb.InternalService{
		{
			ClusterIp: "10.96.0.10",
			Ports:     []*pb.ServicePort{{Port: 80}},
			Endpoints: []*pb.Endpoint{
				{Address: "10.244.0.1", Port: 8080, Ready: false},
			},
		},
	}
	st.Update(services)

	_, ok := st.Lookup("10.96.0.10", 80)
	if ok {
		t.Error("Expected no match when no endpoints are ready")
	}
}

func TestServiceTableMultiplePorts(t *testing.T) {
	st := NewServiceTable()

	services := []*pb.InternalService{
		{
			ClusterIp: "10.96.0.10",
			Ports: []*pb.ServicePort{
				{Name: "http", Port: 80},
				{Name: "grpc", Port: 9090},
			},
			Endpoints: []*pb.Endpoint{
				{Address: "10.244.0.5", Port: 8080, Ready: true},
			},
		},
	}
	st.Update(services)

	if st.ServiceCount() != 2 {
		t.Errorf("Expected 2 routing entries (one per port), got %d", st.ServiceCount())
	}

	_, ok1 := st.Lookup("10.96.0.10", 80)
	_, ok2 := st.Lookup("10.96.0.10", 9090)
	if !ok1 || !ok2 {
		t.Error("Expected both ports to be routable")
	}
}

func TestManagerApplyConfig(t *testing.T) {
	backend := newFakeBackend()
	logger := zap.NewNop()

	mgr := &Manager{
		logger:       logger,
		tproxyPort:   15001,
		serviceTable: NewServiceTable(),
		tproxy:       NewTPROXYManagerWithBackend(logger, 15001, backend),
	}

	services := []*pb.InternalService{
		{
			ClusterIp: "10.96.0.10",
			Ports:     []*pb.ServicePort{{Port: 80}},
			Endpoints: []*pb.Endpoint{
				{Address: "10.244.0.5", Port: 8080, Ready: true},
			},
			MeshEnabled: true,
		},
		{
			ClusterIp: "10.96.0.20",
			Ports:     []*pb.ServicePort{{Port: 443}, {Port: 8080}},
			Endpoints: []*pb.Endpoint{
				{Address: "10.244.0.6", Port: 8443, Ready: true},
			},
			MeshEnabled: true,
		},
	}

	if err := mgr.ApplyConfig(context.Background(), services, nil); err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	// Should have 3 TPROXY rules (1 for first service, 2 for second)
	if mgr.tproxy.ActiveRuleCount() != 3 {
		t.Errorf("Expected 3 TPROXY rules, got %d", mgr.tproxy.ActiveRuleCount())
	}

	// Service table should have 3 entries
	if mgr.serviceTable.ServiceCount() != 3 {
		t.Errorf("Expected 3 routing entries, got %d", mgr.serviceTable.ServiceCount())
	}
}

func TestManagerApplyConfigNotStarted(t *testing.T) {
	mgr := &Manager{
		logger:       zap.NewNop(),
		serviceTable: NewServiceTable(),
	}

	err := mgr.ApplyConfig(context.Background(), nil, nil)
	if err == nil {
		t.Error("Expected error when manager not started")
	}
}
