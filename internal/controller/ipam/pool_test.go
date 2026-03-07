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
	"context"
	"strings"
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestPool_NewPool(t *testing.T) {
	t.Run("valid CIDR", func(t *testing.T) {
		pool, err := NewPool("test", []string{"10.200.0.0/28"}, nil)
		if err != nil {
			t.Fatalf("Failed to create pool: %v", err)
		}

		total := pool.GetTotalCount()
		// /28 = 16 addresses, minus network and broadcast = 14
		if total != 14 {
			t.Errorf("Expected 14 addresses in /28 pool, got %d", total)
		}
	})

	t.Run("explicit addresses", func(t *testing.T) {
		pool, err := NewPool("test", nil, []string{
			"10.200.0.10/32",
			"10.200.0.20/32",
			"10.200.0.30/32",
		})
		if err != nil {
			t.Fatalf("Failed to create pool: %v", err)
		}

		if pool.GetTotalCount() != 3 {
			t.Errorf("Expected 3 addresses, got %d", pool.GetTotalCount())
		}
	})

	t.Run("combined CIDR and explicit", func(t *testing.T) {
		pool, err := NewPool("test",
			[]string{"10.200.0.0/30"},
			[]string{"10.200.1.1/32"},
		)
		if err != nil {
			t.Fatalf("Failed to create pool: %v", err)
		}

		// /30 = 4 addresses minus network and broadcast = 2, plus 1 explicit = 3
		if pool.GetTotalCount() != 3 {
			t.Errorf("Expected 3 addresses, got %d", pool.GetTotalCount())
		}
	})

	t.Run("single IP /32", func(t *testing.T) {
		pool, err := NewPool("test", []string{"10.200.0.1/32"}, nil)
		if err != nil {
			t.Fatalf("Failed to create pool: %v", err)
		}

		if pool.GetTotalCount() != 1 {
			t.Errorf("Expected 1 address for /32, got %d", pool.GetTotalCount())
		}
	})

	t.Run("invalid CIDR", func(t *testing.T) {
		_, err := NewPool("test", []string{"invalid"}, nil)
		if err == nil {
			t.Error("Expected error for invalid CIDR")
		}
	})
}

