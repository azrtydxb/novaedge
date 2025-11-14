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
	"os/exec"
	"strings"
	"sync"
	"time"

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
	// Use ip addr add command
	cmd := exec.Command("ip", "addr", "add", cidr, "dev", h.interfaceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if address already exists
		if strings.Contains(string(output), "File exists") {
			h.logger.Debug("IP address already exists", zap.String("address", cidr))
			return nil
		}
		return fmt.Errorf("ip addr add failed: %s: %w", output, err)
	}

	return nil
}

// removeIPAddress removes an IP address from the network interface
func (h *L2Handler) removeIPAddress(cidr string) error {
	// Use ip addr del command
	cmd := exec.Command("ip", "addr", "del", cidr, "dev", h.interfaceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if address doesn't exist
		if strings.Contains(string(output), "Cannot assign") {
			h.logger.Debug("IP address doesn't exist", zap.String("address", cidr))
			return nil
		}
		return fmt.Errorf("ip addr del failed: %s: %w", output, err)
	}

	return nil
}

// sendGARP sends a gratuitous ARP announcement
func (h *L2Handler) sendGARP(ip net.IP) error {
	// Use arping to send GARP
	// arping -c 3 -A -I <interface> <ip>
	cmd := exec.Command("arping", "-c", "3", "-A", "-I", h.interfaceName, ip.String())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("arping failed: %s: %w", output, err)
	}

	h.logger.Debug("Sent GARP", zap.String("ip", ip.String()))
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
	// Get default route to find primary interface
	cmd := exec.Command("ip", "route", "show", "default")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get default route: %w", err)
	}

	// Parse output: "default via X.X.X.X dev <interface> ..."
	parts := strings.Fields(string(output))
	for i, part := range parts {
		if part == "dev" && i+1 < len(parts) {
			iface := parts[i+1]
			// Verify interface exists
			if _, err := net.InterfaceByName(iface); err == nil {
				return iface, nil
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
