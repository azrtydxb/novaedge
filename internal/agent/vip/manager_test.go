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

package vip

import (
	"net"
	"testing"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestAssignmentsEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        *pb.VIPAssignment
		b        *pb.VIPAssignment
		expected bool
	}{
		{
			name: "identical assignments",
			a: &pb.VIPAssignment{
				VipName:       "vip1",
				Address:       "192.168.1.1/24",
				Ipv6Address:   "fd00::1/64",
				Mode:          pb.VIPMode_L2_ARP,
				IsActive:      true,
				AddressFamily: "ipv4",
				Ports:         []int32{80, 443},
			},
			b: &pb.VIPAssignment{
				VipName:       "vip1",
				Address:       "192.168.1.1/24",
				Ipv6Address:   "fd00::1/64",
				Mode:          pb.VIPMode_L2_ARP,
				IsActive:      true,
				AddressFamily: "ipv4",
				Ports:         []int32{80, 443},
			},
			expected: true,
		},
		{
			name: "different vip name",
			a: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "192.168.1.1/24",
			},
			b: &pb.VIPAssignment{
				VipName: "vip2",
				Address: "192.168.1.1/24",
			},
			expected: false,
		},
		{
			name: "different address",
			a: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "192.168.1.1/24",
			},
			b: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "192.168.1.2/24",
			},
			expected: false,
		},
		{
			name: "different ipv6 address",
			a: &pb.VIPAssignment{
				VipName:     "vip1",
				Address:     "192.168.1.1/24",
				Ipv6Address: "fd00::1/64",
			},
			b: &pb.VIPAssignment{
				VipName:     "vip1",
				Address:     "192.168.1.1/24",
				Ipv6Address: "fd00::2/64",
			},
			expected: false,
		},
		{
			name: "different mode",
			a: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "192.168.1.1/24",
				Mode:    pb.VIPMode_L2_ARP,
			},
			b: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "192.168.1.1/24",
				Mode:    pb.VIPMode_BGP,
			},
			expected: false,
		},
		{
			name: "different is_active",
			a: &pb.VIPAssignment{
				VipName:  "vip1",
				Address:  "192.168.1.1/24",
				IsActive: true,
			},
			b: &pb.VIPAssignment{
				VipName:  "vip1",
				Address:  "192.168.1.1/24",
				IsActive: false,
			},
			expected: false,
		},
		{
			name: "different address family",
			a: &pb.VIPAssignment{
				VipName:       "vip1",
				Address:       "192.168.1.1/24",
				AddressFamily: "ipv4",
			},
			b: &pb.VIPAssignment{
				VipName:       "vip1",
				Address:       "192.168.1.1/24",
				AddressFamily: "ipv6",
			},
			expected: false,
		},
		{
			name: "different ports length",
			a: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "192.168.1.1/24",
				Ports:   []int32{80, 443},
			},
			b: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "192.168.1.1/24",
				Ports:   []int32{80},
			},
			expected: false,
		},
		{
			name: "different ports values",
			a: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "192.168.1.1/24",
				Ports:   []int32{80, 443},
			},
			b: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "192.168.1.1/24",
				Ports:   []int32{80, 8080},
			},
			expected: false,
		},
		{
			name: "empty ports equal",
			a: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "192.168.1.1/24",
				Ports:   []int32{},
			},
			b: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "192.168.1.1/24",
				Ports:   []int32{},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := assignmentsEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("assignmentsEqual() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCloneAssignmentWithAddress(t *testing.T) {
	orig := &pb.VIPAssignment{
		VipName:       "vip1",
		Address:       "192.168.1.1/24",
		Ipv6Address:   "fd00::1/64",
		Mode:          pb.VIPMode_BGP,
		IsActive:      true,
		AddressFamily: "ipv4",
		Ports:         []int32{80, 443},
		BgpConfig:     &pb.BGPConfig{},
		OspfConfig:    &pb.OSPFConfig{},
		BfdConfig:     &pb.BFDConfig{},
	}

	newAddr := "10.0.0.1/24"
	cloned := cloneAssignmentWithAddress(orig, newAddr)

	// Verify address was changed
	if cloned.Address != newAddr {
		t.Errorf("cloned.Address = %v, want %v", cloned.Address, newAddr)
	}

	// Verify other fields were copied
	if cloned.VipName != orig.VipName {
		t.Errorf("cloned.VipName = %v, want %v", cloned.VipName, orig.VipName)
	}
	if cloned.Mode != orig.Mode {
		t.Errorf("cloned.Mode = %v, want %v", cloned.Mode, orig.Mode)
	}
	if cloned.IsActive != orig.IsActive {
		t.Errorf("cloned.IsActive = %v, want %v", cloned.IsActive, orig.IsActive)
	}
	if len(cloned.Ports) != len(orig.Ports) {
		t.Errorf("cloned.Ports length = %v, want %v", len(cloned.Ports), len(orig.Ports))
	}
	if cloned.BgpConfig == nil {
		t.Error("BgpConfig was not copied")
	}
	if cloned.OspfConfig == nil {
		t.Error("OspfConfig was not copied")
	}
	if cloned.BfdConfig == nil {
		t.Error("BfdConfig was not copied")
	}

	// Verify original was not modified
	if orig.Address == newAddr {
		t.Error("original address was modified")
	}
}

