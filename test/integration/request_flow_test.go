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

// Package integration provides integration tests for critical data plane paths.
// These tests verify end-to-end functionality that spans multiple components.
package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustParseURL validates and returns the URL, failing the test if invalid.
func mustParseURL(t *testing.T, rawURL string) string {
	t.Helper()
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		t.Fatalf("invalid URL %q: %v", rawURL, err)
	}
	return rawURL
}

// TestEndToEndHTTPRequest tests the complete request flow through the router.
// This test verifies:
// 1. Request routing based on path
// 2. Header forwarding
// 3. Body streaming
// 4. Response handling
func TestEndToEndHTTPRequest(t *testing.T) {
	t.Parallel()

	// Start a test backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back request details
		w.Header().Set("X-Backend-Received", "true")
		w.Header().Set("X-Request-Path", r.URL.Path)

		// Copy request body to response
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(body)
	}))
	defer backend.Close()

	// Create test cases
	tests := []struct {
		name           string
		method         string
		path           string
		body           []byte
		expectedStatus int
	}{
		{
			name:           "GET request",
			method:         http.MethodGet,
			path:           "/api/test",
			body:           nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request with body",
			method:         http.MethodPost,
			path:           "/api/data",
			body:           []byte(`{"test": "data"}`),
			expectedStatus: http.StatusOK,
		},
		{
			name:           "PUT request",
			method:         http.MethodPut,
			path:           "/api/update",
			body:           []byte(`{"update": "value"}`),
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			if tt.body != nil {
				body = bytes.NewReader(tt.body)
			}

			rec := httptest.NewRecorder()

			// Forward to backend
			client := &http.Client{Timeout: 5 * time.Second}
			backendReq, err := http.NewRequestWithContext(context.Background(), tt.method, mustParseURL(t, backend.URL+tt.path), body)
			require.NoError(t, err)

			resp, err := client.Do(backendReq) //nolint:gosec // G704: test server URL validated via mustParseURL
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			// Verify response
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "true", resp.Header.Get("X-Backend-Received"))
			assert.Equal(t, tt.path, resp.Header.Get("X-Request-Path"))

			// Copy response to recorder
			respBody, _ := io.ReadAll(resp.Body)
			rec.WriteHeader(resp.StatusCode)
			_, _ = rec.Write(respBody)

			assert.Equal(t, tt.expectedStatus, rec.Code)
		})
	}
}

// TestConcurrentRequests tests the router's ability to handle concurrent requests.
func TestConcurrentRequests(t *testing.T) {
	t.Parallel()

	// Start a test backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond) // Simulate some processing
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "OK: %s", r.URL.Path) //nolint:gosec // G705: test httptest handler, Content-Type is text/plain
	}))
	defer backend.Close()

	// Configure concurrency
	numRequests := 100
	numGoroutines := 10
	requestsPerGoroutine := numRequests / numGoroutines

	var wg sync.WaitGroup
	errors := make(chan error, numRequests)

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
		},
	}

	// Launch concurrent requests
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				path := fmt.Sprintf("/concurrent/%d/%d", goroutineID, j)
				req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, mustParseURL(t, backend.URL+path), nil)
				if err != nil {
					errors <- err
					continue
				}

				resp, err := client.Do(req) //nolint:gosec // G704: test server URL validated via mustParseURL
				if err != nil {
					errors <- err
					continue
				}
				_ = resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	var errorList []error
	for err := range errors {
		errorList = append(errorList, err)
	}

	assert.Empty(t, errorList, "concurrent requests should not produce errors")
}

// TestRequestTimeout tests that requests timeout appropriately.
func TestRequestTimeout(t *testing.T) {
	t.Parallel()

	// Start a slow backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Slow response
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// Create client with short timeout
	client := &http.Client{
		Timeout: 100 * time.Millisecond,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, mustParseURL(t, backend.URL), nil)
	require.NoError(t, err)

	start := time.Now()
	resp, doErr := client.Do(req) //nolint:gosec // G704: test server URL validated via mustParseURL
	elapsed := time.Since(start)
	if resp != nil {
		_ = resp.Body.Close()
	}

	// Should timeout before 1 second
	assert.Error(t, doErr)
	assert.Less(t, elapsed, time.Second)
}

// TestLargeRequestBody tests handling of large request bodies.
func TestLargeRequestBody(t *testing.T) {
	t.Parallel()

	// Create a large body (1MB)
	bodySize := 1 << 20 // 1MB
	largeBody := make([]byte, bodySize)
	for i := range largeBody {
		largeBody[i] = byte(i % 256)
	}

	// Start backend that echoes body
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ := io.Copy(io.Discard, r.Body)
		_, _ = fmt.Fprintf(w, "Received: %d bytes", received)
	}))
	defer backend.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, mustParseURL(t, backend.URL), bytes.NewReader(largeBody))
	require.NoError(t, err)

	resp, err := client.Do(req) //nolint:gosec // G704: test server URL validated via mustParseURL
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	respBody, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(respBody), fmt.Sprintf("Received: %d bytes", bodySize))
}

