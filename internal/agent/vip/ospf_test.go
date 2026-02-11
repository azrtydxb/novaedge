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
