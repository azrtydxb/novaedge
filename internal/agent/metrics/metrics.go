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
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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

// Config holds configuration for metrics collection.
type Config struct {
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
	defaultConfig = Config{
		EnableSampling:         false,
		SampleRate:             10,
		MaxEndpointCardinality: 100,
	}

	// Endpoint tracking to limit cardinality
	endpointTracker = &endpointCardinalityTracker{
		endpoints: make(map[string]map[string]*trackedEndpoint),
		mu:        sync.RWMutex{},
	}

	// Cached time bucket for sampling to avoid per-call time.Format allocations
	cachedTimeBucket   string
	cachedTimeBucketMu sync.Mutex
	lastBucketUpdate   time.Time
)

// trackedEndpoint holds metadata for a tracked endpoint.
// lastSeen is stored as UnixNano via atomic.Int64 to allow lock-free updates
// on the hot path (shouldTrackEndpoint is called on every request).
type trackedEndpoint struct {
	lastSeen atomic.Int64 // UnixNano timestamp
}

// endpointCardinalityTracker tracks endpoints per cluster to limit metric cardinality
type endpointCardinalityTracker struct {
	endpoints map[string]map[string]*trackedEndpoint // cluster -> endpoint -> metadata
	mu        sync.RWMutex
}

// shouldTrackEndpoint determines if we should track metrics for this endpoint.
// The hot path (already-tracked endpoint) uses only an RLock + atomic store,
// avoiding the write lock that previously caused contention under load.
func (t *endpointCardinalityTracker) shouldTrackEndpoint(cluster, endpoint string) bool {
	nowNano := time.Now().UnixNano()

	t.mu.RLock()
	clusterEndpoints, exists := t.endpoints[cluster]
	if exists {
		ep, tracked := clusterEndpoints[endpoint]
		t.mu.RUnlock()
		if tracked {
			// Atomic update — no write lock needed.
			// Only update when delta > 1s to reduce cache-line bouncing.
			prev := ep.lastSeen.Load()
			if nowNano-prev > int64(time.Second) {
				ep.lastSeen.Store(nowNano)
			}
			return true
		}
	} else {
		t.mu.RUnlock()
	}

	// Need to check if we should add this endpoint
	t.mu.Lock()
	defer t.mu.Unlock()

	// Double check after acquiring write lock
	if t.endpoints[cluster] == nil {
		t.endpoints[cluster] = make(map[string]*trackedEndpoint)
	}

	// Check if already added by another goroutine
	if ep, ok := t.endpoints[cluster][endpoint]; ok {
		ep.lastSeen.Store(nowNano)
		return true
	}

	// Check cardinality limit
	if len(t.endpoints[cluster]) >= defaultConfig.MaxEndpointCardinality {
		// Already at limit, don't track this endpoint
		return false
	}

	// Add endpoint to tracking
	ep := &trackedEndpoint{}
	ep.lastSeen.Store(nowNano)
	t.endpoints[cluster][endpoint] = ep
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

// cleanupStaleEndpoints removes endpoints not seen within the given TTL
// and deletes their Prometheus metric series.
func (t *endpointCardinalityTracker) cleanupStaleEndpoints(ttl time.Duration) {
	cutoffNano := time.Now().Add(-ttl).UnixNano()

	t.mu.Lock()
	stale := make(map[string][]string) // cluster -> []endpoint
	for cluster, endpoints := range t.endpoints {
		for endpoint, ep := range endpoints {
			if ep.lastSeen.Load() < cutoffNano {
				stale[cluster] = append(stale[cluster], endpoint)
			}
		}
		for _, endpoint := range stale[cluster] {
			delete(endpoints, endpoint)
		}
		if len(endpoints) == 0 {
			delete(t.endpoints, cluster)
		}
	}
	t.mu.Unlock()

	// Delete Prometheus metric series outside the lock
	for cluster, endpoints := range stale {
		for _, endpoint := range endpoints {
			deleteEndpointMetrics(cluster, endpoint)
		}
	}
}

// StartCleanupLoop starts a background goroutine that periodically removes
// stale endpoints not seen within the given TTL. It stops when ctx is cancelled.
func (t *endpointCardinalityTracker) StartCleanupLoop(ctx context.Context, interval, ttl time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.cleanupStaleEndpoints(ttl)
			}
		}
	}()
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

// getTimeBucket returns a cached time bucket string, only reformatting
// when the minute changes. This avoids per-call time.Format allocations.
func getTimeBucket() string {
	now := time.Now()
	cachedTimeBucketMu.Lock()
	defer cachedTimeBucketMu.Unlock()
	if now.Sub(lastBucketUpdate) >= time.Minute {
		cachedTimeBucket = now.Format("2006-01-02-15-04")
		lastBucketUpdate = now
	}
	return cachedTimeBucket
}

// fnv32a computes an FNV-1a hash inline without allocating a hash.Hash.
func fnv32a(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// shouldSample determines if we should record this metric based on sampling rate
func shouldSample(key string) bool {
	if !defaultConfig.EnableSampling {
		return true
	}

	// Use hash-based sampling for consistent decisions.
	// Inline FNV avoids allocating fnv.New32a() per call.
	hash := fnv32a(key + getTimeBucket())

	return int(hash%100) < defaultConfig.SampleRate
}

var (
	// HTTP Request Metrics

	// HTTPRequestsTotal tracks total HTTP requests
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests",
		},
		[]string{"method", "status_class", "cluster"},
	)

	// HTTPRequestDuration tracks HTTP request duration
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "novaedge",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds",
			Buckets:   prometheus.DefBuckets, // 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
		},
		[]string{"method", "cluster"},
	)

	// HTTPRequestsInFlight tracks active HTTP requests
	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "Number of HTTP requests currently being processed",
		},
	)
)

// StatusClass converts an HTTP status code to a bounded class label.
// The returned values are "1xx", "2xx", "3xx", "4xx", or "5xx".
// Unknown or out-of-range codes return "unknown".
func StatusClass(code int) string {
	switch {
	case code >= 100 && code < 200:
		return "1xx"
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "unknown"
	}
}

// RecordHTTPRequest records an HTTP request
func RecordHTTPRequest(method, statusClass, cluster string, duration float64) {
	HTTPRequestsTotal.WithLabelValues(method, statusClass, cluster).Inc()
	HTTPRequestDuration.WithLabelValues(method, cluster).Observe(duration)
}

// ConfigureMetrics updates the metrics configuration
func ConfigureMetrics(config Config) {
	defaultConfig = config
}

// InitOTelExporter creates and starts an OTelExporter if the supplied
// configuration has Enabled set to true. It returns nil without error when
// OTel export is disabled. This function is intended to be called at agent
// startup alongside the existing Prometheus metrics setup so that both
// exporters can run simultaneously.
func InitOTelExporter(config OTelConfig) (*OTelExporter, error) {
	if !config.Enabled {
		return nil, nil
	}

	exporter, err := NewOTelExporter(config)
	if err != nil {
		return nil, fmt.Errorf("creating OTel exporter: %w", err)
	}

	ctx := context.Background()
	if err := exporter.Start(ctx); err != nil {
		return nil, fmt.Errorf("starting OTel exporter: %w", err)
	}

	return exporter, nil
}
