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

package mesh

import (
	"fmt"
	"sync"
	"testing"

	"go.uber.org/zap"
)

// fakeBackend implements RuleBackend for testing.
type fakeBackend struct {
	mu           sync.Mutex
	currentRules map[string]bool // set of "clusterIP:port" keys
	setupCalled  bool
	cleanupCnt   int
	applyCnt     int
	failOn       string // if set, fail ApplyRules when a target key contains this string
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		currentRules: make(map[string]bool),
	}
}

func (f *fakeBackend) Name() string { return "fake" }

func (f *fakeBackend) Setup() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setupCalled = true
	return nil
}

func (f *fakeBackend) ApplyRules(targets []InterceptTarget, _ int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applyCnt++

	newRules := make(map[string]bool, len(targets))
	for _, t := range targets {
		if f.failOn != "" && t.Key() == f.failOn {
			return fmt.Errorf("simulated failure for: %s", t.Key())
		}
		newRules[t.Key()] = true
	}
	f.currentRules = newRules
	return nil
}

func (f *fakeBackend) Cleanup() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanupCnt++
	f.currentRules = make(map[string]bool)
	return nil
}

func (f *fakeBackend) hasRule(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.currentRules[key]
}

func (f *fakeBackend) applyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.applyCnt
}

func newTestManager(backend *fakeBackend) *TPROXYManager {
	logger := zap.NewNop()
	return NewTPROXYManagerWithBackend(logger, 15001, backend)
}

func TestApplyRulesAddsNewRules(t *testing.T) {
	backend := newFakeBackend()
	mgr := newTestManager(backend)

	targets := []InterceptTarget{
		{ClusterIP: "10.96.0.10", Port: 80},
		{ClusterIP: "10.96.0.20", Port: 443},
	}

	if err := mgr.ApplyRules(targets); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	if mgr.ActiveRuleCount() != 2 {
		t.Errorf("Expected 2 active rules, got %d", mgr.ActiveRuleCount())
	}

	if !backend.hasRule("10.96.0.10:80") {
		t.Error("Expected rule for 10.96.0.10:80")
	}
	if !backend.hasRule("10.96.0.20:443") {
		t.Error("Expected rule for 10.96.0.20:443")
	}
}

func TestApplyRulesRemovesOldRules(t *testing.T) {
	backend := newFakeBackend()
	mgr := newTestManager(backend)

	// Apply initial rules.
	if err := mgr.ApplyRules([]InterceptTarget{
		{ClusterIP: "10.96.0.10", Port: 80},
		{ClusterIP: "10.96.0.20", Port: 443},
	}); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	// Apply with one removed.
	if err := mgr.ApplyRules([]InterceptTarget{
		{ClusterIP: "10.96.0.10", Port: 80},
	}); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	if mgr.ActiveRuleCount() != 1 {
		t.Errorf("Expected 1 active rule, got %d", mgr.ActiveRuleCount())
	}

	if backend.hasRule("10.96.0.20:443") {
		t.Error("Expected rule for 10.96.0.20:443 to be removed")
	}
}

func TestApplyRulesIdempotent(t *testing.T) {
	backend := newFakeBackend()
	mgr := newTestManager(backend)

	targets := []InterceptTarget{
		{ClusterIP: "10.96.0.10", Port: 80},
	}

	// Apply twice.
	if err := mgr.ApplyRules(targets); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	if err := mgr.ApplyRules(targets); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	// Both calls go to backend, but the result is the same set of rules.
	if mgr.ActiveRuleCount() != 1 {
		t.Errorf("Expected 1 active rule, got %d", mgr.ActiveRuleCount())
	}
	if backend.applyCount() != 2 {
		t.Errorf("Expected 2 backend apply calls, got %d", backend.applyCount())
	}
}

func TestApplyRulesEmptyDesired(t *testing.T) {
	backend := newFakeBackend()
	mgr := newTestManager(backend)

	// Add a rule.
	if err := mgr.ApplyRules([]InterceptTarget{
		{ClusterIP: "10.96.0.10", Port: 80},
	}); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	// Apply empty → should remove all.
	if err := mgr.ApplyRules(nil); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	if mgr.ActiveRuleCount() != 0 {
		t.Errorf("Expected 0 active rules, got %d", mgr.ActiveRuleCount())
	}
}

func TestApplyRulesReturnsErrorOnFailure(t *testing.T) {
	backend := newFakeBackend()
	backend.failOn = "10.96.0.20:443"
	mgr := newTestManager(backend)

	targets := []InterceptTarget{
		{ClusterIP: "10.96.0.10", Port: 80},
		{ClusterIP: "10.96.0.20", Port: 443}, // This will fail
	}

	err := mgr.ApplyRules(targets)
	if err == nil {
		t.Fatal("Expected error from ApplyRules")
	}

	// On error, the manager should have 0 tracked rules since the
	// atomic apply failed.
	if mgr.ActiveRuleCount() != 0 {
		t.Errorf("Expected 0 active rules after failure, got %d", mgr.ActiveRuleCount())
	}
}

func TestInterceptTargetKey(t *testing.T) {
	target := InterceptTarget{ClusterIP: "10.96.0.10", Port: 80}
	if target.Key() != "10.96.0.10:80" {
		t.Errorf("Expected key 10.96.0.10:80, got %s", target.Key())
	}
}
