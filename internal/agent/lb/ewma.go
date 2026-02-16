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
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// ewmaSnapshot holds an immutable point-in-time view of the data that
// Select() needs. It is stored in an atomic.Value so that Select() can read
// it without acquiring any lock.
type ewmaSnapshot struct {
	healthy        []*pb.Endpoint
	endpointKeys   map[*pb.Endpoint]string
	scores         map[string]*int64
	activeRequests map[string]*int64
}

// EWMA implements Exponentially Weighted Moving Average load balancing
// Selects endpoints based on weighted average response times
type EWMA struct {
	mu        sync.Mutex // protects writes in UpdateEndpoints only
	endpoints []*pb.Endpoint
	// snap holds a *ewmaSnapshot; read lock-free via atomic.Value.
	snap atomic.Value

	// Decay factor for EWMA (0.0 to 1.0, typically 0.9)
	// Higher values give more weight to historical data
	decay float64
}

const (
	// Initial score for new endpoints (100ms)
	initialScore = 100 * 1000 * 1000 // 100ms in nanoseconds

	// Scaling factor for storing float scores as int64
	scorePrecision = 1000
)

// NewEWMA creates a new EWMA load balancer
func NewEWMA(endpoints []*pb.Endpoint) *EWMA {
	scores := make(map[string]*int64)
	activeRequests := make(map[string]*int64)

	for _, ep := range endpoints {
		if ep.Ready {
			key := endpointKey(ep)
			score := int64(initialScore * scorePrecision)
			scores[key] = &score

			var count int64
			activeRequests[key] = &count
		}
	}

	e := &EWMA{
		endpoints: endpoints,
		decay:     0.9, // 90% weight to historical data
	}
	e.storeSnapshot(endpoints, scores, activeRequests)
	return e
}

// storeSnapshot builds and atomically publishes a new snapshot.
func (e *EWMA) storeSnapshot(endpoints []*pb.Endpoint, scores map[string]*int64, activeRequests map[string]*int64) {
	healthy := make([]*pb.Endpoint, 0, len(endpoints))
	keys := make(map[*pb.Endpoint]string, len(endpoints))
	for _, ep := range endpoints {
		keys[ep] = endpointKey(ep)
		if ep.Ready {
			healthy = append(healthy, ep)
		}
	}
	e.snap.Store(&ewmaSnapshot{
		healthy:        healthy,
		endpointKeys:   keys,
		scores:         scores,
		activeRequests: activeRequests,
	})
}

// loadSnapshot returns the current snapshot, or nil if none has been stored.
func (e *EWMA) loadSnapshot() *ewmaSnapshot {
	v := e.snap.Load()
	if v == nil {
		return nil
	}
	s, ok := v.(*ewmaSnapshot)
	if !ok {
		return nil
	}
	return s
}

// Select chooses an endpoint with the lowest EWMA score.
// It reads from an atomic snapshot and never acquires a lock.
func (e *EWMA) Select() *pb.Endpoint {
	s := e.loadSnapshot()
	if s == nil || len(s.healthy) == 0 {
		return nil
	}

	if len(s.healthy) == 1 {
		return s.healthy[0]
	}

	// Select endpoint with lowest weighted score
	// Score = EWMA_latency * (1 + active_requests)
	var bestEndpoint *pb.Endpoint
	bestScore := int64(math.MaxInt64)

	for _, ep := range s.healthy {
		key := cachedKeyFromSnapshot(s.endpointKeys, ep)
		scorePtr, ok := s.scores[key]
		if !ok {
			continue
		}
		ewmaScore := atomic.LoadInt64(scorePtr)
		reqPtr, ok := s.activeRequests[key]
		if !ok {
			continue
		}
		activeCount := atomic.LoadInt64(reqPtr)

		// Penalize endpoints with many active requests
		weightedScore := ewmaScore * (1 + activeCount)

		if weightedScore < bestScore {
			bestScore = weightedScore
			bestEndpoint = ep
		}
	}

	return bestEndpoint
}

