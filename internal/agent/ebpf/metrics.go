//go:build linux

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

package ebpf

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// eBPFProgramsLoaded tracks the number of successfully loaded BPF
	// programs, labelled by subsystem (e.g. "mesh", "xdp", "afxdp").
	eBPFProgramsLoaded = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "novaedge_ebpf_programs_loaded",
			Help: "Number of currently loaded eBPF programs",
		},
		[]string{"subsystem"},
	)

	// eBPFMapOpsTotal counts BPF map operations, labelled by map name,
	// operation type (update, delete, update_lpm, delete_lpm), and result
	// (ok, error).
	eBPFMapOpsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_ebpf_map_operations_total",
			Help: "Total number of eBPF map operations",
		},
		[]string{"map", "operation", "result"},
	)

	// eBPFErrorsTotal counts errors encountered during eBPF operations,
	// labelled by subsystem and error type (load, attach, map, detach).
	eBPFErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_ebpf_errors_total",
			Help: "Total number of eBPF-related errors",
		},
		[]string{"subsystem", "error_type"},
	)

	// eBPFAttachDuration measures the time to attach BPF programs to
	// hooks (XDP, sk_lookup, etc.), labelled by subsystem.
	eBPFAttachDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "novaedge_ebpf_attach_duration_seconds",
			Help:    "Time taken to attach eBPF programs to kernel hooks",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"subsystem"},
	)
)

// RecordError increments the error counter for the given subsystem and
// error type.
func RecordError(subsystem, errorType string) {
	eBPFErrorsTotal.WithLabelValues(subsystem, errorType).Inc()
}

// RecordProgramLoaded increments the loaded program gauge for a subsystem.
func RecordProgramLoaded(subsystem string) {
	eBPFProgramsLoaded.WithLabelValues(subsystem).Inc()
}

// RecordProgramUnloaded decrements the loaded program gauge for a subsystem.
func RecordProgramUnloaded(subsystem string) {
	eBPFProgramsLoaded.WithLabelValues(subsystem).Dec()
}

// RecordMapOp increments the map operation counter.
func RecordMapOp(mapName, operation, result string) {
	eBPFMapOpsTotal.WithLabelValues(mapName, operation, result).Inc()
}

// ObserveAttachDuration records a program attach duration for a subsystem.
func ObserveAttachDuration(subsystem string, seconds float64) {
	eBPFAttachDuration.WithLabelValues(subsystem).Observe(seconds)
}
