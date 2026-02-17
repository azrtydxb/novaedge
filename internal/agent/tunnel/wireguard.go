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

package tunnel

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	v1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

const (
	wireguardInterfacePrefix = "novaedge-wg"
	wireguardKeepalive       = 25
	maxBackoff               = 30 * time.Second
)

// wireGuardTunnel implements the Tunnel interface using WireGuard CLI tools.
// It shells out to the `wg` and `ip` commands to configure a WireGuard
// interface for encrypted L3 tunneling to a remote cluster.
type wireGuardTunnel struct {
	mu          sync.RWMutex
	clusterName string
	config      v1alpha1.TunnelConfig
	ifaceName   string
	localAddr   string
	healthy     atomic.Bool
	logger      *zap.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
}

// newWireGuardTunnel creates a WireGuard tunnel instance.
func newWireGuardTunnel(clusterName string, config v1alpha1.TunnelConfig, logger *zap.Logger) (*wireGuardTunnel, error) {
	if config.WireGuard == nil {
		return nil, fmt.Errorf("wireguard config is required for WireGuard tunnel type")
	}

	// Generate a deterministic interface name from the cluster name
	ifaceName := fmt.Sprintf("%s-%s", wireguardInterfacePrefix, sanitizeInterfaceName(clusterName))
	if len(ifaceName) > 15 {
		ifaceName = ifaceName[:15]
	}

	return &wireGuardTunnel{
		clusterName: clusterName,
		config:      config,
		ifaceName:   ifaceName,
		logger:      logger.With(zap.String("tunnel", "wireguard"), zap.String("cluster", clusterName)),
		done:        make(chan struct{}),
	}, nil
}

// Start begins the WireGuard tunnel connection and maintenance loop.
func (t *wireGuardTunnel) Start(ctx context.Context) error {
	t.mu.Lock()
	t.ctx, t.cancel = context.WithCancel(ctx)
	t.mu.Unlock()

	go t.maintainConnection(t.ctx)

	return nil
}

// Stop gracefully shuts down the WireGuard tunnel.
func (t *wireGuardTunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cancel != nil {
		t.cancel()
	}

	// Wait for maintenance loop to exit
	<-t.done

	t.healthy.Store(false)

	// Tear down the WireGuard interface
	if err := t.teardownInterface(); err != nil {
		t.logger.Warn("failed to tear down wireguard interface", zap.Error(err))
		return err
	}

	return nil
}

// IsHealthy returns whether the WireGuard tunnel is connected.
func (t *wireGuardTunnel) IsHealthy() bool {
	return t.healthy.Load()
}

// LocalAddr returns the local tunnel address.
func (t *wireGuardTunnel) LocalAddr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.localAddr
}

// Type returns the tunnel type identifier.
func (t *wireGuardTunnel) Type() string {
	return "wireguard"
}

// maintainConnection keeps the WireGuard tunnel connected with exponential backoff.
func (t *wireGuardTunnel) maintainConnection(ctx context.Context) {
	defer close(t.done)

	backoff := time.Second
	for {
		if err := t.connect(); err != nil {
			t.logger.Error("wireguard connection failed", zap.Error(err), zap.Duration("backoff", backoff))
			t.healthy.Store(false)

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			backoff = minDuration(backoff*2, maxBackoff)
			continue
		}

		t.healthy.Store(true)
		backoff = time.Second
		t.logger.Info("wireguard tunnel established", zap.String("interface", t.ifaceName))

		// Monitor the connection until it fails or context is cancelled
		if err := t.monitorConnection(ctx); err != nil {
			t.logger.Warn("wireguard connection lost", zap.Error(err))
			t.healthy.Store(false)
		}

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// connect sets up the WireGuard interface and configures peering.
func (t *wireGuardTunnel) connect() error {
	wgConfig := t.config.WireGuard

	// Create WireGuard interface
	if err := t.runCommand("ip", "link", "add", t.ifaceName, "type", "wireguard"); err != nil {
		// Interface may already exist from a previous attempt
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("creating wireguard interface: %w", err)
		}
	}

	// Configure WireGuard peer
	args := []string{"set", t.ifaceName}

	if wgConfig.Endpoint != "" {
		args = append(args, "peer", wgConfig.PublicKey, "endpoint", wgConfig.Endpoint)
	} else if wgConfig.PublicKey != "" {
		args = append(args, "peer", wgConfig.PublicKey)
	}

	if len(wgConfig.AllowedIPs) > 0 {
		args = append(args, "allowed-ips", strings.Join(wgConfig.AllowedIPs, ","))
	}

	keepalive := int32(wireguardKeepalive)
	if wgConfig.PersistentKeepalive != nil {
		keepalive = *wgConfig.PersistentKeepalive
	}
	args = append(args, "persistent-keepalive", fmt.Sprintf("%d", keepalive))

	if err := t.runCommand("wg", args...); err != nil {
		return fmt.Errorf("configuring wireguard peer: %w", err)
	}

	// Bring up the interface
	if err := t.runCommand("ip", "link", "set", t.ifaceName, "up"); err != nil {
		return fmt.Errorf("bringing up wireguard interface: %w", err)
	}

	// Set the local address based on tunnel config
	t.mu.Lock()
	t.localAddr = fmt.Sprintf("%s:15002", t.ifaceName)
	t.mu.Unlock()

	return nil
}

// monitorConnection checks the WireGuard handshake periodically.
func (t *wireGuardTunnel) monitorConnection(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := t.checkHandshake(); err != nil {
				return err
			}
		}
	}
}

// checkHandshake verifies the WireGuard handshake is recent enough.
func (t *wireGuardTunnel) checkHandshake() error {
	output, err := t.runCommandOutput("wg", "show", t.ifaceName, "latest-handshakes")
	if err != nil {
		return fmt.Errorf("checking wireguard handshake: %w", err)
	}

	// A handshake timestamp of 0 means no handshake has occurred
	if strings.TrimSpace(output) == "" || strings.Contains(output, "\t0\n") {
		return fmt.Errorf("no wireguard handshake established")
	}

	return nil
}

// teardownInterface removes the WireGuard interface.
func (t *wireGuardTunnel) teardownInterface() error {
	return t.runCommand("ip", "link", "del", t.ifaceName)
}

// runCommand executes a system command and returns any error.
func (t *wireGuardTunnel) runCommand(name string, args ...string) error {
	t.logger.Debug("executing command", zap.String("cmd", name), zap.Strings("args", args))

	cmd := exec.Command(name, args...) //nolint:gosec // CLI tools are required for WireGuard setup
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w (stderr: %s)", name, strings.Join(args, " "), err, stderr.String())
	}

	return nil
}

// runCommandOutput executes a system command and returns its stdout.
func (t *wireGuardTunnel) runCommandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec // CLI tools are required for WireGuard setup
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w (stderr: %s)", name, strings.Join(args, " "), err, stderr.String())
	}

	return stdout.String(), nil
}

// sanitizeInterfaceName converts a cluster name to a valid interface name suffix.
func sanitizeInterfaceName(name string) string {
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(name))

	return strings.Trim(result, "-")
}

// minDuration returns the smaller of two durations.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}

	return b
}
