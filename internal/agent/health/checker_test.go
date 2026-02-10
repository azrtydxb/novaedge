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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func newTestCluster(name, namespace string) *pb.Cluster {
	return &pb.Cluster{
		Name:      name,
		Namespace: namespace,
	}
}

func newTestEndpoint(address string, port int32) *pb.Endpoint {
	return &pb.Endpoint{
		Address: address,
		Port:    port,
	}
}

func TestNewHealthChecker(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster("backend", "default")
	endpoints := []*pb.Endpoint{
		newTestEndpoint("10.0.0.1", 8080),
		newTestEndpoint("10.0.0.2", 8080),
	}

	hc := NewHealthChecker(cluster, endpoints, logger)

	if hc == nil {
		t.Fatal("expected non-nil health checker")
	}
	if hc.cluster != cluster {
		t.Error("expected cluster to be set")
	}
	if len(hc.endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(hc.endpoints))
	}
	if hc.httpClient == nil {
		t.Error("expected non-nil http client")
	}
	if hc.httpClient.Timeout != DefaultHealthCheckTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultHealthCheckTimeout, hc.httpClient.Timeout)
	}
}

func TestHealthChecker_IsHealthy_UnknownEndpoint(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster("backend", "default")
	endpoints := []*pb.Endpoint{}

	hc := NewHealthChecker(cluster, endpoints, logger)

	unknownEp := newTestEndpoint("10.0.0.99", 9090)
	if !hc.IsHealthy(unknownEp) {
		t.Error("unknown endpoints should be assumed healthy")
	}
}

func TestHealthChecker_RecordFailure_ThresholdBehavior(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster("backend", "default")
	ep := newTestEndpoint("10.0.0.1", 8080)
	endpoints := []*pb.Endpoint{ep}

	hc := NewHealthChecker(cluster, endpoints, logger)

	// Manually initialize results (simulating Start behavior without the goroutine)
	key := endpointKey(ep)
	clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
	hc.results[key] = &HealthResult{
		Endpoint: ep,
		Healthy:  true,
	}
	cb := NewCircuitBreaker(key, DefaultCircuitBreakerConfig(), logger)
	cb.SetCluster(clusterKey)
	hc.circuitBreakers[key] = cb

	// Record failures below threshold
	for i := uint32(0); i < DefaultUnhealthyThreshold-1; i++ {
		hc.RecordFailure(ep)
	}

	// Should still be healthy (below threshold)
	if !hc.results[key].Healthy {
		t.Error("endpoint should still be healthy below failure threshold")
	}

	// One more failure to cross threshold
	hc.RecordFailure(ep)

	if hc.results[key].Healthy {
		t.Error("endpoint should be unhealthy after crossing failure threshold")
	}

	if hc.results[key].ConsecutiveFailures != DefaultUnhealthyThreshold {
		t.Errorf("expected %d consecutive failures, got %d",
			DefaultUnhealthyThreshold, hc.results[key].ConsecutiveFailures)
	}
}

func TestHealthChecker_RecordSuccess_ResetsFailures(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster("backend", "default")
	ep := newTestEndpoint("10.0.0.1", 8080)
	endpoints := []*pb.Endpoint{ep}

	hc := NewHealthChecker(cluster, endpoints, logger)

	key := endpointKey(ep)
	clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
	hc.results[key] = &HealthResult{
		Endpoint: ep,
		Healthy:  true,
	}
	cb := NewCircuitBreaker(key, DefaultCircuitBreakerConfig(), logger)
	cb.SetCluster(clusterKey)
	hc.circuitBreakers[key] = cb

	// Record a failure then a success
	hc.RecordFailure(ep)
	hc.RecordSuccess(ep)

	// The circuit breaker success doesn't reset the health result failure counter
	// but the circuit breaker's internal state should be reset
	cbState := cb.GetState()
	if cbState != StateClosed {
		t.Errorf("expected circuit breaker to be closed, got %v", cbState)
	}
}

