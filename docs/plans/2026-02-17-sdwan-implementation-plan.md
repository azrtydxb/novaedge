# SD-WAN Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Transform NovaEdge into a full SD-WAN by implementing WireGuard data plane (wgctrl), overlay routing, WAN intelligence (probing + path selection), STUN NAT traversal, DSCP marking, and a topology dashboard.

**Architecture:** Layered approach — enhance `internal/agent/tunnel/` for transport (wgctrl WireGuard, overlay routing, STUN) and create new `internal/agent/sdwan/` for intelligence (probing, path selection, link management, DSCP). Two new CRDs: `ProxyWANLink` and `ProxyWANPolicy`.

**Tech Stack:** Go 1.24, `golang.zx2c4.com/wireguard/wgctrl` (kernel WireGuard API), `github.com/pion/stun/v3` (NAT traversal), `vishvananda/netlink` (already in project — route/interface management), `osrg/gobgp/v3` (already in project — overlay prefix advertisement), React/TypeScript (WebUI)

**Design doc:** `docs/plans/2026-02-17-sdwan-implementation-design.md`

**Worktree:** `../novaedge-worktrees/issue-sdwan-implementation` (branch: `issue-sdwan-implementation`)

---

## Phase 1: Data Plane

### Task 1: Add wgctrl and pion/stun dependencies

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Add dependencies**

```bash
cd ../novaedge-worktrees/issue-sdwan-implementation
go get golang.zx2c4.com/wireguard/wgctrl@latest
go get github.com/pion/stun/v3@latest
go mod tidy
```

**Step 2: Verify build**

```bash
go build ./...
```
Expected: PASS (no code uses them yet, just dependency resolution)

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "[Chore] Add wgctrl and pion/stun dependencies for SD-WAN"
```

---

### Task 2: Add OverlayCIDR field to RemoteCluster CRD

**Files:**
- Modify: `api/v1alpha1/novaedgeremotecluster_types.go:66` (add field after `Paused`)
- Modify: `api/v1alpha1/zz_generated.deepcopy.go` (auto-generated)

**Step 1: Add the OverlayCIDR field**

In `api/v1alpha1/novaedgeremotecluster_types.go`, add after the `Paused` field (line 66):

```go
	// OverlayCIDR is the overlay network CIDR assigned to this remote cluster
	// for site-to-site routing (e.g., "10.200.1.0/24").
	// +optional
	OverlayCIDR string `json:"overlayCIDR,omitempty"`
```

**Step 2: Add ListenPort to WireGuardConfig**

In the same file, add after `PersistentKeepalive` (line 190):

```go
	// ListenPort is the local UDP port for the WireGuard interface.
	// +optional
	ListenPort *int32 `json:"listenPort,omitempty"`
```

**Step 3: Regenerate deepcopy and CRD manifests**

```bash
make generate
make generate-crds
```

**Step 4: Verify build**

```bash
go build ./...
make test
```

**Step 5: Commit**

```bash
git add api/v1alpha1/novaedgeremotecluster_types.go api/v1alpha1/zz_generated.deepcopy.go config/crd/
git commit -m "[Feature] Add OverlayCIDR and ListenPort fields to RemoteCluster CRD"
```

---

### Task 3: Rewrite WireGuard tunnel with wgctrl kernel API

**Files:**
- Modify: `internal/agent/tunnel/wireguard.go` (complete rewrite ~350 lines)
- Modify: `internal/agent/tunnel/tunnel.go` (add OverlayAddr to interface)
- Modify: `internal/agent/tunnel/manager_test.go` (update tests)
- Create: `internal/agent/tunnel/wireguard_test.go` (~200 lines)

**Step 1: Update Tunnel interface**

In `internal/agent/tunnel/tunnel.go`, add `OverlayAddr()` to the interface:

```go
type Tunnel interface {
	Start(ctx context.Context) error
	Stop() error
	IsHealthy() bool
	LocalAddr() string
	// OverlayAddr returns the overlay network IP assigned to this tunnel (e.g., "10.200.1.1").
	OverlayAddr() string
	Type() string
}
```

Update `ssh.go` and `websocket.go` to add stub `OverlayAddr() string { return "" }` methods.

**Step 2: Write failing tests for new WireGuard behavior**

Create `internal/agent/tunnel/wireguard_test.go`:

```go
package tunnel

import (
	"testing"

	v1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	"go.uber.org/zap"
)

func TestNewWireGuardTunnel(t *testing.T) {
	logger := zap.NewNop()
	listenPort := int32(51820)
	config := v1alpha1.TunnelConfig{
		Type: v1alpha1.TunnelTypeWireGuard,
		WireGuard: &v1alpha1.WireGuardConfig{
			PublicKey:  "dGVzdC1wdWJsaWMta2V5LWJhc2U2NA==",
			Endpoint:   "203.0.113.1:51820",
			AllowedIPs: []string{"10.200.1.0/24"},
			ListenPort: &listenPort,
		},
	}

	wg, err := newWireGuardTunnel("test-cluster", config, "10.200.0.1/24", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wg.ifaceName == "" {
		t.Fatal("expected non-empty interface name")
	}
	if len(wg.ifaceName) > 15 {
		t.Fatalf("interface name too long: %s", wg.ifaceName)
	}
	if wg.Type() != "wireguard" {
		t.Fatalf("expected type wireguard, got %s", wg.Type())
	}
	if wg.OverlayAddr() != "10.200.0.1" {
		t.Fatalf("expected overlay addr 10.200.0.1, got %s", wg.OverlayAddr())
	}
}

func TestNewWireGuardTunnelNilConfig(t *testing.T) {
	logger := zap.NewNop()
	config := v1alpha1.TunnelConfig{Type: v1alpha1.TunnelTypeWireGuard}

	_, err := newWireGuardTunnel("test", config, "", logger)
	if err == nil {
		t.Fatal("expected error for nil wireguard config")
	}
}

func TestNewWireGuardTunnelInvalidOverlayCIDR(t *testing.T) {
	logger := zap.NewNop()
	config := v1alpha1.TunnelConfig{
		Type:      v1alpha1.TunnelTypeWireGuard,
		WireGuard: &v1alpha1.WireGuardConfig{PublicKey: "key"},
	}

	_, err := newWireGuardTunnel("test", config, "not-a-cidr", logger)
	if err == nil {
		t.Fatal("expected error for invalid overlay CIDR")
	}
}

func TestSanitizeInterfaceNameLength(t *testing.T) {
	// Very long cluster name should be truncated to 15 chars for interface
	name := sanitizeInterfaceName("very-long-cluster-name-that-exceeds-limit")
	if len(wireguardInterfacePrefix+"-"+name) > 15 {
		// The full name gets truncated in newWireGuardTunnel
	}
}
```

**Step 3: Run tests to verify they fail**

```bash
go test -v ./internal/agent/tunnel/ -run TestNewWireGuardTunnel
```
Expected: FAIL (signature changed, `OverlayAddr` not implemented)

**Step 4: Rewrite wireguard.go**

Rewrite `internal/agent/tunnel/wireguard.go` with wgctrl kernel API:

```go
package tunnel

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	v1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

const (
	wireguardInterfacePrefix = "novaedge-wg"
	wireguardDefaultPort     = 51820
	wireguardKeepalive       = 25
	maxBackoff               = 30 * time.Second
	handshakeTimeout         = 120 * time.Second
	monitorInterval          = 10 * time.Second
)

// wireGuardTunnel implements the Tunnel interface using the wgctrl kernel API.
type wireGuardTunnel struct {
	mu          sync.RWMutex
	clusterName string
	config      v1alpha1.TunnelConfig
	ifaceName   string
	overlayIP   net.IP
	overlayCIDR *net.IPNet
	localAddr   string
	healthy     atomic.Bool
	logger      *zap.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	privateKey  wgtypes.Key
}

// newWireGuardTunnel creates a WireGuard tunnel instance.
func newWireGuardTunnel(clusterName string, config v1alpha1.TunnelConfig, overlayCIDR string, logger *zap.Logger) (*wireGuardTunnel, error) {
	if config.WireGuard == nil {
		return nil, fmt.Errorf("wireguard config is required for WireGuard tunnel type")
	}

	// Parse overlay CIDR
	var overlayIP net.IP
	var overlayNet *net.IPNet
	if overlayCIDR != "" {
		var err error
		overlayIP, overlayNet, err = net.ParseCIDR(overlayCIDR)
		if err != nil {
			return nil, fmt.Errorf("parsing overlay CIDR %q: %w", overlayCIDR, err)
		}
	}

	// Generate deterministic interface name
	ifaceName := fmt.Sprintf("%s-%s", wireguardInterfacePrefix, sanitizeInterfaceName(clusterName))
	if len(ifaceName) > 15 {
		ifaceName = ifaceName[:15]
	}

	// Generate ephemeral private key (real deployments use PrivateKeySecretRef)
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("generating wireguard private key: %w", err)
	}

	return &wireGuardTunnel{
		clusterName: clusterName,
		config:      config,
		ifaceName:   ifaceName,
		overlayIP:   overlayIP,
		overlayCIDR: overlayNet,
		logger:      logger.With(zap.String("tunnel", "wireguard"), zap.String("cluster", clusterName)),
		done:        make(chan struct{}),
		privateKey:  privateKey,
	}, nil
}

