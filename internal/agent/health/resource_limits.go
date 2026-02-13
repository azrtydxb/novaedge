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
	"sync/atomic"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
)

const (
	// DefaultMaxConnections is the default maximum number of active connections.
	DefaultMaxConnections int64 = 1024

	// DefaultMaxPendingRequests is the default maximum number of pending requests.
	DefaultMaxPendingRequests int64 = 1024

	// DefaultMaxRequests is the default maximum number of active requests.
	DefaultMaxRequests int64 = 1024

	// DefaultMaxRetries is the default maximum number of active retries.
	DefaultMaxRetries int64 = 3
)

// ResourceLimitsConfig configures the resource limits for a circuit breaker.
type ResourceLimitsConfig struct {
	// MaxConnections is the maximum number of concurrent connections allowed.
	MaxConnections int64
	// MaxPendingRequests is the maximum number of pending requests allowed.
	MaxPendingRequests int64
	// MaxRequests is the maximum number of concurrent requests allowed.
	MaxRequests int64
	// MaxRetries is the maximum number of concurrent retries allowed.
	MaxRetries int64
}

// DefaultResourceLimitsConfig returns a ResourceLimitsConfig with default values.
func DefaultResourceLimitsConfig() ResourceLimitsConfig {
	return ResourceLimitsConfig{
		MaxConnections:     DefaultMaxConnections,
		MaxPendingRequests: DefaultMaxPendingRequests,
		MaxRequests:        DefaultMaxRequests,
		MaxRetries:         DefaultMaxRetries,
	}
}

// ResourceLimits tracks and enforces resource limits for a cluster using atomic
// counters. Each TryAcquire method atomically increments the counter and checks
// against the configured limit; if the limit is exceeded, it decrements back and
// records an overflow event.
type ResourceLimits struct {
	config ResourceLimitsConfig

	activeConnections atomic.Int64
	pendingRequests   atomic.Int64
	activeRequests    atomic.Int64
	activeRetries     atomic.Int64

	overflowConnections atomic.Int64
	overflowPending     atomic.Int64
	overflowRequests    atomic.Int64
	overflowRetries     atomic.Int64

	cluster string
}

// NewResourceLimits creates a new ResourceLimits with the given config and cluster name.
func NewResourceLimits(config ResourceLimitsConfig, cluster string) *ResourceLimits {
	return &ResourceLimits{
		config:  config,
		cluster: cluster,
	}
}

// TryAcquireConnection attempts to acquire a connection slot. It returns true
// if a slot was available, or false if the MaxConnections limit has been reached.
func (rl *ResourceLimits) TryAcquireConnection() bool {
	current := rl.activeConnections.Add(1)
	if current > rl.config.MaxConnections {
		rl.activeConnections.Add(-1)
		rl.overflowConnections.Add(1)
		metrics.RecordCircuitBreakerOverflow(rl.cluster, "connections")
		return false
	}
	return true
}

// ReleaseConnection releases a previously acquired connection slot.
func (rl *ResourceLimits) ReleaseConnection() {
	rl.activeConnections.Add(-1)
}

// TryAcquirePendingRequest attempts to acquire a pending request slot. It returns
// true if a slot was available, or false if the MaxPendingRequests limit has been reached.
func (rl *ResourceLimits) TryAcquirePendingRequest() bool {
	current := rl.pendingRequests.Add(1)
	if current > rl.config.MaxPendingRequests {
		rl.pendingRequests.Add(-1)
		rl.overflowPending.Add(1)
		metrics.RecordCircuitBreakerOverflow(rl.cluster, "pending")
		return false
	}
	return true
}

// ReleasePendingRequest releases a previously acquired pending request slot.
func (rl *ResourceLimits) ReleasePendingRequest() {
	rl.pendingRequests.Add(-1)
}

// TryAcquireRequest attempts to acquire a request slot. It returns true if a
// slot was available, or false if the MaxRequests limit has been reached.
func (rl *ResourceLimits) TryAcquireRequest() bool {
	current := rl.activeRequests.Add(1)
	if current > rl.config.MaxRequests {
		rl.activeRequests.Add(-1)
		rl.overflowRequests.Add(1)
		metrics.RecordCircuitBreakerOverflow(rl.cluster, "requests")
		return false
	}
	return true
}

// ReleaseRequest releases a previously acquired request slot.
func (rl *ResourceLimits) ReleaseRequest() {
	rl.activeRequests.Add(-1)
}

// TryAcquireRetry attempts to acquire a retry slot. It returns true if a slot
// was available, or false if the MaxRetries limit has been reached.
func (rl *ResourceLimits) TryAcquireRetry() bool {
	current := rl.activeRetries.Add(1)
	if current > rl.config.MaxRetries {
		rl.activeRetries.Add(-1)
		rl.overflowRetries.Add(1)
		metrics.RecordCircuitBreakerOverflow(rl.cluster, "retries")
		return false
	}
	return true
}

// ReleaseRetry releases a previously acquired retry slot.
func (rl *ResourceLimits) ReleaseRetry() {
	rl.activeRetries.Add(-1)
}

// OverflowCounts returns the total number of overflow events for each limit type.
func (rl *ResourceLimits) OverflowCounts() (connections, pending, requests, retries int64) {
	return rl.overflowConnections.Load(),
		rl.overflowPending.Load(),
		rl.overflowRequests.Load(),
		rl.overflowRetries.Load()
}

// ActiveCounts returns the current number of active resources for each limit type.
func (rl *ResourceLimits) ActiveCounts() (connections, pending, requests, retries int64) {
	return rl.activeConnections.Load(),
		rl.pendingRequests.Load(),
		rl.activeRequests.Load(),
		rl.activeRetries.Load()
}
