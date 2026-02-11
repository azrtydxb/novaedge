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

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"sort"

	"go.uber.org/zap"
)

// MiddlewarePhase indicates when a middleware executes relative to routing/backend.
type MiddlewarePhase int

const (
	// PhasePreRoute executes before route matching.
	PhasePreRoute MiddlewarePhase = iota
	// PhasePostRoute executes after route matching but before backend.
	PhasePostRoute
	// PhasePreBackend executes just before sending to the backend.
	PhasePreBackend
	// PhasePostBackend executes after the backend response.
	PhasePostBackend
)

// String returns the phase name.
func (p MiddlewarePhase) String() string {
	switch p {
	case PhasePreRoute:
		return "pre-route"
	case PhasePostRoute:
		return "post-route"
	case PhasePreBackend:
		return "pre-backend"
	case PhasePostBackend:
		return "post-backend"
	default:
		return "unknown"
	}
}

// ParseMiddlewarePhase converts a string to a MiddlewarePhase.
func ParseMiddlewarePhase(s string) MiddlewarePhase {
	switch s {
	case "pre-route":
		return PhasePreRoute
	case "post-route":
		return PhasePostRoute
	case "pre-backend":
		return PhasePreBackend
	case "post-backend":
		return PhasePostBackend
	default:
		return PhasePostRoute
	}
}

// MiddlewareType identifies the kind of middleware.
type MiddlewareType string

const (
	// MiddlewareBuiltin is a built-in middleware (rate-limit, CORS, JWT, etc.).
	MiddlewareBuiltin MiddlewareType = "builtin"
	// MiddlewareWASM is a WASM plugin middleware.
	MiddlewareWASM MiddlewareType = "wasm"
)

// MiddlewareEntry represents a single middleware in the pipeline.
type MiddlewareEntry struct {
	// Name is the middleware identifier.
	Name string
	// Type is builtin or wasm.
	Type MiddlewareType
	// Priority determines execution order (lower = earlier).
	Priority int
	// Handler is the http middleware function.
	Handler func(http.Handler) http.Handler
	// Config holds optional key-value configuration.
	Config map[string]string
}

// Pipeline manages an ordered list of middleware for a route.
type Pipeline struct {
	entries []MiddlewareEntry
	logger  *zap.Logger
	sorted  bool
}

// NewPipeline creates an empty middleware pipeline.
func NewPipeline(logger *zap.Logger) *Pipeline {
	return &Pipeline{
		logger: logger,
	}
}

// Add appends a middleware entry. The pipeline must be sorted before use.
func (p *Pipeline) Add(entry MiddlewareEntry) {
	p.entries = append(p.entries, entry)
	p.sorted = false
}

// Sort orders the middleware entries by priority (ascending).
func (p *Pipeline) Sort() {
	sort.SliceStable(p.entries, func(i, j int) bool {
		return p.entries[i].Priority < p.entries[j].Priority
	})
	p.sorted = true
}

// Len returns the number of entries.
func (p *Pipeline) Len() int {
	return len(p.entries)
}

// Entries returns the middleware entries (caller must not mutate).
func (p *Pipeline) Entries() []MiddlewareEntry {
	return p.entries
}

// Wrap wraps the given handler with all middleware in priority order.
// The first middleware in the sorted list is the outermost wrapper.
// If a middleware panics, it is recovered and a 500 is returned.
func (p *Pipeline) Wrap(handler http.Handler) http.Handler {
	if !p.sorted {
		p.Sort()
	}

	// Apply in reverse order so lowest-priority middleware is outermost
	wrapped := handler
	for i := len(p.entries) - 1; i >= 0; i-- {
		entry := p.entries[i]
		mw := entry.Handler
		name := entry.Name

		// Wrap each middleware with panic recovery
		next := mw(wrapped)
		wrapped = p.wrapWithRecovery(name, next)
	}
	return wrapped
}

