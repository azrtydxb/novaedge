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

package service

import (
	"runtime"
	"testing"

	"github.com/piwi3910/novaedge/internal/agent/ebpf/testutil"
	"go.uber.org/zap/zaptest"
)

func TestNewServiceKey(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		port    uint16
		proto   uint8
		wantErr bool
	}{
		{name: "valid TCP", ip: "10.96.0.1", port: 80, proto: ProtoTCP},
		{name: "valid UDP", ip: "10.96.0.1", port: 53, proto: ProtoUDP},
		{name: "invalid IP", ip: "not-an-ip", port: 80, proto: ProtoTCP, wantErr: true},
		{name: "IPv6", ip: "::1", port: 80, proto: ProtoTCP, wantErr: true},
		{name: "empty string", ip: "", port: 80, proto: ProtoTCP, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := NewServiceKey(tt.ip, tt.port, tt.proto)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key.IP == [4]byte{} {
				t.Error("expected non-zero IP")
			}
			if key.Port != tt.port {
				t.Errorf("port: got %d, want %d", key.Port, tt.port)
			}
			if key.Proto != tt.proto {
				t.Errorf("proto: got %d, want %d", key.Proto, tt.proto)
			}
		})
	}
}

func TestNewBackendInfo(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		port      uint16
		weight    uint16
		healthy   bool
		nodeLocal bool
		wantErr   bool
	}{
		{name: "valid healthy local", ip: "10.244.0.5", port: 8080, weight: 100, healthy: true, nodeLocal: true},
		{name: "valid unhealthy remote", ip: "10.244.1.5", port: 9090, weight: 50, healthy: false, nodeLocal: false},
		{name: "invalid IP", ip: "bad", port: 80, weight: 1, healthy: true, nodeLocal: false, wantErr: true},
		{name: "IPv6", ip: "::1", port: 80, weight: 1, healthy: true, nodeLocal: false, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := NewBackendInfo(tt.ip, tt.port, tt.weight, tt.healthy, tt.nodeLocal)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.IP == [4]byte{} {
				t.Error("expected non-zero IP")
			}
			if info.Port != tt.port {
				t.Errorf("port: got %d, want %d", info.Port, tt.port)
			}
			if info.Weight != tt.weight {
				t.Errorf("weight: got %d, want %d", info.Weight, tt.weight)
			}
			if tt.healthy && info.Healthy != 1 {
				t.Error("expected Healthy=1")
			}
			if !tt.healthy && info.Healthy != 0 {
				t.Error("expected Healthy=0")
			}
			if tt.nodeLocal && info.NodeLocal != 1 {
				t.Error("expected NodeLocal=1")
			}
			if !tt.nodeLocal && info.NodeLocal != 0 {
				t.Error("expected NodeLocal=0")
			}
		})
	}
}

func TestFlagConstants(t *testing.T) {
	if FlagMeshEnabled != 1 {
		t.Errorf("FlagMeshEnabled: got %d, want 1", FlagMeshEnabled)
	}
	if FlagMTLSRequired != 2 {
		t.Errorf("FlagMTLSRequired: got %d, want 2", FlagMTLSRequired)
	}
}

func TestProtoConstants(t *testing.T) {
	if ProtoTCP != 6 {
		t.Errorf("ProtoTCP: got %d, want 6", ProtoTCP)
	}
	if ProtoUDP != 17 {
		t.Errorf("ProtoUDP: got %d, want 17", ProtoUDP)
	}
}

func TestNewServiceMap(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sm, err := NewServiceMap(logger, 100, 32)

	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected error on non-Linux platform")
		}
		return
	}

	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewServiceMap() returned error: %v", err)
	}
	defer sm.Close()
}

func TestServiceMapCloseIdempotent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF service maps are Linux-only")
	}

	logger := zaptest.NewLogger(t)
	sm, err := NewServiceMap(logger, 100, 32)
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewServiceMap() returned error: %v", err)
	}

	if err := sm.Close(); err != nil {
		t.Fatalf("first Close() returned error: %v", err)
	}
	if err := sm.Close(); err != nil {
		t.Fatalf("second Close() returned error: %v", err)
	}
}

