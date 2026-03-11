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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// SD-WAN Link Quality Metrics

	// SDWANLinkLatency tracks the current smoothed latency of each WAN link in milliseconds.
	SDWANLinkLatency = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "sdwan",
			Name:      "link_latency_ms",
			Help:      "Current smoothed latency of a WAN link in milliseconds",
		},
		[]string{"link", "remote_site"},
	)

	// SDWANLinkJitter tracks the current jitter (latency standard deviation) of each WAN link.
	SDWANLinkJitter = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "sdwan",
			Name:      "link_jitter_ms",
			Help:      "Current jitter of a WAN link in milliseconds",
		},
		[]string{"link", "remote_site"},
	)

	// SDWANLinkPacketLoss tracks the current packet loss ratio of each WAN link (0.0-1.0).
	SDWANLinkPacketLoss = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "sdwan",
			Name:      "link_packet_loss",
			Help:      "Current packet loss ratio of a WAN link (0.0-1.0)",
		},
		[]string{"link", "remote_site"},
	)

	// SDWANLinkScore tracks the composite quality score of each WAN link (0.0-1.0).
	SDWANLinkScore = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "sdwan",
			Name:      "link_score",
			Help:      "Composite quality score of a WAN link (0.0-1.0, higher is better)",
		},
		[]string{"link", "remote_site"},
	)

	// SDWANLinkHealthy tracks the health status of each WAN link (1=healthy, 0=unhealthy).
	SDWANLinkHealthy = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "sdwan",
			Name:      "link_healthy",
			Help:      "Health status of a WAN link (1=healthy, 0=unhealthy)",
		},
		[]string{"link", "remote_site"},
	)

	// SD-WAN Path Selection Metrics

	// SDWANPathSelections tracks the total number of path selection decisions.
	SDWANPathSelections = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "sdwan",
			Name:      "path_selections_total",
			Help:      "Total number of SD-WAN path selection decisions",
		},
		[]string{"link", "strategy"},
	)

	// SDWANFailovers tracks the total number of failover events.
	SDWANFailovers = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "sdwan",
			Name:      "failovers_total",
			Help:      "Total number of SD-WAN link failover events",
		},
		[]string{"from_link", "to_link"},
	)
)
