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

package tunnel

import (
	"context"
	"fmt"
	"net"
	"strings"
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
	wireguardKeepalive       = 25
	wireguardType            = "wireguard"
	maxBackoff               = 30 * time.Second
	handshakeStaleThreshold  = 5 * time.Minute
)

// wireGuardTunnel implements the Tunnel interface using the wgctrl kernel API.
// It uses netlink for interface management and wgctrl for WireGuard configuration,
// replacing all CLI invocations (ip, wg) with direct kernel API calls.
type wireGuardTunnel struct {
	mu          sync.RWMutex
	clusterName string
	config      v1alpha1.TunnelConfig
	ifaceName   string
	localAddr   string
	overlayAddr string
	overlayCIDR string
	overlayIP   net.IP
	overlayNet  *net.IPNet
	privateKey  wgtypes.Key
	healthy     atomic.Bool
	logger      *zap.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	wgClient    *wgctrl.Client
}

// newWireGuardTunnel creates a WireGuard tunnel instance using the wgctrl kernel API.
// The overlayCIDR parameter specifies the overlay network address for this tunnel
// (e.g., "10.200.1.1/24"). If empty, overlay routing is disabled.
func newWireGuardTunnel(clusterName string, config v1alpha1.TunnelConfig, logger *zap.Logger) (*wireGuardTunnel, error) {
	if config.WireGuard == nil {
		return nil, fmt.Errorf("wireguard config is required for WireGuard tunnel type")
	}

	// Generate a deterministic interface name from the cluster name
	ifaceName := fmt.Sprintf("%s-%s", wireguardInterfacePrefix, sanitizeInterfaceName(clusterName))
	if len(ifaceName) > 15 {
		ifaceName = ifaceName[:15]
	}

	t := &wireGuardTunnel{
		clusterName: clusterName,
		config:      config,
		ifaceName:   ifaceName,
		logger:      logger.With(zap.String("tunnel", wireguardType), zap.String("cluster", clusterName)),
		done:        make(chan struct{}),
	}

	// Generate ephemeral private key
	privKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("generating wireguard private key: %w", err)
	}
	t.privateKey = privKey

	return t, nil
}

// newWireGuardTunnelWithOverlay creates a WireGuard tunnel with overlay CIDR configuration.
func newWireGuardTunnelWithOverlay(clusterName string, config v1alpha1.TunnelConfig, overlayCIDR string, logger *zap.Logger) (*wireGuardTunnel, error) {
	t, err := newWireGuardTunnel(clusterName, config, logger)
	if err != nil {
		return nil, err
	}

	if overlayCIDR != "" {
		ip, ipNet, parseErr := net.ParseCIDR(overlayCIDR)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid overlay CIDR %q: %w", overlayCIDR, parseErr)
		}
		t.overlayCIDR = overlayCIDR
		t.overlayIP = ip
		t.overlayNet = ipNet
	}

	return t, nil
}

// Start begins the WireGuard tunnel connection and maintenance loop.
func (t *wireGuardTunnel) Start(ctx context.Context) error {
	t.mu.Lock()
	t.ctx, t.cancel = context.WithCancel(ctx)
	t.mu.Unlock()

	go t.maintainConnection(t.ctx)

	return nil
}

// Stop gracefully shuts down the WireGuard tunnel.
func (t *wireGuardTunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cancel != nil {
		t.cancel()
	}

	// Wait for maintenance loop to exit
	<-t.done

	t.healthy.Store(false)

	// Close wgctrl client
	if t.wgClient != nil {
		if err := t.wgClient.Close(); err != nil {
			t.logger.Warn("failed to close wgctrl client", zap.Error(err))
		}
		t.wgClient = nil
	}

	// Tear down the WireGuard interface
	if err := t.teardownInterface(); err != nil {
		t.logger.Warn("failed to tear down wireguard interface", zap.Error(err))
		return err
	}

	return nil
}

// IsHealthy returns whether the WireGuard tunnel is connected.
func (t *wireGuardTunnel) IsHealthy() bool {
	return t.healthy.Load()
}

// LocalAddr returns the local tunnel address.
func (t *wireGuardTunnel) LocalAddr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.localAddr
}

// OverlayAddr returns the overlay network address assigned to this tunnel.
func (t *wireGuardTunnel) OverlayAddr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.overlayAddr
}

// Type returns the tunnel type identifier.
func (t *wireGuardTunnel) Type() string {
	return wireguardType
}

// maintainConnection keeps the WireGuard tunnel connected with exponential backoff.
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
			zap.String("overlayAddr", t.overlayAddr),
		)

		// Monitor the connection until it fails or context is cancelled
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

