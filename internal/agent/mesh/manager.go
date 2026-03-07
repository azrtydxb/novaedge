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
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/azrtydxb/novaedge/internal/agent/ebpf/sockmap"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

var (
	errMeshManagerNotStarted = errors.New("mesh manager not started")
)

// safePortToUint16 converts an int32 port to uint16 with bounds checking.
func safePortToUint16(port int32) uint16 {
	if port < 0 || port > 65535 {
		return 0
	}
	return uint16(port)
}

const (
	// DefaultTPROXYPort is the default port for the transparent listener.
	DefaultTPROXYPort int32 = 15001

	// DefaultTunnelPort is the default port for the mTLS tunnel server.
	DefaultTunnelPort int32 = 15002

	// connectTimeout is the timeout for dialing the upstream backend.
	connectTimeout = 5 * time.Second
)

// serviceEntry holds the pre-computed ready endpoints for a service.
// The ready list is computed at config-apply time so lookups at request
// time do not need to filter under a lock.
type serviceEntry struct {
	ready    []*pb.Endpoint // pre-filtered ready endpoints
	lbPolicy pb.LoadBalancingPolicy
	idx      atomic.Uint64 // round-robin counter (atomic for concurrent Lookup)
}

// ServiceTable maps ClusterIP:port to a list of backend endpoints.
// It uses atomic.Pointer for lock-free reads on the hot path.
type ServiceTable struct {
	services atomic.Pointer[map[string]*serviceEntry]
	mu       sync.Mutex // protects writes only
}

// NewServiceTable creates an empty service routing table.
func NewServiceTable() *ServiceTable {
	st := &ServiceTable{}
	empty := make(map[string]*serviceEntry)
	st.services.Store(&empty)
	return st
}

// Update replaces the routing table with the given internal services.
// Ready endpoints are pre-computed here so Lookup() avoids filtering.
func (st *ServiceTable) Update(services []*pb.InternalService) {
	newServices := make(map[string]*serviceEntry, len(services)*2)
	for _, svc := range services {
		// Pre-compute the ready endpoint list once.
		var ready []*pb.Endpoint
		for _, ep := range svc.Endpoints {
			if ep.Ready {
				ready = append(ready, ep)
			}
		}
		for _, port := range svc.Ports {
			key := fmt.Sprintf("%s:%d", svc.ClusterIp, port.Port)
			newServices[key] = &serviceEntry{
				ready:    ready,
				lbPolicy: svc.LbPolicy,
			}
		}
	}
	st.mu.Lock()
	st.services.Store(&newServices)
	st.mu.Unlock()
}

// Lookup finds the service entry for a given original destination.
// It reads the pre-computed ready endpoint list without filtering,
// avoiding lock contention on the hot path.
func (st *ServiceTable) Lookup(ip string, port int) (*pb.Endpoint, bool) {
	svcMap := *st.services.Load()
	key := fmt.Sprintf("%s:%d", ip, port)
	entry, ok := svcMap[key]
	if !ok || len(entry.ready) == 0 {
		return nil, false
	}

	// Atomic round-robin selection over pre-computed ready endpoints.
	idx := entry.idx.Add(1) - 1
	return entry.ready[idx%uint64(len(entry.ready))], true
}

// ServiceCount returns the number of services in the routing table.
func (st *ServiceTable) ServiceCount() int {
	svcMap := *st.services.Load()
	return len(svcMap)
}

// Manager orchestrates the service mesh data plane components: TPROXY rule
// management, transparent listener, protocol detection, service routing,
// mTLS tunnel server/client, and authorization policy enforcement.
//
// When eBPF acceleration is available (Linux with appropriate kernel support),
// the manager also integrates:
//   - SOCKMAP/sk_msg for same-node socket-to-socket redirection (#631)
//   - eBPF service lookup maps for O(1) service resolution (#633)
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

	// nodeIP is the IP address of this node, used for identifying
	// same-node endpoints eligible for SOCKMAP bypass.
	nodeIP string

	// sockMapMgr is the eBPF SOCKMAP manager for same-node traffic
	// acceleration. May be nil if eBPF SOCKMAP is not available.
	sockMapMgr *sockmap.Manager
}

