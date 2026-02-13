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
	"errors"
	"net/url"
	"time"
)

const (
	// ProtocolHTTP selects OTLP over HTTP transport.
	ProtocolHTTP = "http"
	// ProtocolGRPC selects OTLP over gRPC transport.
	ProtocolGRPC = "grpc"

	// DefaultExportInterval is the default interval between metric exports.
	DefaultExportInterval = 30 * time.Second
)

// OTelConfig holds configuration for the OpenTelemetry metrics exporter.
type OTelConfig struct {
	// Enabled controls whether the OTel metrics exporter is active.
	Enabled bool
	// Endpoint is the OTLP collector endpoint URL (e.g. "localhost:4317" for gRPC,
	// "http://localhost:4318/v1/metrics" for HTTP).
	Endpoint string
	// Protocol selects the OTLP transport protocol: "http" or "grpc".
	Protocol string
	// ExportInterval is the interval between periodic metric exports. Defaults
	// to 30s if zero.
	ExportInterval time.Duration
	// ResourceAttributes are additional key-value pairs attached to the OTel
	// resource that identifies this telemetry source (e.g. service.name,
	// service.version, deployment.environment).
	ResourceAttributes map[string]string
	// Insecure disables TLS for the OTLP connection. Useful for local
	// development or when TLS is terminated elsewhere.
	Insecure bool
}

// Validate checks that the OTelConfig contains valid values. It returns an
// error describing the first problem found, or nil when the config is valid.
func (c *OTelConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Endpoint == "" {
		return errors.New("otel: endpoint must not be empty")
	}

	switch c.Protocol {
	case ProtocolHTTP:
		if _, err := url.Parse(c.Endpoint); err != nil {
			return errors.New("otel: endpoint is not a valid URL for HTTP protocol")
		}
	case ProtocolGRPC:
		// gRPC endpoints are host:port; a basic length check suffices here
		// since the gRPC dialer performs full validation.
		if len(c.Endpoint) < 3 {
			return errors.New("otel: endpoint is not a valid host:port for gRPC protocol")
		}
	default:
		return errors.New("otel: protocol must be \"http\" or \"grpc\"")
	}

	if c.ExportInterval < 0 {
		return errors.New("otel: export interval must not be negative")
	}

	return nil
}

// withDefaults returns a copy of the config with zero-value fields replaced by
// sensible defaults.
func (c OTelConfig) withDefaults() OTelConfig {
	if c.ExportInterval == 0 {
		c.ExportInterval = DefaultExportInterval
	}
	if c.Protocol == "" {
		c.Protocol = ProtocolGRPC
	}
	return c
}
