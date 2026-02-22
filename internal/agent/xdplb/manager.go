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

package xdplb

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	novaebpf "github.com/piwi3910/novaedge/internal/agent/ebpf"
	"go.uber.org/zap"
)

const subsystem = "xdp"

// L4Route describes a VIP-to-backends mapping for XDP fast-path LB.
type L4Route struct {
	VIP      string // VIP IPv4 address
	Port     uint16
	Protocol uint8 // 6=TCP, 17=UDP
	Backends []Backend
}

// Backend is a single upstream endpoint for XDP LB.
type Backend struct {
	Addr string // IPv4 address
	Port uint16
	MAC  net.HardwareAddr // destination MAC for XDP_TX
}

// Manager manages the XDP L4 load balancing program lifecycle.
type Manager struct {
	logger     *zap.Logger
	loader     *novaebpf.ProgramLoader
	ifaceName  string
	mu         sync.RWMutex
	xdpLink    link.Link
	vipMap     *ebpf.Map
	backendMap *ebpf.Map
	statsMap   *ebpf.Map
	prog       *ebpf.Program
	nextListID uint32
}

// NewManager creates an XDP LB manager for the given network interface.
func NewManager(logger *zap.Logger, loader *novaebpf.ProgramLoader, iface string) *Manager {
	return &Manager{
		logger:    logger.With(zap.String("component", "xdp-lb")),
		loader:    loader,
		ifaceName: iface,
	}
}

// Start loads and attaches the XDP program to the configured interface.
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	start := time.Now()

	spec, err := loadXdpLb()
	if err != nil {
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("loading XDP LB BPF spec: %w", err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("creating XDP LB collection: %w", err)
	}

	prog := coll.Programs["xdp_lb_prog"]
	if prog == nil {
		coll.Close()
		return fmt.Errorf("xdp_lb_prog not found in BPF collection")
	}

	vipMap := coll.Maps["vip_backends"]
	if vipMap == nil {
		coll.Close()
		return fmt.Errorf("vip_backends map not found")
	}

	backendMap := coll.Maps["backend_list"]
	if backendMap == nil {
		coll.Close()
		return fmt.Errorf("backend_list map not found")
	}

	statsMap := coll.Maps["lb_stats"]

	// Attach XDP program to interface.
	iface, err := net.InterfaceByName(m.ifaceName)
	if err != nil {
		coll.Close()
		novaebpf.RecordError(subsystem, "attach")
		return fmt.Errorf("looking up interface %s: %w", m.ifaceName, err)
	}

	xdpLink, err := link.AttachXDP(link.XDPOptions{
		Program:   prog,
		Interface: iface.Index,
	})
	if err != nil {
		coll.Close()
		novaebpf.RecordError(subsystem, "attach")
		return fmt.Errorf("attaching XDP program to %s: %w", m.ifaceName, err)
	}

	m.prog = prog
	m.vipMap = vipMap
	m.backendMap = backendMap
	m.statsMap = statsMap
	m.xdpLink = xdpLink

	novaebpf.RecordProgramLoaded(subsystem)
	novaebpf.ObserveAttachDuration(subsystem, time.Since(start).Seconds())
	m.logger.Info("XDP LB program attached",
		zap.String("interface", m.ifaceName),
		zap.Int("ifindex", iface.Index))

	return nil
}

// Stop detaches the XDP program and releases all resources.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.xdpLink != nil {
		if err := m.xdpLink.Close(); err != nil {
			m.logger.Warn("failed to detach XDP program", zap.Error(err))
		}
		m.xdpLink = nil
	}
	if m.prog != nil {
		m.prog.Close()
		m.prog = nil
		novaebpf.RecordProgramUnloaded(subsystem)
	}
	if m.vipMap != nil {
		m.vipMap.Close()
		m.vipMap = nil
	}
	if m.backendMap != nil {
		m.backendMap.Close()
		m.backendMap = nil
	}
	if m.statsMap != nil {
		m.statsMap.Close()
		m.statsMap = nil
	}

	m.logger.Info("XDP LB stopped")
	return nil
}

// SyncBackends reconciles the BPF maps with the desired L4 routes.
// This performs a full sync: all existing entries are replaced.
func (m *Manager) SyncBackends(routes []L4Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.vipMap == nil || m.backendMap == nil {
		return fmt.Errorf("XDP LB not started")
	}

	// Clear existing entries by iterating and deleting.
	m.clearMaps()

	m.nextListID = 0
	for _, route := range routes {
		if err := m.addRoute(route); err != nil {
			m.logger.Warn("failed to add XDP LB route",
				zap.String("vip", route.VIP),
				zap.Uint16("port", route.Port),
				zap.Error(err))
		}
	}

	m.logger.Info("XDP LB backends synced", zap.Int("routes", len(routes)))
	return nil
}