// UpdateEndpoints updates the endpoint list
func (e *EWMA) UpdateEndpoints(endpoints []*pb.Endpoint) {
	e.mu.Lock()
	defer e.mu.Unlock()

	oldSnap := e.loadSnapshot()
	var oldScores map[string]*int64
	var oldActiveRequests map[string]*int64
	if oldSnap != nil {
		oldScores = oldSnap.scores
		oldActiveRequests = oldSnap.activeRequests
	}

	e.endpoints = endpoints

	// Update maps for new endpoints
	newScores := make(map[string]*int64)
	newActiveRequests := make(map[string]*int64)

	for _, ep := range endpoints {
		if ep.Ready {
			key := endpointKey(ep)

			// Preserve existing scores
			if oldScores != nil {
				if existingScore, ok := oldScores[key]; ok {
					newScores[key] = existingScore
				} else {
					score := int64(initialScore * scorePrecision)
					newScores[key] = &score
				}
			} else {
				score := int64(initialScore * scorePrecision)
				newScores[key] = &score
			}

			// Preserve existing counters
			if oldActiveRequests != nil {
				if existingCount, ok := oldActiveRequests[key]; ok {
					newActiveRequests[key] = existingCount
				} else {
					var count int64
					newActiveRequests[key] = &count
				}
			} else {
				var count int64
				newActiveRequests[key] = &count
			}
		}
	}

	e.storeSnapshot(endpoints, newScores, newActiveRequests)
}

// RecordLatency updates the EWMA score for an endpoint based on observed latency
func (e *EWMA) RecordLatency(endpoint *pb.Endpoint, latency time.Duration) {
	if endpoint == nil {
		return
	}

	s := e.loadSnapshot()
	if s == nil {
		return
	}
	key := cachedKeyFromSnapshot(s.endpointKeys, endpoint)
	scorePtr, ok := s.scores[key]
	if !ok {
		return
	}

	// Convert latency to scaled int64
	newSample := latency.Nanoseconds() * scorePrecision

	// Calculate new EWMA: score = decay * old_score + (1 - decay) * new_sample
	for {
		oldScore := atomic.LoadInt64(scorePtr)
		newScore := int64(e.decay*float64(oldScore) + (1-e.decay)*float64(newSample))

		if atomic.CompareAndSwapInt64(scorePtr, oldScore, newScore) {
			break
		}
	}
}

// IncrementActive increments the active request count for an endpoint
func (e *EWMA) IncrementActive(endpoint *pb.Endpoint) {
	if endpoint == nil {
		return
	}
	s := e.loadSnapshot()
	if s == nil {
		return
	}
	key := cachedKeyFromSnapshot(s.endpointKeys, endpoint)
	if counter, ok := s.activeRequests[key]; ok {
		atomic.AddInt64(counter, 1)
	}
}

// DecrementActive decrements the active request count for an endpoint
func (e *EWMA) DecrementActive(endpoint *pb.Endpoint) {
	if endpoint == nil {
		return
	}
	s := e.loadSnapshot()
	if s == nil {
		return
	}
	key := cachedKeyFromSnapshot(s.endpointKeys, endpoint)
	if counter, ok := s.activeRequests[key]; ok {
		atomic.AddInt64(counter, -1)
	}
}

// GetScore returns the current EWMA score for an endpoint (in milliseconds)
func (e *EWMA) GetScore(endpoint *pb.Endpoint) float64 {
	if endpoint == nil {
		return 0
	}
	s := e.loadSnapshot()
	if s == nil {
		return 0
	}
	key := cachedKeyFromSnapshot(s.endpointKeys, endpoint)
	if scorePtr, ok := s.scores[key]; ok {
		score := atomic.LoadInt64(scorePtr)
		return float64(score) / scorePrecision / 1000000 // Convert to milliseconds
	}
	return 0
}
