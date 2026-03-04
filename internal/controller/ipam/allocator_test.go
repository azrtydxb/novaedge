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

package ipam

import (
	"fmt"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestNewAllocator(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	if a == nil {
		t.Fatal("NewAllocator() returned nil")
	}

	if a.pools == nil {
		t.Error("pools map should not be nil")
	}

	if len(a.pools) != 0 {
		t.Errorf("expected empty pools map, got %d entries", len(a.pools))
	}
}

func TestAllocator_AddPool(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	tests := []struct {
		name      string
		poolName  string
		cidrs     []string
		addresses []string
		wantErr   bool
	}{
		{
			name:      "valid CIDR",
			poolName:  "test-pool",
			cidrs:     []string{"192.168.1.0/24"},
			addresses: nil,
			wantErr:   false,
		},
		{
			name:      "valid addresses",
			poolName:  "addr-pool",
			cidrs:     nil,
			addresses: []string{"10.0.0.1", "10.0.0.2"},
			wantErr:   false,
		},
		{
			name:      "invalid CIDR",
			poolName:  "bad-pool",
			cidrs:     []string{"invalid"},
			addresses: nil,
			wantErr:   true,
		},
		{
			name:      "mixed valid",
			poolName:  "mixed-pool",
			cidrs:     []string{"10.0.0.0/29"},
			addresses: []string{"10.0.1.1"},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.AddPool(tt.poolName, tt.cidrs, tt.addresses)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddPool() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if _, ok := a.pools[tt.poolName]; !ok {
					t.Error("pool was not added to allocator")
				}
			}
		})
	}
}

