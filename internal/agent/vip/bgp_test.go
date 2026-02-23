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
	"net"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// skipIfBGPUnavailable skips the test if the error indicates the BGP server
// cannot start (privileged port 179 requires root, or port already in use).
// This allows tests to pass in unprivileged CI and when tests share a port.
func skipIfBGPUnavailable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "bind:") {
		t.Skipf("Skipping: BGP server unavailable: %v", err)
	}
}

func TestBGPHandler_NewBGPHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewBGPHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create BGP handler: %v", err)
	}

	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}
	if handler.bfdManager == nil {
		t.Error("Expected BFD manager to be initialized")
	}
	if handler.activeVIPs == nil {
		t.Error("Expected activeVIPs map to be initialized")
	}
	if handler.started {
		t.Error("Expected handler to not be started initially")
	}
}

func TestBGPHandler_Start(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewBGPHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create BGP handler: %v", err)
	}

	ctx := context.Background()

	t.Run("start handler", func(t *testing.T) {
		err := handler.Start(ctx)
		if err != nil {
			skipIfBGPUnavailable(t, err)
			t.Fatalf("Failed to start handler: %v", err)
		}
		if !handler.started {
			t.Error("Expected handler to be marked as started")
		}
	})

	t.Run("start already started handler", func(t *testing.T) {
		err := handler.Start(ctx)
		if err != nil {
			t.Errorf("Starting already started handler should not error: %v", err)
		}
	})
}

func TestBGPHandler_AddVIP_MissingConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewBGPHandler(logger)
	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
	}

	tests := []struct {
		name        string
		assignment  *pb.VIPAssignment
		wantErr     bool
		errContains string
	}{
		{
			name: "missing BGP config",
			assignment: &pb.VIPAssignment{
				VipName:   "test-vip",
				Address:   "10.0.0.1/32",
				Mode:      pb.VIPMode_BGP,
				BgpConfig: nil,
			},
			wantErr:     true,
			errContains: "BGP config is required",
		},
		{
			name: "invalid IP address",
			assignment: &pb.VIPAssignment{
				VipName: "test-vip",
				Address: "invalid-ip",
				Mode:    pb.VIPMode_BGP,
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65001,
					RouterId: "10.0.0.1",
				},
			},
			wantErr:     true,
			errContains: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.AddVIP(ctx, tt.assignment)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddVIP() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBGPHandler_AddRemoveVIP(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewBGPHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create BGP handler: %v", err)
	}

	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
		t.Fatalf("Failed to start handler: %v", err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-bgp-vip",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
			Peers: []*pb.BGPPeer{
				{Address: "10.0.0.2", As: 65002, Port: 179},
			},
		},
	}

	t.Run("add VIP", func(t *testing.T) {
		err := handler.AddVIP(ctx, assignment)
		skipIfBGPUnavailable(t, err)
		if err != nil {
			t.Fatalf("Failed to add VIP: %v", err)
		}

		handler.mu.RLock()
		state, exists := handler.activeVIPs["test-bgp-vip"]
		handler.mu.RUnlock()

		if !exists {
			t.Fatal("VIP not found in activeVIPs")
		}
		if state.IP.String() != "10.100.0.1" {
			t.Errorf("Expected IP 10.100.0.1, got %s", state.IP.String())
		}
		if !state.Announced {
			t.Error("Expected VIP to be announced")
		}
		if state.IsIPv6 {
			t.Error("Expected IPv4 VIP, got IPv6")
		}
	})

	t.Run("add duplicate VIP triggers reconfiguration", func(t *testing.T) {
		modifiedAssignment := &pb.VIPAssignment{
			VipName: "test-bgp-vip",
			Address: "10.100.0.1/32",
			Mode:    pb.VIPMode_BGP,
			BgpConfig: &pb.BGPConfig{
				LocalAs:  65001,
				RouterId: "10.0.0.1",
				Peers: []*pb.BGPPeer{
					{Address: "10.0.0.2", As: 65002, Port: 179},
				},
				LocalPreference: 150,
			},
		}
		err := handler.AddVIP(ctx, modifiedAssignment)
		if err != nil {
			t.Fatalf("Reconfiguring VIP should not error: %v", err)
		}
	})

	t.Run("remove VIP", func(t *testing.T) {
		err := handler.RemoveVIP(ctx, assignment)
		if err != nil {
			t.Fatalf("Failed to remove VIP: %v", err)
		}

		handler.mu.RLock()
		_, exists := handler.activeVIPs["test-bgp-vip"]
		handler.mu.RUnlock()

		if exists {
			t.Error("VIP should be removed from activeVIPs")
		}
	})

	t.Run("remove non-existent VIP", func(t *testing.T) {
		err := handler.RemoveVIP(ctx, &pb.VIPAssignment{
			VipName: "non-existent",
			Address: "10.100.0.99/32",
		})
		if err != nil {
			t.Errorf("Removing non-existent VIP should not error: %v", err)
		}
	})
}