func (t *wireGuardTunnel) Start(ctx context.Context) error {
	t.mu.Lock()
	t.ctx, t.cancel = context.WithCancel(ctx)
	t.mu.Unlock()

	go t.maintainConnection(t.ctx)
	return nil
}

func (t *wireGuardTunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cancel != nil {
		t.cancel()
	}
	<-t.done
	t.healthy.Store(false)

	return t.teardownInterface()
}

func (t *wireGuardTunnel) IsHealthy() bool { return t.healthy.Load() }

func (t *wireGuardTunnel) LocalAddr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.localAddr
}

func (t *wireGuardTunnel) OverlayAddr() string {
	if t.overlayIP == nil {
		return ""
	}
	return t.overlayIP.String()
}

func (t *wireGuardTunnel) Type() string { return "wireguard" }

func (t *wireGuardTunnel) maintainConnection(ctx context.Context) {
	defer close(t.done)

	backoff := time.Second
	for {
		if err := t.connect(ctx); err != nil {
			t.logger.Error("wireguard connection failed", zap.Error(err), zap.Duration("backoff", backoff))
			t.healthy.Store(false)

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = minDuration(backoff*2, maxBackoff)
			continue
		}

		t.healthy.Store(true)
		backoff = time.Second
		t.logger.Info("wireguard tunnel established",
			zap.String("interface", t.ifaceName),
			zap.String("overlay", t.OverlayAddr()),
		)

		if err := t.monitorConnection(ctx); err != nil {
			t.logger.Warn("wireguard connection lost", zap.Error(err))
			t.healthy.Store(false)
		}

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (t *wireGuardTunnel) connect(ctx context.Context) error {
	wgConfig := t.config.WireGuard

	// Create WireGuard interface via netlink
	wgLink := &netlink.Wireguard{LinkAttrs: netlink.LinkAttrs{Name: t.ifaceName}}
	if err := netlink.LinkAdd(wgLink); err != nil {
		// Interface may already exist from a previous attempt
		if !isExistsError(err) {
			return fmt.Errorf("creating wireguard interface: %w", err)
		}
	}

	// Configure WireGuard via wgctrl
	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("creating wgctrl client: %w", err)
	}
	defer client.Close()

	// Parse peer public key
	peerKey, err := wgtypes.ParseKey(wgConfig.PublicKey)
	if err != nil {
		return fmt.Errorf("parsing peer public key: %w", err)
	}

	// Parse peer endpoint
	var peerEndpoint *net.UDPAddr
	if wgConfig.Endpoint != "" {
		peerEndpoint, err = net.ResolveUDPAddr("udp", wgConfig.Endpoint)
		if err != nil {
			return fmt.Errorf("resolving peer endpoint: %w", err)
		}
	}

	// Parse allowed IPs
	var allowedIPs []net.IPNet
	for _, cidr := range wgConfig.AllowedIPs {
		_, ipNet, parseErr := net.ParseCIDR(cidr)
		if parseErr != nil {
			return fmt.Errorf("parsing allowed IP %q: %w", cidr, parseErr)
		}
		allowedIPs = append(allowedIPs, *ipNet)
	}

	// Keepalive
	keepalive := time.Duration(wireguardKeepalive) * time.Second
	if wgConfig.PersistentKeepalive != nil {
		keepalive = time.Duration(*wgConfig.PersistentKeepalive) * time.Second
	}

	// Listen port
	listenPort := wireguardDefaultPort
	if wgConfig.ListenPort != nil {
		listenPort = int(*wgConfig.ListenPort)
	}

	// Configure device
	deviceConfig := wgtypes.Config{
		PrivateKey:   &t.privateKey,
		ListenPort:   &listenPort,
		ReplacePeers: true,
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey:                   peerKey,
				Endpoint:                    peerEndpoint,
				AllowedIPs:                  allowedIPs,
				PersistentKeepaliveInterval: &keepalive,
			},
		},
	}

	if err := client.ConfigureDevice(t.ifaceName, deviceConfig); err != nil {
		return fmt.Errorf("configuring wireguard device: %w", err)
	}

	// Assign overlay IP if configured
	if t.overlayIP != nil && t.overlayCIDR != nil {
		link, linkErr := netlink.LinkByName(t.ifaceName)
		if linkErr != nil {
			return fmt.Errorf("finding wireguard link: %w", linkErr)
		}

		addr := &netlink.Addr{
			IPNet: &net.IPNet{IP: t.overlayIP, Mask: t.overlayCIDR.Mask},
		}
		if err := netlink.AddrReplace(link, addr); err != nil {
			return fmt.Errorf("assigning overlay IP: %w", err)
		}
	}

	// Bring interface up
	link, err := netlink.LinkByName(t.ifaceName)
	if err != nil {
		return fmt.Errorf("finding wireguard link for up: %w", err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("bringing up wireguard interface: %w", err)
	}

	// Set local address
	t.mu.Lock()
	if t.overlayIP != nil {
		t.localAddr = t.overlayIP.String()
	} else {
		t.localAddr = t.ifaceName
	}
	t.mu.Unlock()

	return nil
}

