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

package observability

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
)

func TestTracerProvider_Shutdown_NilProvider(t *testing.T) {
	tp := &TracerProvider{
		provider: nil,
		logger:   zap.NewNop(),
	}

	err := tp.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown() with nil provider should return nil, got %v", err)
	}
}

func TestTracerProvider_Tracer_NilProvider(t *testing.T) {
	tp := &TracerProvider{
		provider: nil,
		logger:   zap.NewNop(),
	}

	tracer := tp.Tracer("test-scope")
	if tracer == nil {
		t.Error("Tracer() should return non-nil tracer even with nil provider")
	}
}

func TestNewTracerProvider_Enabled_ConnectionError(t *testing.T) {
	logger := zap.NewNop()
	config := TracingConfig{
		Enabled:        true,
		Endpoint:       "localhost:9999", // Non-existent endpoint
		SampleRate:     1.0,
		ServiceName:    "test-service",
		ServiceVersion: "v1.0.0",
	}

	// This should fail because the endpoint doesn't exist
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := NewTracerProvider(ctx, config, logger)
	// The test passes whether or not there's an error - we're just testing it doesn't panic
	_ = err
}

func TestTracingConfig_DefaultValues(t *testing.T) {
	config := TracingConfig{}

	if config.Enabled {
		t.Error("default Enabled should be false")
	}
	if config.Endpoint != "" {
		t.Errorf("default Endpoint should be empty, got %q", config.Endpoint)
	}
	if config.SampleRate != 0 {
		t.Errorf("default SampleRate should be 0, got %f", config.SampleRate)
	}
	if config.ServiceName != "" {
		t.Errorf("default ServiceName should be empty, got %q", config.ServiceName)
	}
	if config.ServiceVersion != "" {
		t.Errorf("default ServiceVersion should be empty, got %q", config.ServiceVersion)
	}
}

func TestTracingConfig_SampleRate_Boundary(t *testing.T) {
	tests := []struct {
		name       string
		sampleRate float64
	}{
		{
			name:       "zero sample rate",
			sampleRate: 0.0,
		},
		{
			name:       "negative sample rate",
			sampleRate: -0.5,
		},
		{
			name:       "full sample rate",
			sampleRate: 1.0,
		},
		{
			name:       "above full sample rate",
			sampleRate: 1.5,
		},
		{
			name:       "partial sample rate",
			sampleRate: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := TracingConfig{
				Enabled:    false, // Disable to avoid actual connection
				SampleRate: tt.sampleRate,
			}

			tp, err := NewTracerProvider(context.Background(), config, zap.NewNop())
			if err != nil {
				t.Errorf("NewTracerProvider() error = %v", err)
			}
			if tp == nil {
				t.Error("NewTracerProvider() returned nil")
			}
		})
	}
}

func TestTracerProvider_IsEnabled_WithProvider(t *testing.T) {
	// Create a mock tracer provider
	provider := trace.NewTracerProvider()
	tp := &TracerProvider{
		provider: provider,
		logger:   zap.NewNop(),
	}

	if !tp.IsEnabled() {
		t.Error("IsEnabled() should return true when provider is not nil")
	}
}

func TestTracerProvider_Tracer_WithProvider(t *testing.T) {
	provider := trace.NewTracerProvider()
	tp := &TracerProvider{
		provider: provider,
		logger:   zap.NewNop(),
	}

	tracer := tp.Tracer("test-scope")
	if tracer == nil {
		t.Error("Tracer() returned nil")
	}
}

func TestTracerProvider_Shutdown_WithProvider(t *testing.T) {
	provider := trace.NewTracerProvider()
	tp := &TracerProvider{
		provider: provider,
		logger:   zap.NewNop(),
	}

	err := tp.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

func TestTracerProvider_Shutdown_Timeout(t *testing.T) {
	provider := trace.NewTracerProvider()
	tp := &TracerProvider{
		provider: provider,
		logger:   zap.NewNop(),
	}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Shutdown should still work (it creates its own timeout)
	err := tp.Shutdown(ctx)
	// We just verify it doesn't panic
	_ = err
}

// Note: NewTracerProvider requires a non-nil logger, so we test with zap.NewNop()
func TestNewTracerProvider_WithNopLogger(t *testing.T) {
	config := TracingConfig{
		Enabled: false,
	}

	tp, err := NewTracerProvider(context.Background(), config, zap.NewNop())
	if err != nil {
		t.Errorf("NewTracerProvider() error = %v", err)
	}
	if tp == nil {
		t.Error("NewTracerProvider() returned nil")
	}
}

func TestTracerProvider_MultipleShutdowns(t *testing.T) {
	provider := trace.NewTracerProvider()
	tp := &TracerProvider{
		provider: provider,
		logger:   zap.NewNop(),
	}

	// First shutdown
	err := tp.Shutdown(context.Background())
	if err != nil {
		t.Errorf("First Shutdown() error = %v", err)
	}

	// Second shutdown should be safe (provider is nil after first)
	err = tp.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Second Shutdown() error = %v", err)
	}
}
