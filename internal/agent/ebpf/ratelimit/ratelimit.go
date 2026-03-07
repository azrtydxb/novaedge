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

package ratelimit

import (
	"errors"
	"fmt"
	"net"
	"sync"

	novaebpf "github.com/azrtydxb/novaedge/internal/agent/ebpf"
	"github.com/cilium/ebpf"
	"go.uber.org/zap"
)

// errNotInitialized is returned when a rate limiter operation is attempted
// before the limiter has been properly initialized.
var errNotInitialized = errors.New("rate limiter not initialized")

const subsystem = "ratelimit"

// statsKeyAllowed is the BPF stats map index for the allowed counter.
const statsKeyAllowed = uint32(0)

// statsKeyDenied is the BPF stats map index for the denied counter.
const statsKeyDenied = uint32(1)

// RateLimiter manages an eBPF per-CPU token bucket rate limiter.
// The BPF program runs in the kernel and performs per-source-IP rate
// limiting using a per-CPU LRU hash map for lock-free operation.
//
// The Go side manages configuration and reads counters; the actual
// rate limiting decision happens entirely in BPF.
type RateLimiter struct {
	logger     *zap.Logger
	mu         sync.RWMutex
	maxEntries uint32

	// BPF map handles.
	tokenMap  *ebpf.Map // LRU_PERCPU_HASH: Key -> []Value
	configMap *ebpf.Map // ARRAY(1): Config
	statsMap  *ebpf.Map // PERCPU_ARRAY(2): uint64 (allowed/denied)
}

// NewRateLimiter creates a new eBPF-based rate limiter with the given
// maximum number of tracked source IPs. The BPF maps are created
// immediately; call Configure() to set the rate/burst parameters before
// the limiter is effective.
func NewRateLimiter(logger *zap.Logger, maxEntries uint32) (*RateLimiter, error) {
	if maxEntries == 0 {
		maxEntries = 100000
	}

	// Create the per-CPU LRU hash map for token bucket state.
	tokenMap, err := ebpf.NewMap(&ebpf.MapSpec{
		Name:       "rl_tokens",
		Type:       ebpf.LRUCPUHash,
		KeySize:    16, // Key (16-byte IP)
		ValueSize:  16, // Value per CPU (tokens + last_refill_ns)
		MaxEntries: maxEntries,
	})
	if err != nil {
		novaebpf.RecordError(subsystem, "map_create")
		return nil, fmt.Errorf("creating rate limit token map: %w", err)
	}

	// Create the config array map (1 entry).
	configMap, err := ebpf.NewMap(&ebpf.MapSpec{
		Name:       "rl_config",
		Type:       ebpf.Array,
		KeySize:    4,  // uint32
		ValueSize:  24, // Config (rate + burst + window_ns)
		MaxEntries: 1,
	})
	if err != nil {
		_ = tokenMap.Close()
		novaebpf.RecordError(subsystem, "map_create")
		return nil, fmt.Errorf("creating rate limit config map: %w", err)
	}

	// Create the per-CPU stats array (2 entries: allowed, denied).
	statsMap, err := ebpf.NewMap(&ebpf.MapSpec{
		Name:       "rl_stats",
		Type:       ebpf.PerCPUArray,
		KeySize:    4, // uint32
		ValueSize:  8, // uint64
		MaxEntries: 2,
	})
	if err != nil {
		_ = tokenMap.Close()
		_ = configMap.Close()
		novaebpf.RecordError(subsystem, "map_create")
		return nil, fmt.Errorf("creating rate limit stats map: %w", err)
	}

	logger.Info("eBPF rate limiter maps created",
		zap.Uint32("max_entries", maxEntries))

	return &RateLimiter{
		logger:     logger.With(zap.String("component", "ebpf-ratelimit")),
		maxEntries: maxEntries,
		tokenMap:   tokenMap,
		configMap:  configMap,
		statsMap:   statsMap,
	}, nil
}

// Configure writes the rate limiting parameters to the BPF config map.
// rate is tokens per second, burst is the maximum bucket capacity.
func (rl *RateLimiter) Configure(rate, burst uint64) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.configMap == nil {
		return errNotInitialized
	}

	config := Config{
		Rate:     rate,
		Burst:    burst,
		WindowNS: 1_000_000_000, // 1 second in nanoseconds
	}

	key := uint32(0)
	if err := rl.configMap.Update(key, config, ebpf.UpdateAny); err != nil {
		novaebpf.RecordMapOp("rl_config", "update", "error")
		return fmt.Errorf("updating rate limit config: %w", err)
	}

	novaebpf.RecordMapOp("rl_config", "update", "ok")
	rl.logger.Info("eBPF rate limiter configured",
		zap.Uint64("rate", rate),
		zap.Uint64("burst", burst))

	return nil
}

// CheckAllowed performs a rate limit check for the given IP address by
// looking up the BPF token map. This is intended for Go-side fallback
// checks; the primary rate limiting happens in the BPF program itself.
//
// Returns true if the request should be allowed based on the current
// token bucket state, false if rate limited.
func (rl *RateLimiter) CheckAllowed(ip net.IP) (bool, error) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	if rl.tokenMap == nil {
		return true, errNotInitialized
	}

	key := ipToKey(ip)

	var values []Value
	if err := rl.tokenMap.Lookup(key, &values); err != nil {
		// Key not found means the IP hasn't been seen yet, so allow.
		return true, nil //nolint:nilerr // missing key is expected for new IPs
	}

	// Sum tokens across all CPUs. If any CPU has tokens, allow.
	for _, v := range values {
		if v.Tokens > 0 {
			return true, nil
		}
	}

	return false, nil
}

// GetStats reads the per-CPU stats counters and returns aggregated
// allowed and denied counts.
func (rl *RateLimiter) GetStats() (Stats, error) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	var stats Stats
	if rl.statsMap == nil {
		return stats, errNotInitialized
	}

	// Read allowed counter (per-CPU array, sum across CPUs).
	var allowedValues []uint64
	if err := rl.statsMap.Lookup(statsKeyAllowed, &allowedValues); err == nil {
		for _, v := range allowedValues {
			stats.Allowed += v
		}
	}

	// Read denied counter.
	var deniedValues []uint64
	if err := rl.statsMap.Lookup(statsKeyDenied, &deniedValues); err == nil {
		for _, v := range deniedValues {
			stats.Denied += v
		}
	}

	return stats, nil
}

// IsActive returns true if the eBPF rate limiter has been successfully
// initialized and its maps are available.
func (rl *RateLimiter) IsActive() bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.tokenMap != nil && rl.configMap != nil
}

// Close releases all BPF map resources.
func (rl *RateLimiter) Close() error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.tokenMap != nil {
		_ = rl.tokenMap.Close()
		rl.tokenMap = nil
	}
	if rl.configMap != nil {
		_ = rl.configMap.Close()
		rl.configMap = nil
	}
	if rl.statsMap != nil {
		_ = rl.statsMap.Close()
		rl.statsMap = nil
	}

	rl.logger.Info("eBPF rate limiter closed")
	return nil
}

// ipToKey converts a net.IP to a Key. IPv4 addresses are stored
// in their IPv4-mapped IPv6 form (::ffff:x.x.x.x) to use a single
// 16-byte key format.
func ipToKey(ip net.IP) Key {
	var key Key
	ip16 := ip.To16()
	if ip16 != nil {
		copy(key.IP[:], ip16)
	} else {
		// Fallback: copy whatever bytes we have.
		copy(key.IP[:], ip)
	}
	return key
}
