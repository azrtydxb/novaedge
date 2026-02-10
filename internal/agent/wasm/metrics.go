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

package wasm

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	wasmPluginExecutionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "novaedge",
			Subsystem: "wasm",
			Name:      "plugin_execution_duration_seconds",
			Help:      "Duration of WASM plugin execution.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"plugin", "phase"},
	)

	wasmPluginExecutionTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "wasm",
			Name:      "plugin_execution_total",
			Help:      "Total number of WASM plugin executions.",
		},
		[]string{"plugin", "phase", "status"},
	)

	wasmPluginErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "wasm",
			Name:      "plugin_errors_total",
			Help:      "Total number of WASM plugin errors.",
		},
		[]string{"plugin", "phase"},
	)

	wasmPluginsLoaded = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "wasm",
			Name:      "plugins_loaded",
			Help:      "Number of currently loaded WASM plugins.",
		},
	)

	wasmInstancePoolSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "wasm",
			Name:      "instance_pool_size",
			Help:      "Current number of instances in the WASM instance pool.",
		},
		[]string{"plugin"},
	)
)

// RecordPluginExecution records a WASM plugin execution metric.
func RecordPluginExecution(plugin, phase string, duration time.Duration, err error) {
	status := "success"
	if err != nil {
		status = "error"
		wasmPluginErrorsTotal.WithLabelValues(plugin, phase).Inc()
	}
	wasmPluginExecutionTotal.WithLabelValues(plugin, phase, status).Inc()
	wasmPluginExecutionDuration.WithLabelValues(plugin, phase).Observe(duration.Seconds())
}

// RecordPluginError records a WASM plugin error.
func RecordPluginError(plugin, phase string) {
	wasmPluginErrorsTotal.WithLabelValues(plugin, phase).Inc()
}

// SetPluginsLoaded sets the gauge for loaded plugins.
func SetPluginsLoaded(count int) {
	wasmPluginsLoaded.Set(float64(count))
}

// SetInstancePoolSize sets the gauge for instance pool size.
func SetInstancePoolSize(plugin string, size int) {
	wasmInstancePoolSize.WithLabelValues(plugin).Set(float64(size))
}
