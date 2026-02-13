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

package server

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// OverloadAction represents the action to take when the system is overloaded.
type OverloadAction int

const (
	// ActionNone indicates no overload action is needed.
	ActionNone OverloadAction = iota
	// ActionRejectNew indicates new connections should be rejected with 503.
	ActionRejectNew
	// ActionReduceTimeouts indicates timeouts should be reduced to shed load faster.
	ActionReduceTimeouts
)

// String returns a human-readable representation of the OverloadAction.
func (a OverloadAction) String() string {
	switch a {
	case ActionNone:
		return "none"
	case ActionRejectNew:
		return "reject_new"
	case ActionReduceTimeouts:
		return "reduce_timeouts"
	default:
		return fmt.Sprintf("unknown(%d)", int(a))
	}
}

const (
	// defaultOverloadCheckInterval is the default interval for checking resource usage.
	defaultOverloadCheckInterval = 1 * time.Second

	// hysteresisRecoveryFactor is the multiplier applied to thresholds for recovery.
	// Resources must drop below threshold * hysteresisRecoveryFactor to clear overload.
	hysteresisRecoveryFactor = 0.9

	// overloadRetryAfterSeconds is the value for the Retry-After header in 503 responses.
	overloadRetryAfterSeconds = "5"
)

// OverloadConfig holds configuration for the OverloadManager.
type OverloadConfig struct {
	// MemoryThresholdBytes is the heap allocation threshold in bytes.
	// When HeapAlloc exceeds this value, the system is considered overloaded.
	// A value of 0 disables memory-based overload detection.
	MemoryThresholdBytes int64

	// GoroutineThreshold is the maximum number of goroutines allowed.
	// When runtime.NumGoroutine() exceeds this value, the system is considered overloaded.
	// A value of 0 disables goroutine-based overload detection.
	GoroutineThreshold int

	// FDThreshold is the maximum number of open file descriptors allowed.
	// When the current FD count exceeds this value, the system is considered overloaded.
	// A value of 0 disables FD-based overload detection.
	FDThreshold int

	// CheckInterval is how often resource usage is checked.
	// A value of 0 uses the default of 1 second.
	CheckInterval time.Duration
}

// ResourceReader is an interface for reading system resource usage.
// This allows tests to inject mock resource data.
type ResourceReader interface {
	// ReadMemStats populates the given MemStats struct with current memory statistics.
	ReadMemStats(m *runtime.MemStats)
	// NumGoroutine returns the current number of goroutines.
	NumGoroutine() int
	// CountOpenFDs returns the current number of open file descriptors.
	CountOpenFDs() int
}

// runtimeResourceReader uses the real runtime and OS for resource reading.
type runtimeResourceReader struct{}

func (runtimeResourceReader) ReadMemStats(m *runtime.MemStats) {
	runtime.ReadMemStats(m)
}

func (runtimeResourceReader) NumGoroutine() int {
	return runtime.NumGoroutine()
}

func (runtimeResourceReader) CountOpenFDs() int {
	return countOpenFDs()
}

// OverloadManager monitors system resources and triggers overload protection
// when thresholds are exceeded. It supports hysteresis to prevent oscillation
// between overloaded and normal states.
type OverloadManager struct {
	config   OverloadConfig
	logger   *zap.Logger
	reader   ResourceReader
	action   atomic.Int32
	cancel   context.CancelFunc
	stopOnce sync.Once
	done     chan struct{}
}

// NewOverloadManager creates a new OverloadManager with the given configuration.
func NewOverloadManager(config OverloadConfig, logger *zap.Logger) *OverloadManager {
	if config.CheckInterval <= 0 {
		config.CheckInterval = defaultOverloadCheckInterval
	}

	return &OverloadManager{
		config: config,
		logger: logger,
		reader: runtimeResourceReader{},
		done:   make(chan struct{}),
	}
}

// newOverloadManagerWithReader creates an OverloadManager with a custom ResourceReader (for testing).
func newOverloadManagerWithReader(config OverloadConfig, logger *zap.Logger, reader ResourceReader) *OverloadManager {
	if config.CheckInterval <= 0 {
		config.CheckInterval = defaultOverloadCheckInterval
	}

	return &OverloadManager{
		config: config,
		logger: logger,
		reader: reader,
		done:   make(chan struct{}),
	}
}

