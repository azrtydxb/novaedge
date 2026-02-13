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
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// HealthCheckMode represents the type of health check to perform.
type HealthCheckMode int

const (
	// HealthCheckHTTP performs an HTTP health check.
	HealthCheckHTTP HealthCheckMode = iota
	// HealthCheckGRPC performs a gRPC health check using the standard
	// grpc.health.v1.Health protocol.
	HealthCheckGRPC
)

// HealthChecker performs active health checks on endpoints
type HealthChecker struct {
	mu     sync.RWMutex
	logger *zap.Logger

	// Cluster configuration
	cluster *pb.Cluster

	// Endpoints to check
	endpoints []*pb.Endpoint

	// Health check results
	results map[string]*HealthResult

	// Circuit breakers per endpoint
	circuitBreakers map[string]*CircuitBreaker

	// HTTP client for health checks
	httpClient *http.Client

	// gRPC health checker (nil when mode is HTTP)
	grpcChecker *GRPCHealthChecker

	// Health check mode
	mode HealthCheckMode

	// Stop channel
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// HealthResult stores the result of a health check
type HealthResult struct {
	Endpoint  *pb.Endpoint
	Healthy   bool
	LastCheck time.Time
	LastError error

	// Consecutive check counts
	ConsecutiveSuccesses uint32
	ConsecutiveFailures  uint32
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(cluster *pb.Cluster, endpoints []*pb.Endpoint, logger *zap.Logger) *HealthChecker {
	hc := &HealthChecker{
		logger:          logger,
		cluster:         cluster,
		endpoints:       endpoints,
		results:         make(map[string]*HealthResult),
		circuitBreakers: make(map[string]*CircuitBreaker),
		httpClient: &http.Client{
			Timeout: DefaultHealthCheckTimeout,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   DefaultHealthCheckDialTimeout,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
		mode:   HealthCheckHTTP,
		stopCh: make(chan struct{}),
	}

	// Configure gRPC health checking if the cluster specifies it
	if hcConfig := cluster.GetHealthCheck(); hcConfig != nil &&
		hcConfig.GetType() == pb.HealthCheckType_HEALTH_CHECK_GRPC {
		timeout := DefaultHealthCheckTimeout
		if hcConfig.GetTimeoutMs() > 0 {
			timeout = time.Duration(hcConfig.GetTimeoutMs()) * time.Millisecond
		}

		hc.mode = HealthCheckGRPC
		hc.grpcChecker = &GRPCHealthChecker{
			ServiceName:        hcConfig.GetGrpcServiceName(),
			Timeout:            timeout,
			UnhealthyThreshold: int(hcConfig.GetUnhealthyThreshold()),
			HealthyThreshold:   int(hcConfig.GetHealthyThreshold()),
		}
	}

	return hc
}

// Start starts the health checker
func (hc *HealthChecker) Start(ctx context.Context) {
	hc.logger.Info("Starting health checker",
		zap.String("cluster", fmt.Sprintf("%s/%s", hc.cluster.Namespace, hc.cluster.Name)),
		zap.Int("endpoints", len(hc.endpoints)),
	)

	// Initialize results and circuit breakers for all endpoints
	clusterKey := fmt.Sprintf("%s/%s", hc.cluster.Namespace, hc.cluster.Name)
	hc.mu.Lock()
	for _, ep := range hc.endpoints {
		key := endpointKey(ep)
		hc.results[key] = &HealthResult{
			Endpoint:  ep,
			Healthy:   true, // Optimistically assume healthy initially
			LastCheck: time.Now(),
		}
		cb := NewCircuitBreaker(
			key,
			DefaultCircuitBreakerConfig(),
			hc.logger,
		)
		cb.SetCluster(clusterKey)
		hc.circuitBreakers[key] = cb
	}
	hc.mu.Unlock()

	// Start health check loop
	hc.wg.Add(1)
	go hc.healthCheckLoop(ctx)
}

// Stop stops the health checker
func (hc *HealthChecker) Stop() {
	close(hc.stopCh)
	hc.wg.Wait()
	hc.logger.Info("Health checker stopped")
}

// UpdateEndpoints updates the list of endpoints to check
func (hc *HealthChecker) UpdateEndpoints(endpoints []*pb.Endpoint) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.endpoints = endpoints
	clusterKey := fmt.Sprintf("%s/%s", hc.cluster.Namespace, hc.cluster.Name)

	// Add new endpoints
	for _, ep := range endpoints {
		key := endpointKey(ep)
		if _, exists := hc.results[key]; !exists {
			hc.results[key] = &HealthResult{
				Endpoint:  ep,
				Healthy:   true,
				LastCheck: time.Now(),
			}
			cb := NewCircuitBreaker(
				key,
				DefaultCircuitBreakerConfig(),
				hc.logger,
			)
			cb.SetCluster(clusterKey)
			hc.circuitBreakers[key] = cb
		}
	}

	// Remove old endpoints
	currentKeys := make(map[string]bool)
	for _, ep := range endpoints {
		currentKeys[endpointKey(ep)] = true
	}

	for key := range hc.results {
		if !currentKeys[key] {
			delete(hc.results, key)
			delete(hc.circuitBreakers, key)
		}
	}
}

// IsHealthy returns true if an endpoint is healthy
func (hc *HealthChecker) IsHealthy(endpoint *pb.Endpoint) bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	key := endpointKey(endpoint)
	result, exists := hc.results[key]
	if !exists {
		return true // Unknown endpoints are assumed healthy
	}

	// Check circuit breaker
	cb, cbExists := hc.circuitBreakers[key]
	if cbExists && cb.IsOpen() {
		return false
	}

	return result.Healthy
}

// RecordSuccess records a successful request (for passive health checking)
func (hc *HealthChecker) RecordSuccess(endpoint *pb.Endpoint) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	key := endpointKey(endpoint)
	if cb, exists := hc.circuitBreakers[key]; exists {
		cb.RecordSuccess()
	}
}