func (t *wireGuardTunnel) monitorConnection(ctx context.Context) error {
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := t.checkHandshake(); err != nil {
				return err
			}
		}
	}
}

func (t *wireGuardTunnel) checkHandshake() error {
	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("creating wgctrl client for handshake check: %w", err)
	}
	defer client.Close()

	device, err := client.Device(t.ifaceName)
	if err != nil {
		return fmt.Errorf("getting wireguard device: %w", err)
	}

	for _, peer := range device.Peers {
		if peer.LastHandshakeTime.IsZero() {
			return fmt.Errorf("no wireguard handshake established with peer %s", peer.PublicKey)
		}
		if time.Since(peer.LastHandshakeTime) > handshakeTimeout {
			return fmt.Errorf("wireguard handshake stale (last: %s ago)", time.Since(peer.LastHandshakeTime))
		}
	}

	return nil
}

func (t *wireGuardTunnel) teardownInterface() error {
	link, err := netlink.LinkByName(t.ifaceName)
	if err != nil {
		return nil // Interface doesn't exist, nothing to clean up
	}
	return netlink.LinkDel(link)
}

func isExistsError(err error) bool {
	return err != nil && (err.Error() == "file exists" || err.Error() == "device or resource busy")
}
```

**Step 5: Run tests**

```bash
go test -v ./internal/agent/tunnel/ -run TestNewWireGuardTunnel
go test -v ./internal/agent/tunnel/ -run TestSanitize
```
Expected: PASS

**Step 6: Run full test suite**

```bash
go test ./internal/agent/tunnel/...
gofmt -s -w internal/agent/tunnel/
go vet ./internal/agent/tunnel/...
```

**Step 7: Commit**

```bash
git add internal/agent/tunnel/
git commit -m "[Feature] Rewrite WireGuard tunnel with wgctrl kernel API

Replace CLI shelling (ip link, wg set) with wgctrl kernel-native
WireGuard management. Adds private key generation, overlay IP
assignment via netlink, and handshake monitoring via wgctrl."
```

---

### Task 4: Implement overlay routing

**Files:**
- Create: `internal/agent/tunnel/overlay.go` (~200 lines)
- Create: `internal/agent/tunnel/overlay_test.go` (~150 lines)
- Modify: `internal/agent/tunnel/manager.go` (add overlay CIDR parameter)

**Step 1: Write failing tests**

Create `internal/agent/tunnel/overlay_test.go`:

```go
package tunnel

import (
	"net"
	"testing"
)

func TestParseOverlayCIDR(t *testing.T) {
	tests := []struct {
		name    string
		cidr    string
		wantIP  string
		wantErr bool
	}{
		{"valid IPv4", "10.200.1.0/24", "10.200.1.0", false},
		{"valid IPv6", "fd00:200:1::/48", "fd00:200:1::", false},
		{"invalid", "not-a-cidr", "", true},
		{"empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, _, err := net.ParseCIDR(tt.cidr)
			if tt.wantErr {
				if err == nil && tt.cidr != "" {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ip.String() != tt.wantIP {
				t.Fatalf("expected IP %s, got %s", tt.wantIP, ip.String())
			}
		})
	}
}

func TestOverlayRouteManager(t *testing.T) {
	orm := NewOverlayRouteManager(nil) // nil logger for test
	if orm == nil {
		t.Fatal("expected non-nil overlay route manager")
	}
}
```

**Step 2: Run to verify failure**

```bash
go test -v ./internal/agent/tunnel/ -run TestOverlayRouteManager
```
Expected: FAIL (NewOverlayRouteManager undefined)

**Step 3: Implement overlay.go**

Create `internal/agent/tunnel/overlay.go`:

```go
package tunnel

import (
	"fmt"
	"net"
	"sync"

	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
)

// OverlayRouteManager manages overlay network routes for site-to-site tunnels.
type OverlayRouteManager struct {
	mu     sync.RWMutex
	routes map[string]*overlayRoute // remoteCIDR -> route info
	logger *zap.Logger
}

type overlayRoute struct {
	remoteCIDR *net.IPNet
	linkName   string
	linkIndex  int
}

// NewOverlayRouteManager creates a new overlay route manager.
func NewOverlayRouteManager(logger *zap.Logger) *OverlayRouteManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &OverlayRouteManager{
		routes: make(map[string]*overlayRoute),
		logger: logger,
	}
}

// InstallRoute adds a route for a remote site's overlay CIDR via the specified interface.
func (m *OverlayRouteManager) InstallRoute(remoteCIDR string, linkName string) error {
	_, cidr, err := net.ParseCIDR(remoteCIDR)
	if err != nil {
		return fmt.Errorf("parsing remote CIDR %q: %w", remoteCIDR, err)
	}

	link, err := netlink.LinkByName(linkName)
	if err != nil {
		return fmt.Errorf("finding link %q: %w", linkName, err)
	}

	route := &netlink.Route{
		Dst:       cidr,
		LinkIndex: link.Attrs().Index,
		Scope:     netlink.SCOPE_UNIVERSE,
	}

	if err := netlink.RouteReplace(route); err != nil {
		return fmt.Errorf("installing overlay route %s via %s: %w", remoteCIDR, linkName, err)
	}

	m.mu.Lock()
	m.routes[remoteCIDR] = &overlayRoute{
		remoteCIDR: cidr,
		linkName:   linkName,
		linkIndex:  link.Attrs().Index,
	}
	m.mu.Unlock()

	m.logger.Info("overlay route installed",
		zap.String("cidr", remoteCIDR),
		zap.String("interface", linkName),
	)

	return nil
}

// RemoveRoute removes the overlay route for the specified CIDR.
func (m *OverlayRouteManager) RemoveRoute(remoteCIDR string) error {
	m.mu.Lock()
	route, ok := m.routes[remoteCIDR]
	if !ok {
		m.mu.Unlock()
		return nil // Route doesn't exist, nothing to remove
	}
	delete(m.routes, remoteCIDR)
	m.mu.Unlock()

	nlRoute := &netlink.Route{
		Dst:       route.remoteCIDR,
		LinkIndex: route.linkIndex,
	}

	if err := netlink.RouteDel(nlRoute); err != nil {
		return fmt.Errorf("removing overlay route %s: %w", remoteCIDR, err)
	}

	m.logger.Info("overlay route removed", zap.String("cidr", remoteCIDR))
	return nil
}

// RemoveAllRoutes removes all managed overlay routes.
func (m *OverlayRouteManager) RemoveAllRoutes() {
	m.mu.Lock()
	routes := make(map[string]*overlayRoute, len(m.routes))
	for k, v := range m.routes {
		routes[k] = v
	}
	m.mu.Unlock()

	for cidr := range routes {
		if err := m.RemoveRoute(cidr); err != nil {
			m.logger.Warn("failed to remove overlay route", zap.String("cidr", cidr), zap.Error(err))
		}
	}
}

