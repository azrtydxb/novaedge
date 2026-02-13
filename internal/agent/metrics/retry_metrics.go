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
	// Retry Metrics

	// RetryCount tracks total retry attempts
	RetryCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_retry_count_total",
			Help: "Total number of retry attempts",
		},
	)

	// RetrySuccess tracks successful retries (request succeeded after at least one retry)
	RetrySuccess = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_retry_success_total",
			Help: "Total number of requests that succeeded after retry",
		},
	)

	// RetryExhausted tracks cases where all retries were exhausted
	RetryExhausted = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_retry_exhausted_total",
			Help: "Total number of requests where all retries were exhausted",
		},
	)

	// RetryBudgetExhausted tracks retries rejected because the retry budget was exceeded
	RetryBudgetExhausted = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_retry_budget_exhausted_total",
			Help: "Total number of retries rejected because the per-cluster retry budget was exceeded",
		},
	)
)
