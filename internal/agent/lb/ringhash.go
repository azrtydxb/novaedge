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
	"sort"
	"sync"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// RingHash implements consistent hashing with virtual nodes
type RingHash struct {
	mu        sync.RWMutex
	endpoints []*pb.Endpoint

	// Hash ring: sorted list of hash values
	ring []uint32

	// Map hash values to endpoints
	hashToEndpoint map[uint32]*pb.Endpoint

	// Number of virtual nodes per endpoint
	virtualNodes int
}

const (
	// Default number of virtual nodes per endpoint
	// More virtual nodes = better distribution but more memory
	defaultVirtualNodes = 150
)

// ringEntry represents a position on the hash ring
type ringEntry struct {
	hash     uint32
	endpoint *pb.Endpoint
}

// NewRingHash creates a new Ring Hash load balancer
func NewRingHash(endpoints []*pb.Endpoint) *RingHash {
	rh := &RingHash{
		endpoints:      endpoints,
		ring:           []uint32{},
		hashToEndpoint: make(map[uint32]*pb.Endpoint),
		virtualNodes:   defaultVirtualNodes,
	}

	rh.buildRing()
	return rh
}

// Select chooses an endpoint using consistent hashing based on a key
func (rh *RingHash) Select(key string) *pb.Endpoint {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	if len(rh.ring) == 0 {
		return nil
	}

	// Hash the key
	hash := rh.hashKey(key)

	// Binary search to find the first hash >= our hash
	idx := sort.Search(len(rh.ring), func(i int) bool {
		return rh.ring[i] >= hash
	})

	// Wrap around if we're past the end
	if idx == len(rh.ring) {
		idx = 0
	}

	return rh.hashToEndpoint[rh.ring[idx]]
}

// SelectDefault selects an endpoint without a key (uses first healthy endpoint)
func (rh *RingHash) SelectDefault() *pb.Endpoint {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	for _, ep := range rh.endpoints {
		if ep.Ready {
			return ep
		}
	}
	return nil
}

// UpdateEndpoints updates the endpoint list and rebuilds the ring
func (rh *RingHash) UpdateEndpoints(endpoints []*pb.Endpoint) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	rh.endpoints = endpoints
	rh.buildRing()
}

// buildRing constructs the hash ring from current endpoints
func (rh *RingHash) buildRing() {
	entries := []ringEntry{}

	// Create virtual nodes for each healthy endpoint
	for _, ep := range rh.endpoints {
		if !ep.Ready {
			continue
		}

		epKey := endpointKey(ep)

		// Create virtual nodes
		for i := 0; i < rh.virtualNodes; i++ {
			// Generate unique key for each virtual node
			virtualKey := epKey + "#" + string(rune(i))
			hash := rh.hashKey(virtualKey)

			entries = append(entries, ringEntry{
				hash:     hash,
				endpoint: ep,
			})
		}
	}

	// Sort entries by hash value
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].hash < entries[j].hash
	})

	// Build sorted ring and hash-to-endpoint map
	rh.ring = make([]uint32, len(entries))
	rh.hashToEndpoint = make(map[uint32]*pb.Endpoint)

	for i, entry := range entries {
		rh.ring[i] = entry.hash
		rh.hashToEndpoint[entry.hash] = entry.endpoint
	}
}

// hashKey hashes a string key to a uint32
func (rh *RingHash) hashKey(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

// GetRingSize returns the current size of the hash ring
func (rh *RingHash) GetRingSize() int {
	rh.mu.RLock()
	defer rh.mu.RUnlock()
	return len(rh.ring)
}
