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

// Package httpclient provides a shared HTTP client factory with sensible defaults
// for timeout configurations to prevent hanging connections and resource exhaustion.
package httpclient

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// Default timeout values for HTTP clients.
const (
	// DefaultTimeout is the overall request timeout including body reading.
	DefaultTimeout = 30 * time.Second

	// DefaultIdleTimeout is the timeout for idle connections in the pool.
	DefaultIdleTimeout = 90 * time.Second

	// DefaultTLSHandshakeTimeout is the timeout for TLS handshake.
	DefaultTLSHandshakeTimeout = 10 * time.Second

	// DefaultResponseHeaderTimeout is the timeout for reading response headers.
	DefaultResponseHeaderTimeout = 10 * time.Second

	// DefaultDialTimeout is the timeout for establishing TCP connections.
	DefaultDialTimeout = 10 * time.Second

	// DefaultKeepAlive is the interval between keep-alive probes.
	DefaultKeepAlive = 30 * time.Second

	// DefaultMaxIdleConns is the maximum number of idle connections across all hosts.
	DefaultMaxIdleConns = 100

	// DefaultMaxIdleConnsPerHost is the maximum number of idle connections per host.
	DefaultMaxIdleConnsPerHost = 10
)

// Config holds configuration options for HTTP clients.
type Config struct {
	// Timeout is the overall request timeout.
	Timeout time.Duration

	// IdleTimeout is the timeout for idle connections.
	IdleTimeout time.Duration

	// TLSHandshakeTimeout is the timeout for TLS handshake.
	TLSHandshakeTimeout time.Duration

	// ResponseHeaderTimeout is the timeout for reading response headers.
	ResponseHeaderTimeout time.Duration

	// DialTimeout is the timeout for establishing TCP connections.
	DialTimeout time.Duration

	// KeepAlive is the interval between keep-alive probes.
	KeepAlive time.Duration

	// MaxIdleConns is the maximum number of idle connections across all hosts.
	MaxIdleConns int

	// MaxIdleConnsPerHost is the maximum number of idle connections per host.
	MaxIdleConnsPerHost int

	// TLSConfig is the TLS configuration for HTTPS requests.
	TLSConfig *tls.Config

	// DisableKeepAlives disables HTTP keep-alive.
	DisableKeepAlives bool

	// DisableCompression disables response compression.
	DisableCompression bool
}

// DefaultConfig returns a Config with sensible default values.
func DefaultConfig() *Config {
	return &Config{
		Timeout:               DefaultTimeout,
		IdleTimeout:           DefaultIdleTimeout,
		TLSHandshakeTimeout:   DefaultTLSHandshakeTimeout,
		ResponseHeaderTimeout: DefaultResponseHeaderTimeout,
		DialTimeout:           DefaultDialTimeout,
		KeepAlive:             DefaultKeepAlive,
		MaxIdleConns:          DefaultMaxIdleConns,
		MaxIdleConnsPerHost:   DefaultMaxIdleConnsPerHost,
	}
}

// NewClient creates a new HTTP client with the given configuration.
func NewClient(cfg *Config) *http.Client {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Apply defaults for zero values
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.TLSHandshakeTimeout == 0 {
		cfg.TLSHandshakeTimeout = DefaultTLSHandshakeTimeout
	}
	if cfg.ResponseHeaderTimeout == 0 {
		cfg.ResponseHeaderTimeout = DefaultResponseHeaderTimeout
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = DefaultDialTimeout
	}
	if cfg.KeepAlive == 0 {
		cfg.KeepAlive = DefaultKeepAlive
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = DefaultMaxIdleConns
	}
	if cfg.MaxIdleConnsPerHost == 0 {
		cfg.MaxIdleConnsPerHost = DefaultMaxIdleConnsPerHost
	}

	dialer := &net.Dialer{
		Timeout:   cfg.DialTimeout,
		KeepAlive: cfg.KeepAlive,
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		IdleConnTimeout:       cfg.IdleTimeout,
		TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		TLSClientConfig:       cfg.TLSConfig,
		DisableKeepAlives:     cfg.DisableKeepAlives,
		DisableCompression:    cfg.DisableCompression,
		ForceAttemptHTTP2:     true,
	}

	return &http.Client{
		Timeout:   cfg.Timeout,
		Transport: transport,
	}
}

// NewDefaultClient creates a new HTTP client with default settings.
// This is the recommended way to create HTTP clients for general use.
func NewDefaultClient() *http.Client {
	return NewClient(nil)
}

// NewClientWithTimeout creates a new HTTP client with a custom timeout.
// All other settings use defaults.
func NewClientWithTimeout(timeout time.Duration) *http.Client {
	cfg := DefaultConfig()
	cfg.Timeout = timeout
	return NewClient(cfg)
}

// NewClientWithoutTimeout creates a new HTTP client without an overall timeout.
// This is useful for streaming or long-polling scenarios.
// Connection-level timeouts are still applied.
func NewClientWithoutTimeout() *http.Client {
	cfg := DefaultConfig()
	cfg.Timeout = 0 // No overall timeout
	return NewClient(cfg)
}

// NewInsecureClient creates a new HTTP client that skips TLS verification.
// WARNING: This should only be used for testing or in trusted environments.
func NewInsecureClient() *http.Client {
	cfg := DefaultConfig()
	cfg.TLSConfig = &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // Intentional for testing
	}
	return NewClient(cfg)
}

// NewFastClient creates a new HTTP client optimized for fast, short-lived requests.
// Uses shorter timeouts and fewer idle connections.
func NewFastClient() *http.Client {
	cfg := &Config{
		Timeout:               5 * time.Second,
		IdleTimeout:           30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		DialTimeout:           5 * time.Second,
		KeepAlive:             15 * time.Second,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   5,
	}
	return NewClient(cfg)
}

// NewStreamingClient creates a new HTTP client optimized for streaming.
// No overall timeout, but connection-level timeouts are still applied.
func NewStreamingClient() *http.Client {
	cfg := &Config{
		Timeout:               0, // No overall timeout for streaming
		IdleTimeout:           120 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second, // Allow time for first response
		DialTimeout:           10 * time.Second,
		KeepAlive:             30 * time.Second,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   5,
		DisableCompression:    true, // Don't compress streaming data
	}
	return NewClient(cfg)
}

// NewHealthCheckClient creates a new HTTP client optimized for health checks.
// Uses very short timeouts for quick failure detection.
func NewHealthCheckClient() *http.Client {
	cfg := &Config{
		Timeout:               3 * time.Second,
		IdleTimeout:           10 * time.Second,
		TLSHandshakeTimeout:   2 * time.Second,
		ResponseHeaderTimeout: 2 * time.Second,
		DialTimeout:           2 * time.Second,
		KeepAlive:             10 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   2,
		DisableKeepAlives:     true, // Fresh connection for each health check
	}
	return NewClient(cfg)
}
