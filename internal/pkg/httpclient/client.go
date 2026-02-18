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
// for consistent timeout configuration across the codebase.
package httpclient

import (
	"net/http"
	"time"
)

const (
	// DefaultTimeout is the default total request timeout.
	DefaultTimeout = 30 * time.Second
	// DefaultIdleTimeout is the default idle connection timeout.
	DefaultIdleTimeout = 90 * time.Second
	// DefaultTLSHandshakeTimeout is the default TLS handshake timeout.
	DefaultTLSHandshakeTimeout = 10 * time.Second
	// DefaultResponseHeaderTimeout is the default response header timeout.
	DefaultResponseHeaderTimeout = 10 * time.Second
	// DefaultMaxIdleConns is the default maximum number of idle connections.
	DefaultMaxIdleConns = 100
	// DefaultMaxIdleConnsPerHost is the default maximum number of idle connections per host.
	DefaultMaxIdleConnsPerHost = 10
)

// NewDefaultClient creates a new HTTP client with sensible default timeouts.
func NewDefaultClient() *http.Client {
	return &http.Client{
		Timeout: DefaultTimeout,
		Transport: &http.Transport{
			IdleConnTimeout:       DefaultIdleTimeout,
			TLSHandshakeTimeout:   DefaultTLSHandshakeTimeout,
			ResponseHeaderTimeout: DefaultResponseHeaderTimeout,
			MaxIdleConns:          DefaultMaxIdleConns,
			MaxIdleConnsPerHost:   DefaultMaxIdleConnsPerHost,
		},
	}
}

// NewClientWithTimeout creates a new HTTP client with a custom timeout.
func NewClientWithTimeout(timeout time.Duration) *http.Client {
	client := NewDefaultClient()
	client.Timeout = timeout
	return client
}

// NewClientWithTransport creates a new HTTP client with a custom transport.
func NewClientWithTransport(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Timeout:   DefaultTimeout,
		Transport: transport,
	}
}

// NewClientWithOptions creates a new HTTP client with custom options.
type ClientOptions struct {
	Timeout               time.Duration
	IdleTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	Transport             http.RoundTripper
}

// NewClientWithOptions creates a new HTTP client with the specified options.
// Zero values will use defaults.
func NewClientWithOptions(opts ClientOptions) *http.Client {
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = DefaultIdleTimeout
	}
	if opts.TLSHandshakeTimeout == 0 {
		opts.TLSHandshakeTimeout = DefaultTLSHandshakeTimeout
	}
	if opts.ResponseHeaderTimeout == 0 {
		opts.ResponseHeaderTimeout = DefaultResponseHeaderTimeout
	}
	if opts.MaxIdleConns == 0 {
		opts.MaxIdleConns = DefaultMaxIdleConns
	}
	if opts.MaxIdleConnsPerHost == 0 {
		opts.MaxIdleConnsPerHost = DefaultMaxIdleConnsPerHost
	}

	if opts.Transport != nil {
		return &http.Client{
			Timeout:   opts.Timeout,
			Transport: opts.Transport,
		}
	}

	return &http.Client{
		Timeout: opts.Timeout,
		Transport: &http.Transport{
			IdleConnTimeout:       opts.IdleTimeout,
			TLSHandshakeTimeout:   opts.TLSHandshakeTimeout,
			ResponseHeaderTimeout: opts.ResponseHeaderTimeout,
			MaxIdleConns:          opts.MaxIdleConns,
			MaxIdleConnsPerHost:   opts.MaxIdleConnsPerHost,
		},
	}
}
