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

package service

import (
	"fmt"
	"sync"

	"github.com/cilium/ebpf"
	novaebpf "github.com/piwi3910/novaedge/internal/agent/ebpf"
	"go.uber.org/zap"
)

const subsystem = "service"

// ServiceMap manages the eBPF service lookup maps used to accelerate
// service-to-backend resolution in the mesh data path. It maintains two
// levels of BPF maps:
//
//  1. A service map (BPF_MAP_TYPE_HASH) keyed by {ClusterIP, Port, Proto}
//     that stores service metadata (backend count, flags).
//
//  2. A backend array (BPF_MAP_TYPE_ARRAY) that stores the backend endpoint
//     list for all services in a flat array with per-service offsets.
//
// The BPF data-path programs use these maps for O(1) service lookup,
// avoiding the Go-side ServiceTable.Lookup() hot path for L4 decisions.
// For L7 traffic that requires Go processing, the BPF lookup provides
// service metadata that is attached to the connection context.
type ServiceMap struct {
	mu                    sync.Mutex
	logger                *zap.Logger
	serviceMap            *ebpf.Map // BPF_MAP_TYPE_HASH: ServiceKey -> ServiceValue
	backendArray          *ebpf.Map // BPF_MAP_TYPE_ARRAY: uint32 -> BackendInfo
	maxServices           uint32
	maxBackendsPerService uint32
	// nextBackendSlot tracks the next free slot in the flat backend array.
	// Each service is allocated a contiguous region of maxBackendsPerService
	// slots, so the offset for a new service is nextBackendSlot.
	nextBackendSlot uint32
	// serviceSlots maps service keys to their starting offset in the backend array.
	serviceSlots map[ServiceKey]uint32
	closed       bool
}

// NewServiceMap creates a new eBPF service lookup map manager. The
// maxServices parameter sets the maximum number of services that can be
// tracked, and maxBackendsPerService sets the maximum number of backends
// per service.
//
// The service map uses BPF_MAP_TYPE_HASH for shared-state lookups (not
// per-CPU) because the map is written by the Go control plane and read
// by BPF programs. Per-CPU maps would require the Go side to update all
// CPU copies, which is unnecessary overhead for a read-heavy workload.
// The backend array uses BPF_MAP_TYPE_ARRAY for O(1) indexed lookups
// by the BPF program.
func NewServiceMap(logger *zap.Logger, maxServices, maxBackendsPerService uint32) (*ServiceMap, error) {
	namedLogger := logger.With(zap.String("component", "ebpf-service-map"))

	if maxServices == 0 {
		maxServices = 4096
	}
	if maxBackendsPerService == 0 {
		maxBackendsPerService = 256
	}

	// Create the service lookup hash map.
	svcSpec := &ebpf.MapSpec{
		Name:       "novaedge_svc_map",
		Type:       ebpf.Hash,
		KeySize:    uint32(8), // sizeof(ServiceKey): 4+2+1+1 = 8
		ValueSize:  uint32(8), // sizeof(ServiceValue): 4+4 = 8
		MaxEntries: maxServices,
	}
	svcMap, err := ebpf.NewMap(svcSpec)
	if err != nil {
		novaebpf.RecordError(subsystem, "map_create")
		return nil, fmt.Errorf("creating service map: %w", err)
	}

	// Create the backend array. Total capacity is maxServices * maxBackendsPerService.
	totalBackendSlots := maxServices * maxBackendsPerService
	backendSpec := &ebpf.MapSpec{
		Name:       "novaedge_backend_arr",
		Type:       ebpf.Array,
		KeySize:    uint32(4),  // uint32 index
		ValueSize:  uint32(12), // sizeof(BackendInfo): 4+2+2+1+1+2 = 12
		MaxEntries: totalBackendSlots,
	}
	backendArr, err := ebpf.NewMap(backendSpec)
	if err != nil {
		svcMap.Close()
		novaebpf.RecordError(subsystem, "map_create")
		return nil, fmt.Errorf("creating backend array: %w", err)
	}

	novaebpf.RecordProgramLoaded(subsystem)
	namedLogger.Info("eBPF service map initialized",
		zap.Uint32("max_services", maxServices),
		zap.Uint32("max_backends_per_service", maxBackendsPerService),
		zap.Uint32("total_backend_slots", totalBackendSlots))

	return &ServiceMap{
		logger:                namedLogger,
		serviceMap:            svcMap,
		backendArray:          backendArr,
		maxServices:           maxServices,
		maxBackendsPerService: maxBackendsPerService,
		serviceSlots:          make(map[ServiceKey]uint32),
	}, nil
}

