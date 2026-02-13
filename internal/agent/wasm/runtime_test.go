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

package wasm

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

// minimalWASM is the smallest valid WASM module (magic + version + empty sections).
var minimalWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic "\0asm"
	0x01, 0x00, 0x00, 0x00, // version 1
}

func TestNewRuntime(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	if rt == nil {
		t.Fatal("runtime should not be nil")
	}
}

func TestRuntime_LoadPlugin_NilConfig(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	err = rt.LoadPlugin(ctx, nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestRuntime_LoadPlugin_EmptyName(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	err = rt.LoadPlugin(ctx, &PluginConfig{})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestRuntime_LoadPlugin_EmptyBytes(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	err = rt.LoadPlugin(ctx, &PluginConfig{Name: "test"})
	if err == nil {
		t.Error("expected error for empty WASM bytes")
	}
}

func TestRuntime_LoadPlugin_MinimalWASM(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	err = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "minimal",
		WASMBytes: minimalWASM,
		Phase:     PhaseRequest,
		Priority:  100,
	})
	if err != nil {
		t.Fatalf("failed to load minimal WASM: %v", err)
	}

	// Verify plugin is loaded
	p, ok := rt.GetPlugin("minimal")
	if !ok {
		t.Fatal("plugin should be loaded")
	}
	if p.Name() != "minimal" {
		t.Errorf("expected name 'minimal', got %q", p.Name())
	}
	if p.Phase() != PhaseRequest {
		t.Errorf("expected PhaseRequest, got %v", p.Phase())
	}
}

func TestRuntime_ListPlugins(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	names := rt.ListPlugins()
	if len(names) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(names))
	}

	_ = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "plugin1",
		WASMBytes: minimalWASM,
		Phase:     PhaseRequest,
	})
	_ = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "plugin2",
		WASMBytes: minimalWASM,
		Phase:     PhaseResponse,
	})

	names = rt.ListPlugins()
	if len(names) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(names))
	}
}

func TestRuntime_UnloadPlugin(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	_ = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "test",
		WASMBytes: minimalWASM,
		Phase:     PhaseRequest,
	})

	err = rt.UnloadPlugin(ctx, "test")
	if err != nil {
		t.Fatalf("failed to unload plugin: %v", err)
	}

	_, ok := rt.GetPlugin("test")
	if ok {
		t.Error("plugin should have been unloaded")
	}
}

func TestRuntime_UnloadPlugin_NotFound(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	err = rt.UnloadPlugin(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestRuntime_ReplacePlugin(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	// Load plugin with PhaseRequest
	_ = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "test",
		WASMBytes: minimalWASM,
		Phase:     PhaseRequest,
	})

	// Replace with PhaseResponse
	err = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "test",
		WASMBytes: minimalWASM,
		Phase:     PhaseResponse,
	})
	if err != nil {
		t.Fatalf("failed to replace plugin: %v", err)
	}

	p, ok := rt.GetPlugin("test")
	if !ok {
		t.Fatal("plugin should exist")
	}
	if p.Phase() != PhaseResponse {
		t.Errorf("expected PhaseResponse after replace, got %v", p.Phase())
	}
}

func TestRuntime_Close(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}

	_ = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "test",
		WASMBytes: minimalWASM,
		Phase:     PhaseRequest,
	})

	err = rt.Close(ctx)
	if err != nil {
		t.Fatalf("failed to close runtime: %v", err)
	}

	// Loading after close should fail
	err = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "test2",
		WASMBytes: minimalWASM,
		Phase:     PhaseRequest,
	})
	if err == nil {
		t.Error("expected error when loading after close")
	}
}

func TestPhase_String(t *testing.T) {
	tests := []struct {
		phase    Phase
		expected string
	}{
		{PhaseRequest, "request"},
		{PhaseResponse, "response"},
		{PhaseBoth, "both"},
		{Phase(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.expected {
			t.Errorf("Phase(%d).String() = %q, want %q", tt.phase, got, tt.expected)
		}
	}
}

func TestParsePhase(t *testing.T) {
	tests := []struct {
		input    string
		expected Phase
	}{
		{"request", PhaseRequest},
		{"response", PhaseResponse},
		{"both", PhaseBoth},
		{"unknown", PhaseRequest},
	}
	for _, tt := range tests {
		if got := ParsePhase(tt.input); got != tt.expected {
			t.Errorf("ParsePhase(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestInstancePool_GetPut(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	err = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "test",
		WASMBytes: minimalWASM,
		Phase:     PhaseRequest,
	})
	if err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	plugin, ok := rt.GetPlugin("test")
	if !ok {
		t.Fatal("plugin should be loaded")
	}

	// Get an instance
	inst, err := plugin.pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}

	// Put it back
	plugin.pool.Put(inst)

	// Pool should have 1 instance
	if plugin.pool.Size() != 1 {
		t.Errorf("expected pool size 1, got %d", plugin.pool.Size())
	}
}

func TestPlugin_ExecuteRequestPhase_NoExport(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	// The minimal WASM module has no exports, so ExecuteRequestPhase should
	// return ActionContinue without error (guest doesn't handle this phase)
	err = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "noop",
		WASMBytes: minimalWASM,
		Phase:     PhaseRequest,
	})
	if err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	plugin, _ := rt.GetPlugin("noop")
	reqCtx := &RequestContext{
		Action: ActionContinue,
	}

	action, err := plugin.ExecuteRequestPhase(ctx, reqCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != ActionContinue {
		t.Errorf("expected ActionContinue, got %v", action)
	}
}

func TestRuntime_MemoryLimitPages(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	// Runtime should have been created with memory limits
	// Verify by loading a minimal module (no memory section, so won't hit limit)
	err = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "memlimit",
		WASMBytes: minimalWASM,
		Phase:     PhaseRequest,
	})
	if err != nil {
		t.Fatalf("failed to load plugin with memory limits: %v", err)
	}
}

func TestPlugin_ExecutionTimeout_Default(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	err = rt.LoadPlugin(ctx, &PluginConfig{
		Name:      "timeout-test",
		WASMBytes: minimalWASM,
		Phase:     PhaseRequest,
	})
	if err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	plugin, _ := rt.GetPlugin("timeout-test")

	// Default timeout should be 5 seconds
	timeout := plugin.executionTimeout()
	if timeout != 5*time.Second {
		t.Errorf("expected default timeout 5s, got %v", timeout)
	}
}

func TestPlugin_ExecutionTimeout_Custom(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	err = rt.LoadPlugin(ctx, &PluginConfig{
		Name:             "custom-timeout",
		WASMBytes:        minimalWASM,
		Phase:            PhaseRequest,
		ExecutionTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	plugin, _ := rt.GetPlugin("custom-timeout")
	timeout := plugin.executionTimeout()
	if timeout != 2*time.Second {
		t.Errorf("expected custom timeout 2s, got %v", timeout)
	}
}

func TestPlugin_ExecuteWithTimeout_NoExport(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	rt, err := NewRuntime(ctx, logger)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	err = rt.LoadPlugin(ctx, &PluginConfig{
		Name:             "timeout-noop",
		WASMBytes:        minimalWASM,
		Phase:            PhaseRequest,
		ExecutionTimeout: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	plugin, _ := rt.GetPlugin("timeout-noop")
	reqCtx := &RequestContext{Action: ActionContinue}

	// Should complete quickly since no export exists
	action, err := plugin.ExecuteRequestPhase(ctx, reqCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != ActionContinue {
		t.Errorf("expected ActionContinue, got %v", action)
	}
}
