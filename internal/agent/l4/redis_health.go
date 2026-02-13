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

package l4

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// DefaultRedisHealthCheckInterval is the default interval between health checks
	DefaultRedisHealthCheckInterval = 5 * time.Second
	// DefaultRedisHealthCheckTimeout is the default timeout for a single health check
	DefaultRedisHealthCheckTimeout = 2 * time.Second
	// DefaultRedisHealthCheckThreshold is the number of consecutive failures before marking unhealthy
	DefaultRedisHealthCheckThreshold = 3
)

// RedisHealthCheckerConfig configures the Redis health checker
type RedisHealthCheckerConfig struct {
	// CheckInterval is the time between health checks
	CheckInterval time.Duration
	// CheckTimeout is the timeout for a single PING check
	CheckTimeout time.Duration
	// FailureThreshold is the number of consecutive failures before marking backend unhealthy
	FailureThreshold int
}

// RedisHealthChecker performs Redis-specific health checks by sending PING and expecting PONG
type RedisHealthChecker struct {
	config   RedisHealthCheckerConfig
	logger   *zap.Logger
	mu       sync.RWMutex
	backends []*pb.Endpoint
	// failureCounts tracks consecutive failure count per backend address
	failureCounts map[string]int
	cancel        context.CancelFunc
}

// NewRedisHealthChecker creates a new Redis health checker
func NewRedisHealthChecker(cfg RedisHealthCheckerConfig, logger *zap.Logger) *RedisHealthChecker {
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = DefaultRedisHealthCheckInterval
	}
	if cfg.CheckTimeout == 0 {
		cfg.CheckTimeout = DefaultRedisHealthCheckTimeout
	}
	if cfg.FailureThreshold == 0 {
		cfg.FailureThreshold = DefaultRedisHealthCheckThreshold
	}

	return &RedisHealthChecker{
		config:        cfg,
		logger:        logger.With(zap.String("component", "redis-health-checker")),
		failureCounts: make(map[string]int),
	}
}

// Start begins periodic health checking of all registered backends
func (h *RedisHealthChecker) Start(ctx context.Context) {
	checkCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel

	go h.runHealthCheckLoop(checkCtx)
}

// Stop stops the health checker
func (h *RedisHealthChecker) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
}

// UpdateBackends updates the list of backends to health check
func (h *RedisHealthChecker) UpdateBackends(backends []*pb.Endpoint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.backends = backends

	// Clean up failure counts for removed backends
	activeAddrs := make(map[string]bool, len(backends))
	for _, b := range backends {
		addr := fmt.Sprintf("%s:%d", b.Address, b.Port)
		activeAddrs[addr] = true
	}
	for addr := range h.failureCounts {
		if !activeAddrs[addr] {
			delete(h.failureCounts, addr)
		}
	}
}

// GetBackends returns the current list of backends with health status
func (h *RedisHealthChecker) GetBackends() []*pb.Endpoint {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]*pb.Endpoint, len(h.backends))
	copy(result, h.backends)
	return result
}

// runHealthCheckLoop runs health checks on a periodic interval
func (h *RedisHealthChecker) runHealthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(h.config.CheckInterval)
	defer ticker.Stop()

	// Run an initial check immediately
	h.checkAllBackends(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.checkAllBackends(ctx)
		}
	}
}

// checkAllBackends checks all registered backends
func (h *RedisHealthChecker) checkAllBackends(ctx context.Context) {
	h.mu.Lock()
	backends := make([]*pb.Endpoint, len(h.backends))
	copy(backends, h.backends)
	h.mu.Unlock()

	for _, backend := range backends {
		addr := fmt.Sprintf("%s:%d", backend.Address, backend.Port)
		healthy := h.checkBackend(ctx, addr)

		h.mu.Lock()
		if healthy {
			h.failureCounts[addr] = 0
			if !backend.Ready {
				backend.Ready = true
				h.logger.Info("Redis backend became healthy",
					zap.String("address", addr))
			}
		} else {
			h.failureCounts[addr]++
			if h.failureCounts[addr] >= h.config.FailureThreshold && backend.Ready {
				backend.Ready = false
				h.logger.Warn("Redis backend became unhealthy",
					zap.String("address", addr),
					zap.Int("consecutive_failures", h.failureCounts[addr]))
			}
		}
		h.mu.Unlock()
	}
}

// checkBackend performs a single PING/PONG health check against a Redis backend
func (h *RedisHealthChecker) checkBackend(ctx context.Context, addr string) bool {
	dialCtx, cancel := context.WithTimeout(ctx, h.config.CheckTimeout)
	defer cancel()

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		h.logger.Debug("Redis health check connect failed",
			zap.String("address", addr),
			zap.Error(err))
		return false
	}
	defer func() { _ = conn.Close() }()

	// Set deadline for the entire PING/PONG exchange
	if err := conn.SetDeadline(time.Now().Add(h.config.CheckTimeout)); err != nil {
		return false
	}

	// Send PING command
	pingCmd := EncodeCommand("PING")
	if _, err := conn.Write(pingCmd); err != nil {
		h.logger.Debug("Redis health check write failed",
			zap.String("address", addr),
			zap.Error(err))
		return false
	}

	// Read response
	reader := NewRESPReader(conn)
	val, err := reader.ReadValue()
	if err != nil {
		h.logger.Debug("Redis health check read failed",
			zap.String("address", addr),
			zap.Error(err))
		return false
	}

	// Expect +PONG simple string response
	if val.Type == RESPTypeSimpleString && strings.EqualFold(val.Str, "PONG") {
		return true
	}

	h.logger.Debug("Redis health check unexpected response",
		zap.String("address", addr),
		zap.String("response", val.Str))
	return false
}

// CheckSingle performs a single synchronous health check on a specific address.
// Returns true if the backend responded with PONG.
func (h *RedisHealthChecker) CheckSingle(ctx context.Context, addr string) bool {
	return h.checkBackend(ctx, addr)
}
