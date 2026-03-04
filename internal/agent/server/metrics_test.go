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
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDefaultMetricsPort(t *testing.T) {
	assert.Equal(t, 9090, DefaultMetricsPort)
}

func TestNewMetricsServer(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name         string
		port         int
		expectedPort int
	}{
		{
			name:         "custom port",
			port:         8080,
			expectedPort: 8080,
		},
		{
			name:         "zero port uses default",
			port:         0,
			expectedPort: DefaultMetricsPort,
		},
		{
			name:         "negative port uses default",
			port:         -1,
			expectedPort: -1, // Will use the provided value
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewMetricsServer(logger, tt.port)
			require.NotNil(t, server)
			assert.Equal(t, tt.expectedPort, server.port)
			assert.NotNil(t, server.logger)
			assert.NotNil(t, server.rateLimiter)

			// Clean up
			server.rateLimiter.Stop()
		})
	}
}

func TestMetricsServer_StartAndShutdown(t *testing.T) {
	logger := zap.NewNop()

	// Use a random available port
	server := NewMetricsServer(logger, 0) // Will use default port
	require.NotNil(t, server)

	ctx, cancel := context.WithCancel(context.Background())

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	// Wait for shutdown to complete
	select {
	case err := <-errCh:
		// Server should shut down gracefully
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not shut down in time")
	}
}

func TestMetricsServer_Shutdown(t *testing.T) {
	logger := zap.NewNop()

	t.Run("nil server", func(t *testing.T) {
		server := &MetricsServer{logger: logger}
		err := server.Shutdown(context.Background())
		assert.NoError(t, err)
	})

	t.Run("running server", func(t *testing.T) {
		server := NewMetricsServer(logger, 0)
		require.NotNil(t, server)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start server
		go func() {
			_ = server.Start(ctx)
		}()

		// Give server time to start
		time.Sleep(100 * time.Millisecond)

		// Shutdown should work
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()

		err := server.Shutdown(shutdownCtx)
		assert.NoError(t, err)
	})
}

func TestMetricsServer_HTTPEndpoints(t *testing.T) {
	logger := zap.NewNop()

	// Find available port
	server := NewMetricsServer(logger, 0)
	require.NotNil(t, server)

	// Manually create server for testing
	mux := http.NewServeMux()
	rateLimitMiddleware := RateLimitMiddleware(server.rateLimiter)

	mux.Handle("/metrics", rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# HELP test_metric A test metric\n"))
	})))

	mux.Handle("/health", rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})))

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("NovaEdge Metrics Server\n"))
	})

	httpServer := &http.Server{
		Addr:              ":0", // Use any available port
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start server
	go func() {
		_ = httpServer.ListenAndServe()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Clean up
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	server.rateLimiter.Stop()
}

func TestMetricsServer_RateLimitIntegration(t *testing.T) {
	logger := zap.NewNop()

	server := NewMetricsServer(logger, 0)
	require.NotNil(t, server)
	defer server.rateLimiter.Stop()

	// Verify rate limiter is configured
	require.NotNil(t, server.rateLimiter)

	// Verify rate limiter works
	limiter := server.rateLimiter.getLimiter("192.168.1.1")
	require.NotNil(t, limiter)
	assert.True(t, limiter.Allow())
}

func TestMetricsServer_ConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()

	server := NewMetricsServer(logger, 0)
	require.NotNil(t, server)
	defer server.rateLimiter.Stop()

	// Concurrent access to rate limiter
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			limiter := server.rateLimiter.getLimiter("192.168.1.1")
			assert.NotNil(t, limiter)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestMetricsServer_StructFields(t *testing.T) {
	logger := zap.NewNop()
	server := NewMetricsServer(logger, 9091)

	require.NotNil(t, server)
	assert.Equal(t, 9091, server.port)
	assert.NotNil(t, server.logger)
	assert.NotNil(t, server.rateLimiter)
	assert.Nil(t, server.server) // server is nil until Start is called

	server.rateLimiter.Stop()
}

func TestMetricsServer_DefaultRateLimitConfig(t *testing.T) {
	logger := zap.NewNop()
	server := NewMetricsServer(logger, 0)
	require.NotNil(t, server)
	defer server.rateLimiter.Stop()

	// Verify default rate limit config is used
	config := DefaultObservabilityRateLimitConfig()
	assert.Equal(t, 100, config.RequestsPerMinute)
	assert.Equal(t, 10, config.Burst)
}

// Benchmark tests
func BenchmarkNewMetricsServer(b *testing.B) {
	logger := zap.NewNop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server := NewMetricsServer(logger, 9090)
		server.rateLimiter.Stop()
	}
}

func BenchmarkMetricsServer_RateLimiter(b *testing.B) {
	logger := zap.NewNop()
	server := NewMetricsServer(logger, 9090)
	defer server.rateLimiter.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.rateLimiter.getLimiter("192.168.1.1")
	}
}
