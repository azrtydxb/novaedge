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

package maglev

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"net"
	"sync"

	"github.com/cilium/ebpf"
	novaebpf "github.com/piwi3910/novaedge/internal/agent/ebpf"
	"go.uber.org/zap"
)

const subsystem = "maglev"

// Manager manages the eBPF Maglev consistent hashing lookup table.
// It maintains two inner BPF array maps and uses the outer ARRAY_OF_MAPS
// to atomically swap between them when the backend set changes.
type Manager struct {
	logger    *zap.Logger
	tableSize uint32

	mu         sync.Mutex
	outerMap   *ebpf.Map // ARRAY_OF_MAPS with 2 slots
	backendMap *ebpf.Map // backend_id -> BackendValue
	statsMap   *ebpf.Map
	innerMaps  [2]*ebpf.Map // two inner tables for swap
	activeSlot int          // 0 or 1: which inner map is active
	innerSpec  *ebpf.MapSpec
	closed     bool
}

// NewManager creates a new Maglev BPF map manager with the given table size.
// If tableSize is 0, DefaultTableSize is used.
func NewManager(logger *zap.Logger, tableSize uint32) *Manager {
	if tableSize == 0 {
		tableSize = DefaultTableSize
	}
	return &Manager{
		logger:    logger.With(zap.String("component", "ebpf-maglev")),
		tableSize: tableSize,
	}
}

// Init loads the BPF collection and initialises the maps. This must be
// called before UpdateTable.
func (m *Manager) Init() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	spec, err := loadMaglevLookup()
	if err != nil {
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("loading Maglev BPF spec: %w", err)
	}

	// Adjust inner map size to match configured table size if different
	// from the compiled default.
	if innerSpec, ok := spec.Maps["maglev_inner"]; ok {
		innerSpec.MaxEntries = m.tableSize
		m.innerSpec = innerSpec.Copy()
	}
	if outerSpec, ok := spec.Maps["maglev_outer"]; ok {
		if outerSpec.InnerMap != nil {
			outerSpec.InnerMap.MaxEntries = m.tableSize
		}
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("creating Maglev BPF collection: %w", err)
	}

	outerMap := coll.Maps["maglev_outer"]
	if outerMap == nil {
		coll.Close()
		return fmt.Errorf("maglev_outer map not found in BPF collection")
	}

	backendMap := coll.Maps["maglev_backends"]
	if backendMap == nil {
		coll.Close()
		return fmt.Errorf("maglev_backends map not found in BPF collection")
	}

	statsMap := coll.Maps["maglev_stats"]

	m.outerMap = outerMap
	m.backendMap = backendMap
	m.statsMap = statsMap
	m.activeSlot = 0

	// Create the two inner maps for double-buffering.
	for i := 0; i < 2; i++ {
		innerMap, err := ebpf.NewMap(&ebpf.MapSpec{
			Type:       ebpf.Array,
			KeySize:    4,
			ValueSize:  4, // sizeof(maglev_entry)
			MaxEntries: m.tableSize,
		})
		if err != nil {
			m.closeInner()
			coll.Close()
			return fmt.Errorf("creating inner map %d: %w", i, err)
		}
		m.innerMaps[i] = innerMap

		// Register inner map in the outer map.
		if err := outerMap.Update(uint32(i), innerMap, ebpf.UpdateAny); err != nil {
			m.closeInner()
			coll.Close()
			return fmt.Errorf("registering inner map %d in outer: %w", i, err)
		}
	}

	novaebpf.RecordProgramLoaded(subsystem)
	m.logger.Info("Maglev BPF maps initialised",
		zap.Uint32("tableSize", m.tableSize))

	return nil
}

// UpdateTable computes the Maglev permutation table for the given backends,
// writes it into the standby inner map, updates the backend hash map, and
// atomically swaps the outer map pointer to publish the new table.
func (m *Manager) UpdateTable(backends []Backend) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.outerMap == nil {
		return fmt.Errorf("Maglev manager not initialised")
	}

	if m.closed {
		return fmt.Errorf("Maglev manager is closed")
	}

	// Compute Maglev permutation table.
	table := m.computeTable(backends)

	// Write to the standby (inactive) inner map.
	standbySlot := 1 - m.activeSlot
	standbyMap := m.innerMaps[standbySlot]
	if standbyMap == nil {
		return fmt.Errorf("standby inner map (slot %d) is nil", standbySlot)
	}

	// Populate the standby table.
	for i, backendID := range table {
		key := uint32(i)
		val := Entry{BackendID: backendID}
		if err := standbyMap.Update(key, val, ebpf.UpdateAny); err != nil {
			novaebpf.RecordMapOp("maglev_inner", "update", "error")
			return fmt.Errorf("writing Maglev entry %d: %w", i, err)
		}
	}
	novaebpf.RecordMapOp("maglev_inner", "update", "ok")

	// Update the backend resolution map.
	if err := m.syncBackendMap(backends); err != nil {
		return fmt.Errorf("syncing backend map: %w", err)
	}

	// Atomic swap: update outer map slot 0 to point to the standby map.
	if err := m.outerMap.Update(uint32(0), standbyMap, ebpf.UpdateAny); err != nil {
		novaebpf.RecordMapOp("maglev_outer", "update", "error")
		return fmt.Errorf("swapping active Maglev table: %w", err)
	}
	novaebpf.RecordMapOp("maglev_outer", "update", "ok")

	// Move the old active to slot 1 for next swap.
	oldActive := m.innerMaps[m.activeSlot]
	if err := m.outerMap.Update(uint32(1), oldActive, ebpf.UpdateAny); err != nil {
		m.logger.Warn("failed to update standby slot in outer map", zap.Error(err))
	}

	m.activeSlot = standbySlot

	m.logger.Info("Maglev table updated",
		zap.Int("backends", len(backends)),
		zap.Int("activeSlot", m.activeSlot))

	return nil
}

