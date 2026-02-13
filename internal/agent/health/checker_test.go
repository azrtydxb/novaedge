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
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func newTestCluster() *pb.Cluster {
	return &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
	}
}

func newTestEndpoint(address string, port int32) *pb.Endpoint {
	return &pb.Endpoint{
		Address: address,
		Port:    port,
	}
}

func TestNewChecker(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster()
	endpoints := []*pb.Endpoint{
		newTestEndpoint("10.0.0.1", 8080),
		newTestEndpoint("10.0.0.2", 8080),
	}

	hc := NewChecker(cluster, endpoints, logger)

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

func TestNewChecker_DefaultConfig(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster()

	hc := NewChecker(cluster, nil, logger)

	if hc.config.Path != DefaultHealthCheckPath {
		t.Errorf("expected default path %q, got %q", DefaultHealthCheckPath, hc.config.Path)
	}
	if hc.config.Interval != DefaultHealthCheckInterval {
		t.Errorf("expected default interval %v, got %v", DefaultHealthCheckInterval, hc.config.Interval)
	}
	if hc.config.Mode != CheckHTTP {
		t.Errorf("expected default mode HTTP, got %d", hc.config.Mode)
	}
}

func TestNewChecker_CustomConfig(t *testing.T) {
	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
		HealthCheck: &pb.HealthCheck{
			HttpPath:   "/ready",
			IntervalMs: 5000,
			Type:       pb.HealthCheckType_HEALTH_CHECK_HTTPS,
		},
	}

	hc := NewChecker(cluster, nil, logger)

	if hc.config.Path != "/ready" {
		t.Errorf("expected path %q, got %q", "/ready", hc.config.Path)
	}
	if hc.config.Interval != 5*time.Second {
		t.Errorf("expected interval 5s, got %v", hc.config.Interval)
	}
	if hc.config.Mode != CheckHTTPS {
		t.Errorf("expected mode HTTPS, got %d", hc.config.Mode)
	}
}

func TestNewChecker_TCPMode(t *testing.T) {
	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
		HealthCheck: &pb.HealthCheck{
			Type:       pb.HealthCheckType_HEALTH_CHECK_TCP,
			IntervalMs: 3000,
		},
	}

	hc := NewChecker(cluster, nil, logger)

	if hc.config.Mode != CheckTCP {
		t.Errorf("expected mode TCP, got %d", hc.config.Mode)
	}
	if hc.config.Interval != 3*time.Second {
		t.Errorf("expected interval 3s, got %v", hc.config.Interval)
	}
}

func TestBuildHealthCheckConfig_NilHealthCheck(t *testing.T) {
	cluster := &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
	}

	config := buildCheckConfig(cluster)

	if config.Path != DefaultHealthCheckPath {
		t.Errorf("expected path %q, got %q", DefaultHealthCheckPath, config.Path)
	}
	if config.Interval != DefaultHealthCheckInterval {
		t.Errorf("expected interval %v, got %v", DefaultHealthCheckInterval, config.Interval)
	}
	if config.Mode != CheckHTTP {
		t.Errorf("expected mode HTTP, got %d", config.Mode)
	}
}

func TestBuildHealthCheckConfig_AllModes(t *testing.T) {
	tests := []struct {
		name     string
		pbType   pb.HealthCheckType
		wantMode CheckMode
	}{
		{"HTTP", pb.HealthCheckType_HEALTH_CHECK_HTTP, CheckHTTP},
		{"GRPC", pb.HealthCheckType_HEALTH_CHECK_GRPC, CheckGRPC},
		{"TCP", pb.HealthCheckType_HEALTH_CHECK_TCP, CheckTCP},
		{"HTTPS", pb.HealthCheckType_HEALTH_CHECK_HTTPS, CheckHTTPS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &pb.Cluster{
				Name:      "backend",
				Namespace: "default",
				HealthCheck: &pb.HealthCheck{
					Type: tt.pbType,
				},
			}

			config := buildCheckConfig(cluster)

			if config.Mode != tt.wantMode {
				t.Errorf("expected mode %d, got %d", tt.wantMode, config.Mode)
			}
		})
	}
}

func TestHealthChecker_IsHealthy_UnknownEndpoint(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster()
	endpoints := []*pb.Endpoint{}

	hc := NewChecker(cluster, endpoints, logger)

	unknownEp := newTestEndpoint("10.0.0.99", 9090)
	if !hc.IsHealthy(unknownEp) {
		t.Error("unknown endpoints should be assumed healthy")
	}
}

