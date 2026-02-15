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
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
)

// fakeRunner records iptables commands for verification.
type fakeRunner struct {
	mu       sync.Mutex
	commands []string
	output   string
	failOn   string // if set, fail commands containing this string
}

func (f *fakeRunner) Run(args ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := strings.Join(args, " ")
	f.commands = append(f.commands, cmd)
	if f.failOn != "" && strings.Contains(cmd, f.failOn) {
		return fmt.Errorf("simulated failure for: %s", cmd)
	}
	return nil
}

func (f *fakeRunner) Output(args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := strings.Join(args, " ")
	f.commands = append(f.commands, cmd)
	return f.output, nil
}

func (f *fakeRunner) containsCommand(substr string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, cmd := range f.commands {
		if strings.Contains(cmd, substr) {
			return true
		}
	}
	return false
}

func (f *fakeRunner) commandCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.commands)
}

func newTestManager(runner *fakeRunner) *TPROXYManager {
	logger := zap.NewNop()
	return NewTPROXYManagerWithRunner(logger, 15001, runner)
}

func TestApplyRulesAddsNewRules(t *testing.T) {
	runner := &fakeRunner{}
	mgr := newTestManager(runner)

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

	if !runner.containsCommand("-d 10.96.0.10 --dport 80 -j TPROXY") {
		t.Error("Expected TPROXY rule for 10.96.0.10:80")
	}
	if !runner.containsCommand("-d 10.96.0.20 --dport 443 -j TPROXY") {
		t.Error("Expected TPROXY rule for 10.96.0.20:443")
	}
}

func TestApplyRulesRemovesOldRules(t *testing.T) {
	runner := &fakeRunner{}
	mgr := newTestManager(runner)

	// Apply initial rules
	if err := mgr.ApplyRules([]InterceptTarget{
		{ClusterIP: "10.96.0.10", Port: 80},
		{ClusterIP: "10.96.0.20", Port: 443},
	}); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	// Apply with one removed
	if err := mgr.ApplyRules([]InterceptTarget{
		{ClusterIP: "10.96.0.10", Port: 80},
	}); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	if mgr.ActiveRuleCount() != 1 {
		t.Errorf("Expected 1 active rule, got %d", mgr.ActiveRuleCount())
	}

	// Should have a -D command for the removed service
	if !runner.containsCommand("-D NOVAEDGE_MESH -p tcp -d 10.96.0.20 --dport 443") {
		t.Error("Expected delete rule for 10.96.0.20:443")
	}
}

func TestApplyRulesIdempotent(t *testing.T) {
	runner := &fakeRunner{}
	mgr := newTestManager(runner)

	targets := []InterceptTarget{
		{ClusterIP: "10.96.0.10", Port: 80},
	}

	// Apply twice
	if err := mgr.ApplyRules(targets); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	countAfterFirst := runner.commandCount()

	if err := mgr.ApplyRules(targets); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	// Second call should not add new iptables commands (only log message is output)
	if runner.commandCount() != countAfterFirst {
		t.Errorf("Expected no new commands on idempotent apply, got %d extra",
			runner.commandCount()-countAfterFirst)
	}
}

func TestApplyRulesEmptyDesired(t *testing.T) {
	runner := &fakeRunner{}
	mgr := newTestManager(runner)

	// Add a rule
	if err := mgr.ApplyRules([]InterceptTarget{
		{ClusterIP: "10.96.0.10", Port: 80},
	}); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	// Apply empty → should remove all
	if err := mgr.ApplyRules(nil); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	if mgr.ActiveRuleCount() != 0 {
		t.Errorf("Expected 0 active rules, got %d", mgr.ActiveRuleCount())
	}
}

func TestApplyRulesContinuesOnFailure(t *testing.T) {
	runner := &fakeRunner{failOn: "10.96.0.20"}
	mgr := newTestManager(runner)

	targets := []InterceptTarget{
		{ClusterIP: "10.96.0.10", Port: 80},
		{ClusterIP: "10.96.0.20", Port: 443}, // This will fail
	}

	// Should not return error — failures are logged per-rule
	if err := mgr.ApplyRules(targets); err != nil {
		t.Fatalf("ApplyRules failed: %v", err)
	}

	// Only the first rule should be active
	if mgr.ActiveRuleCount() != 1 {
		t.Errorf("Expected 1 active rule, got %d", mgr.ActiveRuleCount())
	}
}

func TestInterceptTargetKey(t *testing.T) {
	target := InterceptTarget{ClusterIP: "10.96.0.10", Port: 80}
	if target.Key() != "10.96.0.10:80" {
		t.Errorf("Expected key 10.96.0.10:80, got %s", target.Key())
	}
}
