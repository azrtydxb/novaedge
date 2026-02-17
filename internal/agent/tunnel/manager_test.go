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
	"context"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	v1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// mockTunnel implements the Tunnel interface for testing.
type mockTunnel struct {
	tunnelType string
	localAddr  string
	healthy    atomic.Bool
	started    atomic.Bool
	stopped    atomic.Bool
}

func newMockTunnel(tunnelType, localAddr string, healthy bool) *mockTunnel {
	m := &mockTunnel{
		tunnelType: tunnelType,
		localAddr:  localAddr,
	}
	m.healthy.Store(healthy)

	return m
}

func (m *mockTunnel) Start(_ context.Context) error {
	m.started.Store(true)
	return nil
}

func (m *mockTunnel) Stop() error {
	m.stopped.Store(true)
	m.healthy.Store(false)
	return nil
}

func (m *mockTunnel) IsHealthy() bool {
	return m.healthy.Load()
}

func (m *mockTunnel) LocalAddr() string {
	return m.localAddr
}

func (m *mockTunnel) Type() string {
	return m.tunnelType
}

func TestNewNetworkTunnelManager(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewNetworkTunnelManager(logger)

	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}

	if mgr.tunnels == nil {
		t.Fatal("expected non-nil tunnels map")
	}

	if mgr.logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestManagerStartStop(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewNetworkTunnelManager(logger)

	ctx := context.Background()
	mgr.Start(ctx)

	if mgr.ctx == nil {
		t.Fatal("expected context to be set after Start")
	}

	if mgr.cancel == nil {
		t.Fatal("expected cancel to be set after Start")
	}

	mgr.Stop()
}

func TestManagerGetTunnel(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewNetworkTunnelManager(logger)

	ctx := context.Background()
	mgr.Start(ctx)
	defer mgr.Stop()

	// Test getting non-existent tunnel
	_, ok := mgr.GetTunnel("nonexistent")
	if ok {
		t.Fatal("expected GetTunnel to return false for nonexistent cluster")
	}

	// Add a mock tunnel directly
	mock := newMockTunnel("ssh", "127.0.0.1:12345", true)
	mgr.mu.Lock()
	mgr.tunnels["test-cluster"] = mock
	mgr.mu.Unlock()

	// Test getting existing tunnel
	tun, ok := mgr.GetTunnel("test-cluster")
	if !ok {
		t.Fatal("expected GetTunnel to return true for existing cluster")
	}

	if tun.Type() != "ssh" {
		t.Fatalf("expected tunnel type 'ssh', got %q", tun.Type())
	}

	if tun.LocalAddr() != "127.0.0.1:12345" {
		t.Fatalf("expected local addr '127.0.0.1:12345', got %q", tun.LocalAddr())
	}
}

func TestManagerRemoveTunnel(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewNetworkTunnelManager(logger)

	ctx := context.Background()
	mgr.Start(ctx)
	defer mgr.Stop()

	// Test removing non-existent tunnel
	err := mgr.RemoveTunnel("nonexistent")
	if err == nil {
		t.Fatal("expected error when removing nonexistent tunnel")
	}

	// Add and remove a mock tunnel
	mock := newMockTunnel("wireguard", "10.0.0.1:15002", true)
	mgr.mu.Lock()
	mgr.tunnels["wg-cluster"] = mock
	mgr.mu.Unlock()

	err = mgr.RemoveTunnel("wg-cluster")
	if err != nil {
		t.Fatalf("unexpected error removing tunnel: %v", err)
	}

	if !mock.stopped.Load() {
		t.Fatal("expected tunnel to be stopped after removal")
	}

	_, ok := mgr.GetTunnel("wg-cluster")
	if ok {
		t.Fatal("expected tunnel to be removed from map")
	}
}

func TestManagerHealthCheck(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewNetworkTunnelManager(logger)

	ctx := context.Background()
	mgr.Start(ctx)
	defer mgr.Stop()

	// Empty health check
	status := mgr.HealthCheck()
	if len(status) != 0 {
		t.Fatalf("expected empty health check, got %d entries", len(status))
	}

	// Add tunnels with mixed health
	healthyTunnel := newMockTunnel("ssh", "127.0.0.1:1111", true)
	unhealthyTunnel := newMockTunnel("websocket", "127.0.0.1:2222", false)

	mgr.mu.Lock()
	mgr.tunnels["healthy-cluster"] = healthyTunnel
	mgr.tunnels["unhealthy-cluster"] = unhealthyTunnel
	mgr.mu.Unlock()

	status = mgr.HealthCheck()
	if len(status) != 2 {
		t.Fatalf("expected 2 health check entries, got %d", len(status))
	}

	if !status["healthy-cluster"] {
		t.Fatal("expected healthy-cluster to be healthy")
	}

	if status["unhealthy-cluster"] {
		t.Fatal("expected unhealthy-cluster to be unhealthy")
	}
}

