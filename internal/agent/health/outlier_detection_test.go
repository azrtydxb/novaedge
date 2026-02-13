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
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func testOutlierConfig() OutlierDetectionConfig {
	return OutlierDetectionConfig{
		Interval:                   50 * time.Millisecond,
		BaseEjectionTime:           100 * time.Millisecond,
		MaxEjectionPercent:         50,
		SuccessRateMinHosts:        5,
		SuccessRateRequestVolume:   100,
		SuccessRateStdevFactor:     1.9,
		FailurePercentageThreshold: 85,
		ConsecutiveErrors:          5,
	}
}

func TestDefaultOutlierDetectionConfig(t *testing.T) {
	cfg := DefaultOutlierDetectionConfig()

	if cfg.Interval != 10*time.Second {
		t.Errorf("expected Interval=10s, got %v", cfg.Interval)
	}
	if cfg.BaseEjectionTime != 30*time.Second {
		t.Errorf("expected BaseEjectionTime=30s, got %v", cfg.BaseEjectionTime)
	}
	if cfg.MaxEjectionPercent != 50 {
		t.Errorf("expected MaxEjectionPercent=50, got %v", cfg.MaxEjectionPercent)
	}
	if cfg.SuccessRateMinHosts != 5 {
		t.Errorf("expected SuccessRateMinHosts=5, got %v", cfg.SuccessRateMinHosts)
	}
	if cfg.SuccessRateRequestVolume != 100 {
		t.Errorf("expected SuccessRateRequestVolume=100, got %v", cfg.SuccessRateRequestVolume)
	}
	if cfg.SuccessRateStdevFactor != 1.9 {
		t.Errorf("expected SuccessRateStdevFactor=1.9, got %v", cfg.SuccessRateStdevFactor)
	}
	if cfg.FailurePercentageThreshold != 85 {
		t.Errorf("expected FailurePercentageThreshold=85, got %v", cfg.FailurePercentageThreshold)
	}
	if cfg.ConsecutiveErrors != 5 {
		t.Errorf("expected ConsecutiveErrors=5, got %v", cfg.ConsecutiveErrors)
	}
}

func TestNewOutlierDetector(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()

	od := NewOutlierDetector("default/backend", cfg, logger)

	if od == nil {
		t.Fatal("expected non-nil outlier detector")
	}
	if od.cluster != "default/backend" {
		t.Errorf("expected cluster 'default/backend', got %q", od.cluster)
	}
	if od.stats == nil {
		t.Error("expected non-nil stats map")
	}
	if od.ejections == nil {
		t.Error("expected non-nil ejections map")
	}
}

func TestOutlierDetector_RecordSuccessAndFailure(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.ConsecutiveErrors = 100 // High threshold to avoid ejection in this test
	od := NewOutlierDetector("default/backend", cfg, logger)

	od.RecordSuccess(testEndpointAddr)
	od.RecordSuccess(testEndpointAddr)
	od.RecordFailure(testEndpointAddr)

	od.mu.RLock()
	s := od.stats[testEndpointAddr]
	if s.requests != 3 {
		t.Errorf("expected 3 requests, got %d", s.requests)
	}
	if s.successes != 2 {
		t.Errorf("expected 2 successes, got %d", s.successes)
	}
	if s.consecutiveErrors != 1 {
		t.Errorf("expected 1 consecutive error, got %d", s.consecutiveErrors)
	}
	od.mu.RUnlock()
}

func TestOutlierDetector_ConsecutiveErrorDetection(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.ConsecutiveErrors = 3
	od := NewOutlierDetector("default/backend", cfg, logger)

	// Ensure endpoint is tracked in stats to satisfy max ejection percent check.
	od.RecordFailure(testEndpointAddr)
	od.RecordFailure(testEndpointAddr)

	if od.IsEjected(testEndpointAddr) {
		t.Error("should not be ejected below threshold")
	}

	// Third failure should trigger ejection.
	od.RecordFailure(testEndpointAddr)

	if !od.IsEjected(testEndpointAddr) {
		t.Error("should be ejected after consecutive errors threshold")
	}
}

