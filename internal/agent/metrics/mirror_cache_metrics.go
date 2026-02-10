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
	// Mirror Metrics

	// MirrorRequestsTotal tracks total mirrored requests
	MirrorRequestsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_mirror_requests_total",
			Help: "Total number of mirrored requests sent",
		},
	)

	// MirrorErrorsTotal tracks total mirror request errors
	MirrorErrorsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_mirror_errors_total",
			Help: "Total number of mirror request errors",
		},
	)

	// MirrorLatency tracks mirror request latency
	MirrorLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "novaedge_mirror_latency_seconds",
			Help:    "Latency of mirror requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)

	// Cache Metrics

	// CacheHitTotal tracks cache hits
	CacheHitTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_cache_hit_total",
			Help: "Total number of cache hits",
		},
	)

	// CacheMissTotal tracks cache misses
	CacheMissTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_cache_miss_total",
			Help: "Total number of cache misses",
		},
	)

	// CacheEvictionTotal tracks cache evictions
	CacheEvictionTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_cache_eviction_total",
			Help: "Total number of cache evictions",
		},
	)

	// CacheSizeBytes tracks current cache memory usage
	CacheSizeBytes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "novaedge_cache_size_bytes",
			Help: "Current cache memory usage in bytes",
		},
	)
)
