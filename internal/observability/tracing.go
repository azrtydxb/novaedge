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
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TracingConfig holds the configuration for OpenTelemetry tracing
type TracingConfig struct {
	Enabled        bool
	Endpoint       string
	SampleRate     float64
	ServiceName    string
	ServiceVersion string
}

// TracerProvider wraps the OpenTelemetry tracer provider
type TracerProvider struct {
	provider *sdktrace.TracerProvider
	logger   *zap.Logger
}

// NewTracerProvider creates and initializes a new OpenTelemetry tracer provider
func NewTracerProvider(ctx context.Context, config TracingConfig, logger *zap.Logger) (*TracerProvider, error) {
	if !config.Enabled {
		logger.Info("Tracing is disabled")
		return &TracerProvider{
			provider: nil,
			logger:   logger,
		}, nil
	}

	logger.Info("Initializing OpenTelemetry tracing",
		zap.String("endpoint", config.Endpoint),
		zap.Float64("sample_rate", config.SampleRate),
		zap.String("service", config.ServiceName),
	)

	// Create OTLP trace exporter
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Create gRPC connection to OTLP endpoint
	conn, err := grpc.NewClient(
		config.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to OTLP endpoint: %w", err)
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(config.ServiceName),
			semconv.ServiceVersion(config.ServiceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create sampler based on sample rate
	var sampler sdktrace.Sampler
	if config.SampleRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if config.SampleRate <= 0.0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(config.SampleRate)
	}

	// Create tracer provider
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global tracer provider
	otel.SetTracerProvider(provider)

	// Set global text map propagator for context propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.Info("OpenTelemetry tracing initialized successfully")

	return &TracerProvider{
		provider: provider,
		logger:   logger,
	}, nil
}

// Shutdown gracefully shuts down the tracer provider
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if tp.provider == nil {
		return nil
	}

	tp.logger.Info("Shutting down tracer provider")

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := tp.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown tracer provider: %w", err)
	}

	tp.logger.Info("Tracer provider shut down successfully")
	return nil
}

// Tracer returns a tracer for the given instrumentation scope
func (tp *TracerProvider) Tracer(name string) trace.Tracer {
	if tp.provider == nil {
		return otel.Tracer(name)
	}
	return tp.provider.Tracer(name)
}

// IsEnabled returns whether tracing is enabled
func (tp *TracerProvider) IsEnabled() bool {
	return tp.provider != nil
}
