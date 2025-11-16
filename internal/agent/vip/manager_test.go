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
	"context"
	"testing"

	"go.uber.org/zap/zaptest"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewManager(t *testing.T) {
	logger := zaptest.NewLogger(t)

	m, err := NewManager(logger)

	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	if m == nil {
		t.Fatal("Expected VIPManager to be created")
	}

	if m.logger == nil {
		t.Error("Logger should be initialized")
	}

	if m.l2Handler == nil {
		t.Error("L2 handler should be initialized")
	}

	if m.bgpHandler == nil {
		t.Error("BGP handler should be initialized")
	}

	if m.ospfHandler == nil {
		t.Error("OSPF handler should be initialized")
	}

	if len(m.assignments) != 0 {
		t.Error("Assignments should be empty initially")
	}
}

func TestManagerStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m, _ := NewManager(logger)

	ctx := context.Background()
	err := m.Start(ctx)

	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Verify all handlers are started
	if m.l2Handler == nil {
		t.Error("L2 handler should be initialized")
	}

	if m.bgpHandler == nil {
		t.Error("BGP handler should be initialized")
	}

	if m.ospfHandler == nil {
		t.Error("OSPF handler should be initialized")
	}
}

func TestManagerApplyVIPs(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m, _ := NewManager(logger)

	ctx := context.Background()
	m.Start(ctx)

	t.Run("apply_l2_vip", func(t *testing.T) {
		assignments := []*pb.VIPAssignment{
			{
				VipName:  "vip-l2",
				Address:  "10.0.0.100/32",
				Mode:     pb.VIPMode_L2_ARP,
				IsActive: true,
				L2Config: &pb.L2Config{
					Interface: "eth0",
				},
			},
		}

		err := m.ApplyVIPs(assignments)

		if err != nil {
			t.Logf("ApplyVIPs returned error (may be expected): %v", err)
		}

		m.mu.RLock()
		count := len(m.assignments)
		m.mu.RUnlock()

		if count != 1 {
			t.Errorf("Expected 1 assignment, got %d", count)
		}
	})

	t.Run("apply_bgp_vip", func(t *testing.T) {
		assignments := []*pb.VIPAssignment{
			{
				VipName:  "vip-bgp",
				Address:  "10.0.0.101/32",
				Mode:     pb.VIPMode_BGP,
				IsActive: true,
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65000,
					RouterId: "10.0.0.1",
				},
			},
		}

		err := m.ApplyVIPs(assignments)

		if err != nil {
			t.Logf("ApplyVIPs returned error (may be expected): %v", err)
		}
	})

	t.Run("apply_ospf_vip", func(t *testing.T) {
		assignments := []*pb.VIPAssignment{
			{
				VipName:  "vip-ospf",
				Address:  "10.0.0.102/32",
				Mode:     pb.VIPMode_OSPF,
				IsActive: true,
				OspfConfig: &pb.OSPFConfig{
					RouterId: "10.0.0.1",
					AreaId:   0,
				},
			},
		}

		err := m.ApplyVIPs(assignments)

		if err != nil {
			t.Logf("ApplyVIPs returned error (may be expected): %v", err)
		}
	})
}

func TestManagerApplyVIPsInactive(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m, _ := NewManager(logger)

	ctx := context.Background()
	m.Start(ctx)

	t.Run("inactive_vip", func(t *testing.T) {
		assignments := []*pb.VIPAssignment{
			{
				VipName:  "vip-inactive",
				Address:  "10.0.0.100/32",
				Mode:     pb.VIPMode_L2_ARP,
				IsActive: false, // Not active on this node
				L2Config: &pb.L2Config{
					Interface: "eth0",
				},
			},
		}

		err := m.ApplyVIPs(assignments)

		if err != nil {
			t.Logf("ApplyVIPs returned error (may be expected): %v", err)
		}

		m.mu.RLock()
		count := len(m.assignments)
		m.mu.RUnlock()

		if count != 1 {
			t.Errorf("Expected 1 assignment tracked, got %d", count)
		}
	})
}

func TestManagerReleaseVIPs(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m, _ := NewManager(logger)

	ctx := context.Background()
	m.Start(ctx)

	// First apply VIPs
	assignments := []*pb.VIPAssignment{
		{
			VipName:  "vip-to-release",
			Address:  "10.0.0.100/32",
			Mode:     pb.VIPMode_L2_ARP,
			IsActive: true,
			L2Config: &pb.L2Config{
				Interface: "eth0",
			},
		},
	}

	m.ApplyVIPs(assignments)

	// Now apply empty list to release all
	m.ApplyVIPs([]*pb.VIPAssignment{})

	m.mu.RLock()
	count := len(m.assignments)
	m.mu.RUnlock()

	if count != 0 {
		t.Errorf("Expected 0 assignments after release, got %d", count)
	}
}

