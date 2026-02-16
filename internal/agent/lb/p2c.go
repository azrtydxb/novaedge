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
	"math/rand/v2"
	"strconv"
	"sync"
	"sync/atomic"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// p2cSnapshot holds an immutable point-in-time view of the data that
// Select() needs. It is stored in an atomic.Value so that Select() can read
// it without acquiring any lock.
type p2cSnapshot struct {
	healthy        []*pb.Endpoint
	endpointKeys   map[*pb.Endpoint]string
	activeRequests map[string]*int64
}

// P2C implements Power of Two Choices load balancing
// Selects the best of two randomly chosen endpoints based on active requests
type P2C struct {
	mu        sync.Mutex // protects writes in UpdateEndpoints only
	endpoints []*pb.Endpoint
	// snap holds a *p2cSnapshot; read lock-free via atomic.Value.
	snap atomic.Value
}

// NewP2C creates a new P2C load balancer
func NewP2C(endpoints []*pb.Endpoint) *P2C {
	activeRequests := make(map[string]*int64)
	for _, ep := range endpoints {
		if ep.Ready {
			key := endpointKey(ep)
			var count int64
			activeRequests[key] = &count
		}
	}

	p := &P2C{
		endpoints: endpoints,
	}
	p.storeSnapshot(endpoints, activeRequests)
	return p
}

// storeSnapshot builds and atomically publishes a new snapshot.
func (p *P2C) storeSnapshot(endpoints []*pb.Endpoint, activeRequests map[string]*int64) {
	healthy := make([]*pb.Endpoint, 0, len(endpoints))
	keys := make(map[*pb.Endpoint]string, len(endpoints))
	for _, ep := range endpoints {
		keys[ep] = endpointKey(ep)
		if ep.Ready {
			healthy = append(healthy, ep)
		}
	}
	p.snap.Store(&p2cSnapshot{
		healthy:        healthy,
		endpointKeys:   keys,
		activeRequests: activeRequests,
	})
}

// loadSnapshot returns the current snapshot, or nil if none has been stored.
func (p *P2C) loadSnapshot() *p2cSnapshot {
	v := p.snap.Load()
	if v == nil {
		return nil
	}
	s, ok := v.(*p2cSnapshot)
	if !ok {
		return nil
	}
	return s
}

// Select chooses an endpoint using Power of Two Choices.
// It reads from an atomic snapshot and never acquires a lock.
func (p *P2C) Select() *pb.Endpoint {
	s := p.loadSnapshot()
	if s == nil || len(s.healthy) == 0 {
		return nil
	}

	if len(s.healthy) == 1 {
		return s.healthy[0]
	}

	// Pick two random endpoints
	// Global rand functions in Go 1.22+ are auto-seeded with crypto/rand and goroutine-safe.
	// math/rand/v2 is intentional here: cryptographic randomness is not needed for load balancing.
	idx1 := rand.IntN(len(s.healthy)) //nolint:gosec // G404: math/rand is acceptable for load balancer selection
	idx2 := rand.IntN(len(s.healthy)) //nolint:gosec // G404: math/rand is acceptable for load balancer selection
	for idx1 == idx2 && len(s.healthy) > 1 {
		idx2 = rand.IntN(len(s.healthy)) //nolint:gosec // G404: math/rand is acceptable for load balancer selection
	}

	ep1 := s.healthy[idx1]
	ep2 := s.healthy[idx2]

	// Choose the one with fewer active requests
	key1 := cachedKeyFromSnapshot(s.endpointKeys, ep1)
	key2 := cachedKeyFromSnapshot(s.endpointKeys, ep2)
	count1 := atomic.LoadInt64(s.activeRequests[key1])
	count2 := atomic.LoadInt64(s.activeRequests[key2])

	if count1 <= count2 {
		return ep1
	}
	return ep2
}

// UpdateEndpoints updates the endpoint list
func (p *P2C) UpdateEndpoints(endpoints []*pb.Endpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()

	oldSnap := p.loadSnapshot()
	var oldActiveRequests map[string]*int64
	if oldSnap != nil {
		oldActiveRequests = oldSnap.activeRequests
	}

	p.endpoints = endpoints

	// Update active requests map
	newActiveRequests := make(map[string]*int64)
	for _, ep := range endpoints {
		if ep.Ready {
			key := endpointKey(ep)
			// Preserve existing counters
			if oldActiveRequests != nil {
				if existing, ok := oldActiveRequests[key]; ok {
					newActiveRequests[key] = existing
					continue
				}
			}
			var count int64
			newActiveRequests[key] = &count
		}
	}
	p.storeSnapshot(endpoints, newActiveRequests)
}

// IncrementActive increments the active request count for an endpoint
func (p *P2C) IncrementActive(endpoint *pb.Endpoint) {
	if endpoint == nil {
		return
	}
	s := p.loadSnapshot()
	if s == nil {
		return
	}
	key := cachedKeyFromSnapshot(s.endpointKeys, endpoint)
	if counter, ok := s.activeRequests[key]; ok {
		atomic.AddInt64(counter, 1)
	}
}

// DecrementActive decrements the active request count for an endpoint
func (p *P2C) DecrementActive(endpoint *pb.Endpoint) {
	if endpoint == nil {
		return
	}
	s := p.loadSnapshot()
	if s == nil {
		return
	}
	key := cachedKeyFromSnapshot(s.endpointKeys, endpoint)
	if counter, ok := s.activeRequests[key]; ok {
		atomic.AddInt64(counter, -1)
	}
}

// GetActiveCount returns the current active request count for an endpoint
func (p *P2C) GetActiveCount(endpoint *pb.Endpoint) int64 {
	if endpoint == nil {
		return 0
	}
	s := p.loadSnapshot()
	if s == nil {
		return 0
	}
	key := cachedKeyFromSnapshot(s.endpointKeys, endpoint)
	if counter, ok := s.activeRequests[key]; ok {
		return atomic.LoadInt64(counter)
	}
	return 0
}

func endpointKey(ep *pb.Endpoint) string {
	return ep.Address + ":" + strconv.FormatInt(int64(ep.Port), 10)
}
