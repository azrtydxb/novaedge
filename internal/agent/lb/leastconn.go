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
	"crypto/rand"
	"encoding/binary"
	"math"
	"sync"
	"sync/atomic"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// leastConnSnapshot holds an immutable point-in-time view of the data that
// Select() needs. It is stored in an atomic.Value so that Select() can read
// it without acquiring any lock.
type leastConnSnapshot struct {
	healthy      []*pb.Endpoint
	endpointKeys map[*pb.Endpoint]string
	activeConns  map[string]*int64
}

// LeastConn implements least-connections load balancing.
// It tracks the number of active connections per endpoint and selects the
// endpoint with the fewest active connections. When multiple endpoints have
// the same number of connections, one is chosen at random to avoid bias.
type LeastConn struct {
	mu        sync.Mutex // protects writes in UpdateEndpoints only
	endpoints []*pb.Endpoint
	// snap holds a *leastConnSnapshot; read lock-free via atomic.Value.
	snap atomic.Value
}

// NewLeastConn creates a new least-connections load balancer.
func NewLeastConn(endpoints []*pb.Endpoint) *LeastConn {
	activeConns := make(map[string]*int64)
	for _, ep := range endpoints {
		if ep.Ready {
			key := endpointKey(ep)
			var count int64
			activeConns[key] = &count
		}
	}

	lc := &LeastConn{
		endpoints: endpoints,
	}
	lc.storeSnapshot(endpoints, activeConns)
	return lc
}

// storeSnapshot builds and atomically publishes a new snapshot.
func (lc *LeastConn) storeSnapshot(endpoints []*pb.Endpoint, activeConns map[string]*int64) {
	healthy := make([]*pb.Endpoint, 0, len(endpoints))
	keys := make(map[*pb.Endpoint]string, len(endpoints))
	for _, ep := range endpoints {
		keys[ep] = endpointKey(ep)
		if ep.Ready {
			healthy = append(healthy, ep)
		}
	}
	lc.snap.Store(&leastConnSnapshot{
		healthy:      healthy,
		endpointKeys: keys,
		activeConns:  activeConns,
	})
}

// loadSnapshot returns the current snapshot, or nil if none has been stored.
func (lc *LeastConn) loadSnapshot() *leastConnSnapshot {
	v := lc.snap.Load()
	if v == nil {
		return nil
	}
	s, ok := v.(*leastConnSnapshot)
	if !ok {
		return nil
	}
	return s
}

// Select chooses the endpoint with the fewest active connections.
// It reads from an atomic snapshot and never acquires a lock.
func (lc *LeastConn) Select() *pb.Endpoint {
	s := lc.loadSnapshot()
	if s == nil || len(s.healthy) == 0 {
		return nil
	}

	if len(s.healthy) == 1 {
		return s.healthy[0]
	}

	// Single-pass: find the endpoint with the minimum active connection count.
	// On ties, use reservoir sampling (random replacement) to avoid bias.
	var best *pb.Endpoint
	minConns := int64(math.MaxInt64)
	tieCount := 0

	for _, ep := range s.healthy {
		key := cachedKeyFromSnapshot(s.endpointKeys, ep)
		counter, ok := s.activeConns[key]
		if !ok {
			continue
		}
		count := atomic.LoadInt64(counter)
		if count < minConns {
			minConns = count
			best = ep
			tieCount = 1
		} else if count == minConns {
			tieCount++
			// Reservoir sampling: replace with probability 1/tieCount
			if cryptoRandIntn(tieCount) == 0 {
				best = ep
			}
		}
	}

	if best != nil {
		return best
	}
	return s.healthy[cryptoRandIntn(len(s.healthy))]
}

// UpdateEndpoints updates the endpoint list, preserving active connection
// counters for endpoints that remain.
func (lc *LeastConn) UpdateEndpoints(endpoints []*pb.Endpoint) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	oldSnap := lc.loadSnapshot()
	var oldActiveConns map[string]*int64
	if oldSnap != nil {
		oldActiveConns = oldSnap.activeConns
	}

	lc.endpoints = endpoints

	newActiveConns := make(map[string]*int64)
	for _, ep := range endpoints {
		if ep.Ready {
			key := endpointKey(ep)
			if oldActiveConns != nil {
				if existing, ok := oldActiveConns[key]; ok {
					newActiveConns[key] = existing
					continue
				}
			}
			var count int64
			newActiveConns[key] = &count
		}
	}
	lc.storeSnapshot(endpoints, newActiveConns)
}

// IncrementActive increments the active connection count for an endpoint.
// Call this when a new request is sent to the endpoint.
func (lc *LeastConn) IncrementActive(endpoint *pb.Endpoint) {
	if endpoint == nil {
		return
	}
	s := lc.loadSnapshot()
	if s == nil {
		return
	}
	key := cachedKeyFromSnapshot(s.endpointKeys, endpoint)
	if counter, ok := s.activeConns[key]; ok {
		atomic.AddInt64(counter, 1)
	}
}

// DecrementActive decrements the active connection count for an endpoint.
// Call this when a request to the endpoint completes.
func (lc *LeastConn) DecrementActive(endpoint *pb.Endpoint) {
	if endpoint == nil {
		return
	}
	s := lc.loadSnapshot()
	if s == nil {
		return
	}
	key := cachedKeyFromSnapshot(s.endpointKeys, endpoint)
	if counter, ok := s.activeConns[key]; ok {
		atomic.AddInt64(counter, -1)
	}
}

// GetActiveCount returns the current active connection count for an endpoint.
func (lc *LeastConn) GetActiveCount(endpoint *pb.Endpoint) int64 {
	if endpoint == nil {
		return 0
	}
	s := lc.loadSnapshot()
	if s == nil {
		return 0
	}
	key := cachedKeyFromSnapshot(s.endpointKeys, endpoint)
	if counter, ok := s.activeConns[key]; ok {
		return atomic.LoadInt64(counter)
	}
	return 0
}

// cachedKeyFromSnapshot returns the cached key for an endpoint from the given
// key map, falling back to computing it if the pointer is not in the cache.
func cachedKeyFromSnapshot(keys map[*pb.Endpoint]string, ep *pb.Endpoint) string {
	if key, ok := keys[ep]; ok {
		return key
	}
	return endpointKey(ep)
}

// cryptoRandIntn returns a random int in [0, n) using crypto/rand.
func cryptoRandIntn(n int) int {
	if n <= 1 {
		return 0
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0
	}
	result := binary.LittleEndian.Uint64(buf[:]) % uint64(n)
	if result > uint64(math.MaxInt) {
		return 0
	}
	return int(result)
}