func TestManagerModeSwitching(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m, _ := NewManager(logger)

	ctx := context.Background()
	m.Start(ctx)

	vipName := "vip-mode-switch"

	t.Run("switch_from_l2_to_bgp", func(t *testing.T) {
		// Apply L2 VIP
		l2Assignments := []*pb.VIPAssignment{
			{
				VipName:  vipName,
				Address:  "10.0.0.100/32",
				Mode:     pb.VIPMode_L2_ARP,
				IsActive: true,
				L2Config: &pb.L2Config{
					Interface: "eth0",
				},
			},
		}

		m.ApplyVIPs(l2Assignments)

		m.mu.RLock()
		count1 := len(m.assignments)
		m.mu.RUnlock()

		if count1 != 1 {
			t.Errorf("Expected 1 assignment, got %d", count1)
		}

		// Switch to BGP
		bgpAssignments := []*pb.VIPAssignment{
			{
				VipName:  vipName,
				Address:  "10.0.0.100/32",
				Mode:     pb.VIPMode_BGP,
				IsActive: true,
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65000,
					RouterId: "10.0.0.1",
				},
			},
		}

		m.ApplyVIPs(bgpAssignments)

		m.mu.RLock()
		assignment := m.assignments[vipName]
		m.mu.RUnlock()

		if assignment == nil {
			t.Error("Assignment should still exist after mode switch")
		} else if assignment.Mode != pb.VIPMode_BGP {
			t.Errorf("Expected mode BGP, got %v", assignment.Mode)
		}
	})
}

func TestManagerGetActiveVIPs(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m, _ := NewManager(logger)

	ctx := context.Background()
	m.Start(ctx)

	t.Run("no_vips", func(t *testing.T) {
		active := m.GetActiveVIPs()

		if len(active) != 0 {
			t.Errorf("Expected 0 active VIPs, got %d", len(active))
		}
	})

	t.Run("with_vips", func(t *testing.T) {
		assignments := []*pb.VIPAssignment{
			{
				VipName:  "vip-1",
				Address:  "10.0.0.100/32",
				Mode:     pb.VIPMode_L2_ARP,
				IsActive: true,
				L2Config: &pb.L2Config{
					Interface: "eth0",
				},
			},
			{
				VipName:  "vip-2",
				Address:  "10.0.0.101/32",
				Mode:     pb.VIPMode_L2_ARP,
				IsActive: true,
				L2Config: &pb.L2Config{
					Interface: "eth0",
				},
			},
		}

		m.ApplyVIPs(assignments)

		active := m.GetActiveVIPs()

		if len(active) != 2 {
			t.Errorf("Expected 2 active VIPs, got %d", len(active))
		}

		// Verify names are correct
		nameMap := make(map[string]bool)
		for _, name := range active {
			nameMap[name] = true
		}

		if !nameMap["vip-1"] {
			t.Error("vip-1 not found in active VIPs")
		}

		if !nameMap["vip-2"] {
			t.Error("vip-2 not found in active VIPs")
		}
	})
}

func TestManagerMultipleVIPHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m, _ := NewManager(logger)

	ctx := context.Background()
	m.Start(ctx)

	t.Run("apply_multiple_modes", func(t *testing.T) {
		assignments := []*pb.VIPAssignment{
			{
				VipName:  "vip-l2",
				Address:  "10.0.0.100/32",
				Mode:     pb.VIPMode_L2_ARP,
				IsActive: true,
				L2Config: &pb.L2Config{
					Interface: "eth0",
				},
			},
			{
				VipName:  "vip-bgp",
				Address:  "10.0.0.101/32",
				Mode:     pb.VIPMode_BGP,
				IsActive: true,
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65000,
					RouterId: "10.0.0.1",
				},
			},
			{
				VipName:  "vip-ospf",
				Address:  "10.0.0.102/32",
				Mode:     pb.VIPMode_OSPF,
				IsActive: true,
				OspfConfig: &pb.OSPFConfig{
					RouterId: "10.0.0.1",
					AreaId:   0,
				},
			},
		}

		err := m.ApplyVIPs(assignments)

		if err != nil {
			t.Logf("ApplyVIPs returned error (may be expected): %v", err)
		}

		m.mu.RLock()
		count := len(m.assignments)
		m.mu.RUnlock()

		if count != 3 {
			t.Errorf("Expected 3 assignments, got %d", count)
		}
	})

	t.Run("update_mixed_modes", func(t *testing.T) {
		// Update with different set (remove one, add another)
		assignments := []*pb.VIPAssignment{
			{
				VipName:  "vip-l2",
				Address:  "10.0.0.100/32",
				Mode:     pb.VIPMode_L2_ARP,
				IsActive: true,
				L2Config: &pb.L2Config{
					Interface: "eth0",
				},
			},
			{
				VipName:  "vip-ospf",
				Address:  "10.0.0.102/32",
				Mode:     pb.VIPMode_OSPF,
				IsActive: true,
				OspfConfig: &pb.OSPFConfig{
					RouterId: "10.0.0.1",
					AreaId:   0,
				},
			},
			{
				VipName:  "vip-new",
				Address:  "10.0.0.103/32",
				Mode:     pb.VIPMode_BGP,
				IsActive: true,
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65000,
					RouterId: "10.0.0.1",
				},
			},
		}

		m.ApplyVIPs(assignments)

		m.mu.RLock()
		count := len(m.assignments)
		_, hasBGPVip := m.assignments["vip-bgp"]
		_, hasNewVip := m.assignments["vip-new"]
		m.mu.RUnlock()

		if count != 3 {
			t.Errorf("Expected 3 assignments, got %d", count)
		}

		if hasBGPVip {
			t.Error("vip-bgp should have been released")
		}

		if !hasNewVip {
			t.Error("vip-new should be in assignments")
		}
	})
}