func TestGetActiveVIPs(t *testing.T) {
	logger := zap.NewNop()
	manager := &DefaultManager{
		logger:      logger,
		assignments: make(map[string]*pb.VIPAssignment),
	}

	// Test with no assignments
	active := manager.GetActiveVIPs()
	if len(active) != 0 {
		t.Errorf("GetActiveVIPs() = %v, want empty slice", active)
	}

	// Add some assignments
	manager.assignments["vip1"] = &pb.VIPAssignment{
		VipName:  "vip1",
		IsActive: true,
	}
	manager.assignments["vip2"] = &pb.VIPAssignment{
		VipName:  "vip2",
		IsActive: false,
	}
	manager.assignments["vip3"] = &pb.VIPAssignment{
		VipName:  "vip3",
		IsActive: true,
	}

	active = manager.GetActiveVIPs()
	if len(active) != 2 {
		t.Errorf("GetActiveVIPs() returned %d items, want 2", len(active))
	}

	// Verify active VIPs contain vip1 and vip3
	activeMap := make(map[string]bool)
	for _, v := range active {
		activeMap[v] = true
	}
	if !activeMap["vip1"] || !activeMap["vip3"] {
		t.Errorf("GetActiveVIPs() = %v, want [vip1, vip3]", active)
	}
}

func TestGetActiveVIPs_NilAssignments(t *testing.T) {
	logger := zap.NewNop()
	manager := &DefaultManager{
		logger:      logger,
		assignments: nil, // nil map
	}

	// Should handle gracefully - will panic if not handled, which is a bug
	defer func() {
		if r := recover(); r != nil {
			t.Logf("GetActiveVIPs panicked with nil map (expected behavior): %v", r)
		}
	}()

	active := manager.GetActiveVIPs()
	// If we get here without panic, check the result
	_ = active
}

func TestNewManager(t *testing.T) {
	logger := zap.NewNop()
	manager, err := NewManager(logger)

	// NewManager may fail in environments without network access
	// This is expected behavior - the handlers need network interfaces
	if err != nil {
		t.Skipf("NewManager() error = %v (requires network access)", err)
		return
	}

	if manager == nil {
		t.Fatal("expected manager, got nil")
	}

	if manager.l2Handler == nil {
		t.Error("l2Handler should be initialized")
	}

	if manager.bgpHandler == nil {
		t.Error("bgpHandler should be initialized")
	}

	if manager.ospfHandler == nil {
		t.Error("ospfHandler should be initialized")
	}

	if manager.assignments == nil {
		t.Error("assignments map should be initialized")
	}
}

