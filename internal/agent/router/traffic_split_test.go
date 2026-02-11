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

package router

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestSelectBackendForRequest_SingleBackend(t *testing.T) {
	backends := []*pb.BackendRef{
		{Name: "api-v1", Namespace: "default", Weight: 100},
	}

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	selected := selectBackendForRequest(backends, req)
	if selected == nil {
		t.Fatal("Expected non-nil backend")
	}
	if selected.Name != "api-v1" {
		t.Errorf("Expected api-v1, got %s", selected.Name)
	}
}

func TestSelectBackendForRequest_EmptyBackends(t *testing.T) {
	var backends []*pb.BackendRef
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	selected := selectBackendForRequest(backends, req)
	if selected != nil {
		t.Error("Expected nil for empty backends")
	}
}

func TestSelectBackendForRequest_WeightedDistribution(t *testing.T) {
	backends := []*pb.BackendRef{
		{Name: "api-v1", Namespace: "default", Weight: 90},
		{Name: "api-v2", Namespace: "default", Weight: 10},
	}

	req := httptest.NewRequest(http.MethodGet, "/api", nil)

	selections := make(map[string]int)
	const iterations = 10000

	for i := 0; i < iterations; i++ {
		selected := selectBackendForRequest(backends, req)
		if selected == nil {
			t.Fatal("Expected non-nil backend")
		}
		selections[selected.Name]++
	}

	// v1 should get ~90% of traffic, v2 ~10%
	v1Pct := float64(selections["api-v1"]) / float64(iterations) * 100
	v2Pct := float64(selections["api-v2"]) / float64(iterations) * 100

	// Allow 5% tolerance
	if math.Abs(v1Pct-90) > 5 {
		t.Errorf("api-v1 got %.1f%% of traffic, expected ~90%%", v1Pct)
	}
	if math.Abs(v2Pct-10) > 5 {
		t.Errorf("api-v2 got %.1f%% of traffic, expected ~10%%", v2Pct)
	}
}

func TestSelectBackendForRequest_CanaryHeader(t *testing.T) {
	backends := []*pb.BackendRef{
		{Name: "api-v1", Namespace: "default", Weight: 90},
		{Name: "api-v2", Namespace: "default", Weight: 10},
	}

	// Request with X-Canary: true should always go to v2 (lowest weight)
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("X-Canary", "true")

	for i := 0; i < 100; i++ {
		selected := selectBackendForRequest(backends, req)
		if selected == nil {
			t.Fatal("Expected non-nil backend")
		}
		if selected.Name != "api-v2" {
			t.Errorf("Expected canary to route to api-v2, got %s", selected.Name)
		}
	}
}

func TestSelectBackendForRequest_CanaryHeaderFalse(t *testing.T) {
	backends := []*pb.BackendRef{
		{Name: "api-v1", Namespace: "default", Weight: 90},
		{Name: "api-v2", Namespace: "default", Weight: 10},
	}

	// Request with X-Canary: false should use weighted selection
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("X-Canary", "false")

	selections := make(map[string]int)
	for i := 0; i < 1000; i++ {
		selected := selectBackendForRequest(backends, req)
		if selected != nil {
			selections[selected.Name]++
		}
	}

	// Both should be selected (weighted, not canary override)
	if selections["api-v1"] == 0 {
		t.Error("api-v1 was never selected with X-Canary: false")
	}
	if selections["api-v2"] == 0 {
		t.Error("api-v2 was never selected with X-Canary: false")
	}
}

func TestSelectBackendForRequest_NoCanaryHeader(t *testing.T) {
	backends := []*pb.BackendRef{
		{Name: "api-v1", Namespace: "default", Weight: 90},
		{Name: "api-v2", Namespace: "default", Weight: 10},
	}

	// Request without canary header should use weighted selection
	req := httptest.NewRequest(http.MethodGet, "/api", nil)

	selections := make(map[string]int)
	for i := 0; i < 1000; i++ {
		selected := selectBackendForRequest(backends, req)
		if selected != nil {
			selections[selected.Name]++
		}
	}

	// Both should be selected
	if selections["api-v1"] == 0 {
		t.Error("api-v1 was never selected without canary header")
	}
	if selections["api-v2"] == 0 {
		t.Error("api-v2 was never selected without canary header")
	}
}

