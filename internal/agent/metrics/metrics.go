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
	"hash/fnv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// AggregationMode controls how endpoint-level metrics are aggregated
type AggregationMode int

const (
	// AggregateByEndpoint records metrics per-endpoint (high cardinality)
	AggregateByEndpoint AggregationMode = iota
	// AggregateByCluster aggregates all endpoint metrics under the cluster label (low cardinality)
	AggregateByCluster
)

// MetricsConfig holds configuration for metrics collection
type MetricsConfig struct {
	// EnableSampling enables sampling for high-frequency metrics
	EnableSampling bool
	// SampleRate is the percentage of metrics to sample (0-100)
	SampleRate int
	// MaxEndpointCardinality limits the number of tracked endpoints per cluster
	MaxEndpointCardinality int
	// Aggregation controls how endpoint-level labels are recorded
	Aggregation AggregationMode
}

var (
	// Default configuration
	defaultConfig = MetricsConfig{
		EnableSampling:         false,
		SampleRate:             10,
		MaxEndpointCardinality: 100,
	}

	// Endpoint tracking to limit cardinality
	endpointTracker = &endpointCardinalityTracker{
		endpoints: make(map[string]map[string]bool),
		mu:        sync.RWMutex{},
	}
)

// endpointCardinalityTracker tracks endpoints per cluster to limit metric cardinality
type endpointCardinalityTracker struct {
	endpoints map[string]map[string]bool // cluster -> endpoint -> bool
	mu        sync.RWMutex
}

// shouldTrackEndpoint determines if we should track metrics for this endpoint
func (t *endpointCardinalityTracker) shouldTrackEndpoint(cluster, endpoint string) bool {
	t.mu.RLock()
	clusterEndpoints, exists := t.endpoints[cluster]
	if exists {
		_, tracked := clusterEndpoints[endpoint]
		t.mu.RUnlock()
		return tracked
	}
	t.mu.RUnlock()

	// Need to check if we should add this endpoint
	t.mu.Lock()
	defer t.mu.Unlock()

	// Double check after acquiring write lock
	if t.endpoints[cluster] == nil {
		t.endpoints[cluster] = make(map[string]bool)
	}

	// Check cardinality limit
	if len(t.endpoints[cluster]) >= defaultConfig.MaxEndpointCardinality {
		// Already at limit, don't track this endpoint
		return false
	}

	// Add endpoint to tracking
	t.endpoints[cluster][endpoint] = true
	return true
}

// cleanupCluster removes all endpoints for a cluster
func (t *endpointCardinalityTracker) cleanupCluster(cluster string) {
	t.mu.Lock()
	endpoints := t.endpoints[cluster]
	delete(t.endpoints, cluster)
	t.mu.Unlock()

	// Delete stale Prometheus metric series for all endpoints in this cluster
	for endpoint := range endpoints {
		deleteEndpointMetrics(cluster, endpoint)
	}
}

// removeEndpoint removes a single endpoint from tracking and cleans up its metrics
func (t *endpointCardinalityTracker) removeEndpoint(cluster, endpoint string) {
	t.mu.Lock()
	if clusterEndpoints, ok := t.endpoints[cluster]; ok {
		delete(clusterEndpoints, endpoint)
	}
	t.mu.Unlock()

	deleteEndpointMetrics(cluster, endpoint)
}

// deleteEndpointMetrics removes Prometheus metric series for a specific cluster/endpoint pair
func deleteEndpointMetrics(cluster, endpoint string) {
	BackendRequestsTotal.DeleteLabelValues(cluster, endpoint, "success")
	BackendRequestsTotal.DeleteLabelValues(cluster, endpoint, "failure")
	BackendResponseDuration.DeleteLabelValues(cluster, endpoint)
	BackendHealthStatus.DeleteLabelValues(cluster, endpoint)
	BackendActiveConnections.DeleteLabelValues(cluster, endpoint)
	HealthChecksTotal.DeleteLabelValues(cluster, endpoint, "success")
	HealthChecksTotal.DeleteLabelValues(cluster, endpoint, "failure")
	HealthCheckDuration.DeleteLabelValues(cluster, endpoint)
	CircuitBreakerState.DeleteLabelValues(cluster, endpoint)
}

// resolveEndpointLabel resolves the endpoint label based on aggregation mode and cardinality limits.
// When AggregateByCluster mode is set, all endpoints use the "aggregated" label.
// Otherwise, cardinality limits are checked and "other" is used when the limit is exceeded.
func resolveEndpointLabel(cluster, endpoint string) string {
	if defaultConfig.Aggregation == AggregateByCluster {
		return "aggregated"
	}
	if !endpointTracker.shouldTrackEndpoint(cluster, endpoint) {
		return "other"
	}
	return endpoint
}

// shouldSample determines if we should record this metric based on sampling rate
func shouldSample(key string) bool {
	if !defaultConfig.EnableSampling {
		return true
	}

	// Use hash-based sampling for consistent decisions
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte(time.Now().Format("2006-01-02-15-04"))) // Include minute for time-based sampling
	hash := h.Sum32()

	return int(hash%100) < defaultConfig.SampleRate
}

var (
	// HTTP Request Metrics

	// HTTPRequestsTotal tracks total HTTP requests
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "status", "cluster"},
	)

	// HTTPRequestDuration tracks HTTP request duration
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "novaedge_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets, // 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
		},
		[]string{"method", "cluster"},
	)

	// HTTPRequestsInFlight tracks active HTTP requests
	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "novaedge_http_requests_in_flight",
			Help: "Number of HTTP requests currently being processed",
		},
	)
)

// RecordHTTPRequest records an HTTP request
func RecordHTTPRequest(method, status, cluster string, duration float64) {
	HTTPRequestsTotal.WithLabelValues(method, status, cluster).Inc()
	HTTPRequestDuration.WithLabelValues(method, cluster).Observe(duration)
}

// ConfigureMetrics updates the metrics configuration
func ConfigureMetrics(config MetricsConfig) {
	defaultConfig = config
}
