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
	"fmt"
	"net"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	ipampb "github.com/azrtydxb/novanet/api/v1"
)

const (
	// ownerNovaEdge is the owner identifier used for all NovaEdge IPAM allocations.
	ownerNovaEdge = "novaedge"
)

// Verify that GRPCAllocator implements Client.
var _ Client = (*GRPCAllocator)(nil)

// GRPCAllocator implements Client by delegating to NovaNet's IPAM gRPC service.
// It bridges semantic differences between NovaEdge's IPAM model (resource-based,
// CIDR return format) and NovaNet's gRPC API (owner+resource, bare IP).
type GRPCAllocator struct {
	conn   *grpc.ClientConn
	client ipampb.IPAMServiceClient
	logger *zap.Logger
}

// NewGRPCAllocator creates a GRPCAllocator connected to the given Unix socket path.
func NewGRPCAllocator(socketPath string, logger *zap.Logger) (*GRPCAllocator, error) {
	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to IPAM socket %s: %w", socketPath, err)
	}

	return &GRPCAllocator{
		conn:   conn,
		client: ipampb.NewIPAMServiceClient(conn),
		logger: logger.Named("ipam-grpc"),
	}, nil
}

// Close closes the underlying gRPC connection.
func (g *GRPCAllocator) Close() error {
	return g.conn.Close()
}

// Allocate allocates an IP from the specified pool for the given resource.
// It provides idempotency by checking for an existing allocation before requesting a new one.
// Returns the allocated address in CIDR notation (e.g. "10.0.0.1/32" or "2001:db8::1/128").
func (g *GRPCAllocator) Allocate(ctx context.Context, poolName, resource string) (string, error) {
	// Check for existing allocation (idempotency).
	existing, err := g.findAllocationByResource(ctx, poolName, resource)
	if err != nil {
		return "", fmt.Errorf("failed to check existing allocation: %w", err)
	}
	if existing != "" {
		g.logger.Debug("Returning existing allocation",
			zap.String("pool", poolName),
			zap.String("resource", resource),
			zap.String("ip", existing),
		)
		return ipToCIDR(existing), nil
	}

	// Request new allocation.
	resp, err := g.client.Allocate(ctx, &ipampb.AllocateRequest{
		PoolName: poolName,
		Owner:    ownerNovaEdge,
		Resource: resource,
	})
	if err != nil {
		return "", fmt.Errorf("IPAM allocate failed for pool %s resource %s: %w", poolName, resource, err)
	}

	g.logger.Info("IP allocated via gRPC",
		zap.String("pool", poolName),
		zap.String("resource", resource),
		zap.String("address", resp.GetIp()),
	)

	return ipToCIDR(resp.GetIp()), nil
}

// Release releases an IP allocation for the given resource from the specified pool.
// It looks up the IP by resource identifier, then releases it.
func (g *GRPCAllocator) Release(ctx context.Context, poolName, resource string) error {
	// Find the IP allocated for this resource.
	ip, err := g.findAllocationByResource(ctx, poolName, resource)
	if err != nil {
		return fmt.Errorf("failed to find allocation for release: %w", err)
	}
	if ip == "" {
		// No allocation found — nothing to release.
		return nil
	}

	// Release the IP.
	_, err = g.client.Release(ctx, &ipampb.ReleaseRequest{
		PoolName: poolName,
		Ip:       ip,
	})
	if err != nil {
		return fmt.Errorf("IPAM release failed for pool %s ip %s: %w", poolName, ip, err)
	}

	g.logger.Info("IP released via gRPC",
		zap.String("pool", poolName),
		zap.String("resource", resource),
		zap.String("ip", ip),
	)

	return nil
}

// GetPoolStats returns allocation statistics for a pool.
func (g *GRPCAllocator) GetPoolStats(ctx context.Context, poolName string) (allocated, available int, err error) {
	resp, err := g.client.GetPool(ctx, &ipampb.GetPoolRequest{
		Name: poolName,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("IPAM GetPool failed for %s: %w", poolName, err)
	}

	return int(resp.GetAllocated()), int(resp.GetAvailable()), nil
}

// GetPoolAllocations returns all allocations for a pool as a map of IP -> resource name.
func (g *GRPCAllocator) GetPoolAllocations(ctx context.Context, poolName string) (map[string]string, error) {
	resp, err := g.client.ListIPAllocations(ctx, &ipampb.ListIPAllocationsRequest{
		PoolFilter: poolName,
	})
	if err != nil {
		return nil, fmt.Errorf("IPAM ListIPAllocations failed for pool %s: %w", poolName, err)
	}

	result := make(map[string]string, len(resp.GetAllocations()))
	for _, alloc := range resp.GetAllocations() {
		result[alloc.GetIp()] = alloc.GetResource()
	}

	return result, nil
}

// GetPoolNames returns the names of all registered pools.
func (g *GRPCAllocator) GetPoolNames(ctx context.Context) []string {
	resp, err := g.client.ListIPPools(ctx, &ipampb.ListIPPoolsRequest{})
	if err != nil {
		g.logger.Error("Failed to list IP pools via gRPC", zap.Error(err))
		return nil
	}

	names := make([]string, 0, len(resp.GetPools()))
	for _, pool := range resp.GetPools() {
		names = append(names, pool.GetName())
	}
	return names
}

// IsAddressConflict checks if an address is already allocated in any pool.
func (g *GRPCAllocator) IsAddressConflict(ctx context.Context, address string) (string, bool) {
	resp, err := g.client.ListIPPools(ctx, &ipampb.ListIPPoolsRequest{})
	if err != nil {
		g.logger.Error("Failed to list IP pools for conflict check", zap.Error(err))
		return "", false
	}

	for _, pool := range resp.GetPools() {
		allocResp, allocErr := g.client.ListIPAllocations(ctx, &ipampb.ListIPAllocationsRequest{
			PoolFilter: pool.GetName(),
		})
		if allocErr != nil {
			continue
		}
		for _, alloc := range allocResp.GetAllocations() {
			if alloc.GetIp() == address {
				return pool.GetName(), true
			}
		}
	}

	return "", false
}

// findAllocationByResource looks up an existing allocation by owner and resource in a pool.
func (g *GRPCAllocator) findAllocationByResource(ctx context.Context, poolName, resource string) (string, error) {
	resp, err := g.client.ListIPAllocations(ctx, &ipampb.ListIPAllocationsRequest{
		PoolFilter:  poolName,
		OwnerFilter: ownerNovaEdge,
	})
	if err != nil {
		return "", err
	}

	for _, alloc := range resp.GetAllocations() {
		if alloc.GetResource() == resource {
			return alloc.GetIp(), nil
		}
	}

	return "", nil
}

// ipToCIDR appends /32 (IPv4) or /128 (IPv6) to a bare IP address.
func ipToCIDR(ip string) string {
	if strings.Contains(ip, "/") {
		return ip // already in CIDR notation
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip + "/32" // fallback
	}
	if parsed.To4() != nil {
		return ip + "/32"
	}
	return ip + "/128"
}
