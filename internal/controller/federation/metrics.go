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

package federation

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// FederationPeersTotal tracks the number of federation peers
	FederationPeersTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "peers_total",
		Help:      "Total number of federation peers",
	})

	// FederationPeersHealthy tracks healthy federation peers
	FederationPeersHealthy = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "peers_healthy",
		Help:      "Number of healthy federation peers",
	})

	// FederationPeersConnected tracks connected federation peers
	FederationPeersConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "peers_connected",
		Help:      "Number of connected federation peers",
	})

	// FederationPhaseGauge tracks the current federation phase.
	FederationPhaseGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "phase",
		Help:      "Current federation phase (1 = active)",
	}, []string{"phase"})

	// FederationSyncTotal tracks sync operations
	FederationSyncTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "sync_total",
		Help:      "Total number of sync operations",
	}, []string{"type", "direction"})

	// FederationSyncDuration tracks sync duration
	FederationSyncDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "sync_duration_seconds",
		Help:      "Duration of sync operations",
		Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"peer"})

	// FederationChangesReceived tracks changes received from peers
	FederationChangesReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "changes_received_total",
		Help:      "Total number of changes received from peers",
	}, []string{"peer", "resource_type"})

	// FederationChangesSent tracks changes sent to peers
	FederationChangesSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "changes_sent_total",
		Help:      "Total number of changes sent to peers",
	}, []string{"peer", "resource_type"})

	// FederationConflictsTotal tracks detected conflicts
	FederationConflictsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "conflicts_total",
		Help:      "Total number of conflicts detected",
	}, []string{"resolution"})

	// FederationConflictsPending tracks pending conflicts
	FederationConflictsPending = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "conflicts_pending",
		Help:      "Number of conflicts pending manual resolution",
	})

	// FederationVectorClockValue tracks vector clock values
	FederationVectorClockValue = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "vector_clock_value",
		Help:      "Current vector clock value for each member",
	}, []string{"member"})

	// FederationPeerLatency tracks peer latency
	FederationPeerLatency = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "peer_latency_seconds",
		Help:      "Latency to each peer in seconds",
	}, []string{"peer"})

	// FederationPeerSyncLag tracks sync lag with each peer
	FederationPeerSyncLag = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "peer_sync_lag_seconds",
		Help:      "Sync lag with each peer in seconds",
	}, []string{"peer"})

	// FederationResourcesTotal tracks total synced resources
	FederationResourcesTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "resources_total",
		Help:      "Total number of synced resources by type",
	}, []string{"type"})

	// FederationTombstonesTotal tracks tombstones
	FederationTombstonesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "tombstones_total",
		Help:      "Total number of tombstones (deletion records)",
	})

	// FederationPendingChanges tracks pending changes
	FederationPendingChanges = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "pending_changes",
		Help:      "Number of changes pending propagation",
	})

	// FederationHeartbeatsTotal tracks heartbeats
	FederationHeartbeatsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "heartbeats_total",
		Help:      "Total number of heartbeats sent/received",
	}, []string{"direction", "peer"})

	// FederationErrors tracks federation errors
	FederationErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "novaedge",
		Subsystem: "federation",
		Name:      "errors_total",
		Help:      "Total number of federation errors",
	}, []string{"type", "peer"})
)

// MetricsCollector collects federation metrics
type MetricsCollector struct {
	manager *Manager
}

// NewMetricsCollector creates a metrics collector for a federation manager
func NewMetricsCollector(manager *Manager) *MetricsCollector {
	return &MetricsCollector{manager: manager}
}

// Collect collects current metrics
func (c *MetricsCollector) Collect() {
	if c.manager == nil || c.manager.server == nil {
		return
	}

	// Peer counts
	peerStates := c.manager.GetPeerStates()
	var healthyCount, connectedCount float64
	for peer, state := range peerStates {
		if state.Healthy {
			healthyCount++
		}
		if state.Connected {
			connectedCount++
		}

		// Peer latency
		if state.SyncLag > 0 {
			FederationPeerSyncLag.WithLabelValues(peer).Set(state.SyncLag.Seconds())
		}
	}

	FederationPeersTotal.Set(float64(len(peerStates)))
	FederationPeersHealthy.Set(healthyCount)
	FederationPeersConnected.Set(connectedCount)

	// Phase
	phase := c.manager.GetPhase()
	phases := []string{string(PhaseInitializing), string(PhaseSyncing), string(PhaseHealthy), string(PhaseDegraded), string(PhasePartitioned)}
	for _, p := range phases {
		if p == string(phase) {
			FederationPhaseGauge.WithLabelValues(p).Set(1)
		} else {
			FederationPhaseGauge.WithLabelValues(p).Set(0)
		}
	}

	// Vector clock
	vc := c.manager.GetVectorClock()
	for member, value := range vc {
		FederationVectorClockValue.WithLabelValues(member).Set(float64(value))
	}

	// Stats
	stats := c.manager.GetStats()
	FederationPendingChanges.Set(float64(stats.PendingChanges))

	// Conflicts
	conflicts := c.manager.GetConflicts()
	FederationConflictsPending.Set(float64(len(conflicts)))
}

// UpdatePeerMetrics updates metrics for a specific peer
func UpdatePeerMetrics(peerName string, state *PeerState) {
	if state.SyncLag > 0 {
		FederationPeerSyncLag.WithLabelValues(peerName).Set(state.SyncLag.Seconds())
	}
}

// RecordChange records a change metric
func RecordChange(direction, peer, resourceType string) {
	if direction == "received" {
		FederationChangesReceived.WithLabelValues(peer, resourceType).Inc()
	} else {
		FederationChangesSent.WithLabelValues(peer, resourceType).Inc()
	}
}

// RecordConflict records a conflict metric
func RecordConflict(resolution string) {
	FederationConflictsTotal.WithLabelValues(resolution).Inc()
}

// RecordHeartbeat records a heartbeat metric
func RecordHeartbeat(direction, peer string) {
	FederationHeartbeatsTotal.WithLabelValues(direction, peer).Inc()
}

// RecordError records an error metric
func RecordError(errorType, peer string) {
	FederationErrors.WithLabelValues(errorType, peer).Inc()
}

// RecordSyncDuration records sync duration
func RecordSyncDuration(peer string, seconds float64) {
	FederationSyncDuration.WithLabelValues(peer).Observe(seconds)
}
