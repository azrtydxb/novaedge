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

// Package mesh provides east-west service mesh traffic interception using
// iptables TPROXY for transparent proxying of pod-to-pod traffic.
package mesh

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"go.uber.org/zap"
)

const (
	// novaedgeChain is the iptables chain used for TPROXY rules.
	novaedgeChain = "NOVAEDGE_MESH"

	// tproxyMark is the fwmark value set on TPROXY'd packets.
	tproxyMark = "0x1/0x1"
)

// IPTablesRunner abstracts iptables command execution for testability.
type IPTablesRunner interface {
	Run(args ...string) error
	Output(args ...string) (string, error)
}

// execRunner runs real iptables commands.
type execRunner struct{}

func (e *execRunner) Run(args ...string) error {
	cmd := exec.CommandContext(context.Background(), "iptables", args...) //nolint:gosec // args are constructed internally, not from user input
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %s: %s: %w", strings.Join(args, " "), string(out), err)
	}
	return nil
}

func (e *execRunner) Output(args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "iptables", args...) //nolint:gosec // args are constructed internally, not from user input
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("iptables %s: %s: %w", strings.Join(args, " "), string(out), err)
	}
	return string(out), nil
}

// interceptedService tracks an actively intercepted ClusterIP + port combination.
type interceptedService struct {
	clusterIP string
	port      int32
}

// TPROXYManager manages iptables TPROXY rules for intercepting east-west
// traffic destined to Kubernetes ClusterIP Services.
type TPROXYManager struct {
	mu           sync.Mutex
	logger       *zap.Logger
	runner       IPTablesRunner
	tproxyPort   int32
	intercepted  map[string]interceptedService // key: "clusterIP:port"
	chainCreated bool
	routingSetUp bool
}

// NewTPROXYManager creates a new TPROXY rule manager.
// tproxyPort is the local port where the transparent listener accepts connections.
func NewTPROXYManager(logger *zap.Logger, tproxyPort int32) *TPROXYManager {
	return &TPROXYManager{
		logger:      logger.Named("tproxy"),
		runner:      &execRunner{},
		tproxyPort:  tproxyPort,
		intercepted: make(map[string]interceptedService),
	}
}

// NewTPROXYManagerWithRunner creates a TPROXY manager with a custom runner (for testing).
func NewTPROXYManagerWithRunner(logger *zap.Logger, tproxyPort int32, runner IPTablesRunner) *TPROXYManager {
	return &TPROXYManager{
		logger:      logger.Named("tproxy"),
		runner:      runner,
		tproxyPort:  tproxyPort,
		intercepted: make(map[string]interceptedService),
	}
}

// Setup initializes the iptables chain and ip rule for TPROXY.
// Must be called before ApplyRules.
func (m *TPROXYManager) Setup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureChain(); err != nil {
		return fmt.Errorf("failed to create iptables chain: %w", err)
	}

	if err := m.ensureRouting(); err != nil {
		return fmt.Errorf("failed to set up ip rule for TPROXY: %w", err)
	}

	return nil
}

// ApplyRules reconciles iptables TPROXY rules to match the desired set of
// intercepted ClusterIP:port pairs. Rules for removed services are deleted;
// rules for new services are added.
func (m *TPROXYManager) ApplyRules(desired []InterceptTarget) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Build desired map
	desiredMap := make(map[string]InterceptTarget, len(desired))
	for _, t := range desired {
		desiredMap[t.Key()] = t
	}

	// Remove rules that are no longer desired
	for key, svc := range m.intercepted {
		if _, ok := desiredMap[key]; !ok {
			if err := m.removeRule(svc.clusterIP, svc.port); err != nil {
				m.logger.Error("Failed to remove TPROXY rule",
					zap.String("clusterIP", svc.clusterIP),
					zap.Int32("port", svc.port),
					zap.Error(err))
			}
			delete(m.intercepted, key)
		}
	}

	// Add rules for newly desired services
	for key, t := range desiredMap {
		if _, ok := m.intercepted[key]; !ok {
			if err := m.addRule(t.ClusterIP, t.Port); err != nil {
				m.logger.Error("Failed to add TPROXY rule",
					zap.String("clusterIP", t.ClusterIP),
					zap.Int32("port", t.Port),
					zap.Error(err))
				continue
			}
			m.intercepted[key] = interceptedService{
				clusterIP: t.ClusterIP,
				port:      t.Port,
			}
		}
	}

	m.logger.Info("TPROXY rules reconciled", zap.Int("active_rules", len(m.intercepted)))
	return nil
}