func TestBGPHandler_IPv6(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewBGPHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create BGP handler: %v", err)
	}

	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
		t.Fatalf("Failed to start: %v", err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-bgp-ipv6",
		Address: "2001:db8::100/128",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
			Peers: []*pb.BGPPeer{
				{Address: "2001:db8::2", As: 65002},
			},
		},
	}

	err = handler.AddVIP(ctx, assignment)
	skipIfBGPUnavailable(t, err)
	if err != nil {
		t.Fatalf("Failed to add IPv6 VIP: %v", err)
	}

	handler.mu.RLock()
	state, exists := handler.activeVIPs["test-bgp-ipv6"]
	handler.mu.RUnlock()

	if !exists {
		t.Fatal("IPv6 VIP not found")
	}
	if !state.IsIPv6 {
		t.Error("Expected IPv6 VIP to be marked as IPv6")
	}
	if state.IP.String() != "2001:db8::100" {
		t.Errorf("Expected IP 2001:db8::100, got %s", state.IP.String())
	}
}

func TestBGPHandler_WithBFD(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewBGPHandler(logger)
	if err != nil {
		t.Fatalf("Failed to create BGP handler: %v", err)
	}

	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
		t.Fatalf("Failed to start: %v", err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-bgp-bfd",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
			Peers: []*pb.BGPPeer{
				{Address: "10.0.0.2", As: 65002},
				{Address: "10.0.0.3", As: 65003},
			},
		},
		BfdConfig: &pb.BFDConfig{
			Enabled:               true,
			DetectMultiplier:      3,
			DesiredMinTxInterval:  "300ms",
			RequiredMinRxInterval: "300ms",
		},
	}

	err = handler.AddVIP(ctx, assignment)
	if err != nil {
		skipIfBGPUnavailable(t, err)
		t.Fatalf("Failed to add VIP with BFD: %v", err)
	}

	// Verify BFD sessions were created
	sessionCount := handler.bfdManager.GetSessionCount()
	if sessionCount != 2 {
		t.Errorf("Expected 2 BFD sessions, got %d", sessionCount)
	}

	// Verify BFD session states
	peer1IP := net.ParseIP("10.0.0.2")
	peer2IP := net.ParseIP("10.0.0.3")

	state1 := handler.bfdManager.GetSessionState(peer1IP)
	state2 := handler.bfdManager.GetSessionState(peer2IP)

	if state1 == BFDStateAdminDown {
		t.Error("Expected BFD session 1 to not be AdminDown")
	}
	if state2 == BFDStateAdminDown {
		t.Error("Expected BFD session 2 to not be AdminDown")
	}

	// Remove VIP and verify BFD sessions are cleaned up
	err = handler.RemoveVIP(ctx, assignment)
	if err != nil {
		t.Fatalf("Failed to remove VIP: %v", err)
	}

	sessionCount = handler.bfdManager.GetSessionCount()
	if sessionCount != 0 {
		t.Errorf("Expected 0 BFD sessions after removal, got %d", sessionCount)
	}
}