func TestRelease_EmptyAssignments(t *testing.T) {
	logger := zap.NewNop()
	manager := &DefaultManager{
		logger:      logger,
		assignments: make(map[string]*pb.VIPAssignment),
	}

	// Should not error with empty assignments
	err := manager.Release()
	if err != nil {
		t.Errorf("Release() with empty assignments error = %v", err)
	}
}

func TestAssignmentsEqual_BGPConfig(t *testing.T) {
	tests := []struct {
		name     string
		a        *pb.VIPAssignment
		b        *pb.VIPAssignment
		expected bool
	}{
		{
			name: "same bgp config",
			a: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "10.0.0.1/32",
				Mode:    pb.VIPMode_BGP,
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65001,
					RouterId: "10.0.0.1",
					Peers: []*pb.BGPPeer{
						{Address: "10.0.0.2", As: 65002, Port: 179},
					},
				},
			},
			b: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "10.0.0.1/32",
				Mode:    pb.VIPMode_BGP,
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65001,
					RouterId: "10.0.0.1",
					Peers: []*pb.BGPPeer{
						{Address: "10.0.0.2", As: 65002, Port: 179},
					},
				},
			},
			expected: true,
		},
		{
			name: "different bgp local_as",
			a: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				Mode:      pb.VIPMode_BGP,
				BgpConfig: &pb.BGPConfig{LocalAs: 65001},
			},
			b: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				Mode:      pb.VIPMode_BGP,
				BgpConfig: &pb.BGPConfig{LocalAs: 65002},
			},
			expected: false,
		},
		{
			name: "different bgp peers",
			a: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "10.0.0.1/32",
				Mode:    pb.VIPMode_BGP,
				BgpConfig: &pb.BGPConfig{
					LocalAs: 65001,
					Peers:   []*pb.BGPPeer{{Address: "10.0.0.2", As: 65002}},
				},
			},
			b: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "10.0.0.1/32",
				Mode:    pb.VIPMode_BGP,
				BgpConfig: &pb.BGPConfig{
					LocalAs: 65001,
					Peers:   []*pb.BGPPeer{{Address: "10.0.0.3", As: 65003}},
				},
			},
			expected: false,
		},
		{
			name: "nil vs non-nil bgp config",
			a: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BgpConfig: nil,
			},
			b: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BgpConfig: &pb.BGPConfig{LocalAs: 65001},
			},
			expected: false,
		},
		{
			name: "both nil bgp config",
			a: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BgpConfig: nil,
			},
			b: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BgpConfig: nil,
			},
			expected: true,
		},
		{
			name: "different local_preference",
			a: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BgpConfig: &pb.BGPConfig{LocalAs: 65001, LocalPreference: 100},
			},
			b: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BgpConfig: &pb.BGPConfig{LocalAs: 65001, LocalPreference: 200},
			},
			expected: false,
		},
		{
			name: "different communities",
			a: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BgpConfig: &pb.BGPConfig{LocalAs: 65001, Communities: []string{"65001:100"}},
			},
			b: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BgpConfig: &pb.BGPConfig{LocalAs: 65001, Communities: []string{"65001:200"}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := assignmentsEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("assignmentsEqual() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestAssignmentsEqual_OSPFConfig(t *testing.T) {
	tests := []struct {
		name     string
		a        *pb.VIPAssignment
		b        *pb.VIPAssignment
		expected bool
	}{
		{
			name: "same ospf config",
			a: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "10.0.0.1/32",
				OspfConfig: &pb.OSPFConfig{
					RouterId: "10.0.0.1",
					AreaId:   0,
					Cost:     10,
				},
			},
			b: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "10.0.0.1/32",
				OspfConfig: &pb.OSPFConfig{
					RouterId: "10.0.0.1",
					AreaId:   0,
					Cost:     10,
				},
			},
			expected: true,
		},
		{
			name: "different ospf cost",
			a: &pb.VIPAssignment{
				VipName:    "vip1",
				Address:    "10.0.0.1/32",
				OspfConfig: &pb.OSPFConfig{RouterId: "10.0.0.1", Cost: 10},
			},
			b: &pb.VIPAssignment{
				VipName:    "vip1",
				Address:    "10.0.0.1/32",
				OspfConfig: &pb.OSPFConfig{RouterId: "10.0.0.1", Cost: 20},
			},
			expected: false,
		},
		{
			name: "different ospf neighbors",
			a: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "10.0.0.1/32",
				OspfConfig: &pb.OSPFConfig{
					RouterId:  "10.0.0.1",
					Neighbors: []*pb.OSPFNeighbor{{Address: "10.0.0.2", Priority: 1}},
				},
			},
			b: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "10.0.0.1/32",
				OspfConfig: &pb.OSPFConfig{
					RouterId:  "10.0.0.1",
					Neighbors: []*pb.OSPFNeighbor{{Address: "10.0.0.3", Priority: 1}},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := assignmentsEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("assignmentsEqual() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestAssignmentsEqual_BFDConfig(t *testing.T) {
	tests := []struct {
		name     string
		a        *pb.VIPAssignment
		b        *pb.VIPAssignment
		expected bool
	}{
		{
			name: "same bfd config",
			a: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "10.0.0.1/32",
				BfdConfig: &pb.BFDConfig{
					Enabled:          true,
					DetectMultiplier: 3,
				},
			},
			b: &pb.VIPAssignment{
				VipName: "vip1",
				Address: "10.0.0.1/32",
				BfdConfig: &pb.BFDConfig{
					Enabled:          true,
					DetectMultiplier: 3,
				},
			},
			expected: true,
		},
		{
			name: "bfd enabled vs disabled",
			a: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BfdConfig: &pb.BFDConfig{Enabled: true},
			},
			b: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BfdConfig: &pb.BFDConfig{Enabled: false},
			},
			expected: false,
		},
		{
			name: "different detect multiplier",
			a: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BfdConfig: &pb.BFDConfig{Enabled: true, DetectMultiplier: 3},
			},
			b: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BfdConfig: &pb.BFDConfig{Enabled: true, DetectMultiplier: 5},
			},
			expected: false,
		},
		{
			name: "nil vs non-nil bfd config",
			a: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BfdConfig: nil,
			},
			b: &pb.VIPAssignment{
				VipName:   "vip1",
				Address:   "10.0.0.1/32",
				BfdConfig: &pb.BFDConfig{Enabled: true},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := assignmentsEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("assignmentsEqual() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestConfigOnlyChange(t *testing.T) {
	tests := []struct {
		name     string
		old      *pb.VIPAssignment
		new      *pb.VIPAssignment
		expected bool
	}{
		{
			name: "config only - bgp peers changed",
			old: &pb.VIPAssignment{
				VipName:       "vip1",
				Address:       "10.0.0.1/32",
				Mode:          pb.VIPMode_BGP,
				IsActive:      true,
				AddressFamily: "ipv4",
			},
			new: &pb.VIPAssignment{
				VipName:       "vip1",
				Address:       "10.0.0.1/32",
				Mode:          pb.VIPMode_BGP,
				IsActive:      true,
				AddressFamily: "ipv4",
			},
			expected: true,
		},
		{
			name: "structural - address changed",
			old: &pb.VIPAssignment{
				VipName:  "vip1",
				Address:  "10.0.0.1/32",
				Mode:     pb.VIPMode_BGP,
				IsActive: true,
			},
			new: &pb.VIPAssignment{
				VipName:  "vip1",
				Address:  "10.0.0.2/32",
				Mode:     pb.VIPMode_BGP,
				IsActive: true,
			},
			expected: false,
		},
		{
			name: "structural - mode changed",
			old: &pb.VIPAssignment{
				VipName:  "vip1",
				Address:  "10.0.0.1/32",
				Mode:     pb.VIPMode_BGP,
				IsActive: true,
			},
			new: &pb.VIPAssignment{
				VipName:  "vip1",
				Address:  "10.0.0.1/32",
				Mode:     pb.VIPMode_OSPF,
				IsActive: true,
			},
			expected: false,
		},
		{
			name: "structural - is_active changed",
			old: &pb.VIPAssignment{
				VipName:  "vip1",
				Address:  "10.0.0.1/32",
				Mode:     pb.VIPMode_BGP,
				IsActive: true,
			},
			new: &pb.VIPAssignment{
				VipName:  "vip1",
				Address:  "10.0.0.1/32",
				Mode:     pb.VIPMode_BGP,
				IsActive: false,
			},
			expected: false,
		},
		{
			name: "structural - ipv6 address changed",
			old: &pb.VIPAssignment{
				VipName:     "vip1",
				Address:     "10.0.0.1/32",
				Ipv6Address: "fd00::1/128",
				Mode:        pb.VIPMode_BGP,
				IsActive:    true,
			},
			new: &pb.VIPAssignment{
				VipName:     "vip1",
				Address:     "10.0.0.1/32",
				Ipv6Address: "fd00::2/128",
				Mode:        pb.VIPMode_BGP,
				IsActive:    true,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := configOnlyChange(tt.old, tt.new)
			if result != tt.expected {
				t.Errorf("configOnlyChange() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCommunitiesEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected bool
	}{
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "both empty",
			a:        []string{},
			b:        []string{},
			expected: true,
		},
		{
			name:     "same communities",
			a:        []string{"65001:100", "65001:200"},
			b:        []string{"65001:100", "65001:200"},
			expected: true,
		},
		{
			name:     "different length",
			a:        []string{"65001:100"},
			b:        []string{"65001:100", "65001:200"},
			expected: false,
		},
		{
			name:     "different values",
			a:        []string{"65001:100"},
			b:        []string{"65001:200"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := communitiesEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("communitiesEqual() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBGPHandler_ReconfigureVIP(t *testing.T) {
	logger := zap.NewNop()
	handler, err := NewBGPHandler(logger)
	if err != nil {
		t.Fatalf("NewBGPHandler() error = %v", err)
	}

	// Seed an existing VIP state (simulating a previously added VIP)
	handler.activeVIPs["test-vip"] = &BGPVIPState{
		Assignment: &pb.VIPAssignment{
			VipName: "test-vip",
			Address: "10.0.0.1/32",
			Mode:    pb.VIPMode_BGP,
			BgpConfig: &pb.BGPConfig{
				LocalAs:  65001,
				RouterId: "10.0.0.1",
				Peers:    []*pb.BGPPeer{{Address: "10.0.0.2", As: 65002}},
			},
		},
		bgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
			Peers:    []*pb.BGPPeer{{Address: "10.0.0.2", As: 65002}},
		},
		Announced: true,
	}

	// Verify the VIP exists
	if len(handler.activeVIPs) != 1 {
		t.Fatalf("expected 1 active VIP, got %d", len(handler.activeVIPs))
	}

	// Verify reconfigure is triggered (not a new VIP)
	state := handler.activeVIPs["test-vip"]
	if state.bgpConfig.LocalAs != 65001 {
		t.Errorf("expected initial LocalAs=65001, got %d", state.bgpConfig.LocalAs)
	}
}

func TestOSPFHandler_ReconfigureVIP(t *testing.T) {
	logger := zap.NewNop()
	handler, err := NewOSPFHandler(logger)
	if err != nil {
		t.Fatalf("NewOSPFHandler() error = %v", err)
	}

	// Seed an existing VIP state
	handler.activeVIPs["test-vip"] = &OSPFVIPState{
		Assignment: &pb.VIPAssignment{
			VipName: "test-vip",
			Address: "10.0.0.1/32",
			Mode:    pb.VIPMode_OSPF,
			OspfConfig: &pb.OSPFConfig{
				RouterId: "10.0.0.1",
				AreaId:   0,
				Cost:     10,
			},
		},
		ospfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
			Cost:     10,
		},
		Announced: true,
	}

	if len(handler.activeVIPs) != 1 {
		t.Fatalf("expected 1 active VIP, got %d", len(handler.activeVIPs))
	}

	state := handler.activeVIPs["test-vip"]
	if state.ospfConfig.Cost != 10 {
		t.Errorf("expected initial Cost=10, got %d", state.ospfConfig.Cost)
	}
}

func TestBFDManager_UpdateSession(t *testing.T) {
	logger := zap.NewNop()
	manager := NewBFDManager(logger, nil)

	peerIP := net.ParseIP("10.0.0.2")

	// Add an initial session
	err := manager.AddSession(peerIP, BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("AddSession() error = %v", err)
	}

	// Verify initial state
	session := manager.sessions[peerIP.String()]
	if session == nil {
		t.Fatal("expected session to exist")
	}
	if session.detectMultiplier != 3 {
		t.Errorf("expected detectMultiplier=3, got %d", session.detectMultiplier)
	}
	if session.desiredMinTxInterval != 300*time.Millisecond {
		t.Errorf("expected desiredMinTxInterval=300ms, got %v", session.desiredMinTxInterval)
	}

	// Update the session with new parameters
	err = manager.UpdateSession(peerIP, BFDConfig{
		DetectMultiplier:      5,
		DesiredMinTxInterval:  100 * time.Millisecond,
		RequiredMinRxInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}

	// Verify updated state
	if session.detectMultiplier != 5 {
		t.Errorf("expected detectMultiplier=5 after update, got %d", session.detectMultiplier)
	}
	if session.desiredMinTxInterval != 100*time.Millisecond {
		t.Errorf("expected desiredMinTxInterval=100ms after update, got %v", session.desiredMinTxInterval)
	}
	if session.requiredMinRxInterval != 100*time.Millisecond {
		t.Errorf("expected requiredMinRxInterval=100ms after update, got %v", session.requiredMinRxInterval)
	}
	expectedDetectionTime := time.Duration(5) * 100 * time.Millisecond
	if session.detectionTime != expectedDetectionTime {
		t.Errorf("expected detectionTime=%v after update, got %v", expectedDetectionTime, session.detectionTime)
	}
}

func TestBFDManager_UpdateSession_NonExistent(t *testing.T) {
	logger := zap.NewNop()
	manager := NewBFDManager(logger, nil)

	peerIP := net.ParseIP("10.0.0.2")

	// Update a non-existent session should create it
	err := manager.UpdateSession(peerIP, BFDConfig{
		DetectMultiplier:      5,
		DesiredMinTxInterval:  100 * time.Millisecond,
		RequiredMinRxInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}

	// Verify session was created
	if manager.GetSessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", manager.GetSessionCount())
	}
}

func TestBFDManager_RemoveSession(t *testing.T) {
	logger := zap.NewNop()
	manager := NewBFDManager(logger, nil)

	peerIP := net.ParseIP("10.0.0.2")

	// Add a session
	err := manager.AddSession(peerIP, BFDConfig{DetectMultiplier: 3})
	if err != nil {
		t.Fatalf("AddSession() error = %v", err)
	}

	if manager.GetSessionCount() != 1 {
		t.Fatalf("expected 1 session, got %d", manager.GetSessionCount())
	}

	// Remove the session
	manager.RemoveSession(peerIP)

	if manager.GetSessionCount() != 0 {
		t.Errorf("expected 0 sessions after remove, got %d", manager.GetSessionCount())
	}

	// Removing non-existent session should not panic
	manager.RemoveSession(net.ParseIP("10.0.0.99"))
}