func TestAllocator_AddPool_Migration(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Create initial pool
	err := a.AddPool("test-pool", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Allocate an IP
	addr, err := a.Allocate("test-pool", "test-vip")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	// Extract just the IP from CIDR notation
	ipStr := strings.TrimSuffix(addr, "/32")

	// Update pool with overlapping range
	err = a.AddPool("test-pool", []string{"192.168.1.0/28"}, nil)
	if err != nil {
		t.Fatalf("AddPool() update error = %v", err)
	}

	// Verify allocation was migrated
	allocations, _ := a.GetPoolAllocations("test-pool")
	if allocations[ipStr] != "test-vip" {
		t.Errorf("allocation not migrated, got allocations: %v", allocations)
	}
}

func TestAllocator_RemovePool(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add a pool
	err := a.AddPool("test-pool", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Remove the pool
	a.RemovePool("test-pool")

	if _, ok := a.pools["test-pool"]; ok {
		t.Error("pool should be removed")
	}
}

func TestAllocator_RemovePool_WithAllocations(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add a pool
	err := a.AddPool("test-pool", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Allocate an IP
	_, err = a.Allocate("test-pool", "test-vip")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	// Remove the pool (should log warning but still remove)
	a.RemovePool("test-pool")

	if _, ok := a.pools["test-pool"]; ok {
		t.Error("pool should be removed even with allocations")
	}
}

func TestAllocator_RemovePool_NonExistent(_ *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Should not panic
	a.RemovePool("non-existent-pool")
}

func TestAllocator_Allocate(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add a pool
	err := a.AddPool("test-pool", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Allocate an IP
	addr, err := a.Allocate("test-pool", "test-vip")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	if addr == "" {
		t.Error("Allocate() returned empty address")
	}
}

func TestAllocator_Allocate_PoolNotFound(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	_, err := a.Allocate("non-existent", "test-vip")
	if err == nil {
		t.Error("Allocate() should return error for non-existent pool")
	}
}

func TestAllocator_Release(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add a pool
	err := a.AddPool("test-pool", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Allocate an IP
	addr, err := a.Allocate("test-pool", "test-vip")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	// Extract just the IP from CIDR notation
	ipStr := strings.TrimSuffix(addr, "/32")

	// Verify it's allocated
	pool := a.pools["test-pool"]
	if !pool.IsAllocated(ipStr) {
		t.Error("address should be allocated before release")
	}

	// Release by VIP name
	a.Release("test-pool", "test-vip")

	// Verify it was released
	if pool.IsAllocated(ipStr) {
		t.Error("address should be released")
	}
}

func TestAllocator_Release_PoolNotFound(_ *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Should not panic
	a.Release("non-existent", "test-vip")
}

func TestAllocator_GetPoolAllocations(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add a pool
	err := a.AddPool("test-pool", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Allocate some IPs
	addr1, err := a.Allocate("test-pool", "vip-1")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	addr2, err := a.Allocate("test-pool", "vip-2")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	// Extract just the IPs from CIDR notation
	ip1 := strings.TrimSuffix(addr1, "/32")
	ip2 := strings.TrimSuffix(addr2, "/32")

	// Get allocations
	allocations, err := a.GetPoolAllocations("test-pool")
	if err != nil {
		t.Fatalf("GetPoolAllocations() error = %v", err)
	}

	if allocations == nil {
		t.Fatal("GetPoolAllocations() returned nil")
	}

	if len(allocations) != 2 {
		t.Errorf("expected 2 allocations, got %d", len(allocations))
	}

	// allocations is IP -> VIP name
	if allocations[ip1] != "vip-1" {
		t.Errorf("allocation for %s = %s, want vip-1", ip1, allocations[ip1])
	}

	if allocations[ip2] != "vip-2" {
		t.Errorf("allocation for %s = %s, want vip-2", ip2, allocations[ip2])
	}
}

func TestAllocator_GetPoolAllocations_PoolNotFound(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	allocations, err := a.GetPoolAllocations("non-existent")
	if err == nil {
		t.Error("GetPoolAllocations() should return error for non-existent pool")
	}
	if allocations != nil {
		t.Errorf("GetPoolAllocations() should return nil for non-existent pool, got %v", allocations)
	}
}

func TestAllocator_GetPoolStats(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add a pool
	err := a.AddPool("test-pool", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Allocate an IP
	_, err = a.Allocate("test-pool", "vip-1")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	// Get stats
	allocated, available, err := a.GetPoolStats("test-pool")
	if err != nil {
		t.Fatalf("GetPoolStats() error = %v", err)
	}

	if allocated != 1 {
		t.Errorf("allocated = %d, want 1", allocated)
	}

	if available <= 0 {
		t.Errorf("available = %d, expected > 0", available)
	}
}

func TestAllocator_GetPoolStats_PoolNotFound(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	_, _, err := a.GetPoolStats("non-existent")
	if err == nil {
		t.Error("GetPoolStats() should return error for non-existent pool")
	}
}

func TestAllocator_IsAddressConflict(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add a pool
	err := a.AddPool("test-pool", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Allocate an IP
	addr, err := a.Allocate("test-pool", "test-vip")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	// Extract just the IP from CIDR notation
	ipStr := strings.TrimSuffix(addr, "/32")

	// Check conflict
	poolName, isConflict := a.IsAddressConflict(ipStr)
	if !isConflict {
		t.Error("IsAddressConflict() should return true for allocated address")
	}
	if poolName != "test-pool" {
		t.Errorf("poolName = %q, want %q", poolName, "test-pool")
	}

	// Check non-conflict
	_, isConflict = a.IsAddressConflict("192.168.1.250")
	if isConflict {
		t.Error("IsAddressConflict() should return false for unallocated address")
	}
}

func TestAllocator_GetPoolNames(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Initially empty
	names := a.GetPoolNames()
	if len(names) != 0 {
		t.Errorf("expected 0 pool names, got %d", len(names))
	}

	// Add pools
	_ = a.AddPool("pool-1", []string{"192.168.1.0/29"}, nil)
	_ = a.AddPool("pool-2", []string{"10.0.0.0/29"}, nil)

	names = a.GetPoolNames()
	if len(names) != 2 {
		t.Errorf("expected 2 pool names, got %d", len(names))
	}

	// Verify names are present
	found := make(map[string]bool)
	for _, name := range names {
		found[name] = true
	}

	if !found["pool-1"] || !found["pool-2"] {
		t.Error("missing expected pool names")
	}
}

func TestAllocator_MultiplePools(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add multiple pools
	err := a.AddPool("pool-1", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	err = a.AddPool("pool-2", []string{"10.0.0.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Allocate from different pools
	addr1, err := a.Allocate("pool-1", "vip-1")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	addr2, err := a.Allocate("pool-2", "vip-2")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	// Extract IPs from CIDR notation
	ip1 := strings.TrimSuffix(addr1, "/32")
	ip2 := strings.TrimSuffix(addr2, "/32")

	// Verify allocations are independent
	alloc1, _ := a.GetPoolAllocations("pool-1")
	alloc2, _ := a.GetPoolAllocations("pool-2")

	if len(alloc1) != 1 {
		t.Errorf("pool-1 should have 1 allocation, got %d", len(alloc1))
	}

	if len(alloc2) != 1 {
		t.Errorf("pool-2 should have 1 allocation, got %d", len(alloc2))
	}

	// allocations is IP -> VIP name
	if alloc1[ip1] != "vip-1" {
		t.Errorf("pool-1 allocation incorrect, got %v", alloc1)
	}

	if alloc2[ip2] != "vip-2" {
		t.Errorf("pool-2 allocation incorrect, got %v", alloc2)
	}
}

func TestAllocator_ConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add a pool with enough addresses
	err := a.AddPool("test-pool", []string{"192.168.0.0/16"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	done := make(chan bool)

	// Concurrent allocations
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				vipName := fmt.Sprintf("vip-%d-%d", id, j)
				_, _ = a.Allocate("test-pool", vipName)
			}
			done <- true
		}(i)
	}

	// Concurrent releases
	go func() {
		for i := 0; i < 10; i++ {
			allocs, _ := a.GetPoolAllocations("test-pool")
			for ip, vipName := range allocs {
				// Release by VIP name, not IP
				a.Release("test-pool", vipName)
				_ = ip // Just to avoid unused variable warning
				break  // Release one and continue
			}
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 11; i++ {
		<-done
	}
}

func TestAllocator_ExhaustPool(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add a very small pool (only 6 usable addresses in /29)
	err := a.AddPool("small-pool", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Allocate until exhausted
	allocated := []string{}
	for i := 0; i < 10; i++ {
		addr, err := a.Allocate("small-pool", string(rune('A'+i)))
		if err != nil {
			break
		}
		allocated = append(allocated, addr)
	}

	// Should have allocated some addresses
	if len(allocated) == 0 {
		t.Error("should have allocated at least one address")
	}

	// Pool should be exhausted (next allocation should fail)
	_, err = a.Allocate("small-pool", "should-fail")
	if err == nil {
		t.Error("expected error when pool is exhausted")
	}
}

func TestAllocator_ReleaseByVIPName(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add a pool
	err := a.AddPool("test-pool", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Allocate an IP for a specific VIP
	addr, err := a.Allocate("test-pool", "my-app-vip")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	// Verify allocation exists
	allocations, _ := a.GetPoolAllocations("test-pool")
	if len(allocations) != 1 {
		t.Errorf("expected 1 allocation, got %d", len(allocations))
	}

	// Release by VIP name (not IP address)
	a.Release("test-pool", "my-app-vip")

	// Verify allocation was removed
	allocations, _ = a.GetPoolAllocations("test-pool")
	if len(allocations) != 0 {
		t.Errorf("expected 0 allocations after release, got %d", len(allocations))
	}

	// The IP should now be available for reallocation
	addr2, err := a.Allocate("test-pool", "another-vip")
	if err != nil {
		t.Fatalf("Allocate() after release error = %v", err)
	}

	// Should get the same IP back since it was released
	if addr2 != addr {
		t.Errorf("expected same IP %s after release, got %s", addr, addr2)
	}
}

func TestAllocator_AllocateIdempotent(t *testing.T) {
	logger := zap.NewNop()
	a := NewAllocator(logger)

	// Add a pool
	err := a.AddPool("test-pool", []string{"192.168.1.0/29"}, nil)
	if err != nil {
		t.Fatalf("AddPool() error = %v", err)
	}

	// Allocate for same VIP twice
	addr1, err := a.Allocate("test-pool", "same-vip")
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}

	addr2, err := a.Allocate("test-pool", "same-vip")
	if err != nil {
		t.Fatalf("Second Allocate() error = %v", err)
	}

	// Should return the same IP
	if addr1 != addr2 {
		t.Errorf("idempotent allocation should return same IP, got %s then %s", addr1, addr2)
	}

	// Should only have one allocation
	allocations, _ := a.GetPoolAllocations("test-pool")
	if len(allocations) != 1 {
		t.Errorf("expected 1 allocation, got %d", len(allocations))
	}
}
