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
	// Distributed Rate Limit Metrics

	// DistributedRateLimitAllowed tracks requests allowed by distributed rate limiter
	DistributedRateLimitAllowed = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "distributed_rate_limit",
			Name:      "allowed_total",
			Help:      "Total number of requests allowed by distributed rate limiter",
		},
	)

	// DistributedRateLimitDenied tracks requests denied by distributed rate limiter
	DistributedRateLimitDenied = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "distributed_rate_limit",
			Name:      "denied_total",
			Help:      "Total number of requests denied by distributed rate limiter",
		},
	)

	// Redis Metrics

	// RedisLatency tracks Redis operation latency
	RedisLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "novaedge",
			Subsystem: "redis",
			Name:      "latency_seconds",
			Help:      "Redis operation latency in seconds",
			Buckets:   []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
	)

	// RedisErrors tracks Redis operation errors
	RedisErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "redis",
			Name:      "errors_total",
			Help:      "Total number of Redis operation errors",
		},
	)
)
