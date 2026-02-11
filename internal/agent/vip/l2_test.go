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

func TestVIPState_IPv6(t *testing.T) {
	assignment := &pb.VIPAssignment{
		VipName: "test-vip-v6",
		Address: "2001:db8::100/128",
	}

	ip := net.ParseIP("2001:db8::100")
	now := time.Now()

	state := &VIPState{
		Assignment: assignment,
		IP:         ip,
		AddedAt:    now,
		IsIPv6:     true,
	}

	if !state.IsIPv6 {
		t.Error("Expected VIPState to be marked as IPv6")
	}

	if !state.IP.Equal(ip) {
		t.Errorf("Expected IP %s, got %s", ip, state.IP)
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

	t.Run("with IPv6 VIPs", func(t *testing.T) {
		handler.mu.Lock()
		handler.activeVIPs["vip3"] = &VIPState{
			Assignment: &pb.VIPAssignment{VipName: "vip3", Address: "2001:db8::1/128"},
			IP:         net.ParseIP("2001:db8::1"),
			AddedAt:    time.Now(),
			IsIPv6:     true,
		}
		handler.mu.Unlock()

		count := handler.GetActiveVIPCount()
		if count != 3 {
			t.Errorf("Expected 3 active VIPs (including IPv6), got %d", count)
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = handler.GetActiveVIPCount()
			}()
		}

		wg.Wait()
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

		handler.mu.Lock()
		handler.activeVIPs[assignment.VipName] = &VIPState{
			Assignment: assignment,
			IP:         ip,
			AddedAt:    startTime,
		}
		handler.mu.Unlock()

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
	})

	t.Run("dual-stack VIP tracking", func(t *testing.T) {
		v4Assignment := &pb.VIPAssignment{
			VipName: "dual-vip",
			Address: "192.168.1.200/24",
		}
		v6Assignment := &pb.VIPAssignment{
			VipName: "dual-vip-v6",
			Address: "2001:db8::200/128",
		}

		handler.mu.Lock()
		handler.activeVIPs[v4Assignment.VipName] = &VIPState{
			Assignment: v4Assignment,
			IP:         net.ParseIP("192.168.1.200"),
			AddedAt:    time.Now(),
			IsIPv6:     false,
		}
		handler.activeVIPs[v6Assignment.VipName] = &VIPState{
			Assignment: v6Assignment,
			IP:         net.ParseIP("2001:db8::200"),
			AddedAt:    time.Now(),
			IsIPv6:     true,
		}
		handler.mu.Unlock()

		handler.mu.RLock()
		v4State, v4Exists := handler.activeVIPs["dual-vip"]
		v6State, v6Exists := handler.activeVIPs["dual-vip-v6"]
		handler.mu.RUnlock()

		if !v4Exists || !v6Exists {
			t.Fatal("Both IPv4 and IPv6 VIP states should exist")
		}

		if v4State.IsIPv6 {
			t.Error("IPv4 VIP should not be marked as IPv6")
		}
		if !v6State.IsIPv6 {
			t.Error("IPv6 VIP should be marked as IPv6")
		}
	})

	t.Run("removing VIP state", func(t *testing.T) {
		assignment := &pb.VIPAssignment{
			VipName: "remove-test",
			Address: "192.168.1.200/24",
		}

		handler.mu.Lock()
		handler.activeVIPs[assignment.VipName] = &VIPState{
			Assignment: assignment,
			IP:         net.ParseIP("192.168.1.200"),
			AddedAt:    time.Now(),
		}
		handler.mu.Unlock()

		handler.mu.Lock()
		delete(handler.activeVIPs, assignment.VipName)
		handler.mu.Unlock()

		handler.mu.RLock()
		_, exists := handler.activeVIPs[assignment.VipName]
		handler.mu.RUnlock()

		if exists {
			t.Error("VIP should not exist after removal")
		}
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

		for i := 0; i < numReaders; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = handler.GetActiveVIPCount()
			}()
		}

		wg.Wait()
	})
}

func TestL2Handler_AnnouncementLoop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := &L2Handler{
		logger:        logger,
		activeVIPs:    make(map[string]*VIPState),
		interfaceName: "eth0",
	}

	t.Run("announcer starts and stops cleanly", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		done := make(chan struct{})
		go func() {
			handler.announcementLoop(ctx)
			close(done)
		}()

		<-ctx.Done()

		select {
		case <-done:
			// Success
		case <-time.After(200 * time.Millisecond):
			t.Error("Announcement loop did not stop in time")
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
		handler.announceActiveVIPs()
	})

	t.Run("with active VIPs including IPv6", func(t *testing.T) {
		handler.mu.Lock()
		handler.activeVIPs["vip1"] = &VIPState{
			Assignment: &pb.VIPAssignment{VipName: "vip1", Address: "192.168.1.100/24"},
			IP:         net.ParseIP("192.168.1.100"),
			AddedAt:    time.Now(),
			IsIPv6:     false,
		}
		handler.activeVIPs["vip2"] = &VIPState{
			Assignment: &pb.VIPAssignment{VipName: "vip2", Address: "2001:db8::1/64"},
			IP:         net.ParseIP("2001:db8::1"),
			AddedAt:    time.Now(),
			IsIPv6:     true,
		}
		handler.mu.Unlock()

		// Should not panic
		handler.announceActiveVIPs()
	})
}

func TestDetectPrimaryInterface(t *testing.T) {
	t.Run("detect interface", func(t *testing.T) {
		iface, err := detectPrimaryInterface()
		if err != nil {
			t.Skipf("Skipping test in environment without network interfaces: %v", err)
			return
		}

		if iface == "" {
			t.Error("Expected non-empty interface name")
		}

		t.Logf("Detected primary interface: %s", iface)

		_, err = net.InterfaceByName(iface)
		if err != nil {
			t.Errorf("Detected interface %s does not exist: %v", iface, err)
		}
	})
}
