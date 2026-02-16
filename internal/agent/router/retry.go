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

package router

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/lb"
	"github.com/piwi3910/novaedge/internal/agent/metrics"
	"github.com/piwi3910/novaedge/internal/agent/upstream"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// defaultMaxRetries is the default number of retry attempts
const defaultMaxRetries = 3

// defaultBackoffBaseMs is the default base interval for exponential backoff in milliseconds
const defaultBackoffBaseMs = 25

// defaultRetryBudget is the default percentage of requests that can be retried
const defaultRetryBudget = 0.2

// maxRetryBodySize is the maximum request body size (10 MB) that will be
// buffered for retry attempts. Requests with larger bodies skip retries
// and are forwarded once to avoid unbounded memory consumption.
const maxRetryBodySize = 10 << 20

// defaultSafeMethodsSet contains HTTP methods safe for retry by default
var defaultSafeMethodsSet = map[string]bool{
	"GET":     true,
	"HEAD":    true,
	"OPTIONS": true,
}

// RetryPolicy holds parsed retry configuration for a route rule
type RetryPolicy struct {
	MaxRetries     int32
	PerTryTimeout  time.Duration
	RetryOn        map[string]bool
	RetryBudgetPct float64
	BackoffBase    time.Duration
	RetryMethodSet map[string]bool
}

// NewRetryPolicy creates a RetryPolicy from protobuf config
func NewRetryPolicy(cfg *pb.RetryConfig) *RetryPolicy {
	if cfg == nil {
		return nil
	}

	policy := &RetryPolicy{
		MaxRetries:     cfg.MaxRetries,
		RetryBudgetPct: cfg.RetryBudget,
		BackoffBase:    time.Duration(cfg.BackoffBaseMs) * time.Millisecond,
	}

	// Apply defaults
	if policy.MaxRetries <= 0 {
		policy.MaxRetries = defaultMaxRetries
	}
	if policy.RetryBudgetPct <= 0 {
		policy.RetryBudgetPct = defaultRetryBudget
	}
	if policy.BackoffBase <= 0 {
		policy.BackoffBase = time.Duration(defaultBackoffBaseMs) * time.Millisecond
	}

	// Parse per-try timeout
	if cfg.PerTryTimeoutMs > 0 {
		policy.PerTryTimeout = time.Duration(cfg.PerTryTimeoutMs) * time.Millisecond
	}

	// Parse retry conditions
	policy.RetryOn = make(map[string]bool, len(cfg.RetryOn))
	for _, condition := range cfg.RetryOn {
		policy.RetryOn[strings.ToLower(condition)] = true
	}
	// Default: retry on 5xx and connection-failure if nothing specified
	if len(policy.RetryOn) == 0 {
		policy.RetryOn["5xx"] = true
		policy.RetryOn["connection-failure"] = true
	}

	// Parse retry methods
	if len(cfg.RetryMethods) > 0 {
		policy.RetryMethodSet = make(map[string]bool, len(cfg.RetryMethods))
		for _, m := range cfg.RetryMethods {
			policy.RetryMethodSet[strings.ToUpper(m)] = true
		}
	} else {
		// Default safe methods
		policy.RetryMethodSet = defaultSafeMethodsSet
	}

	return policy
}

// retryResponseWriter captures the response status for retry decision
type retryResponseWriter struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
	header     http.Header
	written    bool
}

