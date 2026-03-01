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
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

var (
	errTunnelForCluster = errors.New("tunnel for cluster")
)

// InMemoryTunnelManager tracks tunnel names and provides cleanup during
// remote cluster deletion. It implements TunnelTeardown without depending
// on Linux-specific networking packages (netlink, wgctrl), making it safe
// for use in the operator which runs as a standard Deployment.
type InMemoryTunnelManager struct {
	mu      sync.Mutex
	tunnels map[string]bool
	logger  *zap.Logger
}

// NewInMemoryTunnelManager creates a new InMemoryTunnelManager.
func NewInMemoryTunnelManager(logger *zap.Logger) *InMemoryTunnelManager {
	return &InMemoryTunnelManager{
		tunnels: make(map[string]bool),
		logger:  logger.Named("tunnel-manager"),
	}
}

// RegisterTunnel records a tunnel name so it can be cleaned up later.
func (m *InMemoryTunnelManager) RegisterTunnel(clusterName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tunnels[clusterName] = true
	m.logger.Info("Tunnel registered", zap.String("cluster", clusterName))
}

// RemoveTunnel removes the tunnel record for the given cluster name.
// It satisfies the TunnelTeardown interface.
func (m *InMemoryTunnelManager) RemoveTunnel(clusterName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.tunnels[clusterName] {
		return fmt.Errorf("%w %q not found", errTunnelForCluster, clusterName)
	}

	delete(m.tunnels, clusterName)
	m.logger.Info("Tunnel removed", zap.String("cluster", clusterName))
	return nil
}
