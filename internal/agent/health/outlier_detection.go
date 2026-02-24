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

package health

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
)

// OutlierDetectionConfig configures statistical outlier detection for upstream endpoints.
type OutlierDetectionConfig struct {
	// Interval between outlier detection analysis runs.
	Interval time.Duration
	// BaseEjectionTime is the base duration an endpoint is ejected. Actual ejection
	// time = BaseEjectionTime * ejectionCount (exponential backoff).
	BaseEjectionTime time.Duration
	// MaxEjectionPercent is the maximum percentage of endpoints that can be ejected
	// simultaneously (0-100).
	MaxEjectionPercent float64
	// SuccessRateMinHosts is the minimum number of endpoints required to perform
	// success-rate based outlier detection.
	SuccessRateMinHosts int
	// SuccessRateRequestVolume is the minimum number of requests an endpoint must
	// have in the current window to be included in success-rate analysis.
	SuccessRateRequestVolume int
	// SuccessRateStdevFactor controls how many standard deviations below the mean
	// success rate triggers ejection.
	SuccessRateStdevFactor float64
	// FailurePercentageThreshold is the failure percentage above which an endpoint
	// is ejected (0-100).
	FailurePercentageThreshold float64
	// ConsecutiveErrors is the number of consecutive failures that triggers ejection.
	ConsecutiveErrors int
}

// DefaultOutlierDetectionConfig returns a configuration with sensible defaults.
func DefaultOutlierDetectionConfig() OutlierDetectionConfig {
	return OutlierDetectionConfig{
		Interval:                   10 * time.Second,
		BaseEjectionTime:           30 * time.Second,
		MaxEjectionPercent:         50,
		SuccessRateMinHosts:        5,
		SuccessRateRequestVolume:   100,
		SuccessRateStdevFactor:     1.9,
		FailurePercentageThreshold: 85,
		ConsecutiveErrors:          5,
	}
}

// endpointStats tracks request outcomes for a single endpoint in the current window.
// The requests and successes fields use atomic operations for lock-free recording
// on the hot path. The consecutiveErrors field is only modified under the analysis
// lock (mu) during periodic analysis, or atomically via RecordSuccess/RecordFailure.
type endpointStats struct {
	requests          atomic.Int64
	successes         atomic.Int64
	consecutiveErrors atomic.Int32
}

// ejectionInfo tracks ejection state for an endpoint.
type ejectionInfo struct {
	ejected       bool
	ejectedAt     time.Time
	ejectionCount int
	reason        string
}

