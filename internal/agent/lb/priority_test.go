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

// helper to create an endpoint with a priority label.
func epWithPriority(addr string, port int32, ready bool, priority string) *pb.Endpoint {
	labels := map[string]string{PriorityLabelKey: priority}
	return &pb.Endpoint{
		Address: addr,
		Port:    port,
		Ready:   ready,
		Labels:  labels,
	}
}

func TestPriorityAllTrafficToHighestPriority(t *testing.T) {
	endpoints := []*pb.Endpoint{
		epWithPriority("10.0.0.1", 8080, true, "0"),
		epWithPriority("10.0.0.2", 8080, true, "0"),
		epWithPriority("10.0.0.3", 8080, true, "1"),
		epWithPriority("10.0.0.4", 8080, true, "1"),
	}

	inner := NewRoundRobin(nil)
	config := DefaultPriorityConfig()
	plb := NewPriorityLB(inner, config, endpoints)

	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		ep := plb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// Only priority 0 endpoints should be selected
	if selections["10.0.0.3"] > 0 || selections["10.0.0.4"] > 0 {
		t.Errorf("Priority 1 endpoints should not be selected when priority 0 is healthy: %v", selections)
	}
	if selections["10.0.0.1"] == 0 || selections["10.0.0.2"] == 0 {
		t.Errorf("Both priority 0 endpoints should be selected: %v", selections)
	}
}

