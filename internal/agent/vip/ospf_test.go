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

func TestOSPFHandler_AddRemoveVIP(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewOSPFHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create OSPF handler: %v", err)
	}

	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		t.Fatalf("Failed to start OSPF handler: %v", err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-ospf-vip",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId:      "10.0.0.1",
			AreaId:        0,
			HelloInterval: 10,
			DeadInterval:  40,
			Cost:          20,
		},
	}

	t.Run("add VIP", func(t *testing.T) {
		err := handler.AddVIP(ctx, assignment)
		if err != nil {
			t.Fatalf("Failed to add VIP: %v", err)
		}

		if handler.GetActiveVIPCount() != 1 {
			t.Errorf("Expected 1 active VIP, got %d", handler.GetActiveVIPCount())
		}
	})

	t.Run("add duplicate VIP", func(t *testing.T) {
		err := handler.AddVIP(ctx, assignment)
		if err != nil {
			t.Fatalf("Duplicate add should not error: %v", err)
		}
		if handler.GetActiveVIPCount() != 1 {
			t.Errorf("Expected still 1 active VIP, got %d", handler.GetActiveVIPCount())
		}
	})

	t.Run("LSDB count", func(t *testing.T) {
		count := handler.GetLSDBCount()
		if count != 1 {
			t.Errorf("Expected 1 LSA in LSDB, got %d", count)
		}
	})

	t.Run("neighbor states", func(t *testing.T) {
		states := handler.GetNeighborStates()
		// No neighbors configured in this test
		if len(states) != 0 {
			t.Errorf("Expected 0 neighbor states, got %d", len(states))
		}
	})

	t.Run("remove VIP", func(t *testing.T) {
		err := handler.RemoveVIP(ctx, assignment)
		if err != nil {
			t.Fatalf("Failed to remove VIP: %v", err)
		}

		if handler.GetActiveVIPCount() != 0 {
			t.Errorf("Expected 0 active VIPs, got %d", handler.GetActiveVIPCount())
		}
	})

	t.Run("remove non-existent VIP", func(t *testing.T) {
		err := handler.RemoveVIP(ctx, &pb.VIPAssignment{
			VipName: "non-existent",
			Address: "10.200.0.99/32",
		})
		if err != nil {
			t.Fatalf("Removing non-existent VIP should not error: %v", err)
		}
	})
}

func TestOSPFHandler_IPv6(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewOSPFHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create OSPF handler: %v", err)
	}

	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-ospfv3-vip",
		Address: "2001:db8::100/128",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
			Cost:     15,
		},
	}

	err = handler.AddVIP(ctx, assignment)
	if err != nil {
		t.Fatalf("Failed to add IPv6 VIP: %v", err)
	}

	if handler.GetActiveVIPCount() != 1 {
		t.Errorf("Expected 1 active VIP, got %d", handler.GetActiveVIPCount())
	}

	// Verify LSA is IPv6
	handler.mu.RLock()
	state, exists := handler.activeVIPs["test-ospfv3-vip"]
	handler.mu.RUnlock()

	if !exists {
		t.Fatal("VIP state not found")
	}
	if !state.IsIPv6 {
		t.Error("Expected IPv6 VIP to be marked as IPv6")
	}
}

func TestOSPFHandler_GracefulRestart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewOSPFHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create OSPF handler: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := handler.Start(ctx); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-gr-vip",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId:        "10.0.0.1",
			AreaId:          0,
			GracefulRestart: true,
		},
	}

	err = handler.AddVIP(ctx, assignment)
	if err != nil {
		t.Fatalf("Failed to add VIP: %v", err)
	}

	// Verify graceful restart is configured
	handler.mu.RLock()
	gr := handler.ospfServer.gracefulRestart
	handler.mu.RUnlock()

	if !gr {
		t.Error("Expected graceful restart to be enabled")
	}

	// Cancel context to trigger graceful shutdown
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestOSPFHandler_WithNeighbors(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewOSPFHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create OSPF handler: %v", err)
	}

	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-neighbor-vip",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
			AuthType: "md5",
			AuthKey:  "secret123",
			Neighbors: []*pb.OSPFNeighbor{
				{Address: "10.0.0.2", Priority: 100},
				{Address: "10.0.0.3", Priority: 50},
			},
		},
	}

	err = handler.AddVIP(ctx, assignment)
	if err != nil {
		t.Fatalf("Failed to add VIP: %v", err)
	}

	states := handler.GetNeighborStates()
	if len(states) != 2 {
		t.Errorf("Expected 2 neighbors, got %d", len(states))
	}

	for addr, state := range states {
		if state != ospfNeighborDown {
			t.Errorf("Expected neighbor %s in Down state, got %s", addr, state)
		}
	}
}

