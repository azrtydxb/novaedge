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
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	testAddr1 = "10.0.0.1"
	testAddr2 = "10.0.0.2"
	testAddr3 = "10.0.0.3"
	testAddr4 = "10.0.0.4"
)

// makeEndpoint creates a test endpoint with the given address, ready state,
// and labels.
func makeEndpoint(addr string, ready bool, labels map[string]string) *pb.Endpoint {
	return &pb.Endpoint{
		Address: addr,
		Port:    8080,
		Ready:   ready,
		Labels:  labels,
	}
}

// --- Helper label builders ---

func zoneLabels(zone string) map[string]string {
	return map[string]string{
		ZoneTopologyLabel: zone,
	}
}

func fullLabels(zone, region, cluster string) map[string]string {
	return map[string]string{
		NovaEdgeZoneLabel:    zone,
		NovaEdgeRegionLabel:  region,
		NovaEdgeClusterLabel: cluster,
	}
}

// --- EndpointZone / EndpointRegion / EndpointCluster tests ---

func TestEndpointZone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		endpoint *pb.Endpoint
		want     string
	}{
		{
			name:     "nil endpoint",
			endpoint: nil,
			want:     unknownZone,
		},
		{
			name:     "no labels",
			endpoint: makeEndpoint("1.2.3.4", true, nil),
			want:     unknownZone,
		},
		{
			name:     "kubernetes zone label",
			endpoint: makeEndpoint("1.2.3.4", true, zoneLabels("us-east-1a")),
			want:     "us-east-1a",
		},
		{
			name:     "novaedge zone label takes precedence",
			endpoint: makeEndpoint("1.2.3.4", true, map[string]string{ZoneTopologyLabel: "k8s-zone", NovaEdgeZoneLabel: "nova-zone"}),
			want:     "nova-zone",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EndpointZone(tt.endpoint)
			if got != tt.want {
				t.Errorf("EndpointZone() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEndpointRegion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		endpoint *pb.Endpoint
		want     string
	}{
		{
			name:     "nil endpoint",
			endpoint: nil,
			want:     unknownRegion,
		},
		{
			name:     "no labels",
			endpoint: makeEndpoint("1.2.3.4", true, nil),
			want:     unknownRegion,
		},
		{
			name:     "kubernetes region label",
			endpoint: makeEndpoint("1.2.3.4", true, map[string]string{RegionTopologyLabel: "us-east-1"}),
			want:     "us-east-1",
		},
		{
			name:     "novaedge region label takes precedence",
			endpoint: makeEndpoint("1.2.3.4", true, map[string]string{RegionTopologyLabel: "k8s-region", NovaEdgeRegionLabel: "nova-region"}),
			want:     "nova-region",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EndpointRegion(tt.endpoint)
			if got != tt.want {
				t.Errorf("EndpointRegion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEndpointCluster(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		endpoint *pb.Endpoint
		want     string
	}{
		{
			name:     "nil endpoint",
			endpoint: nil,
			want:     "",
		},
		{
			name:     "no labels",
			endpoint: makeEndpoint("1.2.3.4", true, nil),
			want:     "",
		},
		{
			name:     "cluster label set",
			endpoint: makeEndpoint("1.2.3.4", true, map[string]string{NovaEdgeClusterLabel: "prod-east"}),
			want:     "prod-east",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EndpointCluster(tt.endpoint)
			if got != tt.want {
				t.Errorf("EndpointCluster() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- Two-tier (backward compatible) tests ---

func TestLocalityLB_TwoTier_AllLocalHealthy(t *testing.T) {
	t.Parallel()

	localEp := makeEndpoint(testAddr1, true, zoneLabels("us-east-1a"))
	remoteEp := makeEndpoint(testAddr2, true, zoneLabels("us-west-2a"))

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		MinHealthyPercent: 0.7,
	}

	endpoints := []*pb.Endpoint{localEp, remoteEp}
	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// All selections should come from the local zone.
	for i := 0; i < 10; i++ {
		ep := llb.Select()
		if ep == nil {
			t.Fatal("Select() returned nil")
		}
		if ep.Address != testAddr1 {
			t.Errorf("expected local endpoint 10.0.0.1, got %s", ep.Address)
		}
	}
}

func TestLocalityLB_TwoTier_LocalUnhealthy_FallsBackToAll(t *testing.T) {
	t.Parallel()

	// Only 1 of 3 local endpoints is healthy (33% < 70% threshold).
	localHealthy := makeEndpoint(testAddr1, true, zoneLabels("us-east-1a"))
	localUnhealthy1 := makeEndpoint(testAddr2, false, zoneLabels("us-east-1a"))
	localUnhealthy2 := makeEndpoint(testAddr3, false, zoneLabels("us-east-1a"))
	remoteEp := makeEndpoint(testAddr4, true, zoneLabels("us-west-2a"))

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		MinHealthyPercent: 0.7,
	}

	endpoints := []*pb.Endpoint{localHealthy, localUnhealthy1, localUnhealthy2, remoteEp}
	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// Should fall back to all endpoints (round-robin over healthy ones).
	sawRemote := false
	for i := 0; i < 20; i++ {
		ep := llb.Select()
		if ep != nil && ep.Address == testAddr4 {
			sawRemote = true
			break
		}
	}
	if !sawRemote {
		t.Error("expected to see remote endpoint after local zone degraded")
	}
}

func TestLocalityLB_Disabled_UsesAllEndpoints(t *testing.T) {
	t.Parallel()

	localEp := makeEndpoint(testAddr1, true, zoneLabels("us-east-1a"))
	remoteEp := makeEndpoint(testAddr2, true, zoneLabels("us-west-2a"))

	config := LocalityConfig{
		Enabled:           false,
		LocalZone:         "us-east-1a",
		MinHealthyPercent: 0.7,
	}

	endpoints := []*pb.Endpoint{localEp, remoteEp}
	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// Should round-robin across all endpoints.
	addrs := map[string]bool{}
	for i := 0; i < 20; i++ {
		ep := llb.Select()
		if ep != nil {
			addrs[ep.Address] = true
		}
	}
	if len(addrs) < 2 {
		t.Errorf("expected to see both endpoints when disabled, saw %v", addrs)
	}
}

// --- Three-tier tests ---

func TestLocalityLB_ThreeTier_AllLocalHealthy_StaysInZone(t *testing.T) {
	t.Parallel()

	localEp := makeEndpoint(testAddr1, true, fullLabels("us-east-1a", "us-east", "cluster-a"))
	regionEp := makeEndpoint(testAddr2, true, fullLabels("us-east-1b", "us-east", "cluster-b"))
	remoteEp := makeEndpoint(testAddr3, true, fullLabels("eu-west-1a", "eu-west", "cluster-c"))

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		LocalRegion:       "us-east",
		LocalCluster:      "cluster-a",
		MinHealthyPercent: 0.7,
	}

	endpoints := []*pb.Endpoint{localEp, regionEp, remoteEp}
	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	for i := 0; i < 10; i++ {
		ep := llb.Select()
		if ep == nil {
			t.Fatal("Select() returned nil")
		}
		if ep.Address != testAddr1 {
			t.Errorf("expected local zone endpoint 10.0.0.1, got %s", ep.Address)
		}
	}
}

func TestLocalityLB_ThreeTier_LocalUnhealthy_OverflowsToRegion(t *testing.T) {
	t.Parallel()

	// Local zone (tier 1): 0 of 1 healthy (0% < 70%) -> overflows.
	localUnhealthy := makeEndpoint(testAddr1, false, fullLabels("us-east-1a", "us-east", "cluster-a"))
	// Same region (tier 2): 4 healthy endpoints out of 5 total (80% >= 70%).
	// The unhealthy local endpoint is also in the region set, giving 4/5 = 80%.
	regionEp1 := makeEndpoint(testAddr3, true, fullLabels("us-east-1b", "us-east", "cluster-b"))
	regionEp2 := makeEndpoint(testAddr4, true, fullLabels("us-east-1c", "us-east", "cluster-a"))
	regionEp3 := makeEndpoint("10.0.0.6", true, fullLabels("us-east-1b", "us-east", "cluster-a"))
	regionEp4 := makeEndpoint("10.0.0.7", true, fullLabels("us-east-1c", "us-east", "cluster-b"))
	// Different region.
	remoteEp := makeEndpoint("10.0.0.5", true, fullLabels("eu-west-1a", "eu-west", "cluster-c"))

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		LocalRegion:       "us-east",
		LocalCluster:      "cluster-a",
		MinHealthyPercent: 0.7,
	}

	endpoints := []*pb.Endpoint{localUnhealthy, regionEp1, regionEp2, regionEp3, regionEp4, remoteEp}
	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// Should select from same-region endpoints, never cross-region.
	sawRegion := false
	sawRemote := false
	for i := 0; i < 20; i++ {
		ep := llb.Select()
		if ep == nil {
			continue
		}
		switch ep.Address {
		case "10.0.0.3", "10.0.0.4", "10.0.0.6", "10.0.0.7":
			sawRegion = true
		case "10.0.0.5":
			sawRemote = true
		}
	}
	if !sawRegion {
		t.Error("expected to see same-region endpoints")
	}
	if sawRemote {
		t.Error("should not see cross-region endpoints when same-region is healthy")
	}
}

func TestLocalityLB_ThreeTier_RegionUnhealthy_OverflowsToCrossRegion(t *testing.T) {
	t.Parallel()

	// Local zone: all unhealthy.
	localUnhealthy := makeEndpoint(testAddr1, false, fullLabels("us-east-1a", "us-east", "cluster-a"))
	// Same region: also unhealthy (0 of 2 healthy = 0% < 70%).
	regionUnhealthy1 := makeEndpoint(testAddr2, false, fullLabels("us-east-1b", "us-east", "cluster-b"))
	regionUnhealthy2 := makeEndpoint(testAddr3, false, fullLabels("us-east-1c", "us-east", "cluster-a"))
	// Cross-region: healthy.
	remoteEp := makeEndpoint(testAddr4, true, fullLabels("eu-west-1a", "eu-west", "cluster-c"))

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		LocalRegion:       "us-east",
		LocalCluster:      "cluster-a",
		MinHealthyPercent: 0.7,
	}

	endpoints := []*pb.Endpoint{localUnhealthy, regionUnhealthy1, regionUnhealthy2, remoteEp}
	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// Should fall through to all endpoints.
	sawRemote := false
	for i := 0; i < 10; i++ {
		ep := llb.Select()
		if ep != nil && ep.Address == testAddr4 {
			sawRemote = true
			break
		}
	}
	if !sawRemote {
		t.Error("expected cross-region endpoint when both local and region are degraded")
	}
}

func TestLocalityLB_ThreeTier_BackwardCompatible_NoRegionCluster(t *testing.T) {
	t.Parallel()

	// When LocalRegion and LocalCluster are empty, three-tier is disabled.
	// Zone-only matching should work (original behavior).
	localEp := makeEndpoint(testAddr1, true, fullLabels("us-east-1a", "us-east", "cluster-a"))
	// Same zone but different cluster - should still be local in two-tier mode.
	localEp2 := makeEndpoint(testAddr2, true, fullLabels("us-east-1a", "us-east", "cluster-b"))
	remoteEp := makeEndpoint(testAddr3, true, fullLabels("eu-west-1a", "eu-west", "cluster-c"))

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		LocalRegion:       "", // not set
		LocalCluster:      "", // not set
		MinHealthyPercent: 0.7,
	}

	endpoints := []*pb.Endpoint{localEp, localEp2, remoteEp}
	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// In two-tier mode, both same-zone endpoints count as local.
	sawLocal := map[string]bool{}
	for i := 0; i < 20; i++ {
		ep := llb.Select()
		if ep != nil {
			sawLocal[ep.Address] = true
		}
	}
	if sawLocal[testAddr3] {
		t.Error("should not see remote endpoint when local zone is healthy in two-tier mode")
	}
	if !sawLocal[testAddr1] || !sawLocal[testAddr2] {
		t.Error("should see both same-zone endpoints in two-tier mode")
	}
}

func TestLocalityLB_ThreeTier_MixedClustersInSameZone(t *testing.T) {
	t.Parallel()

	// In three-tier mode, only same zone AND same cluster counts as tier-1.
	localSameCluster := makeEndpoint(testAddr1, true, fullLabels("us-east-1a", "us-east", "cluster-a"))
	localDiffCluster := makeEndpoint(testAddr2, true, fullLabels("us-east-1a", "us-east", "cluster-b"))
	remoteEp := makeEndpoint(testAddr3, true, fullLabels("eu-west-1a", "eu-west", "cluster-c"))

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		LocalRegion:       "us-east",
		LocalCluster:      "cluster-a",
		MinHealthyPercent: 0.7,
	}

	endpoints := []*pb.Endpoint{localSameCluster, localDiffCluster, remoteEp}
	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// Tier 1 has only cluster-a endpoint. Should stick there.
	for i := 0; i < 10; i++ {
		ep := llb.Select()
		if ep == nil {
			t.Fatal("Select() returned nil")
		}
		if ep.Address != testAddr1 {
			t.Errorf("expected local cluster endpoint 10.0.0.1, got %s", ep.Address)
		}
	}
}

func TestLocalityLB_UpdateEndpoints_RebuildsState(t *testing.T) {
	t.Parallel()

	localEp := makeEndpoint(testAddr1, true, fullLabels("us-east-1a", "us-east", "cluster-a"))
	remoteEp := makeEndpoint(testAddr2, true, fullLabels("eu-west-1a", "eu-west", "cluster-c"))

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		LocalRegion:       "us-east",
		LocalCluster:      "cluster-a",
		MinHealthyPercent: 0.7,
	}

	endpoints := []*pb.Endpoint{localEp, remoteEp}
	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// Initially selects local.
	ep := llb.Select()
	if ep == nil || ep.Address != testAddr1 {
		t.Fatalf("expected local endpoint, got %v", ep)
	}

	// Update to only have remote endpoints.
	newEndpoints := []*pb.Endpoint{remoteEp}
	llb.UpdateEndpoints(newEndpoints)

	// Now should use all (only remote).
	ep = llb.Select()
	if ep == nil || ep.Address != testAddr2 {
		t.Fatalf("expected remote endpoint after update, got %v", ep)
	}
}

func TestLocalityLB_EmptyEndpoints(t *testing.T) {
	t.Parallel()

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		LocalRegion:       "us-east",
		LocalCluster:      "cluster-a",
		MinHealthyPercent: 0.7,
	}

	inner := NewRoundRobin(nil)
	llb := NewLocalityLB(inner, config, nil)

	ep := llb.Select()
	if ep != nil {
		t.Errorf("expected nil from empty endpoint set, got %v", ep)
	}
}

func TestDefaultLocalityConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultLocalityConfig()
	if cfg.Enabled {
		t.Error("expected Enabled=false")
	}
	if cfg.LocalZone != "" {
		t.Errorf("expected empty LocalZone, got %q", cfg.LocalZone)
	}
	if cfg.LocalRegion != "" {
		t.Errorf("expected empty LocalRegion, got %q", cfg.LocalRegion)
	}
	if cfg.LocalCluster != "" {
		t.Errorf("expected empty LocalCluster, got %q", cfg.LocalCluster)
	}
	if cfg.MinHealthyPercent != DefaultMinHealthyPercent {
		t.Errorf("expected MinHealthyPercent=%f, got %f", DefaultMinHealthyPercent, cfg.MinHealthyPercent)
	}
}

