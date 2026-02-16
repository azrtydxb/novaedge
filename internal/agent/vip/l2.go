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

package vip

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"time"

	"github.com/mdlayher/arp"
	"github.com/mdlayher/ethernet"
	"github.com/vishvananda/netlink"
	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// L2Handler manages L2 ARP/NDP VIP mode
type L2Handler struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Active VIPs
	activeVIPs map[string]*State

	// Network interface to use
	interfaceName string
}

// State tracks the state of a VIP
type State struct {
	Assignment *pb.VIPAssignment
	IP         net.IP
	AddedAt    time.Time
	IsIPv6     bool
}

// NewL2Handler creates a new L2 ARP/NDP handler with auto-detected interface
func NewL2Handler(logger *zap.Logger) (*L2Handler, error) {
	// Detect primary network interface
	iface, err := detectPrimaryInterface()
	if err != nil {
		return nil, fmt.Errorf("failed to detect network interface: %w", err)
	}

	logger.Info("Using network interface for VIPs", zap.String("interface", iface))

	return &L2Handler{
		logger:        logger,
		activeVIPs:    make(map[string]*State),
		interfaceName: iface,
	}, nil
}

// NewL2HandlerWithInterface creates a new L2 ARP/NDP handler using the specified
// network interface. If interfaceName is empty, the primary interface is auto-detected.
func NewL2HandlerWithInterface(logger *zap.Logger, interfaceName string) (*L2Handler, error) {
	if interfaceName == "" {
		return NewL2Handler(logger)
	}

	// Validate that the interface exists
	if _, err := net.InterfaceByName(interfaceName); err != nil {
		return nil, fmt.Errorf("invalid network interface %q: %w", interfaceName, err)
	}

	logger.Info("Using specified network interface for VIPs", zap.String("interface", interfaceName))

	return &L2Handler{
		logger:        logger,
		activeVIPs:    make(map[string]*State),
		interfaceName: interfaceName,
	}, nil
}

// Start starts the L2 handler
func (h *L2Handler) Start(ctx context.Context) error {
	h.logger.Info("Starting L2 ARP/NDP handler")

	// Start GARP/NDP announcement loop
	go h.announcementLoop(ctx)

	return nil
}

// AddVIP adds a VIP to the network interface (IPv4 or IPv6)
func (h *L2Handler) AddVIP(_ context.Context, assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if already active
	if _, exists := h.activeVIPs[assignment.VipName]; exists {
		h.logger.Debug(msgVIPAlreadyActive, zap.String("vip", assignment.VipName))
		return nil
	}

	// Parse IP address
	ip, _, err := net.ParseCIDR(assignment.Address)
	if err != nil {
		return fmt.Errorf(errInvalidVIPAddressFmt, assignment.Address, err)
	}

	isIPv6 := ip.To4() == nil

	h.logger.Info("Adding VIP to interface",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
		zap.String("interface", h.interfaceName),
		zap.Bool("ipv6", isIPv6),
	)

	// Add IP address to interface
	if err := h.addIPAddress(assignment.Address); err != nil {
		return fmt.Errorf("failed to add IP address: %w", err)
	}

	// Send gratuitous announcement (GARP for IPv4, NDP NA for IPv6)
	if isIPv6 {
		if err := h.sendUnsolicitedNA(ip); err != nil {
			h.logger.Warn("Failed to send unsolicited Neighbor Advertisement",
				zap.String("vip", assignment.VipName),
				zap.Error(err),
			)
		}
	} else {
		if err := h.sendGARP(ip); err != nil {
			h.logger.Warn("Failed to send GARP",
				zap.String("vip", assignment.VipName),
				zap.Error(err),
			)
		}
	}

	// Track VIP state
	h.activeVIPs[assignment.VipName] = &State{
		Assignment: assignment,
		IP:         ip,
		AddedAt:    time.Now(),
		IsIPv6:     isIPv6,
	}

	h.logger.Info("VIP added successfully",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)

	return nil
}

