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

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// newTestPanicMetrics creates PanicMetrics with a fresh registry to avoid
// duplicate registration errors across tests.
func newTestPanicMetrics() *PanicMetrics {
	return &PanicMetrics{
		PanicMode: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "novaedge_lb_panic_mode_test",
				Help: "Whether the load balancer is in panic mode (1=panic, 0=normal)",
			},
			[]string{"cluster"},
		),
	}
}

// getGaugeValue reads the current value from a GaugeVec for the given label.
func getGaugeValue(gaugeVec *prometheus.GaugeVec, label string) float64 {
	var m dto.Metric
	gauge, err := gaugeVec.GetMetricWithLabelValues(label)
	if err != nil {
		return -1
	}
	if err := gauge.Write(&m); err != nil {
		return -1
	}
	return m.GetGauge().GetValue()
}

func TestPanicHandlerNormalMode(t *testing.T) {
	// All endpoints healthy: should NOT enter panic mode
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultPanicConfig()
	logger := zaptest.NewLogger(t)
	ph := NewPanicHandler(inner, config, "test-cluster", endpoints, logger, nil)

	if ph.IsPanicking() {
		t.Fatal("expected normal mode when all endpoints are healthy")
	}

	// All selections should only return healthy endpoints
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		ep := ph.Select()
		if ep == nil {
			t.Fatal("Select returned nil in normal mode")
		}
		if !ep.Ready {
			t.Errorf("Selected unhealthy endpoint %s in normal mode", ep.Address)
		}
		seen[ep.Address] = true
	}

	// Should see all healthy endpoints
	if len(seen) != 4 {
		t.Errorf("expected all 4 healthy endpoints to be selected, got %d", len(seen))
	}
}

func TestPanicHandlerActivatesWhenBelowThreshold(t *testing.T) {
	// 1 out of 4 healthy = 25%, threshold is 50% -> should panic
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: false},
		{Address: "10.0.0.4", Port: 8080, Ready: false},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultPanicConfig() // threshold = 0.5
	logger := zaptest.NewLogger(t)
	ph := NewPanicHandler(inner, config, "test-cluster", endpoints, logger, nil)

	if !ph.IsPanicking() {
		t.Fatal("expected panic mode when only 25% endpoints are healthy (threshold 50%)")
	}
}

func TestPanicHandlerIncludesUnhealthyEndpointsInPanicMode(t *testing.T) {
	// 1 out of 4 healthy = 25% -> panic mode
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: false},
		{Address: "10.0.0.4", Port: 8080, Ready: false},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultPanicConfig()
	logger := zaptest.NewLogger(t)
	ph := NewPanicHandler(inner, config, "test-cluster", endpoints, logger, nil)

	if !ph.IsPanicking() {
		t.Fatal("expected panic mode")
	}

	// In panic mode, unhealthy endpoints should be selectable
	seenUnhealthy := false
	for i := 0; i < 200; i++ {
		ep := ph.Select()
		if ep == nil {
			t.Fatal("Select returned nil in panic mode")
		}
		if !ep.Ready {
			seenUnhealthy = true
			break
		}
	}

	if !seenUnhealthy {
		t.Error("expected unhealthy endpoints to be selectable in panic mode")
	}
}

func TestPanicHandlerExitsPanicWhenHealthRecovers(t *testing.T) {
	// Start with 1/4 healthy -> panic
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: false},
		{Address: "10.0.0.4", Port: 8080, Ready: false},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultPanicConfig()
	logger := zaptest.NewLogger(t)
	ph := NewPanicHandler(inner, config, "test-cluster", endpoints, logger, nil)

	if !ph.IsPanicking() {
		t.Fatal("expected panic mode initially")
	}

	// Recover: 3/4 healthy = 75% > 50% threshold
	recoveredEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: false},
	}

	ph.UpdateEndpoints(recoveredEndpoints)

	if ph.IsPanicking() {
		t.Fatal("expected normal mode after health recovery")
	}

	// Should only select healthy endpoints now
	for i := 0; i < 100; i++ {
		ep := ph.Select()
		if ep == nil {
			t.Fatal("Select returned nil after recovery")
		}
		if !ep.Ready {
			t.Errorf("Selected unhealthy endpoint %s after recovery", ep.Address)
		}
	}
}

func TestPanicHandlerThresholdZeroDisablesPanic(t *testing.T) {
	// Even with 0/4 healthy, threshold 0 should NOT trigger panic
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: false},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: false},
		{Address: "10.0.0.4", Port: 8080, Ready: false},
	}

	inner := NewRoundRobin(endpoints)
	config := PanicConfig{
		Threshold: 0,
		Enabled:   true,
	}
	logger := zaptest.NewLogger(t)
	ph := NewPanicHandler(inner, config, "test-cluster", endpoints, logger, nil)

	if ph.IsPanicking() {
		t.Fatal("expected no panic when threshold is 0")
	}

	// Inner LB has no healthy endpoints, so Select returns nil
	ep := ph.Select()
	if ep != nil {
		t.Error("expected nil when all endpoints unhealthy and panic disabled")
	}
}

