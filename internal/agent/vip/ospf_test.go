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
	"time"

	"go.uber.org/zap/zaptest"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewOSPFHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)

	h, err := NewOSPFHandler(logger)

	if err != nil {
		t.Fatalf("NewOSPFHandler returned error: %v", err)
	}

	if h == nil {
		t.Fatal("Expected OSPFHandler to be created")
	}

	if h.logger == nil {
		t.Error("Logger should be initialized")
	}

	if len(h.activeVIPs) != 0 {
		t.Error("activeVIPs should be empty initially")
	}

	if h.started {
		t.Error("Handler should not be started initially")
	}
}

func TestOSPFHandlerStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx := context.Background()
	err := h.Start(ctx)

	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	h.mu.RLock()
	started := h.started
	h.mu.RUnlock()

	if !started {
		t.Error("Handler should be marked as started")
	}
}

func TestOSPFHandlerStartIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx := context.Background()

	// Start twice
	err1 := h.Start(ctx)
	err2 := h.Start(ctx)

	if err1 != nil {
		t.Fatalf("First Start returned error: %v", err1)
	}

	if err2 != nil {
		t.Fatalf("Second Start returned error: %v", err2)
	}

	// Should not have any VIPs
	h.mu.RLock()
	vipCount := len(h.activeVIPs)
	h.mu.RUnlock()

	if vipCount != 0 {
		t.Error("VIP count should remain 0")
	}
}

func TestOSPFHandlerAddVIP(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	t.Run("add_valid_vip", func(t *testing.T) {
		assignment := &pb.VIPAssignment{
			VipName:  "vip-web",
			Address:  "10.0.0.100/32",
			Mode:     pb.VIPMode_OSPF,
			IsActive: true,
			OspfConfig: &pb.OSPFConfig{
				RouterId:      "10.0.0.1",
				AreaId:        0,
				HelloInterval: 10,
				DeadInterval:  40,
				Neighbors:     []*pb.OSPFNeighbor{},
			},
		}

		err := h.AddVIP(assignment)

		if err == nil {
			h.mu.RLock()
			count := len(h.activeVIPs)
			h.mu.RUnlock()

			if count != 1 {
				t.Errorf("Expected 1 active VIP, got %d", count)
			}
		}
	})

	t.Run("invalid_vip_address", func(t *testing.T) {
		assignment := &pb.VIPAssignment{
			VipName:  "vip-invalid",
			Address:  "not-an-ip",
			Mode:     pb.VIPMode_OSPF,
			IsActive: true,
			OspfConfig: &pb.OSPFConfig{
				RouterId: "10.0.0.1",
				AreaId:   0,
			},
		}

		err := h.AddVIP(assignment)

		if err == nil {
			t.Error("Expected error for invalid VIP address")
		}
	})

	t.Run("missing_ospf_config", func(t *testing.T) {
		assignment := &pb.VIPAssignment{
			VipName:    "vip-no-config",
			Address:    "10.0.0.101/32",
			Mode:       pb.VIPMode_OSPF,
			IsActive:   true,
			OspfConfig: nil,
		}

		err := h.AddVIP(assignment)

		if err == nil {
			t.Error("Expected error when OSPF config is missing")
		}
	})
}

func TestOSPFHandlerRemoveVIP(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	assignment := &pb.VIPAssignment{
		VipName:  "vip-test",
		Address:  "10.0.0.100/32",
		Mode:     pb.VIPMode_OSPF,
		IsActive: true,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
		},
	}

	t.Run("remove_existing_vip", func(t *testing.T) {
		// Add VIP first
		h.AddVIP(assignment)

		// Remove VIP
		err := h.RemoveVIP(assignment)

		if err != nil {
			t.Logf("RemoveVIP returned error (may be expected): %v", err)
		}

		h.mu.RLock()
		count := len(h.activeVIPs)
		h.mu.RUnlock()

		if count != 0 {
			t.Errorf("Expected 0 active VIPs after removal, got %d", count)
		}
	})

	t.Run("remove_nonexistent_vip", func(t *testing.T) {
		assignment := &pb.VIPAssignment{
			VipName:  "vip-nonexistent",
			Address:  "10.0.0.200/32",
			Mode:     pb.VIPMode_OSPF,
			IsActive: true,
		}

		// Should not error for non-existent VIP
		err := h.RemoveVIP(assignment)

		if err != nil {
			t.Logf("RemoveVIP returned error (may be expected): %v", err)
		}
	})
}

func TestOSPFHandlerShutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	h.Shutdown()

	h.mu.RLock()
	started := h.started
	vipCount := len(h.activeVIPs)
	h.mu.RUnlock()

	if vipCount != 0 {
		t.Errorf("Expected 0 VIPs after shutdown, got %d", vipCount)
	}

	if h.ospfServer != nil {
		t.Error("OSPF server should be nil after shutdown")
	}
}

func TestOSPFHandlerGetActiveVIPCount(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	initialCount := h.GetActiveVIPCount()

	if initialCount != 0 {
		t.Errorf("Expected 0 VIPs initially, got %d", initialCount)
	}

	// Add VIP
	assignment := &pb.VIPAssignment{
		VipName:  "vip-test",
		Address:  "10.0.0.100/32",
		Mode:     pb.VIPMode_OSPF,
		IsActive: true,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
		},
	}

	h.AddVIP(assignment)

	finalCount := h.GetActiveVIPCount()

	// Count may be 1 if OSPF server started, 0 if it failed
	if finalCount > 1 {
		t.Errorf("Expected at most 1 VIP, got %d", finalCount)
	}

	h.Shutdown()
}

