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
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// CheckMode represents the type of health check to perform.
type CheckMode int

const (
	// CheckHTTP performs an HTTP health check.
	CheckHTTP CheckMode = iota
	// CheckGRPC performs a gRPC health check using the standard
	// grpc.health.v1.Health protocol.
	CheckGRPC
	// CheckTCP performs a TCP health check by dialing the endpoint.
	// The check succeeds if a TCP connection can be established.
	CheckTCP
	// CheckHTTPS performs an HTTPS health check with TLS.
	CheckHTTPS
)

// DefaultHealthCheckPath is the default HTTP(S) health check path used when
// no path is configured in the cluster health check configuration.
const DefaultHealthCheckPath = "/health"

// DefaultHealthCheckInterval is the default interval between consecutive
// health checks when no interval is configured.
const DefaultHealthCheckInterval = 10 * time.Second

// CheckConfig holds configurable health check parameters extracted
// from the cluster protobuf configuration with sensible defaults.
type CheckConfig struct {
	// Path is the HTTP(S) path to check (default: "/health").
	Path string

	// Interval is the duration between consecutive health checks (default: 10s).
	Interval time.Duration

	// Mode is the type of health check to perform.
	Mode CheckMode

	// InsecureSkipVerify disables TLS certificate verification for HTTPS
	// health checks. Defaults to false (certificates are verified).
	InsecureSkipVerify bool
}

// Checker performs active health checks on endpoints
type Checker struct {
	mu     sync.RWMutex
	logger *zap.Logger

	// Cluster configuration
	cluster *pb.Cluster

	// Endpoints to check
	endpoints []*pb.Endpoint

	// Health check results
	results map[string]*Result

	// Circuit breakers per endpoint
	circuitBreakers map[string]*CircuitBreaker

	// HTTP client for health checks
	httpClient *http.Client

	// HTTPS client for health checks (with TLS, skip verify)
	httpsClient *http.Client

	// gRPC health checker (nil when mode is not gRPC)
	grpcChecker *GRPCHealthChecker

	// Health check configuration
	config CheckConfig

	// Stop channel
	stopCh chan struct{}
	wg     sync.WaitGroup

	// recordCh is used for async passive health recording (RecordSuccess/RecordFailure).
	// Sending to this channel is non-blocking, keeping the hot request path lock-free.
	recordCh chan passiveRecord
}

// passiveRecord carries a single passive health event (success or failure).
type passiveRecord struct {
	endpoint *pb.Endpoint
	success  bool
}

// Result stores the result of a health check
type Result struct {
	Endpoint  *pb.Endpoint
	Healthy   bool
	LastCheck time.Time
	LastError error

	// Consecutive check counts
	ConsecutiveSuccesses uint32
	ConsecutiveFailures  uint32
}

// NewChecker creates a new health checker
func NewChecker(cluster *pb.Cluster, endpoints []*pb.Endpoint, logger *zap.Logger) *Checker {
	config := buildCheckConfig(cluster)

	hc := &Checker{
		logger:          logger,
		cluster:         cluster,
		endpoints:       endpoints,
		results:         make(map[string]*Result),
		circuitBreakers: make(map[string]*CircuitBreaker),
		httpClient: &http.Client{
			Timeout: DefaultHealthCheckTimeout,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   DefaultHealthCheckDialTimeout,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:        50,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		httpsClient: &http.Client{
			Timeout: DefaultHealthCheckTimeout,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   DefaultHealthCheckDialTimeout,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:        50,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     90 * time.Second,
				TLSClientConfig: &tls.Config{
					MinVersion:         tls.VersionTLS12,
					InsecureSkipVerify: config.InsecureSkipVerify, //nolint:gosec // Configurable per-backend TLS verification
				},
			},
		},
		config:   config,
		stopCh:   make(chan struct{}),
		recordCh: make(chan passiveRecord, 10000),
	}

	// Configure gRPC health checking if the cluster specifies it
	if hcConfig := cluster.GetHealthCheck(); hcConfig != nil &&
		hcConfig.GetType() == pb.HealthCheckType_HEALTH_CHECK_GRPC {
		timeout := DefaultHealthCheckTimeout
		if hcConfig.GetTimeoutMs() > 0 {
			timeout = time.Duration(hcConfig.GetTimeoutMs()) * time.Millisecond
		}

		hc.config.Mode = CheckGRPC
		hc.grpcChecker = &GRPCHealthChecker{
			ServiceName:        hcConfig.GetGrpcServiceName(),
			Timeout:            timeout,
			UnhealthyThreshold: int(hcConfig.GetUnhealthyThreshold()),
			HealthyThreshold:   int(hcConfig.GetHealthyThreshold()),
		}
	}

	return hc
}

