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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// ringData holds the immutable hash ring state that is atomically swapped.
type ringData struct {
	// sorted hash values forming the ring
	ring []uint32
	// maps each hash value to its endpoint
	hashToEndpoint map[uint32]*pb.Endpoint
	// snapshot of endpoints used to build this ring
	endpoints []*pb.Endpoint
}

// RingHash implements consistent hashing with virtual nodes.
// Select() is lock-free via atomic load of the immutable ringData.
// UpdateEndpoints() builds a new ring without holding locks, then swaps atomically.
type RingHash struct {
	data atomic.Pointer[ringData]

	// mu serialises concurrent UpdateEndpoints calls so only one rebuild
	// runs at a time; Select() never acquires this lock.
	mu sync.Mutex

	// Number of virtual nodes per endpoint
	virtualNodes int
}

const (
	// Default number of virtual nodes per endpoint.
	// 100 virtual nodes provides good distribution while keeping
	// rebuild cost lower than the previous value of 150.
	defaultVirtualNodes = 100
)

// ringEntry represents a position on the hash ring
type ringEntry struct {
	hash     uint32
	endpoint *pb.Endpoint
}

// NewRingHash creates a new Ring Hash load balancer
func NewRingHash(endpoints []*pb.Endpoint) *RingHash {
	rh := &RingHash{
		virtualNodes: defaultVirtualNodes,
	}

	rd := rh.buildRing(endpoints)
	rh.data.Store(rd)
	return rh
}

// Select chooses an endpoint using consistent hashing based on a key.
// This method is lock-free.
func (rh *RingHash) Select(key string) *pb.Endpoint {
	rd := rh.data.Load()

	if len(rd.ring) == 0 {
		return nil
	}

	// Hash the key
	hash := hashKey(key)

	// Binary search to find the first hash >= our hash
	idx := sort.Search(len(rd.ring), func(i int) bool {
		return rd.ring[i] >= hash
	})

	// Wrap around if we're past the end
	if idx == len(rd.ring) {
		idx = 0
	}

	return rd.hashToEndpoint[rd.ring[idx]]
}

// SelectDefault selects an endpoint without a key (uses first healthy endpoint)
func (rh *RingHash) SelectDefault() *pb.Endpoint {
	rd := rh.data.Load()

	for _, ep := range rd.endpoints {
		if ep.Ready {
			return ep
		}
	}
	return nil
}

// UpdateEndpoints updates the endpoint list and rebuilds the ring.
// The new ring is built into local variables (no lock held for Select),
// then atomically swapped in.
func (rh *RingHash) UpdateEndpoints(endpoints []*pb.Endpoint) {
	// Build the new ring outside any lock that Select uses
	rd := rh.buildRing(endpoints)

	// Serialise concurrent UpdateEndpoints calls
	rh.mu.Lock()
	rh.data.Store(rd)
	rh.mu.Unlock()
}

// buildRing constructs an immutable ringData from the given endpoints.
// This is a pure function that does not touch any shared state.
func (rh *RingHash) buildRing(endpoints []*pb.Endpoint) *ringData {
	entries := []ringEntry{}

	// Create virtual nodes for each healthy endpoint
	for _, ep := range endpoints {
		if !ep.Ready {
			continue
		}

		epKey := endpointKey(ep)

		// Create virtual nodes
		for i := 0; i < rh.virtualNodes; i++ {
			// Generate unique key for each virtual node.
			// Use strconv.Itoa instead of string(rune(i)) to avoid
			// multi-byte UTF-8 collisions for i >= 128.
			var b strings.Builder
			b.Grow(len(epKey) + 1 + 3) // epKey + "#" + up to 3 digits
			b.WriteString(epKey)
			b.WriteByte('#')
			b.WriteString(strconv.Itoa(i))
			hash := hashKey(b.String())

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
	ring := make([]uint32, len(entries))
	hashToEndpoint := make(map[uint32]*pb.Endpoint, len(entries))

	for i, entry := range entries {
		ring[i] = entry.hash
		hashToEndpoint[entry.hash] = entry.endpoint
	}

	return &ringData{
		ring:           ring,
		hashToEndpoint: hashToEndpoint,
		endpoints:      endpoints,
	}
}

// hashKey hashes a string key to a uint32
func hashKey(key string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return h.Sum32()
}

// GetRingSize returns the current size of the hash ring
func (rh *RingHash) GetRingSize() int {
	rd := rh.data.Load()
	return len(rd.ring)
}