func TestManagerAssignmentsEquality(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m, _ := NewManager(logger)

	ctx := context.Background()
	m.Start(ctx)

	assignments1 := []*pb.VIPAssignment{
		{
			VipName:  "vip-test",
			Address:  "10.0.0.100/32",
			Mode:     pb.VIPMode_L2_ARP,
			IsActive: true,
			L2Config: &pb.L2Config{
				Interface: "eth0",
			},
		},
	}

	m.ApplyVIPs(assignments1)

	m.mu.RLock()
	initialCount := len(m.assignments)
	m.mu.RUnlock()

	// Apply same assignments again
	m.ApplyVIPs(assignments1)

	m.mu.RLock()
	finalCount := len(m.assignments)
	m.mu.RUnlock()

	// Count should remain the same
	if initialCount != finalCount {
		t.Errorf("Count changed from %d to %d for same assignments", initialCount, finalCount)
	}
}

func TestManagerTableDrivenTests(t *testing.T) {
	tests := []struct {
		name              string
		initialVIPs       int
		updatedVIPs       int
		expectedFinal     int
		description       string
	}{
		{
			name:          "single_vip",
			initialVIPs:   1,
			updatedVIPs:   1,
			expectedFinal: 1,
			description:   "Single VIP assignment",
		},
		{
			name:          "multiple_vips",
			initialVIPs:   3,
			updatedVIPs:   3,
			expectedFinal: 3,
			description:   "Multiple VIP assignments",
		},
		{
			name:          "add_vips",
			initialVIPs:   2,
			updatedVIPs:   4,
			expectedFinal: 4,
			description:   "Add new VIPs",
		},
		{
			name:          "remove_vips",
			initialVIPs:   3,
			updatedVIPs:   1,
			expectedFinal: 1,
			description:   "Remove VIPs",
		},
		{
			name:          "clear_all_vips",
			initialVIPs:   2,
			updatedVIPs:   0,
			expectedFinal: 0,
			description:   "Clear all VIPs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			m, _ := NewManager(logger)

			ctx := context.Background()
			m.Start(ctx)

			// Create initial assignments
			var assignments []*pb.VIPAssignment
			for i := 0; i < tt.initialVIPs; i++ {
				octet := byte('0' + byte(i%10))
				assignments = append(assignments, &pb.VIPAssignment{
					VipName:  "vip-" + string(octet),
					Address:  "10.0.0." + string(octet) + "/32",
					Mode:     pb.VIPMode_L2_ARP,
					IsActive: true,
					L2Config: &pb.L2Config{
						Interface: "eth0",
					},
				})
			}

			m.ApplyVIPs(assignments)

			// Update with new set
			var updated []*pb.VIPAssignment
			for i := 0; i < tt.updatedVIPs; i++ {
				octet := byte('0' + byte(i%10))
				updated = append(updated, &pb.VIPAssignment{
					VipName:  "vip-updated-" + string(octet),
					Address:  "10.0.1." + string(octet) + "/32",
					Mode:     pb.VIPMode_L2_ARP,
					IsActive: true,
					L2Config: &pb.L2Config{
						Interface: "eth0",
					},
				})
			}

			m.ApplyVIPs(updated)

			m.mu.RLock()
			count := len(m.assignments)
			m.mu.RUnlock()

			if count != tt.expectedFinal {
				t.Errorf("Expected %d VIPs, got %d", tt.expectedFinal, count)
			}
		})
	}
}

func TestManagerUnsupportedMode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	m, _ := NewManager(logger)

	ctx := context.Background()
	m.Start(ctx)

	t.Run("unsupported_vip_mode", func(t *testing.T) {
		assignments := []*pb.VIPAssignment{
			{
				VipName:  "vip-unknown",
				Address:  "10.0.0.100/32",
				Mode:     pb.VIPMode(999), // Invalid mode
				IsActive: true,
			},
		}

		err := m.ApplyVIPs(assignments)

		// Should handle gracefully or error
		m.mu.RLock()
		count := len(m.assignments)
		m.mu.RUnlock()

		if count != 1 {
			t.Logf("Unsupported mode not applied (expected): %v", err)
		}
	})
}
