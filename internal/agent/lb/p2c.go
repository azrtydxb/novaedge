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
	"math/rand"
	"sync"
	"sync/atomic"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// P2C implements Power of Two Choices load balancing
// Selects the best of two randomly chosen endpoints based on active requests
type P2C struct {
	mu        sync.RWMutex
	endpoints []*pb.Endpoint
	// Track active requests per endpoint
	activeRequests map[string]*int64
	rng            *rand.Rand
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

	return &P2C{
		endpoints:      endpoints,
		activeRequests: activeRequests,
		rng:            rand.New(rand.NewSource(rand.Int63())),
	}
}

// Select chooses an endpoint using Power of Two Choices
func (p *P2C) Select() *pb.Endpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()

	healthy := p.getHealthyEndpoints()
	if len(healthy) == 0 {
		return nil
	}

	if len(healthy) == 1 {
		return healthy[0]
	}

	// Pick two random endpoints
	idx1 := p.rng.Intn(len(healthy))
	idx2 := p.rng.Intn(len(healthy))

	// Ensure we pick different endpoints
	for idx1 == idx2 && len(healthy) > 1 {
		idx2 = p.rng.Intn(len(healthy))
	}

	ep1 := healthy[idx1]
	ep2 := healthy[idx2]

	// Choose the one with fewer active requests
	count1 := atomic.LoadInt64(p.activeRequests[endpointKey(ep1)])
	count2 := atomic.LoadInt64(p.activeRequests[endpointKey(ep2)])

	if count1 <= count2 {
		return ep1
	}
	return ep2
}

// UpdateEndpoints updates the endpoint list
func (p *P2C) UpdateEndpoints(endpoints []*pb.Endpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.endpoints = endpoints

	// Update active requests map
	newActiveRequests := make(map[string]*int64)
	for _, ep := range endpoints {
		if ep.Ready {
			key := endpointKey(ep)
			// Preserve existing counters
			if existing, ok := p.activeRequests[key]; ok {
				newActiveRequests[key] = existing
			} else {
				var count int64
				newActiveRequests[key] = &count
			}
		}
	}
	p.activeRequests = newActiveRequests
}

// IncrementActive increments the active request count for an endpoint
func (p *P2C) IncrementActive(endpoint *pb.Endpoint) {
	if endpoint == nil {
		return
	}
	key := endpointKey(endpoint)
	p.mu.RLock()
	counter := p.activeRequests[key]
	p.mu.RUnlock()
	if counter != nil {
		atomic.AddInt64(counter, 1)
	}
}

// DecrementActive decrements the active request count for an endpoint
func (p *P2C) DecrementActive(endpoint *pb.Endpoint) {
	if endpoint == nil {
		return
	}
	key := endpointKey(endpoint)
	p.mu.RLock()
	counter := p.activeRequests[key]
	p.mu.RUnlock()
	if counter != nil {
		atomic.AddInt64(counter, -1)
	}
}

// GetActiveCount returns the current active request count for an endpoint
func (p *P2C) GetActiveCount(endpoint *pb.Endpoint) int64 {
	if endpoint == nil {
		return 0
	}
	key := endpointKey(endpoint)
	p.mu.RLock()
	counter := p.activeRequests[key]
	p.mu.RUnlock()
	if counter != nil {
		return atomic.LoadInt64(counter)
	}
	return 0
}

func (p *P2C) getHealthyEndpoints() []*pb.Endpoint {
	var healthy []*pb.Endpoint
	for _, ep := range p.endpoints {
		if ep.Ready {
			healthy = append(healthy, ep)
		}
	}
	return healthy
}

func endpointKey(ep *pb.Endpoint) string {
	return ep.Address + ":" + string(rune(ep.Port))
}
