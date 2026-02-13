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
	testAddrRemote1 = "10.0.1.1"
	testAddrRemote2 = "10.0.1.2"
)

// helper to create an endpoint with a zone label
func endpointWithZone(address string, _ int32, ready bool, zone string) *pb.Endpoint {
	ep := &pb.Endpoint{
		Address: address,
		Port:    8080,
		Ready:   ready,
	}
	if zone != "" {
		ep.Labels = map[string]string{
			ZoneTopologyLabel: zone,
		}
	}
	return ep
}

func TestLocalityLBLocalZonePreferred(t *testing.T) {
	endpoints := []*pb.Endpoint{
		endpointWithZone(testAddrEWMA, 8080, true, "us-east-1a"),
		endpointWithZone(testAddrLC2, 8080, true, "us-east-1a"),
		endpointWithZone("10.0.0.3", 8080, true, "us-east-1a"),
		endpointWithZone("10.0.0.4", 8080, true, "us-east-1b"),
		endpointWithZone("10.0.0.5", 8080, true, "us-east-1b"),
	}

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		MinHealthyPercent: 0.7,
	}

	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// All local zone endpoints are healthy (100% >= 70%), so only local zone
	// endpoints should be selected.
	selections := make(map[string]int)
	for i := 0; i < 300; i++ {
		ep := llb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// Only us-east-1a endpoints should be selected
	for addr := range selections {
		if addr != testAddrEWMA && addr != testAddrLC2 && addr != "10.0.0.3" {
			t.Errorf("Non-local endpoint %s was selected when local zone is healthy", addr)
		}
	}

	if len(selections) == 0 {
		t.Fatal("No endpoints were selected")
	}
}

func TestLocalityLBFallbackWhenLocalZoneUnhealthy(t *testing.T) {
	endpoints := []*pb.Endpoint{
		endpointWithZone(testAddrEWMA, 8080, true, "us-east-1a"),
		endpointWithZone(testAddrLC2, 8080, false, "us-east-1a"),
		endpointWithZone("10.0.0.3", 8080, false, "us-east-1a"),
		endpointWithZone("10.0.0.4", 8080, false, "us-east-1a"),
		endpointWithZone("10.0.0.5", 8080, true, "us-east-1b"),
		endpointWithZone("10.0.0.6", 8080, true, "us-east-1b"),
	}

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		MinHealthyPercent: 0.7,
	}

	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// Only 1 out of 4 local endpoints healthy = 25%, which is below 70%
	// Should fall back to all zones.
	selections := make(map[string]int)
	for i := 0; i < 300; i++ {
		ep := llb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// Should include endpoints from us-east-1b (remote zone)
	hasRemote := false
	for addr := range selections {
		if addr == "10.0.0.5" || addr == "10.0.0.6" {
			hasRemote = true
			break
		}
	}
	if !hasRemote {
		t.Error("Expected fallback to include remote zone endpoints when local zone is degraded")
	}
}

func TestLocalityLBDisabled(t *testing.T) {
	endpoints := []*pb.Endpoint{
		endpointWithZone(testAddrEWMA, 8080, true, "us-east-1a"),
		endpointWithZone(testAddrLC2, 8080, true, "us-east-1b"),
		endpointWithZone("10.0.0.3", 8080, true, "us-east-1c"),
	}

	config := LocalityConfig{
		Enabled:           false,
		LocalZone:         "us-east-1a",
		MinHealthyPercent: 0.7,
	}

	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// When disabled, all endpoints should be used
	selections := make(map[string]int)
	for i := 0; i < 300; i++ {
		ep := llb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// All three endpoints should be selected
	if len(selections) != 3 {
		t.Errorf("Expected 3 endpoints selected when locality disabled, got %d", len(selections))
	}
}

func TestLocalityLBEndpointsWithoutZoneMetadata(t *testing.T) {
	endpoints := []*pb.Endpoint{
		endpointWithZone(testAddrEWMA, 8080, true, "us-east-1a"),
		{Address: testAddrLC2, Port: 8080, Ready: true},                             // no labels at all
		{Address: "10.0.0.3", Port: 8080, Ready: true, Labels: nil},                 // nil labels
		{Address: "10.0.0.4", Port: 8080, Ready: true, Labels: map[string]string{}}, // empty labels
	}

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		MinHealthyPercent: 0.7,
	}

	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// Only 1 endpoint in us-east-1a; that is 100% healthy, so should select
	// only the local zone endpoint.
	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		ep := llb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	if selections[testAddrEWMA] != 100 {
		t.Errorf("Expected only local zone endpoint, but got selections: %v", selections)
	}
}