// ManagerConfig holds configuration for creating a mesh Manager.
type ManagerConfig struct {
	TPROXYPort  int32
	TunnelPort  int32
	TrustDomain string
	// NodeIP is the IP address of this node. Used for identifying same-node
	// endpoints that are eligible for SOCKMAP bypass.
	NodeIP string
	// Federation holds cross-cluster federation settings. May be nil when
	// federation is not active.
	Federation *FederationConfig
	// RuleBackendOverride, if non-nil, is used instead of the auto-detected
	// nftables/iptables backend. This is used by the eBPF sk_lookup backend
	// which is initialized before the mesh manager.
	RuleBackendOverride RuleBackend
	// SockMapManager, if non-nil, enables eBPF SOCKMAP-based same-node
	// traffic acceleration. The caller is responsible for creating the
	// manager (typically after eBPF capability detection).
	SockMapManager *sockmap.Manager
}

// NewManager creates a new mesh manager with mTLS tunnel support.
func NewManager(logger *zap.Logger, cfg ManagerConfig) *Manager {
	namedLogger := logger.Named("mesh")
	trustDomain := cfg.TrustDomain
	if trustDomain == "" {
		trustDomain = "cluster.local"
	}
	m := &Manager{
		logger:              namedLogger,
		tproxyPort:          cfg.TPROXYPort,
		tunnelPort:          cfg.TunnelPort,
		serviceTable:        NewServiceTable(),
		tlsProvider:         NewTLSProvider(namedLogger, trustDomain, cfg.Federation),
		authorizer:          NewAuthorizer(namedLogger),
		trustDomain:         trustDomain,
		ruleBackendOverride: cfg.RuleBackendOverride,
		nodeIP:              cfg.NodeIP,
		sockMapMgr:          cfg.SockMapManager,
	}

	if m.sockMapMgr != nil {
		namedLogger.Info("eBPF SOCKMAP acceleration enabled for same-node traffic")
	}

	return m
}

// ListenerRegistrar is implemented by backends that need the listener
// socket's file descriptor (e.g., eBPF SOCKMAP).
type ListenerRegistrar interface {
	SetListenerFD(fd int) error
}

// Start initializes the mesh data plane: sets up TPROXY interception rules,
// starts the transparent listener, and starts the mTLS tunnel server.
// If a RuleBackendOverride was provided via ManagerConfig (e.g. eBPF
// sk_lookup), it is used directly; otherwise auto-detection selects the
// best nftables/iptables backend.
//
// All spawned goroutines share the same context. If any component fails to
// start, the context is cancelled to stop all others, preventing goroutine
// leaks on error paths.
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

	// Use errgroup to track all spawned goroutines. If any goroutine
	// returns an error, the shared context is cancelled to stop all others.
	eg, egCtx := errgroup.WithContext(listenerCtx)

	listener := NewTransparentListener(m.logger, m.tproxyPort, m.handleConn)

	// If the backend needs the listener socket FD (eBPF SOCKMAP), create
	// the listener first, register the FD, then start the accept loop.
	if registrar, ok := m.ruleBackendOverride.(ListenerRegistrar); ok {
		tcpListener, err := listener.CreateListener(egCtx)
		if err != nil {
			cancel()
			return fmt.Errorf("creating listener for eBPF registration: %w", err)
		}

		if tcpLn, ok := tcpListener.(*net.TCPListener); ok {
			file, err := tcpLn.File()
			if err != nil {
				_ = tcpListener.Close()
				cancel()
				return fmt.Errorf("getting listener file descriptor: %w", err)
			}
			fd := int(file.Fd()) //nolint:gosec // G115: file descriptor conversion is safe on supported 64-bit platforms
			if err := registrar.SetListenerFD(fd); err != nil {
				_ = file.Close()
				_ = tcpListener.Close()
				cancel()
				return fmt.Errorf("registering listener FD with eBPF backend: %w", err)
			}
			_ = file.Close()
		}

		listener.SetListener(tcpListener)
	}

	eg.Go(func() error {
		if err := listener.Start(egCtx); err != nil {
			m.logger.Error("Transparent listener stopped", zap.Error(err))
			return fmt.Errorf("transparent listener: %w", err)
		}
		return nil
	})

	// Start tunnel server (will serve once TLS certificates are available).
	if m.tunnelPort > 0 {
		serverTLS := m.tlsProvider.ServerTLSConfig()
		m.tunnelServer = NewTunnelServer(m.logger, m.tunnelPort, serverTLS, m.authorizer, m.tlsProvider)
		eg.Go(func() error {
			if err := m.tunnelServer.Start(egCtx); err != nil {
				m.logger.Error("Tunnel server stopped", zap.Error(err))
				return fmt.Errorf("tunnel server: %w", err)
			}
			return nil
		})

		// Create tunnel pool for outbound connections.
		clientTLS := m.tlsProvider.ClientTLSConfig()
		m.tunnelPool = NewTunnelPool(m.logger, clientTLS)

		m.logger.Info("Mesh tunnel server started", zap.Int32("tunnel_port", m.tunnelPort))
	}

	// Monitor the errgroup in a background goroutine so that if any
	// component fails, all others are cancelled.
	go func() {
		if err := eg.Wait(); err != nil {
			m.logger.Error("Mesh component failed, stopping all components", zap.Error(err))
			cancel()
		}
	}()

	m.logger.Info("Mesh manager started",
		zap.Int32("tproxy_port", m.tproxyPort),
		zap.Int32("tunnel_port", m.tunnelPort),
		zap.String("trust_domain", m.trustDomain))
	return nil
}

