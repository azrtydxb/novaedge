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
	"sync"

	"go.uber.org/zap"
)

// Allocator manages IP address allocation across multiple pools
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

// AddPool adds or updates a pool in the allocator
func (a *Allocator) AddPool(name string, cidrs []string, addresses []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	pool, err := NewPool(name, cidrs, addresses)
	if err != nil {
		return fmt.Errorf("failed to create pool %s: %w", name, err)
	}

	// If pool already exists, migrate allocations
	if existing, ok := a.pools[name]; ok {
		existingAllocations := existing.GetAllocations()
		for addr, vipName := range existingAllocations {
			if pool.Contains(addr) {
				pool.mu.Lock()
				pool.allocated[addr] = vipName
				pool.mu.Unlock()
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

// Allocate allocates an IP from the specified pool for a VIP
func (a *Allocator) Allocate(poolName, vipName string) (string, error) {
	a.mu.RLock()
	pool, ok := a.pools[poolName]
	a.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("pool %s not found", poolName)
	}

	addr, err := pool.Allocate(vipName)
	if err != nil {
		return "", err
	}

	a.logger.Info("IP allocated",
		zap.String("pool", poolName),
		zap.String("vip", vipName),
		zap.String("address", addr),
	)

	return addr, nil
}

// Release releases an IP allocation for a VIP from the specified pool
func (a *Allocator) Release(poolName, vipName string) {
	a.mu.RLock()
	pool, ok := a.pools[poolName]
	a.mu.RUnlock()

	if !ok {
		return
	}

	pool.Release(vipName)

	a.logger.Info("IP released",
		zap.String("pool", poolName),
		zap.String("vip", vipName),
	)
}

// GetPoolStats returns allocation statistics for a pool
func (a *Allocator) GetPoolStats(poolName string) (allocated, available int, err error) {
	a.mu.RLock()
	pool, ok := a.pools[poolName]
	a.mu.RUnlock()

	if !ok {
		return 0, 0, fmt.Errorf("pool %s not found", poolName)
	}

	return pool.GetAllocatedCount(), pool.GetAvailableCount(), nil
}

// GetPoolAllocations returns all allocations for a pool
func (a *Allocator) GetPoolAllocations(poolName string) (map[string]string, error) {
	a.mu.RLock()
	pool, ok := a.pools[poolName]
	a.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("pool %s not found", poolName)
	}

	return pool.GetAllocations(), nil
}

// IsAddressConflict checks if an address is already allocated in any pool
func (a *Allocator) IsAddressConflict(address string) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for poolName, pool := range a.pools {
		if pool.IsAllocated(address) {
			return poolName, true
		}
	}

	return "", false
}

// GetPoolNames returns the names of all registered pools
func (a *Allocator) GetPoolNames() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	names := make([]string, 0, len(a.pools))
	for name := range a.pools {
		names = append(names, name)
	}
	return names
}
