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
	// Hedging Metrics

	// HedgingRequestsTotal tracks total hedged requests sent
	HedgingRequestsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_hedging_requests_total",
			Help: "Total number of hedged requests sent",
		},
	)

	// HedgingWins tracks how many times the hedged request won the race
	HedgingWins = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_hedging_wins_total",
			Help: "Total number of times the hedged request completed before the original",
		},
	)

	// HedgingCancelled tracks how many hedged requests were cancelled
	HedgingCancelled = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_hedging_cancelled_total",
			Help: "Total number of hedged requests cancelled because the original completed first",
		},
	)
)