// ApplyConfig updates the mesh routing table, TPROXY interception rules,
// authorization policies, and eBPF acceleration maps from a config snapshot.
func (m *Manager) ApplyConfig(services []*pb.InternalService, authzPolicies []*pb.MeshAuthorizationPolicy) error {
	if m.tproxy == nil {
		return errMeshManagerNotStarted
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

	// Populate eBPF SOCKMAP endpoint map with same-node endpoints (#631).
	// Only endpoints whose pod IP is on this node are eligible for
	// SOCKMAP bypass, which redirects data directly between sockets
	// without traversing the full TCP/IP stack.
	if m.sockMapMgr != nil {
		m.reconcileSockMapEndpoints(services)
	}

	m.logger.Info("Mesh config applied",
		zap.Int("services", len(services)),
		zap.Int("intercept_rules", len(targets)),
		zap.Int("routing_entries", m.serviceTable.ServiceCount()),
		zap.Int("authz_policies", len(authzPolicies)),
		zap.Bool("sockmap_enabled", m.sockMapMgr != nil))

	return nil
}

// reconcileSockMapEndpoints identifies same-node endpoints from the service
// list and updates the eBPF SOCKMAP endpoint map. An endpoint is considered
// "same-node" if it matches the node's IP or is on the same node's pod CIDR.
// For simplicity, we currently check all ready endpoints and mark those
// that the BPF program should try to shortcircuit.
func (m *Manager) reconcileSockMapEndpoints(services []*pb.InternalService) {
	desired := make(map[sockmap.EndpointKey]sockmap.EndpointValue)
	for _, svc := range services {
		if !svc.MeshEnabled {
			continue
		}
		for _, ep := range svc.Endpoints {
			if !ep.Ready {
				continue
			}
			// Check if endpoint is on this node.
			// The endpoint's labels may contain topology info, or we can
			// compare the address against the node IP range. For now,
			// we use the "topology.kubernetes.io/zone" or node-local
			// detection heuristic: if the endpoint address matches a
			// known node-local CIDR or the node IP itself.
			if !m.isLocalEndpoint(ep) {
				continue
			}
			for _, port := range svc.Ports {
				key, err := sockmap.NewEndpointKey(ep.Address, safePortToUint16(port.TargetPort))
				if err != nil {
					m.logger.Debug("Skipping invalid endpoint for SOCKMAP",
						zap.String("address", ep.Address),
						zap.Int32("port", port.TargetPort),
						zap.Error(err))
					continue
				}
				desired[key] = sockmap.EndpointValue{Eligible: 1}
			}
		}
	}

	if err := m.sockMapMgr.SyncEndpoints(desired); err != nil {
		m.logger.Warn("Failed to reconcile SOCKMAP endpoints", zap.Error(err))
	}
}

// isLocalEndpoint determines whether a backend endpoint is running on the
// same node as this agent. Currently uses a simple IP comparison against
// the configured node IP. Future versions may use pod CIDR detection or
// topology labels.
func (m *Manager) isLocalEndpoint(ep *pb.Endpoint) bool {
	if m.nodeIP == "" {
		return false
	}
	// Check endpoint labels for node-local hint.
	if nodeName, ok := ep.Labels["topology.kubernetes.io/node"]; ok {
		// If label is present but empty, treat as unknown.
		return nodeName != "" && nodeName == m.nodeIP
	}
	// Fallback: not enough info to determine locality.
	return false
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

	// Clean up eBPF acceleration resources.
	if m.sockMapMgr != nil {
		if err := m.sockMapMgr.Close(); err != nil {
			m.logger.Error("SOCKMAP manager cleanup failed", zap.Error(err))
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