// OutlierDetector performs statistical outlier detection on upstream endpoints.
// It tracks per-endpoint success/failure rates in a sliding window and periodically
// ejects endpoints that are statistically worse than their peers.
//
// RecordSuccess and RecordFailure use atomic counters and are lock-free on the
// hot path. Ejection decisions are deferred to the periodic analysis loop which
// runs under the mu lock.
type OutlierDetector struct {
	mu     sync.RWMutex
	logger *zap.Logger
	config OutlierDetectionConfig

	// Per-endpoint request statistics for the current analysis window.
	// The map itself is protected by mu, but individual stat fields use atomics.
	stats map[string]*endpointStats

	// Per-endpoint ejection state.
	ejections map[string]*ejectionInfo

	// Cluster identifier for metrics.
	cluster string

	// Lifecycle management.
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewOutlierDetector creates a new OutlierDetector for the given cluster.
func NewOutlierDetector(cluster string, config OutlierDetectionConfig, logger *zap.Logger) *OutlierDetector {
	return &OutlierDetector{
		logger:    logger,
		config:    config,
		stats:     make(map[string]*endpointStats),
		ejections: make(map[string]*ejectionInfo),
		cluster:   cluster,
		stopCh:    make(chan struct{}),
	}
}

// RecordSuccess records a successful request for the given endpoint.
// This method is optimized for the hot path: it only uses atomic operations
// after the initial map lookup (which requires a read lock).
func (od *OutlierDetector) RecordSuccess(endpointKey string) {
	s := od.getOrCreateStats(endpointKey)
	s.requests.Add(1)
	s.successes.Add(1)
	s.consecutiveErrors.Store(0)
}

// RecordFailure records a failed request for the given endpoint.
// This method is optimized for the hot path: it only uses atomic operations
// after the initial map lookup. Consecutive error ejection is deferred to the
// periodic analysis loop to avoid write locks on every request.
func (od *OutlierDetector) RecordFailure(endpointKey string) {
	s := od.getOrCreateStats(endpointKey)
	s.requests.Add(1)
	newConsec := s.consecutiveErrors.Add(1)

	// Check consecutive error ejection threshold.
	// If exceeded, trigger ejection under the write lock.
	if int(newConsec) >= od.config.ConsecutiveErrors {
		od.mu.Lock()
		od.ejectEndpoint(endpointKey, "consecutive_errors")
		s.consecutiveErrors.Store(0)
		od.mu.Unlock()
	}
}

// IsEjected returns true if the endpoint is currently ejected.
func (od *OutlierDetector) IsEjected(endpointKey string) bool {
	od.mu.RLock()
	defer od.mu.RUnlock()

	info, exists := od.ejections[endpointKey]
	if !exists {
		return false
	}
	return info.ejected
}

// Start begins the periodic outlier detection analysis loop. It runs until the
// context is cancelled or Stop is called.
func (od *OutlierDetector) Start(ctx context.Context) {
	od.logger.Info("Starting outlier detector",
		zap.String("cluster", od.cluster),
		zap.Duration("interval", od.config.Interval),
	)

	od.wg.Add(1)
	go od.analysisLoop(ctx)
}

// Stop stops the outlier detector and waits for the analysis loop to exit.
func (od *OutlierDetector) Stop() {
	close(od.stopCh)
	od.wg.Wait()
	od.logger.Info("Outlier detector stopped", zap.String("cluster", od.cluster))
}

// analysisLoop periodically runs outlier detection and unejection checks.
func (od *OutlierDetector) analysisLoop(ctx context.Context) {
	defer od.wg.Done()

	ticker := time.NewTicker(od.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-od.stopCh:
			return
		case <-ticker.C:
			od.runAnalysis()
		}
	}
}

// runAnalysis performs a single cycle of outlier detection: uneject expired endpoints,
// detect outliers by success rate and failure percentage, and reset window stats.
func (od *OutlierDetector) runAnalysis() {
	od.mu.Lock()
	defer od.mu.Unlock()

	// Phase 1: Auto-uneject endpoints whose ejection period has expired.
	od.checkUnejections()

	// Phase 2: Success-rate based outlier detection.
	od.detectSuccessRateOutliers()

	// Phase 3: Failure-percentage based outlier detection.
	od.detectFailurePercentageOutliers()

	// Phase 4: Reset window statistics for the next interval.
	od.resetStats()

	// Update the ejected gauge metric.
	ejectedCount := 0
	for _, info := range od.ejections {
		if info.ejected {
			ejectedCount++
		}
	}
	metrics.SetEndpointsEjected(od.cluster, ejectedCount)
}

// checkUnejections removes ejections whose duration has expired.
func (od *OutlierDetector) checkUnejections() {
	now := time.Now()
	for key, info := range od.ejections {
		if !info.ejected {
			continue
		}
		ejectionDuration := od.config.BaseEjectionTime * time.Duration(info.ejectionCount)
		if now.Sub(info.ejectedAt) >= ejectionDuration {
			info.ejected = false
			od.logger.Info("Endpoint unejected after ejection period expired",
				zap.String("cluster", od.cluster),
				zap.String("endpoint", key),
				zap.Int("ejection_count", info.ejectionCount),
			)
		}
	}
}

// detectSuccessRateOutliers ejects endpoints whose success rate is more than
// SuccessRateStdevFactor standard deviations below the cluster mean.
func (od *OutlierDetector) detectSuccessRateOutliers() {
	// Collect success rates for endpoints meeting the minimum request volume.
	type epRate struct {
		key  string
		rate float64
	}
	var eligible []epRate

	for key, s := range od.stats {
		reqs := s.requests.Load()
		if reqs >= int64(od.config.SuccessRateRequestVolume) {
			rate := float64(s.successes.Load()) / float64(reqs) * 100.0
			eligible = append(eligible, epRate{key: key, rate: rate})
		}
	}

	// Need minimum number of hosts to compute meaningful statistics.
	if len(eligible) < od.config.SuccessRateMinHosts {
		return
	}

	// Compute mean success rate.
	var sum float64
	for _, ep := range eligible {
		sum += ep.rate
	}
	mean := sum / float64(len(eligible))

	// Compute standard deviation.
	var varianceSum float64
	for _, ep := range eligible {
		diff := ep.rate - mean
		varianceSum += diff * diff
	}
	stdev := math.Sqrt(varianceSum / float64(len(eligible)))

	threshold := mean - od.config.SuccessRateStdevFactor*stdev

	od.logger.Debug("Success rate outlier analysis",
		zap.String("cluster", od.cluster),
		zap.Float64("mean", mean),
		zap.Float64("stdev", stdev),
		zap.Float64("threshold", threshold),
		zap.Int("eligible_hosts", len(eligible)),
	)

	for _, ep := range eligible {
		if ep.rate < threshold {
			od.ejectEndpoint(ep.key, "success_rate")
		}
	}
}