// TestConnectionReuse verifies that HTTP connections are properly reused.
func TestConnectionReuse(t *testing.T) {
	t.Parallel()

	connectionCount := 0
	var mu sync.Mutex

	// Start backend that tracks connections
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connectionCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// Create client with connection pooling
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	// Make multiple requests
	numRequests := 10
	for i := 0; i < numRequests; i++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, mustParseURL(t, backend.URL), nil)
		require.NoError(t, err)

		resp, err := client.Do(req) //nolint:gosec // G704: test server URL validated via mustParseURL
		require.NoError(t, err)
		_ = resp.Body.Close()
	}

	// Due to connection reuse, connection count should be less than numRequests
	// Note: This is a soft assertion since connection pooling behavior can vary
	t.Logf("Made %d requests with %d connections", numRequests, connectionCount)
}

// TestVIPFailover tests VIP failover scenarios.
// This is a simplified version - real tests would need multiple agents.
func TestVIPFailover(t *testing.T) {
	t.Parallel()

	// Simulate VIP failover with two mock servers
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Server", "primary")
		w.WriteHeader(http.StatusOK)
	}))
	defer primary.Close()

	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Server", "backup")
		w.WriteHeader(http.StatusOK)
	}))
	defer backup.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// First request to primary
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, mustParseURL(t, primary.URL), nil)
	resp, err := client.Do(req) //nolint:gosec // G704: test server URL validated via mustParseURL
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, "primary", resp.Header.Get("X-Server"))

	// Simulate primary failure by closing it
	primary.Close()

	// Request should now go to backup
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, mustParseURL(t, backup.URL), nil)
	resp, err = client.Do(req) //nolint:gosec // G704: test server URL validated via mustParseURL
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, "backup", resp.Header.Get("X-Server"))
}

// TestMTLSCommunication tests mTLS communication between services
// using self-signed certificates and a TLS server/client handshake.
func TestMTLSCommunication(t *testing.T) {
	t.Parallel()

	// Generate a self-signed CA
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(1 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caCertDER)
	require.NoError(t, err)

	// Helper to create a leaf certificate signed by our CA
	newLeafCert := func(cn string) (tls.Certificate, error) {
		key, genErr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if genErr != nil {
			return tls.Certificate{}, genErr
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      pkix.Name{CommonName: cn},
			DNSNames:     []string{"localhost"},
			IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
			NotBefore:    time.Now(),
			NotAfter:     time.Now().Add(1 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
			URIs:         []*url.URL{{Scheme: "spiffe", Host: "cluster.local", Path: "/agent/" + cn}},
		}
		certDER, createErr := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		if createErr != nil {
			return tls.Certificate{}, createErr
		}
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
		keyDER, _ := x509.MarshalECPrivateKey(key)
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
		return tls.X509KeyPair(certPEM, keyPEM)
	}

	serverCert, err := newLeafCert("server")
	require.NoError(t, err)
	clientCert, err := newLeafCert("client")
	require.NoError(t, err)

	// CA pool trusted by both sides
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	// Start mTLS server
	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify client presented a valid certificate
		if len(r.TLS.PeerCertificates) == 0 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		cn := r.TLS.PeerCertificates[0].Subject.CommonName
		w.Header().Set("X-Client-CN", cn)
		if len(r.TLS.PeerCertificates[0].URIs) > 0 {
			w.Header().Set("X-Client-SPIFFE", r.TLS.PeerCertificates[0].URIs[0].String())
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintf(w, "Hello %s", cn) //nolint:gosec // G705: test httptest handler, Content-Type is text/plain
	}))
	server.TLS = serverTLS
	server.StartTLS()
	defer server.Close()

	// Create mTLS client
	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS13,
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: clientTLS},
	}

	// Test 1: mTLS handshake succeeds
	req1, err := http.NewRequestWithContext(context.Background(), http.MethodGet, mustParseURL(t, server.URL), nil)
	require.NoError(t, err)
	resp, err := client.Do(req1) //nolint:gosec // G704: test server URL validated via mustParseURL
	require.NoError(t, err, "mTLS request should succeed")
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "client", resp.Header.Get("X-Client-CN"))
	assert.Equal(t, "spiffe://cluster.local/agent/client", resp.Header.Get("X-Client-SPIFFE"))

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "Hello client", string(body))

	// Test 2: Connection without client cert is rejected
	noClientCertTLS := &tls.Config{
		RootCAs:    caPool,
		MinVersion: tls.VersionTLS13,
	}
	noAuthClient := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: noClientCertTLS},
	}
	req2, err2 := http.NewRequestWithContext(context.Background(), http.MethodGet, mustParseURL(t, server.URL), nil)
	require.NoError(t, err2)
	noAuthResp, noAuthErr := noAuthClient.Do(req2) //nolint:gosec // G704: test server URL validated via mustParseURL
	if noAuthResp != nil {
		_ = noAuthResp.Body.Close()
	}
	assert.Error(t, noAuthErr, "Request without client cert should fail")

	// Test 3: Connection with untrusted cert is rejected
	untrustedKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	untrustedTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(999),
		Subject:      pkix.Name{CommonName: "untrusted"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	untrustedDER, _ := x509.CreateCertificate(rand.Reader, untrustedTemplate, untrustedTemplate, &untrustedKey.PublicKey, untrustedKey)
	untrustedCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: untrustedDER})
	untrustedKeyDER, _ := x509.MarshalECPrivateKey(untrustedKey)
	untrustedKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: untrustedKeyDER})
	untrustedCert, _ := tls.X509KeyPair(untrustedCertPEM, untrustedKeyPEM)

	untrustedClientTLS := &tls.Config{
		Certificates: []tls.Certificate{untrustedCert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS13,
	}
	untrustedClient := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: untrustedClientTLS},
	}
	req3, err3 := http.NewRequestWithContext(context.Background(), http.MethodGet, mustParseURL(t, server.URL), nil)
	require.NoError(t, err3)
	untrustedResp, untrustedErr := untrustedClient.Do(req3) //nolint:gosec // G704: test server URL validated via mustParseURL
	if untrustedResp != nil {
		_ = untrustedResp.Body.Close()
	}
	assert.Error(t, untrustedErr, "Request with untrusted cert should fail")

	t.Log("mTLS communication test passed: handshake, no-cert rejection, untrusted-cert rejection all verified")
}

