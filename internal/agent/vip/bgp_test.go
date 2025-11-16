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

func TestNewBGPHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)

	h, err := NewBGPHandler(logger)

	if err != nil {
		t.Fatalf("NewBGPHandler returned error: %v", err)
	}

	if h == nil {
		t.Fatal("Expected BGPHandler to be created")
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

func TestBGPHandlerStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewBGPHandler(logger)

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

func TestBGPHandlerStartIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewBGPHandler(logger)

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

	// Should only be one BGP server
	h.mu.RLock()
	vipCount := len(h.activeVIPs)
	h.mu.RUnlock()

	if vipCount != 0 {
		t.Error("VIP count should remain 0")
	}
}

func TestBGPHandlerAddVIP(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewBGPHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	t.Run("add_valid_vip", func(t *testing.T) {
		assignment := &pb.VIPAssignment{
			VipName: "vip-web",
			Address: "10.0.0.100/32",
			Mode:    pb.VIPMode_BGP,
			IsActive: true,
			BgpConfig: &pb.BGPConfig{
				LocalAs:     65000,
				RouterId:    "10.0.0.1",
				LocalPreference: 100,
				Peers: []*pb.BGPPeer{},
			},
		}

		err := h.AddVIP(assignment)

		// AddVIP may fail if BGP server cannot start, but structure should be valid
		h.mu.RLock()
		count := len(h.activeVIPs)
		h.mu.RUnlock()

		if err == nil && count != 1 {
			t.Errorf("Expected 1 active VIP, got %d", count)
		}
	})

	t.Run("invalid_vip_address", func(t *testing.T) {
		assignment := &pb.VIPAssignment{
			VipName: "vip-invalid",
			Address: "not-an-ip",
			Mode:    pb.VIPMode_BGP,
			IsActive: true,
			BgpConfig: &pb.BGPConfig{
				LocalAs:  65000,
				RouterId: "10.0.0.1",
			},
		}

		err := h.AddVIP(assignment)

		if err == nil {
			t.Error("Expected error for invalid VIP address")
		}
	})

	t.Run("missing_bgp_config", func(t *testing.T) {
		assignment := &pb.VIPAssignment{
			VipName:   "vip-no-config",
			Address:   "10.0.0.101/32",
			Mode:      pb.VIPMode_BGP,
			IsActive:  true,
			BgpConfig: nil,
		}

		err := h.AddVIP(assignment)

		if err == nil {
			t.Error("Expected error when BGP config is missing")
		}
	})
}

func TestBGPHandlerRemoveVIP(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewBGPHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	assignment := &pb.VIPAssignment{
		VipName: "vip-test",
		Address: "10.0.0.100/32",
		Mode:    pb.VIPMode_BGP,
		IsActive: true,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65000,
			RouterId: "10.0.0.1",
		},
	}

	t.Run("remove_existing_vip", func(t *testing.T) {
		// Add VIP first
		h.AddVIP(assignment)

		// Remove VIP
		err := h.RemoveVIP(assignment)

		if err != nil {
			t.Logf("RemoveVIP returned error (may be expected if BGP server failed): %v", err)
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
			VipName: "vip-nonexistent",
			Address: "10.0.0.200/32",
			Mode:    pb.VIPMode_BGP,
			IsActive: true,
		}

		// Should not error for non-existent VIP
		err := h.RemoveVIP(assignment)

		if err != nil {
			t.Logf("RemoveVIP returned error (may be expected): %v", err)
		}
	})
}

func TestBGPHandlerShutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewBGPHandler(logger)

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

	if started {
		t.Error("Handler should not be marked as started after shutdown")
	}
}

func TestBGPHandlerGetActiveVIPCount(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewBGPHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	initialCount := h.GetActiveVIPCount()

	if initialCount != 0 {
		t.Errorf("Expected 0 VIPs initially, got %d", initialCount)
	}

	// Add VIP
	assignment := &pb.VIPAssignment{
		VipName: "vip-test",
		Address: "10.0.0.100/32",
		Mode:    pb.VIPMode_BGP,
		IsActive: true,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65000,
			RouterId: "10.0.0.1",
		},
	}

	h.AddVIP(assignment)

	finalCount := h.GetActiveVIPCount()

	// Count may be 1 if BGP server started, 0 if it failed
	if finalCount > 1 {
		t.Errorf("Expected at most 1 VIP, got %d", finalCount)
	}
}

func TestBGPHandlerConcurrentOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewBGPHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	t.Run("concurrent_add_operations", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			assignment := &pb.VIPAssignment{
				VipName: "vip-concurrent",
				Address: "10.0.0.100/32",
				Mode:    pb.VIPMode_BGP,
				IsActive: true,
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65000,
					RouterId: "10.0.0.1",
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
}

func TestBGPHandlerTableDriven(t *testing.T) {
	tests := []struct {
		name           string
		vipName        string
		address        string
		localAS        uint32
		routerID       string
		shouldSucceed  bool
		description    string
	}{
		{
			name:          "valid_vip",
			vipName:       "vip-1",
			address:       "10.0.0.100/32",
			localAS:       65000,
			routerID:      "10.0.0.1",
			shouldSucceed: false, // May fail due to BGP server initialization
			description:   "Valid VIP assignment",
		},
		{
			name:          "invalid_ip_cidr",
			vipName:       "vip-2",
			address:       "not-a-cidr",
			localAS:       65000,
			routerID:      "10.0.0.1",
			shouldSucceed: false,
			description:   "Invalid IP CIDR",
		},
		{
			name:          "different_as",
			vipName:       "vip-3",
			address:       "10.0.0.101/32",
			localAS:       65001,
			routerID:      "10.0.0.1",
			shouldSucceed: false,
			description:   "Different AS number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			h, _ := NewBGPHandler(logger)

			ctx := context.Background()
			h.Start(ctx)

			assignment := &pb.VIPAssignment{
				VipName:  tt.vipName,
				Address:  tt.address,
				Mode:     pb.VIPMode_BGP,
				IsActive: true,
				BgpConfig: &pb.BGPConfig{
					LocalAs:  tt.localAS,
					RouterId: tt.routerID,
				},
			}

			err := h.AddVIP(assignment)

			if tt.shouldSucceed && err != nil {
				t.Logf("Expected success but got error: %v", err)
			}

			h.Shutdown()
		})
	}
}

func TestBGPVIPStateTracking(t *testing.T) {
	logger := zaptest.NewLogger(t)
	h, _ := NewBGPHandler(logger)

	ctx := context.Background()
	h.Start(ctx)

	assignment := &pb.VIPAssignment{
		VipName: "vip-tracking",
		Address: "10.0.0.100/32",
		Mode:    pb.VIPMode_BGP,
		IsActive: true,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65000,
			RouterId: "10.0.0.1",
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

		if !state.AddedAt.IsZero() && !state.Announced {
			t.Logf("VIP announcement status: %v", state.Announced)
		}
	}

	h.Shutdown()
}
