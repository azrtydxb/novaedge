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

// EWMA implements Exponentially Weighted Moving Average load balancing
// Selects endpoints based on weighted average response times
type EWMA struct {
	mu        sync.RWMutex
	endpoints []*pb.Endpoint

	// EWMA scores per endpoint (lower is better)
	scores map[string]*int64 // Stored as nanoseconds * 1000 for precision

	// Active requests per endpoint
	activeRequests map[string]*int64

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

	return &EWMA{
		endpoints:      endpoints,
		scores:         scores,
		activeRequests: activeRequests,
		decay:          0.9, // 90% weight to historical data
	}
}

// Select chooses an endpoint with the lowest EWMA score
func (e *EWMA) Select() *pb.Endpoint {
	e.mu.RLock()
	defer e.mu.RUnlock()

	healthy := e.getHealthyEndpoints()
	if len(healthy) == 0 {
		return nil
	}

	if len(healthy) == 1 {
		return healthy[0]
	}

	// Select endpoint with lowest weighted score
	// Score = EWMA_latency * (1 + active_requests)
	var bestEndpoint *pb.Endpoint
	bestScore := int64(math.MaxInt64)

	for _, ep := range healthy {
		key := endpointKey(ep)
		ewmaScore := atomic.LoadInt64(e.scores[key])
		activeCount := atomic.LoadInt64(e.activeRequests[key])

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

	e.endpoints = endpoints

	// Update maps for new endpoints
	newScores := make(map[string]*int64)
	newActiveRequests := make(map[string]*int64)

	for _, ep := range endpoints {
		if ep.Ready {
			key := endpointKey(ep)

			// Preserve existing scores and counters
			if existingScore, ok := e.scores[key]; ok {
				newScores[key] = existingScore
			} else {
				score := int64(initialScore * scorePrecision)
				newScores[key] = &score
			}

			if existingCount, ok := e.activeRequests[key]; ok {
				newActiveRequests[key] = existingCount
			} else {
				var count int64
				newActiveRequests[key] = &count
			}
		}
	}

	e.scores = newScores
	e.activeRequests = newActiveRequests
}

// RecordLatency updates the EWMA score for an endpoint based on observed latency
func (e *EWMA) RecordLatency(endpoint *pb.Endpoint, latency time.Duration) {
	if endpoint == nil {
		return
	}

	key := endpointKey(endpoint)
	e.mu.RLock()
	scorePtr := e.scores[key]
	e.mu.RUnlock()

	if scorePtr == nil {
		return
	}

	// Convert latency to scaled int64
	newSample := int64(latency.Nanoseconds() * scorePrecision)

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
	key := endpointKey(endpoint)
	e.mu.RLock()
	counter := e.activeRequests[key]
	e.mu.RUnlock()
	if counter != nil {
		atomic.AddInt64(counter, 1)
	}
}

// DecrementActive decrements the active request count for an endpoint
func (e *EWMA) DecrementActive(endpoint *pb.Endpoint) {
	if endpoint == nil {
		return
	}
	key := endpointKey(endpoint)
	e.mu.RLock()
	counter := e.activeRequests[key]
	e.mu.RUnlock()
	if counter != nil {
		atomic.AddInt64(counter, -1)
	}
}

// GetScore returns the current EWMA score for an endpoint (in milliseconds)
func (e *EWMA) GetScore(endpoint *pb.Endpoint) float64 {
	if endpoint == nil {
		return 0
	}
	key := endpointKey(endpoint)
	e.mu.RLock()
	scorePtr := e.scores[key]
	e.mu.RUnlock()
	if scorePtr != nil {
		score := atomic.LoadInt64(scorePtr)
		return float64(score) / scorePrecision / 1000000 // Convert to milliseconds
	}
	return 0
}

func (e *EWMA) getHealthyEndpoints() []*pb.Endpoint {
	var healthy []*pb.Endpoint
	for _, ep := range e.endpoints {
		if ep.Ready {
			healthy = append(healthy, ep)
		}
	}
	return healthy
}
