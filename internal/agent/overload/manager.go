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

package overload

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Config configures the overload manager.
type Config struct {
	// Enabled turns overload protection on.
	Enabled bool
	// PollInterval is how often to check resource usage (default 1s).
	PollInterval time.Duration
	// MemoryThreshold is the heap memory ratio (0-1.0) to start shedding (default 0.9).
	MemoryThreshold float64
	// MemoryRecoverThreshold is below which to stop shedding (default 0.85, hysteresis).
	MemoryRecoverThreshold float64
	// GoroutineThreshold triggers shedding above this count (default 100000).
	GoroutineThreshold int
	// GoroutineRecoverThreshold stops shedding below this (default 90000).
	GoroutineRecoverThreshold int
	// MaxActiveConnections triggers shedding above this count (default 50000).
	MaxActiveConnections int64
	// ActiveConnectionRecoverThreshold stops shedding below this (default 45000).
	ActiveConnectionRecoverThreshold int64
	// MemoryLimitBytes is the maximum heap memory in bytes. If zero, it defaults
	// to the system memory reported by runtime.MemStats.Sys.
	MemoryLimitBytes uint64
}

// DefaultConfig returns sensible defaults for the overload manager.
func DefaultConfig() Config {
	return Config{
		Enabled:                          true,
		PollInterval:                     time.Second,
		MemoryThreshold:                  0.9,
		MemoryRecoverThreshold:           0.85,
		GoroutineThreshold:               100000,
		GoroutineRecoverThreshold:        90000,
		MaxActiveConnections:             50000,
		ActiveConnectionRecoverThreshold: 45000,
	}
}

// State tracks current resource pressure.
type State struct {
	HeapUsageRatio    float64
	GoroutineCount    int
	ActiveConnections int64
	IsOverloaded      bool
	ShedReason        string
}

// Manager monitors system resources and signals overload conditions.
// It uses hysteresis (separate trigger and recover thresholds) to prevent
// rapid oscillation between normal and overloaded states.
type Manager struct {
	mu     sync.RWMutex
	config Config
	state  State

	activeConns atomic.Int64
	logger      *zap.Logger
	stopCh      chan struct{}
	wg          sync.WaitGroup

	// Callbacks invoked on state transitions.
	onOverloadStart func()
	onOverloadEnd   func()
}

// NewManager creates a new overload manager with the given configuration.
func NewManager(config Config, logger *zap.Logger) *Manager {
	return &Manager{
		config: config,
		logger: logger.With(zap.String("component", "overload-manager")),
		stopCh: make(chan struct{}),
	}
}

// Start begins the periodic resource monitoring loop. It blocks until the
// provided context is cancelled or Stop is called.
func (m *Manager) Start(ctx context.Context) {
	if !m.config.Enabled {
		m.logger.Info("overload manager is disabled")
		return
	}

	m.logger.Info("starting overload manager",
		zap.Duration("poll_interval", m.config.PollInterval),
		zap.Float64("memory_threshold", m.config.MemoryThreshold),
		zap.Int("goroutine_threshold", m.config.GoroutineThreshold),
		zap.Int64("max_active_connections", m.config.MaxActiveConnections),
	)

	m.wg.Add(1)
	go m.monitor(ctx)
}

// Stop signals the monitoring loop to exit and waits for it to finish.
func (m *Manager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

// ShouldShed returns true if the system is currently in an overloaded state
// and requests should be shed.
func (m *Manager) ShouldShed() bool {
	if !m.config.Enabled {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.IsOverloaded
}

// GetState returns a copy of the current overload state.
func (m *Manager) GetState() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// IncrementConnections atomically increments the active connection count.
func (m *Manager) IncrementConnections() {
	m.activeConns.Add(1)
}

// DecrementConnections atomically decrements the active connection count.
func (m *Manager) DecrementConnections() {
	m.activeConns.Add(-1)
}

// SetCallbacks sets the functions called when overload state transitions occur.
func (m *Manager) SetCallbacks(onStart, onEnd func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onOverloadStart = onStart
	m.onOverloadEnd = onEnd
}

// monitor runs the periodic resource check loop.
func (m *Manager) monitor(ctx context.Context) {
	defer m.wg.Done()

	interval := m.config.PollInterval
	if interval <= 0 {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkResources()
		}
	}
}

// checkResources evaluates all resource monitors and updates the overload state.
func (m *Manager) checkResources() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Determine memory limit.
	memLimit := m.config.MemoryLimitBytes
	if memLimit == 0 {
		memLimit = memStats.Sys
	}

	var heapRatio float64
	if memLimit > 0 {
		heapRatio = float64(memStats.HeapAlloc) / float64(memLimit)
	}

	goroutines := runtime.NumGoroutine()
	conns := m.activeConns.Load()

	m.mu.Lock()
	defer m.mu.Unlock()

	wasOverloaded := m.state.IsOverloaded

	m.state.HeapUsageRatio = heapRatio
	m.state.GoroutineCount = goroutines
	m.state.ActiveConnections = conns

	if m.state.IsOverloaded {
		// Check if we should recover (below all recover thresholds).
		memOK := heapRatio < m.config.MemoryRecoverThreshold
		goroutineOK := goroutines < m.config.GoroutineRecoverThreshold
		connOK := conns < m.config.ActiveConnectionRecoverThreshold

		if memOK && goroutineOK && connOK {
			m.state.IsOverloaded = false
			m.state.ShedReason = ""
			m.logger.Info("overload recovered",
				zap.Float64("heap_ratio", heapRatio),
				zap.Int("goroutines", goroutines),
				zap.Int64("connections", conns),
			)
		}
	} else {
		// Check if any threshold is exceeded.
		reason := ""

		switch {
		case heapRatio >= m.config.MemoryThreshold:
			reason = "memory"
		case goroutines >= m.config.GoroutineThreshold:
			reason = "goroutines"
		case conns >= m.config.MaxActiveConnections:
			reason = "connections"
		}

		if reason != "" {
			m.state.IsOverloaded = true
			m.state.ShedReason = reason
			m.logger.Warn("overload detected",
				zap.String("reason", reason),
				zap.Float64("heap_ratio", heapRatio),
				zap.Int("goroutines", goroutines),
				zap.Int64("connections", conns),
			)
		}
	}

	// Invoke callbacks on state transitions.
	if wasOverloaded && !m.state.IsOverloaded {
		if m.onOverloadEnd != nil {
			m.onOverloadEnd()
		}
	} else if !wasOverloaded && m.state.IsOverloaded {
		if m.onOverloadStart != nil {
			m.onOverloadStart()
		}
	}

	// Update metrics.
	updateMetrics(m.state)
}