// Start begins the periodic resource monitoring loop. It blocks until the
// context is cancelled or Stop() is called.
func (om *OverloadManager) Start(ctx context.Context) {
	ctx, om.cancel = context.WithCancel(ctx)
	defer close(om.done)

	ticker := time.NewTicker(om.config.CheckInterval)
	defer ticker.Stop()

	om.logger.Info("Overload manager started",
		zap.Int64("memory_threshold_bytes", om.config.MemoryThresholdBytes),
		zap.Int("goroutine_threshold", om.config.GoroutineThreshold),
		zap.Int("fd_threshold", om.config.FDThreshold),
		zap.Duration("check_interval", om.config.CheckInterval),
	)

	for {
		select {
		case <-ctx.Done():
			om.logger.Info("Overload manager stopped")
			return
		case <-ticker.C:
			om.check()
		}
	}
}

// Stop stops the overload manager. It is safe to call multiple times.
func (om *OverloadManager) Stop() {
	om.stopOnce.Do(func() {
		if om.cancel != nil {
			om.cancel()
			<-om.done
		}
	})
}

// IsOverloaded returns true if any resource threshold is currently exceeded.
func (om *OverloadManager) IsOverloaded() bool {
	return OverloadAction(om.action.Load()) != ActionNone
}

// CurrentAction returns the current overload action being taken.
func (om *OverloadManager) CurrentAction() OverloadAction {
	return OverloadAction(om.action.Load())
}

// check reads current resource usage and updates the overload state.
func (om *OverloadManager) check() {
	wasOverloaded := om.IsOverloaded()
	newAction := ActionNone

	// Check memory usage
	if om.config.MemoryThresholdBytes > 0 {
		var memStats runtime.MemStats
		om.reader.ReadMemStats(&memStats)
		heapAlloc := int64(memStats.HeapAlloc)
		threshold := om.config.MemoryThresholdBytes

		if heapAlloc > threshold {
			newAction = ActionRejectNew
			if ce := om.logger.Check(zap.WarnLevel, "Memory threshold exceeded"); ce != nil {
				ce.Write(
					zap.Int64("heap_alloc_bytes", heapAlloc),
					zap.Int64("threshold_bytes", threshold),
				)
			}
		} else if wasOverloaded && heapAlloc > int64(float64(threshold)*hysteresisRecoveryFactor) {
			// Within hysteresis band: maintain current state
			newAction = OverloadAction(om.action.Load())
		}
	}

	// Check goroutine count (only escalate, never downgrade)
	if om.config.GoroutineThreshold > 0 {
		numGoroutines := om.reader.NumGoroutine()
		threshold := om.config.GoroutineThreshold

		if numGoroutines > threshold {
			newAction = ActionRejectNew
			if ce := om.logger.Check(zap.WarnLevel, "Goroutine threshold exceeded"); ce != nil {
				ce.Write(
					zap.Int("goroutine_count", numGoroutines),
					zap.Int("threshold", threshold),
				)
			}
		} else if wasOverloaded && numGoroutines > int(float64(threshold)*hysteresisRecoveryFactor) {
			// Within hysteresis band: maintain current state if not already escalated
			if newAction == ActionNone {
				newAction = OverloadAction(om.action.Load())
			}
		}
	}

	// Check file descriptor count (only escalate, never downgrade)
	if om.config.FDThreshold > 0 {
		fdCount := om.reader.CountOpenFDs()
		threshold := om.config.FDThreshold

		if fdCount > threshold {
			newAction = ActionRejectNew
			if ce := om.logger.Check(zap.WarnLevel, "File descriptor threshold exceeded"); ce != nil {
				ce.Write(
					zap.Int("fd_count", fdCount),
					zap.Int("threshold", threshold),
				)
			}
		} else if wasOverloaded && fdCount > int(float64(threshold)*hysteresisRecoveryFactor) {
			// Within hysteresis band: maintain current state if not already escalated
			if newAction == ActionNone {
				newAction = OverloadAction(om.action.Load())
			}
		}
	}

	// Update action atomically
	oldAction := OverloadAction(om.action.Swap(int32(newAction)))

	// Log state transitions
	if oldAction != newAction {
		if newAction == ActionNone {
			om.logger.Info("System recovered from overload",
				zap.String("previous_action", oldAction.String()),
			)
		} else {
			om.logger.Warn("System entered overload state",
				zap.String("action", newAction.String()),
			)
		}
	}
}

// OverloadMiddleware returns an HTTP middleware that rejects requests with 503
// Service Unavailable when the system is overloaded.
func OverloadMiddleware(om *OverloadManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if om.IsOverloaded() {
				w.Header().Set("Retry-After", overloadRetryAfterSeconds)
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("Service temporarily unavailable due to resource constraints"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