func TestPriorityOverflowWhenBelowThreshold(t *testing.T) {
	// 3 endpoints at priority 0, 1 healthy = 33% healthy, below 70% threshold
	endpoints := []*pb.Endpoint{
		epWithPriority("10.0.0.1", 8080, true, "0"),
		epWithPriority("10.0.0.2", 8080, false, "0"),
		epWithPriority("10.0.0.3", 8080, false, "0"),
		epWithPriority("10.0.0.4", 8080, true, "1"),
		epWithPriority("10.0.0.5", 8080, true, "1"),
	}

	inner := NewRoundRobin(nil)
	config := DefaultPriorityConfig()
	plb := NewPriorityLB(inner, config, endpoints)

	selections := make(map[string]int)
	for i := 0; i < 300; i++ {
		ep := plb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// Priority 0 has 1/3 healthy (33%) which is below 70% threshold,
	// so priority 1 endpoints should also be included.
	if selections["10.0.0.4"] == 0 || selections["10.0.0.5"] == 0 {
		t.Errorf("Priority 1 endpoints should be included during overflow: %v", selections)
	}
	// The one healthy priority 0 endpoint should still receive traffic
	if selections["10.0.0.1"] == 0 {
		t.Errorf("Healthy priority 0 endpoint should still receive traffic: %v", selections)
	}
	// Unhealthy endpoints should never be selected
	if selections["10.0.0.2"] > 0 || selections["10.0.0.3"] > 0 {
		t.Errorf("Unhealthy endpoints should not be selected: %v", selections)
	}
}

func TestPriorityRecoveryReturnsToHighestPriority(t *testing.T) {
	// Start with degraded priority 0
	endpoints := []*pb.Endpoint{
		epWithPriority("10.0.0.1", 8080, true, "0"),
		epWithPriority("10.0.0.2", 8080, false, "0"),
		epWithPriority("10.0.0.3", 8080, false, "0"),
		epWithPriority("10.0.0.4", 8080, true, "1"),
	}

	inner := NewRoundRobin(nil)
	config := DefaultPriorityConfig()
	plb := NewPriorityLB(inner, config, endpoints)

	// Verify overflow is happening
	eligible := plb.EligibleEndpoints()
	if len(eligible) != 2 {
		t.Fatalf("Expected 2 eligible endpoints during overflow, got %d", len(eligible))
	}

	// Now recover priority 0 (all healthy)
	recoveredEndpoints := []*pb.Endpoint{
		epWithPriority("10.0.0.1", 8080, true, "0"),
		epWithPriority("10.0.0.2", 8080, true, "0"),
		epWithPriority("10.0.0.3", 8080, true, "0"),
		epWithPriority("10.0.0.4", 8080, true, "1"),
	}
	plb.UpdateEndpoints(recoveredEndpoints)

	// Only priority 0 should now be selected
	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		ep := plb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	if selections["10.0.0.4"] > 0 {
		t.Errorf("Priority 1 should not be selected after recovery: %v", selections)
	}
	if selections["10.0.0.1"] == 0 || selections["10.0.0.2"] == 0 || selections["10.0.0.3"] == 0 {
		t.Errorf("All priority 0 endpoints should be selected after recovery: %v", selections)
	}
}

func TestPriorityMultipleLevels(t *testing.T) {
	// Priority 0: all unhealthy
	// Priority 1: partially healthy (below threshold)
	// Priority 2: all healthy
	endpoints := []*pb.Endpoint{
		epWithPriority("10.0.0.1", 8080, false, "0"),
		epWithPriority("10.0.0.2", 8080, false, "0"),
		epWithPriority("10.0.0.3", 8080, true, "1"),
		epWithPriority("10.0.0.4", 8080, false, "1"),
		epWithPriority("10.0.0.5", 8080, false, "1"),
		epWithPriority("10.0.0.6", 8080, true, "2"),
		epWithPriority("10.0.0.7", 8080, true, "2"),
	}

	inner := NewRoundRobin(nil)
	config := DefaultPriorityConfig()
	plb := NewPriorityLB(inner, config, endpoints)

	selections := make(map[string]int)
	for i := 0; i < 300; i++ {
		ep := plb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// Priority 0: 0/2 healthy (0%) -> overflow
	// Priority 1: 1/3 healthy (33%) -> still below 70%, overflow again
	// Priority 2: 2/2 healthy (100%) -> stop
	// So eligible = healthy from p0 (none) + healthy from p1 (10.0.0.3) + healthy from p2 (10.0.0.6, 10.0.0.7)
	if selections["10.0.0.1"] > 0 || selections["10.0.0.2"] > 0 {
		t.Errorf("Unhealthy priority 0 endpoints should not be selected: %v", selections)
	}
	if selections["10.0.0.4"] > 0 || selections["10.0.0.5"] > 0 {
		t.Errorf("Unhealthy priority 1 endpoints should not be selected: %v", selections)
	}
	if selections["10.0.0.3"] == 0 {
		t.Errorf("Healthy priority 1 endpoint should be selected: %v", selections)
	}
	if selections["10.0.0.6"] == 0 || selections["10.0.0.7"] == 0 {
		t.Errorf("Priority 2 endpoints should be selected: %v", selections)
	}
}

func TestPriorityNoPriorityLabel(t *testing.T) {
	// Endpoints without priority labels default to priority 0
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		epWithPriority("10.0.0.3", 8080, true, "1"),
	}

	inner := NewRoundRobin(nil)
	config := DefaultPriorityConfig()
	plb := NewPriorityLB(inner, config, endpoints)

	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		ep := plb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// Endpoints without labels are priority 0, so priority 1 should not be selected
	if selections["10.0.0.3"] > 0 {
		t.Errorf("Priority 1 endpoint should not be selected when priority 0 is healthy: %v", selections)
	}
}

func TestPriorityNoEndpoints(t *testing.T) {
	inner := NewRoundRobin(nil)
	config := DefaultPriorityConfig()
	plb := NewPriorityLB(inner, config, nil)

	ep := plb.Select()
	if ep != nil {
		t.Errorf("Expected nil when no endpoints, got %v", ep)
	}
}

func TestPriorityAllUnhealthy(t *testing.T) {
	endpoints := []*pb.Endpoint{
		epWithPriority("10.0.0.1", 8080, false, "0"),
		epWithPriority("10.0.0.2", 8080, false, "1"),
	}

	inner := NewRoundRobin(nil)
	config := DefaultPriorityConfig()
	plb := NewPriorityLB(inner, config, endpoints)

	ep := plb.Select()
	if ep != nil {
		t.Errorf("Expected nil when all endpoints unhealthy, got %v", ep)
	}
}

func TestPriorityCustomThreshold(t *testing.T) {
	// 2 endpoints at priority 0, 1 healthy = 50%
	// With threshold 0.4, this should NOT overflow
	endpoints := []*pb.Endpoint{
		epWithPriority("10.0.0.1", 8080, true, "0"),
		epWithPriority("10.0.0.2", 8080, false, "0"),
		epWithPriority("10.0.0.3", 8080, true, "1"),
	}

	inner := NewRoundRobin(nil)
	config := PriorityConfig{OverflowThreshold: 0.4}
	plb := NewPriorityLB(inner, config, endpoints)

	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		ep := plb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// 50% healthy >= 40% threshold, so no overflow
	if selections["10.0.0.3"] > 0 {
		t.Errorf("Should not overflow with 50%% healthy and 40%% threshold: %v", selections)
	}
}

func TestPriorityExactThreshold(t *testing.T) {
	// 2 endpoints at priority 0, 1 healthy = 50%
	// With threshold exactly 0.5, this should NOT overflow (>= comparison)
	endpoints := []*pb.Endpoint{
		epWithPriority("10.0.0.1", 8080, true, "0"),
		epWithPriority("10.0.0.2", 8080, false, "0"),
		epWithPriority("10.0.0.3", 8080, true, "1"),
	}

	inner := NewRoundRobin(nil)
	config := PriorityConfig{OverflowThreshold: 0.5}
	plb := NewPriorityLB(inner, config, endpoints)

	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		ep := plb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// Exactly at threshold, no overflow
	if selections["10.0.0.3"] > 0 {
		t.Errorf("Should not overflow when exactly at threshold: %v", selections)
	}
}

func TestPriorityInvalidPriorityLabel(t *testing.T) {
	// Invalid priority values should default to 0
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true, Labels: map[string]string{PriorityLabelKey: "invalid"}},
		{Address: "10.0.0.2", Port: 8080, Ready: true, Labels: map[string]string{PriorityLabelKey: "-1"}},
		epWithPriority("10.0.0.3", 8080, true, "1"),
	}

	inner := NewRoundRobin(nil)
	config := DefaultPriorityConfig()
	plb := NewPriorityLB(inner, config, endpoints)

	selections := make(map[string]int)
	for i := 0; i < 100; i++ {
		ep := plb.Select()
		if ep == nil {
			t.Fatal("Select returned nil")
		}
		selections[ep.Address]++
	}

	// Invalid labels default to priority 0
	if selections["10.0.0.3"] > 0 {
		t.Errorf("Priority 1 should not be selected: %v", selections)
	}
	if selections["10.0.0.1"] == 0 || selections["10.0.0.2"] == 0 {
		t.Errorf("Default priority 0 endpoints should be selected: %v", selections)
	}
}