func TestEndpointZone(t *testing.T) {
	tests := []struct {
		name     string
		endpoint *pb.Endpoint
		want     string
	}{
		{
			name:     "nil endpoint",
			endpoint: nil,
			want:     "unknown",
		},
		{
			name:     "no labels",
			endpoint: &pb.Endpoint{Address: testAddrEWMA, Port: 8080},
			want:     "unknown",
		},
		{
			name: "empty labels",
			endpoint: &pb.Endpoint{
				Address: testAddrEWMA,
				Port:    8080,
				Labels:  map[string]string{},
			},
			want: "unknown",
		},
		{
			name: "zone label present",
			endpoint: &pb.Endpoint{
				Address: testAddrEWMA,
				Port:    8080,
				Labels: map[string]string{
					ZoneTopologyLabel: "eu-west-1a",
				},
			},
			want: "eu-west-1a",
		},
		{
			name: "empty zone label value",
			endpoint: &pb.Endpoint{
				Address: testAddrEWMA,
				Port:    8080,
				Labels: map[string]string{
					ZoneTopologyLabel: "",
				},
			},
			want: "unknown",
		},
		{
			name: "other labels but no zone",
			endpoint: &pb.Endpoint{
				Address: testAddrEWMA,
				Port:    8080,
				Labels: map[string]string{
					"app": "web",
				},
			},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EndpointZone(tt.endpoint)
			if got != tt.want {
				t.Errorf("EndpointZone() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLocalityLBMinHealthyPercentThreshold(t *testing.T) {
	// 5 local endpoints: 3 healthy, 2 unhealthy = 60% healthy
	localEndpoints := []*pb.Endpoint{
		endpointWithZone(testAddrEWMA, 8080, true, "us-east-1a"),
		endpointWithZone(testAddrLC2, 8080, true, "us-east-1a"),
		endpointWithZone("10.0.0.3", 8080, true, "us-east-1a"),
		endpointWithZone("10.0.0.4", 8080, false, "us-east-1a"),
		endpointWithZone("10.0.0.5", 8080, false, "us-east-1a"),
	}
	remoteEndpoints := []*pb.Endpoint{
		endpointWithZone(testAddrRemote1, 8080, true, "us-east-1b"),
		endpointWithZone(testAddrRemote2, 8080, true, "us-east-1b"),
	}
	allEndpoints := make([]*pb.Endpoint, 0, len(localEndpoints)+len(remoteEndpoints))
	allEndpoints = append(allEndpoints, localEndpoints...)
	allEndpoints = append(allEndpoints, remoteEndpoints...)

	// Test with threshold at 70%: 60% healthy < 70% -> should fall back
	t.Run("below threshold falls back", func(t *testing.T) {
		config := LocalityConfig{
			Enabled:           true,
			LocalZone:         "us-east-1a",
			MinHealthyPercent: 0.7,
		}

		inner := NewRoundRobin(allEndpoints)
		llb := NewLocalityLB(inner, config, allEndpoints)

		selections := make(map[string]int)
		for i := 0; i < 300; i++ {
			ep := llb.Select()
			if ep == nil {
				t.Fatal("Select returned nil")
			}
			selections[ep.Address]++
		}

		hasRemote := false
		for addr := range selections {
			if addr == testAddrRemote1 || addr == testAddrRemote2 {
				hasRemote = true
				break
			}
		}
		if !hasRemote {
			t.Error("Expected remote endpoints when local healthy % is below threshold")
		}
	})

	// Test with threshold at 50%: 60% healthy >= 50% -> should stay local
	t.Run("above threshold stays local", func(t *testing.T) {
		config := LocalityConfig{
			Enabled:           true,
			LocalZone:         "us-east-1a",
			MinHealthyPercent: 0.5,
		}

		inner := NewRoundRobin(allEndpoints)
		llb := NewLocalityLB(inner, config, allEndpoints)

		selections := make(map[string]int)
		for i := 0; i < 300; i++ {
			ep := llb.Select()
			if ep == nil {
				t.Fatal("Select returned nil")
			}
			selections[ep.Address]++
		}

		for addr := range selections {
			if addr == testAddrRemote1 || addr == testAddrRemote2 {
				t.Errorf("Remote endpoint %s should not be selected when local zone is above threshold", addr)
			}
		}
	})

	// Test with threshold at exactly 60%: 60% healthy >= 60% -> should stay local
	t.Run("at exact threshold stays local", func(t *testing.T) {
		config := LocalityConfig{
			Enabled:           true,
			LocalZone:         "us-east-1a",
			MinHealthyPercent: 0.6,
		}

		inner := NewRoundRobin(allEndpoints)
		llb := NewLocalityLB(inner, config, allEndpoints)

		selections := make(map[string]int)
		for i := 0; i < 300; i++ {
			ep := llb.Select()
			if ep == nil {
				t.Fatal("Select returned nil")
			}
			selections[ep.Address]++
		}

		for addr := range selections {
			if addr == testAddrRemote1 || addr == testAddrRemote2 {
				t.Errorf("Remote endpoint %s should not be selected when local zone meets threshold exactly", addr)
			}
		}
	})
}

func TestLocalityLBNoLocalZoneEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		endpointWithZone(testAddrEWMA, 8080, true, "us-east-1b"),
		endpointWithZone(testAddrLC2, 8080, true, "us-east-1c"),
	}

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		MinHealthyPercent: 0.7,
	}

	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// No local zone endpoints at all — should use all endpoints
	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		ep := llb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	if len(selections) != 2 {
		t.Errorf("Expected 2 endpoints selected, got %d", len(selections))
	}
}

func TestLocalityLBUpdateEndpoints(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		endpointWithZone(testAddrEWMA, 8080, true, "us-east-1a"),
		endpointWithZone(testAddrLC2, 8080, true, "us-east-1b"),
	}

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "us-east-1a",
		MinHealthyPercent: 0.7,
	}

	inner := NewRoundRobin(initialEndpoints)
	llb := NewLocalityLB(inner, config, initialEndpoints)

	// Initially, local zone has 1 healthy endpoint (100% healthy), so should
	// only select local.
	ep := llb.Select()
	if ep == nil {
		t.Fatal("Select returned nil")
	}
	if ep.Address != testAddrEWMA {
		t.Errorf("Expected local endpoint 10.0.0.1, got %s", ep.Address)
	}

	// Update: remove local endpoints, add new remote ones
	newEndpoints := []*pb.Endpoint{
		endpointWithZone("10.0.0.3", 8080, true, "us-east-1b"),
		endpointWithZone("10.0.0.4", 8080, true, "us-east-1c"),
	}
	llb.UpdateEndpoints(newEndpoints)

	// No local zone endpoints now — should select from all
	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		ep := llb.Select()
		if ep == nil {
			t.Fatal("Select returned nil after update")
		}
		selections[ep.Address]++
	}

	if selections[testAddrEWMA] > 0 || selections[testAddrLC2] > 0 {
		t.Error("Old endpoints should not be selected after update")
	}
	if selections["10.0.0.3"] == 0 || selections["10.0.0.4"] == 0 {
		t.Error("New endpoints should be selected after update")
	}
}