func TestBGPHandler_Reconfigure_ASNChange(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewBGPHandler(logger)
	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
	}

	originalAssignment := &pb.VIPAssignment{
		VipName: "test-reconfig",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
		},
	}

	// Add original VIP
	_ = handler.AddVIP(ctx, originalAssignment)

	// Modify ASN
	modifiedAssignment := &pb.VIPAssignment{
		VipName: "test-reconfig",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65002, // Changed ASN
			RouterId: "10.0.0.1",
		},
	}

	err := handler.AddVIP(ctx, modifiedAssignment)
	if err != nil {
		skipIfBGPUnavailable(t, err)
		t.Fatalf("Reconfiguration with ASN change failed: %v", err)
	}

	handler.mu.RLock()
	state := handler.activeVIPs["test-reconfig"]
	handler.mu.RUnlock()

	if state.bgpConfig.LocalAs != 65002 {
		t.Errorf("Expected ASN 65002, got %d", state.bgpConfig.LocalAs)
	}
}

func TestBGPHandler_Reconfigure_RouteAttributes(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewBGPHandler(logger)
	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
	}

	originalAssignment := &pb.VIPAssignment{
		VipName: "test-route-attrs",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:         65001,
			RouterId:        "10.0.0.1",
			LocalPreference: 100,
			Communities:     []string{"65001:100"},
		},
	}

	_ = handler.AddVIP(ctx, originalAssignment)

	// Modify route attributes
	modifiedAssignment := &pb.VIPAssignment{
		VipName: "test-route-attrs",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:         65001,
			RouterId:        "10.0.0.1",
			LocalPreference: 150,                   // Changed
			Communities:     []string{"65001:200"}, // Changed
		},
	}

	err := handler.AddVIP(ctx, modifiedAssignment)
	if err != nil {
		skipIfBGPUnavailable(t, err)
		t.Fatalf("Reconfiguration with attribute change failed: %v", err)
	}

	handler.mu.RLock()
	state := handler.activeVIPs["test-route-attrs"]
	handler.mu.RUnlock()

	if state.bgpConfig.LocalPreference != 150 {
		t.Errorf("Expected LocalPreference 150, got %d", state.bgpConfig.LocalPreference)
	}
	if len(state.bgpConfig.Communities) != 1 || state.bgpConfig.Communities[0] != "65001:200" {
		t.Errorf("Expected communities [65001:200], got %v", state.bgpConfig.Communities)
	}
}

func TestBGPHandler_Reconfigure_BFD(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewBGPHandler(logger)
	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
	}

	tests := []struct {
		name         string
		initial      *pb.BFDConfig
		updated      *pb.BFDConfig
		wantSessions int
	}{
		{
			name:    "enable BFD",
			initial: nil,
			updated: &pb.BFDConfig{
				Enabled:          true,
				DetectMultiplier: 3,
			},
			wantSessions: 1,
		},
		{
			name: "disable BFD",
			initial: &pb.BFDConfig{
				Enabled:          true,
				DetectMultiplier: 3,
			},
			updated:      nil,
			wantSessions: 0,
		},
		{
			name: "update BFD parameters",
			initial: &pb.BFDConfig{
				Enabled:              true,
				DetectMultiplier:     3,
				DesiredMinTxInterval: "300ms",
			},
			updated: &pb.BFDConfig{
				Enabled:              true,
				DetectMultiplier:     5,
				DesiredMinTxInterval: "500ms",
			},
			wantSessions: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset handler state
			handler.mu.Lock()
			handler.activeVIPs = make(map[string]*BGPVIPState)
			handler.mu.Unlock()

			// Add initial VIP
			initialAssignment := &pb.VIPAssignment{
				VipName: "test-bfd-reconfig",
				Address: "10.100.0.1/32",
				Mode:    pb.VIPMode_BGP,
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65001,
					RouterId: "10.0.0.1",
					Peers:    []*pb.BGPPeer{{Address: "10.0.0.2", As: 65002}},
				},
				BfdConfig: tt.initial,
			}
			if err := handler.AddVIP(ctx, initialAssignment); err != nil {
				skipIfBGPUnavailable(t, err)
			}

			// Update with new BFD config
			updatedAssignment := &pb.VIPAssignment{
				VipName: "test-bfd-reconfig",
				Address: "10.100.0.1/32",
				Mode:    pb.VIPMode_BGP,
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65001,
					RouterId: "10.0.0.1",
					Peers:    []*pb.BGPPeer{{Address: "10.0.0.2", As: 65002}},
				},
				BfdConfig: tt.updated,
			}
			_ = handler.AddVIP(ctx, updatedAssignment)

			sessionCount := handler.bfdManager.GetSessionCount()
			if sessionCount != tt.wantSessions {
				t.Errorf("Expected %d BFD sessions, got %d", tt.wantSessions, sessionCount)
			}

			// Cleanup
			_ = handler.RemoveVIP(ctx, updatedAssignment)
		})
	}
}

