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
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// LeastConn implements least-connections load balancing.
// It tracks the number of active connections per endpoint and selects the
// endpoint with the fewest active connections. When multiple endpoints have
// the same number of connections, one is chosen at random to avoid bias.
type LeastConn struct {
	mu               sync.RWMutex
	endpoints        []*pb.Endpoint
	healthyEndpoints []*pb.Endpoint          // cached healthy list
	endpointKeys     map[*pb.Endpoint]string // cached keys
	// activeConns tracks active connection count per endpoint key ("address:port")
	activeConns map[string]*int64
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
		endpoints:   endpoints,
		activeConns: activeConns,
	}
	lc.buildKeyCache()
	lc.rebuildHealthy()
	return lc
}

// Select chooses the endpoint with the fewest active connections.
// If multiple endpoints are tied, one is chosen randomly among those with the
// minimum connection count.
func (lc *LeastConn) Select() *pb.Endpoint {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	healthy := lc.getHealthyEndpoints()
	if len(healthy) == 0 {
		return nil
	}

	if len(healthy) == 1 {
		return healthy[0]
	}

	// Find the minimum active connection count
	minConns := int64(math.MaxInt64)
	for _, ep := range healthy {
		key := lc.cachedKey(ep)
		if counter, ok := lc.activeConns[key]; ok {
			count := atomic.LoadInt64(counter)
			if count < minConns {
				minConns = count
			}
		}
	}

	// Collect all endpoints with the minimum count
	candidates := make([]*pb.Endpoint, 0, len(healthy))
	for _, ep := range healthy {
		key := lc.cachedKey(ep)
		if counter, ok := lc.activeConns[key]; ok {
			if atomic.LoadInt64(counter) == minConns {
				candidates = append(candidates, ep)
			}
		}
	}

	// Fallback if atomic counters changed between two passes (concurrent access)
	if len(candidates) == 0 {
		return healthy[rand.IntN(len(healthy))]
	}

	// Random selection among tied candidates to prevent bias
	if len(candidates) == 1 {
		return candidates[0]
	}
	return candidates[rand.IntN(len(candidates))]
}

// UpdateEndpoints updates the endpoint list, preserving active connection
// counters for endpoints that remain.
func (lc *LeastConn) UpdateEndpoints(endpoints []*pb.Endpoint) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	lc.endpoints = endpoints

	newActiveConns := make(map[string]*int64)
	for _, ep := range endpoints {
		if ep.Ready {
			key := endpointKey(ep)
			if existing, ok := lc.activeConns[key]; ok {
				newActiveConns[key] = existing
			} else {
				var count int64
				newActiveConns[key] = &count
			}
		}
	}
	lc.activeConns = newActiveConns
	lc.buildKeyCache()
	lc.rebuildHealthy()
}

// IncrementActive increments the active connection count for an endpoint.
// Call this when a new request is sent to the endpoint.
func (lc *LeastConn) IncrementActive(endpoint *pb.Endpoint) {
	if endpoint == nil {
		return
	}
	lc.mu.RLock()
	key := lc.cachedKey(endpoint)
	counter := lc.activeConns[key]
	lc.mu.RUnlock()
	if counter != nil {
		atomic.AddInt64(counter, 1)
	}
}

// DecrementActive decrements the active connection count for an endpoint.
// Call this when a request to the endpoint completes.
func (lc *LeastConn) DecrementActive(endpoint *pb.Endpoint) {
	if endpoint == nil {
		return
	}
	lc.mu.RLock()
	key := lc.cachedKey(endpoint)
	counter := lc.activeConns[key]
	lc.mu.RUnlock()
	if counter != nil {
		atomic.AddInt64(counter, -1)
	}
}

// GetActiveCount returns the current active connection count for an endpoint.
func (lc *LeastConn) GetActiveCount(endpoint *pb.Endpoint) int64 {
	if endpoint == nil {
		return 0
	}
	lc.mu.RLock()
	key := lc.cachedKey(endpoint)
	counter := lc.activeConns[key]
	lc.mu.RUnlock()
	if counter != nil {
		return atomic.LoadInt64(counter)
	}
	return 0
}

// rebuildHealthy filters and caches the list of healthy endpoints.
// Must be called with lc.mu held (write lock).
func (lc *LeastConn) rebuildHealthy() {
	healthy := make([]*pb.Endpoint, 0, len(lc.endpoints))
	for _, ep := range lc.endpoints {
		if ep.Ready {
			healthy = append(healthy, ep)
		}
	}
	lc.healthyEndpoints = healthy
}

func (lc *LeastConn) getHealthyEndpoints() []*pb.Endpoint {
	return lc.healthyEndpoints
}

// buildKeyCache pre-computes endpoint keys for all endpoints.
// Must be called with lc.mu held (write lock).
func (lc *LeastConn) buildKeyCache() {
	lc.endpointKeys = make(map[*pb.Endpoint]string, len(lc.endpoints))
	for _, ep := range lc.endpoints {
		lc.endpointKeys[ep] = endpointKey(ep)
	}
}

// cachedKey returns the cached key for an endpoint, falling back to computing it
// if the pointer is not in the cache.
func (lc *LeastConn) cachedKey(ep *pb.Endpoint) string {
	if key, ok := lc.endpointKeys[ep]; ok {
		return key
	}
	return endpointKey(ep)
}