// Stats returns per-CPU aggregated Maglev statistics.
func (m *Manager) Stats() map[string]uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]uint64)
	if m.statsMap == nil {
		return result
	}

	statNames := []string{
		"lookups", "hits", "misses", "backend_miss",
	}

	for i, name := range statNames {
		key := uint32(i)
		var values []uint64
		if err := m.statsMap.Lookup(key, &values); err != nil {
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

// OuterMap returns the outer ARRAY_OF_MAPS handle, which can be shared with
// the XDP LB program for integrated Maglev lookup.
func (m *Manager) OuterMap() *ebpf.Map {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outerMap
}

// BackendMap returns the backend resolution map handle.
func (m *Manager) BackendMap() *ebpf.Map {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backendMap
}

// Close releases all BPF resources.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true

	m.closeInner()

	if m.outerMap != nil {
		m.outerMap.Close()
		m.outerMap = nil
	}
	if m.backendMap != nil {
		m.backendMap.Close()
		m.backendMap = nil
	}
	if m.statsMap != nil {
		m.statsMap.Close()
		m.statsMap = nil
	}

	novaebpf.RecordProgramUnloaded(subsystem)
	m.logger.Info("Maglev BPF maps closed")
	return nil
}

// closeInner closes both inner maps.
func (m *Manager) closeInner() {
	for i, im := range m.innerMaps {
		if im != nil {
			im.Close()
			m.innerMaps[i] = nil
		}
	}
}

// computeTable builds the Maglev lookup table using the standard Maglev
// permutation algorithm. Returns a slice of backend IDs indexed by hash slot.
func (m *Manager) computeTable(backends []Backend) []uint32 {
	table := make([]uint32, m.tableSize)
	n := len(backends)

	if n == 0 {
		// Fill with a sentinel backend ID 0.
		for i := range table {
			table[i] = 0
		}
		return table
	}

	// Initialise table to "empty" sentinel.
	filled := make([]bool, m.tableSize)

	// Generate permutations for each backend.
	type perm struct {
		offset uint32
		skip   uint32
	}
	perms := make([]perm, n)

	for i, be := range backends {
		key := fmt.Sprintf("%s:%d#%d", be.Addr, be.Port, be.ID)

		h1 := hashKey(key + "#offset")
		h2 := hashKey(key + "#skip")

		perms[i] = perm{
			offset: uint32(h1 % uint64(m.tableSize)),
			skip:   uint32((h2 % uint64(m.tableSize-1)) + 1),
		}
	}

	// Fill the lookup table using Maglev's algorithm.
	next := make([]uint32, n)
	filledCount := uint32(0)

	for filledCount < m.tableSize {
		for i := 0; i < n; i++ {
			// Compute next candidate slot for this backend.
			c := (perms[i].offset + next[i]*perms[i].skip) % m.tableSize
			for filled[c] {
				next[i]++
				c = (perms[i].offset + next[i]*perms[i].skip) % m.tableSize
			}
			table[c] = backends[i].ID
			filled[c] = true
			next[i]++
			filledCount++
			if filledCount == m.tableSize {
				break
			}
		}
	}

	return table
}

// syncBackendMap reconciles the BPF backend hash map with the desired set
// of backends.
func (m *Manager) syncBackendMap(backends []Backend) error {
	// Build desired state.
	desired := make(map[BackendKey]BackendValue, len(backends))
	for _, be := range backends {
		addr, err := ipToBytes(be.Addr)
		if err != nil {
			m.logger.Warn("skipping backend with invalid IP",
				zap.String("addr", be.Addr),
				zap.Error(err))
			continue
		}
		desired[BackendKey{ID: be.ID}] = BackendValue{
			IP:   addr,
			Port: htons(be.Port),
		}
	}

	// Delete stale entries.
	var existKey BackendKey
	var existVal BackendValue
	toDelete := make([]BackendKey, 0)
	iter := m.backendMap.Iterate()
	for iter.Next(&existKey, &existVal) {
		if _, ok := desired[existKey]; !ok {
			toDelete = append(toDelete, existKey)
		}
	}
	for _, k := range toDelete {
		if err := m.backendMap.Delete(k); err != nil {
			novaebpf.RecordMapOp("maglev_backends", "delete", "error")
			m.logger.Warn("failed to delete stale backend", zap.Error(err))
		} else {
			novaebpf.RecordMapOp("maglev_backends", "delete", "ok")
		}
	}

	// Upsert desired entries.
	for k, v := range desired {
		if err := m.backendMap.Update(k, v, ebpf.UpdateAny); err != nil {
			novaebpf.RecordMapOp("maglev_backends", "update", "error")
			return fmt.Errorf("updating backend %d: %w", k.ID, err)
		}
		novaebpf.RecordMapOp("maglev_backends", "update", "ok")
	}

	return nil
}

// ipToBytes converts an IPv4 address string to a 4-byte array.
func ipToBytes(ip string) ([4]byte, error) {
	var addr [4]byte
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return addr, fmt.Errorf("invalid IP: %s", ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return addr, fmt.Errorf("not IPv4: %s", ip)
	}
	copy(addr[:], ip4)
	return addr, nil
}

// htons converts a uint16 from host to network byte order.
func htons(v uint16) uint16 {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	return binary.NativeEndian.Uint16(buf[:])
}

// hashKey hashes a string key to a uint64 using FNV-1a.
func hashKey(key string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return h.Sum64()
}