// RecordFailure records a failed request (for passive health checking)
func (hc *HealthChecker) RecordFailure(endpoint *pb.Endpoint) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	key := endpointKey(endpoint)
	if cb, exists := hc.circuitBreakers[key]; exists {
		cb.RecordFailure()
	}

	// Also update health result
	if result, exists := hc.results[key]; exists {
		result.ConsecutiveSuccesses = 0
		result.ConsecutiveFailures++

		// Mark unhealthy after threshold
		if result.ConsecutiveFailures >= DefaultUnhealthyThreshold {
			result.Healthy = false
		}
	}
}

// healthCheckLoop runs the active health check loop
func (hc *HealthChecker) healthCheckLoop(ctx context.Context) {
	defer hc.wg.Done()

	// Default interval: 10 seconds
	interval := 10 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-hc.stopCh:
			return
		case <-ticker.C:
			hc.performHealthChecks(ctx)
		}
	}
}

// performHealthChecks performs health checks on all endpoints
func (hc *HealthChecker) performHealthChecks(ctx context.Context) {
	hc.mu.RLock()
	endpoints := make([]*pb.Endpoint, len(hc.endpoints))
	copy(endpoints, hc.endpoints)
	hc.mu.RUnlock()

	// Use WaitGroup to ensure all health checks complete before returning
	var wg sync.WaitGroup
	for _, ep := range endpoints {
		wg.Add(1)
		// Perform check in goroutine for concurrency
		go func(endpoint *pb.Endpoint) {
			defer wg.Done()
			hc.checkEndpoint(ctx, endpoint)
		}(ep)
	}

	// Wait for all health checks to complete
	wg.Wait()
}

