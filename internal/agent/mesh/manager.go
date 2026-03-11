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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/azrtydxb/novaedge/internal/agent/novanet"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

var (
	errMeshManagerNotStarted = errors.New("mesh manager not started")
)

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
	// same-node endpoints eligible for SOCKMAP bypass via NovaNet.
	nodeIP string

	// nodeName is the Kubernetes node name, used for matching the
	// topology.kubernetes.io/node label on endpoints.
	nodeName string

	// novanetClient delegates eBPF operations (SOCKMAP, mesh redirects,
	// rate limiting, health monitoring) to the NovaNet agent. May be nil
	// if NovaNet integration is not configured.
	novanetClient *novanet.Client

	// meshRedirects tracks the set of mesh redirect entries currently
	// installed via NovaNet (key: "clusterIP:port"). Used to compute
	// add/remove diffs during reconciliation. Stores the parsed IP and
	// port so stale entries can be removed without re-parsing the key.
	meshRedirects map[string]redirectEntry

	// sockmapPods tracks pods currently enabled for SOCKMAP acceleration
	// via NovaNet (key: "namespace/name"). Used to disable acceleration
	// for pods that are no longer in the desired set.
	sockmapPods map[string]bool

	// rateLimits tracks CIDRs with active kernel-level rate limits
	// installed via NovaNet. Used to remove stale entries on reconciliation.
	rateLimits map[string]bool
}

// ManagerConfig holds configuration for creating a mesh Manager.
type ManagerConfig struct {
	TPROXYPort  int32
	TunnelPort  int32
	TrustDomain string
	// NodeIP is the IP address of this node. Used for identifying same-node
	// endpoints that are eligible for SOCKMAP bypass.
	NodeIP string
	// NodeName is the Kubernetes node name. Used for matching the
	// topology.kubernetes.io/node label on endpoints for SOCKMAP bypass.
	NodeName string
	// Federation holds cross-cluster federation settings. May be nil when
	// federation is not active.
	Federation *FederationConfig
	// RuleBackendOverride, if non-nil, is used instead of the auto-detected
	// nftables/iptables backend. This is used by the eBPF sk_lookup backend
	// which is initialized before the mesh manager.
	RuleBackendOverride RuleBackend
	// NovaNetClient, if non-nil, delegates eBPF operations (SOCKMAP,
	// mesh redirects, rate limiting, health) to the NovaNet agent.
	NovaNetClient *novanet.Client
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
		nodeName:            cfg.NodeName,
		novanetClient:       cfg.NovaNetClient,
		meshRedirects:       make(map[string]redirectEntry),
		sockmapPods:         make(map[string]bool),
		rateLimits:          make(map[string]bool),
	}

	if m.novanetClient != nil {
		namedLogger.Info("NovaNet eBPF services integration enabled")
	}

	return m
}

// Start initializes the mesh data plane: sets up TPROXY interception rules,
// starts the transparent listener, and starts the mTLS tunnel server.
// If a RuleBackendOverride was provided via ManagerConfig, it is used
// directly; otherwise auto-detection selects the best nftables/iptables
// backend.
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

	// NovaNet handles eBPF-based redirection to well-known port 15001;
	// no listener FD registration needed here.

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
func (m *Manager) ApplyConfig(ctx context.Context, services []*pb.InternalService, authzPolicies []*pb.MeshAuthorizationPolicy) error {
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

	// Request NovaNet to install eBPF mesh redirects and SOCKMAP acceleration.
	// Skip reconciliation if NovaNet is not connected to avoid logging
	// no-op successes and corrupting tracked state.
	if m.novanetClient != nil && m.novanetClient.IsConnected() {
		reconcileCtx, reconcileCancel := context.WithTimeout(ctx, 10*time.Second)
		defer reconcileCancel()
		m.reconcileNovaNetMeshRedirects(reconcileCtx, services)
		m.reconcileNovaNetSockMap(reconcileCtx, services)
	}

	m.logger.Info("Mesh config applied",
		zap.Int("services", len(services)),
		zap.Int("intercept_rules", len(targets)),
		zap.Int("routing_entries", m.serviceTable.ServiceCount()),
		zap.Int("authz_policies", len(authzPolicies)),
		zap.Bool("novanet_connected", m.novanetClient != nil && m.novanetClient.IsConnected()))

	return nil
}

// redirectEntry holds the parsed IP and port for a mesh redirect key.
type redirectEntry struct {
	ip   string
	port uint32
}

