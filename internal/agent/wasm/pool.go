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
	"errors"
	"context"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero/api"
	"go.uber.org/zap"
)
var (
	errInstancePoolIsClosed = errors.New("instance pool is closed")
)


const poolDefaultSize = 4

// Instance wraps a single instantiated WASM module.
type Instance struct {
	module api.Module
}

// InstancePool keeps a pool of pre-instantiated WASM module instances
// to avoid per-request instantiation overhead.
type InstancePool struct {
	mu     sync.Mutex
	plugin *Plugin
	pool   chan *Instance
	size   int
	closed bool
}

// NewInstancePool creates a new pool. Instances are lazily created on first Get.
func NewInstancePool(plugin *Plugin, size int) *InstancePool {
	if size <= 0 {
		size = poolDefaultSize
	}
	return &InstancePool{
		plugin: plugin,
		pool:   make(chan *Instance, size),
		size:   size,
	}
}

// Get returns an instance from the pool, creating one if the pool is empty.
func (p *InstancePool) Get(ctx context.Context) (*Instance, error) {
	// Try to get an existing instance without blocking
	select {
	case inst := <-p.pool:
		return inst, nil
	default:
	}

	// Create a new instance
	return p.createInstance(ctx)
}

// Put returns an instance to the pool.
func (p *InstancePool) Put(inst *Instance) {
	if inst == nil {
		return
	}

	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()

	if closed {
		// Pool is closed, close the instance
		_ = inst.module.Close(context.Background())
		return
	}

	// Return to pool or discard if full
	select {
	case p.pool <- inst:
	default:
		// Pool is full, close this instance
		_ = inst.module.Close(context.Background())
	}
}

// Close closes all pooled instances.
func (p *InstancePool) Close(ctx context.Context) {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	// Drain the pool
	for {
		select {
		case inst := <-p.pool:
			if inst != nil && inst.module != nil {
				_ = inst.module.Close(ctx)
			}
		default:
			return
		}
	}
}

// Size returns the current number of instances in the pool.
func (p *InstancePool) Size() int {
	return len(p.pool)
}

// createInstance instantiates a new WASM module from the plugin's compiled module.
func (p *InstancePool) createInstance(ctx context.Context) (*Instance, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errInstancePoolIsClosed
	}
	p.mu.Unlock()

	mod, err := p.plugin.instantiate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate WASM module: %w", err)
	}

	p.plugin.logger.Debug("Created new WASM instance",
		zap.String("plugin", p.plugin.config.Name),
	)

	return &Instance{module: mod}, nil
}