func TestBGPHandler_BFDNeighborDown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewBGPHandler(logger)
	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-bfd-failover",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
			Peers: []*pb.BGPPeer{
				{Address: "10.0.0.2", As: 65002},
			},
		},
		BfdConfig: &pb.BFDConfig{
			Enabled:          true,
			DetectMultiplier: 3,
		},
	}

	err := handler.AddVIP(ctx, assignment)
	if err != nil {
		t.Skip("BGP server requires elevated permissions, skipping BFD test")
	}

	// Simulate BFD neighbor down
	peerIP := net.ParseIP("10.0.0.2")
	handler.onBFDNeighborDown(peerIP)

	// Verify VIP state still exists - the VIP itself is not removed, only routes withdrawn
	handler.mu.RLock()
	_, exists := handler.activeVIPs["test-bfd-failover"]
	handler.mu.RUnlock()

	if !exists {
		t.Error("VIP state should still exist after BFD neighbor down")
	}
}

func TestBGPHandler_BFDNeighborUp(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewBGPHandler(logger)
	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-bfd-recovery",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
			Peers: []*pb.BGPPeer{
				{Address: "10.0.0.2", As: 65002},
			},
		},
		BfdConfig: &pb.BFDConfig{
			Enabled:          true,
			DetectMultiplier: 3,
		},
	}

	err := handler.AddVIP(ctx, assignment)
	if err != nil {
		t.Skip("BGP server requires elevated permissions, skipping BFD test")
	}

	// Simulate BFD neighbor down then up
	peerIP := net.ParseIP("10.0.0.2")
	handler.onBFDNeighborDown(peerIP)
	handler.onBFDNeighborUp(peerIP)

	// Verify VIP is still active
	handler.mu.RLock()
	_, exists := handler.activeVIPs["test-bfd-recovery"]
	handler.mu.RUnlock()

	if !exists {
		t.Error("VIP should still exist after BFD recovery")
	}
}

func TestBGPHandler_MultipleVIPs(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewBGPHandler(logger)
	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
	}

	vips := []*pb.VIPAssignment{
		{
			VipName: "vip1",
			Address: "10.100.0.1/32",
			Mode:    pb.VIPMode_BGP,
			BgpConfig: &pb.BGPConfig{
				LocalAs:  65001,
				RouterId: "10.0.0.1",
			},
		},
		{
			VipName: "vip2",
			Address: "10.100.0.2/32",
			Mode:    pb.VIPMode_BGP,
			BgpConfig: &pb.BGPConfig{
				LocalAs:  65001,
				RouterId: "10.0.0.1",
			},
		},
		{
			VipName: "vip3",
			Address: "2001:db8::100/128",
			Mode:    pb.VIPMode_BGP,
			BgpConfig: &pb.BGPConfig{
				LocalAs:  65001,
				RouterId: "10.0.0.1",
			},
		},
	}

	// Add all VIPs
	for _, vip := range vips {
		if err := handler.AddVIP(ctx, vip); err != nil {
			skipIfBGPUnavailable(t, err)
			t.Fatalf("Failed to add VIP %s: %v", vip.VipName, err)
		}
	}

	handler.mu.RLock()
	activeCount := len(handler.activeVIPs)
	handler.mu.RUnlock()

	if activeCount != 3 {
		t.Errorf("Expected 3 active VIPs, got %d", activeCount)
	}

	// Verify IPv4 and IPv6 are correctly identified
	handler.mu.RLock()
	vip1 := handler.activeVIPs["vip1"]
	vip2 := handler.activeVIPs["vip2"]
	vip3 := handler.activeVIPs["vip3"]
	handler.mu.RUnlock()

	if vip1.IsIPv6 || vip2.IsIPv6 {
		t.Error("Expected vip1 and vip2 to be IPv4")
	}
	if !vip3.IsIPv6 {
		t.Error("Expected vip3 to be IPv6")
	}

	// Remove one VIP
	if err := handler.RemoveVIP(ctx, vips[1]); err != nil {
		t.Fatalf("Failed to remove VIP: %v", err)
	}

	handler.mu.RLock()
	activeCount = len(handler.activeVIPs)
	handler.mu.RUnlock()

	if activeCount != 2 {
		t.Errorf("Expected 2 active VIPs after removal, got %d", activeCount)
	}
}

