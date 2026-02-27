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
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
)

var (
	errNoOverlayRouteFoundForCIDR = errors.New("no overlay route found for CIDR")
)

// overlayRoute tracks a single overlay route installed in the kernel routing table.
type overlayRoute struct {
	CIDR     string
	LinkName string
	Route    *netlink.Route
}

// OverlayRouteManager manages overlay network routes for site-to-site connectivity.
// It installs and removes kernel routes that direct traffic for remote overlay CIDRs
// through the appropriate WireGuard tunnel interface.
type OverlayRouteManager struct {
	mu     sync.RWMutex
	routes map[string]*overlayRoute // remoteCIDR -> overlayRoute
	logger *zap.Logger
}

// NewOverlayRouteManager creates a new overlay route manager.
func NewOverlayRouteManager(logger *zap.Logger) *OverlayRouteManager {
	return &OverlayRouteManager{
		routes: make(map[string]*overlayRoute),
		logger: logger.With(zap.String("component", "overlay-route-manager")),
	}
}

// InstallRoute installs a kernel route for the given remote CIDR through the
// specified network interface. If a route for the CIDR already exists, it is
// replaced with the new configuration.
func (m *OverlayRouteManager) InstallRoute(remoteCIDR, linkName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, dst, err := net.ParseCIDR(remoteCIDR)
	if err != nil {
		return fmt.Errorf("parsing remote CIDR %q: %w", remoteCIDR, err)
	}

	link, err := netlink.LinkByName(linkName)
	if err != nil {
		return fmt.Errorf("looking up interface %q: %w", linkName, err)
	}

	route := &netlink.Route{
		Dst:       dst,
		LinkIndex: link.Attrs().Index,
	}

	if err := netlink.RouteReplace(route); err != nil {
		return fmt.Errorf("installing route %s via %s: %w", remoteCIDR, linkName, err)
	}

	m.routes[remoteCIDR] = &overlayRoute{
		CIDR:     remoteCIDR,
		LinkName: linkName,
		Route:    route,
	}

	m.logger.Info("overlay route installed",
		zap.String("cidr", remoteCIDR),
		zap.String("interface", linkName),
	)

	return nil
}

// RemoveRoute removes the kernel route for the given remote CIDR.
func (m *OverlayRouteManager) RemoveRoute(remoteCIDR string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.routes[remoteCIDR]
	if !ok {
		return fmt.Errorf("%w: %s", errNoOverlayRouteFoundForCIDR, remoteCIDR)
	}

	if err := netlink.RouteDel(existing.Route); err != nil {
		return fmt.Errorf("removing route for %s: %w", remoteCIDR, err)
	}

	delete(m.routes, remoteCIDR)

	m.logger.Info("overlay route removed",
		zap.String("cidr", remoteCIDR),
		zap.String("interface", existing.LinkName),
	)

	return nil
}

// RemoveAllRoutes removes all managed overlay routes.
func (m *OverlayRouteManager) RemoveAllRoutes() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for cidr, route := range m.routes {
		if err := netlink.RouteDel(route.Route); err != nil {
			m.logger.Warn("failed to remove overlay route during cleanup",
				zap.String("cidr", cidr),
				zap.Error(err),
			)
		} else {
			m.logger.Info("overlay route removed during cleanup", zap.String("cidr", cidr))
		}
	}

	m.routes = make(map[string]*overlayRoute)
}

// GetRoutes returns a copy of the current CIDR-to-linkName mapping.
func (m *OverlayRouteManager) GetRoutes() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]string, len(m.routes))
	for cidr, route := range m.routes {
		result[cidr] = route.LinkName
	}

	return result
}