func TestPriorityGetInner(t *testing.T) {
	inner := NewRoundRobin(nil)
	config := DefaultPriorityConfig()
	plb := NewPriorityLB(inner, config, nil)

	if plb.GetInner() != inner {
		t.Error("GetInner should return the underlying load balancer")
	}
}

func TestParsePriority(t *testing.T) {
	tests := []struct {
		name     string
		ep       *pb.Endpoint
		expected int
	}{
		{
			name:     "nil labels",
			ep:       &pb.Endpoint{Address: "10.0.0.1", Port: 8080},
			expected: 0,
		},
		{
			name:     "no priority label",
			ep:       &pb.Endpoint{Address: "10.0.0.1", Port: 8080, Labels: map[string]string{"other": "val"}},
			expected: 0,
		},
		{
			name:     "valid priority 0",
			ep:       &pb.Endpoint{Address: "10.0.0.1", Port: 8080, Labels: map[string]string{PriorityLabelKey: "0"}},
			expected: 0,
		},
		{
			name:     "valid priority 2",
			ep:       &pb.Endpoint{Address: "10.0.0.1", Port: 8080, Labels: map[string]string{PriorityLabelKey: "2"}},
			expected: 2,
		},
		{
			name:     "negative priority defaults to 0",
			ep:       &pb.Endpoint{Address: "10.0.0.1", Port: 8080, Labels: map[string]string{PriorityLabelKey: "-1"}},
			expected: 0,
		},
		{
			name:     "non-numeric defaults to 0",
			ep:       &pb.Endpoint{Address: "10.0.0.1", Port: 8080, Labels: map[string]string{PriorityLabelKey: "abc"}},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parsePriority(tt.ep)
			if result != tt.expected {
				t.Errorf("parsePriority() = %d, want %d", result, tt.expected)
			}
		})
	}
}
