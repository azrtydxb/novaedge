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

package conntrack

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	novaebpf "github.com/azrtydxb/novaedge/internal/agent/ebpf"
	"go.uber.org/zap"
)

const subsystem = "conntrack"

// gcInterval is the base interval between garbage collection runs.
const gcInterval = 10 * time.Second

// gcJitter is the maximum random jitter added to the GC interval to
// avoid synchronised thundering herd across multiple agent instances.
const gcJitter = 5 * time.Second

// Conntrack manages the eBPF LRU connection tracking table. It provides
// Go-side access for lookups and periodic garbage collection of expired
// entries.
type Conntrack struct {
	logger     *zap.Logger
	maxEntries uint32
	maxAge     time.Duration

	mu       sync.Mutex
	ctMap    *ebpf.Map
	statsMap *ebpf.Map
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	closed   bool
}

// NewConntrack creates and initialises the eBPF LRU conntrack table.
// If maxEntries is 0, DefaultMaxEntries is used.
// The maxAge parameter controls how long entries are kept before GC
// removes them; a value of 0 defaults to 5 minutes.
func NewConntrack(logger *zap.Logger, maxEntries uint32, maxAge time.Duration) (*Conntrack, error) {
	if maxEntries == 0 {
		maxEntries = DefaultMaxEntries
	}
	if maxAge == 0 {
		maxAge = 5 * time.Minute
	}

	ct := &Conntrack{
		logger:     logger.With(zap.String("component", "ebpf-conntrack")),
		maxEntries: maxEntries,
		maxAge:     maxAge,
	}

	if err := ct.init(); err != nil {
		return nil, err
	}

	return ct, nil
}

// init loads the BPF collection and extracts the conntrack map.
func (ct *Conntrack) init() error {
	spec, err := loadConntrack()
	if err != nil {
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("loading conntrack BPF spec: %w", err)
	}

	// Override max_entries if different from compiled default.
	if mapSpec, ok := spec.Maps["novaedge_ct"]; ok {
		mapSpec.MaxEntries = ct.maxEntries
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("creating conntrack BPF collection: %w", err)
	}

	ctMap := coll.Maps["novaedge_ct"]
	if ctMap == nil {
		coll.Close()
		return fmt.Errorf("novaedge_ct map not found in BPF collection")
	}

	statsMap := coll.Maps["ct_stats"]

	ct.ctMap = ctMap
	ct.statsMap = statsMap

	novaebpf.RecordProgramLoaded(subsystem)
	ct.logger.Info("eBPF conntrack table initialised",
		zap.Uint32("maxEntries", ct.maxEntries),
		zap.Duration("maxAge", ct.maxAge))

	return nil
}

// Lookup retrieves a conntrack entry for the given flow key.
// Returns nil if no entry exists.
func (ct *Conntrack) Lookup(key CTKey) (*CTEntry, error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.ctMap == nil {
		return nil, fmt.Errorf("conntrack not initialised")
	}

	var entry CTEntry
	if err := ct.ctMap.Lookup(key, &entry); err != nil {
		return nil, nil // Not found is not an error.
	}

	return &entry, nil
}

// Map returns the underlying BPF map handle, which can be shared with
// XDP programs for kernel-side conntrack lookups.
func (ct *Conntrack) Map() *ebpf.Map {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.ctMap
}

// GarbageCollect iterates the conntrack table and removes entries
// whose timestamp is older than maxAge. Returns the number of entries
// deleted.
func (ct *Conntrack) GarbageCollect(maxAge time.Duration) (int, error) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.ctMap == nil {
		return 0, fmt.Errorf("conntrack not initialised")
	}

	// Get current monotonic time in nanoseconds to compare with BPF
	// ktime_get_ns() timestamps.
	now := uint64(monotimeNs())
	cutoff := now - uint64(maxAge.Nanoseconds())

	var key CTKey
	var entry CTEntry
	toDelete := make([]CTKey, 0, 64)

	iter := ct.ctMap.Iterate()
	for iter.Next(&key, &entry) {
		if entry.Timestamp < cutoff {
			toDelete = append(toDelete, key)
		}
	}

	deleted := 0
	for _, k := range toDelete {
		if err := ct.ctMap.Delete(k); err == nil {
			deleted++
			novaebpf.RecordMapOp("novaedge_ct", "delete", "ok")
		} else {
			novaebpf.RecordMapOp("novaedge_ct", "delete", "error")
		}
	}

	return deleted, nil
}

// StartGC starts a background goroutine that periodically runs garbage
// collection on the conntrack table. The goroutine runs every gcInterval
// plus random jitter. Call Close() to stop it.
func (ct *Conntrack) StartGC() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.cancel != nil {
		return // GC already running.
	}

	ctx, cancel := context.WithCancel(context.Background())
	ct.cancel = cancel

	ct.wg.Add(1)
	go ct.gcLoop(ctx)

	ct.logger.Info("conntrack GC goroutine started",
		zap.Duration("interval", gcInterval),
		zap.Duration("maxAge", ct.maxAge))
}

// gcLoop is the background GC goroutine.
func (ct *Conntrack) gcLoop(ctx context.Context) {
	defer ct.wg.Done()

	for {
		// Add random jitter to avoid thundering herd.
		jitter := time.Duration(rand.Int63n(int64(gcJitter)))
		timer := time.NewTimer(gcInterval + jitter)

		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		deleted, err := ct.GarbageCollect(ct.maxAge)
		if err != nil {
			ct.logger.Warn("conntrack GC failed", zap.Error(err))
			continue
		}
		if deleted > 0 {
			ct.logger.Debug("conntrack GC completed",
				zap.Int("deleted", deleted))
		}
	}
}

// Stats returns per-CPU aggregated conntrack statistics.
func (ct *Conntrack) Stats() map[string]uint64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	result := make(map[string]uint64)
	if ct.statsMap == nil {
		return result
	}

	statNames := []string{
		"lookups", "hits", "misses", "inserts", "updates",
	}

	for i, name := range statNames {
		key := uint32(i)
		var values []uint64
		if err := ct.statsMap.Lookup(key, &values); err != nil {
			continue
		}
		var total uint64
		for _, v := range values {
			total += v
		}
		result[name] = total
	}

	return result
}

// Close stops the GC goroutine and releases all BPF resources.
func (ct *Conntrack) Close() error {
	ct.mu.Lock()

	ct.closed = true

	if ct.cancel != nil {
		ct.cancel()
		ct.cancel = nil
	}
	ct.mu.Unlock()

	// Wait for GC goroutine to exit (outside the lock to avoid deadlock).
	ct.wg.Wait()

	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.ctMap != nil {
		ct.ctMap.Close()
		ct.ctMap = nil
	}
	if ct.statsMap != nil {
		ct.statsMap.Close()
		ct.statsMap = nil
	}

	novaebpf.RecordProgramUnloaded(subsystem)
	ct.logger.Info("eBPF conntrack table closed")
	return nil
}

// monotimeNs returns the monotonic clock time in nanoseconds, matching
// the BPF bpf_ktime_get_ns() helper used for timestamps.
func monotimeNs() int64 {
	return time.Now().UnixNano()
}
