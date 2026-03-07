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

package sockmap

import (
	"errors"
	"fmt"
	"net"
	"sync"

	novaebpf "github.com/azrtydxb/novaedge/internal/agent/ebpf"
	"github.com/cilium/ebpf"
	"go.uber.org/zap"
)

const subsystem = "sockmap"

var (
	errManagerClosed = errors.New("sockmap manager is closed")
	errNotIPv4       = errors.New("not an IPv4 address")
)

// Manager manages the eBPF SOCKHASH and endpoint maps used for same-node
// mesh traffic bypass. When both source and destination pods are on the
// same node, the BPF sock_ops program captures socket establishment events
// and inserts the socket into a SOCKHASH map. The BPF sk_msg program then
// redirects sendmsg() calls directly between the paired sockets, bypassing
// the entire TCP/IP stack for dramatically reduced latency and CPU usage.
//
// The Go-side Manager maintains the endpoint_map which tells the BPF
// programs which endpoints are eligible for SOCKMAP redirection. Only
// endpoints that are local to this node and whose mesh policy permits
// bypass are registered.
type Manager struct {
	mu          sync.Mutex
	logger      *zap.Logger
	endpointMap *ebpf.Map // BPF_MAP_TYPE_HASH: EndpointKey -> EndpointValue
	sockHash    *ebpf.Map // BPF_MAP_TYPE_SOCKHASH: SockKey -> socket FD
	statsMap    *ebpf.Map // BPF_MAP_TYPE_ARRAY: uint32 -> uint64 (redirected, fallback counters)
	closed      bool
}

// NewSockMapManager creates a new SOCKMAP manager for same-node mesh traffic
// acceleration. It creates the BPF maps required by the sock_ops and sk_msg
// programs. The BPF programs themselves are loaded and attached separately
// (via bpf2go or the ProgramLoader); this manager provides the userspace
// control plane for managing the maps.
//
// The manager creates three BPF maps:
//   - sock_hash (SOCKHASH): populated by the BPF sock_ops program when sockets
//     are established; used by the BPF sk_msg program for message redirection
//   - endpoint_map (HASH): populated by this manager with same-node endpoints;
//     consulted by BPF programs to decide whether to redirect
//   - stats_map (ARRAY): per-entry counters for redirected and fallback packets;
//     read by GetStats() for observability
func NewSockMapManager(logger *zap.Logger) (*Manager, error) {
	namedLogger := logger.With(zap.String("component", "ebpf-sockmap"))

	// Create the SOCKHASH map for socket-to-socket redirection.
	sockHashSpec := &ebpf.MapSpec{
		Name:       "novaedge_sock_hash",
		Type:       ebpf.SockHash,
		KeySize:    uint32(24), // sizeof(SockKey): 4+4+4+4+4 = 20, padded to 24
		ValueSize:  uint32(4),  // socket cookie / FD reference
		MaxEntries: 65536,
	}
	sockHash, err := ebpf.NewMap(sockHashSpec)
	if err != nil {
		novaebpf.RecordError(subsystem, "map_create")
		return nil, fmt.Errorf("creating SOCKHASH map: %w", err)
	}

	// Create the endpoint eligibility map.
	endpointSpec := &ebpf.MapSpec{
		Name:       "novaedge_endpoint_map",
		Type:       ebpf.Hash,
		KeySize:    uint32(8), // sizeof(EndpointKey): 4+2+2 = 8
		ValueSize:  uint32(4), // sizeof(EndpointValue): 4
		MaxEntries: 4096,
	}
	endpointMap, err := ebpf.NewMap(endpointSpec)
	if err != nil {
		_ = sockHash.Close()
		novaebpf.RecordError(subsystem, "map_create")
		return nil, fmt.Errorf("creating endpoint map: %w", err)
	}

	// Create the stats counter array (2 entries: redirected, fallback).
	statsSpec := &ebpf.MapSpec{
		Name:       "novaedge_sockmap_stats",
		Type:       ebpf.Array,
		KeySize:    uint32(4), // uint32 index
		ValueSize:  uint32(8), // uint64 counter
		MaxEntries: 2,
	}
	statsMap, err := ebpf.NewMap(statsSpec)
	if err != nil {
		_ = sockHash.Close()
		_ = endpointMap.Close()
		novaebpf.RecordError(subsystem, "map_create")
		return nil, fmt.Errorf("creating stats map: %w", err)
	}

	novaebpf.RecordProgramLoaded(subsystem)
	namedLogger.Info("SOCKMAP manager initialized",
		zap.Int("sock_hash_max", 65536),
		zap.Int("endpoint_map_max", 4096))

	return &Manager{
		logger:      namedLogger,
		endpointMap: endpointMap,
		sockHash:    sockHash,
		statsMap:    statsMap,
	}, nil
}

// AddSameNodeEndpoint marks an endpoint (IP:port) as eligible for SOCKMAP
// redirection. This should be called for endpoints whose pod is running
// on the same node as this agent and whose mesh policy permits same-node
// bypass (i.e., the traffic does not require L7 inspection).
func (m *Manager) AddSameNodeEndpoint(ip net.IP, port uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errManagerClosed
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return fmt.Errorf("%w: %s", errNotIPv4, ip)
	}

	key := EndpointKey{
		Port: port,
	}
	copy(key.Addr[:], ip4)

	val := EndpointValue{Eligible: 1}

	if err := m.endpointMap.Update(key, val, ebpf.UpdateAny); err != nil {
		novaebpf.RecordMapOp("endpoint_map", "update", "error")
		return fmt.Errorf("adding same-node endpoint %s:%d: %w", ip, port, err)
	}

	novaebpf.RecordMapOp("endpoint_map", "update", "ok")
	m.logger.Debug("Added same-node endpoint for SOCKMAP bypass",
		zap.String("ip", ip.String()),
		zap.Uint16("port", port))
	return nil
}