func TestPanicHandlerDisabledConfig(t *testing.T) {
	// Panic explicitly disabled: should never enter panic mode
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: false},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
	}

	inner := NewRoundRobin(endpoints)
	config := PanicConfig{
		Threshold: 0.5,
		Enabled:   false,
	}
	logger := zaptest.NewLogger(t)
	ph := NewPanicHandler(inner, config, "test-cluster", endpoints, logger, nil)

	if ph.IsPanicking() {
		t.Fatal("expected no panic when Enabled is false")
	}
}

func TestPanicHandlerMetricsGaugeUpdated(t *testing.T) {
	m := newTestPanicMetrics()

	// Start with all healthy -> gauge should be 0
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultPanicConfig()
	logger := zaptest.NewLogger(t)
	ph := NewPanicHandler(inner, config, "metrics-cluster", endpoints, logger, m)

	if ph.IsPanicking() {
		t.Fatal("expected normal mode initially")
	}

	gaugeVal := getGaugeValue(m.PanicMode, "metrics-cluster")
	if gaugeVal != 0 {
		t.Errorf("expected panic gauge = 0, got %f", gaugeVal)
	}

	// Drop to 1/4 healthy -> panic, gauge should be 1
	degradedEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: false},
		{Address: "10.0.0.4", Port: 8080, Ready: false},
	}
	ph.UpdateEndpoints(degradedEndpoints)

	if !ph.IsPanicking() {
		t.Fatal("expected panic mode after degradation")
	}

	gaugeVal = getGaugeValue(m.PanicMode, "metrics-cluster")
	if gaugeVal != 1 {
		t.Errorf("expected panic gauge = 1, got %f", gaugeVal)
	}

	// Recover to 4/4 healthy -> normal, gauge should be 0
	recoveredEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: true},
	}
	ph.UpdateEndpoints(recoveredEndpoints)

	if ph.IsPanicking() {
		t.Fatal("expected normal mode after recovery")
	}

	gaugeVal = getGaugeValue(m.PanicMode, "metrics-cluster")
	if gaugeVal != 0 {
		t.Errorf("expected panic gauge = 0 after recovery, got %f", gaugeVal)
	}
}

func TestPanicHandlerExactThresholdBoundary(t *testing.T) {
	// 2 out of 4 healthy = 50% exactly at threshold -> should NOT panic
	// (panic only when strictly below threshold)
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: false},
		{Address: "10.0.0.4", Port: 8080, Ready: false},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultPanicConfig() // threshold = 0.5
	logger := zaptest.NewLogger(t)
	ph := NewPanicHandler(inner, config, "test-cluster", endpoints, logger, nil)

	if ph.IsPanicking() {
		t.Fatal("expected normal mode when healthy fraction equals threshold exactly")
	}
}

func TestPanicHandlerNoEndpoints(t *testing.T) {
	var endpoints []*pb.Endpoint

	inner := NewRoundRobin(endpoints)
	config := DefaultPanicConfig()
	logger := zaptest.NewLogger(t)
	ph := NewPanicHandler(inner, config, "test-cluster", endpoints, logger, nil)

	if ph.IsPanicking() {
		t.Fatal("expected no panic with empty endpoint list")
	}

	ep := ph.Select()
	if ep != nil {
		t.Error("expected nil when no endpoints exist")
	}
}

func TestPanicHandlerNilLogger(t *testing.T) {
	// Ensure nil logger does not cause a panic
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: false},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultPanicConfig()

	// Should not panic with nil logger
	ph := NewPanicHandler(inner, config, "test-cluster", endpoints, nil, nil)

	if !ph.IsPanicking() {
		t.Fatal("expected panic mode with all unhealthy endpoints")
	}
}

func TestPanicHandlerGetInner(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultPanicConfig()
	ph := NewPanicHandler(inner, config, "test-cluster", endpoints, nil, nil)

	if ph.GetInner() != inner {
		t.Error("GetInner should return the underlying load balancer")
	}
}

func TestDefaultPanicConfig(t *testing.T) {
	config := DefaultPanicConfig()

	if config.Threshold != 0.5 {
		t.Errorf("expected default threshold 0.5, got %f", config.Threshold)
	}
	if !config.Enabled {
		t.Error("expected default Enabled to be true")
	}
}

func TestPanicHandlerSelectDistribution(t *testing.T) {
	// In panic mode, verify all endpoints (healthy + unhealthy) can be selected
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: false},
		{Address: "10.0.0.4", Port: 8080, Ready: false},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultPanicConfig()
	logger := zap.NewNop()
	ph := NewPanicHandler(inner, config, "test-cluster", endpoints, logger, nil)

	if !ph.IsPanicking() {
		t.Fatal("expected panic mode")
	}

	seen := make(map[string]bool)
	for i := 0; i < 500; i++ {
		ep := ph.Select()
		if ep == nil {
			t.Fatal("Select returned nil in panic mode with endpoints available")
		}
		seen[ep.Address] = true
	}

	// All 4 endpoints should have been selected at some point
	for _, ep := range endpoints {
		if !seen[ep.Address] {
			t.Errorf("endpoint %s was never selected in panic mode", ep.Address)
		}
	}
}
