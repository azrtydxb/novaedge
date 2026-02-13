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
	// RateLimiterCleanupsTotal tracks the total number of inactive rate limiters
	// removed during cleanup cycles.
	RateLimiterCleanupsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_rate_limiter_cleanups_total",
			Help: "Total number of inactive rate limiters removed during cleanup",
		},
	)

	// RateLimiterActiveCount tracks the current number of active rate limiters.
	RateLimiterActiveCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "novaedge_rate_limiter_active_count",
			Help: "Current number of active rate limiters",
		},
	)
)
