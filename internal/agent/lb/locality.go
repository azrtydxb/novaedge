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

// RegionTopologyLabel is the well-known Kubernetes label used to identify
// the region of a node/endpoint.
const RegionTopologyLabel = "topology.kubernetes.io/region"

// NovaEdge-specific topology labels for federation cross-cluster endpoints.
const (
	NovaEdgeZoneLabel    = "novaedge.io/zone"
	NovaEdgeRegionLabel  = "novaedge.io/region"
	NovaEdgeClusterLabel = "novaedge.io/cluster"
)

// unknownZone is the zone assigned to endpoints that lack zone metadata.
const unknownZone = "unknown"

// unknownRegion is the region assigned to endpoints that lack region metadata.
const unknownRegion = "unknown"

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

	// LocalRegion is the region of this agent (e.g. "us-east-1").
	// When set together with LocalCluster, enables three-tier locality
	// selection: same-zone > same-region > all (cross-region).
	// When empty, the LB falls back to the original two-tier behaviour
	// (same-zone > all).
	LocalRegion string

	// LocalCluster is the cluster name of this agent (e.g. "prod-east").
	// Used together with LocalRegion for three-tier locality selection.
	// When empty, zone-level matching ignores cluster affinity.
	LocalCluster string

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
		LocalRegion:       "",
		LocalCluster:      "",
		MinHealthyPercent: DefaultMinHealthyPercent,
	}
}

// LocalityLB wraps an inner LoadBalancer with zone/locality-aware endpoint
// selection. When enabled, it groups endpoints by locality and preferentially
// routes to the most local tier that has enough healthy endpoints.
//
// Three-tier mode (when LocalRegion and LocalCluster are set):
//   - Tier 1: same zone AND same cluster
//   - Tier 2: same region (any zone, any cluster)
//   - Tier 3: all endpoints (cross-region)
//
// Two-tier mode (backward compatible, when LocalRegion/LocalCluster are empty):
//   - Tier 1: same zone
//   - Tier 2: all endpoints
//
// Each tier overflows to the next when the healthy endpoint ratio drops below
// MinHealthyPercent.
type LocalityLB struct {
	config LocalityConfig

	mu sync.RWMutex

	// allEndpoints holds every endpoint regardless of locality.
	allEndpoints []*pb.Endpoint
	// localEndpoints holds endpoints in the same zone (two-tier) or
	// same zone AND same cluster (three-tier).
	localEndpoints []*pb.Endpoint
	// regionEndpoints holds endpoints in the same region (three-tier only).
	regionEndpoints []*pb.Endpoint

	localLB  LoadBalancer // balancer over local-zone endpoints
	regionLB LoadBalancer // balancer over same-region endpoints (three-tier)
	allLB    LoadBalancer // balancer over all endpoints
}

// threeTierEnabled returns true when the config has both LocalRegion and
// LocalCluster set, enabling the three-tier locality hierarchy.
func (l *LocalityLB) threeTierEnabled() bool {
	return l.config.LocalRegion != "" && l.config.LocalCluster != ""
}

// NewLocalityLB creates a locality-aware wrapper around the given LoadBalancer.
// The provided inner LB is used as the "all zones" balancer. Additional
// balancers (RoundRobin) are created for local-zone and same-region tiers.
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
// the all-zones balancer. When enabled, it checks tiers in order of locality
// preference and selects from the most local tier with enough healthy endpoints.
func (l *LocalityLB) Select() *pb.Endpoint {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if !l.config.Enabled || l.config.LocalZone == "" {
		return l.allLB.Select()
	}

	// Tier 1: same zone (and same cluster in three-tier mode).
	if len(l.localEndpoints) > 0 {
		healthy := filterHealthy(l.localEndpoints)
		healthyRatio := float64(len(healthy)) / float64(len(l.localEndpoints))
		if healthyRatio >= l.config.MinHealthyPercent && len(healthy) > 0 {
			return l.localLB.Select()
		}
	}

	// Tier 2: same region (three-tier mode only).
	if l.threeTierEnabled() && len(l.regionEndpoints) > 0 {
		healthy := filterHealthy(l.regionEndpoints)
		healthyRatio := float64(len(healthy)) / float64(len(l.regionEndpoints))
		if healthyRatio >= l.config.MinHealthyPercent && len(healthy) > 0 {
			return l.regionLB.Select()
		}
	}

	// Tier 3: all endpoints (cross-region fallback).
	return l.allLB.Select()
}

