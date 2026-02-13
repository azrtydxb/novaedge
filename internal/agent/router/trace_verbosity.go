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

package router

import "sync/atomic"

// TraceVerbosity controls how much tracing detail is emitted per request.
// In "minimal" mode only the top-level server and client (upstream) spans
// are created, keeping overhead low for production traffic.  In "detailed"
// mode additional child spans are emitted for route matching, middleware /
// filter execution, backend selection, connection pool acquisition, and
// health-check status lookup, which is useful for debugging.
type TraceVerbosity string

const (
	// TraceVerbosityMinimal creates only the server and upstream client spans.
	TraceVerbosityMinimal TraceVerbosity = "minimal"

	// TraceVerbosityDetailed adds child spans for route matching, middleware
	// execution, backend selection, connection pool acquisition, and health
	// check lookups.
	TraceVerbosityDetailed TraceVerbosity = "detailed"
)

// TraceVerbosityConfig holds the current trace verbosity level.  The level
// is stored in an atomic.Value so it can be changed at runtime (for example
// via an admin API) without locks on the hot path.
type TraceVerbosityConfig struct {
	level atomic.Value // stores TraceVerbosity (string)
}

// NewTraceVerbosityConfig returns a TraceVerbosityConfig initialised to
// the given level.  If level is empty, TraceVerbosityMinimal is used.
func NewTraceVerbosityConfig(level TraceVerbosity) *TraceVerbosityConfig {
	c := &TraceVerbosityConfig{}
	if level == "" {
		level = TraceVerbosityMinimal
	}
	c.level.Store(level)
	return c
}

// SetVerbosity changes the trace verbosity level atomically.
func (c *TraceVerbosityConfig) SetVerbosity(level TraceVerbosity) {
	c.level.Store(level)
}

// GetVerbosity returns the current trace verbosity level.
func (c *TraceVerbosityConfig) GetVerbosity() TraceVerbosity {
	v, ok := c.level.Load().(TraceVerbosity)
	if !ok {
		return TraceVerbosityMinimal
	}
	return v
}

// ShouldTraceDetailed returns true when the current verbosity level is
// TraceVerbosityDetailed.  This is the guard callers use to decide whether
// to create additional child spans.
func (c *TraceVerbosityConfig) ShouldTraceDetailed() bool {
	return c.GetVerbosity() == TraceVerbosityDetailed
}

// DefaultTraceVerbosity is the package-level singleton used by the router
// and forwarding code.  It defaults to minimal and can be changed at
// startup or at runtime via an admin API.
var DefaultTraceVerbosity = NewTraceVerbosityConfig(TraceVerbosityMinimal)
