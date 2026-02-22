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
	"fmt"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

const (
	// novaedgeChain is the iptables chain used for TPROXY rules.
	novaedgeChain = "NOVAEDGE_MESH"

	// tproxyMark is the fwmark value set on TPROXY'd packets.
	tproxyMark = "0x1/0x1"
)

// iptablesBackend implements RuleBackend using iptables exec calls.
// This is the fallback when nftables is not available.
type iptablesBackend struct {
	logger       *zap.Logger
	currentRules map[string]bool // set of "clusterIP:port" keys currently installed
	chainCreated bool
	routingSetUp bool
}

func newIPTablesBackend(logger *zap.Logger) *iptablesBackend {
	return &iptablesBackend{
		logger:       logger,
		currentRules: make(map[string]bool),
	}
}

func (b *iptablesBackend) Name() string { return "iptables" }

// Setup creates the NOVAEDGE_MESH chain in the mangle table, adds a jump
// from PREROUTING, and configures the ip rule + route for TPROXY delivery.
func (b *iptablesBackend) Setup() error {
	if err := b.ensureChain(); err != nil {
		return fmt.Errorf("failed to create iptables chain: %w", err)
	}
	if err := b.ensureRouting(); err != nil {
		return fmt.Errorf("failed to set up ip rule for TPROXY: %w", err)
	}
	return nil
}

// ApplyRules reconciles iptables TPROXY rules to match the desired set.
// Rules for removed services are deleted; rules for new services are added.
func (b *iptablesBackend) ApplyRules(targets []InterceptTarget, tproxyPort int32) error {
	desired := make(map[string]InterceptTarget, len(targets))
	for _, t := range targets {
		desired[t.Key()] = t
	}

	// Remove rules no longer desired.
	for key := range b.currentRules {
		if _, ok := desired[key]; !ok {
			// Parse key back to IP:port.
			parts := strings.SplitN(key, ":", 2)
			if len(parts) == 2 {
				if err := b.removeRule(parts[0], parts[1], tproxyPort); err != nil {
					b.logger.Error("Failed to remove iptables TPROXY rule",
						zap.String("key", key), zap.Error(err))
				}
			}
			delete(b.currentRules, key)
		}
	}

	// Add rules for newly desired services.
	for key, t := range desired {
		if !b.currentRules[key] {
			if err := b.addRule(t.ClusterIP, t.Port, tproxyPort); err != nil {
				b.logger.Error("Failed to add iptables TPROXY rule",
					zap.String("clusterIP", t.ClusterIP),
					zap.Int32("port", t.Port),
					zap.Error(err))
				continue
			}
			b.currentRules[key] = true
		}
	}
	return nil
}

// Cleanup removes all TPROXY rules, the custom chain, and ip rules.
func (b *iptablesBackend) Cleanup() error {
	// Remove chain jump from PREROUTING.
	if b.chainCreated {
		_ = b.run("-t", "mangle", "-D", "PREROUTING", "-j", novaedgeChain)
		_ = b.run("-t", "mangle", "-F", novaedgeChain)
		_ = b.run("-t", "mangle", "-X", novaedgeChain)
		b.chainCreated = false
	}

	// Remove ip rule + route.
	if b.routingSetUp {
		_ = exec.CommandContext(context.Background(), "ip", "rule", "del", "fwmark", "1", "lookup", "100").Run()                     //nolint:gosec // static args
		_ = exec.CommandContext(context.Background(), "ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", "100").Run() //nolint:gosec // static args
		b.routingSetUp = false
	}

	b.currentRules = make(map[string]bool)
	return nil
}

// ensureChain creates the NOVAEDGE_MESH chain and adds a PREROUTING jump.
func (b *iptablesBackend) ensureChain() error {
	// Create chain (ignore error if exists).
	_ = b.run("-t", "mangle", "-N", novaedgeChain)

	// Check if jump already exists.
	out, _ := b.output("-t", "mangle", "-S", "PREROUTING")
	if !strings.Contains(out, novaedgeChain) {
		if err := b.run("-t", "mangle", "-A", "PREROUTING", "-j", novaedgeChain); err != nil {
			return err
		}
	}

	b.chainCreated = true
	return nil
}

// ensureRouting sets up the ip rule and routing table needed for TPROXY.
func (b *iptablesBackend) ensureRouting() error {
	if err := exec.CommandContext(context.Background(), "ip", "rule", "add", "fwmark", "1", "lookup", "100").Run(); err != nil { //nolint:gosec // static args
		out, _ := exec.CommandContext(context.Background(), "ip", "rule", "show").Output()                                        //nolint:gosec // static args
		if !strings.Contains(string(out), "fwmark 0x1 lookup 100") {
			return fmt.Errorf("failed to add ip rule: %w", err)
		}
	}

	if err := exec.CommandContext(context.Background(), "ip", "route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", "100").Run(); err != nil { //nolint:gosec // static args
		out, _ := exec.CommandContext(context.Background(), "ip", "route", "show", "table", "100").Output()                                          //nolint:gosec // static args
		if !strings.Contains(string(out), "local default dev lo") {
			return fmt.Errorf("failed to add local route: %w", err)
		}
	}

	b.routingSetUp = true
	return nil
}

func (b *iptablesBackend) addRule(clusterIP string, port int32, tproxyPort int32) error {
	return b.run(
		"-t", "mangle",
		"-A", novaedgeChain,
		"-p", "tcp",
		"-d", clusterIP,
		"--dport", fmt.Sprintf("%d", port),
		"-j", "TPROXY",
		"--tproxy-mark", tproxyMark,
		"--on-port", fmt.Sprintf("%d", tproxyPort),
	)
}

func (b *iptablesBackend) removeRule(clusterIP string, dport string, tproxyPort int32) error {
	return b.run(
		"-t", "mangle",
		"-D", novaedgeChain,
		"-p", "tcp",
		"-d", clusterIP,
		"--dport", dport,
		"-j", "TPROXY",
		"--tproxy-mark", tproxyMark,
		"--on-port", fmt.Sprintf("%d", tproxyPort),
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