func TestServiceMapUpsertAndDelete(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF service maps are Linux-only")
	}

	logger := zaptest.NewLogger(t)
	sm, err := NewServiceMap(logger, 100, 32)
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewServiceMap() returned error: %v", err)
	}
	defer sm.Close()

	key := ServiceKey{
		IP:    [4]byte{10, 96, 0, 1},
		Port:  80,
		Proto: ProtoTCP,
	}
	backends := []BackendInfo{
		{IP: [4]byte{10, 244, 0, 5}, Port: 8080, Weight: 100, Healthy: 1, NodeLocal: 1},
		{IP: [4]byte{10, 244, 1, 5}, Port: 8080, Weight: 100, Healthy: 1, NodeLocal: 0},
	}

	if err := sm.UpsertService(key, backends); err != nil {
		t.Fatalf("UpsertService() error: %v", err)
	}

	// Update with different backends.
	backends2 := []BackendInfo{
		{IP: [4]byte{10, 244, 0, 5}, Port: 8080, Weight: 100, Healthy: 1, NodeLocal: 1},
	}
	if err := sm.UpsertService(key, backends2); err != nil {
		t.Fatalf("UpsertService() update error: %v", err)
	}

	if err := sm.DeleteService(key); err != nil {
		t.Fatalf("DeleteService() error: %v", err)
	}
}

func TestServiceMapReconcile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF service maps are Linux-only")
	}

	logger := zaptest.NewLogger(t)
	sm, err := NewServiceMap(logger, 100, 32)
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewServiceMap() returned error: %v", err)
	}
	defer sm.Close()

	desired := map[ServiceKey][]BackendInfo{
		{IP: [4]byte{10, 96, 0, 1}, Port: 80, Proto: ProtoTCP}: {
			{IP: [4]byte{10, 244, 0, 5}, Port: 8080, Weight: 100, Healthy: 1},
		},
		{IP: [4]byte{10, 96, 0, 2}, Port: 443, Proto: ProtoTCP}: {
			{IP: [4]byte{10, 244, 0, 6}, Port: 8443, Weight: 100, Healthy: 1},
			{IP: [4]byte{10, 244, 1, 6}, Port: 8443, Weight: 100, Healthy: 1},
		},
	}

	if err := sm.Reconcile(desired); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	// Reconcile with smaller set should remove stale entries.
	smaller := map[ServiceKey][]BackendInfo{
		{IP: [4]byte{10, 96, 0, 1}, Port: 80, Proto: ProtoTCP}: {
			{IP: [4]byte{10, 244, 0, 5}, Port: 8080, Weight: 100, Healthy: 1},
		},
	}
	if err := sm.Reconcile(smaller); err != nil {
		t.Fatalf("Reconcile() with smaller set error: %v", err)
	}
}

func TestServiceMapOperationsAfterClose(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF service maps are Linux-only")
	}

	logger := zaptest.NewLogger(t)
	sm, err := NewServiceMap(logger, 100, 32)
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewServiceMap() returned error: %v", err)
	}

	sm.Close()

	key := ServiceKey{IP: [4]byte{10, 96, 0, 1}, Port: 80, Proto: ProtoTCP}
	if err := sm.UpsertService(key, nil); err == nil {
		t.Error("expected error after Close()")
	}
	if err := sm.DeleteService(key); err == nil {
		t.Error("expected error after Close()")
	}
	if err := sm.Reconcile(nil); err == nil {
		t.Error("expected error after Close()")
	}
}

func TestServiceMapDefaultParams(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF service maps are Linux-only")
	}

	logger := zaptest.NewLogger(t)
	// Pass 0 for both params to test defaults.
	sm, err := NewServiceMap(logger, 0, 0)
	testutil.SkipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewServiceMap() returned error: %v", err)
	}
	defer sm.Close()
}