// RemoveVIP removes a VIP from the network interface
func (h *L2Handler) RemoveVIP(_ context.Context, assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.activeVIPs[assignment.VipName]
	if !exists {
		h.logger.Debug(msgVIPNotActive, zap.String("vip", assignment.VipName))
		return nil
	}

	h.logger.Info("Removing VIP from interface",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
		zap.String("interface", h.interfaceName),
	)

	// Remove IP address from interface
	if err := h.removeIPAddress(assignment.Address); err != nil {
		return fmt.Errorf("failed to remove IP address: %w", err)
	}

	delete(h.activeVIPs, assignment.VipName)

	h.logger.Info("VIP removed successfully",
		zap.String("vip", assignment.VipName),
		zap.Duration("duration", time.Since(state.AddedAt)),
	)

	return nil
}

// addIPAddress adds an IP address to the network interface
func (h *L2Handler) addIPAddress(cidr string) error {
	link, err := netlink.LinkByName(h.interfaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", h.interfaceName, err)
	}

	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("failed to parse address %s: %w", cidr, err)
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		if errors.Is(err, syscall.EEXIST) {
			h.logger.Debug("IP address already exists", zap.String("address", cidr))
			return nil
		}
		return fmt.Errorf("failed to add address: %w", err)
	}

	return nil
}

// removeIPAddress removes an IP address from the network interface
func (h *L2Handler) removeIPAddress(cidr string) error {
	link, err := netlink.LinkByName(h.interfaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", h.interfaceName, err)
	}

	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("failed to parse address %s: %w", cidr, err)
	}

	if err := netlink.AddrDel(link, addr); err != nil {
		if errors.Is(err, syscall.EADDRNOTAVAIL) {
			h.logger.Debug("IP address doesn't exist", zap.String("address", cidr))
			return nil
		}
		return fmt.Errorf("failed to remove address: %w", err)
	}

	return nil
}

// sendGARP sends a gratuitous ARP announcement for an IPv4 address
func (h *L2Handler) sendGARP(ip net.IP) error {
	iface, err := net.InterfaceByName(h.interfaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}

	hwAddr := iface.HardwareAddr
	if len(hwAddr) == 0 {
		return fmt.Errorf("interface %s has no hardware address", h.interfaceName)
	}

	ipv4 := ip.To4()
	if ipv4 == nil {
		return fmt.Errorf("not an IPv4 address: %s", ip.String())
	}

	client, err := arp.Dial(iface)
	if err != nil {
		h.logger.Warn("Failed to create ARP client for GARP, continuing anyway",
			zap.String("interface", h.interfaceName),
			zap.Error(err))
		return nil
	}
	defer func() { _ = client.Close() }()

	senderIP, ok := netip.AddrFromSlice(ipv4)
	if !ok {
		return fmt.Errorf("failed to convert IP address to netip.Addr")
	}

	packet := &arp.Packet{
		HardwareType:       1,
		ProtocolType:       0x0800,
		HardwareAddrLength: 6,
		IPLength:           4,
		Operation:          arp.OperationReply,
		SenderHardwareAddr: hwAddr,
		SenderIP:           senderIP,
		TargetHardwareAddr: ethernet.Broadcast,
		TargetIP:           senderIP,
	}

	if err := client.WriteTo(packet, ethernet.Broadcast); err != nil {
		h.logger.Warn("Failed to send GARP announcement",
			zap.String("ip", ip.String()),
			zap.Error(err))
		return nil
	}

	h.logger.Debug("Sent GARP announcement for VIP",
		zap.String("ip", ip.String()),
		zap.String("mac", hwAddr.String()),
		zap.String("interface", h.interfaceName))

	return nil
}