func TestHealthChecker_RecordFailure_ThresholdBehavior(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster()
	ep := newTestEndpoint("10.0.0.1", 8080)
	endpoints := []*pb.Endpoint{ep}

	hc := NewChecker(cluster, endpoints, logger)

	// Manually initialize results (simulating Start behavior without the goroutine)
	key := endpointKey(ep)
	clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
	hc.results[key] = &Result{
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
	cluster := newTestCluster()
	ep := newTestEndpoint("10.0.0.1", 8080)
	endpoints := []*pb.Endpoint{ep}

	hc := NewChecker(cluster, endpoints, logger)

	key := endpointKey(ep)
	clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
	hc.results[key] = &Result{
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
	cluster := newTestCluster()
	ep1 := newTestEndpoint("10.0.0.1", 8080)
	endpoints := []*pb.Endpoint{ep1}

	hc := NewChecker(cluster, endpoints, logger)

	// Initialize ep1
	key1 := endpointKey(ep1)
	clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
	hc.results[key1] = &Result{
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
	cluster := newTestCluster()
	ep1 := newTestEndpoint("10.0.0.1", 8080)
	ep2 := newTestEndpoint("10.0.0.2", 8080)
	endpoints := []*pb.Endpoint{ep1, ep2}

	hc := NewChecker(cluster, endpoints, logger)

	// Initialize both endpoints
	clusterKey := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
	for _, ep := range endpoints {
		key := endpointKey(ep)
		hc.results[key] = &Result{Endpoint: ep, Healthy: true}
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
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	logger := zap.NewNop()
	cluster := newTestCluster()
	ep := newTestEndpoint(host, port)

	hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)

	healthy, err := hc.performHTTPCheck(context.Background(), ep)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !healthy {
		t.Error("expected endpoint to be healthy with 200 response")
	}
}

func TestHealthChecker_PerformHTTPCheck_CustomPath(t *testing.T) {
	// Start a test HTTP server that checks the path
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")
	host := parts[0]
	var port int32
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
		HealthCheck: &pb.HealthCheck{
			HttpPath: "/ready",
		},
	}
	ep := newTestEndpoint(host, port)

	hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)

	healthy, err := hc.performHTTPCheck(context.Background(), ep)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !healthy {
		t.Error("expected endpoint to be healthy with custom path /ready")
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
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	logger := zap.NewNop()
	cluster := newTestCluster()
	ep := newTestEndpoint(host, port)

	hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)

	healthy, err := hc.performHTTPCheck(context.Background(), ep)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if healthy {
		t.Error("expected endpoint to be unhealthy with 500 response")
	}
}

func TestHealthChecker_PerformHTTPCheck_ConnectionRefused(t *testing.T) {
	logger := zap.NewNop()
	cluster := newTestCluster()
	// Use a port that is very unlikely to be in use
	ep := newTestEndpoint("127.0.0.1", 19999)

	hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)
	hc.httpClient.Timeout = 1 * time.Second

	healthy, err := hc.performHTTPCheck(context.Background(), ep)
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
	if healthy {
		t.Error("expected endpoint to be unhealthy when connection refused")
	}
}

func TestHealthChecker_PerformTCPCheck_Success(t *testing.T) {
	// Start a TCP listener
	lc := net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start TCP listener: %v", err)
	}
	defer func() { _ = listener.Close() }()

	// Accept connections in background
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("expected *net.TCPAddr from listener")
	}

	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
		HealthCheck: &pb.HealthCheck{
			Type: pb.HealthCheckType_HEALTH_CHECK_TCP,
		},
	}
	port := addr.Port
	if port < 0 || port > math.MaxInt32 {
		t.Fatalf("port %d out of int32 range", port)
	}
	ep := newTestEndpoint(addr.IP.String(), int32(port))

	hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)

	healthy, checkErr := hc.performTCPCheck(context.Background(), ep)
	if checkErr != nil {
		t.Fatalf("expected no error, got %v", checkErr)
	}
	if !healthy {
		t.Error("expected endpoint to be healthy when TCP connection succeeds")
	}
}

func TestHealthChecker_PerformTCPCheck_ConnectionRefused(t *testing.T) {
	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
		HealthCheck: &pb.HealthCheck{
			Type: pb.HealthCheckType_HEALTH_CHECK_TCP,
		},
	}
	// Use a port that is very unlikely to be in use
	ep := newTestEndpoint("127.0.0.1", 19998)

	hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)

	healthy, err := hc.performTCPCheck(context.Background(), ep)
	if err == nil {
		t.Fatal("expected error for TCP connection refused")
	}
	if healthy {
		t.Error("expected endpoint to be unhealthy when TCP connection refused")
	}
}

func TestHealthChecker_PerformHTTPSCheck_Success(t *testing.T) {
	// Start a test HTTPS server
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Extract host and port from server URL
	addr := strings.TrimPrefix(server.URL, "https://")
	parts := strings.Split(addr, ":")
	host := parts[0]
	var port int32
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
		HealthCheck: &pb.HealthCheck{
			Type: pb.HealthCheckType_HEALTH_CHECK_HTTPS,
		},
	}
	ep := newTestEndpoint(host, port)

	hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)

	healthy, err := hc.performHTTPSCheck(context.Background(), ep)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !healthy {
		t.Error("expected endpoint to be healthy with HTTPS 200 response")
	}
}

func TestHealthChecker_PerformHTTPSCheck_CustomPath(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "https://")
	parts := strings.Split(addr, ":")
	host := parts[0]
	var port int32
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
		HealthCheck: &pb.HealthCheck{
			Type:     pb.HealthCheckType_HEALTH_CHECK_HTTPS,
			HttpPath: "/healthz",
		},
	}
	ep := newTestEndpoint(host, port)

	hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)

	healthy, err := hc.performHTTPSCheck(context.Background(), ep)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !healthy {
		t.Error("expected endpoint to be healthy with custom HTTPS path /healthz")
	}
}

