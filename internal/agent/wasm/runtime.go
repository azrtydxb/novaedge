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
	"errors"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"go.uber.org/zap"
)

var (
	errPluginConfigIsNil          = errors.New("plugin config is nil")
	errPluginNameIsRequired       = errors.New("plugin name is required")
	errWASMBytesAreEmptyForPlugin = errors.New("WASM bytes are empty for plugin")
	errRuntimeIsClosed            = errors.New("runtime is closed")
	errPlugin                     = errors.New("plugin")
)

// Runtime manages the wazero WASM runtime and all loaded plugins.
type Runtime struct {
	mu      sync.RWMutex
	logger  *zap.Logger
	runtime wazero.Runtime
	plugins map[string]*Plugin // name -> plugin
	closed  bool
}

// NewRuntime creates a new WASM runtime.
func NewRuntime(ctx context.Context, logger *zap.Logger) (*Runtime, error) {
	cfg := wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(512) // 32MB max per module (512 * 64KB)

	rt := wazero.NewRuntimeWithConfig(ctx, cfg)

	r := &Runtime{
		logger:  logger,
		runtime: rt,
		plugins: make(map[string]*Plugin),
	}

	// Instantiate host module with ABI functions
	if err := r.registerHostModule(ctx); err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("failed to register host module: %w", err)
	}

	logger.Info("WASM runtime initialized")
	return r, nil
}

// LoadPlugin compiles a WASM module, creates an instance pool, and registers
// the plugin under the given name. If a plugin with the same name already
// exists it is unloaded first.
func (r *Runtime) LoadPlugin(ctx context.Context, cfg *PluginConfig) error {
	if cfg == nil {
		return errPluginConfigIsNil
	}
	if cfg.Name == "" {
		return errPluginNameIsRequired
	}
	if len(cfg.WASMBytes) == 0 {
		return fmt.Errorf("%w: %s", errWASMBytesAreEmptyForPlugin, cfg.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return errRuntimeIsClosed
	}

	// Unload existing plugin with the same name
	if existing, ok := r.plugins[cfg.Name]; ok {
		r.logger.Info("Replacing existing plugin", zap.String("plugin", cfg.Name))
		existing.Close(ctx)
		delete(r.plugins, cfg.Name)
	}

	// Compile module
	compiled, err := r.runtime.CompileModule(ctx, cfg.WASMBytes)
	if err != nil {
		return fmt.Errorf("failed to compile WASM module %s: %w", cfg.Name, err)
	}

	plugin := NewPlugin(cfg, compiled, r.runtime, r.logger)
	r.plugins[cfg.Name] = plugin
	SetPluginsLoaded(len(r.plugins))
	SetInstancePoolSize(cfg.Name, plugin.pool.Size())

	r.logger.Info("Loaded WASM plugin",
		zap.String("plugin", cfg.Name),
		zap.String("phase", cfg.Phase.String()),
		zap.Int("priority", cfg.Priority),
	)
	return nil
}

// UnloadPlugin removes and closes a plugin by name.
func (r *Runtime) UnloadPlugin(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	plugin, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("%w: %s not found", errPlugin, name)
	}

	plugin.Close(ctx)
	delete(r.plugins, name)
	SetPluginsLoaded(len(r.plugins))

	r.logger.Info("Unloaded WASM plugin", zap.String("plugin", name))
	return nil
}

// GetPlugin returns a loaded plugin by name.
func (r *Runtime) GetPlugin(name string) (*Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

// ListPlugins returns the names of all loaded plugins.
func (r *Runtime) ListPlugins() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}

// Close shuts down the runtime and all plugins.
func (r *Runtime) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	for name, plugin := range r.plugins {
		plugin.Close(ctx)
		delete(r.plugins, name)
	}
	SetPluginsLoaded(0)

	r.logger.Info("WASM runtime closed")
	return r.runtime.Close(ctx)
}