// reconcileNovaNetMeshRedirects computes the desired set of mesh redirect
// entries (clusterIP:port → tproxyPort) from mesh-enabled services and
// reconciles them against the previously installed set by calling
// AddMeshRedirect for new entries and RemoveMeshRedirect for stale ones.
//
// Tracked state is updated only for entries that were successfully
// installed or removed, so that failed operations are retried on the
// next reconciliation cycle.
func (m *Manager) reconcileNovaNetMeshRedirects(ctx context.Context, services []*pb.InternalService) {
	desired := make(map[string]redirectEntry)

	for _, svc := range services {
		if !svc.MeshEnabled {
			continue
		}
		for _, port := range svc.Ports {
			key := net.JoinHostPort(svc.ClusterIp, fmt.Sprintf("%d", port.Port))
			desired[key] = redirectEntry{ip: svc.ClusterIp, port: uint32(port.Port)} //nolint:gosec // port from proto is int32, always non-negative
		}
	}

	// Build new tracked state: start with entries that remain desired
	// and were already tracked; add newly installed entries below.
	newTracked := make(map[string]redirectEntry)

	// Add new redirect entries.
	for key, entry := range desired {
		if _, tracked := m.meshRedirects[key]; tracked {
			newTracked[key] = entry
			continue
		}
		if err := m.novanetClient.AddMeshRedirect(ctx, entry.ip, entry.port, uint32(m.tproxyPort)); err != nil { //nolint:gosec // tproxyPort is always a valid port number
			m.logger.Warn("Failed to add mesh redirect via NovaNet",
				zap.String("target", key),
				zap.Int32("tproxy_port", m.tproxyPort),
				zap.Error(err))
			// Do not track; will be retried next cycle.
		} else {
			m.logger.Debug("Added mesh redirect via NovaNet",
				zap.String("target", key),
				zap.Int32("tproxy_port", m.tproxyPort))
			newTracked[key] = entry
		}
	}

	// Remove stale redirect entries.
	for key, entry := range m.meshRedirects {
		if _, stillDesired := desired[key]; stillDesired {
			continue
		}
		if err := m.novanetClient.RemoveMeshRedirect(ctx, entry.ip, entry.port); err != nil {
			m.logger.Warn("Failed to remove mesh redirect via NovaNet",
				zap.String("target", key),
				zap.Error(err))
			// Keep in tracked state so removal is retried next cycle.
			newTracked[key] = entry
		} else {
			m.logger.Debug("Removed mesh redirect via NovaNet",
				zap.String("target", key))
		}
	}

	m.meshRedirects = newTracked
}

// reconcileNovaNetSockMap identifies same-node endpoints from the service
// list and requests NovaNet to enable SOCKMAP acceleration for the
// corresponding pods. An endpoint is considered "same-node" if it matches
// the node's name or IP via topology label or address comparison. Pods
// that were previously enabled but are no longer in the desired set are
// disabled.
//
// Tracked state is updated only for entries that were successfully
// enabled or disabled, so that failed operations are retried on the
// next reconciliation cycle.
func (m *Manager) reconcileNovaNetSockMap(ctx context.Context, services []*pb.InternalService) {
	desired := make(map[string]bool)

	for _, svc := range services {
		if !svc.MeshEnabled {
			continue
		}
		for _, ep := range svc.Endpoints {
			if !ep.Ready || !m.isLocalEndpoint(ep) {
				continue
			}
			ns := ep.Labels["kubernetes.io/namespace"]
			name := ep.Labels["kubernetes.io/name"]
			if ns == "" || name == "" {
				continue
			}
			key := ns + "/" + name
			desired[key] = true
		}
	}

	newTracked := make(map[string]bool)

	// Enable SOCKMAP for newly desired pods.
	for key := range desired {
		if m.sockmapPods[key] {
			newTracked[key] = true
			continue // already enabled
		}
		ns, name := splitNsName(key)
		if err := m.novanetClient.EnableSockmap(ctx, ns, name); err != nil {
			m.logger.Warn("Failed to enable SOCKMAP via NovaNet",
				zap.String("namespace", ns),
				zap.String("pod", name),
				zap.Error(err))
			// Do not track; will be retried next cycle.
		} else {
			m.logger.Debug("Enabled SOCKMAP via NovaNet",
				zap.String("namespace", ns),
				zap.String("pod", name))
			newTracked[key] = true
		}
	}

	// Disable SOCKMAP for pods no longer in the desired set.
	for key := range m.sockmapPods {
		if desired[key] {
			continue
		}
		ns, name := splitNsName(key)
		if err := m.novanetClient.DisableSockmap(ctx, ns, name); err != nil {
			m.logger.Warn("Failed to disable SOCKMAP via NovaNet",
				zap.String("namespace", ns),
				zap.String("pod", name),
				zap.Error(err))
			// Keep in tracked state so removal is retried next cycle.
			newTracked[key] = true
		} else {
			m.logger.Debug("Disabled SOCKMAP via NovaNet",
				zap.String("namespace", ns),
				zap.String("pod", name))
		}
	}

	m.sockmapPods = newTracked
}