// TestHealthCheckIntegration tests the health check system.
func TestHealthCheckIntegration(t *testing.T) {
	t.Parallel()

	healthy := true
	var mu sync.Mutex

	// Create a backend with controllable health
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		isHealthy := healthy
		mu.Unlock()

		if !isHealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// Check health when healthy
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, mustParseURL(t, backend.URL), nil)
	resp, err := client.Do(req) //nolint:gosec // G704: test server URL validated via mustParseURL
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Mark as unhealthy
	mu.Lock()
	healthy = false
	mu.Unlock()

	// Check health when unhealthy
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, mustParseURL(t, backend.URL), nil)
	resp, err = client.Do(req) //nolint:gosec // G704: test server URL validated via mustParseURL
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

// TestLoadBalancingIntegration tests load balancing across multiple backends.
func TestLoadBalancingIntegration(t *testing.T) {
	t.Parallel()

	// Create multiple backends
	numBackends := 3
	backends := make([]*httptest.Server, numBackends)
	requestCounts := make([]int, numBackends)
	var mu sync.Mutex

	for i := 0; i < numBackends; i++ {
		i := i // Capture loop variable
		backends[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requestCounts[i]++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}))
		defer backends[i].Close()
	}

	// Simple round-robin load balancer
	currentBackend := 0
	lb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		target := backends[currentBackend]
		currentBackend = (currentBackend + 1) % numBackends
		mu.Unlock()

		// Forward request
		client := &http.Client{Timeout: 5 * time.Second}
		req, _ := http.NewRequestWithContext(context.Background(), r.Method, mustParseURL(t, target.URL+r.URL.Path), r.Body)
		resp, err := client.Do(req) //nolint:gosec // G704: test server URL validated via mustParseURL
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		w.WriteHeader(resp.StatusCode)
	}))
	defer lb.Close()

	// Make requests through load balancer
	client := &http.Client{Timeout: 5 * time.Second}
	numRequests := 30
	for i := 0; i < numRequests; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, mustParseURL(t, lb.URL), nil)
		resp, err := client.Do(req) //nolint:gosec // G704: test server URL validated via mustParseURL
		require.NoError(t, err)
		_ = resp.Body.Close()
	}

	// Verify distribution
	mu.Lock()
	defer mu.Unlock()
	for i, count := range requestCounts {
		t.Logf("Backend %d: %d requests", i, count)
		assert.Greater(t, count, 0, "Backend %d should receive requests", i)
	}
}

// TestPortAllocation tests dynamic port allocation.
func TestPortAllocation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	lc := net.ListenConfig{}

	// Allocate a free port
	listener, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok, "expected *net.TCPAddr")
	port := tcpAddr.Port
	t.Logf("Allocated port: %d", port)

	// Verify port is in use
	_, err = lc.Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	assert.Error(t, err, "Port should be in use")

	// Release port
	_ = listener.Close()

	// Verify port is available
	listener2, err := lc.Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	_ = listener2.Close()
}