func TestPool_Allocate(t *testing.T) {
	pool, err := NewPool("test", nil, []string{
		"10.200.0.1/32",
		"10.200.0.2/32",
		"10.200.0.3/32",
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	t.Run("first allocation", func(t *testing.T) {
		addr, err := pool.Allocate("vip-1")
		if err != nil {
			t.Fatalf("Failed to allocate: %v", err)
		}

		if !strings.HasSuffix(addr, "/32") {
			t.Errorf("Expected /32 CIDR notation, got %s", addr)
		}

		if pool.GetAllocatedCount() != 1 {
			t.Errorf("Expected 1 allocated, got %d", pool.GetAllocatedCount())
		}

		if pool.GetAvailableCount() != 2 {
			t.Errorf("Expected 2 available, got %d", pool.GetAvailableCount())
		}
	})

	t.Run("idempotent allocation", func(t *testing.T) {
		addr1, _ := pool.Allocate("vip-1")
		addr2, _ := pool.Allocate("vip-1")

		if addr1 != addr2 {
			t.Errorf("Expected same address for same VIP, got %s and %s", addr1, addr2)
		}
	})

	t.Run("pool exhaustion", func(t *testing.T) {
		_, _ = pool.Allocate("vip-2")
		_, _ = pool.Allocate("vip-3")

		_, err := pool.Allocate("vip-4")
		if err == nil {
			t.Error("Expected exhaustion error")
		}
	})
}

func TestPool_Release(t *testing.T) {
	pool, err := NewPool("test", nil, []string{
		"10.200.0.1/32",
		"10.200.0.2/32",
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	_, _ = pool.Allocate("vip-1")
	_, _ = pool.Allocate("vip-2")

	if pool.GetAvailableCount() != 0 {
		t.Errorf("Expected 0 available, got %d", pool.GetAvailableCount())
	}

	pool.Release("vip-1")

	if pool.GetAllocatedCount() != 1 {
		t.Errorf("Expected 1 allocated, got %d", pool.GetAllocatedCount())
	}

	if pool.GetAvailableCount() != 1 {
		t.Errorf("Expected 1 available, got %d", pool.GetAvailableCount())
	}

	// Can now allocate again
	_, err = pool.Allocate("vip-3")
	if err != nil {
		t.Fatalf("Failed to allocate after release: %v", err)
	}
}

func TestPool_Contains(t *testing.T) {
	pool, err := NewPool("test", []string{"10.200.0.0/24"}, nil)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	if !pool.Contains("10.200.0.50") {
		t.Error("Expected pool to contain 10.200.0.50")
	}

	if pool.Contains("10.201.0.50") {
		t.Error("Expected pool to not contain 10.201.0.50")
	}
}

func TestPool_IsAllocated(t *testing.T) {
	pool, err := NewPool("test", nil, []string{"10.200.0.1/32"})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	if pool.IsAllocated("10.200.0.1") {
		t.Error("Expected address to not be allocated initially")
	}

	_, _ = pool.Allocate("vip-1")

	if !pool.IsAllocated("10.200.0.1") {
		t.Error("Expected address to be allocated after Allocate()")
	}
}

func TestAllocator_MultiplePool(t *testing.T) {
	logger := zaptest.NewLogger(t)
	allocator := NewAllocator(logger)
	ctx := context.Background()

	err := allocator.AddPool("pool-a", []string{"10.200.0.0/30"}, nil)
	if err != nil {
		t.Fatalf("Failed to add pool-a: %v", err)
	}

	err = allocator.AddPool("pool-b", nil, []string{"10.201.0.1/32", "10.201.0.2/32"})
	if err != nil {
		t.Fatalf("Failed to add pool-b: %v", err)
	}

	t.Run("allocate from pool-a", func(t *testing.T) {
		addr, err := allocator.Allocate(ctx, "pool-a", "vip-1")
		if err != nil {
			t.Fatalf("Failed to allocate: %v", err)
		}
		if addr == "" {
			t.Error("Expected non-empty address")
		}
	})

	t.Run("allocate from pool-b", func(t *testing.T) {
		addr, err := allocator.Allocate(ctx, "pool-b", "vip-2")
		if err != nil {
			t.Fatalf("Failed to allocate: %v", err)
		}
		if addr == "" {
			t.Error("Expected non-empty address")
		}
	})

	t.Run("allocate from non-existent pool", func(t *testing.T) {
		_, err := allocator.Allocate(ctx, "pool-c", "vip-3")
		if err == nil {
			t.Error("Expected error for non-existent pool")
		}
	})

	t.Run("conflict detection", func(t *testing.T) {
		poolName, conflict := allocator.IsAddressConflict(ctx, "10.200.0.1")
		if !conflict {
			t.Error("Expected conflict for allocated address")
		}
		if poolName != "pool-a" {
			t.Errorf("Expected pool-a, got %s", poolName)
		}
	})

	t.Run("pool stats", func(t *testing.T) {
		allocated, available, err := allocator.GetPoolStats(ctx, "pool-a")
		if err != nil {
			t.Fatalf("Failed to get stats: %v", err)
		}
		if allocated != 1 {
			t.Errorf("Expected 1 allocated, got %d", allocated)
		}
		if available != 1 { // /30 = 2 usable addresses, 1 allocated
			t.Errorf("Expected 1 available, got %d", available)
		}
	})

	t.Run("remove pool", func(t *testing.T) {
		allocator.RemovePool("pool-a")
		names := allocator.GetPoolNames(ctx)
		if len(names) != 1 {
			t.Errorf("Expected 1 pool remaining, got %d", len(names))
		}
	})
}

func TestPool_IPv6(t *testing.T) {
	pool, err := NewPool("test-v6", nil, []string{
		"2001:db8::1/128",
		"2001:db8::2/128",
		"2001:db8::3/128",
	})
	if err != nil {
		t.Fatalf("Failed to create IPv6 pool: %v", err)
	}

	if pool.GetTotalCount() != 3 {
		t.Errorf("Expected 3 IPv6 addresses, got %d", pool.GetTotalCount())
	}

	addr, err := pool.Allocate("vip-v6")
	if err != nil {
		t.Fatalf("Failed to allocate IPv6: %v", err)
	}

	if !strings.HasSuffix(addr, "/128") {
		t.Errorf("Expected /128 CIDR notation for IPv6, got %s", addr)
	}
}