func TestOSPFHandler_Shutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewOSPFHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create OSPF handler: %v", err)
	}

	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-shutdown-vip",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
		},
	}

	_ = handler.AddVIP(ctx, assignment)

	handler.Shutdown()

	if handler.GetActiveVIPCount() != 0 {
		t.Errorf("Expected 0 active VIPs after shutdown, got %d", handler.GetActiveVIPCount())
	}
}

func TestOSPFHandler_MissingConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewOSPFHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create OSPF handler: %v", err)
	}

	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	assignment := &pb.VIPAssignment{
		VipName:    "test-no-config",
		Address:    "10.200.0.1/32",
		Mode:       pb.VIPMode_OSPF,
		OspfConfig: nil,
	}

	err = handler.AddVIP(ctx, assignment)
	if err == nil {
		t.Error("Expected error when OSPF config is nil")
	}
}

func TestOSPFHandler_InvalidAddress(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewOSPFHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create OSPF handler: %v", err)
	}

	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-invalid-addr",
		Address: "invalid-address",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
		},
	}

	err = handler.AddVIP(ctx, assignment)
	if err == nil {
		t.Error("Expected error when address is invalid")
	}
}

func TestOSPFHandler_Reconfigure(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewOSPFHandler(logger)
	ctx := context.Background()
	_ = handler.Start(ctx)

	originalAssignment := &pb.VIPAssignment{
		VipName: "test-reconfig-ospf",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId:      "10.0.0.1",
			AreaId:        0,
			HelloInterval: 10,
			DeadInterval:  40,
			Cost:          10,
		},
	}

	_ = handler.AddVIP(ctx, originalAssignment)

	// Modify OSPF config
	modifiedAssignment := &pb.VIPAssignment{
		VipName: "test-reconfig-ospf",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId:      "10.0.0.1",
			AreaId:        0,
			HelloInterval: 20, // Changed
			DeadInterval:  80, // Changed
			Cost:          20, // Changed
		},
	}

	err := handler.AddVIP(ctx, modifiedAssignment)
	if err != nil {
		t.Fatalf("Reconfiguration failed: %v", err)
	}

	handler.mu.RLock()
	state := handler.activeVIPs["test-reconfig-ospf"]
	handler.mu.RUnlock()

	if state.ospfConfig.HelloInterval != 20 {
		t.Errorf("Expected HelloInterval 20, got %d", state.ospfConfig.HelloInterval)
	}
	if state.ospfConfig.Cost != 20 {
		t.Errorf("Expected Cost 20, got %d", state.ospfConfig.Cost)
	}
}

func TestOSPFHandler_Reconfigure_RouterIDChange(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewOSPFHandler(logger)
	ctx := context.Background()
	_ = handler.Start(ctx)

	originalAssignment := &pb.VIPAssignment{
		VipName: "test-router-id-change",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
		},
	}

	_ = handler.AddVIP(ctx, originalAssignment)

	// Change Router ID (requires server restart)
	modifiedAssignment := &pb.VIPAssignment{
		VipName: "test-router-id-change",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.2", // Changed
			AreaId:   0,
		},
	}

	err := handler.AddVIP(ctx, modifiedAssignment)
	if err != nil {
		t.Fatalf("Reconfiguration with RouterID change failed: %v", err)
	}

	handler.mu.RLock()
	state := handler.activeVIPs["test-router-id-change"]
	handler.mu.RUnlock()

	if state.ospfConfig.RouterId != "10.0.0.2" {
		t.Errorf("Expected RouterId 10.0.0.2, got %s", state.ospfConfig.RouterId)
	}
}

