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

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// validateOverlayCIDR validates that the given string is a valid CIDR notation.
// It returns an error if the CIDR is empty or cannot be parsed.
func validateOverlayCIDR(cidr string) error {
	if cidr == "" {
		return fmt.Errorf("overlay CIDR must not be empty")
	}

	_, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid overlay CIDR %q: %w", cidr, err)
	}

	return nil
}

// AnnounceOverlayPrefix announces an overlay network prefix via BGP.
// This is used for SD-WAN site-to-site routing, where each site advertises
// its overlay CIDR to peer routers so traffic can be forwarded between sites.
// The BGP server must be started before calling this method.
func (h *BGPHandler) AnnounceOverlayPrefix(ctx context.Context, cidr string, config *pb.BGPConfig) error {
	if err := validateOverlayCIDR(cidr); err != nil {
		return err
	}

	if config == nil {
		return fmt.Errorf("BGP config is required for overlay prefix announcement")
	}

	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parsing overlay CIDR: %w", err)
	}

	// Use the network address, not the host address
	networkIP := ipNet.IP
	isIPv6 := networkIP.To4() == nil

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.bgpServer == nil {
		return fmt.Errorf("BGP server not started, cannot announce overlay prefix")
	}

	h.logger.Info("announcing overlay prefix via BGP",
		zap.String("cidr", cidr),
		zap.String("network", networkIP.String()),
		zap.String("host_ip", ip.String()),
		zap.Bool("ipv6", isIPv6),
	)

	if err := h.announceRoute(ctx, networkIP, config, isIPv6); err != nil {
		return fmt.Errorf("announcing overlay prefix %s: %w", cidr, err)
	}

	h.logger.Info("overlay prefix announced via BGP successfully",
		zap.String("cidr", cidr),
	)

	return nil
}

// WithdrawOverlayPrefix withdraws an overlay network prefix from BGP.
// This is the inverse of AnnounceOverlayPrefix and should be called when
// a site's tunnel goes down or the SD-WAN configuration changes.
func (h *BGPHandler) WithdrawOverlayPrefix(ctx context.Context, cidr string, config *pb.BGPConfig) error {
	if err := validateOverlayCIDR(cidr); err != nil {
		return err
	}

	if config == nil {
		return fmt.Errorf("BGP config is required for overlay prefix withdrawal")
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parsing overlay CIDR: %w", err)
	}

	networkIP := ipNet.IP
	isIPv6 := networkIP.To4() == nil

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.bgpServer == nil {
		return fmt.Errorf("BGP server not started, cannot withdraw overlay prefix")
	}

	h.logger.Info("withdrawing overlay prefix from BGP",
		zap.String("cidr", cidr),
		zap.Bool("ipv6", isIPv6),
	)

	if err := h.withdrawRoute(ctx, networkIP, config, isIPv6); err != nil {
		return fmt.Errorf("withdrawing overlay prefix %s: %w", cidr, err)
	}

	h.logger.Info("overlay prefix withdrawn from BGP successfully",
		zap.String("cidr", cidr),
	)

	return nil
}
