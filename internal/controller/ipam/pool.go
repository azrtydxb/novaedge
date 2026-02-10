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

// Package ipam provides IP Address Management for VIP allocation from pools.
package ipam

import (
	"fmt"
	"math/big"
	"net"
	"sort"
	"sync"
)

// Pool represents an IP address pool with allocation tracking
type Pool struct {
	mu sync.RWMutex

	// name is the pool identifier
	name string

	// cidrs are the CIDR ranges in this pool
	cidrs []*net.IPNet

	// addresses are explicit addresses in this pool
	addresses []net.IP

	// allocated tracks which addresses are currently allocated (IP string -> VIP name)
	allocated map[string]string

	// allAddresses is the expanded set of all available addresses
	allAddresses []net.IP
}

// NewPool creates a new IP address pool
func NewPool(name string, cidrs []string, addresses []string) (*Pool, error) {
	pool := &Pool{
		name:      name,
		allocated: make(map[string]string),
	}

	// Parse CIDRs and expand into individual addresses
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
		}
		pool.cidrs = append(pool.cidrs, ipNet)

		// Expand CIDR into individual host addresses
		expanded := expandCIDR(ipNet)
		pool.allAddresses = append(pool.allAddresses, expanded...)
	}

	// Parse explicit addresses
	for _, addr := range addresses {
		ip, _, err := net.ParseCIDR(addr)
		if err != nil {
			// Try parsing as plain IP
			ip = net.ParseIP(addr)
			if ip == nil {
				return nil, fmt.Errorf("invalid address %s: %w", addr, err)
			}
		}
		pool.addresses = append(pool.addresses, ip)
		pool.allAddresses = append(pool.allAddresses, ip)
	}

	// Remove duplicates
	pool.allAddresses = deduplicateIPs(pool.allAddresses)

	return pool, nil
}

// Allocate allocates an IP address from the pool for the given VIP name.
// Returns the allocated IP in CIDR notation (e.g., "10.200.0.1/32" or "2001:db8::1/128").
func (p *Pool) Allocate(vipName string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if VIP already has an allocation
	for addr, name := range p.allocated {
		if name == vipName {
			return formatAllocation(net.ParseIP(addr)), nil
		}
	}

	// Find the first available address
	for _, ip := range p.allAddresses {
		ipStr := ip.String()
		if _, taken := p.allocated[ipStr]; !taken {
			p.allocated[ipStr] = vipName
			return formatAllocation(ip), nil
		}
	}

	return "", fmt.Errorf("pool %s exhausted: no available addresses", p.name)
}

// Release releases an IP allocation for a VIP
func (p *Pool) Release(vipName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for addr, name := range p.allocated {
		if name == vipName {
			delete(p.allocated, addr)
			return
		}
	}
}

// IsAllocated checks if an address is already allocated
func (p *Pool) IsAllocated(address string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	ip := net.ParseIP(address)
	if ip == nil {
		// Try parsing CIDR
		var err error
		ip, _, err = net.ParseCIDR(address)
		if err != nil {
			return false
		}
	}

	_, allocated := p.allocated[ip.String()]
	return allocated
}

// GetAllocatedCount returns the number of allocated addresses
func (p *Pool) GetAllocatedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.allocated)
}

// GetAvailableCount returns the number of available addresses
func (p *Pool) GetAvailableCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.allAddresses) - len(p.allocated)
}

// GetTotalCount returns the total number of addresses in the pool
func (p *Pool) GetTotalCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.allAddresses)
}

// GetAllocations returns all current allocations (IP -> VIP name)
func (p *Pool) GetAllocations() map[string]string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]string, len(p.allocated))
	for addr, vipName := range p.allocated {
		result[addr] = vipName
	}
	return result
}

// Contains checks if an IP address is within this pool's ranges
func (p *Pool) Contains(address string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	ip := net.ParseIP(address)
	if ip == nil {
		return false
	}

	for _, ipNet := range p.cidrs {
		if ipNet.Contains(ip) {
			return true
		}
	}

	for _, poolIP := range p.addresses {
		if poolIP.Equal(ip) {
			return true
		}
	}

	return false
}

// expandCIDR expands a CIDR into individual host IP addresses
// Excludes network and broadcast addresses for IPv4
func expandCIDR(ipNet *net.IPNet) []net.IP {
	var ips []net.IP

	// Determine if IPv4 or IPv6
	ones, bits := ipNet.Mask.Size()
	if bits == 0 {
		return ips
	}

	// For /32 (IPv4) or /128 (IPv6), return the single IP
	if ones == bits {
		ip := make(net.IP, len(ipNet.IP))
		copy(ip, ipNet.IP)
		return []net.IP{ip}
	}

	// Calculate the number of addresses
	hostBits := bits - ones
	numAddresses := new(big.Int).Lsh(big.NewInt(1), uint(hostBits))

	// Limit expansion to prevent memory issues (max 65536 addresses per CIDR)
	maxExpansion := big.NewInt(65536)
	if numAddresses.Cmp(maxExpansion) > 0 {
		numAddresses = maxExpansion
	}

	// Convert base IP to big.Int for arithmetic
	baseIP := ipToInt(ipNet.IP)

	// Skip network address (first) and broadcast address (last) for IPv4
	startOffset := big.NewInt(1)
	endOffset := new(big.Int).Sub(numAddresses, big.NewInt(1))

	if bits == 128 {
		// For IPv6, include all addresses (no broadcast concept)
		startOffset = big.NewInt(0)
		endOffset = numAddresses
	}

	for i := new(big.Int).Set(startOffset); i.Cmp(endOffset) < 0; i.Add(i, big.NewInt(1)) {
		addr := new(big.Int).Add(baseIP, i)
		ip := intToIP(addr, bits)
		if ip != nil {
			ips = append(ips, ip)
		}
	}

	return ips
}

// ipToInt converts a net.IP to a big.Int
func ipToInt(ip net.IP) *big.Int {
	// Use native representation to preserve byte length:
	// 4 bytes for IPv4, 16 bytes for IPv6
	if v4 := ip.To4(); v4 != nil {
		return new(big.Int).SetBytes(v4)
	}
	ip = ip.To16()
	if ip == nil {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(ip)
}

// intToIP converts a big.Int back to a net.IP
func intToIP(n *big.Int, bits int) net.IP {
	b := n.Bytes()

	var ipLen int
	if bits == 32 {
		ipLen = 4
	} else {
		ipLen = 16
	}

	// Pad with leading zeros
	ip := make(net.IP, ipLen)
	offset := ipLen - len(b)
	if offset < 0 {
		return nil
	}
	copy(ip[offset:], b)

	return ip
}

// formatAllocation formats an IP as a CIDR allocation string
func formatAllocation(ip net.IP) string {
	if ip == nil {
		return ""
	}
	if ip.To4() != nil {
		return fmt.Sprintf("%s/32", ip.String())
	}
	return fmt.Sprintf("%s/128", ip.String())
}

// deduplicateIPs removes duplicate IP addresses from a slice
func deduplicateIPs(ips []net.IP) []net.IP {
	seen := make(map[string]bool)
	result := make([]net.IP, 0, len(ips))

	for _, ip := range ips {
		key := ip.String()
		if !seen[key] {
			seen[key] = true
			result = append(result, ip)
		}
	}

	// Sort for deterministic allocation order
	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})

	return result
}
