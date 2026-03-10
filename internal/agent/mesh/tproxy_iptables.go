//go:build linux

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
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

const (
	// novaedgeChain is the iptables chain used for NAT DNAT rules.
	novaedgeChain = "NOVAEDGE_MESH"
)

// iptablesBackend implements RuleBackend using iptables exec calls.
// This is the fallback when nftables is not available. Like the nftables
// backend, it uses NAT DNAT to 127.0.0.1 for universal CNI compatibility -- see the
// package-level documentation in tproxy.go for the full rationale.
type iptablesBackend struct {
	logger       *zap.Logger
	currentRules map[string]bool // set of "clusterIP:port" keys currently installed
	chainCreated bool
}

func newIPTablesBackend(logger *zap.Logger) *iptablesBackend {
	return &iptablesBackend{
		logger:       logger,
		currentRules: make(map[string]bool),
	}
}

func (b *iptablesBackend) Name() string { return "iptables" }

// Setup creates the NOVAEDGE_MESH chain in the nat table and adds a
// PREROUTING jump. The chain is inserted at the top of PREROUTING to
// fire before kube-proxy's KUBE-SERVICES chain, preserving the original
// ClusterIP destination in conntrack for SO_ORIGINAL_DST retrieval.
func (b *iptablesBackend) Setup() error {
	// Enable route_localnet so the kernel accepts DNAT to 127.0.0.1 on non-loopback interfaces.
	if err := os.WriteFile("/proc/sys/net/ipv4/conf/all/route_localnet", []byte("1"), 0o600); err != nil {
		return fmt.Errorf("failed to set route_localnet: %w", err)
	}

	if err := b.ensureChain(); err != nil {
		return fmt.Errorf("failed to create iptables chain: %w", err)
	}
	return nil
}

// ApplyRules reconciles iptables NAT DNAT rules to match the
// desired set. Rules for removed services are deleted; rules for new
// services are added. Errors from individual rule operations are
// aggregated and returned so callers can detect partial failures.
func (b *iptablesBackend) ApplyRules(targets []InterceptTarget, tproxyPort int32) error {
	desired := make(map[string]InterceptTarget, len(targets))
	for _, t := range targets {
		desired[t.Key()] = t
	}

	var errs []error

	// Remove rules no longer desired.
	for key := range b.currentRules {
		if _, ok := desired[key]; !ok {
			parts := strings.SplitN(key, ":", 2)
			if len(parts) != 2 {
				b.logger.Error("Invalid iptables rule key, cannot parse IP:port",
					zap.String("key", key))
				delete(b.currentRules, key)
				continue
			}
			if err := b.removeRule(parts[0], parts[1], tproxyPort); err != nil {
				b.logger.Error("Failed to remove iptables DNAT rule",
					zap.String("key", key), zap.Error(err))
				errs = append(errs, fmt.Errorf("remove %s: %w", key, err))
				// Keep key in currentRules so next reconcile retries.
				continue
			}
			delete(b.currentRules, key)
		}
	}

	// Add rules for newly desired services.
	for key, t := range desired {
		if !b.currentRules[key] {
			if err := b.addRule(t.ClusterIP, t.Port, tproxyPort); err != nil {
				b.logger.Error("Failed to add iptables DNAT rule",
					zap.String("clusterIP", t.ClusterIP),
					zap.Int32("port", t.Port),
					zap.Error(err))
				errs = append(errs, fmt.Errorf("add %s: %w", key, err))
				continue
			}
			b.currentRules[key] = true
		}
	}
	return errors.Join(errs...)
}

// Cleanup removes all DNAT rules, the custom chain, and the
// PREROUTING jump.
func (b *iptablesBackend) Cleanup() error {
	if b.chainCreated {
		_ = b.run("-t", "nat", "-D", "PREROUTING", "-j", novaedgeChain)
		_ = b.run("-t", "nat", "-F", novaedgeChain)
		_ = b.run("-t", "nat", "-X", novaedgeChain)
		b.chainCreated = false
	}

	b.currentRules = make(map[string]bool)
	return nil
}

// ensureChain creates the NOVAEDGE_MESH chain in the nat table and
// inserts it at the top of PREROUTING so it fires before kube-proxy.
func (b *iptablesBackend) ensureChain() error {
	// Create chain. The -N command returns an error if the chain already
	// exists, which is expected on restart -- only propagate unexpected errors.
	if err := b.run("-t", "nat", "-N", novaedgeChain); err != nil {
		if !strings.Contains(err.Error(), "Chain already exists") {
			return fmt.Errorf("create chain %s: %w", novaedgeChain, err)
		}
	}

	// Check if jump already exists.
	out, err := b.output("-t", "nat", "-S", "PREROUTING")
	if err != nil {
		return fmt.Errorf("list PREROUTING rules: %w", err)
	}
	if !strings.Contains(out, novaedgeChain) {
		// Insert at position 1 to fire before kube-proxy KUBE-SERVICES.
		if err := b.run("-t", "nat", "-I", "PREROUTING", "1", "-j", novaedgeChain); err != nil {
			return fmt.Errorf("insert PREROUTING jump: %w", err)
		}
	}

	b.chainCreated = true
	return nil
}

func (b *iptablesBackend) addRule(clusterIP string, port int32, tproxyPort int32) error {
	return b.run(
		"-t", "nat",
		"-A", novaedgeChain,
		"-p", "tcp",
		"-d", clusterIP,
		"--dport", fmt.Sprintf("%d", port),
		"-j", "DNAT",
		"--to-destination", fmt.Sprintf("127.0.0.1:%d", tproxyPort),
	)
}

func (b *iptablesBackend) removeRule(clusterIP string, dport string, tproxyPort int32) error {
	return b.run(
		"-t", "nat",
		"-D", novaedgeChain,
		"-p", "tcp",
		"-d", clusterIP,
		"--dport", dport,
		"-j", "DNAT",
		"--to-destination", fmt.Sprintf("127.0.0.1:%d", tproxyPort),
	)
}

func (b *iptablesBackend) run(args ...string) error {
	cmd := exec.CommandContext(context.Background(), "iptables", args...) //nolint:gosec // args are constructed internally
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %s: %s: %w", strings.Join(args, " "), string(out), err)
	}
	return nil
}

func (b *iptablesBackend) output(args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "iptables", args...) //nolint:gosec // args are constructed internally
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("iptables %s: %s: %w", strings.Join(args, " "), string(out), err)
	}
	return string(out), nil
}
