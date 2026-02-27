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

// Package upstream provides HTTP connection pooling and reverse proxy transport
// configuration for the NovaEdge data plane agent.
//
// DEPRECATED: This package will be removed once --forwarding-plane=rust is
// validated and the Rust dataplane handles all upstream connection management natively.
// See docs/plans/forwarding-deprecation.md for the removal timeline.
package upstream

const (
	// Connection pool limits

	// DefaultMaxIdleConns is the default maximum number of idle connections across all hosts
	// This limits the total number of persistent connections kept open when idle.
	// Higher values improve performance by reusing connections but consume more resources.
	DefaultMaxIdleConns = 100

	// DefaultMaxIdleConnsPerHost is the default maximum number of idle connections per host
	// This prevents a single backend from monopolizing the connection pool.
	// For gRPC and HTTP/2, this should be higher to support multiplexing.
	DefaultMaxIdleConnsPerHost = 100
)
