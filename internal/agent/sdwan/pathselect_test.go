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

package sdwan

import (
	"math"
	"testing"
	"time"

	"go.uber.org/zap"
)

const testLinkB = "link-b"

func TestSelectPath_LowestLatency(t *testing.T) {
	links := []LinkQuality{
		{LinkName: "link-a", LatencyMs: 50, Healthy: true},
		{LinkName: "link-b", LatencyMs: 10, Healthy: true},
		{LinkName: "link-c", LatencyMs: 30, Healthy: true},
	}

	got := selectPath(StrategyLowestLatency, links, nil, nil)
	if got != testLinkB {
		t.Errorf("expected link-b (lowest latency 10ms), got %q", got)
	}
}

func TestSelectPath_HighestBandwidth(t *testing.T) {
	links := []LinkQuality{
		{LinkName: "link-a", Healthy: true},
		{LinkName: testLinkB, Healthy: true},
		{LinkName: "link-c", Healthy: true},
	}
	bandwidths := map[string]string{
		"link-a":  "100Mbps",
		testLinkB: "1Gbps",
		"link-c":  "500Mbps",
	}

	got := selectPath(StrategyHighestBandwidth, links, bandwidths, nil)
	if got != testLinkB {
		t.Errorf("expected link-b (1Gbps highest), got %q", got)
	}
}

func TestSelectPath_MostReliable(t *testing.T) {
	links := []LinkQuality{
		{LinkName: "link-a", PacketLoss: 0.05, Healthy: true},
		{LinkName: testLinkB, PacketLoss: 0.001, Healthy: true},
		{LinkName: "link-c", PacketLoss: 0.02, Healthy: true},
	}

	got := selectPath(StrategyMostReliable, links, nil, nil)
	if got != testLinkB {
		t.Errorf("expected link-b (lowest packet loss), got %q", got)
	}
}

func TestSelectPath_LowestCost(t *testing.T) {
	links := []LinkQuality{
		{LinkName: "link-a", Healthy: true},
		{LinkName: testLinkB, Healthy: true},
		{LinkName: "link-c", Healthy: true},
	}
	costs := map[string]int32{
		"link-a":  100,
		testLinkB: 50,
		"link-c":  200,
	}

	got := selectPath(StrategyLowestCost, links, nil, costs)
	if got != testLinkB {
		t.Errorf("expected link-b (cost 50), got %q", got)
	}
}

func TestSelectPath_SkipsUnhealthy(t *testing.T) {
	links := []LinkQuality{
		{LinkName: "link-a", LatencyMs: 5, Healthy: false},
		{LinkName: testLinkB, LatencyMs: 50, Healthy: true},
	}

	got := selectPath(StrategyLowestLatency, links, nil, nil)
	if got != testLinkB {
		t.Errorf("expected link-b (only healthy link), got %q", got)
	}
}

func TestSelectPath_NoHealthyLinks(t *testing.T) {
	links := []LinkQuality{
		{LinkName: "link-a", Healthy: false},
		{LinkName: testLinkB, Healthy: false},
	}

	got := selectPath(StrategyLowestLatency, links, nil, nil)
	if got != "" {
		t.Errorf("expected empty string when no healthy links, got %q", got)
	}
}

func TestSelectPath_EmptyLinks(t *testing.T) {
	got := selectPath(StrategyLowestLatency, nil, nil, nil)
	if got != "" {
		t.Errorf("expected empty string for nil links, got %q", got)
	}
}

func TestSelectPath_UnknownStrategy(t *testing.T) {
	links := []LinkQuality{
		{LinkName: "link-a", LatencyMs: 10, Healthy: true},
		{LinkName: "link-b", LatencyMs: 5, Healthy: true},
	}

	// Unknown strategy should fall back to lowest-latency
	got := selectPath("unknown-strategy", links, nil, nil)
	if got != testLinkB {
		t.Errorf("expected link-b (fallback to lowest latency), got %q", got)
	}
}

func TestPathSelector_Hysteresis(t *testing.T) {
	logger := zap.NewNop()
	ps := NewPathSelector(logger)
	// Use a short hysteresis for testing
	ps.hysteresis = 50 * time.Millisecond

	links := []LinkQuality{
		{LinkName: "link-a", LatencyMs: 10, Healthy: true},
		{LinkName: "link-b", LatencyMs: 20, Healthy: true},
	}

	// First selection: should pick link-a
	selected, err := ps.Select("policy1", StrategyLowestLatency, links, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected != "link-a" {
		t.Errorf("expected link-a, got %q", selected)
	}

	// Reverse quality: link-b now better
	links[0].LatencyMs = 100
	links[1].LatencyMs = 5

	// Immediate re-select: hysteresis should prevent switch
	selected, err = ps.Select("policy1", StrategyLowestLatency, links, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected != "link-a" {
		t.Errorf("expected link-a (hysteresis), got %q", selected)
	}

	// Wait for hysteresis to expire
	time.Sleep(60 * time.Millisecond)

	// Now the switch should happen
	selected, err = ps.Select("policy1", StrategyLowestLatency, links, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected != "link-b" {
		t.Errorf("expected link-b after hysteresis expired, got %q", selected)
	}
}

func TestPathSelector_SetCurrentLink(t *testing.T) {
	logger := zap.NewNop()
	ps := NewPathSelector(logger)

	ps.SetCurrentLink("policy1", "link-x")

	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if ps.currentLinks["policy1"] != "link-x" {
		t.Errorf("expected current link 'link-x', got %q", ps.currentLinks["policy1"])
	}
}

func TestPathSelector_SelectError(t *testing.T) {
	logger := zap.NewNop()
	ps := NewPathSelector(logger)

	// Empty links should return error
	_, err := ps.Select("policy1", StrategyLowestLatency, nil, nil, nil)
	if err == nil {
		t.Error("expected error for empty links")
	}

	// All unhealthy
	links := []LinkQuality{
		{LinkName: "link-a", Healthy: false},
	}
	_, err = ps.Select("policy1", StrategyLowestLatency, links, nil, nil)
	if err == nil {
		t.Error("expected error for no healthy links")
	}
}

func TestParseBandwidthMbps(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"100Mbps", 100},
		{"1Gbps", 1000},
		{"10gbps", 10000},
		{"500kbps", 0.5},
		{"100mbps", 100},
		{"", 0},
		{"invalid", 0},
		{" 250Mbps ", 250},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseBandwidthMbps(tt.input)
			if math.Abs(got-tt.expected) > 0.001 {
				t.Errorf("parseBandwidthMbps(%q) = %f, want %f", tt.input, got, tt.expected)
			}
		})
	}
}