// RemoveSameNodeEndpoint removes an endpoint from the SOCKMAP eligibility
// map. After this call, new connections to this endpoint will not be
// redirected via SOCKMAP (existing redirected connections are not affected).
func (m *Manager) RemoveSameNodeEndpoint(ip net.IP, port uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errManagerClosed
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return fmt.Errorf("%w: %s", errNotIPv4, ip)
	}

	key := EndpointKey{
		Port: port,
	}
	copy(key.Addr[:], ip4)

	if err := m.endpointMap.Delete(key); err != nil {
		novaebpf.RecordMapOp("endpoint_map", "delete", "error")
		return fmt.Errorf("removing same-node endpoint %s:%d: %w", ip, port, err)
	}

	novaebpf.RecordMapOp("endpoint_map", "delete", "ok")
	m.logger.Debug("Removed same-node endpoint from SOCKMAP bypass",
		zap.String("ip", ip.String()),
		zap.Uint16("port", port))
	return nil
}

// SyncEndpoints reconciles the endpoint map to match the desired set of
// same-node endpoints. Endpoints not in the desired set are removed;
// new endpoints are added. This is called during config snapshot application.
func (m *Manager) SyncEndpoints(endpoints map[EndpointKey]EndpointValue) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errManagerClosed
	}

	// Collect existing keys for deletion pass.
	existing := make(map[EndpointKey]struct{})
	var cursor EndpointKey
	var val EndpointValue
	iter := m.endpointMap.Iterate()
	for iter.Next(&cursor, &val) {
		keyCopy := cursor
		existing[keyCopy] = struct{}{}
	}

	// Delete stale entries.
	deleted := 0
	for k := range existing {
		if _, want := endpoints[k]; !want {
			if err := m.endpointMap.Delete(k); err != nil {
				novaebpf.RecordMapOp("endpoint_map", "delete", "error")
				m.logger.Warn("failed to delete stale endpoint from SOCKMAP map", zap.Error(err))
			} else {
				novaebpf.RecordMapOp("endpoint_map", "delete", "ok")
				deleted++
			}
		}
	}

	// Upsert desired entries.
	upserted := 0
	for k, v := range endpoints {
		if err := m.endpointMap.Update(k, v, ebpf.UpdateAny); err != nil {
			novaebpf.RecordMapOp("endpoint_map", "update", "error")
			return fmt.Errorf("updating endpoint map: %w", err)
		}
		novaebpf.RecordMapOp("endpoint_map", "update", "ok")
		upserted++
	}

	m.logger.Info("SOCKMAP endpoint map reconciled",
		zap.Int("active", len(endpoints)),
		zap.Int("deleted", deleted),
		zap.Int("upserted", upserted))

	return nil
}

// GetStats reads the BPF stats counters for redirected and fallback packets.
// These counters are incremented by the BPF sk_msg program: "redirected"
// counts messages successfully redirected via SOCKHASH, "fallback" counts
// messages that fell through to normal TCP delivery.
func (m *Manager) GetStats() (redirected, fallback uint64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, 0, errManagerClosed
	}

	var val uint64

	if err := m.statsMap.Lookup(StatsKeyRedirected, &val); err != nil {
		return 0, 0, fmt.Errorf("reading redirected counter: %w", err)
	}
	redirected = val

	if err := m.statsMap.Lookup(StatsKeyFallback, &val); err != nil {
		return redirected, 0, fmt.Errorf("reading fallback counter: %w", err)
	}
	fallback = val

	return redirected, fallback, nil
}

// SockHashMap returns the underlying SOCKHASH map for use by BPF program
// attachment. The BPF sock_ops program needs this map FD to insert sockets,
// and the sk_msg program needs it for message redirection.
func (m *Manager) SockHashMap() *ebpf.Map {
	return m.sockHash
}

// EndpointMap returns the underlying endpoint eligibility map for use by
// BPF program attachment.
func (m *Manager) EndpointMap() *ebpf.Map {
	return m.endpointMap
}

// StatsMap returns the underlying stats counter map for use by BPF program
// attachment.
func (m *Manager) StatsMap() *ebpf.Map {
	return m.statsMap
}

// Close releases all BPF map resources. After Close(), the BPF programs
// that reference these maps will continue to function until they are
// detached, but no new userspace operations can be performed.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true

	var firstErr error
	if m.sockHash != nil {
		if err := m.sockHash.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing SOCKHASH map: %w", err)
		}
		m.sockHash = nil
	}
	if m.endpointMap != nil {
		if err := m.endpointMap.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing endpoint map: %w", err)
		}
		m.endpointMap = nil
	}
	if m.statsMap != nil {
		if err := m.statsMap.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing stats map: %w", err)
		}
		m.statsMap = nil
	}

	novaebpf.RecordProgramUnloaded(subsystem)
	m.logger.Info("SOCKMAP manager closed")
	return firstErr
}
