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
	"sync/atomic"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// maglevData holds the immutable Maglev lookup table state that is atomically swapped.
type maglevData struct {
	// Lookup table mapping hash values to endpoint indices
	lookupTable []int
	// Healthy endpoints used to build this table
	healthyEndpoints []*pb.Endpoint
	// All endpoints (including unhealthy) for reference
	endpoints []*pb.Endpoint
}

// Maglev implements Google's Maglev consistent hashing algorithm.
// Select() is lock-free via atomic load of the immutable maglevData.
// UpdateEndpoints() builds a new table, then atomically swaps it in.
type Maglev struct {
	data atomic.Pointer[maglevData]

	// mu serialises concurrent UpdateEndpoints calls so only one rebuild
	// runs at a time; Select() never acquires this lock.
	mu sync.Mutex

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
		tableSize: defaultMaglevTableSize,
	}

	md := m.buildLookupTable(endpoints)
	m.data.Store(md)
	return m
}

// Select chooses an endpoint using Maglev hashing based on a key.
// This method is lock-free.
func (m *Maglev) Select(key string) *pb.Endpoint {
	md := m.data.Load()

	if len(md.healthyEndpoints) == 0 {
		return nil
	}

	// Hash the key
	hash := m.hashKey(key)

	// Lookup in table
	idx := hash % m.tableSize
	endpointIdx := md.lookupTable[idx]

	if endpointIdx >= 0 && endpointIdx < len(md.healthyEndpoints) {
		return md.healthyEndpoints[endpointIdx]
	}

	// Fallback to first endpoint if table is corrupted
	return md.healthyEndpoints[0]
}

// SelectDefault selects an endpoint without a key.
// This method is lock-free.
func (m *Maglev) SelectDefault() *pb.Endpoint {
	md := m.data.Load()

	if len(md.healthyEndpoints) > 0 {
		return md.healthyEndpoints[0]
	}
	return nil
}

// UpdateEndpoints updates the endpoint list and rebuilds the lookup table.
// The new table is built into local variables (no lock held for Select),
// then atomically swapped in.
func (m *Maglev) UpdateEndpoints(endpoints []*pb.Endpoint) {
	// Build the new table outside any lock that Select uses
	md := m.buildLookupTable(endpoints)

	// Serialise concurrent UpdateEndpoints calls
	m.mu.Lock()
	m.data.Store(md)
	m.mu.Unlock()
}

// buildLookupTable constructs an immutable maglevData from the given endpoints.
// This is a pure function that does not touch any shared state.
func (m *Maglev) buildLookupTable(endpoints []*pb.Endpoint) *maglevData {
	// Filter healthy endpoints
	healthyEndpoints := make([]*pb.Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		if ep.Ready {
			healthyEndpoints = append(healthyEndpoints, ep)
		}
	}

	lookupTable := make([]int, m.tableSize)

	n := len(healthyEndpoints)
	if n == 0 {
		// No healthy endpoints, clear table
		for i := range lookupTable {
			lookupTable[i] = -1
		}
		return &maglevData{
			lookupTable:      lookupTable,
			healthyEndpoints: healthyEndpoints,
			endpoints:        endpoints,
		}
	}

	// Initialize lookup table to -1 (empty) before filling
	for i := range lookupTable {
		lookupTable[i] = -1
	}

	// Generate permutation for each endpoint
	permutations := make([][]uint64, n)
	for i, ep := range healthyEndpoints {
		permutations[i] = m.generatePermutation(ep)
	}

	// Build lookup table using Maglev's algorithm
	next := make([]uint64, n)

	filled := uint64(0)
	for filled < m.tableSize {
		for i := 0; i < n; i++ {
			c := permutations[i][next[i]]
			for lookupTable[c] >= 0 {
				next[i]++
				c = permutations[i][next[i]]
			}
			lookupTable[c] = i
			next[i]++
			filled++
			if filled == m.tableSize {
				break
			}
		}
	}

	return &maglevData{
		lookupTable:      lookupTable,
		healthyEndpoints: healthyEndpoints,
		endpoints:        endpoints,
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
	_, _ = h.Write([]byte(key))
	return h.Sum64()
}

// GetTableSize returns the size of the lookup table
func (m *Maglev) GetTableSize() uint64 {
	return m.tableSize
}

// GetDistribution returns the distribution of endpoints in the lookup table
// Useful for testing and debugging
func (m *Maglev) GetDistribution() map[string]int {
	md := m.data.Load()

	distribution := make(map[string]int)

	for _, idx := range md.lookupTable {
		if idx >= 0 && idx < len(md.healthyEndpoints) {
			ep := md.healthyEndpoints[idx]
			key := endpointKey(ep)
			distribution[key]++
		}
	}

	return distribution
}
