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

import "context"

// Client defines the interface for IP address management operations.
// It is implemented by the local Allocator (for standalone/test use) and
// GRPCAllocator (for production use via NovaNet's IPAM gRPC service).
//
// Pool lifecycle (AddPool/RemovePool) is NOT part of this interface because
// in the gRPC path pools are managed as NovaNet IPPool CRDs by the
// ProxyIPPoolReconciler.
type Client interface {
	// Allocate allocates an IP from the specified pool for the given resource.
	// The resource string identifies the requesting entity (e.g. "proxyvip/my-vip").
	// Returns the allocated address in CIDR notation (e.g. "10.0.0.1/32").
	// Allocation is idempotent: repeated calls for the same resource return the same IP.
	Allocate(ctx context.Context, poolName, resource string) (string, error)

	// Release releases an IP allocation for the given resource from the specified pool.
	Release(ctx context.Context, poolName, resource string) error

	// GetPoolStats returns allocation statistics for a pool.
	GetPoolStats(ctx context.Context, poolName string) (allocated, available int, err error)

	// GetPoolAllocations returns all allocations for a pool as a map of IP -> resource name.
	GetPoolAllocations(ctx context.Context, poolName string) (map[string]string, error)

	// GetPoolNames returns the names of all registered pools.
	GetPoolNames(ctx context.Context) []string

	// IsAddressConflict checks if an address is already allocated in any pool.
	// Returns the pool name and true if conflicted.
	IsAddressConflict(ctx context.Context, address string) (string, bool)
}
