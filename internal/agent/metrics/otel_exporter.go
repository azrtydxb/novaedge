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
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var (
	errOtelExporterAlreadyStarted  = errors.New("otel exporter already started")
	errOtelExporterHasBeenShutDown = errors.New("otel exporter has been shut down")
	errUnsupportedProtocol         = errors.New("unsupported OTel export protocol")
)

// OTelExporter manages the OpenTelemetry metrics pipeline that exports
// NovaEdge metrics via OTLP. It creates OTel-native metric instruments
// that mirror the existing Prometheus metrics, allowing both systems to
// run simultaneously.
type OTelExporter struct {
	config   OTelConfig
	provider *metric.MeterProvider

	// OTel metric instruments that mirror Prometheus metrics.
	httpRequestsTotal           otelmetric.Int64Counter
	httpRequestDurationSeconds  otelmetric.Float64Histogram
	httpRequestsInFlight        otelmetric.Int64UpDownCounter
	upstreamRequestDurationSecs otelmetric.Float64Histogram

	mu       sync.Mutex
	started  atomic.Bool
	shutdown atomic.Bool
}

// NewOTelExporter creates a new OTelExporter with the supplied configuration.
// The exporter is not started until Start is called.
func NewOTelExporter(config OTelConfig) (*OTelExporter, error) {
	config = config.withDefaults()
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid otel config: %w", err)
	}

	return &OTelExporter{
		config: config,
	}, nil
}

// Start initialises the OTLP exporter, meter provider and creates the metric
// instruments. It is safe to call from any goroutine but must only be called
// once.
func (e *OTelExporter) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started.Load() {
		return errOtelExporterAlreadyStarted
	}
	if e.shutdown.Load() {
		return errOtelExporterHasBeenShutDown
	}

	exporter, err := e.buildExporter(ctx)
	if err != nil {
		return fmt.Errorf("creating OTLP metric exporter: %w", err)
	}

	res, err := e.buildResource(ctx)
	if err != nil {
		return fmt.Errorf("building OTel resource: %w", err)
	}

	reader := metric.NewPeriodicReader(
		exporter,
		metric.WithInterval(e.config.ExportInterval),
	)

	e.provider = metric.NewMeterProvider(
		metric.WithReader(reader),
		metric.WithResource(res),
	)

	if err := e.createInstruments(); err != nil {
		return fmt.Errorf("creating OTel metric instruments: %w", err)
	}

	e.started.Store(true)
	return nil
}

// Shutdown gracefully flushes pending metrics and releases resources. It blocks
// until all pending exports complete or the context expires.
func (e *OTelExporter) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.started.Load() || e.shutdown.Load() {
		return nil
	}

	e.shutdown.Store(true)
	if e.provider != nil {
		return e.provider.Shutdown(ctx)
	}
	return nil
}

// MeterProvider returns the underlying OTel MeterProvider. This can be used to
// register additional instruments or to set the global meter provider.
func (e *OTelExporter) MeterProvider() *metric.MeterProvider {
	return e.provider
}

// RecordHTTPRequest records an HTTP request in the OTel metric instruments.
// This should be called alongside the Prometheus RecordHTTPRequest function
// so that both exporters receive the same data.
func (e *OTelExporter) RecordHTTPRequest(ctx context.Context, method, statusClass, cluster string, duration float64) {
	if !e.started.Load() || e.shutdown.Load() {
		return
	}

	attrs := otelmetric.WithAttributes(
		attribute.String("method", method),
		attribute.String("status_class", statusClass),
		attribute.String("cluster", cluster),
	)
	e.httpRequestsTotal.Add(ctx, 1, attrs)

	durationAttrs := otelmetric.WithAttributes(
		attribute.String("method", method),
		attribute.String("cluster", cluster),
	)
	e.httpRequestDurationSeconds.Record(ctx, duration, durationAttrs)
}

// RecordInFlightChange adjusts the in-flight request gauge by delta (+1 or -1).
func (e *OTelExporter) RecordInFlightChange(ctx context.Context, delta int64) {
	if !e.started.Load() || e.shutdown.Load() {
		return
	}
	e.httpRequestsInFlight.Add(ctx, delta)
}

// RecordUpstreamDuration records an upstream (backend) request duration.
func (e *OTelExporter) RecordUpstreamDuration(ctx context.Context, cluster, endpoint string, duration float64) {
	if !e.started.Load() || e.shutdown.Load() {
		return
	}
	attrs := otelmetric.WithAttributes(
		attribute.String("cluster", cluster),
		attribute.String("endpoint", endpoint),
	)
	e.upstreamRequestDurationSecs.Record(ctx, duration, attrs)
}

// buildExporter creates the appropriate OTLP metric exporter based on protocol.
func (e *OTelExporter) buildExporter(ctx context.Context) (metric.Exporter, error) {
	switch e.config.Protocol {
	case ProtocolHTTP:
		return e.buildHTTPExporter(ctx)
	case ProtocolGRPC:
		return e.buildGRPCExporter(ctx)
	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedProtocol, e.config.Protocol)
	}
}

func (e *OTelExporter) buildHTTPExporter(ctx context.Context) (metric.Exporter, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpointURL(e.config.Endpoint),
	}
	if e.config.Insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}
	return otlpmetrichttp.New(ctx, opts...)
}

func (e *OTelExporter) buildGRPCExporter(ctx context.Context) (metric.Exporter, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(e.config.Endpoint),
	}
	if e.config.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}
	return otlpmetricgrpc.New(ctx, opts...)
}

// buildResource creates the OTel resource with service identification and any
// user-supplied attributes.
func (e *OTelExporter) buildResource(ctx context.Context) (*resource.Resource, error) {
	attrs := make([]attribute.KeyValue, 0, 1+len(e.config.ResourceAttributes))
	attrs = append(attrs, semconv.ServiceName("novaedge-agent"))
	for k, v := range e.config.ResourceAttributes {
		attrs = append(attrs, attribute.String(k, v))
	}
	return resource.New(ctx,
		resource.WithAttributes(attrs...),
	)
}

// createInstruments registers the OTel metric instruments that mirror existing
// Prometheus metrics.
func (e *OTelExporter) createInstruments() error {
	meter := e.provider.Meter("github.com/azrtydxb/novaedge/agent/metrics")

	var err error

	e.httpRequestsTotal, err = meter.Int64Counter(
		"novaedge.http.requests.total",
		otelmetric.WithDescription("Total number of HTTP requests"),
		otelmetric.WithUnit("{request}"),
	)
	if err != nil {
		return fmt.Errorf("creating http_requests_total counter: %w", err)
	}

	e.httpRequestDurationSeconds, err = meter.Float64Histogram(
		"novaedge.http.request.duration",
		otelmetric.WithDescription("HTTP request duration in seconds"),
		otelmetric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("creating http_request_duration histogram: %w", err)
	}

	e.httpRequestsInFlight, err = meter.Int64UpDownCounter(
		"novaedge.http.requests.in_flight",
		otelmetric.WithDescription("Number of HTTP requests currently being processed"),
		otelmetric.WithUnit("{request}"),
	)
	if err != nil {
		return fmt.Errorf("creating http_requests_in_flight gauge: %w", err)
	}

	e.upstreamRequestDurationSecs, err = meter.Float64Histogram(
		"novaedge.upstream.request.duration",
		otelmetric.WithDescription("Upstream (backend) request duration in seconds"),
		otelmetric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("creating upstream_request_duration histogram: %w", err)
	}

	return nil
}
