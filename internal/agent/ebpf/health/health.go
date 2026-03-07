//go:build linux

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

package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	novaebpf "github.com/azrtydxb/novaedge/internal/agent/ebpf"
	"go.uber.org/zap"
)

const subsystem = "health"

// Monitor manages an eBPF per-CPU hash map that tracks passive
// health signals for backend endpoints. The BPF program (attached to
// network hooks) records connection outcomes per backend; this Go-side
// monitor periodically reads and aggregates the counters.
type Monitor struct {
	logger     *zap.Logger
	mu         sync.RWMutex
	healthMap  *ebpf.Map // PERCPU_HASH: BackendKey -> []BackendHealth
	aggregator *Aggregator

	cancelPoller context.CancelFunc
	pollerWg     sync.WaitGroup
}

// NewMonitor creates a new eBPF health signal monitor with the
// given maximum number of tracked backends.
func NewMonitor(logger *zap.Logger, maxBackends uint32) (*Monitor, error) {
	if maxBackends == 0 {
		maxBackends = 4096
	}

	healthMap, err := ebpf.NewMap(&ebpf.MapSpec{
		Name:       "backend_health",
		Type:       ebpf.PerCPUHash,
		KeySize:    8,  // BackendKey (4-byte addr + 2-byte port + 2-byte pad)
		ValueSize:  56, // BackendHealth (7 x uint64)
		MaxEntries: maxBackends,
	})
	if err != nil {
		novaebpf.RecordError(subsystem, "map_create")
		return nil, fmt.Errorf("creating backend health map: %w", err)
	}

	logger.Info("eBPF health monitor map created",
		zap.Uint32("max_backends", maxBackends))

	return &Monitor{
		logger:     logger.With(zap.String("component", "ebpf-health")),
		healthMap:  healthMap,
		aggregator: NewAggregator(),
	}, nil
}

// Poll reads all entries from the per-CPU health map, aggregates
// counters across CPUs, and returns the node-wide health state with
// deltas from the previous poll.
func (hm *Monitor) Poll() (map[BackendKey]AggregatedHealth, error) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	if hm.healthMap == nil {
		return nil, fmt.Errorf("health monitor not initialized")
	}

	// Read all entries from the per-CPU hash map.
	perCPU := make(map[BackendKey][]BackendHealth)

	var key BackendKey
	var values []BackendHealth
	iter := hm.healthMap.Iterate()
	for iter.Next(&key, &values) {
		// Copy the values slice since Iterate reuses it.
		cpuCopy := make([]BackendHealth, len(values))
		copy(cpuCopy, values)
		perCPU[key] = cpuCopy
	}

	// Aggregate per-CPU data into node-wide health.
	return hm.aggregator.Aggregate(perCPU), nil
}

// StartPoller starts a background goroutine that periodically polls
// the BPF health map and invokes the callback with aggregated health
// data. The poller runs until Close() is called or the context is
// cancelled.
func (hm *Monitor) StartPoller(ctx context.Context, interval time.Duration, callback func(map[BackendKey]AggregatedHealth)) {
	ctx, cancel := context.WithCancel(ctx)
	hm.cancelPoller = cancel

	hm.pollerWg.Add(1)
	go func() {
		defer hm.pollerWg.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				data, err := hm.Poll()
				if err != nil {
					hm.logger.Warn("failed to poll eBPF health map",
						zap.Error(err))
					continue
				}
				if callback != nil && len(data) > 0 {
					callback(data)
				}
			}
		}
	}()

	hm.logger.Info("eBPF health poller started",
		zap.Duration("interval", interval))
}

// IsActive returns true if the health monitor has been successfully
// initialized and its map is available.
func (hm *Monitor) IsActive() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.healthMap != nil
}

// Close stops the poller and releases BPF map resources.
func (hm *Monitor) Close() error {
	if hm.cancelPoller != nil {
		hm.cancelPoller()
		hm.pollerWg.Wait()
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	if hm.healthMap != nil {
		hm.healthMap.Close()
		hm.healthMap = nil
	}

	hm.logger.Info("eBPF health monitor closed")
	return nil
}