// UpsertService adds or updates a service entry in the BPF maps. The
// service key identifies the ClusterIP:port:proto tuple, and the backends
// slice contains the endpoint list. If the service already exists, its
// backend list is replaced in-place.
//
// The backends are written to a contiguous region of the backend array
// at the service's assigned offset. If a service is new, the next
// available region is allocated. The ServiceValue in the service map
// is updated to reflect the new backend count and flags.
func (sm *ServiceMap) UpsertService(key ServiceKey, backends []BackendInfo) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.closed {
		return fmt.Errorf("service map is closed")
	}

	if uint32(len(backends)) > sm.maxBackendsPerService {
		return fmt.Errorf("backend count %d exceeds max %d per service",
			len(backends), sm.maxBackendsPerService)
	}

	// Determine the backend array offset for this service.
	offset, exists := sm.serviceSlots[key]
	if !exists {
		// Allocate a new slot region.
		offset = sm.nextBackendSlot
		if offset+sm.maxBackendsPerService > sm.maxServices*sm.maxBackendsPerService {
			return fmt.Errorf("backend array is full (no free slots)")
		}
		sm.serviceSlots[key] = offset
		sm.nextBackendSlot = offset + sm.maxBackendsPerService
	}

	// Write backends to the array. Zero out unused slots.
	for i := uint32(0); i < sm.maxBackendsPerService; i++ {
		idx := offset + i
		var info BackendInfo
		if i < uint32(len(backends)) {
			info = backends[i]
		}
		if err := sm.backendArray.Update(idx, info, ebpf.UpdateAny); err != nil {
			novaebpf.RecordMapOp("backend_array", "update", "error")
			return fmt.Errorf("updating backend array at index %d: %w", idx, err)
		}
		novaebpf.RecordMapOp("backend_array", "update", "ok")
	}

	// Build flags from backend info.
	var flags uint32
	for _, b := range backends {
		if b.NodeLocal == 1 {
			flags |= FlagMeshEnabled
		}
	}

	// Update the service map entry.
	svcVal := ServiceValue{
		BackendCount: uint32(len(backends)),
		Flags:        flags,
	}
	if err := sm.serviceMap.Update(key, svcVal, ebpf.UpdateAny); err != nil {
		novaebpf.RecordMapOp("service_map", "update", "error")
		return fmt.Errorf("updating service map: %w", err)
	}
	novaebpf.RecordMapOp("service_map", "update", "ok")

	sm.logger.Debug("Service upserted in BPF maps",
		zap.Binary("ip", key.IP[:]),
		zap.Uint16("port", key.Port),
		zap.Uint8("proto", key.Proto),
		zap.Int("backends", len(backends)),
		zap.Uint32("array_offset", offset))

	return nil
}

// DeleteService removes a service and its backends from the BPF maps.
// The backend array slots are zeroed out but not reclaimed (the slot
// region remains allocated). For long-running agents, periodic
// compaction via a full reconciliation pass is recommended.
func (sm *ServiceMap) DeleteService(key ServiceKey) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.closed {
		return fmt.Errorf("service map is closed")
	}

	// Remove from the service map.
	if err := sm.serviceMap.Delete(key); err != nil {
		novaebpf.RecordMapOp("service_map", "delete", "error")
		return fmt.Errorf("deleting service from BPF map: %w", err)
	}
	novaebpf.RecordMapOp("service_map", "delete", "ok")

	// Zero out the backend slots.
	if offset, exists := sm.serviceSlots[key]; exists {
		emptyBackend := BackendInfo{}
		for i := uint32(0); i < sm.maxBackendsPerService; i++ {
			idx := offset + i
			if err := sm.backendArray.Update(idx, emptyBackend, ebpf.UpdateAny); err != nil {
				novaebpf.RecordMapOp("backend_array", "update", "error")
				sm.logger.Warn("failed to zero backend array slot", zap.Uint32("index", idx), zap.Error(err))
			} else {
				novaebpf.RecordMapOp("backend_array", "update", "ok")
			}
		}
		delete(sm.serviceSlots, key)
	}

	sm.logger.Debug("Service deleted from BPF maps",
		zap.Binary("ip", key.IP[:]),
		zap.Uint16("port", key.Port))

	return nil
}