// buildCheckConfig extracts health check configuration from the cluster
// protobuf, falling back to defaults for any unset fields.
func buildCheckConfig(cluster *pb.Cluster) CheckConfig {
	config := CheckConfig{
		Path:     DefaultHealthCheckPath,
		Interval: DefaultHealthCheckInterval,
		Mode:     CheckHTTP,
	}

	// Configure TLS skip-verify from cluster TLS settings.
	// This defaults to false (secure) when not explicitly set.
	if tlsConfig := cluster.GetTls(); tlsConfig != nil {
		config.InsecureSkipVerify = tlsConfig.GetInsecureSkipVerify()
	}

	hcConfig := cluster.GetHealthCheck()
	if hcConfig == nil {
		return config
	}

	// Configure path from protobuf
	if hcConfig.GetHttpPath() != "" {
		config.Path = hcConfig.GetHttpPath()
	}

	// Configure interval from protobuf
	if hcConfig.GetIntervalMs() > 0 {
		config.Interval = time.Duration(hcConfig.GetIntervalMs()) * time.Millisecond
	}

	// Configure mode from protobuf type
	switch hcConfig.GetType() {
	case pb.HealthCheckType_HEALTH_CHECK_GRPC:
		config.Mode = CheckGRPC
	case pb.HealthCheckType_HEALTH_CHECK_TCP:
		config.Mode = CheckTCP
	case pb.HealthCheckType_HEALTH_CHECK_HTTPS:
		config.Mode = CheckHTTPS
	default:
		config.Mode = CheckHTTP
	}

	return config
}