func TestSelectCanaryBackend(t *testing.T) {
	tests := []struct {
		name     string
		backends []*pb.BackendRef
		expected string
	}{
		{
			name: "picks lowest weight",
			backends: []*pb.BackendRef{
				{Name: "stable", Weight: 90},
				{Name: "canary", Weight: 10},
			},
			expected: "canary",
		},
		{
			name: "single backend",
			backends: []*pb.BackendRef{
				{Name: "only", Weight: 100},
			},
			expected: "only",
		},
		{
			name: "three backends",
			backends: []*pb.BackendRef{
				{Name: "v1", Weight: 70},
				{Name: "v2", Weight: 25},
				{Name: "v3", Weight: 5},
			},
			expected: "v3",
		},
		{
			name:     "empty backends",
			backends: []*pb.BackendRef{},
			expected: "",
		},
		{
			name: "zero weight defaults to 1",
			backends: []*pb.BackendRef{
				{Name: "a", Weight: 0},
				{Name: "b", Weight: 10},
			},
			expected: "a", // Weight 0 -> 1, which is less than 10
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := selectCanaryBackend(tt.backends)
			if tt.expected == "" {
				if result != nil {
					t.Errorf("Expected nil, got %s", result.Name)
				}
				return
			}
			if result == nil {
				t.Fatal("Expected non-nil result")
			}
			if result.Name != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result.Name)
			}
		})
	}
}

func TestEffectiveWeight(t *testing.T) {
	tests := []struct {
		weight   int32
		expected int32
	}{
		{0, 1},
		{-1, 1},
		{1, 1},
		{50, 50},
		{100, 100},
	}

	for _, tt := range tests {
		b := &pb.BackendRef{Weight: tt.weight}
		result := effectiveWeight(b)
		if result != tt.expected {
			t.Errorf("effectiveWeight(%d) = %d, want %d", tt.weight, result, tt.expected)
		}
	}
}

func TestSelectWeightedBackend_EqualWeights(t *testing.T) {
	backends := []*pb.BackendRef{
		{Name: "a", Weight: 50},
		{Name: "b", Weight: 50},
	}

	selections := make(map[string]int)
	for i := 0; i < 10000; i++ {
		selected := selectWeightedBackend(backends)
		if selected != nil {
			selections[selected.Name]++
		}
	}

	// With equal weights, distribution should be roughly 50/50
	aPct := float64(selections["a"]) / 10000 * 100
	if math.Abs(aPct-50) > 5 {
		t.Errorf("Backend 'a' got %.1f%% of traffic, expected ~50%%", aPct)
	}
}

func TestSelectWeightedBackend_ThreeWaysSplit(t *testing.T) {
	backends := []*pb.BackendRef{
		{Name: "a", Weight: 60},
		{Name: "b", Weight: 30},
		{Name: "c", Weight: 10},
	}

	selections := make(map[string]int)
	for i := 0; i < 10000; i++ {
		selected := selectWeightedBackend(backends)
		if selected != nil {
			selections[selected.Name]++
		}
	}

	aPct := float64(selections["a"]) / 10000 * 100
	bPct := float64(selections["b"]) / 10000 * 100
	cPct := float64(selections["c"]) / 10000 * 100

	if math.Abs(aPct-60) > 5 {
		t.Errorf("Backend 'a' got %.1f%%, expected ~60%%", aPct)
	}
	if math.Abs(bPct-30) > 5 {
		t.Errorf("Backend 'b' got %.1f%%, expected ~30%%", bPct)
	}
	if math.Abs(cPct-10) > 5 {
		t.Errorf("Backend 'c' got %.1f%%, expected ~10%%", cPct)
	}
}