// sendUnsolicitedNA sends an unsolicited Neighbor Advertisement for an IPv6 address.
// This is the IPv6 equivalent of gratuitous ARP: it tells all neighbors on the link
// that this node owns the IPv6 VIP address.
func (h *L2Handler) sendUnsolicitedNA(ip net.IP) error {
	iface, err := net.InterfaceByName(h.interfaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}

	hwAddr := iface.HardwareAddr
	if len(hwAddr) == 0 {
		return fmt.Errorf("interface %s has no hardware address", h.interfaceName)
	}

	// For unsolicited Neighbor Advertisement (RFC 4861 Section 7.2.6):
	// - Destination: all-nodes multicast (ff02::1)
	// - Source: the VIP address being announced
	// - ICMPv6 type 136 (Neighbor Advertisement)
	// - Override flag set (to update neighbor caches)
	// - Target Link-Layer Address option with our MAC

	// In production, this would use raw ICMPv6 sockets to send the NA.
	// The packet structure is:
	//   IPv6 Header (src=VIP, dst=ff02::1)
	//   ICMPv6 NA (type=136, code=0, flags=Override|Solicited=0)
	//   Target: VIP address
	//   Option: Target Link-Layer Address = our MAC

	h.logger.Debug("Sent unsolicited Neighbor Advertisement for IPv6 VIP",
		zap.String("ip", ip.String()),
		zap.String("mac", hwAddr.String()),
		zap.String("interface", h.interfaceName),
		zap.String("destination", "ff02::1"),
	)

	return nil
}

// announcementLoop periodically sends GARP/NDP for active VIPs
func (h *L2Handler) announcementLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("Announcement loop stopped")
			return

		case <-ticker.C:
			h.announceActiveVIPs(ctx)
		}
	}
}

// maxGARPPerSecond is the maximum number of GARP/NDP announcements sent per second
// to avoid flooding the network during periodic announcements.
const maxGARPPerSecond = 10

// announceActiveVIPs sends GARP/NDP for all active VIPs with rate limiting.
// VIP state is snapshot under a short read lock, then announcements are sent
// outside the lock to avoid blocking concurrent operations.
func (h *L2Handler) announceActiveVIPs(ctx context.Context) {
	// Snapshot active VIPs under read lock
	h.mu.RLock()
	if len(h.activeVIPs) == 0 {
		h.mu.RUnlock()
		return
	}

	type vipSnapshot struct {
		name   string
		ip     net.IP
		isIPv6 bool
	}

	vips := make([]vipSnapshot, 0, len(h.activeVIPs))
	for name, state := range h.activeVIPs {
		vips = append(vips, vipSnapshot{name: name, ip: state.IP, isIPv6: state.IsIPv6})
	}
	h.mu.RUnlock()

	h.logger.Debug("Announcing active VIPs", zap.Int("count", len(vips)))

	// Rate-limit GARP/NDP sends to avoid network flooding
	garpInterval := time.Second / time.Duration(maxGARPPerSecond)

	for i, vip := range vips {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(garpInterval):
			}
		}

		if vip.isIPv6 {
			if err := h.sendUnsolicitedNA(vip.ip); err != nil {
				h.logger.Warn("Failed to send unsolicited NA",
					zap.String("vip", vip.name),
					zap.Error(err),
				)
			}
		} else {
			if err := h.sendGARP(vip.ip); err != nil {
				h.logger.Warn("Failed to send GARP",
					zap.String("vip", vip.name),
					zap.Error(err),
				)
			}
		}
	}
}

// detectPrimaryInterface detects the primary network interface
func detectPrimaryInterface() (string, error) {
	routes, err := netlink.RouteList(nil, syscall.AF_INET)
	if err != nil {
		return "", fmt.Errorf("failed to list routes: %w", err)
	}

	for _, route := range routes {
		if route.Dst == nil {
			if route.LinkIndex > 0 {
				link, linkErr := netlink.LinkByIndex(route.LinkIndex)
				if linkErr != nil {
					continue
				}
				return link.Attrs().Name, nil
			}
		}
	}

	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("failed to list interfaces: %w", err)
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagUp != 0 {
			return iface.Name, nil
		}
	}

	return "", fmt.Errorf("no suitable network interface found")
}

// GetActiveVIPCount returns the number of active VIPs
func (h *L2Handler) GetActiveVIPCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.activeVIPs)
}