// Stats returns per-CPU aggregated statistics from the XDP program.
func (m *Manager) Stats() map[string]uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]uint64)
	if m.statsMap == nil {
		return result
	}

	statNames := []string{
		"xdp_pass", "xdp_tx", "xdp_drop",
		"lookup_miss", "backend_miss",
		"packets_total", "bytes_total",
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

// IsRunning returns whether the XDP program is currently attached.
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.xdpLink != nil
}

// clearMaps removes all entries from vip_backends and backend_list maps.
func (m *Manager) clearMaps() {
	// Clear VIP map.
	var vk vipKey
	iter := m.vipMap.Iterate()
	var vm vipMeta
	keysToDelete := make([]vipKey, 0)
	for iter.Next(&vk, &vm) {
		keysToDelete = append(keysToDelete, vk)
	}
	for _, k := range keysToDelete {
		_ = m.vipMap.Delete(k)
	}

	// Clear backend list.
	var blk backendListKey
	var be backendEntry
	blKeysToDelete := make([]backendListKey, 0)
	blIter := m.backendMap.Iterate()
	for blIter.Next(&blk, &be) {
		blKeysToDelete = append(blKeysToDelete, blk)
	}
	for _, k := range blKeysToDelete {
		_ = m.backendMap.Delete(k)
	}
}

// addRoute adds a single VIP route with its backends to the BPF maps.
func (m *Manager) addRoute(route L4Route) error {
	vk, err := makeVIPKey(route.VIP, route.Port, route.Protocol)
	if err != nil {
		return err
	}

	listID := m.nextListID
	m.nextListID++

	vm := vipMeta{
		BackendCount:  uint32(len(route.Backends)),
		BackendListID: listID,
	}

	if err := m.vipMap.Update(vk, vm, ebpf.UpdateAny); err != nil {
		novaebpf.RecordMapOp("vip_backends", "update", "error")
		return fmt.Errorf("updating vip_backends: %w", err)
	}
	novaebpf.RecordMapOp("vip_backends", "update", "ok")

	for i, be := range route.Backends {
		blk := backendListKey{
			ListID: listID,
			Index:  uint32(i),
		}
		entry, err := makeBackendEntry(be)
		if err != nil {
			return fmt.Errorf("backend %d: %w", i, err)
		}
		if err := m.backendMap.Update(blk, entry, ebpf.UpdateAny); err != nil {
			novaebpf.RecordMapOp("backend_list", "update", "error")
			return fmt.Errorf("updating backend_list: %w", err)
		}
		novaebpf.RecordMapOp("backend_list", "update", "ok")
	}

	return nil
}

// BPF map key/value types matching C struct layout.

type vipKey struct {
	Addr  [4]byte
	Port  uint16
	Proto uint8
	Pad   uint8
}

type vipMeta struct {
	BackendCount  uint32
	BackendListID uint32
}

type backendListKey struct {
	ListID uint32
	Index  uint32
}

type backendEntry struct {
	Addr [4]byte
	Port uint16
	MAC  [6]byte
}

func makeVIPKey(ip string, port uint16, proto uint8) (vipKey, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return vipKey{}, fmt.Errorf("invalid VIP IP: %s", ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return vipKey{}, fmt.Errorf("not IPv4: %s", ip)
	}
	key := vipKey{
		Port:  htons(port),
		Proto: proto,
	}
	copy(key.Addr[:], ip4)
	return key, nil
}

func makeBackendEntry(be Backend) (backendEntry, error) {
	parsed := net.ParseIP(be.Addr)
	if parsed == nil {
		return backendEntry{}, fmt.Errorf("invalid backend IP: %s", be.Addr)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return backendEntry{}, fmt.Errorf("not IPv4: %s", be.Addr)
	}
	entry := backendEntry{
		Port: htons(be.Port),
	}
	copy(entry.Addr[:], ip4)
	if len(be.MAC) == 6 {
		copy(entry.MAC[:], be.MAC)
	}
	return entry, nil
}

func htons(v uint16) uint16 {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	return binary.NativeEndian.Uint16(buf[:])
}

// loadXdpLb loads the BPF collection spec from the embedded ELF.
// Placeholder until bpf2go generates the real loader.
func loadXdpLb() (*ebpf.CollectionSpec, error) {
	return nil, fmt.Errorf("BPF objects not generated yet; run 'go generate ./internal/agent/xdplb/'")
}
