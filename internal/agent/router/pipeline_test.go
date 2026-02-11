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
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestPipeline_OrderedExecution(t *testing.T) {
	logger := zap.NewNop()
	pipeline := NewPipeline(logger)

	var order []int

	// Add middleware with different priorities (should execute in priority order)
	pipeline.Add(MiddlewareEntry{
		Name:     "second",
		Type:     MiddlewareBuiltin,
		Priority: 20,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, 2)
				next.ServeHTTP(w, r)
			})
		},
	})

	pipeline.Add(MiddlewareEntry{
		Name:     "first",
		Type:     MiddlewareBuiltin,
		Priority: 10,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, 1)
				next.ServeHTTP(w, r)
			})
		},
	})

	pipeline.Add(MiddlewareEntry{
		Name:     "third",
		Type:     MiddlewareBuiltin,
		Priority: 30,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, 3)
				next.ServeHTTP(w, r)
			})
		},
	})

	// Final handler
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, 0) // 0 = final handler
		w.WriteHeader(http.StatusOK)
	})

	wrapped := pipeline.Wrap(final)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if len(order) != 4 {
		t.Fatalf("expected 4 executions, got %d", len(order))
	}
	if order[0] != 1 || order[1] != 2 || order[2] != 3 || order[3] != 0 {
		t.Errorf("expected execution order [1, 2, 3, 0], got %v", order)
	}
}

func TestPipeline_ShortCircuit(t *testing.T) {
	logger := zap.NewNop()
	pipeline := NewPipeline(logger)

	finalCalled := false

	pipeline.Add(MiddlewareEntry{
		Name:     "auth",
		Type:     MiddlewareBuiltin,
		Priority: 10,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Short-circuit: return 401 without calling next
				w.WriteHeader(http.StatusUnauthorized)
			})
		},
	})

	pipeline.Add(MiddlewareEntry{
		Name:     "logging",
		Type:     MiddlewareBuiltin,
		Priority: 20,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		},
	})

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finalCalled = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := pipeline.Wrap(final)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	if finalCalled {
		t.Error("final handler should not have been called")
	}
}

func TestPipeline_PanicRecovery(t *testing.T) {
	logger := zap.NewNop()
	pipeline := NewPipeline(logger)

	pipeline.Add(MiddlewareEntry{
		Name:     "panicking",
		Type:     MiddlewareBuiltin,
		Priority: 10,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("test panic")
			})
		},
	})

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := pipeline.Wrap(final)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 after panic, got %d", rec.Code)
	}
}

func TestPipeline_PerRouteConfig(t *testing.T) {
	logger := zap.NewNop()

	// Build a pipeline for route A
	pipelineA := NewPipeline(logger)
	pipelineA.Add(MiddlewareEntry{
		Name:     "rate-limit",
		Type:     MiddlewareBuiltin,
		Priority: 10,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Route", "A")
				next.ServeHTTP(w, r)
			})
		},
	})

	// Build a different pipeline for route B
	pipelineB := NewPipeline(logger)
	pipelineB.Add(MiddlewareEntry{
		Name:     "auth",
		Type:     MiddlewareBuiltin,
		Priority: 10,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Route", "B")
				next.ServeHTTP(w, r)
			})
		},
	})

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Route A
	wrappedA := pipelineA.Wrap(final)
	reqA := httptest.NewRequest(http.MethodGet, "/", nil)
	recA := httptest.NewRecorder()
	wrappedA.ServeHTTP(recA, reqA)

	if recA.Header().Get("X-Route") != "A" {
		t.Errorf("expected X-Route=A for route A pipeline")
	}

	// Route B
	wrappedB := pipelineB.Wrap(final)
	reqB := httptest.NewRequest(http.MethodGet, "/", nil)
	recB := httptest.NewRecorder()
	wrappedB.ServeHTTP(recB, reqB)

	if recB.Header().Get("X-Route") != "B" {
		t.Errorf("expected X-Route=B for route B pipeline")
	}
}

func TestPipelineState(t *testing.T) {
	logger := zap.NewNop()
	pipeline := NewPipeline(logger)

	pipeline.Add(MiddlewareEntry{
		Name:     "setter",
		Type:     MiddlewareBuiltin,
		Priority: 10,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				state, ok := GetPipelineState(r.Context())
				if ok {
					state.Set("user", "testuser")
				}
				next.ServeHTTP(w, r)
			})
		},
	})

	pipeline.Add(MiddlewareEntry{
		Name:     "getter",
		Type:     MiddlewareBuiltin,
		Priority: 20,
		Handler: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				state, ok := GetPipelineState(r.Context())
				if ok {
					if user, found := state.GetString("user"); found {
						w.Header().Set("X-User", user)
					}
				}
				next.ServeHTTP(w, r)
			})
		},
	})

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with PipelineMiddleware to inject state
	stateMiddleware := PipelineMiddleware()
	wrapped := stateMiddleware(pipeline.Wrap(final))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Header().Get("X-User") != "testuser" {
		t.Errorf("expected X-User=testuser, got %q", rec.Header().Get("X-User"))
	}
}

func TestBuildPipeline(t *testing.T) {
	logger := zap.NewNop()

	factory := func(name string, _ map[string]string) (func(http.Handler) http.Handler, error) {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("X-Middleware", name)
				next.ServeHTTP(w, r)
			})
		}, nil
	}

	refs := []MiddlewareRef{
		{Type: "builtin", Name: "rate-limit", Priority: 10},
		{Type: "builtin", Name: "auth", Priority: 20},
	}

	pipeline, err := BuildPipeline(logger, refs, factory, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pipeline.Len() != 2 {
		t.Errorf("expected 2 entries, got %d", pipeline.Len())
	}

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := pipeline.Wrap(final)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	middlewares := rec.Header().Values("X-Middleware")
	if len(middlewares) != 2 {
		t.Fatalf("expected 2 middleware headers, got %d: %v", len(middlewares), middlewares)
	}
	if middlewares[0] != "rate-limit" || middlewares[1] != "auth" {
		t.Errorf("expected [rate-limit, auth], got %v", middlewares)
	}
}
