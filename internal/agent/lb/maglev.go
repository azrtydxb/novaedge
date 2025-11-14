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

package lb

import (
	"hash/fnv"
	"sync"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Maglev implements Google's Maglev consistent hashing algorithm
// Uses a fixed-size lookup table for O(1) lookups
type Maglev struct {
	mu        sync.RWMutex
	endpoints []*pb.Endpoint

	// Lookup table mapping hash values to endpoint indices
	// Size must be a prime number
	lookupTable []int

	// Endpoint list (for index lookups)
	healthyEndpoints []*pb.Endpoint

	// Table size (prime number, typically 65537)
	tableSize uint64
}

const (
	// Default Maglev table size (must be prime)
	// 65537 is recommended by the Maglev paper
	defaultMaglevTableSize = 65537
)

// NewMaglev creates a new Maglev load balancer
func NewMaglev(endpoints []*pb.Endpoint) *Maglev {
	m := &Maglev{
		endpoints:        endpoints,
		healthyEndpoints: []*pb.Endpoint{},
		lookupTable:      make([]int, defaultMaglevTableSize),
		tableSize:        defaultMaglevTableSize,
	}

	m.buildLookupTable()
	return m
}

// Select chooses an endpoint using Maglev hashing based on a key
func (m *Maglev) Select(key string) *pb.Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.healthyEndpoints) == 0 {
		return nil
	}

	// Hash the key
	hash := m.hashKey(key)

	// Lookup in table
	idx := hash % m.tableSize
	endpointIdx := m.lookupTable[idx]

	if endpointIdx >= 0 && endpointIdx < len(m.healthyEndpoints) {
		return m.healthyEndpoints[endpointIdx]
	}

	// Fallback to first endpoint if table is corrupted
	return m.healthyEndpoints[0]
}

// SelectDefault selects an endpoint without a key
func (m *Maglev) SelectDefault() *pb.Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.healthyEndpoints) > 0 {
		return m.healthyEndpoints[0]
	}
	return nil
}

// UpdateEndpoints updates the endpoint list and rebuilds the lookup table
func (m *Maglev) UpdateEndpoints(endpoints []*pb.Endpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.endpoints = endpoints
	m.buildLookupTable()
}

// buildLookupTable constructs the Maglev lookup table
// This is the core of the Maglev algorithm
func (m *Maglev) buildLookupTable() {
	// Filter healthy endpoints
	m.healthyEndpoints = []*pb.Endpoint{}
	for _, ep := range m.endpoints {
		if ep.Ready {
			m.healthyEndpoints = append(m.healthyEndpoints, ep)
		}
	}

	n := len(m.healthyEndpoints)
	if n == 0 {
		// No healthy endpoints, clear table
		for i := range m.lookupTable {
			m.lookupTable[i] = -1
		}
		return
	}

	// Generate permutation for each endpoint
	permutations := make([][]uint64, n)
	for i, ep := range m.healthyEndpoints {
		permutations[i] = m.generatePermutation(ep)
	}

	// Build lookup table using Maglev's algorithm
	next := make([]uint64, n)
	for i := range next {
		next[i] = 0
	}

	filled := uint64(0)
	for filled < m.tableSize {
		for i := 0; i < n; i++ {
			c := permutations[i][next[i]]
			for m.lookupTable[c] >= 0 {
				next[i]++
				c = permutations[i][next[i]]
			}
			m.lookupTable[c] = i
			next[i]++
			filled++
			if filled == m.tableSize {
				break
			}
		}
	}
}

// generatePermutation generates a permutation sequence for an endpoint
func (m *Maglev) generatePermutation(ep *pb.Endpoint) []uint64 {
	epKey := endpointKey(ep)

	// Generate offset and skip using two different hash functions
	h1 := m.hashKey(epKey + "#offset")
	h2 := m.hashKey(epKey + "#skip")

	offset := h1 % m.tableSize
	skip := (h2 % (m.tableSize - 1)) + 1 // skip must be >= 1

	// Generate permutation
	perm := make([]uint64, m.tableSize)
	for i := uint64(0); i < m.tableSize; i++ {
		perm[i] = (offset + i*skip) % m.tableSize
	}

	return perm
}

// hashKey hashes a string key to a uint64
func (m *Maglev) hashKey(key string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	return h.Sum64()
}

// GetTableSize returns the size of the lookup table
func (m *Maglev) GetTableSize() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int(m.tableSize)
}

// GetDistribution returns the distribution of endpoints in the lookup table
// Useful for testing and debugging
func (m *Maglev) GetDistribution() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	distribution := make(map[string]int)

	for _, idx := range m.lookupTable {
		if idx >= 0 && idx < len(m.healthyEndpoints) {
			ep := m.healthyEndpoints[idx]
			key := endpointKey(ep)
			distribution[key]++
		}
	}

	return distribution
}
