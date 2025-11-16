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

package health

import "time"

const (
	// Health check timeouts

	// DefaultHealthCheckTimeout is the default timeout for health check HTTP requests
	// If a health check doesn't complete within this time, it's considered failed.
	DefaultHealthCheckTimeout = 3 * time.Second

	// DefaultHealthCheckDialTimeout is the default timeout for establishing health check connections
	// This prevents health checks from hanging on unreachable endpoints.
	DefaultHealthCheckDialTimeout = 2 * time.Second

	// Health check thresholds

	// DefaultHealthyThreshold is the default number of consecutive successful health checks
	// required before marking an endpoint as healthy after it was unhealthy.
	DefaultHealthyThreshold = 2

	// DefaultUnhealthyThreshold is the default number of consecutive failed health checks
	// required before marking an endpoint as unhealthy.
	DefaultUnhealthyThreshold = 3
)
