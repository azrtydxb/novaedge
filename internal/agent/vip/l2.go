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

// L2Handler manages L2 ARP VIP mode
type L2Handler struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Active VIPs
	activeVIPs map[string]*VIPState

	// Network interface to use
	interfaceName string
}

// VIPState tracks the state of a VIP
type VIPState struct {
	Assignment *pb.VIPAssignment
	IP         net.IP
	AddedAt    time.Time
}

// NewL2Handler creates a new L2 ARP handler
func NewL2Handler(logger *zap.Logger) (*L2Handler, error) {
	// Detect primary network interface
	iface, err := detectPrimaryInterface()
	if err != nil {
		return nil, fmt.Errorf("failed to detect network interface: %w", err)
	}

	logger.Info("Using network interface for VIPs", zap.String("interface", iface))

	return &L2Handler{
		logger:        logger,
		activeVIPs:    make(map[string]*VIPState),
		interfaceName: iface,
	}, nil
}

// Start starts the L2 handler
func (h *L2Handler) Start(ctx context.Context) error {
	h.logger.Info("Starting L2 ARP handler")

	// Start GARP announcement loop
	go h.garpAnnouncer(ctx)

	return nil
}

// AddVIP adds a VIP to the network interface
func (h *L2Handler) AddVIP(assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if already active
	if _, exists := h.activeVIPs[assignment.VipName]; exists {
		h.logger.Debug("VIP already active", zap.String("vip", assignment.VipName))
		return nil
	}

	// Parse IP address
	ip, _, err := net.ParseCIDR(assignment.Address)
	if err != nil {
		return fmt.Errorf("invalid VIP address %s: %w", assignment.Address, err)
	}

	h.logger.Info("Adding VIP to interface",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
		zap.String("interface", h.interfaceName),
	)

	// Add IP address to interface
	if err := h.addIPAddress(assignment.Address); err != nil {
		return fmt.Errorf("failed to add IP address: %w", err)
	}

	// Send gratuitous ARP
	if err := h.sendGARP(ip); err != nil {
		h.logger.Warn("Failed to send GARP",
			zap.String("vip", assignment.VipName),
			zap.Error(err),
		)
		// Don't fail the whole operation if GARP fails
	}

	// Track VIP state
	h.activeVIPs[assignment.VipName] = &VIPState{
		Assignment: assignment,
		IP:         ip,
		AddedAt:    time.Now(),
	}

	h.logger.Info("VIP added successfully",
		zap.String("vip", assignment.VipName),
		zap.String("address", assignment.Address),
	)

	return nil
}

// RemoveVIP removes a VIP from the network interface
func (h *L2Handler) RemoveVIP(assignment *pb.VIPAssignment) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, exists := h.activeVIPs[assignment.VipName]
	if !exists {
		h.logger.Debug("VIP not active", zap.String("vip", assignment.VipName))
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
	// Get the network link/interface
	link, err := netlink.LinkByName(h.interfaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", h.interfaceName, err)
	}

	// Parse the CIDR address
	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("failed to parse address %s: %w", cidr, err)
	}

	// Add the address to the interface
	if err := netlink.AddrAdd(link, addr); err != nil {
		// Check if address already exists (EEXIST error)
		if err == syscall.EEXIST {
			h.logger.Debug("IP address already exists", zap.String("address", cidr))
			return nil
		}
		return fmt.Errorf("failed to add address: %w", err)
	}

	return nil
}

// removeIPAddress removes an IP address from the network interface
func (h *L2Handler) removeIPAddress(cidr string) error {
	// Get the network link/interface
	link, err := netlink.LinkByName(h.interfaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", h.interfaceName, err)
	}

	// Parse the CIDR address
	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("failed to parse address %s: %w", cidr, err)
	}

	// Remove the address from the interface
	if err := netlink.AddrDel(link, addr); err != nil {
		// Check if address doesn't exist (EADDRNOTAVAIL error)
		if err == syscall.EADDRNOTAVAIL {
			h.logger.Debug("IP address doesn't exist", zap.String("address", cidr))
			return nil
		}
		return fmt.Errorf("failed to remove address: %w", err)
	}

	return nil
}