func newRetryResponseWriter() *retryResponseWriter {
	return &retryResponseWriter{
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (rw *retryResponseWriter) Header() http.Header {
	return rw.header
}

func (rw *retryResponseWriter) Write(b []byte) (int, error) {
	rw.written = true
	return rw.body.Write(b)
}

func (rw *retryResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.written = true
}

// flushTo writes the captured response to the actual ResponseWriter
func (rw *retryResponseWriter) flushTo(w http.ResponseWriter) {
	for key, values := range rw.header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(rw.statusCode)
	if rw.body.Len() > 0 {
		_, _ = w.Write(rw.body.Bytes())
	}
}

// reset clears the response writer for reuse
func (rw *retryResponseWriter) reset() {
	rw.header = make(http.Header)
	rw.statusCode = http.StatusOK
	rw.body.Reset()
	rw.written = false
}

// shouldRetry determines if a response warrants a retry based on the policy
func (p *RetryPolicy) shouldRetry(statusCode int, err error) bool {
	if err != nil {
		// Connection failures
		if p.RetryOn["connection-failure"] {
			return true
		}
		if p.RetryOn["reset"] {
			return true
		}
		if p.RetryOn["refused-stream"] {
			return true
		}
		return false
	}

	// Check status code conditions
	if p.RetryOn["5xx"] && statusCode >= 500 && statusCode < 600 {
		return true
	}

	return false
}

// isMethodRetryable checks if the HTTP method is eligible for retry
func (p *RetryPolicy) isMethodRetryable(method string) bool {
	return p.RetryMethodSet[strings.ToUpper(method)]
}

// forwardWithRetry performs backend forwarding with automatic retry support
func (r *Router) forwardWithRetry(
	_ *RouteEntry,
	w http.ResponseWriter,
	req *http.Request,
	retryPolicy *RetryPolicy,
	pool *upstream.Pool,
	clusterKey string,
	loadBalancer lb.LoadBalancer,
	hashLB interface{},
	span trace.Span,
	logger *zap.Logger,
) {
	// Get the per-cluster retry budget and track the active request
	budget := getClusterRetryBudget(clusterKey)
	budget.IncActiveRequests()
	defer budget.DecActiveRequests()

	// Check if method is retryable
	methodRetryable := retryPolicy.isMethodRetryable(req.Method)

	// Buffer the request body for retries (only if method is retryable).
	// Bodies larger than maxRetryBodySize skip retries to prevent OOM.
	var bodyBytes []byte
	if methodRetryable && req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(io.LimitReader(req.Body, maxRetryBodySize+1))
		if err != nil {
			logger.Error("Failed to read request body for retry", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if int64(len(bodyBytes)) > maxRetryBodySize {
			// Body exceeds limit — disable retries and forward once with
			// the already-read prefix plus the remainder of the stream.
			logger.Warn("Request body exceeds max retry buffer size, skipping retries",
				zap.Int64("maxRetryBodySize", maxRetryBodySize),
			)
			req.Body = io.NopCloser(io.MultiReader(bytes.NewReader(bodyBytes), req.Body))
			methodRetryable = false
			bodyBytes = nil
		} else {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
	}

	var excludedEndpoints []*pb.Endpoint
	var lastResponse *retryResponseWriter
	var retryCount int32

	for attempt := int32(0); attempt <= retryPolicy.MaxRetries; attempt++ {
		// Select endpoint (excluding previously failed ones)
		endpoint := selectEndpointExcluding(loadBalancer, hashLB, req, clusterKey, excludedEndpoints)
		if endpoint == nil {
			logger.Warn("No healthy endpoint available for retry",
				zap.String("cluster", clusterKey),
				zap.Int32("attempt", attempt),
			)
			if lastResponse != nil {
				lastResponse.flushTo(w)
				return
			}
			http.Error(w, "No healthy backend", http.StatusServiceUnavailable)
			return
		}

		endpointKey := formatEndpointKey(endpoint.Address, endpoint.Port)

		// Set retry count header on upstream request
		if attempt > 0 {
			req.Header.Set("X-Retry-Count", strconv.Itoa(int(attempt)))
		}

		// Reset body for retry
		if attempt > 0 && bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// Create response capture writer
		captureWriter := newRetryResponseWriter()

		// Track timing
		backendStart := time.Now()

		// Forward to backend
		err := pool.Forward(endpoint, req, captureWriter)

		backendDuration := time.Since(backendStart).Seconds()

		if err != nil {
			// Connection failure
			pool.RecordFailure(endpoint)
			metrics.RecordBackendRequest(clusterKey, endpointKey, "failure", backendDuration)
			excludedEndpoints = append(excludedEndpoints, endpoint)

			logger.Warn("Backend request failed, evaluating retry",
				zap.String("cluster", clusterKey),
				zap.String("endpoint", endpointKey),
				zap.Int32("attempt", attempt),
				zap.Error(err),
			)

			// Check if we should retry
			if !methodRetryable || attempt >= retryPolicy.MaxRetries {
				metrics.RetryExhausted.Inc()
				http.Error(w, "Backend error", http.StatusBadGateway)
				return
			}

			if !budget.AllowRetry() {
				logger.Warn("Retry budget exhausted",
					zap.String("cluster", clusterKey),
					zap.Int64("active_requests", budget.ActiveRequests()),
					zap.Int64("active_retries", budget.ActiveRetries()),
				)
				metrics.RetryBudgetExhausted.Inc()
				metrics.RetryExhausted.Inc()
				http.Error(w, "Backend error", http.StatusBadGateway)
				return
			}

			// Backoff before retry
			retryCount++
			budget.IncActiveRetries()
			defer budget.DecActiveRetries()
			metrics.RetryCount.Inc()
			backoff := retryPolicy.BackoffBase * time.Duration(math.Pow(2, float64(attempt)))
			select {
			case <-time.After(backoff):
			case <-req.Context().Done():
				http.Error(w, "Request Cancelled", http.StatusServiceUnavailable)
				return
			}
			continue
		}

		// Request succeeded at transport level, check status code
		pool.RecordSuccess(endpoint)
		metrics.RecordBackendRequest(clusterKey, endpointKey, "success", backendDuration)

		if retryPolicy.shouldRetry(captureWriter.statusCode, nil) && methodRetryable && attempt < retryPolicy.MaxRetries {
			// Response indicates a retryable condition
			excludedEndpoints = append(excludedEndpoints, endpoint)
			lastResponse = captureWriter

			if !budget.AllowRetry() {
				logger.Warn("Retry budget exhausted",
					zap.String("cluster", clusterKey),
					zap.Int64("active_requests", budget.ActiveRequests()),
					zap.Int64("active_retries", budget.ActiveRetries()),
				)
				captureWriter.flushTo(w)
				metrics.RetryBudgetExhausted.Inc()
				metrics.RetryExhausted.Inc()
				return
			}

			retryCount++
			budget.IncActiveRetries()
			defer budget.DecActiveRetries()
			metrics.RetryCount.Inc()

			logger.Warn("Retryable response, attempting retry",
				zap.String("cluster", clusterKey),
				zap.String("endpoint", endpointKey),
				zap.Int("status", captureWriter.statusCode),
				zap.Int32("attempt", attempt),
			)

			backoff := retryPolicy.BackoffBase * time.Duration(math.Pow(2, float64(attempt)))
			select {
			case <-time.After(backoff):
			case <-req.Context().Done():
				http.Error(w, "Request Cancelled", http.StatusServiceUnavailable)
				return
			}
			continue
		}

		// Success or non-retryable response
		if retryCount > 0 {
			metrics.RetrySuccess.Inc()
			span.SetAttributes(attribute.Int("novaedge.retry.count", int(retryCount)))
			span.AddEvent("retry_succeeded", trace.WithAttributes(
				attribute.Int("attempt", int(attempt)),
				attribute.String("endpoint", endpointKey),
			))
		}

		captureWriter.flushTo(w)
		return
	}

	// All retries exhausted
	metrics.RetryExhausted.Inc()
	if lastResponse != nil {
		lastResponse.flushTo(w)
	} else {
		http.Error(w, "Backend error", http.StatusBadGateway)
	}
}

// selectEndpointExcluding selects an endpoint while excluding previously failed ones
func selectEndpointExcluding(
	loadBalancer lb.LoadBalancer,
	hashLB interface{},
	req *http.Request,
	_ string,
	excluded []*pb.Endpoint,
) *pb.Endpoint {
	// Try up to 10 times to find a non-excluded endpoint
	for range 10 {
		var endpoint *pb.Endpoint

		if hashLB != nil {
			clientIP := req.RemoteAddr
			if idx := strings.LastIndex(clientIP, ":"); idx != -1 {
				clientIP = clientIP[:idx]
			}
			switch h := hashLB.(type) {
			case *lb.RingHash:
				// Add attempt suffix to get different hash result
				endpoint = h.Select(clientIP + fmt.Sprintf("-%d", len(excluded)))
			case *lb.Maglev:
				endpoint = h.Select(clientIP + fmt.Sprintf("-%d", len(excluded)))
			}
		} else if loadBalancer != nil {
			endpoint = loadBalancer.Select()
		}

		if endpoint == nil {
			return nil
		}

		// Check if excluded
		isExcluded := false
		for _, ex := range excluded {
			if ex.Address == endpoint.Address && ex.Port == endpoint.Port {
				isExcluded = true
				break
			}
		}
		if !isExcluded {
			return endpoint
		}
	}

	// If all are excluded, return any endpoint (better than nothing)
	if loadBalancer != nil {
		return loadBalancer.Select()
	}
	return nil
}

// retryMetricsOnce ensures retry metrics are registered once
var retryMetricsOnce atomic.Bool

func init() {
	// Metrics are registered in the metrics package
	_ = retryMetricsOnce.Load()
}