func TestBGPHandler_Shutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewBGPHandler(logger)
	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-shutdown",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
		},
	}

	_ = handler.AddVIP(ctx, assignment)

	handler.Shutdown()

	handler.mu.RLock()
	activeCount := len(handler.activeVIPs)
	handler.mu.RUnlock()

	if activeCount != 0 {
		t.Errorf("Expected 0 active VIPs after shutdown, got %d", activeCount)
	}
}

func TestBGPHandler_GetActiveVIPCount(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewBGPHandler(logger)
	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
	}

	if count := handler.GetActiveVIPCount(); count != 0 {
		t.Errorf("Expected 0 active VIPs initially, got %d", count)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-count",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
		},
	}

	if err := handler.AddVIP(ctx, assignment); err != nil {
		skipIfBGPUnavailable(t, err)
		t.Fatalf("Failed to add VIP: %v", err)
	}

	if count := handler.GetActiveVIPCount(); count != 1 {
		t.Errorf("Expected 1 active VIP, got %d", count)
	}
}

func TestCommunitiesEqual(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "both empty",
			a:    []string{},
			b:    []string{},
			want: true,
		},
		{
			name: "identical communities",
			a:    []string{"65001:100", "65001:200"},
			b:    []string{"65001:100", "65001:200"},
			want: true,
		},
		{
			name: "different length",
			a:    []string{"65001:100"},
			b:    []string{"65001:100", "65001:200"},
			want: false,
		},
		{
			name: "different values",
			a:    []string{"65001:100"},
			b:    []string{"65001:200"},
			want: false,
		},
		{
			name: "different order",
			a:    []string{"65001:100", "65001:200"},
			b:    []string{"65001:200", "65001:100"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := communitiesEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("communitiesEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBGPHandler_InvalidBFDInterval(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewBGPHandler(logger)
	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
	}

	assignment := &pb.VIPAssignment{
		VipName: "test-invalid-bfd",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
			Peers:    []*pb.BGPPeer{{Address: "10.0.0.2", As: 65002}},
		},
		BfdConfig: &pb.BFDConfig{
			Enabled:               true,
			DetectMultiplier:      3,
			DesiredMinTxInterval:  "invalid-duration",
			RequiredMinRxInterval: "also-invalid",
		},
	}

	// Should not fail, but use defaults
	err := handler.AddVIP(ctx, assignment)
	if err != nil {
		skipIfBGPUnavailable(t, err)
		t.Fatalf("AddVIP with invalid BFD intervals should not fail: %v", err)
	}

	sessionCount := handler.bfdManager.GetSessionCount()
	if sessionCount != 1 {
		t.Errorf("Expected 1 BFD session despite invalid intervals, got %d", sessionCount)
	}
}

func TestBGPHandler_AddedAtTimestamp(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, _ := NewBGPHandler(logger)
	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		skipIfBGPUnavailable(t, err)
	}

	beforeAdd := time.Now()

	assignment := &pb.VIPAssignment{
		VipName: "test-timestamp",
		Address: "10.100.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
		},
	}

	err := handler.AddVIP(ctx, assignment)
	// BGP server may fail to start due to permissions, that's OK for this test
	if err != nil {
		t.Skip("BGP server requires elevated permissions, skipping timestamp test")
	}

	afterAdd := time.Now()

	handler.mu.RLock()
	state, exists := handler.activeVIPs["test-timestamp"]
	handler.mu.RUnlock()

	if !exists {
		t.Fatal("VIP not found after add")
	}

	if state.AddedAt.Before(beforeAdd) || state.AddedAt.After(afterAdd) {
		t.Errorf("AddedAt timestamp not within expected range")
	}

	// Remove and check duration logging
	_ = handler.RemoveVIP(ctx, assignment)

	duration := time.Since(state.AddedAt)
	if duration < 0 {
		t.Error("Duration should be non-negative")
	}
}