// GetRoutes returns a copy of all installed overlay routes.
func (m *OverlayRouteManager) GetRoutes() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]string, len(m.routes))
	for cidr, route := range m.routes {
		result[cidr] = route.linkName
	}
	return result
}
```

**Step 4: Update manager.go to pass overlay CIDR**

In `internal/agent/tunnel/manager.go`, update `createTunnel` (line 162) to accept `overlayCIDR`:

```go
func (m *NetworkTunnelManager) createTunnel(clusterName string, config v1alpha1.TunnelConfig, overlayCIDR string) (Tunnel, error) {
	switch config.Type {
	case v1alpha1.TunnelTypeWireGuard:
		return newWireGuardTunnel(clusterName, config, overlayCIDR, m.logger)
	case v1alpha1.TunnelTypeSSH:
		return newSSHTunnel(clusterName, config, m.logger)
	case v1alpha1.TunnelTypeWebSocket:
		return newWebSocketTunnel(clusterName, config, m.logger)
	default:
		return nil, fmt.Errorf("unsupported tunnel type: %s", config.Type)
	}
}
```

Update `AddTunnel` signature to accept `overlayCIDR string` and pass it through.

**Step 5: Run tests**

```bash
go test -v ./internal/agent/tunnel/...
gofmt -s -w internal/agent/tunnel/
```

**Step 6: Commit**

```bash
git add internal/agent/tunnel/
git commit -m "[Feature] Add overlay route manager for site-to-site routing

Installs/removes overlay network routes via netlink for traffic
between sites through WireGuard tunnels. Routes remote site CIDRs
through the tunnel interface."
```

---

### Task 5: Add BGP overlay prefix advertisement

**Files:**
- Modify: `internal/agent/vip/bgp.go` (add `AnnounceOverlayPrefix` / `WithdrawOverlayPrefix` methods)
- Create: `internal/agent/vip/bgp_overlay_test.go` (~100 lines)

**Step 1: Write failing test**

Create `internal/agent/vip/bgp_overlay_test.go`:

```go
package vip

import (
	"testing"
)

func TestBGPOverlayPrefixValidation(t *testing.T) {
	tests := []struct {
		name    string
		cidr    string
		wantErr bool
	}{
		{"valid /24", "10.200.1.0/24", false},
		{"valid /16", "10.200.0.0/16", false},
		{"invalid", "not-a-cidr", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOverlayCIDR(tt.cidr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateOverlayCIDR(%q) error = %v, wantErr %v", tt.cidr, err, tt.wantErr)
			}
		})
	}
}
```

**Step 2: Implement overlay advertisement in bgp.go**

Add after the `withdrawRoute` method (~line 1070):

```go
// validateOverlayCIDR validates an overlay CIDR string.
func validateOverlayCIDR(cidr string) error {
	if cidr == "" {
		return fmt.Errorf("overlay CIDR cannot be empty")
	}
	_, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid overlay CIDR %q: %w", cidr, err)
	}
	return nil
}

// AnnounceOverlayPrefix advertises an overlay network prefix via BGP.
func (h *BGPHandler) AnnounceOverlayPrefix(ctx context.Context, cidr string, config *pb.BGPConfig) error {
	if err := validateOverlayCIDR(cidr); err != nil {
		return err
	}

	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parsing overlay CIDR: %w", err)
	}

	isIPv6 := ip.To4() == nil
	prefixLen, _ := ipNet.Mask.Size()

	h.logger.Info("announcing overlay prefix via BGP",
		zap.String("cidr", cidr),
		zap.Int("prefixLen", prefixLen),
	)

	return h.announceRoute(ctx, ipNet.IP, config, isIPv6)
}

// WithdrawOverlayPrefix withdraws an overlay network prefix from BGP.
func (h *BGPHandler) WithdrawOverlayPrefix(ctx context.Context, cidr string, config *pb.BGPConfig) error {
	if err := validateOverlayCIDR(cidr); err != nil {
		return err
	}

	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parsing overlay CIDR: %w", err)
	}

	isIPv6 := ip.To4() == nil

	h.logger.Info("withdrawing overlay prefix from BGP", zap.String("cidr", cidr))

	return h.withdrawRoute(ctx, ipNet.IP, config, isIPv6)
}
```

**Step 3: Run tests**

```bash
go test -v ./internal/agent/vip/ -run TestBGPOverlay
gofmt -s -w internal/agent/vip/
```

**Step 4: Commit**

```bash
git add internal/agent/vip/
git commit -m "[Feature] Add BGP overlay prefix announcement for SD-WAN

