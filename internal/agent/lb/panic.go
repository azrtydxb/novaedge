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

package lb

import (
	"math/rand/v2"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// PanicConfig holds configuration for panic mode thresholds.
type PanicConfig struct {
	// Threshold is the minimum healthy endpoint fraction (0.0–1.0).
	// When the healthy percentage drops below this value, panic mode activates
	// and the load balancer selects from ALL endpoints (healthy + unhealthy).
	// Default is 0.5 (panic when <50% of endpoints are healthy).
	// Setting Threshold to 0 effectively disables panic mode.
	Threshold float64

	// Enabled controls whether panic mode is active. Default is true.
	Enabled bool
}

// DefaultPanicConfig returns a PanicConfig with sensible defaults.
func DefaultPanicConfig() PanicConfig {
	return PanicConfig{
		Threshold: 0.5,
		Enabled:   true,
	}
}

// PanicMetrics holds Prometheus metrics for panic mode tracking.
type PanicMetrics struct {
	// PanicMode is a gauge that is 1 when panic mode is active, 0 otherwise.
	PanicMode *prometheus.GaugeVec
}

// NewPanicMetrics creates a new PanicMetrics instance with registered Prometheus metrics.
func NewPanicMetrics() *PanicMetrics {
	return &PanicMetrics{
		PanicMode: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "novaedge_lb_panic_mode",
				Help: "Whether the load balancer is in panic mode (1=panic, 0=normal)",
			},
			[]string{"cluster"},
		),
	}
}

// PanicHandler wraps an inner LoadBalancer with panic mode support.
// When the fraction of healthy endpoints drops below the configured threshold,
// the handler enters panic mode and selects from all endpoints (healthy and unhealthy)
// to avoid dropping traffic entirely.
type PanicHandler struct {
	mu        sync.RWMutex
	inner     LoadBalancer
	config    PanicConfig
	cluster   string
	endpoints []*pb.Endpoint
	panicking bool
	logger    *zap.Logger
	metrics   *PanicMetrics
}

// NewPanicHandler creates a new PanicHandler wrapping the given LoadBalancer.
// The cluster parameter is used as a label for metrics.
// If logger is nil, a no-op logger is used.
// If metrics is nil, no metrics are recorded.
func NewPanicHandler(inner LoadBalancer, config PanicConfig, cluster string, endpoints []*pb.Endpoint, logger *zap.Logger, metrics *PanicMetrics) *PanicHandler {
	if logger == nil {
		logger = zap.NewNop()
	}

	ph := &PanicHandler{
		inner:     inner,
		config:    config,
		cluster:   cluster,
		endpoints: endpoints,
		logger:    logger,
		metrics:   metrics,
	}

	// Evaluate initial panic state
	ph.evaluatePanicState()

	return ph
}

// Select chooses an endpoint. In normal mode, it delegates to the inner LB
// (which only considers healthy endpoints). In panic mode, it selects from
// all endpoints regardless of health status.
func (ph *PanicHandler) Select() *pb.Endpoint {
	ph.mu.RLock()
	panicking := ph.panicking
	allEndpoints := ph.endpoints
	ph.mu.RUnlock()

	if panicking && len(allEndpoints) > 0 {
		// In panic mode: select from all endpoints (healthy + unhealthy)
		// using simple random selection to avoid depending on inner LB state.
		return allEndpoints[rand.IntN(len(allEndpoints))] //nolint:gosec // G404: math/rand is acceptable for load balancer selection
	}

	// Normal mode: delegate to inner LB (which filters for healthy endpoints)
	return ph.inner.Select()
}

// UpdateEndpoints updates endpoints on both the panic handler and the inner LB.
// It re-evaluates panic state based on the new endpoint set.
func (ph *PanicHandler) UpdateEndpoints(endpoints []*pb.Endpoint) {
	ph.mu.Lock()
	ph.endpoints = endpoints
	ph.mu.Unlock()

	// Update inner LB (this filters to healthy endpoints internally)
	ph.inner.UpdateEndpoints(endpoints)

	// Re-evaluate panic state with new endpoints
	ph.evaluatePanicState()
}

// IsPanicking returns true if the handler is currently in panic mode.
func (ph *PanicHandler) IsPanicking() bool {
	ph.mu.RLock()
	defer ph.mu.RUnlock()
	return ph.panicking
}

// GetInner returns the underlying load balancer.
func (ph *PanicHandler) GetInner() LoadBalancer {
	return ph.inner
}

// evaluatePanicState computes the healthy endpoint fraction and updates
// the panic state accordingly. It logs transitions and updates metrics.
func (ph *PanicHandler) evaluatePanicState() {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	if !ph.config.Enabled || ph.config.Threshold <= 0 {
		// Panic mode disabled; ensure we are not panicking
		if ph.panicking {
			ph.panicking = false
			ph.logger.Warn("panic mode disabled, exiting panic mode",
				zap.String("cluster", ph.cluster),
			)
			ph.setPanicMetric(0)
		}
		return
	}

	total := len(ph.endpoints)
	if total == 0 {
		// No endpoints at all; nothing to panic about
		if ph.panicking {
			ph.panicking = false
			ph.setPanicMetric(0)
		}
		return
	}

	healthy := 0
	for _, ep := range ph.endpoints {
		if ep.Ready {
			healthy++
		}
	}

	healthyFraction := float64(healthy) / float64(total)
	shouldPanic := healthyFraction < ph.config.Threshold

	if shouldPanic && !ph.panicking {
		ph.panicking = true
		ph.logger.Warn("entering panic mode: healthy endpoint fraction below threshold",
			zap.String("cluster", ph.cluster),
			zap.Int("healthy", healthy),
			zap.Int("total", total),
			zap.Float64("healthy_fraction", healthyFraction),
			zap.Float64("threshold", ph.config.Threshold),
		)
		ph.setPanicMetric(1)
	} else if !shouldPanic && ph.panicking {
		ph.panicking = false
		ph.logger.Warn("exiting panic mode: healthy endpoint fraction recovered above threshold",
			zap.String("cluster", ph.cluster),
			zap.Int("healthy", healthy),
			zap.Int("total", total),
			zap.Float64("healthy_fraction", healthyFraction),
			zap.Float64("threshold", ph.config.Threshold),
		)
		ph.setPanicMetric(0)
	}
}

// setPanicMetric updates the Prometheus gauge for panic mode.
// Must be called with ph.mu held.
func (ph *PanicHandler) setPanicMetric(value float64) {
	if ph.metrics != nil {
		ph.metrics.PanicMode.WithLabelValues(ph.cluster).Set(value)
	}
}