func TestManagerStopStopsAllTunnels(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewNetworkTunnelManager(logger)

	ctx := context.Background()
	mgr.Start(ctx)

	mock1 := newMockTunnel("ssh", "127.0.0.1:1111", true)
	mock2 := newMockTunnel("websocket", "127.0.0.1:2222", true)

	mgr.mu.Lock()
	mgr.tunnels["cluster-1"] = mock1
	mgr.tunnels["cluster-2"] = mock2
	mgr.mu.Unlock()

	mgr.Stop()

	if !mock1.stopped.Load() {
		t.Fatal("expected cluster-1 tunnel to be stopped")
	}

	if !mock2.stopped.Load() {
		t.Fatal("expected cluster-2 tunnel to be stopped")
	}

	// Tunnels map should be cleared
	if len(mgr.tunnels) != 0 {
		t.Fatalf("expected empty tunnels map after stop, got %d", len(mgr.tunnels))
	}
}

func TestManagerAddTunnelNotStarted(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewNetworkTunnelManager(logger)

	// Don't call Start - manager is not started
	err := mgr.AddTunnel(context.Background(), "test", v1alpha1.TunnelConfig{
		Type: v1alpha1.TunnelTypeSSH,
	})
	if err == nil {
		t.Fatal("expected error when adding tunnel to non-started manager")
	}
}

func TestManagerCreateTunnelUnsupportedType(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewNetworkTunnelManager(logger)

	ctx := context.Background()
	mgr.Start(ctx)
	defer mgr.Stop()

	err := mgr.AddTunnel(ctx, "test", v1alpha1.TunnelConfig{
		Type: "Unknown",
	})
	if err == nil {
		t.Fatal("expected error for unsupported tunnel type")
	}
}

func TestSanitizeInterfaceName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with-dash", "with-dash"},
		{"UPPER", "upper"},
		{"with.dots", "with-dots"},
		{"with_underscore", "with-underscore"},
		{"cluster-1", "cluster-1"},
	}

	for _, tt := range tests {
		result := sanitizeInterfaceName(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeInterfaceName(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestMinDuration(t *testing.T) {
	a := 5 * maxBackoff
	b := maxBackoff

	if result := minDuration(a, b); result != b {
		t.Errorf("minDuration(%v, %v) = %v, want %v", a, b, result, b)
	}

	if result := minDuration(b, a); result != b {
		t.Errorf("minDuration(%v, %v) = %v, want %v", b, a, result, b)
	}
}

func TestNewWireGuardTunnelRequiresConfig(t *testing.T) {
	logger := zap.NewNop()

	_, err := newWireGuardTunnel("test", v1alpha1.TunnelConfig{
		Type: v1alpha1.TunnelTypeWireGuard,
	}, logger)
	if err == nil {
		t.Fatal("expected error when WireGuard config is nil")
	}
}

func TestNewSSHTunnelRequiresEndpoint(t *testing.T) {
	logger := zap.NewNop()

	_, err := newSSHTunnel("test", v1alpha1.TunnelConfig{
		Type: v1alpha1.TunnelTypeSSH,
	}, logger)
	if err == nil {
		t.Fatal("expected error when relay endpoint is empty")
	}
}

func TestNewWebSocketTunnelRequiresEndpoint(t *testing.T) {
	logger := zap.NewNop()

	_, err := newWebSocketTunnel("test", v1alpha1.TunnelConfig{
		Type: v1alpha1.TunnelTypeWebSocket,
	}, logger)
	if err == nil {
		t.Fatal("expected error when relay endpoint is empty")
	}
}

func TestTunnelTypeStrings(t *testing.T) {
	logger := zap.NewNop()

	wg, err := newWireGuardTunnel("test", v1alpha1.TunnelConfig{
		Type:      v1alpha1.TunnelTypeWireGuard,
		WireGuard: &v1alpha1.WireGuardConfig{},
	}, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wg.Type() != "wireguard" {
		t.Errorf("expected type 'wireguard', got %q", wg.Type())
	}

	sshTun, err := newSSHTunnel("test", v1alpha1.TunnelConfig{
		Type:          v1alpha1.TunnelTypeSSH,
		RelayEndpoint: "relay.example.com:22",
	}, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sshTun.Type() != "ssh" {
		t.Errorf("expected type 'ssh', got %q", sshTun.Type())
	}

	wsTun, err := newWebSocketTunnel("test", v1alpha1.TunnelConfig{
		Type:          v1alpha1.TunnelTypeWebSocket,
		RelayEndpoint: "wss://relay.example.com/tunnel",
	}, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wsTun.Type() != "websocket" {
		t.Errorf("expected type 'websocket', got %q", wsTun.Type())
	}
}
