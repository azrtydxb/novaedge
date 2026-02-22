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

package ebpfmesh

import (
	"testing"

	novaebpf "github.com/piwi3910/novaedge/internal/agent/ebpf"
	"github.com/piwi3910/novaedge/internal/agent/mesh"
	"go.uber.org/zap/zaptest"
)

func TestBackendName(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	b := NewBackend(logger, loader)
	if got := b.Name(); got != "ebpf-sk-lookup" {
		t.Errorf("Name() = %q, want %q", got, "ebpf-sk-lookup")
	}
}

func TestBackendImplementsRuleBackend(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	b := NewBackend(logger, loader)

	// Verify at compile time that Backend implements mesh.RuleBackend.
	var _ mesh.RuleBackend = b
}

func TestBackendSetupWithoutBPFObjects(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	b := NewBackend(logger, loader)

	// Setup should fail because bpf2go objects aren't generated on this
	// platform (or in CI without clang).
	err := b.Setup()
	if err == nil {
		// If Setup somehow succeeds (e.g. on a Linux box with BPF objects
		// already generated), clean up.
		b.Cleanup()
		return
	}
	// Expected: error about BPF objects not generated or Linux-only.
	t.Logf("Expected Setup error: %v", err)
}

func TestBackendApplyRulesWithoutSetup(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	b := NewBackend(logger, loader)

	targets := []mesh.InterceptTarget{
		{ClusterIP: "10.96.0.1", Port: 80},
	}
	err := b.ApplyRules(targets, 15001)
	if err == nil {
		t.Error("expected error calling ApplyRules without Setup")
	}
}

func TestBackendCleanupIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	b := NewBackend(logger, loader)

	// Cleanup on a fresh backend should be safe (no-op).
	if err := b.Cleanup(); err != nil {
		t.Errorf("Cleanup() on fresh backend returned error: %v", err)
	}

	// Double cleanup should also be safe.
	if err := b.Cleanup(); err != nil {
		t.Errorf("second Cleanup() returned error: %v", err)
	}
}

func TestMakeServiceKey(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		port    int32
		wantErr bool
	}{
		{name: "valid", ip: "10.96.0.1", port: 80},
		{name: "valid high port", ip: "10.96.255.255", port: 443},
		{name: "invalid ip", ip: "not-an-ip", port: 80, wantErr: true},
		{name: "ipv6", ip: "::1", port: 80, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := makeServiceKey(tt.ip, tt.port)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key.Addr == [4]byte{} {
				t.Error("expected non-zero address")
			}
			if key.Port == 0 {
				t.Error("expected non-zero port")
			}
		})
	}
}

func TestHtons(t *testing.T) {
	// htons should convert host to network byte order.
	result := htons(80)
	if result == 0 {
		t.Error("htons(80) returned 0")
	}
}