func TestOutlierDetector_ConsecutiveErrorsResetOnSuccess(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.ConsecutiveErrors = 3
	od := NewOutlierDetector("default/backend", cfg, logger)

	od.RecordFailure(testEndpointAddr)
	od.RecordFailure(testEndpointAddr)
	// A success resets the consecutive error counter.
	od.RecordSuccess(testEndpointAddr)
	od.RecordFailure(testEndpointAddr)
	od.RecordFailure(testEndpointAddr)

	if od.IsEjected(testEndpointAddr) {
		t.Error("should not be ejected; success reset the consecutive error counter")
	}
}

func TestOutlierDetector_SuccessRateOutlierDetection(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.SuccessRateMinHosts = 5
	cfg.SuccessRateRequestVolume = 10
	cfg.SuccessRateStdevFactor = 1.0
	cfg.ConsecutiveErrors = 1000         // Disable consecutive error detection for this test.
	cfg.FailurePercentageThreshold = 100 // Disable failure percentage detection.
	od := NewOutlierDetector("default/backend", cfg, logger)

	// Create 5 good endpoints with 90% success rate.
	goodEndpoints := []string{
		testEndpointAddr,
		"10.0.0.2:8080",
		"10.0.0.3:8080",
		"10.0.0.4:8080",
		"10.0.0.5:8080",
	}
	for _, ep := range goodEndpoints {
		for i := 0; i < 9; i++ {
			od.RecordSuccess(ep)
		}
		od.RecordFailure(ep)
	}

	// Create 1 bad endpoint with 10% success rate.
	badEndpoint := "10.0.0.6:8080"
	od.RecordSuccess(badEndpoint)
	for i := 0; i < 9; i++ {
		od.RecordFailure(badEndpoint)
	}

	// Run analysis.
	od.mu.Lock()
	od.detectSuccessRateOutliers()
	od.mu.Unlock()

	if !od.IsEjected(badEndpoint) {
		t.Error("bad endpoint should be ejected by success rate outlier detection")
	}

	// Good endpoints should not be ejected.
	for _, ep := range goodEndpoints {
		if od.IsEjected(ep) {
			t.Errorf("good endpoint %s should not be ejected", ep)
		}
	}
}

func TestOutlierDetector_FailurePercentageThreshold(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.FailurePercentageThreshold = 80
	cfg.ConsecutiveErrors = 1000 // Disable consecutive error detection.
	od := NewOutlierDetector("default/backend", cfg, logger)

	ep := testEndpointAddr

	// 85% failure rate (above 80% threshold).
	for i := 0; i < 15; i++ {
		od.RecordSuccess(ep)
	}
	for i := 0; i < 85; i++ {
		od.RecordFailure(ep)
	}

	// Run failure percentage analysis.
	od.mu.Lock()
	od.detectFailurePercentageOutliers()
	od.mu.Unlock()

	if !od.IsEjected(ep) {
		t.Error("endpoint should be ejected when failure percentage exceeds threshold")
	}
}

func TestOutlierDetector_FailurePercentageBelowThreshold(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.FailurePercentageThreshold = 80
	cfg.ConsecutiveErrors = 1000 // Disable consecutive error detection.
	od := NewOutlierDetector("default/backend", cfg, logger)

	ep := testEndpointAddr

	// 50% failure rate (below 80% threshold).
	for i := 0; i < 50; i++ {
		od.RecordSuccess(ep)
	}
	for i := 0; i < 50; i++ {
		od.RecordFailure(ep)
	}

	od.mu.Lock()
	od.detectFailurePercentageOutliers()
	od.mu.Unlock()

	if od.IsEjected(ep) {
		t.Error("endpoint should not be ejected when failure percentage is below threshold")
	}
}

