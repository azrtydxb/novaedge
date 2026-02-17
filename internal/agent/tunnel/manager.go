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
	"fmt"
	"sync"

	"go.uber.org/zap"

	v1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// NetworkTunnelManager manages network tunnels for remote cluster connectivity.
// It handles creation, lifecycle, and health monitoring of tunnels that provide
// L3/L4 connectivity for NAT/firewall traversal.
type NetworkTunnelManager struct {
	mu      sync.RWMutex
	tunnels map[string]Tunnel // clusterName -> tunnel
	logger  *zap.Logger
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewNetworkTunnelManager creates a new tunnel manager.
func NewNetworkTunnelManager(logger *zap.Logger) *NetworkTunnelManager {
	return &NetworkTunnelManager{
		tunnels: make(map[string]Tunnel),
		logger:  logger,
	}
}

// Start initializes the tunnel manager with a context for lifecycle management.
func (m *NetworkTunnelManager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.logger.Info("network tunnel manager started")
}

// Stop gracefully shuts down all managed tunnels and the manager itself.
func (m *NetworkTunnelManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}

	for name, t := range m.tunnels {
		if err := t.Stop(); err != nil {
			m.logger.Error("failed to stop tunnel",
				zap.String("cluster", name),
				zap.String("type", t.Type()),
				zap.Error(err),
			)
		}
	}

	m.tunnels = make(map[string]Tunnel)
	m.logger.Info("network tunnel manager stopped")
}

// AddTunnel creates and starts a tunnel for the specified remote cluster.
// If a tunnel already exists for the cluster, it is stopped and replaced.
func (m *NetworkTunnelManager) AddTunnel(ctx context.Context, clusterName string, config v1alpha1.TunnelConfig) error {
	return m.AddTunnelWithOverlay(ctx, clusterName, config, "")
}

// AddTunnelWithOverlay creates and starts a tunnel with overlay CIDR configuration.
// The overlayCIDR parameter specifies the overlay network address for site-to-site
// routing (e.g., "10.200.1.1/24"). If empty, overlay routing is disabled.
func (m *NetworkTunnelManager) AddTunnelWithOverlay(ctx context.Context, clusterName string, config v1alpha1.TunnelConfig, overlayCIDR string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ctx == nil {
		return fmt.Errorf("tunnel manager not started")
	}

	// Stop existing tunnel if present
	if existing, ok := m.tunnels[clusterName]; ok {
		if err := existing.Stop(); err != nil {
			m.logger.Warn("failed to stop existing tunnel during replacement",
				zap.String("cluster", clusterName),
				zap.Error(err),
			)
		}
	}

	t, err := m.createTunnelWithOverlay(clusterName, config, overlayCIDR)
	if err != nil {
		return fmt.Errorf("creating tunnel for cluster %s: %w", clusterName, err)
	}

	if err := t.Start(ctx); err != nil {
		return fmt.Errorf("starting tunnel for cluster %s: %w", clusterName, err)
	}

	m.tunnels[clusterName] = t
	m.logger.Info("tunnel added",
		zap.String("cluster", clusterName),
		zap.String("type", t.Type()),
		zap.String("localAddr", t.LocalAddr()),
		zap.String("overlayCIDR", overlayCIDR),
	)

	return nil
}

// RemoveTunnel stops and removes the tunnel for the specified cluster.
func (m *NetworkTunnelManager) RemoveTunnel(clusterName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tunnels[clusterName]
	if !ok {
		return fmt.Errorf("no tunnel found for cluster %s", clusterName)
	}

	if err := t.Stop(); err != nil {
		return fmt.Errorf("stopping tunnel for cluster %s: %w", clusterName, err)
	}

	delete(m.tunnels, clusterName)
	m.logger.Info("tunnel removed", zap.String("cluster", clusterName))

	return nil
}

// GetTunnel returns the tunnel for the specified cluster, if one exists.
func (m *NetworkTunnelManager) GetTunnel(clusterName string) (Tunnel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.tunnels[clusterName]
	return t, ok
}

// HealthCheck returns the health status of all managed tunnels.
func (m *NetworkTunnelManager) HealthCheck() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]bool, len(m.tunnels))
	for name, t := range m.tunnels {
		status[name] = t.IsHealthy()
	}

	return status
}

// createTunnelWithOverlay instantiates a tunnel with optional overlay CIDR support.
func (m *NetworkTunnelManager) createTunnelWithOverlay(clusterName string, config v1alpha1.TunnelConfig, overlayCIDR string) (Tunnel, error) {
	switch config.Type {
	case v1alpha1.TunnelTypeWireGuard:
		if overlayCIDR != "" {
			return newWireGuardTunnelWithOverlay(clusterName, config, overlayCIDR, m.logger)
		}
		return newWireGuardTunnel(clusterName, config, m.logger)
	case v1alpha1.TunnelTypeSSH:
		return newSSHTunnel(clusterName, config, m.logger)
	case v1alpha1.TunnelTypeWebSocket:
		return newWebSocketTunnel(clusterName, config, m.logger)
	default:
		return nil, fmt.Errorf("unsupported tunnel type: %s", config.Type)
	}
}
