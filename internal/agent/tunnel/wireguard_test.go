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
	"testing"

	"go.uber.org/zap"

	v1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestNewWireGuardTunnelNilConfig(t *testing.T) {
	logger := zap.NewNop()

	_, err := newWireGuardTunnel("test", v1alpha1.TunnelConfig{
		Type: v1alpha1.TunnelTypeWireGuard,
	}, logger)
	if err == nil {
		t.Fatal("expected error when WireGuard config is nil")
	}
}

func TestNewWireGuardTunnelSuccess(t *testing.T) {
	logger := zap.NewNop()

	tun, err := newWireGuardTunnel("test-cluster", v1alpha1.TunnelConfig{
		Type:      v1alpha1.TunnelTypeWireGuard,
		WireGuard: &v1alpha1.WireGuardConfig{},
	}, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tun.Type() != "wireguard" {
		t.Fatalf("expected type 'wireguard', got %q", tun.Type())
	}

	if tun.ifaceName == "" {
		t.Fatal("expected non-empty interface name")
	}

	// Verify private key was generated (non-zero)
	zeroKey := [32]byte{}
	if tun.privateKey == zeroKey {
		t.Fatal("expected non-zero private key")
	}

	// OverlayAddr should be empty when no overlay is configured
	if tun.OverlayAddr() != "" {
		t.Fatalf("expected empty overlay addr, got %q", tun.OverlayAddr())
	}
}

func TestNewWireGuardTunnelWithOverlaySuccess(t *testing.T) {
	logger := zap.NewNop()

	tun, err := newWireGuardTunnelWithOverlay("site-a", v1alpha1.TunnelConfig{
		Type:      v1alpha1.TunnelTypeWireGuard,
		WireGuard: &v1alpha1.WireGuardConfig{},
	}, "10.200.1.1/24", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tun.overlayIP == nil {
		t.Fatal("expected overlay IP to be set")
	}

	if tun.overlayNet == nil {
		t.Fatal("expected overlay network to be set")
	}

	if tun.overlayCIDR != "10.200.1.1/24" {
		t.Fatalf("expected overlay CIDR '10.200.1.1/24', got %q", tun.overlayCIDR)
	}

	if tun.overlayIP.String() != "10.200.1.1" {
		t.Fatalf("expected overlay IP '10.200.1.1', got %q", tun.overlayIP.String())
	}
}

func TestNewWireGuardTunnelWithOverlayInvalidCIDR(t *testing.T) {
	logger := zap.NewNop()

	_, err := newWireGuardTunnelWithOverlay("site-a", v1alpha1.TunnelConfig{
		Type:      v1alpha1.TunnelTypeWireGuard,
		WireGuard: &v1alpha1.WireGuardConfig{},
	}, "not-a-cidr", logger)
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestNewWireGuardTunnelWithOverlayEmptyCIDR(t *testing.T) {
	logger := zap.NewNop()

	tun, err := newWireGuardTunnelWithOverlay("site-a", v1alpha1.TunnelConfig{
		Type:      v1alpha1.TunnelTypeWireGuard,
		WireGuard: &v1alpha1.WireGuardConfig{},
	}, "", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tun.overlayIP != nil {
		t.Fatal("expected nil overlay IP for empty CIDR")
	}
}

func TestWireGuardInterfaceNameSanitization(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		clusterName   string
		expectedIface string
	}{
		{"simple", "novaedge-wg-sim"},
		{"a", "novaedge-wg-a"},
		{"with-dash", "novaedge-wg-wit"},
		{"UPPER", "novaedge-wg-upp"},
		{"cluster-1", "novaedge-wg-clu"},
	}

	for _, tt := range tests {
		tun, err := newWireGuardTunnel(tt.clusterName, v1alpha1.TunnelConfig{
			Type:      v1alpha1.TunnelTypeWireGuard,
			WireGuard: &v1alpha1.WireGuardConfig{},
		}, logger)
		if err != nil {
			t.Fatalf("unexpected error for cluster %q: %v", tt.clusterName, err)
		}

		if len(tun.ifaceName) > 15 {
			t.Errorf("interface name %q exceeds 15 chars for cluster %q", tun.ifaceName, tt.clusterName)
		}
	}
}

func TestWireGuardTunnelIsHealthyDefault(t *testing.T) {
	logger := zap.NewNop()

	tun, err := newWireGuardTunnel("test", v1alpha1.TunnelConfig{
		Type:      v1alpha1.TunnelTypeWireGuard,
		WireGuard: &v1alpha1.WireGuardConfig{},
	}, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tun.IsHealthy() {
		t.Fatal("expected tunnel to not be healthy before Start")
	}
}
