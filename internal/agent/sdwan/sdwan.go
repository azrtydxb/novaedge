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

package sdwan

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
)

const (
	// stateUpdateInterval is how often the manager refreshes link states
	// and exports metrics.
	stateUpdateInterval = 5 * time.Second
)

// SDWANManager is the top-level orchestrator for SD-WAN functionality.
// It owns the Prober, PathSelector, and LinkManager, providing a single
// entry point for the agent to manage WAN links.
type SDWANManager struct {
	linkMgr      *LinkManager
	prober       *Prober
	pathSelector *PathSelector
	logger       *zap.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// NewSDWANManager creates a new SD-WAN manager with all sub-components.
func NewSDWANManager(logger *zap.Logger) *SDWANManager {
	l := logger.Named("sdwan")
	prober := NewProber(l)
	selector := NewPathSelector(l)
	linkMgr := NewLinkManager(prober, selector, l)

	return &SDWANManager{
		linkMgr:      linkMgr,
		prober:       prober,
		pathSelector: selector,
		logger:       l,
	}
}

// Start initializes and starts the SD-WAN manager, including the prober
// and a periodic state-update loop that refreshes link states and exports metrics.
func (m *SDWANManager) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	m.prober.Start(m.ctx)

	m.wg.Add(1)
	go m.stateLoop()

	m.logger.Info("SD-WAN manager started")
	return nil
}

// Stop gracefully shuts down the SD-WAN manager.
func (m *SDWANManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.prober.Stop()
	m.wg.Wait()
	m.logger.Info("SD-WAN manager stopped")
}

// AddLink adds a new WAN link to management.
func (m *SDWANManager) AddLink(config LinkConfig) error {
	if err := m.linkMgr.AddLink(config); err != nil {
		return fmt.Errorf("failed to add link %q: %w", config.Name, err)
	}
	return nil
}

// RemoveLink removes a WAN link from management.
func (m *SDWANManager) RemoveLink(name string) error {
	if err := m.linkMgr.RemoveLink(name); err != nil {
		return fmt.Errorf("failed to remove link %q: %w", name, err)
	}
	return nil
}

// GetLinkQualities returns current quality metrics for all managed links.
func (m *SDWANManager) GetLinkQualities() map[string]*LinkQuality {
	return m.prober.GetAllQualities()
}

// SelectPath selects the best WAN link for the given policy and strategy.
func (m *SDWANManager) SelectPath(policyName, strategy string) (string, error) {
	selected, err := m.linkMgr.SelectPathForPolicy(policyName, strategy)
	if err != nil {
		return "", err
	}

	metrics.SDWANPathSelections.WithLabelValues(selected, strategy).Inc()
	return selected, nil
}

// stateLoop periodically updates link states and exports Prometheus metrics.
func (m *SDWANManager) stateLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(stateUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.linkMgr.UpdateLinkStates()
			m.exportMetrics()
		}
	}
}

// exportMetrics publishes current link quality data to Prometheus gauges.
func (m *SDWANManager) exportMetrics() {
	qualities := m.prober.GetAllQualities()
	for _, q := range qualities {
		metrics.SDWANLinkLatency.WithLabelValues(q.LinkName, q.RemoteSite).Set(q.LatencyMs)
		metrics.SDWANLinkJitter.WithLabelValues(q.LinkName, q.RemoteSite).Set(q.JitterMs)
		metrics.SDWANLinkPacketLoss.WithLabelValues(q.LinkName, q.RemoteSite).Set(q.PacketLoss)
		metrics.SDWANLinkScore.WithLabelValues(q.LinkName, q.RemoteSite).Set(q.Score)

		healthy := 0.0
		if q.Healthy {
			healthy = 1.0
		}
		metrics.SDWANLinkHealthy.WithLabelValues(q.LinkName, q.RemoteSite).Set(healthy)
	}
}