// Reconcile performs a full reconciliation of the BPF service map to match
// the desired state. Services not in the desired set are removed; new
// services are added; existing services are updated. This is the preferred
// method for config snapshot application as it handles additions, updates,
// and deletions atomically.
func (sm *ServiceMap) Reconcile(desired map[ServiceKey][]BackendInfo) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.closed {
		return fmt.Errorf("service map is closed")
	}

	// Collect existing keys.
	existingKeys := make(map[ServiceKey]struct{})
	var cursor ServiceKey
	var val ServiceValue
	iter := sm.serviceMap.Iterate()
	for iter.Next(&cursor, &val) {
		keyCopy := cursor
		existingKeys[keyCopy] = struct{}{}
	}

	// Delete stale services (temporarily release lock semantics are
	// handled by the fact that this entire method holds the lock).
	deleted := 0
	for k := range existingKeys {
		if _, want := desired[k]; !want {
			// Inline delete (can't call DeleteService which also locks).
			if err := sm.serviceMap.Delete(k); err != nil {
				novaebpf.RecordMapOp("service_map", "delete", "error")
				sm.logger.Warn("failed to delete stale service", zap.Error(err))
			} else {
				novaebpf.RecordMapOp("service_map", "delete", "ok")
				deleted++
			}
			if offset, exists := sm.serviceSlots[k]; exists {
				emptyBackend := BackendInfo{}
				for i := uint32(0); i < sm.maxBackendsPerService; i++ {
					idx := offset + i
					_ = sm.backendArray.Update(idx, emptyBackend, ebpf.UpdateAny)
				}
				delete(sm.serviceSlots, k)
			}
		}
	}

	// Upsert desired services (inline to avoid double-locking).
	upserted := 0
	for key, backends := range desired {
		if uint32(len(backends)) > sm.maxBackendsPerService {
			sm.logger.Warn("backend count exceeds max, truncating",
				zap.Int("count", len(backends)),
				zap.Uint32("max", sm.maxBackendsPerService))
			backends = backends[:sm.maxBackendsPerService]
		}

		offset, exists := sm.serviceSlots[key]
		if !exists {
			offset = sm.nextBackendSlot
			if offset+sm.maxBackendsPerService > sm.maxServices*sm.maxBackendsPerService {
				return fmt.Errorf("backend array is full")
			}
			sm.serviceSlots[key] = offset
			sm.nextBackendSlot = offset + sm.maxBackendsPerService
		}

		for i := uint32(0); i < sm.maxBackendsPerService; i++ {
			idx := offset + i
			var info BackendInfo
			if i < uint32(len(backends)) {
				info = backends[i]
			}
			if err := sm.backendArray.Update(idx, info, ebpf.UpdateAny); err != nil {
				novaebpf.RecordMapOp("backend_array", "update", "error")
				return fmt.Errorf("updating backend array at index %d: %w", idx, err)
			}
			novaebpf.RecordMapOp("backend_array", "update", "ok")
		}

		var flags uint32
		for _, b := range backends {
			if b.NodeLocal == 1 {
				flags |= FlagMeshEnabled
			}
		}

		svcVal := ServiceValue{
			BackendCount: uint32(len(backends)),
			Flags:        flags,
		}
		if err := sm.serviceMap.Update(key, svcVal, ebpf.UpdateAny); err != nil {
			novaebpf.RecordMapOp("service_map", "update", "error")
			return fmt.Errorf("updating service map: %w", err)
		}
		novaebpf.RecordMapOp("service_map", "update", "ok")
		upserted++
	}

	sm.logger.Info("eBPF service map reconciled",
		zap.Int("active", len(desired)),
		zap.Int("deleted", deleted),
		zap.Int("upserted", upserted))

	return nil
}

// ServiceMapFD returns the file descriptor of the underlying BPF service
// map for use by BPF program attachment.
func (sm *ServiceMap) ServiceMapFD() *ebpf.Map {
	return sm.serviceMap
}

// BackendArrayFD returns the file descriptor of the underlying BPF backend
// array for use by BPF program attachment.
func (sm *ServiceMap) BackendArrayFD() *ebpf.Map {
	return sm.backendArray
}

// Close releases all BPF map resources.
func (sm *ServiceMap) Close() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.closed {
		return nil
	}
	sm.closed = true

	var firstErr error
	if sm.serviceMap != nil {
		if err := sm.serviceMap.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing service map: %w", err)
		}
		sm.serviceMap = nil
	}
	if sm.backendArray != nil {
		if err := sm.backendArray.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing backend array: %w", err)
		}
		sm.backendArray = nil
	}

	sm.serviceSlots = nil
	novaebpf.RecordProgramUnloaded(subsystem)
	sm.logger.Info("eBPF service map closed")
	return firstErr
}
