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

// Package mesh provides east-west service mesh traffic interception using
// nftables/iptables TPROXY for transparent proxying of pod-to-pod traffic.
package mesh

import (
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// RuleBackend abstracts the underlying packet-filtering system used for
// TPROXY interception (nftables or iptables). Implementations must be safe
// for sequential use but are always called under TPROXYManager's mutex.
type RuleBackend interface {
	// Setup initialises chains/tables and policy-routing rules needed for
	// TPROXY. It is called once before the first ApplyRules call.
	Setup() error

	// ApplyRules atomically reconciles the set of intercepted targets so
	// that only the given targets are matched after the call returns.
	ApplyRules(targets []InterceptTarget, tproxyPort int32) error

	// Cleanup removes all rules, chains/tables, and routing entries
	// created by Setup/ApplyRules.
	Cleanup() error

	// Name returns a human-readable backend identifier (e.g. "nftables").
	Name() string
}

// InterceptTarget represents a ClusterIP:port to intercept.
type InterceptTarget struct {
	ClusterIP string
	Port      int32
}

// Key returns a unique key for this target.
func (t InterceptTarget) Key() string {
	return fmt.Sprintf("%s:%d", t.ClusterIP, t.Port)
}

// TPROXYManager manages TPROXY rules for intercepting east-west traffic
// destined to Kubernetes ClusterIP Services. It delegates the actual
// packet-filter manipulation to a RuleBackend (nftables or iptables).
type TPROXYManager struct {
	mu          sync.Mutex
	logger      *zap.Logger
	backend     RuleBackend
	tproxyPort  int32
	intercepted map[string]InterceptTarget // key: "clusterIP:port"
}

// NewTPROXYManager creates a new TPROXY rule manager using auto-detected
// backend (nftables preferred, iptables fallback).
// tproxyPort is the local port where the transparent listener accepts connections.
func NewTPROXYManager(logger *zap.Logger, tproxyPort int32) *TPROXYManager {
	named := logger.Named("tproxy")
	backend := detectBackend(named)
	named.Info("Selected TPROXY backend", zap.String("backend", backend.Name()))
	return &TPROXYManager{
		logger:      named,
		backend:     backend,
		tproxyPort:  tproxyPort,
		intercepted: make(map[string]InterceptTarget),
	}
}

// NewTPROXYManagerWithBackend creates a TPROXY manager with an explicit
// backend (for testing or manual override).
func NewTPROXYManagerWithBackend(logger *zap.Logger, tproxyPort int32, backend RuleBackend) *TPROXYManager {
	return &TPROXYManager{
		logger:      logger.Named("tproxy"),
		backend:     backend,
		tproxyPort:  tproxyPort,
		intercepted: make(map[string]InterceptTarget),
	}
}

// Setup initializes the backend (chains/tables and routing rules).
// Must be called before ApplyRules.
func (m *TPROXYManager) Setup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.backend.Setup(); err != nil {
		return fmt.Errorf("TPROXY backend setup failed (%s): %w", m.backend.Name(), err)
	}
	return nil
}

// ApplyRules reconciles TPROXY rules to match the desired set of
// intercepted ClusterIP:port pairs.
func (m *TPROXYManager) ApplyRules(desired []InterceptTarget) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.backend.ApplyRules(desired, m.tproxyPort); err != nil {
		return fmt.Errorf("TPROXY apply rules failed (%s): %w", m.backend.Name(), err)
	}

	// Rebuild the intercepted map for tracking/logging.
	m.intercepted = make(map[string]InterceptTarget, len(desired))
	for _, t := range desired {
		m.intercepted[t.Key()] = t
	}

	m.logger.Info("TPROXY rules reconciled",
		zap.Int("active_rules", len(m.intercepted)),
		zap.String("backend", m.backend.Name()))
	return nil
}

// Cleanup removes all TPROXY rules and routing entries.
func (m *TPROXYManager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.backend.Cleanup(); err != nil {
		m.logger.Error("TPROXY cleanup failed",
			zap.String("backend", m.backend.Name()),
			zap.Error(err))
		return err
	}

	m.intercepted = make(map[string]InterceptTarget)
	m.logger.Info("TPROXY cleanup complete", zap.String("backend", m.backend.Name()))
	return nil
}

// ActiveRuleCount returns the number of active TPROXY rules.
func (m *TPROXYManager) ActiveRuleCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.intercepted)
}
