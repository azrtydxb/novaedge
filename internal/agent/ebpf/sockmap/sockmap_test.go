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

package sockmap

const osLinux = osLinux

import (
	"net"
	"runtime"
	"testing"

	"github.com/piwi3910/novaedge/internal/agent/ebpf/testutil"
	"go.uber.org/zap/zaptest"
)

func TestNewEndpointKey(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		port    uint16
		wantErr bool
	}{
		{name: "valid IPv4", ip: "10.244.0.5", port: 8080},
		{name: "valid loopback", ip: "127.0.0.1", port: 80},
		{name: "invalid IP", ip: "not-an-ip", port: 80, wantErr: true},
		{name: "IPv6", ip: "::1", port: 80, wantErr: true},
		{name: "empty string", ip: "", port: 80, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := NewEndpointKey(tt.ip, tt.port)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key.Addr == [4]byte{} {
				t.Error("expected non-zero address")
			}
			if key.Port != tt.port {
				t.Errorf("port: got %d, want %d", key.Port, tt.port)
			}
		})
	}
}

func TestNewSockMapManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr, err := NewSockMapManager(logger)

	if runtime.GOOS != osLinux {
		// On non-Linux, expect an error.
		if err == nil {
			t.Fatal("expected error on non-Linux platform")
		}
		return
	}

	// On Linux, the manager may or may not succeed depending on
	// kernel capabilities and permissions.
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewSockMapManager() returned error: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestManagerCloseIdempotent(t *testing.T) {
	if runtime.GOOS != osLinux {
		t.Skip("SOCKMAP is Linux-only")
	}

	logger := zaptest.NewLogger(t)
	mgr, err := NewSockMapManager(logger)
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewSockMapManager() returned error: %v", err)
	}

	// First close should succeed.
	if err := mgr.Close(); err != nil {
		t.Fatalf("first Close() returned error: %v", err)
	}

	// Second close should be a no-op.
	if err := mgr.Close(); err != nil {
		t.Fatalf("second Close() returned error: %v", err)
	}
}

func TestManagerAddRemoveEndpoint(t *testing.T) {
	if runtime.GOOS != osLinux {
		t.Skip("SOCKMAP is Linux-only")
	}

	logger := zaptest.NewLogger(t)
	mgr, err := NewSockMapManager(logger)
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewSockMapManager() returned error: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	ip := net.ParseIP("10.244.0.5")
	if ip == nil {
		t.Fatal("failed to parse test IP")
	}

	// Add endpoint.
	if err := mgr.AddSameNodeEndpoint(ip, 8080); err != nil {
		t.Fatalf("AddSameNodeEndpoint() error: %v", err)
	}

	// Remove endpoint.
	if err := mgr.RemoveSameNodeEndpoint(ip, 8080); err != nil {
		t.Fatalf("RemoveSameNodeEndpoint() error: %v", err)
	}

	// Remove non-existent endpoint should fail gracefully.
	err = mgr.RemoveSameNodeEndpoint(ip, 9999)
	if err == nil {
		t.Log("RemoveSameNodeEndpoint for non-existent key returned nil (map may allow it)")
	}
}

func TestManagerSyncEndpoints(t *testing.T) {
	if runtime.GOOS != osLinux {
		t.Skip("SOCKMAP is Linux-only")
	}

	logger := zaptest.NewLogger(t)
	mgr, err := NewSockMapManager(logger)
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewSockMapManager() returned error: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	// Build desired state.
	desired := map[EndpointKey]EndpointValue{
		{Addr: [4]byte{10, 244, 0, 5}, Port: 8080}: {Eligible: 1},
		{Addr: [4]byte{10, 244, 0, 6}, Port: 9090}: {Eligible: 1},
	}

	if err := mgr.SyncEndpoints(desired); err != nil {
		t.Fatalf("SyncEndpoints() error: %v", err)
	}

	// Sync with smaller set should remove the stale entry.
	smaller := map[EndpointKey]EndpointValue{
		{Addr: [4]byte{10, 244, 0, 5}, Port: 8080}: {Eligible: 1},
	}
	if err := mgr.SyncEndpoints(smaller); err != nil {
		t.Fatalf("SyncEndpoints() with smaller set error: %v", err)
	}
}

func TestManagerGetStats(t *testing.T) {
	if runtime.GOOS != osLinux {
		t.Skip("SOCKMAP is Linux-only")
	}

	logger := zaptest.NewLogger(t)
	mgr, err := NewSockMapManager(logger)
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewSockMapManager() returned error: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	redirected, fallback, err := mgr.GetStats()
	if err != nil {
		t.Fatalf("GetStats() error: %v", err)
	}

	// Fresh maps should have zero counters.
	if redirected != 0 {
		t.Errorf("expected redirected=0, got %d", redirected)
	}
	if fallback != 0 {
		t.Errorf("expected fallback=0, got %d", fallback)
	}
}

func TestManagerOperationsAfterClose(t *testing.T) {
	if runtime.GOOS != osLinux {
		t.Skip("SOCKMAP is Linux-only")
	}

	logger := zaptest.NewLogger(t)
	mgr, err := NewSockMapManager(logger)
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewSockMapManager() returned error: %v", err)
	}

	_ = mgr.Close()

	ip := net.ParseIP("10.244.0.5")
	if err := mgr.AddSameNodeEndpoint(ip, 8080); err == nil {
		t.Error("expected error after Close()")
	}
	if err := mgr.RemoveSameNodeEndpoint(ip, 8080); err == nil {
		t.Error("expected error after Close()")
	}
	if _, _, err := mgr.GetStats(); err == nil {
		t.Error("expected error after Close()")
	}
}

func TestManagerAddInvalidIP(t *testing.T) {
	if runtime.GOOS != osLinux {
		t.Skip("SOCKMAP is Linux-only")
	}

	logger := zaptest.NewLogger(t)
	mgr, err := NewSockMapManager(logger)
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewSockMapManager() returned error: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	// IPv6 address should fail.
	ip6 := net.ParseIP("::1")
	if err := mgr.AddSameNodeEndpoint(ip6, 80); err == nil {
		t.Error("expected error for IPv6 address")
	}

	// Nil IP should fail.
	if err := mgr.AddSameNodeEndpoint(nil, 80); err == nil {
		t.Error("expected error for nil IP")
	}
}

func TestStatsKeyConstants(t *testing.T) {
	if StatsKeyRedirected != 0 {
		t.Errorf("StatsKeyRedirected: got %d, want 0", StatsKeyRedirected)
	}
	if StatsKeyFallback != 1 {
		t.Errorf("StatsKeyFallback: got %d, want 1", StatsKeyFallback)
	}
}
