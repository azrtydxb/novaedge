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

package health

import (
	"runtime"
	"testing"

	"github.com/piwi3910/novaedge/internal/agent/ebpf/testutil"
	"go.uber.org/zap/zaptest"
)

func TestNewHealthMonitor(t *testing.T) {
	logger := zaptest.NewLogger(t)
	hm, err := NewHealthMonitor(logger, 1024)

	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected error on non-Linux platform")
		}
		return
	}

	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewHealthMonitor() returned error: %v", err)
	}
	defer hm.Close()

	if !hm.IsActive() {
		t.Error("expected IsActive() == true after creation")
	}
}

func TestHealthMonitorPoll(t *testing.T) {
	logger := zaptest.NewLogger(t)
	hm, err := NewHealthMonitor(logger, 1024)

	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected error on non-Linux platform")
		}
		return
	}

	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewHealthMonitor() returned error: %v", err)
	}
	defer hm.Close()

	data, err := hm.Poll()
	if err != nil {
		t.Errorf("Poll() returned error: %v", err)
	}
	if data == nil {
		t.Error("Poll() returned nil map")
	}
	if len(data) != 0 {
		t.Errorf("expected empty map from fresh monitor, got %d entries", len(data))
	}
}

func TestHealthMonitorCloseIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	hm, err := NewHealthMonitor(logger, 1024)

	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected error on non-Linux platform")
		}
		return
	}

	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewHealthMonitor() returned error: %v", err)
	}

	if err := hm.Close(); err != nil {
		t.Errorf("first Close() returned error: %v", err)
	}
	if err := hm.Close(); err != nil {
		t.Errorf("second Close() returned error: %v", err)
	}

	if hm.IsActive() {
		t.Error("expected IsActive() == false after Close")
	}
}

func TestNewBackendKey(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		port    uint16
		wantErr bool
	}{
		{name: "valid", ip: "10.0.0.1", port: 8080},
		{name: "valid high port", ip: "192.168.1.100", port: 443},
		{name: "invalid ip", ip: "not-an-ip", port: 80, wantErr: true},
		{name: "ipv6", ip: "::1", port: 80, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := NewBackendKey(tt.ip, tt.port)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key.Addr == [4]byte{} {
				t.Error("expected non-zero address")
			}
			if key.Port != tt.port {
				t.Errorf("port: got %d, want %d", key.Port, tt.port)
			}
		})
	}
}

func TestBackendKeyString(t *testing.T) {
	key, err := NewBackendKey("10.0.0.1", 8080)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := key.String()
	if s != "10.0.0.1:8080" {
		t.Errorf("String() = %q, want %q", s, "10.0.0.1:8080")
	}
}

func TestAggregator(t *testing.T) {
	agg := NewAggregator()

	// First poll with some data.
	perCPU := map[BackendKey][]BackendHealth{
		{Addr: [4]byte{10, 0, 0, 1}, Port: 8080}: {
			{TotalConns: 100, FailedConns: 5, TimeoutConns: 3, SuccessConns: 92, TotalRTTNS: 920000, LastSuccessNS: 1000, LastFailureNS: 500},
			{TotalConns: 50, FailedConns: 2, TimeoutConns: 1, SuccessConns: 47, TotalRTTNS: 470000, LastSuccessNS: 900, LastFailureNS: 600},
		},
	}

	result := agg.Aggregate(perCPU)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	key := BackendKey{Addr: [4]byte{10, 0, 0, 1}, Port: 8080}
	h, ok := result[key]
	if !ok {
		t.Fatal("expected key not found in result")
	}

	// Check summed counters.
	if h.TotalConns != 150 {
		t.Errorf("TotalConns: got %d, want 150", h.TotalConns)
	}
	if h.FailedConns != 7 {
		t.Errorf("FailedConns: got %d, want 7", h.FailedConns)
	}
	if h.TimeoutConns != 4 {
		t.Errorf("TimeoutConns: got %d, want 4", h.TimeoutConns)
	}
	if h.SuccessConns != 139 {
		t.Errorf("SuccessConns: got %d, want 139", h.SuccessConns)
	}

	// Check derived metrics.
	expectedFailureRate := float64(7+4) / float64(150)
	if h.FailureRate < expectedFailureRate-0.001 || h.FailureRate > expectedFailureRate+0.001 {
		t.Errorf("FailureRate: got %f, want ~%f", h.FailureRate, expectedFailureRate)
	}

	expectedAvgRTT := uint64((920000 + 470000) / 139)
	if h.AvgRTTNS != expectedAvgRTT {
		t.Errorf("AvgRTTNS: got %d, want %d", h.AvgRTTNS, expectedAvgRTT)
	}

	// Check timestamps (max across CPUs).
	if h.LastSuccessNS != 1000 {
		t.Errorf("LastSuccessNS: got %d, want 1000", h.LastSuccessNS)
	}
	if h.LastFailureNS != 600 {
		t.Errorf("LastFailureNS: got %d, want 600", h.LastFailureNS)
	}

	// First poll deltas should equal absolute values.
	if h.DeltaTotal != 150 {
		t.Errorf("DeltaTotal: got %d, want 150", h.DeltaTotal)
	}

	// Second poll with increased counters.
	perCPU2 := map[BackendKey][]BackendHealth{
		{Addr: [4]byte{10, 0, 0, 1}, Port: 8080}: {
			{TotalConns: 200, FailedConns: 10, TimeoutConns: 5, SuccessConns: 185, TotalRTTNS: 1850000, LastSuccessNS: 2000, LastFailureNS: 1500},
			{TotalConns: 100, FailedConns: 4, TimeoutConns: 2, SuccessConns: 94, TotalRTTNS: 940000, LastSuccessNS: 1900, LastFailureNS: 1600},
		},
	}

	result2 := agg.Aggregate(perCPU2)
	h2 := result2[key]

	// Delta should reflect the change.
	if h2.DeltaTotal != 150 { // (200+100) - (100+50) = 150
		t.Errorf("DeltaTotal (second poll): got %d, want 150", h2.DeltaTotal)
	}
}

func TestAggregatorEmptyInput(t *testing.T) {
	agg := NewAggregator()
	result := agg.Aggregate(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

func TestSumPerCPUEmpty(t *testing.T) {
	agg := sumPerCPU(nil)
	if agg.TotalConns != 0 {
		t.Errorf("expected zero TotalConns, got %d", agg.TotalConns)
	}
	if agg.FailureRate != 0 {
		t.Errorf("expected zero FailureRate, got %f", agg.FailureRate)
	}
}

func TestSaturatingSub(t *testing.T) {
	if saturatingSub(10, 5) != 5 {
		t.Error("saturatingSub(10, 5) != 5")
	}
	if saturatingSub(5, 10) != 0 {
		t.Error("saturatingSub(5, 10) != 0")
	}
	if saturatingSub(0, 0) != 0 {
		t.Error("saturatingSub(0, 0) != 0")
	}
}

func TestBackendHealth(t *testing.T) {
	bh := BackendHealth{
		TotalConns:    100,
		FailedConns:   5,
		TimeoutConns:  3,
		SuccessConns:  92,
		LastSuccessNS: 1000,
		LastFailureNS: 500,
		TotalRTTNS:    920000,
	}
	if bh.TotalConns != 100 {
		t.Errorf("unexpected TotalConns: %d", bh.TotalConns)
	}
}

func TestHtons(t *testing.T) {
	result := htons(80)
	if result == 0 {
		t.Error("htons(80) returned 0")
	}
}