func TestOSPFHandler_Reconfigure_NeighborsChange(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewOSPFHandler(logger)
	ctx := context.Background()
	_ = handler.Start(ctx)

	originalAssignment := &pb.VIPAssignment{
		VipName: "test-neighbors-change",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
			Neighbors: []*pb.OSPFNeighbor{
				{Address: "10.0.0.2", Priority: 100},
			},
		},
	}

	_ = handler.AddVIP(ctx, originalAssignment)

	// Modify neighbors
	modifiedAssignment := &pb.VIPAssignment{
		VipName: "test-neighbors-change",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
			Neighbors: []*pb.OSPFNeighbor{
				{Address: "10.0.0.2", Priority: 100},
				{Address: "10.0.0.3", Priority: 50}, // Added neighbor
			},
		},
	}

	err := handler.AddVIP(ctx, modifiedAssignment)
	if err != nil {
		t.Fatalf("Reconfiguration with neighbor change failed: %v", err)
	}

	states := handler.GetNeighborStates()
	if len(states) != 2 {
		t.Errorf("Expected 2 neighbors after reconfiguration, got %d", len(states))
	}
}

func TestOSPFHandler_MultipleVIPs(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewOSPFHandler(logger)
	ctx := context.Background()
	_ = handler.Start(ctx)

	vips := []*pb.VIPAssignment{
		{
			VipName: "ospf-vip1",
			Address: "10.200.0.1/32",
			Mode:    pb.VIPMode_OSPF,
			OspfConfig: &pb.OSPFConfig{
				RouterId: "10.0.0.1",
				AreaId:   0,
				Cost:     10,
			},
		},
		{
			VipName: "ospf-vip2",
			Address: "10.200.0.2/32",
			Mode:    pb.VIPMode_OSPF,
			OspfConfig: &pb.OSPFConfig{
				RouterId: "10.0.0.1",
				AreaId:   0,
				Cost:     20,
			},
		},
		{
			VipName: "ospf-vip3",
			Address: "2001:db8::200/128",
			Mode:    pb.VIPMode_OSPF,
			OspfConfig: &pb.OSPFConfig{
				RouterId: "10.0.0.1",
				AreaId:   0,
				Cost:     15,
			},
		},
	}

	// Add all VIPs
	for _, vip := range vips {
		if err := handler.AddVIP(ctx, vip); err != nil {
			t.Fatalf("Failed to add VIP %s: %v", vip.VipName, err)
		}
	}

	if handler.GetActiveVIPCount() != 3 {
		t.Errorf("Expected 3 active VIPs, got %d", handler.GetActiveVIPCount())
	}

	// Verify LSDB has entries for all VIPs
	lsdbCount := handler.GetLSDBCount()
	if lsdbCount < 3 {
		t.Errorf("Expected at least 3 LSAs in LSDB, got %d", lsdbCount)
	}

	// Remove one VIP
	if err := handler.RemoveVIP(ctx, vips[1]); err != nil {
		t.Fatalf("Failed to remove VIP: %v", err)
	}

	if handler.GetActiveVIPCount() != 2 {
		t.Errorf("Expected 2 active VIPs after removal, got %d", handler.GetActiveVIPCount())
	}
}

func TestOSPFHandler_DifferentAreaIDs(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewOSPFHandler(logger)
	ctx := context.Background()
	_ = handler.Start(ctx)

	vips := []*pb.VIPAssignment{
		{
			VipName: "area0-vip",
			Address: "10.200.0.1/32",
			Mode:    pb.VIPMode_OSPF,
			OspfConfig: &pb.OSPFConfig{
				RouterId: "10.0.0.1",
				AreaId:   0, // Backbone area
			},
		},
		{
			VipName: "area1-vip",
			Address: "10.200.0.2/32",
			Mode:    pb.VIPMode_OSPF,
			OspfConfig: &pb.OSPFConfig{
				RouterId: "10.0.0.1",
				AreaId:   1, // Non-backbone area
			},
		},
	}

	for _, vip := range vips {
		if err := handler.AddVIP(ctx, vip); err != nil {
			t.Fatalf("Failed to add VIP %s: %v", vip.VipName, err)
		}
	}

	if handler.GetActiveVIPCount() != 2 {
		t.Errorf("Expected 2 active VIPs with different areas, got %d", handler.GetActiveVIPCount())
	}
}

