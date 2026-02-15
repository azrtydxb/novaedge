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

package mesh

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// DefaultTPROXYPort is the default port for the transparent listener.
	DefaultTPROXYPort int32 = 15001

	// connectTimeout is the timeout for dialing the upstream backend.
	connectTimeout = 5 * time.Second
)

// ServiceTable maps ClusterIP:port to a list of backend endpoints.
type ServiceTable struct {
	mu       sync.RWMutex
	services map[string]*serviceEntry // key: "clusterIP:port"
}

type serviceEntry struct {
	endpoints []*pb.Endpoint
	lbPolicy  pb.LoadBalancingPolicy
	idx       uint64 // round-robin counter
}

// NewServiceTable creates an empty service routing table.
func NewServiceTable() *ServiceTable {
	return &ServiceTable{
		services: make(map[string]*serviceEntry),
	}
}

// Update replaces the routing table with the given internal services.
func (st *ServiceTable) Update(services []*pb.InternalService) {
	st.mu.Lock()
	defer st.mu.Unlock()

	newServices := make(map[string]*serviceEntry, len(services)*2)
	for _, svc := range services {
		for _, port := range svc.Ports {
			key := fmt.Sprintf("%s:%d", svc.ClusterIp, port.Port)
			newServices[key] = &serviceEntry{
				endpoints: svc.Endpoints,
				lbPolicy:  svc.LbPolicy,
			}
		}
	}
	st.services = newServices
}

// Lookup finds the service entry for a given original destination.
func (st *ServiceTable) Lookup(ip string, port int) (*pb.Endpoint, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	key := fmt.Sprintf("%s:%d", ip, port)
	entry, ok := st.services[key]
	if !ok || len(entry.endpoints) == 0 {
		return nil, false
	}

	// Filter to ready endpoints
	var ready []*pb.Endpoint
	for _, ep := range entry.endpoints {
		if ep.Ready {
			ready = append(ready, ep)
		}
	}
	if len(ready) == 0 {
		return nil, false
	}

	// Simple round-robin selection
	idx := entry.idx
	entry.idx++
	return ready[idx%uint64(len(ready))], true
}

// ServiceCount returns the number of services in the routing table.
func (st *ServiceTable) ServiceCount() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.services)
}

// Manager orchestrates the service mesh data plane components: TPROXY rule
// management, transparent listener, protocol detection, and service routing.
type Manager struct {
	logger       *zap.Logger
	tproxy       *TPROXYManager
	serviceTable *ServiceTable
	tproxyPort   int32
	cancel       context.CancelFunc
}

// NewManager creates a new mesh manager.
func NewManager(logger *zap.Logger, tproxyPort int32) *Manager {
	return &Manager{
		logger:       logger.Named("mesh"),
		tproxyPort:   tproxyPort,
		serviceTable: NewServiceTable(),
	}
}

// Start initializes the mesh data plane: sets up TPROXY iptables rules
// and starts the transparent listener.
func (m *Manager) Start(ctx context.Context) error {
	m.tproxy = NewTPROXYManager(m.logger, m.tproxyPort)

	if err := m.tproxy.Setup(); err != nil {
		return fmt.Errorf("TPROXY setup failed: %w", err)
	}

	listenerCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	listener := NewTransparentListener(m.logger, m.tproxyPort, m.handleConn)
	go func() {
		if err := listener.Start(listenerCtx); err != nil {
			m.logger.Error("Transparent listener stopped", zap.Error(err))
		}
	}()

	m.logger.Info("Mesh manager started", zap.Int32("tproxy_port", m.tproxyPort))
	return nil
}

// ApplyConfig updates the mesh routing table and TPROXY interception rules
// from a new set of InternalService entries.
func (m *Manager) ApplyConfig(services []*pb.InternalService) error {
	if m.tproxy == nil {
		return fmt.Errorf("mesh manager not started")
	}

	// Update service routing table
	m.serviceTable.Update(services)

	// Build TPROXY intercept targets from services
	var targets []InterceptTarget
	for _, svc := range services {
		if !svc.MeshEnabled {
			continue
		}
		for _, port := range svc.Ports {
			targets = append(targets, InterceptTarget{
				ClusterIP: svc.ClusterIp,
				Port:      port.Port,
			})
		}
	}

	// Reconcile iptables rules
	if err := m.tproxy.ApplyRules(targets); err != nil {
		return fmt.Errorf("failed to apply TPROXY rules: %w", err)
	}

	m.logger.Info("Mesh config applied",
		zap.Int("services", len(services)),
		zap.Int("intercept_rules", len(targets)),
		zap.Int("routing_entries", m.serviceTable.ServiceCount()))

	return nil
}

// Shutdown stops the mesh manager and cleans up all resources.
func (m *Manager) Shutdown(_ context.Context) error {
	if m.cancel != nil {
		m.cancel()
	}

	if m.tproxy != nil {
		if err := m.tproxy.Cleanup(); err != nil {
			m.logger.Error("TPROXY cleanup failed", zap.Error(err))
		}
	}

	m.logger.Info("Mesh manager stopped")
	return nil
}

// handleConn processes an intercepted connection by looking up the original
// destination in the service table, detecting the protocol, and proxying
// the traffic to a backend endpoint.
func (m *Manager) handleConn(ctx context.Context, conn net.Conn, origDst net.IP, origPort int) {
	defer func() { _ = conn.Close() }()

	// Look up the service
	endpoint, ok := m.serviceTable.Lookup(origDst.String(), origPort)
	if !ok {
		m.logger.Debug("No service found for intercepted connection, passing through",
			zap.String("dest", fmt.Sprintf("%s:%d", origDst, origPort)))
		// Dial the original destination directly (passthrough)
		m.proxyTCP(ctx, conn, origDst.String(), origPort)
		return
	}

	// Detect protocol
	proto, pc := DetectProtocol(conn)
	m.logger.Debug("Protocol detected",
		zap.String("protocol", string(proto)),
		zap.String("dest", fmt.Sprintf("%s:%d", origDst, origPort)),
		zap.String("backend", fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)))

	// For now, all protocols are proxied as L4 TCP.
	// HTTP-aware routing will be added in a future phase.
	m.proxyTCP(ctx, pc, endpoint.Address, int(endpoint.Port))
}

// proxyTCP establishes a connection to the backend and bidirectionally
// copies data between the client and backend connections.
func (m *Manager) proxyTCP(ctx context.Context, clientConn io.ReadWriteCloser, backendAddr string, backendPort int) {
	addr := net.JoinHostPort(backendAddr, fmt.Sprintf("%d", backendPort))
	dialer := &net.Dialer{Timeout: connectTimeout}
	backendConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		m.logger.Error("Failed to connect to backend",
			zap.String("backend", addr),
			zap.Error(err))
		return
	}
	defer func() { _ = backendConn.Close() }()

	// Bidirectional copy
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(backendConn, clientConn)
		done <- struct{}{}
	}()

	_, _ = io.Copy(clientConn, backendConn)
	<-done
}