// sendGARP sends a gratuitous ARP announcement
func (h *L2Handler) sendGARP(ip net.IP) error {
	// Get the network interface
	iface, err := net.InterfaceByName(h.interfaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}

	// Get interface hardware address (MAC)
	hwAddr := iface.HardwareAddr
	if len(hwAddr) == 0 {
		return fmt.Errorf("interface %s has no hardware address", h.interfaceName)
	}

	// Convert IPv4 address to 4-byte format
	ipv4 := ip.To4()
	if ipv4 == nil {
		// For IPv6, we would use NDP instead of ARP
		// For now, just log and return (IPv6 GARP is handled differently)
		h.logger.Debug("Skipping GARP for IPv6 address", zap.String("ip", ip.String()))
		return nil
	}

	// Create a gratuitous ARP packet
	// In a gratuitous ARP:
	// - Sender IP = Target IP (the IP we're announcing)
	// - Sender MAC = our MAC
	// - Target MAC = broadcast (ff:ff:ff:ff:ff:ff)
	// - Operation = ARP Reply (2) for GARP

	// Create ARP client for the interface
	client, err := arp.Dial(iface)
	if err != nil {
		// If we can't send GARP, log but don't fail - the IP is already added to the interface
		h.logger.Warn("Failed to create ARP client for GARP, continuing anyway",
			zap.String("interface", h.interfaceName),
			zap.Error(err))
		return nil
	}
	defer client.Close()

	// Convert net.IP to netip.Addr
	senderIP, ok := netip.AddrFromSlice(ipv4)
	if !ok {
		return fmt.Errorf("failed to convert IP address to netip.Addr")
	}

	// Create gratuitous ARP packet
	// Both sender and target IP are set to the VIP we're announcing
	packet := &arp.Packet{
		HardwareType:       1,      // Ethernet
		ProtocolType:       0x0800, // IPv4
		HardwareAddrLength: 6,
		IPLength:           4,
		Operation:          arp.OperationReply, // Gratuitous ARP uses Reply
		SenderHardwareAddr: hwAddr,
		SenderIP:           senderIP,
		TargetHardwareAddr: ethernet.Broadcast, // Broadcast MAC
		TargetIP:           senderIP,           // Same as sender for GARP
	}

	// Send the GARP packet
	if err := client.WriteTo(packet, ethernet.Broadcast); err != nil {
		// Log but don't fail - GARP is optimization, not critical
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

// garpAnnouncer periodically sends GARP for active VIPs
func (h *L2Handler) garpAnnouncer(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("GARP announcer stopped")
			return

		case <-ticker.C:
			h.announceActiveVIPs()
		}
	}
}

// announceActiveVIPs sends GARP for all active VIPs
func (h *L2Handler) announceActiveVIPs() {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.activeVIPs) == 0 {
		return
	}

	h.logger.Debug("Announcing active VIPs", zap.Int("count", len(h.activeVIPs)))

	for vipName, state := range h.activeVIPs {
		if err := h.sendGARP(state.IP); err != nil {
			h.logger.Warn("Failed to send GARP",
				zap.String("vip", vipName),
				zap.Error(err),
			)
		}
	}
}

// detectPrimaryInterface detects the primary network interface
func detectPrimaryInterface() (string, error) {
	// Get default route to find primary interface using netlink
	routes, err := netlink.RouteList(nil, syscall.AF_INET)
	if err != nil {
		return "", fmt.Errorf("failed to list routes: %w", err)
	}

	// Find the default route (destination 0.0.0.0/0)
	for _, route := range routes {
		if route.Dst == nil {
			// Default route found
			if route.LinkIndex > 0 {
				link, err := netlink.LinkByIndex(route.LinkIndex)
				if err != nil {
					continue
				}
				return link.Attrs().Name, nil
			}
		}
	}

	// Fallback to first non-loopback interface
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
