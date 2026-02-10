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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewRetryPolicy_Defaults(t *testing.T) {
	cfg := &pb.RetryConfig{}
	policy := NewRetryPolicy(cfg)

	if policy == nil {
		t.Fatal("expected non-nil policy")
	}
	if policy.MaxRetries != defaultMaxRetries {
		t.Errorf("expected MaxRetries=%d, got %d", defaultMaxRetries, policy.MaxRetries)
	}
	if policy.RetryBudget != defaultRetryBudget {
		t.Errorf("expected RetryBudget=%f, got %f", defaultRetryBudget, policy.RetryBudget)
	}
	if !policy.RetryOn["5xx"] {
		t.Error("expected 5xx in default RetryOn")
	}
	if !policy.RetryOn["connection-failure"] {
		t.Error("expected connection-failure in default RetryOn")
	}
	if !policy.RetryMethodSet["GET"] {
		t.Error("expected GET in default retry methods")
	}
	if !policy.RetryMethodSet["HEAD"] {
		t.Error("expected HEAD in default retry methods")
	}
	if !policy.RetryMethodSet["OPTIONS"] {
		t.Error("expected OPTIONS in default retry methods")
	}
}

func TestNewRetryPolicy_CustomConfig(t *testing.T) {
	cfg := &pb.RetryConfig{
		MaxRetries:      5,
		PerTryTimeoutMs: 1000,
		RetryOn:         []string{"5xx", "reset"},
		RetryBudget:     0.5,
		BackoffBaseMs:   100,
		RetryMethods:    []string{"GET", "POST"},
	}
	policy := NewRetryPolicy(cfg)

	if policy.MaxRetries != 5 {
		t.Errorf("expected MaxRetries=5, got %d", policy.MaxRetries)
	}
	if policy.PerTryTimeout.Milliseconds() != 1000 {
		t.Errorf("expected PerTryTimeout=1000ms, got %dms", policy.PerTryTimeout.Milliseconds())
	}
	if !policy.RetryOn["5xx"] {
		t.Error("expected 5xx in RetryOn")
	}
	if !policy.RetryOn["reset"] {
		t.Error("expected reset in RetryOn")
	}
	if policy.RetryBudget != 0.5 {
		t.Errorf("expected RetryBudget=0.5, got %f", policy.RetryBudget)
	}
	if policy.BackoffBase.Milliseconds() != 100 {
		t.Errorf("expected BackoffBase=100ms, got %dms", policy.BackoffBase.Milliseconds())
	}
	if !policy.RetryMethodSet["POST"] {
		t.Error("expected POST in RetryMethods")
	}
}

func TestNewRetryPolicy_Nil(t *testing.T) {
	policy := NewRetryPolicy(nil)
	if policy != nil {
		t.Error("expected nil policy for nil config")
	}
}

func TestRetryPolicy_ShouldRetry_5xx(t *testing.T) {
	policy := &RetryPolicy{
		RetryOn: map[string]bool{"5xx": true},
	}

	tests := []struct {
		name       string
		statusCode int
		err        error
		expected   bool
	}{
		{"500 should retry", 500, nil, true},
		{"502 should retry", 502, nil, true},
		{"503 should retry", 503, nil, true},
		{"504 should retry", 504, nil, true},
		{"400 should not retry", 400, nil, false},
		{"200 should not retry", 200, nil, false},
		{"404 should not retry", 404, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := policy.shouldRetry(tt.statusCode, tt.err)
			if result != tt.expected {
				t.Errorf("shouldRetry(%d, %v) = %v, want %v", tt.statusCode, tt.err, result, tt.expected)
			}
		})
	}
}

func TestRetryPolicy_ShouldRetry_ConnectionFailure(t *testing.T) {
	policy := &RetryPolicy{
		RetryOn: map[string]bool{"connection-failure": true},
	}

	if !policy.shouldRetry(0, errors.New("connection refused")) {
		t.Error("expected retry on connection failure")
	}
	if policy.shouldRetry(200, nil) {
		t.Error("expected no retry on 200 without error")
	}
}

func TestRetryPolicy_IsMethodRetryable(t *testing.T) {
	policy := &RetryPolicy{
		RetryMethodSet: map[string]bool{
			"GET":  true,
			"HEAD": true,
		},
	}

	if !policy.isMethodRetryable("GET") {
		t.Error("GET should be retryable")
	}
	if !policy.isMethodRetryable("HEAD") {
		t.Error("HEAD should be retryable")
	}
	if policy.isMethodRetryable("POST") {
		t.Error("POST should not be retryable")
	}
	if policy.isMethodRetryable("DELETE") {
		t.Error("DELETE should not be retryable")
	}
}

func TestRetryBudgetTracker(t *testing.T) {
	tracker := &retryBudgetTracker{
		counters: make(map[string]*budgetCounter),
	}

	// Budget should be available initially
	if !tracker.checkBudget("cluster1", 0.2) {
		t.Error("budget should be available initially")
	}

	// Record some requests
	for range 10 {
		tracker.recordRequest("cluster1")
	}

	// Record 1 retry out of 10 requests (10%) - should be within 20% budget
	tracker.recordRetry("cluster1")
	if !tracker.checkBudget("cluster1", 0.2) {
		t.Error("budget should be available at 10% usage with 20% limit")
	}

	// Record more retries to exceed budget
	tracker.recordRetry("cluster1")
	tracker.recordRetry("cluster1")
	if tracker.checkBudget("cluster1", 0.2) {
		t.Error("budget should be exhausted at 30% usage with 20% limit")
	}
}

func TestRetryResponseWriter(t *testing.T) {
	rw := newRetryResponseWriter()

	// Write headers
	rw.Header().Set("X-Custom", "value")
	rw.WriteHeader(http.StatusBadGateway)
	_, _ = rw.Write([]byte("error"))

	if rw.statusCode != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", rw.statusCode)
	}
	if rw.body.String() != "error" {
		t.Errorf("expected body 'error', got '%s'", rw.body.String())
	}

	// Test flush
	w := httptest.NewRecorder()
	rw.flushTo(w)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected flushed status 502, got %d", w.Code)
	}
	if w.Body.String() != "error" {
		t.Errorf("expected flushed body 'error', got '%s'", w.Body.String())
	}
	if w.Header().Get("X-Custom") != "value" {
		t.Errorf("expected X-Custom header, got '%s'", w.Header().Get("X-Custom"))
	}

	// Test reset
	rw.reset()
	if rw.statusCode != http.StatusOK {
		t.Errorf("expected reset status 200, got %d", rw.statusCode)
	}
	if rw.body.Len() != 0 {
		t.Error("expected empty body after reset")
	}
	if rw.written {
		t.Error("expected written=false after reset")
	}
}