// checkEndpoint performs a health check on a single endpoint
func (hc *HealthChecker) checkEndpoint(ctx context.Context, ep *pb.Endpoint) {
	key := endpointKey(ep)
	clusterKey := fmt.Sprintf("%s/%s", hc.cluster.Namespace, hc.cluster.Name)

	// Check circuit breaker before performing health check
	hc.mu.RLock()
	cb, cbExists := hc.circuitBreakers[key]
	hc.mu.RUnlock()

	if cbExists {
		// If circuit breaker is open, skip health check to avoid overloading failing backend
		if cb.IsOpen() {
			hc.logger.Debug("Skipping health check - circuit breaker is open",
				zap.String("endpoint", key),
				zap.String("cluster", clusterKey))
			return
		}

		// Check if request is allowed by circuit breaker
		if !cb.Allow() {
			hc.logger.Debug("Health check rejected by circuit breaker",
				zap.String("endpoint", key),
				zap.String("cluster", clusterKey))
			return
		}
	}

	// Track health check timing
	checkStart := time.Now()

	// Dispatch to the appropriate health check implementation
	healthy, err := hc.performCheck(ctx, ep)

	// Record health check metrics
	checkDuration := time.Since(checkStart).Seconds()
	checkResult := "success"
	if !healthy {
		checkResult = "failure"
	}
	metrics.RecordHealthCheck(clusterKey, key, checkResult, checkDuration)

	hc.mu.Lock()
	defer hc.mu.Unlock()

	result, exists := hc.results[key]
	if !exists {
		return
	}

	result.LastCheck = time.Now()
	result.LastError = err

	if healthy {
		result.ConsecutiveSuccesses++
		result.ConsecutiveFailures = 0

		// Mark healthy after threshold
		if result.ConsecutiveSuccesses >= DefaultHealthyThreshold {
			if !result.Healthy {
				hc.logger.Info("Endpoint became healthy",
					zap.String("endpoint", key),
				)
			}
			result.Healthy = true
			// Update health status metric
			metrics.SetBackendHealth(clusterKey, key, true)
		}

		// Record success in circuit breaker
		if cb, exists := hc.circuitBreakers[key]; exists {
			cb.RecordSuccess()
		}
	} else {
		result.ConsecutiveSuccesses = 0
		result.ConsecutiveFailures++

		// Mark unhealthy after threshold
		if result.ConsecutiveFailures >= DefaultUnhealthyThreshold {
			if result.Healthy {
				hc.logger.Warn("Endpoint became unhealthy",
					zap.String("endpoint", key),
					zap.Error(err),
				)
			}
			result.Healthy = false
			// Update health status metric
			metrics.SetBackendHealth(clusterKey, key, false)
		}

		// Record failure in circuit breaker
		if cb, exists := hc.circuitBreakers[key]; exists {
			cb.RecordFailure()
		}
	}
}

// performCheck dispatches to the appropriate health check implementation
// based on the configured health check mode.
func (hc *HealthChecker) performCheck(ctx context.Context, ep *pb.Endpoint) (bool, error) {
	switch hc.mode {
	case HealthCheckGRPC:
		return hc.performGRPCCheck(ctx, ep)
	default:
		return hc.performHTTPCheck(ctx, ep)
	}
}

// performHTTPCheck performs an HTTP health check
func (hc *HealthChecker) performHTTPCheck(ctx context.Context, ep *pb.Endpoint) (bool, error) {
	// Build health check URL
	checkURL := "http://" + net.JoinHostPort(ep.Address, fmt.Sprint(ep.Port)) + "/health"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return false, err
	}

	resp, err := hc.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Consider 200-299 as healthy
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}

	return false, fmt.Errorf("unhealthy status code: %d", resp.StatusCode)
}

// performGRPCCheck performs a gRPC health check using the standard
// grpc.health.v1.Health/Check protocol.
func (hc *HealthChecker) performGRPCCheck(ctx context.Context, ep *pb.Endpoint) (bool, error) {
	address := net.JoinHostPort(ep.Address, fmt.Sprint(ep.Port))
	return hc.grpcChecker.Check(ctx, address)
}

// GetHealthyEndpoints returns only healthy endpoints
func (hc *HealthChecker) GetHealthyEndpoints() []*pb.Endpoint {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	healthy := make([]*pb.Endpoint, 0, len(hc.endpoints))
	for _, ep := range hc.endpoints {
		if hc.IsHealthy(ep) {
			healthy = append(healthy, ep)
		}
	}

	return healthy
}

func endpointKey(ep *pb.Endpoint) string {
	return net.JoinHostPort(ep.Address, fmt.Sprint(ep.Port))
}
