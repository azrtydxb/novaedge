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

package snapshot

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// SnapshotBuildDuration tracks the time taken to build snapshots
	SnapshotBuildDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "novaedge_snapshot_build_duration_seconds",
			Help:    "Time taken to build config snapshots",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"node"},
	)

	// SnapshotSize tracks the size of generated snapshots
	SnapshotSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "novaedge_snapshot_size_bytes",
			Help:    "Size of generated config snapshots in bytes",
			Buckets: []float64{1024, 4096, 16384, 65536, 262144, 1048576},
		},
		[]string{"node"},
	)

	// SnapshotResourceCount tracks the number of resources in snapshots
	SnapshotResourceCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_snapshot_resources",
			Help: "Number of resources in config snapshots",
		},
		[]string{"node", "type"},
	)

	// ActiveConfigStreams tracks the number of active config streams
	ActiveConfigStreams = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "novaedge_active_config_streams",
			Help: "Number of active config distribution streams to agents",
		},
	)

	// SnapshotUpdatesSent tracks the number of snapshot updates sent
	SnapshotUpdatesSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_snapshot_updates_total",
			Help: "Total number of config snapshot updates sent to agents",
		},
		[]string{"node", "trigger"},
	)

	// SnapshotBuildErrors tracks snapshot build errors
	SnapshotBuildErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_snapshot_build_errors_total",
			Help: "Total number of errors building config snapshots",
		},
		[]string{"node", "error_type"},
	)

	// AgentStatus tracks the health status reported by agents
	AgentStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_agent_status",
			Help: "Health status of NovaEdge agents (1=healthy, 0=unhealthy)",
		},
		[]string{"node", "version"},
	)

	// CachedSnapshots tracks the number of cached snapshots
	CachedSnapshots = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "novaedge_cached_snapshots",
			Help: "Number of config snapshots currently cached",
		},
	)
)

func init() {
	// Register metrics with controller-runtime's registry
	metrics.Registry.MustRegister(
		SnapshotBuildDuration,
		SnapshotSize,
		SnapshotResourceCount,
		ActiveConfigStreams,
		SnapshotUpdatesSent,
		SnapshotBuildErrors,
		AgentStatus,
		CachedSnapshots,
	)
}

// RecordSnapshotBuild records metrics for a snapshot build
func RecordSnapshotBuild(nodeName string, durationSeconds float64, sizeBytes int, resourceCounts map[string]int) {
	SnapshotBuildDuration.WithLabelValues(nodeName).Observe(durationSeconds)
	SnapshotSize.WithLabelValues(nodeName).Observe(float64(sizeBytes))

	for resourceType, count := range resourceCounts {
		SnapshotResourceCount.WithLabelValues(nodeName, resourceType).Set(float64(count))
	}
}

// RecordSnapshotUpdate records a snapshot update being sent
func RecordSnapshotUpdate(nodeName, trigger string) {
	SnapshotUpdatesSent.WithLabelValues(nodeName, trigger).Inc()
}

// RecordSnapshotError records a snapshot build error
func RecordSnapshotError(nodeName, errorType string) {
	SnapshotBuildErrors.WithLabelValues(nodeName, errorType).Inc()
}

// UpdateAgentStatus updates the agent health status metric
func UpdateAgentStatus(nodeName, version string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	AgentStatus.WithLabelValues(nodeName, version).Set(value)
}

// UpdateActiveStreams updates the active streams metric
func UpdateActiveStreams(count int64) {
	ActiveConfigStreams.Set(float64(count))
}

// UpdateCachedSnapshots updates the cached snapshots metric
func UpdateCachedSnapshots(count int) {
	CachedSnapshots.Set(float64(count))
}
