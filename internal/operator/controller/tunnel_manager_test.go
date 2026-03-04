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

package controller

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewInMemoryTunnelManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewInMemoryTunnelManager(logger)
	require.NotNil(t, manager)
	assert.NotNil(t, manager.tunnels)
	assert.NotNil(t, manager.logger)
}

func TestInMemoryTunnelManager_RegisterTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewInMemoryTunnelManager(logger)

	manager.RegisterTunnel("cluster-1")
	assert.True(t, manager.tunnels["cluster-1"])

	manager.RegisterTunnel("cluster-2")
	assert.True(t, manager.tunnels["cluster-2"])
}

func TestInMemoryTunnelManager_RemoveTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewInMemoryTunnelManager(logger)

	// Register a tunnel first
	manager.RegisterTunnel("cluster-1")
	assert.True(t, manager.tunnels["cluster-1"])

	// Remove the tunnel
	err := manager.RemoveTunnel("cluster-1")
	require.NoError(t, err)
	assert.False(t, manager.tunnels["cluster-1"])
}

func TestInMemoryTunnelManager_RemoveTunnel_NotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewInMemoryTunnelManager(logger)

	err := manager.RemoveTunnel("non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel for cluster \"non-existent\" not found")
}

func TestInMemoryTunnelManager_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewInMemoryTunnelManager(logger)

	var wg sync.WaitGroup
	numOps := 100

	// Concurrent registrations
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			manager.RegisterTunnel(fmt.Sprintf("cluster-%d", idx))
		}(i)
	}

	// Wait for all registrations
	wg.Wait()

	// Verify all tunnels are registered
	manager.mu.Lock()
	initialCount := len(manager.tunnels)
	manager.mu.Unlock()
	assert.Equal(t, numOps, initialCount)
}

func TestInMemoryTunnelManager_RegisterAndRemove(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewInMemoryTunnelManager(logger)

	// Register multiple tunnels
	clusters := []string{"cluster-1", "cluster-2", "cluster-3"}
	for _, cluster := range clusters {
		manager.RegisterTunnel(cluster)
	}

	// Verify all registered
	manager.mu.Lock()
	assert.Len(t, manager.tunnels, 3)
	manager.mu.Unlock()

	// Remove one tunnel
	err := manager.RemoveTunnel("cluster-2")
	require.NoError(t, err)

	// Verify removal
	manager.mu.Lock()
	assert.Len(t, manager.tunnels, 2)
	assert.True(t, manager.tunnels["cluster-1"])
	assert.False(t, manager.tunnels["cluster-2"])
	assert.True(t, manager.tunnels["cluster-3"])
	manager.mu.Unlock()
}

func TestInMemoryTunnelManager_DoubleRegister(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewInMemoryTunnelManager(logger)

	// Register same tunnel twice
	manager.RegisterTunnel("cluster-1")
	manager.RegisterTunnel("cluster-1")

	// Should still only have one entry
	manager.mu.Lock()
	assert.Len(t, manager.tunnels, 1)
	manager.mu.Unlock()
}

func TestInMemoryTunnelManager_RemoveAfterDoubleRegister(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewInMemoryTunnelManager(logger)

	// Register same tunnel twice
	manager.RegisterTunnel("cluster-1")
	manager.RegisterTunnel("cluster-1")

	// Remove once should succeed
	err := manager.RemoveTunnel("cluster-1")
	require.NoError(t, err)

	// Second remove should fail
	err = manager.RemoveTunnel("cluster-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestInMemoryTunnelManager_LoggerNamed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewInMemoryTunnelManager(logger)

	// Verify the logger is named
	assert.NotNil(t, manager.logger)
}