// detectFailurePercentageOutliers ejects endpoints whose failure percentage
// exceeds the configured threshold.
func (od *OutlierDetector) detectFailurePercentageOutliers() {
	for key, s := range od.stats {
		reqs := s.requests.Load()
		if reqs == 0 {
			continue
		}
		succs := s.successes.Load()
		failurePct := float64(reqs-succs) / float64(reqs) * 100.0
		if failurePct >= od.config.FailurePercentageThreshold {
			od.ejectEndpoint(key, "failure_percentage")
		}
	}
}

// ejectEndpoint ejects the given endpoint if the max ejection percentage is not exceeded.
// Must be called with od.mu held.
func (od *OutlierDetector) ejectEndpoint(endpointKey, reason string) {
	// Check if already ejected.
	if info, exists := od.ejections[endpointKey]; exists && info.ejected {
		return
	}

	// Enforce max ejection percentage.
	totalEndpoints := len(od.stats)
	if totalEndpoints == 0 {
		return
	}

	currentlyEjected := 0
	for _, info := range od.ejections {
		if info.ejected {
			currentlyEjected++
		}
	}

	maxEjectable := int(math.Floor(float64(totalEndpoints) * od.config.MaxEjectionPercent / 100.0))
	if maxEjectable < 1 {
		maxEjectable = 1
	}
	if currentlyEjected >= maxEjectable {
		od.logger.Debug("Max ejection percentage reached, skipping ejection",
			zap.String("cluster", od.cluster),
			zap.String("endpoint", endpointKey),
			zap.String("reason", reason),
			zap.Int("currently_ejected", currentlyEjected),
			zap.Int("max_ejectable", maxEjectable),
		)
		return
	}

	// Create or update ejection info.
	info, exists := od.ejections[endpointKey]
	if !exists {
		info = &ejectionInfo{}
		od.ejections[endpointKey] = info
	}

	info.ejected = true
	info.ejectedAt = time.Now()
	info.ejectionCount++
	info.reason = reason

	od.logger.Warn("Endpoint ejected by outlier detection",
		zap.String("cluster", od.cluster),
		zap.String("endpoint", endpointKey),
		zap.String("reason", reason),
		zap.Int("ejection_count", info.ejectionCount),
		zap.Duration("ejection_duration", od.config.BaseEjectionTime*time.Duration(info.ejectionCount)),
	)

	// Record metrics.
	metrics.RecordEndpointEjection(od.cluster, endpointKey, reason)
}

// resetStats clears per-endpoint request/success counters for the next window.
// Consecutive error counts are preserved across windows.
func (od *OutlierDetector) resetStats() {
	for _, s := range od.stats {
		s.requests.Store(0)
		s.successes.Store(0)
		// consecutiveErrors is NOT reset; it persists across windows.
	}
}

// getOrCreateStats returns the stats entry for the endpoint, creating one if needed.
// Uses a read lock for the fast path (endpoint already exists) and upgrades to a
// write lock only when a new entry must be created.
func (od *OutlierDetector) getOrCreateStats(endpointKey string) *endpointStats {
	// Fast path: read lock
	od.mu.RLock()
	s, exists := od.stats[endpointKey]
	od.mu.RUnlock()
	if exists {
		return s
	}

	// Slow path: write lock to create new entry
	od.mu.Lock()
	defer od.mu.Unlock()

	// Double-check after acquiring write lock
	if s, exists = od.stats[endpointKey]; exists {
		return s
	}

	s = &endpointStats{}
	od.stats[endpointKey] = s
	return s
}
