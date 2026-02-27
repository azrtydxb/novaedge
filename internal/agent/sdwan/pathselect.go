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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	errNoLinksAvailableForSelection  = errors.New("no links available for selection")
	errNoHealthyLinkFoundForStrategy = errors.New("no healthy link found for strategy")
)

// Local strategy constants matching the CRD values to avoid an import cycle.
const (
	StrategyLowestLatency    = "lowest-latency"
	StrategyHighestBandwidth = "highest-bandwidth"
	StrategyMostReliable     = "most-reliable"
	StrategyLowestCost       = "lowest-cost"
)

// defaultHysteresis is the minimum duration between path switches
// to prevent flapping between links with similar metrics.
const defaultHysteresis = 10 * time.Second

// PathSelector selects the optimal WAN link for a given strategy
// based on current link quality metrics. It implements hysteresis
// to prevent rapid flapping between links with similar quality.
type PathSelector struct {
	mu           sync.RWMutex
	currentLinks map[string]string    // policy name -> currently selected link name
	switchTimes  map[string]time.Time // policy name -> last switch time
	hysteresis   time.Duration
	policies     []PolicyConfig
	logger       *zap.Logger
}

// NewPathSelector creates a new path selector with default hysteresis.
func NewPathSelector(logger *zap.Logger) *PathSelector {
	return &PathSelector{
		currentLinks: make(map[string]string),
		switchTimes:  make(map[string]time.Time),
		hysteresis:   defaultHysteresis,
		logger:       logger.Named("sdwan-pathselect"),
	}
}

// Select chooses the best WAN link for the given strategy and link quality data.
// It applies hysteresis to prevent rapid switching.
// bandwidths maps link name to bandwidth string (e.g., "100Mbps").
// costs maps link name to administrative cost.
func (ps *PathSelector) Select(
	policyName string,
	strategy string,
	links []LinkQuality,
	bandwidths map[string]string,
	costs map[string]int32,
) (string, error) {
	if len(links) == 0 {
		return "", errNoLinksAvailableForSelection
	}

	selected := selectPath(strategy, links, bandwidths, costs)
	if selected == "" {
		return "", fmt.Errorf("%w: %q", errNoHealthyLinkFoundForStrategy, strategy)
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	current, exists := ps.currentLinks[policyName]
	if !exists || current == "" {
		// First selection: no hysteresis check needed
		ps.currentLinks[policyName] = selected
		ps.switchTimes[policyName] = time.Now()
		ps.logger.Info("Initial path selected",
			zap.String("policy", policyName),
			zap.String("link", selected),
			zap.String("strategy", strategy),
		)
		return selected, nil
	}

	if selected != current && ps.shouldSwitch(policyName) {
		ps.currentLinks[policyName] = selected
		ps.switchTimes[policyName] = time.Now()
		ps.logger.Info("Path switched",
			zap.String("policy", policyName),
			zap.String("from", current),
			zap.String("to", selected),
			zap.String("strategy", strategy),
		)
		return selected, nil
	}

	// Return current link (no switch due to hysteresis or same link selected)
	return ps.currentLinks[policyName], nil
}

// SetCurrentLink sets the currently active link for a policy.
// This is useful for bootstrapping or manual overrides.
func (ps *PathSelector) SetCurrentLink(policyName, linkName string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.currentLinks[policyName] = linkName
	ps.switchTimes[policyName] = time.Now()
}

// shouldSwitch returns true if enough time has passed since the last switch
// for the given policy (hysteresis check).
// Caller must hold ps.mu.
func (ps *PathSelector) shouldSwitch(policyName string) bool {
	lastSwitch, exists := ps.switchTimes[policyName]
	if !exists {
		return true
	}
	return time.Since(lastSwitch) >= ps.hysteresis
}

// selectPath is a pure function that selects the best link based on strategy.
// It filters out unhealthy links, then sorts according to the strategy criteria.
func selectPath(
	strategy string,
	links []LinkQuality,
	bandwidths map[string]string,
	costs map[string]int32,
) string {
	// Filter to only healthy links
	healthy := make([]LinkQuality, 0, len(links))
	for _, l := range links {
		if l.Healthy {
			healthy = append(healthy, l)
		}
	}
	if len(healthy) == 0 {
		return ""
	}

	switch strategy {
	case StrategyLowestLatency:
		sort.Slice(healthy, func(i, j int) bool {
			return healthy[i].LatencyMs < healthy[j].LatencyMs
		})
		return healthy[0].LinkName

	case StrategyHighestBandwidth:
		sort.Slice(healthy, func(i, j int) bool {
			bwI := parseBandwidthMbps(bandwidths[healthy[i].LinkName])
			bwJ := parseBandwidthMbps(bandwidths[healthy[j].LinkName])
			return bwI > bwJ // descending
		})
		return healthy[0].LinkName

	case StrategyMostReliable:
		sort.Slice(healthy, func(i, j int) bool {
			return healthy[i].PacketLoss < healthy[j].PacketLoss
		})
		return healthy[0].LinkName

	case StrategyLowestCost:
		sort.Slice(healthy, func(i, j int) bool {
			return costs[healthy[i].LinkName] < costs[healthy[j].LinkName]
		})
		return healthy[0].LinkName

	default:
		// Fall back to lowest latency
		sort.Slice(healthy, func(i, j int) bool {
			return healthy[i].LatencyMs < healthy[j].LatencyMs
		})
		return healthy[0].LinkName
	}
}

// parseBandwidthMbps converts a bandwidth string like "100Mbps", "1Gbps", "10mbps"
// to a numeric value in Mbps. Returns 0 for unparseable values.
func parseBandwidthMbps(bw string) float64 {
	if bw == "" {
		return 0
	}
	bw = strings.TrimSpace(bw)
	lower := strings.ToLower(bw)

	var multiplier float64
	var numStr string

	switch {
	case strings.HasSuffix(lower, "gbps"):
		multiplier = 1000
		numStr = bw[:len(bw)-4]
	case strings.HasSuffix(lower, "mbps"):
		multiplier = 1
		numStr = bw[:len(bw)-4]
	case strings.HasSuffix(lower, "kbps"):
		multiplier = 0.001
		numStr = bw[:len(bw)-4]
	default:
		// Try parsing as raw Mbps number
		numStr = bw
		multiplier = 1
	}

	val, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
	if err != nil {
		return 0
	}
	return val * multiplier
}

// ApplyPolicies stores the current set of WAN policies for use in path selection.
func (ps *PathSelector) ApplyPolicies(policies []PolicyConfig) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.policies = policies
}