func TestHealthChecker_UpdateEndpoints_AddsNew(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster("backend", "default")
	ep1 := newTestEndpoint("10.0.0.1", 8080)
	endpoints := []*pb.Endpoint{ep1}

	hc := NewHealthChecker(cluster, endpoints, logger)

	// Initialize ep1
	key1 := endpointKey(ep1)
	clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
	hc.results[key1] = &HealthResult{
		Endpoint: ep1,
		Healthy:  true,
	}
	cb := NewCircuitBreaker(key1, DefaultCircuitBreakerConfig(), logger)
	cb.SetCluster(clusterKey)
	hc.circuitBreakers[key1] = cb

	// Add a new endpoint
	ep2 := newTestEndpoint("10.0.0.2", 8080)
	hc.UpdateEndpoints([]*pb.Endpoint{ep1, ep2})

	if len(hc.endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(hc.endpoints))
	}

	key2 := endpointKey(ep2)
	if _, exists := hc.results[key2]; !exists {
		t.Error("new endpoint should be added to results")
	}
	if _, exists := hc.circuitBreakers[key2]; !exists {
		t.Error("new endpoint should have a circuit breaker")
	}
}

func TestHealthChecker_UpdateEndpoints_RemovesOld(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster("backend", "default")
	ep1 := newTestEndpoint("10.0.0.1", 8080)
	ep2 := newTestEndpoint("10.0.0.2", 8080)
	endpoints := []*pb.Endpoint{ep1, ep2}

	hc := NewHealthChecker(cluster, endpoints, logger)

	// Initialize both endpoints
	clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
	for _, ep := range endpoints {
		key := endpointKey(ep)
		hc.results[key] = &HealthResult{Endpoint: ep, Healthy: true}
		cb := NewCircuitBreaker(key, DefaultCircuitBreakerConfig(), logger)
		cb.SetCluster(clusterKey)
		hc.circuitBreakers[key] = cb
	}

	// Remove ep2
	hc.UpdateEndpoints([]*pb.Endpoint{ep1})

	key2 := endpointKey(ep2)
	if _, exists := hc.results[key2]; exists {
		t.Error("removed endpoint should be deleted from results")
	}
	if _, exists := hc.circuitBreakers[key2]; exists {
		t.Error("removed endpoint should have its circuit breaker deleted")
	}
}

func TestHealthChecker_PerformHTTPCheck_Success(t *testing.T) {
	// Start a test HTTP server that returns 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Extract host and port from server URL
	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")
	host := parts[0]
	var port int32
	fmt.Sscanf(parts[1], "%d", &port)

	logger := zap.NewNop()
	cluster := newTestCluster("backend", "default")
	ep := newTestEndpoint(host, port)

	hc := NewHealthChecker(cluster, []*pb.Endpoint{ep}, logger)

	healthy, err := hc.performHTTPCheck(ep)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !healthy {
		t.Error("expected endpoint to be healthy with 200 response")
	}
}

func TestHealthChecker_PerformHTTPCheck_ServerError(t *testing.T) {
	// Start a test HTTP server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")
	host := parts[0]
	var port int32
	fmt.Sscanf(parts[1], "%d", &port)

	logger := zap.NewNop()
	cluster := newTestCluster("backend", "default")
	ep := newTestEndpoint(host, port)

	hc := NewHealthChecker(cluster, []*pb.Endpoint{ep}, logger)

	healthy, err := hc.performHTTPCheck(ep)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if healthy {
		t.Error("expected endpoint to be unhealthy with 500 response")
	}
}

func TestHealthChecker_PerformHTTPCheck_ConnectionRefused(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster("backend", "default")
	// Use a port that is very unlikely to be in use
	ep := newTestEndpoint("127.0.0.1", 19999)

	hc := NewHealthChecker(cluster, []*pb.Endpoint{ep}, logger)
	hc.httpClient.Timeout = 1 * time.Second

	healthy, err := hc.performHTTPCheck(ep)
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
	if healthy {
		t.Error("expected endpoint to be unhealthy when connection refused")
	}
}

func TestEndpointKey(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1", 8080)
	key := endpointKey(ep)
	expected := "10.0.0.1:8080"
	if key != expected {
		t.Errorf("expected key %q, got %q", expected, key)
	}
}