// --- Filter function tests ---

func TestFilterLocalZoneCluster(t *testing.T) {
	t.Parallel()

	eps := []*pb.Endpoint{
		makeEndpoint(testAddr1, true, fullLabels("us-east-1a", "us-east", "cluster-a")),
		makeEndpoint(testAddr2, true, fullLabels("us-east-1a", "us-east", "cluster-b")),
		makeEndpoint(testAddr3, true, fullLabels("us-east-1b", "us-east", "cluster-a")),
		makeEndpoint(testAddr4, true, fullLabels("eu-west-1a", "eu-west", "cluster-c")),
	}

	result := filterLocalZoneCluster(eps, "us-east-1a", "cluster-a")
	if len(result) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(result))
	}
	if result[0].Address != testAddr1 {
		t.Errorf("expected 10.0.0.1, got %s", result[0].Address)
	}
}

func TestFilterSameRegion(t *testing.T) {
	t.Parallel()

	eps := []*pb.Endpoint{
		makeEndpoint(testAddr1, true, fullLabels("us-east-1a", "us-east", "cluster-a")),
		makeEndpoint(testAddr2, true, fullLabels("us-east-1b", "us-east", "cluster-b")),
		makeEndpoint(testAddr3, true, fullLabels("eu-west-1a", "eu-west", "cluster-c")),
	}

	result := filterSameRegion(eps, "us-east")
	if len(result) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(result))
	}
	addrs := map[string]bool{}
	for _, ep := range result {
		addrs[ep.Address] = true
	}
	if !addrs[testAddr1] || !addrs[testAddr2] {
		t.Errorf("expected us-east endpoints, got %v", addrs)
	}
}