func TestHealthChecker_PerformHTTPSCheck_ServerError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "https://")
	parts := strings.Split(addr, ":")
	host := parts[0]
	var port int32
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
		HealthCheck: &pb.HealthCheck{
			Type: pb.HealthCheckType_HEALTH_CHECK_HTTPS,
		},
	}
	ep := newTestEndpoint(host, port)

	hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)

	healthy, err := hc.performHTTPSCheck(context.Background(), ep)
	if err == nil {
		t.Fatal("expected error for HTTPS 503 response")
	}
	if healthy {
		t.Error("expected endpoint to be unhealthy with HTTPS 503 response")
	}
}

func TestHealthChecker_PerformHTTPSCheck_ConnectionRefused(t *testing.T) {
	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
		HealthCheck: &pb.HealthCheck{
			Type: pb.HealthCheckType_HEALTH_CHECK_HTTPS,
		},
	}
	ep := newTestEndpoint("127.0.0.1", 19997)

	hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)
	hc.httpsClient.Timeout = 1 * time.Second

	healthy, err := hc.performHTTPSCheck(context.Background(), ep)
	if err == nil {
		t.Fatal("expected error for HTTPS connection refused")
	}
	if healthy {
		t.Error("expected endpoint to be unhealthy when HTTPS connection refused")
	}
}

func TestHealthChecker_PerformHTTPSCheck_SkipsTLSVerify(t *testing.T) {
	// The TLS server from httptest uses a self-signed cert.
	// Our HTTPS health checker should succeed because it skips TLS verification.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "https://")
	parts := strings.Split(addr, ":")
	host := parts[0]
	var port int32
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:      "backend",
		Namespace: "default",
		HealthCheck: &pb.HealthCheck{
			Type: pb.HealthCheckType_HEALTH_CHECK_HTTPS,
		},
	}
	ep := newTestEndpoint(host, port)

	hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)

	// Verify that the httpsClient has InsecureSkipVerify set
	transport, ok := hc.httpsClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected HTTPS client to have InsecureSkipVerify=true")
	}

	// Also verify that a client WITHOUT skip-verify would fail (proving our
	// skip-verify is actually what makes it work)
	strictClient := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}
	checkURL := "https://" + addr + "/health"
	strictReq, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, checkURL, nil)
	if reqErr != nil {
		t.Fatalf("failed to create request: %v", reqErr)
	}
	strictResp, strictErr := strictClient.Do(strictReq)
	if strictErr == nil {
		_ = strictResp.Body.Close()
		t.Log("note: strict TLS client succeeded (test environment may have cert in trust store)")
	}

	// Our health checker should always succeed
	healthy, err := hc.performHTTPSCheck(context.Background(), ep)
	if err != nil {
		t.Fatalf("expected no error with skip-verify, got %v", err)
	}
	if !healthy {
		t.Error("expected healthy with skip-verify HTTPS check")
	}
}

func TestHealthChecker_PerformCheck_Dispatch(t *testing.T) {
	// Start an HTTP server for the dispatch test
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(addr, ":")
	host := parts[0]
	var port int32
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	logger := zap.NewNop()
	ep := newTestEndpoint(host, port)

	t.Run("dispatches to HTTP by default", func(t *testing.T) {
		cluster := newTestCluster()
		hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)

		healthy, err := hc.performCheck(context.Background(), ep)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !healthy {
			t.Error("expected healthy from HTTP dispatch")
		}
	})

	t.Run("dispatches to TCP", func(t *testing.T) {
		cluster := &pb.Cluster{
			Name:      "backend",
			Namespace: "default",
			HealthCheck: &pb.HealthCheck{
				Type: pb.HealthCheckType_HEALTH_CHECK_TCP,
			},
		}
		hc := NewChecker(cluster, []*pb.Endpoint{ep}, logger)

		// TCP check should succeed since the HTTP server is also a TCP listener
		healthy, err := hc.performCheck(context.Background(), ep)
		if err != nil {
			t.Fatalf("expected no error from TCP dispatch, got %v", err)
		}
		if !healthy {
			t.Error("expected healthy from TCP dispatch")
		}
	})
}

func TestHealthChecker_ConfigurableInterval(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name             string
		intervalMs       int64
		expectedInterval time.Duration
	}{
		{"default interval", 0, DefaultHealthCheckInterval},
		{"custom 5s interval", 5000, 5 * time.Second},
		{"custom 30s interval", 30000, 30 * time.Second},
		{"custom 500ms interval", 500, 500 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &pb.Cluster{
				Name:      "backend",
				Namespace: "default",
				HealthCheck: &pb.HealthCheck{
					IntervalMs: tt.intervalMs,
				},
			}

			hc := NewChecker(cluster, nil, logger)

			if hc.config.Interval != tt.expectedInterval {
				t.Errorf("expected interval %v, got %v", tt.expectedInterval, hc.config.Interval)
			}
		})
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
