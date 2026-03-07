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
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

var (
	errPool = errors.New("pool")
)

// Verify that Allocator implements Client.
var _ Client = (*Allocator)(nil)

// Allocator manages IP address allocation across multiple pools.
// It implements Client for use as a local/standalone IPAM backend.
type Allocator struct {
	mu     sync.RWMutex
	logger *zap.Logger
	pools  map[string]*Pool // pool name -> Pool
}

// NewAllocator creates a new IP address allocator
func NewAllocator(logger *zap.Logger) *Allocator {
	return &Allocator{
		logger: logger.Named("ipam"),
		pools:  make(map[string]*Pool),
	}
}

// AddPool adds or updates a pool in the allocator.
//
// Lock ordering: allocator.mu is always acquired before pool.mu to prevent
// deadlocks (#611). When migrating allocations we snapshot the old pool's
// state first (under its own lock via GetAllocations), then populate the
// new pool directly -- the new pool is not yet published so no other
// goroutine can hold its lock.
func (a *Allocator) AddPool(name string, cidrs []string, addresses []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	pool, err := NewPool(name, cidrs, addresses)
	if err != nil {
		return fmt.Errorf("failed to create pool %s: %w", name, err)
	}

	// If pool already exists, migrate allocations.
	// Snapshot the old pool's allocations under its own lock, then release
	// it before touching the new pool to avoid holding both locks (#611).
	if existing, ok := a.pools[name]; ok {
		existingAllocations := existing.GetAllocations() // acquires & releases existing.mu
		for addr, vipName := range existingAllocations {
			// pool is local (not yet in a.pools), so no lock needed
			if pool.containsUnlocked(addr) {
				pool.allocated[addr] = vipName
			}
		}
	}

	a.pools[name] = pool

	a.logger.Info("IP pool registered",
		zap.String("pool", name),
		zap.Int("total_addresses", pool.GetTotalCount()),
	)

	return nil
}

// RemovePool removes a pool from the allocator
func (a *Allocator) RemovePool(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if pool, ok := a.pools[name]; ok {
		allocCount := pool.GetAllocatedCount()
		if allocCount > 0 {
			a.logger.Warn("Removing pool with active allocations",
				zap.String("pool", name),
				zap.Int("allocated", allocCount),
			)
		}
		delete(a.pools, name)
	}
}

// Allocate allocates an IP from the specified pool for a resource.
// The context parameter is accepted for interface compatibility but ignored locally.
func (a *Allocator) Allocate(_ context.Context, poolName, resource string) (string, error) {
	a.mu.RLock()
	pool, ok := a.pools[poolName]
	a.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("%w: %s not found", errPool, poolName)
	}

	addr, err := pool.Allocate(resource)
	if err != nil {
		return "", err
	}

	a.logger.Info("IP allocated",
		zap.String("pool", poolName),
		zap.String("resource", resource),
		zap.String("address", addr),
	)

	return addr, nil
}

// Release releases an IP allocation for a resource from the specified pool.
// The context parameter is accepted for interface compatibility but ignored locally.
func (a *Allocator) Release(_ context.Context, poolName, resource string) error {
	a.mu.RLock()
	pool, ok := a.pools[poolName]
	a.mu.RUnlock()

	if !ok {
		return nil
	}

	pool.Release(resource)

	a.logger.Info("IP released",
		zap.String("pool", poolName),
		zap.String("resource", resource),
	)

	return nil
}

// GetPoolStats returns allocation statistics for a pool.
// The context parameter is accepted for interface compatibility but ignored locally.
func (a *Allocator) GetPoolStats(_ context.Context, poolName string) (allocated, available int, err error) {
	a.mu.RLock()
	pool, ok := a.pools[poolName]
	a.mu.RUnlock()

	if !ok {
		return 0, 0, fmt.Errorf("%w: %s not found", errPool, poolName)
	}

	return pool.GetAllocatedCount(), pool.GetAvailableCount(), nil
}

// GetPoolAllocations returns all allocations for a pool.
// The context parameter is accepted for interface compatibility but ignored locally.
func (a *Allocator) GetPoolAllocations(_ context.Context, poolName string) (map[string]string, error) {
	a.mu.RLock()
	pool, ok := a.pools[poolName]
	a.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w: %s not found", errPool, poolName)
	}

	return pool.GetAllocations(), nil
}

// IsAddressConflict checks if an address is already allocated in any pool.
// The context parameter is accepted for interface compatibility but ignored locally.
func (a *Allocator) IsAddressConflict(_ context.Context, address string) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for poolName, pool := range a.pools {
		if pool.IsAllocated(address) {
			return poolName, true
		}
	}

	return "", false
}

// GetPoolNames returns the names of all registered pools.
// The context parameter is accepted for interface compatibility but ignored locally.
func (a *Allocator) GetPoolNames(_ context.Context) []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	names := make([]string, 0, len(a.pools))
	for name := range a.pools {
		names = append(names, name)
	}
	return names
}