// Cleanup removes all TPROXY rules, the custom chain, and ip rules.
func (m *TPROXYManager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove all individual rules
	for key, svc := range m.intercepted {
		if err := m.removeRule(svc.clusterIP, svc.port); err != nil {
			m.logger.Error("Failed to remove TPROXY rule during cleanup",
				zap.String("key", key), zap.Error(err))
		}
		delete(m.intercepted, key)
	}

	// Remove chain jump from PREROUTING
	if m.chainCreated {
		_ = m.runner.Run("-t", "mangle", "-D", "PREROUTING", "-j", novaedgeChain)

		// Flush and delete the chain
		_ = m.runner.Run("-t", "mangle", "-F", novaedgeChain)
		_ = m.runner.Run("-t", "mangle", "-X", novaedgeChain)
		m.chainCreated = false
	}

	// Remove ip rule
	if m.routingSetUp {
		_ = exec.CommandContext(context.Background(), "ip", "rule", "del", "fwmark", "1", "lookup", "100").Run()                     //nolint:gosec // static args
		_ = exec.CommandContext(context.Background(), "ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", "100").Run() //nolint:gosec // static args
		m.routingSetUp = false
	}

	m.logger.Info("TPROXY cleanup complete")
	return nil
}

// ensureChain creates the NOVAEDGE_MESH chain in the mangle table and
// adds a jump from PREROUTING if not already present.
func (m *TPROXYManager) ensureChain() error {
	// Create chain (ignore error if exists)
	_ = m.runner.Run("-t", "mangle", "-N", novaedgeChain)

	// Check if jump already exists
	out, _ := m.runner.Output("-t", "mangle", "-S", "PREROUTING")
	if !strings.Contains(out, novaedgeChain) {
		if err := m.runner.Run("-t", "mangle", "-A", "PREROUTING", "-j", novaedgeChain); err != nil {
			return err
		}
	}

	m.chainCreated = true
	return nil
}

// ensureRouting sets up the ip rule and routing table needed for TPROXY.
// Packets marked by TPROXY need a local route to be delivered to the proxy.
func (m *TPROXYManager) ensureRouting() error {
	// Add ip rule: fwmark 1 → lookup table 100
	if err := exec.CommandContext(context.Background(), "ip", "rule", "add", "fwmark", "1", "lookup", "100").Run(); err != nil { //nolint:gosec // static args
		// May already exist — check
		out, _ := exec.CommandContext(context.Background(), "ip", "rule", "show").Output() //nolint:gosec // static args
		if !strings.Contains(string(out), "fwmark 0x1 lookup 100") {
			return fmt.Errorf("failed to add ip rule: %w", err)
		}
	}

	// Add local route: all traffic in table 100 goes to loopback
	if err := exec.CommandContext(context.Background(), "ip", "route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", "100").Run(); err != nil { //nolint:gosec // static args
		out, _ := exec.CommandContext(context.Background(), "ip", "route", "show", "table", "100").Output() //nolint:gosec // static args
		if !strings.Contains(string(out), "local default dev lo") {
			return fmt.Errorf("failed to add local route: %w", err)
		}
	}

	m.routingSetUp = true
	return nil
}

// addRule adds a TPROXY rule to intercept traffic to clusterIP:port.
func (m *TPROXYManager) addRule(clusterIP string, port int32) error {
	return m.runner.Run(
		"-t", "mangle",
		"-A", novaedgeChain,
		"-p", "tcp",
		"-d", clusterIP,
		"--dport", fmt.Sprintf("%d", port),
		"-j", "TPROXY",
		"--tproxy-mark", tproxyMark,
		"--on-port", fmt.Sprintf("%d", m.tproxyPort),
	)
}

// removeRule removes the TPROXY rule for clusterIP:port.
func (m *TPROXYManager) removeRule(clusterIP string, port int32) error {
	return m.runner.Run(
		"-t", "mangle",
		"-D", novaedgeChain,
		"-p", "tcp",
		"-d", clusterIP,
		"--dport", fmt.Sprintf("%d", port),
		"-j", "TPROXY",
		"--tproxy-mark", tproxyMark,
		"--on-port", fmt.Sprintf("%d", m.tproxyPort),
	)
}

// InterceptTarget represents a ClusterIP:port to intercept.
type InterceptTarget struct {
	ClusterIP string
	Port      int32
}

// Key returns a unique key for this target.
func (t InterceptTarget) Key() string {
	return fmt.Sprintf("%s:%d", t.ClusterIP, t.Port)
}

// ActiveRuleCount returns the number of active TPROXY rules.
func (m *TPROXYManager) ActiveRuleCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.intercepted)
}