// Start starts the health checker
func (hc *Checker) Start(ctx context.Context) {
	hc.logger.Info("Starting health checker",
		zap.String("cluster", fmt.Sprintf("%s/%s", hc.cluster.Namespace, hc.cluster.Name)),
		zap.Int("endpoints", len(hc.endpoints)),
	)

	// Initialize results and circuit breakers for all endpoints
	clusterKey := fmt.Sprintf("%s/%s", hc.cluster.Namespace, hc.cluster.Name)
	hc.mu.Lock()
	for _, ep := range hc.endpoints {
		key := endpointKey(ep)
		hc.results[key] = &Result{
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

	// Start passive record processor
	hc.wg.Add(1)
	go hc.processPassiveRecords(ctx)
}

// Stop stops the health checker
func (hc *Checker) Stop() {
	close(hc.stopCh)
	hc.wg.Wait()
	hc.logger.Info("Health checker stopped")
}

// UpdateEndpoints updates the list of endpoints to check
func (hc *Checker) UpdateEndpoints(endpoints []*pb.Endpoint) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.endpoints = endpoints
	clusterKey := fmt.Sprintf("%s/%s", hc.cluster.Namespace, hc.cluster.Name)

	// Add new endpoints
	for _, ep := range endpoints {
		key := endpointKey(ep)
		if _, exists := hc.results[key]; !exists {
			hc.results[key] = &Result{
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
func (hc *Checker) IsHealthy(endpoint *pb.Endpoint) bool {
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

// RecordSuccess records a successful request (for passive health checking).
// This is called on every request so it must be lock-free — it sends a
// non-blocking event to the background processor.
func (hc *Checker) RecordSuccess(endpoint *pb.Endpoint) {
	select {
	case hc.recordCh <- passiveRecord{endpoint: endpoint, success: true}:
	default:
		// Channel full — drop event. Passive records are best-effort;
		// active health checks provide the authoritative health state.
	}
}

// RecordFailure records a failed request (for passive health checking).
// This is called on every failed request so it must be lock-free — it sends
// a non-blocking event to the background processor.
func (hc *Checker) RecordFailure(endpoint *pb.Endpoint) {
	select {
	case hc.recordCh <- passiveRecord{endpoint: endpoint, success: false}:
	default:
		// Channel full — drop event (see RecordSuccess comment).
	}
}

// processPassiveRecords drains the recordCh channel and applies passive
// health events under the write lock. Running as a single goroutine
// serializes all passive updates without blocking the request hot path.
func (hc *Checker) processPassiveRecords(ctx context.Context) {
	defer hc.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-hc.stopCh:
			return
		case rec := <-hc.recordCh:
			hc.applyPassiveRecord(rec)
		}
	}
}

// applyPassiveRecord applies a single passive health record under the write lock.
func (hc *Checker) applyPassiveRecord(rec passiveRecord) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	key := endpointKey(rec.endpoint)
	if rec.success {
		if cb, exists := hc.circuitBreakers[key]; exists {
			cb.RecordSuccess()
		}
	} else {
		if cb, exists := hc.circuitBreakers[key]; exists {
			cb.RecordFailure()
		}
		if result, exists := hc.results[key]; exists {
			result.ConsecutiveSuccesses = 0
			result.ConsecutiveFailures++
			if result.ConsecutiveFailures >= DefaultUnhealthyThreshold {
				result.Healthy = false
			}
		}
	}
}

// drainRecordCh synchronously processes all buffered passive records.
// It is intended for use in tests only, where the background goroutine
// started by Start() is not running.
func (hc *Checker) drainRecordCh() {
	for {
		select {
		case rec := <-hc.recordCh:
			hc.applyPassiveRecord(rec)
		default:
			return
		}
	}
}

// healthCheckLoop runs the active health check loop
func (hc *Checker) healthCheckLoop(ctx context.Context) {
	defer hc.wg.Done()

	ticker := time.NewTicker(hc.config.Interval)
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

// healthCheckWorkers is the maximum number of concurrent goroutines used
// to perform health checks. This bounds resource usage regardless of
// how many endpoints are configured.
const healthCheckWorkers = 10

// performHealthChecks performs health checks on all endpoints using a
// bounded worker pool to avoid spawning unbounded goroutines.
func (hc *Checker) performHealthChecks(ctx context.Context) {
	hc.mu.RLock()
	endpoints := make([]*pb.Endpoint, len(hc.endpoints))
	copy(endpoints, hc.endpoints)
	hc.mu.RUnlock()

	// Create a job channel and enqueue all endpoints
	jobs := make(chan *pb.Endpoint, len(endpoints))
	for _, ep := range endpoints {
		jobs <- ep
	}
	close(jobs)

	// Spawn a bounded number of workers
	var wg sync.WaitGroup
	for i := 0; i < healthCheckWorkers && i < len(endpoints); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ep := range jobs {
				hc.checkEndpoint(ctx, ep)
			}
		}()
	}
	wg.Wait()
}

// checkEndpoint performs a health check on a single endpoint
func (hc *Checker) checkEndpoint(ctx context.Context, ep *pb.Endpoint) {
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
func (hc *Checker) performCheck(ctx context.Context, ep *pb.Endpoint) (bool, error) {
	switch hc.config.Mode {
	case CheckGRPC:
		return hc.performGRPCCheck(ctx, ep)
	case CheckTCP:
		return hc.performTCPCheck(ctx, ep)
	case CheckHTTPS:
		return hc.performHTTPSCheck(ctx, ep)
	default:
		return hc.performHTTPCheck(ctx, ep)
	}
}

// performHTTPCheck performs an HTTP health check
func (hc *Checker) performHTTPCheck(ctx context.Context, ep *pb.Endpoint) (bool, error) {
	addr := net.JoinHostPort(ep.Address, fmt.Sprint(ep.Port))
	checkURL := "http://" + addr + hc.config.Path

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

// performHTTPSCheck performs an HTTPS health check with TLS.
// TLS certificate verification is skipped by default since health checks
// probe backend endpoints that may use self-signed certificates.
func (hc *Checker) performHTTPSCheck(ctx context.Context, ep *pb.Endpoint) (bool, error) {
	addr := net.JoinHostPort(ep.Address, fmt.Sprint(ep.Port))
	checkURL := "https://" + addr + hc.config.Path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return false, err
	}

	resp, err := hc.httpsClient.Do(req)
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

// performTCPCheck performs a TCP health check by dialing the endpoint.
// The check succeeds if a TCP connection can be established within the
// configured dial timeout.
func (hc *Checker) performTCPCheck(ctx context.Context, ep *pb.Endpoint) (bool, error) {
	addr := net.JoinHostPort(ep.Address, fmt.Sprint(ep.Port))

	dialer := &net.Dialer{Timeout: DefaultHealthCheckDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false, fmt.Errorf("tcp health check failed for %s: %w", addr, err)
	}
	_ = conn.Close()

	return true, nil
}

// performGRPCCheck performs a gRPC health check using the standard
// grpc.health.v1.Health/Check protocol.
func (hc *Checker) performGRPCCheck(ctx context.Context, ep *pb.Endpoint) (bool, error) {
	address := net.JoinHostPort(ep.Address, fmt.Sprint(ep.Port))
	return hc.grpcChecker.Check(ctx, address)
}

// GetHealthyEndpoints returns only healthy endpoints
func (hc *Checker) GetHealthyEndpoints() []*pb.Endpoint {
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