// UpdateEndpoints replaces the full endpoint set and regroups by locality.
func (l *LocalityLB) UpdateEndpoints(endpoints []*pb.Endpoint) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.rebuildState(endpoints)
}

// rebuildState regroups endpoints by locality and updates all inner balancers.
// Must be called with l.mu held for writing.
func (l *LocalityLB) rebuildState(endpoints []*pb.Endpoint) {
	l.allEndpoints = endpoints
	l.allLB.UpdateEndpoints(endpoints)

	if l.config.Enabled && l.config.LocalZone != "" {
		if l.threeTierEnabled() {
			l.localEndpoints = filterLocalZoneCluster(endpoints, l.config.LocalZone, l.config.LocalCluster)
			l.regionEndpoints = filterSameRegion(endpoints, l.config.LocalRegion)
		} else {
			// Two-tier backward-compatible: group by zone only.
			zones := groupByZone(endpoints)
			l.localEndpoints = zones[l.config.LocalZone]
			l.regionEndpoints = nil
		}
	} else {
		l.localEndpoints = nil
		l.regionEndpoints = nil
	}

	// Update or create local-zone balancer.
	if l.localLB == nil {
		l.localLB = NewRoundRobin(l.localEndpoints)
	} else {
		l.localLB.UpdateEndpoints(l.localEndpoints)
	}

	// Update or create same-region balancer.
	if l.regionLB == nil {
		l.regionLB = NewRoundRobin(l.regionEndpoints)
	} else {
		l.regionLB.UpdateEndpoints(l.regionEndpoints)
	}
}

// EndpointZone extracts the availability zone from an endpoint's labels.
// It checks the NovaEdge label first, then the standard Kubernetes topology
// label. Returns "unknown" when the endpoint has no zone metadata.
func EndpointZone(endpoint *pb.Endpoint) string {
	if endpoint == nil {
		return unknownZone
	}
	labels := endpoint.GetLabels()
	if zone, ok := labels[NovaEdgeZoneLabel]; ok && zone != "" {
		return zone
	}
	if zone, ok := labels[ZoneTopologyLabel]; ok && zone != "" {
		return zone
	}
	return unknownZone
}

// EndpointRegion extracts the region from an endpoint's labels.
// It checks the NovaEdge label first, then the standard Kubernetes topology
// label. Returns "unknown" when the endpoint has no region metadata.
func EndpointRegion(endpoint *pb.Endpoint) string {
	if endpoint == nil {
		return unknownRegion
	}
	labels := endpoint.GetLabels()
	if region, ok := labels[NovaEdgeRegionLabel]; ok && region != "" {
		return region
	}
	if region, ok := labels[RegionTopologyLabel]; ok && region != "" {
		return region
	}
	return unknownRegion
}

// EndpointCluster extracts the cluster name from an endpoint's labels.
// Returns empty string when the endpoint has no cluster metadata.
func EndpointCluster(endpoint *pb.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	if cluster, ok := endpoint.GetLabels()[NovaEdgeClusterLabel]; ok {
		return cluster
	}
	return ""
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

// filterLocalZoneCluster returns endpoints that match both the given zone
// and cluster. Used in three-tier mode for tier-1 selection.
func filterLocalZoneCluster(endpoints []*pb.Endpoint, zone, cluster string) []*pb.Endpoint {
	var result []*pb.Endpoint
	for _, ep := range endpoints {
		epZone := EndpointZone(ep)
		epCluster := EndpointCluster(ep)
		if epZone == zone && epCluster == cluster {
			result = append(result, ep)
		}
	}
	return result
}

// filterSameRegion returns endpoints that match the given region.
// Used in three-tier mode for tier-2 selection.
func filterSameRegion(endpoints []*pb.Endpoint, region string) []*pb.Endpoint {
	var result []*pb.Endpoint
	for _, ep := range endpoints {
		if EndpointRegion(ep) == region {
			result = append(result, ep)
		}
	}
	return result
}
