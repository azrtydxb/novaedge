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

package sdwan

import (
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"
)
var (
	errLinkNameIsRequired = errors.New("link name is required")
	errLink = errors.New("link")
	errNoLinkQualityDataAvailable = errors.New("no link quality data available")
)


// LinkState describes the operational state of a WAN link.
type LinkState string

const (
	// LinkStateActive means the link is operating within SLA.
	LinkStateActive LinkState = "active"
	// LinkStateDegraded means the link is operational but SLA is violated.
	LinkStateDegraded LinkState = "degraded"
	// LinkStateDown means the link is not operational.
	LinkStateDown LinkState = "down"
)

// WANLinkRole defines the role of a link in multi-WAN configurations.
// Local constants to avoid importing the CRD package.
type WANLinkRole string

const (
	// RolePrimary is the preferred link.
	RolePrimary WANLinkRole = "primary"
	// RoleBackup is used when primary links are unavailable.
	RoleBackup WANLinkRole = "backup"
	// RoleLoadbalance participates in active load balancing.
	RoleLoadbalance WANLinkRole = "loadbalance"
)

// TunnelEndpoint defines a public endpoint for tunnel traffic.
type TunnelEndpoint struct {
	PublicIP string
	Port     int32
}

// LinkConfig is the configuration used to add a new managed link.
type LinkConfig struct {
	Name           string
	Site           string
	Provider       string
	Role           WANLinkRole
	Bandwidth      string
	Cost           int32
	SLA            *WANLinkSLA
	TunnelEndpoint *TunnelEndpoint
	ProbeAddr      string // address:port for quality probing
	RemoteSite     string // remote site name for probing
}

// ManagedLink represents a WAN link that is actively managed by the link manager.
type ManagedLink struct {
	Name           string
	Site           string
	Provider       string
	Role           WANLinkRole
	Bandwidth      string
	Cost           int32
	SLA            *WANLinkSLA
	TunnelEndpoint *TunnelEndpoint
	State          LinkState
	Quality        *LinkQuality
}

// LinkManager manages the lifecycle and state of multiple WAN links.
// It integrates with the Prober for quality monitoring and the PathSelector
// for optimal link selection.
type LinkManager struct {
	mu       sync.RWMutex
	links    map[string]*ManagedLink
	prober   *Prober
	selector *PathSelector
	logger   *zap.Logger
}

// NewLinkManager creates a new link manager.
func NewLinkManager(prober *Prober, selector *PathSelector, logger *zap.Logger) *LinkManager {
	return &LinkManager{
		links:    make(map[string]*ManagedLink),
		prober:   prober,
		selector: selector,
		logger:   logger.Named("sdwan-linkmanager"),
	}
}

// AddLink registers a new WAN link and begins monitoring its quality.
func (m *LinkManager) AddLink(config LinkConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if config.Name == "" {
		return errLinkNameIsRequired
	}
	if _, exists := m.links[config.Name]; exists {
		return fmt.Errorf("%w: %q already exists", errLink, config.Name)
	}

	link := &ManagedLink{
		Name:           config.Name,
		Site:           config.Site,
		Provider:       config.Provider,
		Role:           config.Role,
		Bandwidth:      config.Bandwidth,
		Cost:           config.Cost,
		SLA:            config.SLA,
		TunnelEndpoint: config.TunnelEndpoint,
		State:          LinkStateActive,
	}
	m.links[config.Name] = link

	// Register with prober if a probe address is provided
	if config.ProbeAddr != "" {
		m.prober.AddTarget(config.Name, config.RemoteSite, config.ProbeAddr, config.SLA)
	}

	m.logger.Info("Link added",
		zap.String("name", config.Name),
		zap.String("site", config.Site),
		zap.String("provider", config.Provider),
		zap.String("role", string(config.Role)),
	)
	return nil
}

// RemoveLink unregisters a WAN link and stops monitoring.
func (m *LinkManager) RemoveLink(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.links[name]; !exists {
		return fmt.Errorf("%w: %q not found", errLink, name)
	}

	delete(m.links, name)
	m.prober.RemoveTarget(name)

	m.logger.Info("Link removed", zap.String("name", name))
	return nil
}

// GetLink returns the managed link with the given name.
func (m *LinkManager) GetLink(name string) (*ManagedLink, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	link, exists := m.links[name]
	if !exists {
		return nil, false
	}
	// Return a copy to avoid races
	cp := *link
	return &cp, true
}

// GetAllLinks returns all managed links.
func (m *LinkManager) GetAllLinks() []*ManagedLink {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*ManagedLink, 0, len(m.links))
	for _, link := range m.links {
		cp := *link
		result = append(result, &cp)
	}
	return result
}

// GetLinksForSite returns all managed links belonging to the given site.
func (m *LinkManager) GetLinksForSite(site string) []*ManagedLink {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*ManagedLink
	for _, link := range m.links {
		if link.Site == site {
			cp := *link
			result = append(result, &cp)
		}
	}
	return result
}

// UpdateLinkStates refreshes the state of all links based on current probe quality data.
// A link is "active" if healthy, "degraded" if unhealthy but responding, and "down"
// if packet loss is 100%.
func (m *LinkManager) UpdateLinkStates() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, link := range m.links {
		quality := m.prober.GetQuality(name)
		if quality == nil {
			continue
		}
		link.Quality = quality

		switch {
		case quality.PacketLoss >= 1.0:
			if link.State != LinkStateDown {
				m.logger.Warn("Link is down",
					zap.String("name", name),
					zap.Float64("packet_loss", quality.PacketLoss),
				)
			}
			link.State = LinkStateDown
		case !quality.Healthy:
			if link.State == LinkStateActive {
				m.logger.Warn("Link degraded",
					zap.String("name", name),
					zap.Float64("latency_ms", quality.LatencyMs),
					zap.Float64("jitter_ms", quality.JitterMs),
					zap.Float64("packet_loss", quality.PacketLoss),
				)
			}
			link.State = LinkStateDegraded
		default:
			if link.State != LinkStateActive {
				m.logger.Info("Link recovered to active",
					zap.String("name", name),
				)
			}
			link.State = LinkStateActive
		}
	}
}

// SelectPathForPolicy selects the best link for a policy using the given strategy.
// It uses current quality data from all healthy links.
func (m *LinkManager) SelectPathForPolicy(policyName, strategy string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	qualities := m.prober.GetAllQualities()
	if len(qualities) == 0 {
		return "", errNoLinkQualityDataAvailable
	}

	// Build link quality list and metadata maps
	links := make([]LinkQuality, 0, len(qualities))
	bandwidths := make(map[string]string, len(m.links))
	costs := make(map[string]int32, len(m.links))

	for name, q := range qualities {
		links = append(links, *q)
		if link, exists := m.links[name]; exists {
			bandwidths[name] = link.Bandwidth
			costs[name] = link.Cost
		}
	}

	return m.selector.Select(policyName, strategy, links, bandwidths, costs)
}

// ListLinks returns the names of all managed links.
func (m *LinkManager) ListLinks() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.links))
	for name := range m.links {
		names = append(names, name)
	}
	return names
}