func TestLocalityLBEmptyLocalZone(t *testing.T) {
	endpoints := []*pb.Endpoint{
		endpointWithZone(testAddrEWMA, 8080, true, "us-east-1a"),
		endpointWithZone(testAddrLC2, 8080, true, "us-east-1b"),
	}

	config := LocalityConfig{
		Enabled:           true,
		LocalZone:         "", // Empty local zone
		MinHealthyPercent: 0.7,
	}

	inner := NewRoundRobin(endpoints)
	llb := NewLocalityLB(inner, config, endpoints)

	// Empty LocalZone should use all endpoints (same as disabled)
	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		ep := llb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	if len(selections) != 2 {
		t.Errorf("Expected 2 endpoints selected with empty LocalZone, got %d", len(selections))
	}
}

func TestDefaultLocalityConfig(t *testing.T) {
	config := DefaultLocalityConfig()

	if config.Enabled {
		t.Error("Default config should have Enabled=false")
	}
	if config.LocalZone != "" {
		t.Errorf("Default config should have empty LocalZone, got %q", config.LocalZone)
	}
	if config.MinHealthyPercent != DefaultMinHealthyPercent {
		t.Errorf("Default config MinHealthyPercent = %f, want %f", config.MinHealthyPercent, DefaultMinHealthyPercent)
	}
}

func TestGroupByZone(t *testing.T) {
	endpoints := []*pb.Endpoint{
		endpointWithZone(testAddrEWMA, 8080, true, "us-east-1a"),
		endpointWithZone(testAddrLC2, 8080, true, "us-east-1a"),
		endpointWithZone("10.0.0.3", 8080, true, "us-east-1b"),
		{Address: "10.0.0.4", Port: 8080, Ready: true}, // no zone
	}

	zones := groupByZone(endpoints)

	if len(zones["us-east-1a"]) != 2 {
		t.Errorf("Expected 2 endpoints in us-east-1a, got %d", len(zones["us-east-1a"]))
	}
	if len(zones["us-east-1b"]) != 1 {
		t.Errorf("Expected 1 endpoint in us-east-1b, got %d", len(zones["us-east-1b"]))
	}
	if len(zones["unknown"]) != 1 {
		t.Errorf("Expected 1 endpoint in unknown zone, got %d", len(zones["unknown"]))
	}
}