// TestBGPHandler_currentServerAS_SetOnStart verifies that currentServerAS is
// populated when the BGP server starts and that the field correctly reflects
// the AS number supplied to startBGPServer.  This is a unit-level test that
// exercises the field without requiring network privileges: we call
// startBGPServer (which may fail on permission-denied) and skip in that case,
// or we confirm the field value when it succeeds.
func TestBGPHandler_currentServerAS_SetOnStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewBGPHandler(logger)
	if err != nil {
		t.Fatalf("NewBGPHandler failed: %v", err)
	}

	// The field must be zero before any server is started.
	if handler.currentServerAS != 0 {
		t.Errorf("expected currentServerAS == 0 before start, got %d", handler.currentServerAS)
	}

	ctx := context.Background()
	config := &pb.BGPConfig{
		LocalAs:  65099,
		RouterId: "10.0.0.1",
	}

	err = handler.startBGPServer(ctx, config)
	if err != nil {
		skipIfBGPUnavailable(t, err)
		t.Fatalf("startBGPServer failed: %v", err)
	}

	if handler.currentServerAS != 65099 {
		t.Errorf("expected currentServerAS == 65099 after start, got %d", handler.currentServerAS)
	}
}

// TestBGPHandler_CrossVIP_ASNMismatch verifies that AddVIP detects when a
// second (new) VIP requests a different Local AS number than the running BGP
// server, and that it restarts the server with the new AS.  The test confirms
// this by inspecting currentServerAS after the mismatch is resolved.
func TestBGPHandler_CrossVIP_ASNMismatch(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler, err := NewBGPHandler(logger)
	if err != nil {
		t.Fatalf("NewBGPHandler failed: %v", err)
	}

	ctx := context.Background()

	firstVIP := &pb.VIPAssignment{
		VipName: "vip-as65001",
		Address: "10.200.0.1/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65001,
			RouterId: "10.0.0.1",
		},
	}

	if err := handler.AddVIP(ctx, firstVIP); err != nil {
		skipIfBGPUnavailable(t, err)
		t.Fatalf("AddVIP (first) failed: %v", err)
	}

	// Confirm the server started with AS 65001.
	handler.mu.RLock()
	currentAS := handler.currentServerAS
	handler.mu.RUnlock()
	if currentAS != 65001 {
		t.Errorf("expected currentServerAS == 65001 after first VIP, got %d", currentAS)
	}

	// Add a second VIP that requests a different AS number — this must trigger
	// a BGP server restart.
	secondVIP := &pb.VIPAssignment{
		VipName: "vip-as65002",
		Address: "10.200.0.2/32",
		Mode:    pb.VIPMode_BGP,
		BgpConfig: &pb.BGPConfig{
			LocalAs:  65002,
			RouterId: "10.0.0.1",
		},
	}

	if err := handler.AddVIP(ctx, secondVIP); err != nil {
		skipIfBGPUnavailable(t, err)
		t.Fatalf("AddVIP (second, different AS) failed: %v", err)
	}

	// After the restart the server must be running under the new AS.
	handler.mu.RLock()
	currentAS = handler.currentServerAS
	handler.mu.RUnlock()
	if currentAS != 65002 {
		t.Errorf("expected currentServerAS == 65002 after AS-mismatch restart, got %d", currentAS)
	}

	// Both VIPs must still be tracked in activeVIPs.
	handler.mu.RLock()
	_, firstExists := handler.activeVIPs["vip-as65001"]
	_, secondExists := handler.activeVIPs["vip-as65002"]
	handler.mu.RUnlock()

	if !firstExists {
		t.Error("first VIP should still be tracked in activeVIPs after restart")
	}
	if !secondExists {
		t.Error("second VIP should be tracked in activeVIPs after restart")
	}
}
