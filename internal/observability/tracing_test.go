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

	"go.uber.org/zap"
)

func TestNewTracerProvider_Disabled(t *testing.T) {
	logger := zap.NewNop()
	config := TracingConfig{
		Enabled: false,
	}

	tp, err := NewTracerProvider(context.Background(), config, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil tracer provider even when disabled")
	}
	if tp.IsEnabled() {
		t.Error("expected IsEnabled to return false when tracing is disabled")
	}
}

func TestTracerProvider_Shutdown_WhenDisabled(t *testing.T) {
	logger := zap.NewNop()
	config := TracingConfig{
		Enabled: false,
	}

	tp, err := NewTracerProvider(context.Background(), config, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Shutdown should be a no-op when disabled
	err = tp.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("expected no error on shutdown when disabled, got %v", err)
	}
}

func TestTracerProvider_Tracer_WhenDisabled(t *testing.T) {
	logger := zap.NewNop()
	config := TracingConfig{
		Enabled: false,
	}

	tp, err := NewTracerProvider(context.Background(), config, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return a nop tracer when disabled (from global otel)
	tracer := tp.Tracer("test-scope")
	if tracer == nil {
		t.Fatal("expected non-nil tracer even when disabled")
	}
}

func TestTracingConfig_Fields(t *testing.T) {
	config := TracingConfig{
		Enabled:        true,
		Endpoint:       "localhost:4317",
		SampleRate:     0.5,
		ServiceName:    "novaedge-agent",
		ServiceVersion: "v1.0.0",
	}

	if !config.Enabled {
		t.Error("expected Enabled to be true")
	}
	if config.Endpoint != "localhost:4317" {
		t.Errorf("expected endpoint 'localhost:4317', got %q", config.Endpoint)
	}
	if config.SampleRate != 0.5 {
		t.Errorf("expected sample rate 0.5, got %f", config.SampleRate)
	}
	if config.ServiceName != "novaedge-agent" {
		t.Errorf("expected service name 'novaedge-agent', got %q", config.ServiceName)
	}
	if config.ServiceVersion != "v1.0.0" {
		t.Errorf("expected service version 'v1.0.0', got %q", config.ServiceVersion)
	}
}

func TestTracerProvider_IsEnabled_Disabled(t *testing.T) {
	tp := &TracerProvider{
		provider: nil,
		logger:   zap.NewNop(),
	}

	if tp.IsEnabled() {
		t.Error("expected IsEnabled to return false when provider is nil")
	}
}