// connect sets up the WireGuard interface and configures peering using wgctrl.
func (t *wireGuardTunnel) connect(_ context.Context) error {
	wgConfig := t.config.WireGuard

	// Create wgctrl client
	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("creating wgctrl client: %w", err)
	}

	t.mu.Lock()
	t.wgClient = client
	t.mu.Unlock()

	// Create WireGuard interface using netlink
	link := &netlink.Wireguard{
		LinkAttrs: netlink.LinkAttrs{
			Name: t.ifaceName,
		},
	}

	if err := netlink.LinkAdd(link); err != nil {
		// Interface may already exist from a previous attempt
		if !strings.Contains(err.Error(), "file exists") {
			return fmt.Errorf("creating wireguard interface: %w", err)
		}
	}

	// Parse peer public key
	peerKey, err := wgtypes.ParseKey(wgConfig.PublicKey)
	if err != nil {
		return fmt.Errorf("parsing peer public key: %w", err)
	}

	// Build peer configuration
	keepalive := time.Duration(wireguardKeepalive) * time.Second
	if wgConfig.PersistentKeepalive != nil {
		keepalive = time.Duration(*wgConfig.PersistentKeepalive) * time.Second
	}

	peer := wgtypes.PeerConfig{
		PublicKey:                   peerKey,
		PersistentKeepaliveInterval: &keepalive,
		ReplaceAllowedIPs:           true,
	}

	// Parse endpoint
	if wgConfig.Endpoint != "" {
		endpoint, resolveErr := net.ResolveUDPAddr("udp", wgConfig.Endpoint)
		if resolveErr != nil {
			return fmt.Errorf("resolving wireguard endpoint %s: %w", wgConfig.Endpoint, resolveErr)
		}
		peer.Endpoint = endpoint
	}

	// Parse allowed IPs
	for _, cidr := range wgConfig.AllowedIPs {
		_, ipNet, parseErr := net.ParseCIDR(cidr)
		if parseErr != nil {
			return fmt.Errorf("parsing allowed IP %s: %w", cidr, parseErr)
		}
		peer.AllowedIPs = append(peer.AllowedIPs, *ipNet)
	}

	// Build device configuration
	deviceConfig := wgtypes.Config{
		PrivateKey:   &t.privateKey,
		ReplacePeers: true,
		Peers:        []wgtypes.PeerConfig{peer},
	}

	// Set listen port if specified
	if wgConfig.ListenPort != nil {
		listenPort := int(*wgConfig.ListenPort)
		deviceConfig.ListenPort = &listenPort
	}

	// Apply WireGuard configuration
	if err := client.ConfigureDevice(t.ifaceName, deviceConfig); err != nil {
		return fmt.Errorf("configuring wireguard device: %w", err)
	}

	// Assign overlay IP address if configured
	if t.overlayIP != nil && t.overlayNet != nil {
		nlAddr := &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   t.overlayIP,
				Mask: t.overlayNet.Mask,
			},
		}

		iface, linkErr := netlink.LinkByName(t.ifaceName)
		if linkErr != nil {
			return fmt.Errorf("getting wireguard interface: %w", linkErr)
		}

		if err := netlink.AddrReplace(iface, nlAddr); err != nil {
			return fmt.Errorf("assigning overlay address %s: %w", t.overlayCIDR, err)
		}
	}

	// Bring the interface up
	iface, err := netlink.LinkByName(t.ifaceName)
	if err != nil {
		return fmt.Errorf("getting wireguard interface for link up: %w", err)
	}

	if err := netlink.LinkSetUp(iface); err != nil {
		return fmt.Errorf("bringing up wireguard interface: %w", err)
	}

	// Set local address
	t.mu.Lock()
	if t.overlayIP != nil {
		t.overlayAddr = t.overlayIP.String()
		t.localAddr = t.overlayIP.String()
	} else {
		t.localAddr = fmt.Sprintf("%s:15002", t.ifaceName)
	}
	t.mu.Unlock()

	return nil
}

// monitorConnection checks the WireGuard handshake periodically using wgctrl.
func (t *wireGuardTunnel) monitorConnection(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)
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

// checkHandshake verifies the WireGuard handshake is recent enough using wgctrl.
func (t *wireGuardTunnel) checkHandshake() error {
	t.mu.RLock()
	client := t.wgClient
	t.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("wgctrl client not initialized")
	}

	device, err := client.Device(t.ifaceName)
	if err != nil {
		return fmt.Errorf("querying wireguard device: %w", err)
	}

	for _, peer := range device.Peers {
		if peer.LastHandshakeTime.IsZero() {
			return fmt.Errorf("no wireguard handshake established")
		}

		since := time.Since(peer.LastHandshakeTime)
		if since > handshakeStaleThreshold {
			return fmt.Errorf("wireguard handshake stale (last: %v ago)", since)
		}
	}

	return nil
}

// teardownInterface removes the WireGuard interface using netlink.
func (t *wireGuardTunnel) teardownInterface() error {
	link, err := netlink.LinkByName(t.ifaceName)
	if err != nil {
		// Interface may not exist
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return fmt.Errorf("looking up interface %s: %w", t.ifaceName, err)
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("deleting interface %s: %w", t.ifaceName, err)
	}

	return nil
}

// sanitizeInterfaceName converts a cluster name to a valid interface name suffix.
func sanitizeInterfaceName(name string) string {
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(name))

	return strings.Trim(result, "-")
}

// minDuration returns the smaller of two durations.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}

	return b
}
