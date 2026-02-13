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
	"sort"
	"strconv"
	"sync"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// PriorityLabelKey is the endpoint label key used to read the priority level.
// Lower values indicate higher priority (0 = highest).
const PriorityLabelKey = "lb.priority"

// DefaultOverflowThreshold is the default healthy percentage threshold below
// which traffic overflows to the next priority group.
const DefaultOverflowThreshold = 0.7

// PriorityConfig holds configuration for priority-based load balancing.
type PriorityConfig struct {
	// OverflowThreshold is the minimum healthy endpoint ratio for a priority
	// group to handle traffic exclusively. When the healthy ratio drops below
	// this value, the next priority group is included. Must be between 0 and 1.
	// Default: 0.7 (70%).
	OverflowThreshold float64
}

// DefaultPriorityConfig returns a PriorityConfig with sensible defaults.
func DefaultPriorityConfig() PriorityConfig {
	return PriorityConfig{
		OverflowThreshold: DefaultOverflowThreshold,
	}
}

// priorityGroup holds all endpoints (both healthy and unhealthy) at a given
// priority level and caches the healthy subset.
type priorityGroup struct {
	priority int
	all      []*pb.Endpoint
	healthy  []*pb.Endpoint
}

// PriorityLB wraps an inner LoadBalancer and groups endpoints by priority level.
// Traffic is sent to the highest-priority (lowest number) group as long as
// a sufficient percentage of that group's endpoints are healthy. When the
// healthy ratio drops below the configured OverflowThreshold, endpoints from
// the next priority group are added to the selection pool.
type PriorityLB struct {
	mu     sync.RWMutex
	inner  LoadBalancer
	config PriorityConfig
	groups []priorityGroup
}

// NewPriorityLB creates a new priority-based load balancer that wraps inner.
func NewPriorityLB(inner LoadBalancer, config PriorityConfig, endpoints []*pb.Endpoint) *PriorityLB {
	p := &PriorityLB{
		inner:  inner,
		config: config,
	}
	p.groups = buildPriorityGroups(endpoints)
	p.syncInner()
	return p
}

// Select picks an endpoint from the eligible priority groups and delegates
// the final selection to the inner LoadBalancer.
func (p *PriorityLB) Select() *pb.Endpoint {
	return p.inner.Select()
}

// UpdateEndpoints regroups the endpoints by priority and updates the inner LB.
func (p *PriorityLB) UpdateEndpoints(endpoints []*pb.Endpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.groups = buildPriorityGroups(endpoints)
	p.syncInnerLocked()
}

// GetInner returns the underlying load balancer.
func (p *PriorityLB) GetInner() LoadBalancer {
	return p.inner
}

// EligibleEndpoints returns the set of healthy endpoints from eligible priority
// groups based on the overflow threshold. This is useful for observability.
func (p *PriorityLB) EligibleEndpoints() []*pb.Endpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.computeEligible()
}

// syncInner updates the inner LB with the current eligible endpoints.
// Acquires the write lock internally.
func (p *PriorityLB) syncInner() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.syncInnerLocked()
}

// syncInnerLocked updates the inner LB. Must be called with p.mu held.
func (p *PriorityLB) syncInnerLocked() {
	eligible := p.computeEligible()
	p.inner.UpdateEndpoints(eligible)
}

// computeEligible walks through priority groups in order (lowest number first)
// and returns the healthy endpoints that should receive traffic.
//
// For each group starting from the highest priority:
//  1. If the group has no endpoints at all, skip it.
//  2. Compute healthyRatio = len(healthy) / len(all).
//  3. Add the group's healthy endpoints to the eligible pool.
//  4. If healthyRatio >= threshold, stop (this group can handle traffic).
//  5. Otherwise, continue to the next group (overflow).
//
// Must be called with p.mu held (at least read lock).
func (p *PriorityLB) computeEligible() []*pb.Endpoint {
	if len(p.groups) == 0 {
		return nil
	}

	var eligible []*pb.Endpoint

	for _, g := range p.groups {
		if len(g.all) == 0 {
			continue
		}

		eligible = append(eligible, g.healthy...)

		healthyRatio := float64(len(g.healthy)) / float64(len(g.all))
		if healthyRatio >= p.config.OverflowThreshold {
			break
		}
		// Healthy ratio below threshold; overflow to next priority group.
	}

	return eligible
}

// buildPriorityGroups groups endpoints by their priority label, sorts by
// priority (ascending), and caches the healthy subset for each group.
func buildPriorityGroups(endpoints []*pb.Endpoint) []priorityGroup {
	groupMap := make(map[int]*priorityGroup)

	for _, ep := range endpoints {
		pri := parsePriority(ep)

		g, ok := groupMap[pri]
		if !ok {
			g = &priorityGroup{priority: pri}
			groupMap[pri] = g
		}
		g.all = append(g.all, ep)
		if ep.Ready {
			g.healthy = append(g.healthy, ep)
		}
	}

	groups := make([]priorityGroup, 0, len(groupMap))
	for _, g := range groupMap {
		groups = append(groups, *g)
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].priority < groups[j].priority
	})

	return groups
}

// parsePriority reads the priority level from an endpoint's labels.
// Returns 0 (highest priority) if the label is absent or unparseable.
func parsePriority(ep *pb.Endpoint) int {
	if ep.Labels == nil {
		return 0
	}
	val, ok := ep.Labels[PriorityLabelKey]
	if !ok {
		return 0
	}
	pri, err := strconv.Atoi(val)
	if err != nil || pri < 0 {
		return 0
	}
	return pri
}
