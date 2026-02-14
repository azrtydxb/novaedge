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
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestServerConstants(t *testing.T) {
	// Verify server timeout constants are reasonable
	if ServerReadTimeout != 30*time.Second {
		t.Errorf("ServerReadTimeout = %v, want 30s", ServerReadTimeout)
	}
	if ServerWriteTimeout != 30*time.Second {
		t.Errorf("ServerWriteTimeout = %v, want 30s", ServerWriteTimeout)
	}
	if ServerIdleTimeout != 120*time.Second {
		t.Errorf("ServerIdleTimeout = %v, want 120s", ServerIdleTimeout)
	}
	if ServerReadHeaderTimeout != 10*time.Second {
		t.Errorf("ServerReadHeaderTimeout = %v, want 10s", ServerReadHeaderTimeout)
	}
	if MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %v, want 1MB", MaxHeaderBytes)
	}
	if GracefulShutdownTimeout != 5*time.Second {
		t.Errorf("GracefulShutdownTimeout = %v, want 5s", GracefulShutdownTimeout)
	}
}

func TestMetricsServerConstants(t *testing.T) {
	if MetricsServerReadTimeout != 10*time.Second {
		t.Errorf("MetricsServerReadTimeout = %v, want 10s", MetricsServerReadTimeout)
	}
	if MetricsServerWriteTimeout != 10*time.Second {
		t.Errorf("MetricsServerWriteTimeout = %v, want 10s", MetricsServerWriteTimeout)
	}
	if MetricsServerIdleTimeout != 60*time.Second {
		t.Errorf("MetricsServerIdleTimeout = %v, want 60s", MetricsServerIdleTimeout)
	}
	if DefaultMetricsPort != 9090 {
		t.Errorf("DefaultMetricsPort = %v, want 9090", DefaultMetricsPort)
	}
}

func TestHTTP3Constants(t *testing.T) {
	if HTTP3DefaultMaxIdleTimeout != 30*time.Second {
		t.Errorf("HTTP3DefaultMaxIdleTimeout = %v, want 30s", HTTP3DefaultMaxIdleTimeout)
	}
	if HTTP3DefaultMaxBiStreams != 100 {
		t.Errorf("HTTP3DefaultMaxBiStreams = %v, want 100", HTTP3DefaultMaxBiStreams)
	}
	if HTTP3DefaultMaxUniStreams != 100 {
		t.Errorf("HTTP3DefaultMaxUniStreams = %v, want 100", HTTP3DefaultMaxUniStreams)
	}
	if HTTP3AltSvcMaxAge != 2592000 {
		t.Errorf("HTTP3AltSvcMaxAge = %v, want 2592000", HTTP3AltSvcMaxAge)
	}
}

func TestNewMetricsServer(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name     string
		port     int
		expected int
	}{
		{"default port", 0, DefaultMetricsPort},
		{"custom port", 8080, 8080},
		{"another custom port", 9999, 9999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := NewMetricsServer(logger, tt.port)
			if ms == nil {
				t.Fatal("NewMetricsServer returned nil")
			}
			if ms.port != tt.expected {
				t.Errorf("port = %d, want %d", ms.port, tt.expected)
			}
			if ms.logger == nil {
				t.Error("logger should not be nil")
			}
			if ms.rateLimiter == nil {
				t.Error("rateLimiter should not be nil")
			}
		})
	}
}

func TestNewMetricsServer_NilLogger(t *testing.T) {
	// Should not panic with nil logger
	ms := NewMetricsServer(nil, 9090)
	if ms == nil {
		t.Fatal("NewMetricsServer returned nil")
	}
}

func TestMetricsServer_Shutdown_NilServer(t *testing.T) {
	logger := zap.NewNop()
	ms := NewMetricsServer(logger, 9090)
	// server is nil before Start is called

	ctx := context.Background()
	err := ms.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() with nil server should return nil, got %v", err)
	}
}
