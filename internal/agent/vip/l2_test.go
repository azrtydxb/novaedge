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
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Note: Full testing of L2Handler requires network privileges and real interfaces.
// These tests focus on state management and logic that doesn't require network access.

func TestVIPState(t *testing.T) {
	assignment := &pb.VIPAssignment{
		VipName: "test-vip",
		Address: "192.168.1.100/24",
	}

	ip := net.ParseIP("192.168.1.100")
	now := time.Now()

	state := &VIPState{
		Assignment: assignment,
		IP:         ip,
		AddedAt:    now,
	}

	if state.Assignment != assignment {
		t.Error("VIPState assignment does not match")
	}

	if !state.IP.Equal(ip) {
		t.Errorf("Expected IP %s, got %s", ip, state.IP)
	}

	if state.AddedAt != now {
		t.Error("VIPState AddedAt does not match")
	}
}

func TestL2Handler_GetActiveVIPCount(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := &L2Handler{
		logger:        logger,
		activeVIPs:    make(map[string]*VIPState),
		interfaceName: "eth0",
	}

	t.Run("initially zero", func(t *testing.T) {
		count := handler.GetActiveVIPCount()
		if count != 0 {
			t.Errorf("Expected 0 active VIPs, got %d", count)
		}
	})

	t.Run("after adding VIPs to state", func(t *testing.T) {
		// Directly add to state (bypassing network operations for testing)
		handler.mu.Lock()
		handler.activeVIPs["vip1"] = &VIPState{
			Assignment: &pb.VIPAssignment{VipName: "vip1", Address: "192.168.1.100/24"},
			IP:         net.ParseIP("192.168.1.100"),
			AddedAt:    time.Now(),
		}
		handler.activeVIPs["vip2"] = &VIPState{
			Assignment: &pb.VIPAssignment{VipName: "vip2", Address: "192.168.1.101/24"},
			IP:         net.ParseIP("192.168.1.101"),
			AddedAt:    time.Now(),
		}
		handler.mu.Unlock()

		count := handler.GetActiveVIPCount()
		if count != 2 {
			t.Errorf("Expected 2 active VIPs, got %d", count)
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100

		// Concurrently read the count
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = handler.GetActiveVIPCount()
			}()
		}

		wg.Wait()
		// Test passes if no race condition detected
	})
}

func TestL2Handler_StateManagement(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := &L2Handler{
		logger:        logger,
		activeVIPs:    make(map[string]*VIPState),
		interfaceName: "eth0",
	}

	t.Run("tracking VIP state", func(t *testing.T) {
		assignment := &pb.VIPAssignment{
			VipName: "test-vip",
			Address: "192.168.1.100/24",
		}

		ip := net.ParseIP("192.168.1.100")
		startTime := time.Now()

		// Simulate adding VIP state (without network operations)
		handler.mu.Lock()
		handler.activeVIPs[assignment.VipName] = &VIPState{
			Assignment: assignment,
			IP:         ip,
			AddedAt:    startTime,
		}
		handler.mu.Unlock()

		// Verify state exists
		handler.mu.RLock()
		state, exists := handler.activeVIPs[assignment.VipName]
		handler.mu.RUnlock()

		if !exists {
			t.Fatal("Expected VIP state to exist")
		}

		if state.Assignment.VipName != assignment.VipName {
			t.Errorf("Expected VIP name %s, got %s", assignment.VipName, state.Assignment.VipName)
		}

		if !state.IP.Equal(ip) {
			t.Errorf("Expected IP %s, got %s", ip, state.IP)
		}

		if state.AddedAt.Before(startTime) {
			t.Error("AddedAt time is before start time")
		}
	})

	t.Run("removing VIP state", func(t *testing.T) {
		assignment := &pb.VIPAssignment{
			VipName: "remove-test",
			Address: "192.168.1.200/24",
		}

		// Add VIP state
		handler.mu.Lock()
		handler.activeVIPs[assignment.VipName] = &VIPState{
			Assignment: assignment,
			IP:         net.ParseIP("192.168.1.200"),
			AddedAt:    time.Now(),
		}
		handler.mu.Unlock()

		// Verify it exists
		handler.mu.RLock()
		_, exists := handler.activeVIPs[assignment.VipName]
		handler.mu.RUnlock()

		if !exists {
			t.Fatal("VIP should exist before removal")
		}

		// Remove VIP state
		handler.mu.Lock()
		delete(handler.activeVIPs, assignment.VipName)
		handler.mu.Unlock()

		// Verify it's gone
		handler.mu.RLock()
		_, exists = handler.activeVIPs[assignment.VipName]
		handler.mu.RUnlock()

		if exists {
			t.Error("VIP should not exist after removal")
		}
	})

	t.Run("duplicate VIP handling", func(t *testing.T) {
		assignment := &pb.VIPAssignment{
			VipName: "duplicate-test",
			Address: "192.168.1.150/24",
		}

		// Add VIP state first time
		handler.mu.Lock()
		handler.activeVIPs[assignment.VipName] = &VIPState{
			Assignment: assignment,
			IP:         net.ParseIP("192.168.1.150"),
			AddedAt:    time.Now(),
		}
		handler.mu.Unlock()

		// Check if VIP already exists (simulating AddVIP logic)
		handler.mu.Lock()
		_, exists := handler.activeVIPs[assignment.VipName]
		handler.mu.Unlock()

		if !exists {
			t.Error("Expected VIP to already exist")
		}

		// In AddVIP, if exists, it returns early without error
		// This test verifies the existence check works
	})
}

