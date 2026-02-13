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

package metrics

import (
	"context"
	"testing"
	"time"
)

func TestOTelConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  OTelConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "disabled config is always valid",
			config: OTelConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid gRPC config",
			config: OTelConfig{
				Enabled:  true,
				Endpoint: "localhost:4317",
				Protocol: ProtocolGRPC,
			},
			wantErr: false,
		},
		{
			name: "valid HTTP config",
			config: OTelConfig{
				Enabled:  true,
				Endpoint: "http://localhost:4318/v1/metrics",
				Protocol: ProtocolHTTP,
			},
			wantErr: false,
		},
		{
			name: "empty endpoint",
			config: OTelConfig{
				Enabled:  true,
				Endpoint: "",
				Protocol: ProtocolGRPC,
			},
			wantErr: true,
			errMsg:  "endpoint must not be empty",
		},
		{
			name: "invalid protocol",
			config: OTelConfig{
				Enabled:  true,
				Endpoint: "localhost:4317",
				Protocol: "udp",
			},
			wantErr: true,
			errMsg:  "protocol must be",
		},
		{
			name: "negative export interval",
			config: OTelConfig{
				Enabled:        true,
				Endpoint:       "localhost:4317",
				Protocol:       ProtocolGRPC,
				ExportInterval: -1 * time.Second,
			},
			wantErr: true,
			errMsg:  "export interval must not be negative",
		},
		{
			name: "gRPC endpoint too short",
			config: OTelConfig{
				Enabled:  true,
				Endpoint: "ab",
				Protocol: ProtocolGRPC,
			},
			wantErr: true,
			errMsg:  "not a valid host:port",
		},
		{
			name: "valid config with resource attributes",
			config: OTelConfig{
				Enabled:        true,
				Endpoint:       "collector.monitoring:4317",
				Protocol:       ProtocolGRPC,
				ExportInterval: 15 * time.Second,
				ResourceAttributes: map[string]string{
					"service.version":        "1.0.0",
					"deployment.environment": "production",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" {
					if !containsSubstring(err.Error(), tt.errMsg) {
						t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
					}
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestOTelConfigDefaults(t *testing.T) {
	cfg := OTelConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
	}
	result := cfg.withDefaults()

	if result.Protocol != ProtocolGRPC {
		t.Errorf("expected default protocol %q, got %q", ProtocolGRPC, result.Protocol)
	}
	if result.ExportInterval != DefaultExportInterval {
		t.Errorf("expected default export interval %v, got %v", DefaultExportInterval, result.ExportInterval)
	}
}

func TestOTelConfigDefaultsPreservesExplicit(t *testing.T) {
	cfg := OTelConfig{
		Enabled:        true,
		Endpoint:       "localhost:4318",
		Protocol:       ProtocolHTTP,
		ExportInterval: 10 * time.Second,
	}
	result := cfg.withDefaults()

	if result.Protocol != ProtocolHTTP {
		t.Errorf("expected protocol %q to be preserved, got %q", ProtocolHTTP, result.Protocol)
	}
	if result.ExportInterval != 10*time.Second {
		t.Errorf("expected export interval 10s to be preserved, got %v", result.ExportInterval)
	}
}

func TestNewOTelExporterValidConfig(t *testing.T) {
	config := OTelConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
		Protocol: ProtocolGRPC,
	}

	exporter, err := NewOTelExporter(config)
	if err != nil {
		t.Fatalf("NewOTelExporter() unexpected error: %v", err)
	}
	if exporter == nil {
		t.Fatal("NewOTelExporter() returned nil exporter")
	}
}

func TestNewOTelExporterInvalidConfig(t *testing.T) {
	config := OTelConfig{
		Enabled:  true,
		Endpoint: "",
		Protocol: "invalid",
	}

	exporter, err := NewOTelExporter(config)
	if err == nil {
		t.Fatal("NewOTelExporter() expected error for invalid config, got nil")
	}
	if exporter != nil {
		t.Fatal("NewOTelExporter() expected nil exporter on error")
	}
}

func TestOTelExporterStartAndShutdownGRPC(t *testing.T) {
	config := OTelConfig{
		Enabled:        true,
		Endpoint:       "localhost:4317",
		Protocol:       ProtocolGRPC,
		ExportInterval: 1 * time.Second,
		Insecure:       true,
		ResourceAttributes: map[string]string{
			"service.version": "test",
		},
	}

	exporter, err := NewOTelExporter(config)
	if err != nil {
		t.Fatalf("NewOTelExporter() error: %v", err)
	}

	ctx := context.Background()

	// Start should succeed even without a real collector; the gRPC exporter
	// uses non-blocking dial by default.
	if err := exporter.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Verify provider is available.
	if exporter.MeterProvider() == nil {
		t.Fatal("MeterProvider() returned nil after Start")
	}

	// Record some metrics to ensure instruments work without panic.
	exporter.RecordHTTPRequest(ctx, "GET", "200", "test-cluster", 0.05)
	exporter.RecordInFlightChange(ctx, 1)
	exporter.RecordInFlightChange(ctx, -1)
	exporter.RecordUpstreamDuration(ctx, "test-cluster", "10.0.0.1:8080", 0.03)

	// Shutdown may return an export error because no collector is running.
	// We only verify it does not panic and completes within the timeout.
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Ignore shutdown error: the periodic reader flushes and the exporter
	// rightfully reports a connection error when no collector is listening.
	_ = exporter.Shutdown(shutdownCtx)
}

func TestOTelExporterStartAndShutdownHTTP(t *testing.T) {
	config := OTelConfig{
		Enabled:        true,
		Endpoint:       "http://localhost:4318",
		Protocol:       ProtocolHTTP,
		ExportInterval: 1 * time.Second,
		Insecure:       true,
	}

	exporter, err := NewOTelExporter(config)
	if err != nil {
		t.Fatalf("NewOTelExporter() error: %v", err)
	}

	ctx := context.Background()

	if err := exporter.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Record some metrics.
	exporter.RecordHTTPRequest(ctx, "POST", "201", "api-cluster", 0.12)
	exporter.RecordUpstreamDuration(ctx, "api-cluster", "10.0.0.2:9090", 0.08)

	// Shutdown may return a connection error when no collector is running;
	// we only verify it completes without panicking.
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = exporter.Shutdown(shutdownCtx)
}

func TestOTelExporterDoubleStartFails(t *testing.T) {
	config := OTelConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
		Protocol: ProtocolGRPC,
		Insecure: true,
	}

	exporter, err := NewOTelExporter(config)
	if err != nil {
		t.Fatalf("NewOTelExporter() error: %v", err)
	}

	ctx := context.Background()
	if err := exporter.Start(ctx); err != nil {
		t.Fatalf("first Start() error: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = exporter.Shutdown(shutdownCtx)
	}()

	if err := exporter.Start(ctx); err == nil {
		t.Fatal("second Start() expected error, got nil")
	}
}

func TestOTelExporterShutdownIdempotent(t *testing.T) {
	config := OTelConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
		Protocol: ProtocolGRPC,
		Insecure: true,
	}

	exporter, err := NewOTelExporter(config)
	if err != nil {
		t.Fatalf("NewOTelExporter() error: %v", err)
	}

	ctx := context.Background()
	if err := exporter.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// First shutdown (may return export error, which is fine).
	_ = exporter.Shutdown(shutdownCtx)

	// Second shutdown should be a no-op and not panic.
	err = exporter.Shutdown(shutdownCtx)
	if err != nil {
		t.Fatalf("second Shutdown() should be no-op, got: %v", err)
	}
}