// wrapWithRecovery wraps a handler with panic recovery.
func (p *Pipeline) wrapWithRecovery(name string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				p.logger.Error("Middleware panic recovered",
					zap.String("middleware", name),
					zap.Any("panic", rec),
					zap.String("stack", string(debug.Stack())),
				)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		handler.ServeHTTP(w, r)
	})
}

// pipelineStateKey is the context key for pipeline state.
type pipelineStateKey struct{}

// PipelineState carries mutable state through the middleware pipeline,
// allowing middleware entries to communicate with each other.
type PipelineState struct {
	// Values is a general-purpose key-value store for inter-middleware communication.
	Values map[string]interface{}
}

// NewPipelineState creates a new empty pipeline state.
func NewPipelineState() *PipelineState {
	return &PipelineState{
		Values: make(map[string]interface{}),
	}
}

// Set stores a value in the pipeline state.
func (ps *PipelineState) Set(key string, value interface{}) {
	ps.Values[key] = value
}

// Get retrieves a value from the pipeline state. Returns (nil, false) if not found.
func (ps *PipelineState) Get(key string) (interface{}, bool) {
	v, ok := ps.Values[key]
	return v, ok
}

// GetString retrieves a string value. Returns ("", false) if not found or wrong type.
func (ps *PipelineState) GetString(key string) (string, bool) {
	v, ok := ps.Values[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// WithPipelineState stores PipelineState in a context.
func WithPipelineState(ctx context.Context, state *PipelineState) context.Context {
	return context.WithValue(ctx, pipelineStateKey{}, state)
}

// GetPipelineState retrieves PipelineState from a context.
func GetPipelineState(ctx context.Context) (*PipelineState, bool) {
	state, ok := ctx.Value(pipelineStateKey{}).(*PipelineState)
	return state, ok
}

// BuildPipeline constructs a Pipeline from a list of MiddlewareRef configurations
// and available builtin/WASM middleware factories.
func BuildPipeline(
	logger *zap.Logger,
	refs []MiddlewareRef,
	builtinFactory func(name string, config map[string]string) (func(http.Handler) http.Handler, error),
	wasmFactory func(name string, config map[string]string) (func(http.Handler) http.Handler, error),
) (*Pipeline, error) {
	pipeline := NewPipeline(logger)

	for _, ref := range refs {
		var handler func(http.Handler) http.Handler
		var err error

		switch MiddlewareType(ref.Type) {
		case MiddlewareBuiltin:
			if builtinFactory == nil {
				return nil, fmt.Errorf("builtin middleware factory not available for %s", ref.Name)
			}
			handler, err = builtinFactory(ref.Name, ref.Config)
		case MiddlewareWASM:
			if wasmFactory == nil {
				return nil, fmt.Errorf("WASM middleware factory not available for %s", ref.Name)
			}
			handler, err = wasmFactory(ref.Name, ref.Config)
		default:
			return nil, fmt.Errorf("unknown middleware type %q for %s", ref.Type, ref.Name)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to create middleware %s: %w", ref.Name, err)
		}

		pipeline.Add(MiddlewareEntry{
			Name:     ref.Name,
			Type:     MiddlewareType(ref.Type),
			Priority: ref.Priority,
			Handler:  handler,
			Config:   ref.Config,
		})
	}

	pipeline.Sort()
	return pipeline, nil
}

// MiddlewareRef is a serializable reference to a middleware used in route config.
type MiddlewareRef struct {
	// Type is "builtin" or "wasm".
	Type string
	// Name identifies the middleware.
	Name string
	// Priority determines execution order (lower = earlier).
	Priority int
	// Config holds optional key-value configuration.
	Config map[string]string
}

// PipelineMiddleware returns an http middleware that injects PipelineState
// into the request context so downstream middleware can communicate.
func PipelineMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := NewPipelineState()
			ctx := WithPipelineState(r.Context(), state)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
