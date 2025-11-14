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
	"sync/atomic"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// LoadBalancer selects backend endpoints
type LoadBalancer interface {
	Select() *pb.Endpoint
	UpdateEndpoints(endpoints []*pb.Endpoint)
}

// RoundRobin implements round-robin load balancing
type RoundRobin struct {
	endpoints []*pb.Endpoint
	counter   uint64
}

// NewRoundRobin creates a new round-robin load balancer
func NewRoundRobin(endpoints []*pb.Endpoint) *RoundRobin {
	return &RoundRobin{
		endpoints: filterHealthy(endpoints),
		counter:   0,
	}
}

// Select selects the next endpoint using round-robin
func (rr *RoundRobin) Select() *pb.Endpoint {
	if len(rr.endpoints) == 0 {
		return nil
	}

	// Atomically increment and get current value
	current := atomic.AddUint64(&rr.counter, 1) - 1
	index := int(current % uint64(len(rr.endpoints)))

	return rr.endpoints[index]
}

// UpdateEndpoints updates the list of endpoints
func (rr *RoundRobin) UpdateEndpoints(endpoints []*pb.Endpoint) {
	rr.endpoints = filterHealthy(endpoints)
	// Reset counter when endpoints change
	atomic.StoreUint64(&rr.counter, 0)
}

// filterHealthy filters endpoints to only include ready ones
func filterHealthy(endpoints []*pb.Endpoint) []*pb.Endpoint {
	var healthy []*pb.Endpoint
	for _, ep := range endpoints {
		if ep.Ready {
			healthy = append(healthy, ep)
		}
	}
	return healthy
}
