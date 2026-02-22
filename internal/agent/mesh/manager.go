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
	"google.golang.org/grpc"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// DefaultTPROXYPort is the default port for the transparent listener.
	DefaultTPROXYPort int32 = 15001

	// DefaultTunnelPort is the default port for the mTLS tunnel server.
	DefaultTunnelPort int32 = 15002

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
// management, transparent listener, protocol detection, service routing,
// mTLS tunnel server/client, and authorization policy enforcement.
type Manager struct {
	logger              *zap.Logger
	tproxy              *TPROXYManager
	serviceTable        *ServiceTable
	tproxyPort          int32
	tunnelPort          int32
	tunnelServer        *TunnelServer
	tunnelPool          *TunnelPool
	tlsProvider         *TLSProvider
	authorizer          *Authorizer
	trustDomain         string
	ruleBackendOverride RuleBackend
	cancel              context.CancelFunc
}

// ManagerConfig holds configuration for creating a mesh Manager.
type ManagerConfig struct {
	TPROXYPort  int32
	TunnelPort  int32
	TrustDomain string
	// Federation holds cross-cluster federation settings. May be nil when
	// federation is not active.
	Federation *FederationConfig
	// RuleBackendOverride, if non-nil, is used instead of the auto-detected
	// nftables/iptables backend. This is used by the eBPF sk_lookup backend
	// which is initialized before the mesh manager.
	RuleBackendOverride RuleBackend
}

// NewManager creates a new mesh manager with mTLS tunnel support.
func NewManager(logger *zap.Logger, cfg ManagerConfig) *Manager {
	namedLogger := logger.Named("mesh")
	trustDomain := cfg.TrustDomain
	if trustDomain == "" {
		trustDomain = "cluster.local"
	}
	return &Manager{
		logger:              namedLogger,
		tproxyPort:          cfg.TPROXYPort,
		tunnelPort:          cfg.TunnelPort,
		serviceTable:        NewServiceTable(),
		tlsProvider:         NewTLSProvider(namedLogger, trustDomain, cfg.Federation),
		authorizer:          NewAuthorizer(namedLogger),
		trustDomain:         trustDomain,
		ruleBackendOverride: cfg.RuleBackendOverride,
	}
}

// Start initializes the mesh data plane: sets up TPROXY interception rules,
// starts the transparent listener, and starts the mTLS tunnel server.
// If a RuleBackendOverride was provided via ManagerConfig (e.g. eBPF
// sk_lookup), it is used directly; otherwise auto-detection selects the
// best nftables/iptables backend.
func (m *Manager) Start(ctx context.Context) error {
	if m.ruleBackendOverride != nil {
		m.tproxy = NewTPROXYManagerWithBackend(m.logger, m.tproxyPort, m.ruleBackendOverride)
	} else {
		m.tproxy = NewTPROXYManager(m.logger, m.tproxyPort)
	}

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

	// Start tunnel server (will serve once TLS certificates are available).
	if m.tunnelPort > 0 {
		serverTLS := m.tlsProvider.ServerTLSConfig()
		m.tunnelServer = NewTunnelServer(m.logger, m.tunnelPort, serverTLS, m.authorizer, m.tlsProvider)
		go func() {
			if err := m.tunnelServer.Start(listenerCtx); err != nil {
				m.logger.Error("Tunnel server stopped", zap.Error(err))
			}
		}()

		// Create tunnel pool for outbound connections.
		clientTLS := m.tlsProvider.ClientTLSConfig()
		m.tunnelPool = NewTunnelPool(m.logger, clientTLS)

		m.logger.Info("Mesh tunnel server started", zap.Int32("tunnel_port", m.tunnelPort))
	}

	m.logger.Info("Mesh manager started",
		zap.Int32("tproxy_port", m.tproxyPort),
		zap.Int32("tunnel_port", m.tunnelPort),
		zap.String("trust_domain", m.trustDomain))
	return nil
}

// ApplyConfig updates the mesh routing table, TPROXY interception rules,
// and authorization policies from a config snapshot.
func (m *Manager) ApplyConfig(services []*pb.InternalService, authzPolicies []*pb.MeshAuthorizationPolicy) error {
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

	// Update authorization policies
	if m.authorizer != nil && authzPolicies != nil {
		m.authorizer.UpdatePolicies(authzPolicies)
	}

	m.logger.Info("Mesh config applied",
		zap.Int("services", len(services)),
		zap.Int("intercept_rules", len(targets)),
		zap.Int("routing_entries", m.serviceTable.ServiceCount()),
		zap.Int("authz_policies", len(authzPolicies)))

	return nil
}

// StartCertRequester launches a background goroutine that requests a mesh
// workload certificate from the controller and renews it before expiry.
func (m *Manager) StartCertRequester(ctx context.Context, nodeName string, conn *grpc.ClientConn) {
	cr := NewCertRequester(m.logger, nodeName, m.trustDomain, m.tlsProvider.fedCfg, m.UpdateTLSCertificate)
	go cr.Run(ctx, conn)
}

// UpdateTLSCertificate updates the mesh mTLS certificate material.
func (m *Manager) UpdateTLSCertificate(certPEM, keyPEM, caCertPEM []byte, spiffeID string) error {
	return m.tlsProvider.UpdateCertificate(certPEM, keyPEM, caCertPEM, spiffeID)
}

// HasTLSCertificate returns true if the mesh has a valid TLS certificate loaded.
func (m *Manager) HasTLSCertificate() bool {
	return m.tlsProvider.HasCertificate()
}

// TrustDomain returns the configured SPIFFE trust domain.
func (m *Manager) TrustDomain() string {
	return m.trustDomain
}

// Shutdown stops the mesh manager and cleans up all resources.
func (m *Manager) Shutdown(_ context.Context) error {
	if m.cancel != nil {
		m.cancel()
	}

	if m.tunnelPool != nil {
		m.tunnelPool.Close()
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
		if _, err := io.Copy(backendConn, clientConn); err != nil {
			m.logger.Debug("io.Copy client->backend finished with error", zap.Error(err))
		}
		done <- struct{}{}
	}()

	if _, err := io.Copy(clientConn, backendConn); err != nil {
		m.logger.Debug("io.Copy backend->client finished with error", zap.Error(err))
	}
	<-done
}