func TestOSPFHandler_HighCostRoutes(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewOSPFHandler(logger)
	ctx := context.Background()
	_ = handler.Start(ctx)

	tests := []struct {
		name string
		cost uint32
	}{
		{"low cost", 1},
		{"default cost", 10},
		{"high cost", 1000},
		{"very high cost", 65535},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assignment := &pb.VIPAssignment{
				VipName: "test-cost-" + tt.name,
				Address: "10.200.0.1/32",
				Mode:    pb.VIPMode_OSPF,
				OspfConfig: &pb.OSPFConfig{
					RouterId: "10.0.0.1",
					AreaId:   0,
					Cost:     tt.cost,
				},
			}

			// Remove previous VIP with same address
			handler.mu.Lock()
			handler.activeVIPs = make(map[string]*OSPFVIPState)
			handler.mu.Unlock()

			err := handler.AddVIP(ctx, assignment)
			if err != nil {
				t.Fatalf("Failed to add VIP with cost %d: %v", tt.cost, err)
			}

			handler.mu.RLock()
			state := handler.activeVIPs["test-cost-"+tt.name]
			handler.mu.RUnlock()

			if state.ospfConfig.Cost != tt.cost {
				t.Errorf("Expected cost %d, got %d", tt.cost, state.ospfConfig.Cost)
			}
		})
	}
}

func TestOSPFHandler_HelloDeadIntervals(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewOSPFHandler(logger)
	ctx := context.Background()
	_ = handler.Start(ctx)

	assignment := &pb.VIPAssignment{
		VipName: "test-intervals",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId:      "10.0.0.1",
			AreaId:        0,
			HelloInterval: 5,
			DeadInterval:  20,
		},
	}

	err := handler.AddVIP(ctx, assignment)
	if err != nil {
		t.Fatalf("Failed to add VIP with custom intervals: %v", err)
	}

	handler.mu.RLock()
	state := handler.activeVIPs["test-intervals"]
	handler.mu.RUnlock()

	if state.ospfConfig.HelloInterval != 5 {
		t.Errorf("Expected HelloInterval 5, got %d", state.ospfConfig.HelloInterval)
	}
	if state.ospfConfig.DeadInterval != 20 {
		t.Errorf("Expected DeadInterval 20, got %d", state.ospfConfig.DeadInterval)
	}
}

func TestOSPFHandler_AddedAtTimestamp(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewOSPFHandler(logger)
	ctx := context.Background()
	_ = handler.Start(ctx)

	beforeAdd := time.Now()

	assignment := &pb.VIPAssignment{
		VipName: "test-timestamp-ospf",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_OSPF,
		OspfConfig: &pb.OSPFConfig{
			RouterId: "10.0.0.1",
			AreaId:   0,
		},
	}

	_ = handler.AddVIP(ctx, assignment)

	afterAdd := time.Now()

	handler.mu.RLock()
	state := handler.activeVIPs["test-timestamp-ospf"]
	handler.mu.RUnlock()

	if state.AddedAt.Before(beforeAdd) || state.AddedAt.After(afterAdd) {
		t.Errorf("AddedAt timestamp not within expected range")
	}

	// Remove and check duration
	_ = handler.RemoveVIP(ctx, assignment)

	duration := time.Since(state.AddedAt)
	if duration < 0 {
		t.Error("Duration should be non-negative")
	}
}

func TestOSPFHandler_AuthenticationTypes(t *testing.T) {
	tests := []struct {
		name     string
		authType string
		authKey  string
	}{
		{
			name:     "MD5 authentication",
			authType: "md5",
			authKey:  "secret123",
		},
		{
			name:     "cleartext authentication",
			authType: "cleartext",
			authKey:  "password",
		},
		{
			name:     "no authentication",
			authType: "",
			authKey:  "",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fresh handler for each test to avoid state pollution
			logger := zaptest.NewLogger(t)
			handler, _ := NewOSPFHandler(logger)
			ctx := context.Background()
			_ = handler.Start(ctx)

			assignment := &pb.VIPAssignment{
				VipName: "test-auth-" + string(rune('a'+i)),
				Address: "10.200.0." + string(rune('1'+i)) + "/32",
				Mode:    pb.VIPMode_OSPF,
				OspfConfig: &pb.OSPFConfig{
					RouterId: "10.0.0.1",
					AreaId:   0,
					AuthType: tt.authType,
					AuthKey:  tt.authKey,
				},
			}

			err := handler.AddVIP(ctx, assignment)
			if err != nil {
				t.Fatalf("Failed to add VIP with %s: %v", tt.name, err)
			}

			// Verify auth settings were applied
			handler.mu.RLock()
			if handler.ospfServer != nil {
				if handler.ospfServer.authType != tt.authType {
					t.Errorf("Expected authType %s, got %s", tt.authType, handler.ospfServer.authType)
				}
			}
			handler.mu.RUnlock()
		})
	}
}