func TestOutlierDetector_MaxEjectionPercentRespected(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.MaxEjectionPercent = 25 // Only 25% of 4 endpoints = 1 can be ejected.
	cfg.ConsecutiveErrors = 2
	od := NewOutlierDetector("default/backend", cfg, logger)

	endpoints := []string{
		testEndpointAddr,
		"10.0.0.2:8080",
		"10.0.0.3:8080",
		"10.0.0.4:8080",
	}

	// Register all endpoints in stats so totalEndpoints is correct.
	for _, ep := range endpoints {
		od.RecordSuccess(ep)
	}

	// Trigger consecutive error ejection for all endpoints.
	for _, ep := range endpoints {
		od.RecordFailure(ep)
		od.RecordFailure(ep) // triggers ejection attempt
	}

	ejectedCount := 0
	for _, ep := range endpoints {
		if od.IsEjected(ep) {
			ejectedCount++
		}
	}

	// maxEjectable = floor(4 * 25 / 100) = 1
	if ejectedCount > 1 {
		t.Errorf("expected at most 1 ejected endpoint (25%% of 4), got %d", ejectedCount)
	}
	if ejectedCount == 0 {
		t.Error("expected at least 1 endpoint to be ejected")
	}
}

func TestOutlierDetector_AutoUnejectionAfterPeriod(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.BaseEjectionTime = 50 * time.Millisecond
	cfg.ConsecutiveErrors = 2
	od := NewOutlierDetector("default/backend", cfg, logger)

	ep := testEndpointAddr
	od.RecordFailure(ep)
	od.RecordFailure(ep)

	if !od.IsEjected(ep) {
		t.Fatal("endpoint should be ejected after consecutive errors")
	}

	// Wait for ejection period to expire (BaseEjectionTime * ejectionCount=1 = 50ms).
	time.Sleep(80 * time.Millisecond)

	// Run analysis to trigger unejection check.
	od.mu.Lock()
	od.checkUnejections()
	od.mu.Unlock()

	if od.IsEjected(ep) {
		t.Error("endpoint should be unejected after ejection period expires")
	}
}

func TestOutlierDetector_EjectionBackoff(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.BaseEjectionTime = 50 * time.Millisecond
	cfg.ConsecutiveErrors = 2
	od := NewOutlierDetector("default/backend", cfg, logger)

	ep := testEndpointAddr

	// First ejection.
	od.RecordFailure(ep)
	od.RecordFailure(ep)
	if !od.IsEjected(ep) {
		t.Fatal("expected ejection after first round of failures")
	}

	od.mu.RLock()
	firstCount := od.ejections[ep].ejectionCount
	od.mu.RUnlock()
	if firstCount != 1 {
		t.Errorf("expected ejection count 1, got %d", firstCount)
	}

	// Wait for first ejection period to expire.
	time.Sleep(80 * time.Millisecond)
	od.mu.Lock()
	od.checkUnejections()
	od.mu.Unlock()

	if od.IsEjected(ep) {
		t.Fatal("endpoint should be unejected after first ejection period")
	}

	// Second ejection - should have ejectionCount=2, so duration = 100ms.
	od.RecordFailure(ep)
	od.RecordFailure(ep)
	if !od.IsEjected(ep) {
		t.Fatal("expected ejection after second round of failures")
	}

	od.mu.RLock()
	secondCount := od.ejections[ep].ejectionCount
	od.mu.RUnlock()
	if secondCount != 2 {
		t.Errorf("expected ejection count 2, got %d", secondCount)
	}

	// After 80ms the second ejection should still be active (duration = 100ms).
	time.Sleep(80 * time.Millisecond)
	od.mu.Lock()
	od.checkUnejections()
	od.mu.Unlock()

	if !od.IsEjected(ep) {
		t.Error("endpoint should still be ejected during backoff period (100ms not elapsed)")
	}

	// Wait another 40ms to exceed the 100ms second ejection period.
	time.Sleep(40 * time.Millisecond)
	od.mu.Lock()
	od.checkUnejections()
	od.mu.Unlock()

	if od.IsEjected(ep) {
		t.Error("endpoint should be unejected after backoff period expires")
	}
}