func TestL2Handler_ConcurrentStateAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := &L2Handler{
		logger:        logger,
		activeVIPs:    make(map[string]*VIPState),
		interfaceName: "eth0",
	}

	t.Run("concurrent reads and writes", func(t *testing.T) {
		var wg sync.WaitGroup
		numWriters := 10
		numReaders := 20

		// Concurrent writers
		for i := 0; i < numWriters; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				assignment := &pb.VIPAssignment{
					VipName: "vip-" + string(rune('0'+id)),
					Address: "192.168.1." + string(rune('0'+id)) + "/24",
				}

				handler.mu.Lock()
				handler.activeVIPs[assignment.VipName] = &VIPState{
					Assignment: assignment,
					IP:         net.ParseIP("192.168.1." + string(rune('0'+id))),
					AddedAt:    time.Now(),
				}
				handler.mu.Unlock()
			}(i)
		}

		// Concurrent readers
		for i := 0; i < numReaders; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = handler.GetActiveVIPCount()
			}()
		}

		wg.Wait()
		// Test passes if no race condition detected
	})
}

func TestL2Handler_GARPAnnouncer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := &L2Handler{
		logger:        logger,
		activeVIPs:    make(map[string]*VIPState),
		interfaceName: "eth0",
	}

	t.Run("announcer starts and stops cleanly", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Start the GARP announcer
		done := make(chan struct{})
		go func() {
			handler.garpAnnouncer(ctx)
			close(done)
		}()

		// Wait for context timeout
		<-ctx.Done()

		// Wait for announcer to finish
		select {
		case <-done:
			// Success - announcer stopped cleanly
		case <-time.After(200 * time.Millisecond):
			t.Error("GARP announcer did not stop in time")
		}
	})
}

func TestL2Handler_AnnounceActiveVIPs(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := &L2Handler{
		logger:        logger,
		activeVIPs:    make(map[string]*VIPState),
		interfaceName: "eth0",
	}

	t.Run("no VIPs to announce", func(t *testing.T) {
		// Should not panic when there are no active VIPs
		handler.announceActiveVIPs()
	})

	t.Run("with active VIPs", func(t *testing.T) {
		// Add some VIP states
		handler.mu.Lock()
		handler.activeVIPs["vip1"] = &VIPState{
			Assignment: &pb.VIPAssignment{VipName: "vip1", Address: "192.168.1.100/24"},
			IP:         net.ParseIP("192.168.1.100"),
			AddedAt:    time.Now(),
		}
		handler.activeVIPs["vip2"] = &VIPState{
			Assignment: &pb.VIPAssignment{VipName: "vip2", Address: "2001:db8::1/64"},
			IP:         net.ParseIP("2001:db8::1"),
			AddedAt:    time.Now(),
		}
		handler.mu.Unlock()

		// Should not panic when announcing (GARP sending is logged but not actually sent in current implementation)
		handler.announceActiveVIPs()
	})
}

func TestDetectPrimaryInterface(t *testing.T) {
	// Note: This test may fail in environments without network interfaces
	// or without appropriate permissions
	t.Run("detect interface", func(t *testing.T) {
		iface, err := detectPrimaryInterface()
		if err != nil {
			// This is acceptable in test environments without network
			t.Skipf("Skipping test in environment without network interfaces: %v", err)
			return
		}

		if iface == "" {
			t.Error("Expected non-empty interface name")
		}

		t.Logf("Detected primary interface: %s", iface)

		// Verify the interface actually exists
		_, err = net.InterfaceByName(iface)
		if err != nil {
			t.Errorf("Detected interface %s does not exist: %v", iface, err)
		}
	})
}

// Integration test example (requires network privileges and real interfaces)
/*
func TestL2Handler_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if running with sufficient privileges
	if os.Getuid() != 0 {
		t.Skip("Integration tests require root privileges")
	}

	logger := zaptest.NewLogger(t)

	// Create handler
	handler, err := NewL2Handler(logger)
	if err != nil {
		t.Fatalf("Failed to create L2 handler: %v", err)
	}

	// Start handler
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := handler.Start(ctx); err != nil {
		t.Fatalf("Failed to start L2 handler: %v", err)
	}

	// Create test VIP assignment
	assignment := &pb.VIPAssignment{
		VipName: "test-vip",
		Address: "192.0.2.1/32", // TEST-NET-1 (RFC 5737)
		Node:    "test-node",
	}

	// Add VIP
	if err := handler.AddVIP(assignment); err != nil {
		t.Errorf("Failed to add VIP: %v", err)
	}

	// Verify VIP was added
	count := handler.GetActiveVIPCount()
	if count != 1 {
		t.Errorf("Expected 1 active VIP, got %d", count)
	}

	// Remove VIP
	if err := handler.RemoveVIP(assignment); err != nil {
		t.Errorf("Failed to remove VIP: %v", err)
	}

	// Verify VIP was removed
	count = handler.GetActiveVIPCount()
	if count != 0 {
		t.Errorf("Expected 0 active VIPs after removal, got %d", count)
	}
}
*/