Enables advertising overlay network CIDRs via BGP so that nodes
on the same site learn routes to remote site overlay networks."
```

---

## Phase 2: WAN Intelligence

### Task 6: Create ProxyWANLink CRD types

**Files:**
- Create: `api/v1alpha1/proxywanlink_types.go` (~150 lines)
- Modify: `api/v1alpha1/zz_generated.deepcopy.go` (auto-generated)

**Step 1: Create CRD types**

Create `api/v1alpha1/proxywanlink_types.go`:

```go
/*
Copyright 2024 NovaEdge Authors.
...license header...
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WANLinkRole defines the role of a WAN link.
type WANLinkRole string

const (
	// WANLinkRolePrimary is the primary WAN link for a site.
	WANLinkRolePrimary WANLinkRole = "primary"
	// WANLinkRoleBackup is a backup WAN link activated on primary failure.
	WANLinkRoleBackup WANLinkRole = "backup"
	// WANLinkRoleLoadBalance distributes traffic across multiple links.
	WANLinkRoleLoadBalance WANLinkRole = "loadbalance"
)

// ProxyWANLinkSpec defines the desired state of a WAN link.
type ProxyWANLinkSpec struct {
	// Site identifies which site this WAN link belongs to.
	Site string `json:"site"`

	// Interface is the physical network interface name.
	// +optional
	Interface string `json:"interface,omitempty"`

	// Provider is a human-readable ISP/provider name.
	// +optional
	Provider string `json:"provider,omitempty"`

	// Bandwidth is the advertised link bandwidth (e.g., "1Gbps").
	// +optional
	Bandwidth string `json:"bandwidth,omitempty"`

	// Cost is the relative cost of this link (lower = preferred).
	// +optional
	Cost int32 `json:"cost,omitempty"`

	// SLA defines quality thresholds for this link.
	// +optional
	SLA *WANLinkSLA `json:"sla,omitempty"`

	// TunnelEndpoint defines the WireGuard endpoint on this link.
	// +optional
	TunnelEndpoint *WANTunnelEndpoint `json:"tunnelEndpoint,omitempty"`

	// Role defines the link's role: primary, backup, or loadbalance.
	// +optional
	Role WANLinkRole `json:"role,omitempty"`
}

// WANLinkSLA defines quality thresholds for a WAN link.
type WANLinkSLA struct {
	// MaxLatency is the maximum acceptable one-way latency.
	// +optional
	MaxLatency *metav1.Duration `json:"maxLatency,omitempty"`

	// MaxJitter is the maximum acceptable jitter (latency variance).
	// +optional
	MaxJitter *metav1.Duration `json:"maxJitter,omitempty"`

	// MaxPacketLoss is the maximum acceptable packet loss percentage (0.0-100.0).
	// +optional
	MaxPacketLoss *float64 `json:"maxPacketLoss,omitempty"`
}

// WANTunnelEndpoint defines a WireGuard tunnel endpoint on a WAN link.
type WANTunnelEndpoint struct {
	// PublicIP is the public IP address for this tunnel endpoint.
	// +optional
	PublicIP string `json:"publicIP,omitempty"`

	// Port is the UDP port for the WireGuard endpoint.
	// +optional
	Port int32 `json:"port,omitempty"`
}

// ProxyWANLinkStatus defines the observed state of a WAN link.
type ProxyWANLinkStatus struct {
	// Phase is the current lifecycle phase.
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// CurrentLatency is the current measured latency.
	// +optional
	CurrentLatency *metav1.Duration `json:"currentLatency,omitempty"`

	// CurrentPacketLoss is the current measured packet loss percentage.
	// +optional
	CurrentPacketLoss *float64 `json:"currentPacketLoss,omitempty"`

	// Healthy indicates whether the link meets its SLA thresholds.
	Healthy bool `json:"healthy,omitempty"`

	// ObservedGeneration is the most recent generation observed.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Site",type=string,JSONPath=`.spec.site`
// +kubebuilder:printcolumn:name="Role",type=string,JSONPath=`.spec.role`
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider`
// +kubebuilder:printcolumn:name="Healthy",type=boolean,JSONPath=`.status.healthy`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ProxyWANLink represents a WAN uplink at a site for SD-WAN routing.
type ProxyWANLink struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProxyWANLinkSpec   `json:"spec,omitempty"`
	Status ProxyWANLinkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyWANLinkList contains a list of ProxyWANLink.
type ProxyWANLinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyWANLink `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProxyWANLink{}, &ProxyWANLinkList{})
}
```

**Step 2: Generate deepcopy and CRD manifests**

```bash
make generate
make generate-crds
```

**Step 3: Verify build**

```bash
go build ./...
```

**Step 4: Commit**

```bash
git add api/v1alpha1/proxywanlink_types.go api/v1alpha1/zz_generated.deepcopy.go config/crd/
git commit -m "[Feature] Add ProxyWANLink CRD for SD-WAN multi-link management"
```

---

### Task 7: Create ProxyWANPolicy CRD types

**Files:**
- Create: `api/v1alpha1/proxywanpolicy_types.go` (~120 lines)

**Step 1: Create CRD types**

Create `api/v1alpha1/proxywanpolicy_types.go` with `WANStrategy` enum (lowest-latency, highest-bandwidth, most-reliable, lowest-cost), `WANPolicyMatch` (hosts, paths, headers), `WANPathSelection` (strategy, failover, dscpClass), full CRD with status and kubebuilder markers. Same pattern as Task 6.

**Step 2: Generate and verify**

```bash
make generate
make generate-crds
go build ./...
```

**Step 3: Commit**

```bash
git add api/v1alpha1/proxywanpolicy_types.go api/v1alpha1/zz_generated.deepcopy.go config/crd/
git commit -m "[Feature] Add ProxyWANPolicy CRD for application-aware path selection"
```

---

### Task 8: Implement WAN link quality prober

**Files:**
- Create: `internal/agent/sdwan/prober.go` (~400 lines)
- Create: `internal/agent/sdwan/prober_test.go` (~200 lines)

**Step 1: Write failing tests**

Create `internal/agent/sdwan/prober_test.go`:

```go
package sdwan

import (
	"math"
	"testing"
	"time"
)

func TestLinkQualityScore(t *testing.T) {
	tests := []struct {
		name       string
		latencyMs  float64
		jitterMs   float64
		packetLoss float64
		wantMin    float64
		wantMax    float64
	}{
		{"perfect", 1.0, 0.0, 0.0, 0.5, 2.0},
		{"good", 10.0, 2.0, 0.01, 0.05, 0.15},
		{"degraded", 100.0, 50.0, 0.1, 0.001, 0.01},
		{"total loss", 100.0, 0.0, 1.0, 0.0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := calculateScore(tt.latencyMs, tt.jitterMs, tt.packetLoss)
			if score < tt.wantMin || score > tt.wantMax {
				t.Fatalf("score %f not in range [%f, %f]", score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestEWMASmoothing(t *testing.T) {
	e := newEWMA(0.3)
	e.Add(100.0)
	if e.Value() != 100.0 {
		t.Fatalf("first value should be exact, got %f", e.Value())
	}
	e.Add(200.0)
	expected := 100.0*0.7 + 200.0*0.3 // 130.0
	if math.Abs(e.Value()-expected) > 0.001 {
		t.Fatalf("expected %f, got %f", expected, e.Value())
	}
}

func TestJitterCalculation(t *testing.T) {
	j := newJitterTracker(5)
	samples := []float64{10, 12, 8, 11, 9}
	for _, s := range samples {
		j.Add(s)
	}
	jitter := j.Jitter()
	if jitter < 1.0 || jitter > 2.0 {
		t.Fatalf("jitter %f outside expected range [1.0, 2.0]", jitter)
	}
}

func TestProbeResultToLinkQuality(t *testing.T) {
	lq := &LinkQuality{LinkName: "test-link", RemoteSite: "site-b"}
	if lq.LinkName != "test-link" {
		t.Fatal("unexpected link name")
	}
}
```

**Step 2: Implement prober.go**

Create `internal/agent/sdwan/prober.go` with:
- `Prober` struct with goroutine per link
- `LinkQuality` struct (latencyMs, jitterMs, packetLoss, score, healthy)
- `newEWMA(alpha float64)` for latency smoothing
- `newJitterTracker(windowSize int)` for jitter (stddev of window)
- `calculateScore(latencyMs, jitterMs, packetLoss float64) float64`
- `probeLoop()` — sends UDP echo probes every 1s, measures RTT
- `GetQuality(linkName string) *LinkQuality` — returns latest quality
- `Start(ctx context.Context)` / `Stop()`

**Step 3: Run tests**

```bash
go test -v ./internal/agent/sdwan/ -run TestLinkQuality
go test -v ./internal/agent/sdwan/ -run TestEWMA
go test -v ./internal/agent/sdwan/ -run TestJitter
gofmt -s -w internal/agent/sdwan/
```

**Step 4: Commit**

```bash
git add internal/agent/sdwan/
git commit -m "[Feature] Add WAN link quality prober with EWMA smoothing

UDP-based prober measures latency, jitter, and packet loss per
WAN link. EWMA smoothing for stable readings. Composite quality
score for path selection decisions."
```

---

### Task 9: Implement path selection engine

**Files:**
- Create: `internal/agent/sdwan/pathselect.go` (~300 lines)
- Create: `internal/agent/sdwan/pathselect_test.go` (~200 lines)

**Step 1: Write failing tests**

Create `internal/agent/sdwan/pathselect_test.go` with table-driven tests for each strategy:

```go
package sdwan

import "testing"

func TestPathSelectLowestLatency(t *testing.T) {
	links := []LinkQuality{
		{LinkName: "link-a", LatencyMs: 50, Healthy: true},
		{LinkName: "link-b", LatencyMs: 10, Healthy: true},
		{LinkName: "link-c", LatencyMs: 100, Healthy: true},
	}
	result := selectPath(WANStrategyLowestLatency, links)
	if result != "link-b" {
		t.Fatalf("expected link-b, got %s", result)
	}
}

func TestPathSelectMostReliable(t *testing.T) {
	links := []LinkQuality{
		{LinkName: "link-a", PacketLoss: 0.05, Healthy: true},
		{LinkName: "link-b", PacketLoss: 0.001, Healthy: true},
	}
	result := selectPath(WANStrategyMostReliable, links)
	if result != "link-b" {
		t.Fatalf("expected link-b, got %s", result)
	}
}

func TestPathSelectSkipsUnhealthy(t *testing.T) {
	links := []LinkQuality{
		{LinkName: "link-a", LatencyMs: 5, Healthy: false},
		{LinkName: "link-b", LatencyMs: 50, Healthy: true},
	}
	result := selectPath(WANStrategyLowestLatency, links)
	if result != "link-b" {
		t.Fatalf("expected link-b (skip unhealthy), got %s", result)
	}
}

func TestPathSelectFailoverHysteresis(t *testing.T) {
	ps := NewPathSelector(nil)
	// Test that switching requires sustained improvement
	ps.SetCurrentLink("link-a")
	// link-b is better but hysteresis should prevent immediate switch
	// (detailed test with time mocking)
}
```

**Step 2: Implement pathselect.go**

Create `internal/agent/sdwan/pathselect.go` with:
- Strategy constants: `WANStrategyLowestLatency`, `WANStrategyHighestBandwidth`, `WANStrategyMostReliable`, `WANStrategyLowestCost`
- `PathSelector` struct with current link, hysteresis timer (10s)
- `selectPath(strategy, links)` pure function
- `Select(policy, availableLinks) (linkName string, err error)` with failover
- Hysteresis: don't switch back to a better link until it's been above threshold for 10s

**Step 3: Run tests and commit**

```bash
go test -v ./internal/agent/sdwan/ -run TestPathSelect
gofmt -s -w internal/agent/sdwan/
git add internal/agent/sdwan/
git commit -m "[Feature] Add SLA-based path selection engine for SD-WAN

Supports lowest-latency, highest-bandwidth, most-reliable, and
lowest-cost strategies. Includes failover with 10s hysteresis
to prevent link flip-flop."
```

---

### Task 10: Implement multi-WAN link manager

**Files:**
- Create: `internal/agent/sdwan/linkmanager.go` (~350 lines)
- Create: `internal/agent/sdwan/linkmanager_test.go` (~150 lines)

**Step 1: Write tests, implement, verify**

`LinkManager` struct orchestrates:
- Watch ProxyWANLink CRDs → add/remove links
- Create tunnels per link via `tunnel.NetworkTunnelManager`
- Start/stop probing per link via `Prober`
- Expose link states: active, degraded, down
- Coordinate active-active (ECMP) vs active-passive based on WANLinkRole

**Step 2: Commit**

```bash
git add internal/agent/sdwan/
git commit -m "[Feature] Add multi-WAN link lifecycle manager

Watches ProxyWANLink CRDs, manages tunnel creation per link,
coordinates probing, and tracks link states (active/degraded/down)."
```

---

### Task 11: Implement SD-WAN orchestrator and metrics

**Files:**
- Create: `internal/agent/sdwan/sdwan.go` (~200 lines)
- Create: `internal/agent/sdwan/metrics.go` (~100 lines)
- Create: `internal/agent/metrics/sdwan_metrics.go` (~80 lines)

**Step 1: Create metrics**

In `internal/agent/metrics/sdwan_metrics.go`, define Prometheus metrics following the existing pattern (`promauto.NewGaugeVec` with `novaedge_sdwan_` prefix):

```go
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	SDWANLinkLatency = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_sdwan_link_latency_ms",
			Help: "Current latency of SD-WAN link in milliseconds",
		},
		[]string{"link", "remote_site"},
	)
	SDWANLinkJitter = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_sdwan_link_jitter_ms",
			Help: "Current jitter of SD-WAN link in milliseconds",
		},
		[]string{"link", "remote_site"},
	)
	SDWANLinkPacketLoss = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_sdwan_link_packet_loss_ratio",
			Help: "Current packet loss ratio of SD-WAN link (0.0-1.0)",
		},
		[]string{"link", "remote_site"},
	)
	SDWANLinkScore = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_sdwan_link_score",
			Help: "Composite quality score of SD-WAN link",
		},
		[]string{"link", "remote_site"},
	)
	SDWANLinkHealthy = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_sdwan_link_healthy",
			Help: "Whether SD-WAN link is healthy (1=healthy, 0=unhealthy)",
		},
		[]string{"link", "remote_site"},
	)
	SDWANPathSelections = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_sdwan_path_selections_total",
			Help: "Total number of SD-WAN path selection decisions",
		},
		[]string{"strategy", "link"},
	)
	SDWANFailovers = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_sdwan_failovers_total",
			Help: "Total number of SD-WAN link failovers",
		},
		[]string{"from_link", "to_link"},
	)
)
```

**Step 2: Create SDWANManager orchestrator**

In `internal/agent/sdwan/sdwan.go`:

```go
type SDWANManager struct {
	linkMgr      *LinkManager
	prober       *Prober
	pathSelector *PathSelector
	logger       *zap.Logger
}

func NewSDWANManager(tunnelMgr *tunnel.NetworkTunnelManager, logger *zap.Logger) *SDWANManager
func (m *SDWANManager) Start(ctx context.Context) error
func (m *SDWANManager) Stop()
func (m *SDWANManager) GetLinkQualities() map[string]*LinkQuality
func (m *SDWANManager) SelectPath(policyName string) (string, error)
```

**Step 3: Run tests and commit**

```bash
go test -v ./internal/agent/sdwan/...
go test -v ./internal/agent/metrics/...
gofmt -s -w internal/agent/sdwan/ internal/agent/metrics/
git add internal/agent/sdwan/ internal/agent/metrics/
git commit -m "[Feature] Add SD-WAN orchestrator and Prometheus metrics

SDWANManager coordinates link management, probing, and path
selection. Prometheus metrics for link quality, path selections,
and failover events."
```

---

## Phase 3: Polish

### Task 12: Implement STUN endpoint discovery

**Files:**
- Create: `internal/agent/tunnel/stun.go` (~150 lines)
- Create: `internal/agent/tunnel/stun_test.go` (~100 lines)

**Step 1: Write failing tests**

```go
package tunnel

import "testing"

func TestSTUNDiscoveryParsesResult(t *testing.T) {
	d := NewSTUNDiscoverer(nil, nil) // nil logger, nil servers = defaults
	if d == nil {
		t.Fatal("expected non-nil discoverer")
	}
	if len(d.servers) == 0 {
		t.Fatal("expected default STUN servers")
	}
}

func TestSTUNDefaultServers(t *testing.T) {
	d := NewSTUNDiscoverer(nil, nil)
	found := false
	for _, s := range d.servers {
		if s == "stun.l.google.com:19302" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Google STUN server in defaults")
	}
}
```

**Step 2: Implement stun.go**

```go
package tunnel

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/stun/v3"
	"go.uber.org/zap"
)

var defaultSTUNServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
}

const stunCacheTTL = 5 * time.Minute

type STUNDiscoverer struct {
	mu          sync.RWMutex
	servers     []string
	cachedAddr  *net.UDPAddr
	cachedAt    time.Time
	logger      *zap.Logger
}

func NewSTUNDiscoverer(servers []string, logger *zap.Logger) *STUNDiscoverer {
	if len(servers) == 0 {
		servers = defaultSTUNServers
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &STUNDiscoverer{servers: servers, logger: logger}
}

// Discover returns the public IP and port as seen by STUN servers.
func (d *STUNDiscoverer) Discover() (*net.UDPAddr, error) {
	d.mu.RLock()
	if d.cachedAddr != nil && time.Since(d.cachedAt) < stunCacheTTL {
		addr := d.cachedAddr
		d.mu.RUnlock()
		return addr, nil
	}
	d.mu.RUnlock()

	for _, server := range d.servers {
		addr, err := d.querySTUN(server)
		if err != nil {
			d.logger.Debug("STUN query failed", zap.String("server", server), zap.Error(err))
			continue
		}

		d.mu.Lock()
		d.cachedAddr = addr
		d.cachedAt = time.Now()
		d.mu.Unlock()

		d.logger.Info("STUN discovery successful",
			zap.String("public_addr", addr.String()),
			zap.String("server", server),
		)
		return addr, nil
	}

	return nil, fmt.Errorf("all STUN servers failed")
}

func (d *STUNDiscoverer) querySTUN(server string) (*net.UDPAddr, error) {
	conn, err := stun.Dial("udp4", server)
	if err != nil {
		return nil, fmt.Errorf("dialing STUN server: %w", err)
	}
	defer conn.Close()

	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	var mappedAddr stun.XORMappedAddress
	if err := conn.Do(message, func(res stun.Event) {
		if res.Error != nil {
			return
		}
		_ = mappedAddr.GetFrom(res.Message)
	}); err != nil {
		return nil, fmt.Errorf("STUN binding request: %w", err)
	}

	return &net.UDPAddr{IP: mappedAddr.IP, Port: mappedAddr.Port}, nil
}

// ClearCache invalidates the cached discovery result.
func (d *STUNDiscoverer) ClearCache() {
	d.mu.Lock()
	d.cachedAddr = nil
	d.mu.Unlock()
}
```

**Step 3: Run tests and commit**

```bash
go test -v ./internal/agent/tunnel/ -run TestSTUN
gofmt -s -w internal/agent/tunnel/
git add internal/agent/tunnel/
git commit -m "[Feature] Add STUN endpoint discovery for NAT traversal

Discovers public IP and port via STUN servers before establishing
WireGuard tunnels. 5-minute cache, configurable servers, falls
back to static CRD endpoint on failure."
```

---

### Task 13: Implement DSCP marking

**Files:**
- Create: `internal/agent/sdwan/dscp.go` (~100 lines)
- Create: `internal/agent/sdwan/dscp_test.go` (~80 lines)

**Step 1: Write failing tests**

```go
package sdwan

import "testing"

func TestDSCPClassToValue(t *testing.T) {
	tests := []struct {
		class string
		value int
	}{
		{"EF", 46},
		{"AF41", 34},
		{"AF21", 18},
		{"CS1", 8},
		{"BE", 0},
		{"", 0},
		{"UNKNOWN", 0},
	}
	for _, tt := range tests {
		t.Run(tt.class, func(t *testing.T) {
			v := DSCPClassToValue(tt.class)
			if v != tt.value {
				t.Fatalf("DSCPClassToValue(%q) = %d, want %d", tt.class, v, tt.value)
			}
		})
	}
}

func TestDSCPValueToTOS(t *testing.T) {
	// DSCP value is shifted left 2 bits for TOS byte
	if DSCPToTOS(46) != 184 {
		t.Fatalf("expected TOS 184 for DSCP EF(46), got %d", DSCPToTOS(46))
	}
}
```

**Step 2: Implement dscp.go**

```go
package sdwan

// DSCPClassToValue maps DSCP class names to their numeric values.
func DSCPClassToValue(class string) int {
	switch class {
	case "EF":
		return 46 // Expedited Forwarding (voice)
	case "AF41":
		return 34 // Assured Forwarding 41 (video)
	case "AF31":
		return 26 // Assured Forwarding 31
	case "AF21":
		return 18 // Assured Forwarding 21 (transactional)
	case "AF11":
		return 10 // Assured Forwarding 11
	case "CS1":
		return 8 // Scavenger/bulk
	case "BE", "", "default":
		return 0 // Best Effort
	default:
		return 0
	}
}

// DSCPToTOS converts a DSCP value to the IP TOS byte value.
// The DSCP value occupies the 6 most significant bits of the TOS byte.
func DSCPToTOS(dscp int) int {
	return dscp << 2
}

// SetDSCPOnSocket sets the DSCP/TOS value on a raw file descriptor.
// This should be called on the dialer's control function.
func SetDSCPOnSocket(fd uintptr, dscpClass string) error {
	dscp := DSCPClassToValue(dscpClass)
	if dscp == 0 {
		return nil // Best effort, nothing to set
	}
	tos := DSCPToTOS(dscp)
	return setTOS(fd, tos)
}
```

With a platform-specific `setTOS` using `syscall.SetsockoptInt`.

**Step 3: Run tests and commit**

```bash
go test -v ./internal/agent/sdwan/ -run TestDSCP
gofmt -s -w internal/agent/sdwan/
git add internal/agent/sdwan/
git commit -m "[Feature] Add DSCP packet marking for SD-WAN QoS

Maps application classes (EF, AF41, AF21, CS1, BE) to DSCP
values and sets IP TOS byte on tunnel sockets for network-level
QoS prioritization."
```

---

### Task 14: Add SD-WAN API endpoints to novactl

**Files:**
- Create: `cmd/novactl/pkg/webui/handlers_sdwan.go` (~200 lines)
- Modify: `cmd/novactl/pkg/webui/server.go:169` (register routes)
- Create: `cmd/novactl/cmd/sdwan.go` (~150 lines)
- Modify: `cmd/novactl/cmd/root.go` (register command)

**Step 1: Create API handlers**

Create `cmd/novactl/pkg/webui/handlers_sdwan.go` with handlers for:
- `GET /api/v1/sdwan/links` — list all WAN links with quality data
- `GET /api/v1/sdwan/topology` — site topology graph (nodes + edges)
- `GET /api/v1/sdwan/policies` — active WAN policies
- `GET /api/v1/sdwan/events` — recent path selection events

**Step 2: Register routes**

In `cmd/novactl/pkg/webui/server.go`, add after line 169:

```go
mux.HandleFunc("/api/v1/sdwan/links", s.handleSDWANLinks)
mux.HandleFunc("/api/v1/sdwan/topology", s.handleSDWANTopology)
mux.HandleFunc("/api/v1/sdwan/policies", s.handleSDWANPolicies)
mux.HandleFunc("/api/v1/sdwan/events", s.handleSDWANEvents)
```

**Step 3: Create CLI command**

Create `cmd/novactl/cmd/sdwan.go` with `novactl sdwan status`, `novactl sdwan links`, `novactl sdwan topology` subcommands.

**Step 4: Verify and commit**

```bash
go build ./cmd/novactl/...
gofmt -s -w cmd/novactl/
git add cmd/novactl/
git commit -m "[Feature] Add SD-WAN API endpoints and CLI commands

REST API for link status, topology, policies, and events.
CLI commands: novactl sdwan status/links/topology."
```

---

### Task 15: Add WebUI SD-WAN topology page

**Files:**
- Create: `web/src/pages/SDWANOverview.tsx` (~350 lines)
- Create: `web/src/hooks/useSDWAN.ts` (~60 lines)
- Create: `web/src/types/sdwan.ts` (~50 lines)
- Modify: `web/src/App.tsx` (add route + lazy import)

**Step 1: Create types**

Create `web/src/types/sdwan.ts`:

```typescript
export interface WANLink {
  name: string;
  site: string;
  provider: string;
  role: string;
  bandwidth: string;
  latencyMs: number;
  jitterMs: number;
  packetLossPercent: number;
  score: number;
  healthy: boolean;
}

export interface SDWANTopology {
  sites: Array<{
    name: string;
    region: string;
    overlayAddr: string;
  }>;
  links: Array<{
    from: string;
    to: string;
    linkName: string;
    latencyMs: number;
    healthy: boolean;
  }>;
}

export interface WANPolicy {
  name: string;
  strategy: string;
  matchHosts: string[];
  dscpClass: string;
  selections: number;
}

export interface SDWANEvent {
  timestamp: string;
  type: string;
  fromLink: string;
  toLink: string;
  reason: string;
  policy: string;
}
```

**Step 2: Create hooks**

Create `web/src/hooks/useSDWAN.ts` with `useSDWANLinks()`, `useSDWANTopology()`, `useSDWANPolicies()`, `useSDWANEvents()` hooks following existing patterns.

**Step 3: Create the page**

Create `web/src/pages/SDWANOverview.tsx` with:
- Site topology visualization (colored nodes for sites, edges for links with quality indicators)
- Link quality table (sortable, with SLA threshold highlighting)
- Active policies table
- Recent events log

**Step 4: Add route to App.tsx**

In `web/src/App.tsx`, add lazy import and route:

```tsx
const SDWANOverview = lazy(() => import('./pages/SDWANOverview'));
// ...
<Route path="/sdwan" element={<SDWANOverview />} />
```

**Step 5: Build and commit**

```bash
cd web && pnpm build && cd ..
git add web/src/
git commit -m "[Feature] Add SD-WAN topology dashboard to WebUI

Topology map with site nodes and link edges, link quality table
with real-time latency/jitter/loss, policy list, and event log."
```

---

### Task 16: Add sample YAMLs and documentation

**Files:**
- Create: `config/samples/proxywanlink_primary_sample.yaml`
- Create: `config/samples/proxywanlink_backup_sample.yaml`
- Create: `config/samples/proxywanpolicy_voice_sample.yaml`
- Create: `config/samples/proxywanpolicy_bulk_sample.yaml`
- Create: `config/samples/novaedgeremotecluster_sdwan_sample.yaml`
- Create: `docs/user-guide/sdwan.md`
- Create: `docs/reference/proxywanlink.md`
- Create: `docs/reference/proxywanpolicy.md`
- Modify: `mkdocs.yml` (add nav entries)

**Step 1: Create sample YAMLs**

Example `config/samples/proxywanlink_primary_sample.yaml`:
```yaml
apiVersion: novaedge.io/v1alpha1
kind: ProxyWANLink
metadata:
  name: site-a-isp1
spec:
  site: site-a
  interface: eth0
  provider: "ISP-1"
  bandwidth: "1Gbps"
  cost: 100
  role: primary
  sla:
    maxLatency: 50ms
    maxJitter: 10ms
    maxPacketLoss: 1.0
  tunnelEndpoint:
    publicIP: "203.0.113.1"
    port: 51820
```

**Step 2: Create documentation**

- `docs/user-guide/sdwan.md`: Overview, setup guide, multi-site deployment walkthrough, troubleshooting
- `docs/reference/proxywanlink.md`: Full CRD field reference
- `docs/reference/proxywanpolicy.md`: Full CRD field reference with strategy examples

**Step 3: Update mkdocs.yml nav**

Add under User Guide: `- SD-WAN: user-guide/sdwan.md`
Add under Reference: `- ProxyWANLink: reference/proxywanlink.md`, `- ProxyWANPolicy: reference/proxywanpolicy.md`

**Step 4: Verify docs build**

```bash
mkdocs build --strict
```

**Step 5: Commit**

```bash
git add config/samples/ docs/ mkdocs.yml
git commit -m "[Docs] Add SD-WAN user guide, CRD reference, and sample YAMLs

Complete documentation for SD-WAN setup, ProxyWANLink and
ProxyWANPolicy CRD reference, plus sample configurations
for primary/backup links and voice/bulk policies."
```

---

### Task 17: Final verification and cleanup

**Step 1: Run full test suite**

```bash
make test
```

**Step 2: Run linter**

```bash
make lint
```

**Step 3: Build all binaries**

```bash
make build-all
```

**Step 4: Run go vet**

```bash
make vet
```

**Step 5: Verify docs**

```bash
mkdocs build --strict
```

**Step 6: Format all modified files**

```bash
gofmt -s -w internal/agent/tunnel/ internal/agent/sdwan/ internal/agent/vip/ internal/agent/metrics/ api/v1alpha1/ cmd/novactl/
```

**Step 7: Final commit if needed**

```bash
git add -A
git commit -m "[Chore] Final cleanup and formatting for SD-WAN implementation"
```

---

## Task Summary

| Task | Phase | Component | Est. Lines |
|------|-------|-----------|------------|
| 1 | Setup | Add wgctrl + pion/stun deps | go.mod only |
| 2 | 1 | Add OverlayCIDR to CRD | ~20 |
| 3 | 1 | Rewrite WireGuard (wgctrl) | ~350 |
| 4 | 1 | Overlay route manager | ~350 |
| 5 | 1 | BGP overlay prefix advertisement | ~80 |
| 6 | 2 | ProxyWANLink CRD types | ~150 |
| 7 | 2 | ProxyWANPolicy CRD types | ~120 |
| 8 | 2 | WAN link quality prober | ~600 |
| 9 | 2 | Path selection engine | ~500 |
| 10 | 2 | Multi-WAN link manager | ~500 |
| 11 | 2 | SD-WAN orchestrator + metrics | ~380 |
| 12 | 3 | STUN endpoint discovery | ~250 |
| 13 | 3 | DSCP packet marking | ~180 |
| 14 | 3 | novactl API + CLI | ~350 |
| 15 | 3 | WebUI topology dashboard | ~460 |
| 16 | 3 | Docs + samples | ~500 |
| 17 | 3 | Final verification | — |

**Total:** ~4,790 lines of new/rewritten code + documentation