// splitNsName splits a "namespace/name" key into its components.
func splitNsName(key string) (string, string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 {
		return key, ""
	}
	return parts[0], parts[1]
}

// isLocalEndpoint determines whether a backend endpoint is running on the
// same node as this agent. It first checks the topology label (which
// contains a node name) against the configured node name, then falls back
// to comparing the endpoint address against the configured node IP.
func (m *Manager) isLocalEndpoint(ep *pb.Endpoint) bool {
	// Check endpoint labels for node-local hint. The
	// topology.kubernetes.io/node label contains a node name, not an IP.
	if labelNode, ok := ep.Labels["topology.kubernetes.io/node"]; ok && labelNode != "" {
		return m.nodeName != "" && labelNode == m.nodeName
	}
	// Fallback: compare endpoint address against the node IP.
	if m.nodeIP != "" && ep.Address != "" {
		return ep.Address == m.nodeIP
	}
	return false
}

// RateLimitEntry represents a per-CIDR kernel-level rate limit to be
// pushed to NovaNet's eBPF layer.
type RateLimitEntry struct {
	CIDR  string
	Rate  uint32
	Burst uint32
}

// ApplyRateLimits reconciles kernel-level rate limits via the NovaNet client.
// This should be called when per-CIDR rate limit policies change in the
// config snapshot. Entries not in the desired set are removed.
//
// This function is available for use by the agent config application path.
// Currently, the ConfigSnapshot's ProxyPolicy objects carry rate limit
// configuration as middleware parameters (token bucket rate/burst), but
// they are keyed by route, not by source CIDR. A future feature will add
// per-source-CIDR rate limit extraction from policies, at which point
// applyAgentConfig() will call this method with the extracted entries.
func (m *Manager) ApplyRateLimits(ctx context.Context, desired []RateLimitEntry) {
	if m.novanetClient == nil {
		return
	}

	desiredSet := make(map[string]bool, len(desired))
	for _, entry := range desired {
		desiredSet[entry.CIDR] = true
		if err := m.novanetClient.ConfigureRateLimit(ctx, entry.CIDR, entry.Rate, entry.Burst); err != nil {
			m.logger.Warn("Failed to configure kernel rate limit via NovaNet",
				zap.String("cidr", entry.CIDR),
				zap.Uint32("rate", entry.Rate),
				zap.Uint32("burst", entry.Burst),
				zap.Error(err))
		} else {
			m.logger.Debug("Configured kernel rate limit via NovaNet",
				zap.String("cidr", entry.CIDR),
				zap.Uint32("rate", entry.Rate),
				zap.Uint32("burst", entry.Burst))
		}
	}

	// Remove rate limits that are no longer desired.
	for cidr := range m.rateLimits {
		if desiredSet[cidr] {
			continue
		}
		if err := m.novanetClient.RemoveRateLimit(ctx, cidr); err != nil {
			m.logger.Warn("Failed to remove kernel rate limit via NovaNet",
				zap.String("cidr", cidr),
				zap.Error(err))
		} else {
			m.logger.Debug("Removed kernel rate limit via NovaNet",
				zap.String("cidr", cidr))
		}
	}

	m.rateLimits = desiredSet
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
// The provided context controls the timeout for cleanup operations
// such as disabling SOCKMAP on tracked pods.
func (m *Manager) Shutdown(ctx context.Context) error {
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

	// Disable SOCKMAP for all tracked pods before shutdown.
	if m.novanetClient != nil {
		for key := range m.sockmapPods {
			ns, name := splitNsName(key)
			if err := m.novanetClient.DisableSockmap(ctx, ns, name); err != nil {
				m.logger.Warn("Failed to disable SOCKMAP during shutdown",
					zap.String("namespace", ns),
					zap.String("pod", name),
					zap.Error(err))
			}
		}
		m.sockmapPods = make(map[string]bool)
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
