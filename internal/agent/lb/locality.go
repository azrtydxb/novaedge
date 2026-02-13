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
	"sync"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// ZoneTopologyLabel is the well-known Kubernetes label used to identify
// the availability zone of a node/endpoint.
const ZoneTopologyLabel = "topology.kubernetes.io/zone"

// unknownZone is the zone assigned to endpoints that lack zone metadata.
const unknownZone = "unknown"

// DefaultMinHealthyPercent is the default threshold for preferring local-zone
// endpoints. If at least this fraction of local-zone endpoints are healthy,
// traffic stays within the local zone.
const DefaultMinHealthyPercent = 0.7

// LocalityConfig controls zone/locality-aware load balancing behaviour.
type LocalityConfig struct {
	// Enabled turns locality-aware routing on or off.
	Enabled bool

	// LocalZone is the availability zone of this agent (e.g. "us-east-1a").
	LocalZone string

	// MinHealthyPercent is the minimum fraction (0.0-1.0) of local-zone
	// endpoints that must be healthy before the balancer restricts traffic to
	// the local zone. When the healthy ratio drops below this threshold, all
	// zones are used. Defaults to 0.7 (70%).
	MinHealthyPercent float64
}

// DefaultLocalityConfig returns a LocalityConfig with sensible defaults.
func DefaultLocalityConfig() LocalityConfig {
	return LocalityConfig{
		Enabled:           false,
		LocalZone:         "",
		MinHealthyPercent: DefaultMinHealthyPercent,
	}
}

// LocalityLB wraps an inner LoadBalancer with zone/locality-aware endpoint
// selection. When enabled, it groups endpoints by zone and preferentially
// routes to local-zone endpoints as long as a sufficient fraction of them
// are healthy. If the local zone is degraded it falls back to all zones.
//
// Internally it maintains two round-robin balancers: one over local-zone
// endpoints and one over all endpoints. Select reads current health and
// delegates to the appropriate balancer without mutating shared state on
// the hot path.
type LocalityLB struct {
	config LocalityConfig

	mu             sync.RWMutex
	allEndpoints   []*pb.Endpoint
	localEndpoints []*pb.Endpoint
	localLB        LoadBalancer // balancer over local-zone endpoints
	allLB          LoadBalancer // balancer over all endpoints
}

// NewLocalityLB creates a locality-aware wrapper around the given LoadBalancer.
// The provided inner LB is used as the "all zones" balancer. A second balancer
// of the same type (RoundRobin) is created for local-zone endpoints.
func NewLocalityLB(inner LoadBalancer, config LocalityConfig, endpoints []*pb.Endpoint) *LocalityLB {
	llb := &LocalityLB{
		config: config,
		allLB:  inner,
	}
	llb.rebuildState(endpoints)
	return llb
}

// Select picks an endpoint respecting locality configuration.
//
// When locality is disabled or LocalZone is empty, it delegates directly to
// the all-zones balancer. When enabled, it checks whether the local zone has
// enough healthy endpoints; if so, it selects from the local-zone balancer,
// otherwise it selects from the all-zones balancer.
func (l *LocalityLB) Select() *pb.Endpoint {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if !l.config.Enabled || l.config.LocalZone == "" {
		return l.allLB.Select()
	}

	if len(l.localEndpoints) == 0 {
		return l.allLB.Select()
	}

	healthy := filterHealthy(l.localEndpoints)
	healthyRatio := float64(len(healthy)) / float64(len(l.localEndpoints))

	if healthyRatio >= l.config.MinHealthyPercent && len(healthy) > 0 {
		return l.localLB.Select()
	}

	return l.allLB.Select()
}

// UpdateEndpoints replaces the full endpoint set and regroups by zone.
func (l *LocalityLB) UpdateEndpoints(endpoints []*pb.Endpoint) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.rebuildState(endpoints)
}

// rebuildState regroups endpoints by zone and updates both inner balancers.
// Must be called with l.mu held for writing.
func (l *LocalityLB) rebuildState(endpoints []*pb.Endpoint) {
	l.allEndpoints = endpoints
	l.allLB.UpdateEndpoints(endpoints)

	if l.config.Enabled && l.config.LocalZone != "" {
		zones := groupByZone(endpoints)
		l.localEndpoints = zones[l.config.LocalZone]
	} else {
		l.localEndpoints = nil
	}

	if l.localLB == nil {
		l.localLB = NewRoundRobin(l.localEndpoints)
	} else {
		l.localLB.UpdateEndpoints(l.localEndpoints)
	}
}

// EndpointZone extracts the availability zone from an endpoint's labels.
// Returns "unknown" when the endpoint has no zone metadata.
func EndpointZone(endpoint *pb.Endpoint) string {
	if endpoint == nil {
		return unknownZone
	}
	if zone, ok := endpoint.GetLabels()[ZoneTopologyLabel]; ok && zone != "" {
		return zone
	}
	return unknownZone
}

// groupByZone partitions endpoints into a map keyed by their zone label.
func groupByZone(endpoints []*pb.Endpoint) map[string][]*pb.Endpoint {
	zones := make(map[string][]*pb.Endpoint)
	for _, ep := range endpoints {
		zone := EndpointZone(ep)
		zones[zone] = append(zones[zone], ep)
	}
	return zones
}