func TestOutlierDetector_IsEjected_NonExistentEndpoint(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	od := NewOutlierDetector("default/backend", cfg, logger)

	if od.IsEjected("nonexistent:8080") {
		t.Error("non-existent endpoint should not be ejected")
	}
}

func TestOutlierDetector_StartStop(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.Interval = 20 * time.Millisecond
	cfg.BaseEjectionTime = 20 * time.Millisecond
	cfg.ConsecutiveErrors = 2
	od := NewOutlierDetector("default/backend", cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	od.Start(ctx)

	// Record failures to trigger ejection.
	ep := testEndpointAddr
	od.RecordFailure(ep)
	od.RecordFailure(ep)

	if !od.IsEjected(ep) {
		t.Fatal("endpoint should be ejected")
	}

	// Wait for analysis loop to auto-uneject (BaseEjectionTime=20ms, Interval=20ms).
	time.Sleep(100 * time.Millisecond)

	if od.IsEjected(ep) {
		t.Error("endpoint should be auto-unejected by the analysis loop")
	}

	od.Stop()
}

func TestOutlierDetector_StartStop_ContextCancellation(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	od := NewOutlierDetector("default/backend", cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	od.Start(ctx)

	// Cancel the context; the loop should exit.
	cancel()

	// Stop should return promptly.
	done := make(chan struct{})
	go func() {
		od.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success.
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return in time after context cancellation")
	}
}

func TestOutlierDetector_ResetStatsClearsCounters(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.ConsecutiveErrors = 1000 // Disable consecutive error ejection.
	od := NewOutlierDetector("default/backend", cfg, logger)

	ep := testEndpointAddr
	od.RecordSuccess(ep)
	od.RecordSuccess(ep)
	od.RecordFailure(ep)

	od.mu.Lock()
	od.resetStats()
	s := od.stats[ep]
	if s.requests != 0 {
		t.Errorf("expected requests=0 after reset, got %d", s.requests)
	}
	if s.successes != 0 {
		t.Errorf("expected successes=0 after reset, got %d", s.successes)
	}
	// Consecutive errors should persist across resets.
	if s.consecutiveErrors != 1 {
		t.Errorf("expected consecutiveErrors=1 after reset (preserved), got %d", s.consecutiveErrors)
	}
	od.mu.Unlock()
}

func TestOutlierDetector_SuccessRateSkippedWithTooFewHosts(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.SuccessRateMinHosts = 5
	cfg.SuccessRateRequestVolume = 10
	cfg.ConsecutiveErrors = 1000
	cfg.FailurePercentageThreshold = 100 // Disable failure percentage detection.
	od := NewOutlierDetector("default/backend", cfg, logger)

	// Only 2 endpoints (below minimum of 5).
	for i := 0; i < 10; i++ {
		od.RecordFailure(testEndpointAddr) // 0% success
		od.RecordSuccess("10.0.0.2:8080")  // 100% success
	}

	od.mu.Lock()
	od.detectSuccessRateOutliers()
	od.mu.Unlock()

	// Neither should be ejected because we have fewer hosts than the minimum.
	if od.IsEjected(testEndpointAddr) {
		t.Error("should not eject when below SuccessRateMinHosts")
	}
}

func TestOutlierDetector_AlreadyEjectedNotDoubleEjected(t *testing.T) {
	logger := zap.NewNop()
	cfg := testOutlierConfig()
	cfg.ConsecutiveErrors = 2
	od := NewOutlierDetector("default/backend", cfg, logger)

	ep := testEndpointAddr
	od.RecordFailure(ep)
	od.RecordFailure(ep)

	if !od.IsEjected(ep) {
		t.Fatal("should be ejected")
	}

	od.mu.RLock()
	count := od.ejections[ep].ejectionCount
	od.mu.RUnlock()

	// Record more failures; ejection count should not increment since already ejected.
	od.RecordFailure(ep)
	od.RecordFailure(ep)

	od.mu.RLock()
	newCount := od.ejections[ep].ejectionCount
	od.mu.RUnlock()

	if newCount != count {
		t.Errorf("ejection count should not change when already ejected; was %d, now %d", count, newCount)
	}
}