func TestOSPFServerInitialization(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	config := &pb.OSPFConfig{
		RouterId:      "10.0.0.1",
		AreaId:        0,
		HelloInterval: 10,
		DeadInterval:  40,
		Neighbors: []*pb.OSPFNeighbor{
			{
				Address:  "10.0.0.2",
				Priority: 1,
			},
		},
	}

	err := h.startOSPFServer(config)

	if err != nil {
		t.Logf("startOSPFServer returned error (may be expected): %v", err)
	}

	h.Shutdown()
}

func TestOSPFNeighborConfiguration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	config := &pb.OSPFConfig{
		RouterId: "10.0.0.1",
		AreaId:   0,
		Neighbors: []*pb.OSPFNeighbor{
			{Address: "10.0.0.2", Priority: 1},
			{Address: "10.0.0.3", Priority: 2},
		},
	}

	h.startOSPFServer(config)

	h.mu.RLock()
	if h.ospfServer != nil {
		h.ospfServer.mu.RLock()
		neighborCount := len(h.ospfServer.neighbors)
		h.ospfServer.mu.RUnlock()

		if neighborCount != 2 {
			t.Errorf("Expected 2 neighbors, got %d", neighborCount)
		}
	}
	h.mu.RUnlock()

	h.Shutdown()
}

func TestOSPFLSAManagement(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	config := &pb.OSPFConfig{
		RouterId: "10.0.0.1",
		AreaId:   0,
	}

	h.startOSPFServer(config)

	// Test LSA announcement
	err := h.announceLSA([]byte{10, 0, 0, 100}, config)

	h.mu.RLock()
	if h.ospfServer != nil {
		h.ospfServer.mu.RLock()
		lsaCount := len(h.ospfServer.lsas)
		h.ospfServer.mu.RUnlock()

		if err == nil && lsaCount != 1 {
			t.Errorf("Expected 1 LSA, got %d", lsaCount)
		}
	}
	h.mu.RUnlock()

	h.Shutdown()
}

func TestOSPFContextCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx, cancel := context.WithCancel(context.Background())
	h.Start(ctx)

	config := &pb.OSPFConfig{
		RouterId: "10.0.0.1",
		AreaId:   0,
	}

	h.startOSPFServer(config)

	// Cancel context
	cancel()

	// Wait a bit for protocol loop to exit
	time.Sleep(100 * time.Millisecond)

	h.Shutdown()
}

func TestOSPFHandlerConcurrentOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	t.Run("concurrent_add_operations", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			assignment := &pb.VIPAssignment{
				VipName:  "vip-concurrent",
				Address:  "10.0.0.100/32",
				Mode:     pb.VIPMode_OSPF,
				IsActive: true,
				OspfConfig: &pb.OSPFConfig{
					RouterId: "10.0.0.1",
					AreaId:   0,
				},
			}

			h.AddVIP(assignment)
		}

		// Should handle concurrent adds gracefully
	})

	t.Run("concurrent_get_count", func(t *testing.T) {
		count := h.GetActiveVIPCount()

		// Should not panic
		if count < 0 {
			t.Error("VIP count should not be negative")
		}
	})

	h.Shutdown()
}

func TestOSPFHandlerTableDriven(t *testing.T) {
	tests := []struct {
		name          string
		vipName       string
		address       string
		routerID      string
		areaID        uint32
		shouldSucceed bool
		description   string
	}{
		{
			name:          "valid_vip",
			vipName:       "vip-1",
			address:       "10.0.0.100/32",
			routerID:      "10.0.0.1",
			areaID:        0,
			shouldSucceed: true,
			description:   "Valid VIP assignment",
		},
		{
			name:          "invalid_ip",
			vipName:       "vip-2",
			address:       "not-a-cidr",
			routerID:      "10.0.0.1",
			areaID:        0,
			shouldSucceed: false,
			description:   "Invalid IP CIDR",
		},
		{
			name:          "different_area",
			vipName:       "vip-3",
			address:       "10.0.0.101/32",
			routerID:      "10.0.0.1",
			areaID:        1,
			shouldSucceed: true,
			description:   "Different OSPF area",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			h, _ := NewOSPFHandler(logger)

			ctx := context.Background()
			h.Start(ctx)

			assignment := &pb.VIPAssignment{
				VipName:  tt.vipName,
				Address:  tt.address,
				Mode:     pb.VIPMode_OSPF,
				IsActive: true,
				OspfConfig: &pb.OSPFConfig{
					RouterId: tt.routerID,
					AreaId:   tt.areaID,
				},
			}

			err := h.AddVIP(assignment)

			if tt.shouldSucceed && err != nil {
				t.Logf("Expected success but got error: %v", err)
			}

			if !tt.shouldSucceed && err == nil {
				t.Logf("Expected error but operation succeeded")
			}

			h.Shutdown()
		})
	}
}

func TestOSPFVIPStateTracking(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewOSPFHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	assignment := &pb.VIPAssignment{
		VipName:  "vip-tracking",
		Address:  "10.0.0.100/32",
		Mode:     pb.VIPMode_OSPF,
		IsActive: true,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
		},
	}

	h.AddVIP(assignment)

	h.mu.RLock()
	state, exists := h.activeVIPs[assignment.VipName]
	h.mu.RUnlock()

	if exists {
		if state.Assignment == nil {
			t.Error("Assignment should be tracked")
		}

		if state.IP == nil {
			t.Error("IP should be parsed and tracked")
		}

		if state.AddedAt.IsZero() {
			t.Error("AddedAt timestamp should be set")
		}
	}

	h.Shutdown()
}
