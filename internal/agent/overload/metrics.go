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

package overload

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// overloadState indicates whether the system is overloaded (1) or normal (0).
	overloadState = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "overload",
		Name:      "state",
		Help:      "Current overload state: 0 = normal, 1 = overloaded.",
	})

	// shedTotal counts the total number of requests shed due to overload.
	shedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "novaedge",
		Subsystem: "overload",
		Name:      "shed_total",
		Help:      "Total number of requests shed due to overload.",
	})

	// heapUsageRatio tracks the current heap memory usage as a ratio of the limit.
	heapUsageRatio = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "overload",
		Name:      "heap_usage_ratio",
		Help:      "Current heap memory usage as a ratio of the configured limit.",
	})

	// goroutineCount tracks the current number of goroutines.
	goroutineCount = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "overload",
		Name:      "goroutine_count",
		Help:      "Current number of goroutines.",
	})

	// activeConnections tracks the current number of active connections.
	activeConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "novaedge",
		Subsystem: "overload",
		Name:      "active_connections",
		Help:      "Current number of active connections.",
	})
)

// updateMetrics updates all Prometheus metrics from the given state.
func updateMetrics(state State) {
	if state.IsOverloaded {
		overloadState.Set(1)
	} else {
		overloadState.Set(0)
	}
	heapUsageRatio.Set(state.HeapUsageRatio)
	goroutineCount.Set(float64(state.GoroutineCount))
	activeConnections.Set(float64(state.ActiveConnections))
}