func TestOTelExporterShutdownWithoutStart(t *testing.T) {
	config := OTelConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
		Protocol: ProtocolGRPC,
	}

	exporter, err := NewOTelExporter(config)
	if err != nil {
		t.Fatalf("NewOTelExporter() error: %v", err)
	}

	ctx := context.Background()
	if err := exporter.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() without Start() should succeed, got: %v", err)
	}
}

func TestOTelExporterRecordWithoutStart(t *testing.T) {
	config := OTelConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
		Protocol: ProtocolGRPC,
	}

	exporter, err := NewOTelExporter(config)
	if err != nil {
		t.Fatalf("NewOTelExporter() error: %v", err)
	}

	// These should not panic even though the exporter has not been started.
	ctx := context.Background()
	exporter.RecordHTTPRequest(ctx, "GET", "200", "test", 0.01)
	exporter.RecordInFlightChange(ctx, 1)
	exporter.RecordUpstreamDuration(ctx, "test", "10.0.0.1:80", 0.05)
}

func TestInitOTelExporterDisabled(t *testing.T) {
	config := OTelConfig{
		Enabled: false,
	}

	exporter, err := InitOTelExporter(config)
	if err != nil {
		t.Fatalf("InitOTelExporter() unexpected error: %v", err)
	}
	if exporter != nil {
		t.Fatal("InitOTelExporter() expected nil exporter when disabled")
	}
}

func TestInitOTelExporterEnabled(t *testing.T) {
	config := OTelConfig{
		Enabled:  true,
		Endpoint: "localhost:4317",
		Protocol: ProtocolGRPC,
		Insecure: true,
	}

	exporter, err := InitOTelExporter(config)
	if err != nil {
		t.Fatalf("InitOTelExporter() error: %v", err)
	}
	if exporter == nil {
		t.Fatal("InitOTelExporter() returned nil exporter when enabled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = exporter.Shutdown(ctx)
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
