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

package wasm

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"go.uber.org/zap"
)

// Plugin represents a loaded, compiled WASM module that can be executed.
type Plugin struct {
	config   *PluginConfig
	compiled wazero.CompiledModule
	runtime  wazero.Runtime
	pool     *InstancePool
	logger   *zap.Logger
}

// NewPlugin creates a new plugin from a compiled module.
func NewPlugin(cfg *PluginConfig, compiled wazero.CompiledModule, rt wazero.Runtime, logger *zap.Logger) *Plugin {
	p := &Plugin{
		config:   cfg,
		compiled: compiled,
		runtime:  rt,
		logger:   logger.With(zap.String("wasm_plugin", cfg.Name)),
	}
	p.pool = NewInstancePool(p, poolDefaultSize)
	return p
}

// Name returns the plugin name.
func (p *Plugin) Name() string { return p.config.Name }

// Phase returns the plugin execution phase.
func (p *Plugin) Phase() Phase { return p.config.Phase }

// Priority returns the ordering priority.
func (p *Plugin) Priority() int { return p.config.Priority }

// Config returns the plugin configuration map.
func (p *Plugin) Config() map[string]string { return p.config.Config }

// ExecuteRequestPhase runs the guest on_request_headers export.
// It returns the Action the guest chose (continue or pause).
func (p *Plugin) ExecuteRequestPhase(ctx context.Context, reqCtx *RequestContext) (Action, error) {
	start := time.Now()

	inst, err := p.pool.Get(ctx)
	if err != nil {
		RecordPluginError(p.config.Name, "request")
		return ActionContinue, fmt.Errorf("failed to get WASM instance: %w", err)
	}
	defer p.pool.Put(inst)

	reqCtx.PluginConfig = p.config.Config
	wasmCtx := withRequestContext(ctx, reqCtx)

	fn := inst.module.ExportedFunction(guestExportNames.OnRequestHeaders)
	if fn == nil {
		// Guest does not handle this phase — continue.
		return ActionContinue, nil
	}

	// Call the guest function. The guest communicates back via host functions.
	_, err = fn.Call(wasmCtx)

	duration := time.Since(start)
	RecordPluginExecution(p.config.Name, "request", duration, err)

	if err != nil {
		return ActionContinue, fmt.Errorf("wasm on_request_headers failed: %w", err)
	}

	return reqCtx.Action, nil
}

// ExecuteResponsePhase runs the guest on_response_headers export.
func (p *Plugin) ExecuteResponsePhase(ctx context.Context, reqCtx *RequestContext) (Action, error) {
	start := time.Now()

	inst, err := p.pool.Get(ctx)
	if err != nil {
		RecordPluginError(p.config.Name, "response")
		return ActionContinue, fmt.Errorf("failed to get WASM instance: %w", err)
	}
	defer p.pool.Put(inst)

	reqCtx.PluginConfig = p.config.Config
	wasmCtx := withRequestContext(ctx, reqCtx)

	fn := inst.module.ExportedFunction(guestExportNames.OnResponseHeaders)
	if fn == nil {
		return ActionContinue, nil
	}

	_, err = fn.Call(wasmCtx)

	duration := time.Since(start)
	RecordPluginExecution(p.config.Name, "response", duration, err)

	if err != nil {
		return ActionContinue, fmt.Errorf("wasm on_response_headers failed: %w", err)
	}

	return reqCtx.Action, nil
}

// Close releases all instances and the compiled module.
func (p *Plugin) Close(ctx context.Context) {
	if p.pool != nil {
		p.pool.Close(ctx)
	}
	if p.compiled != nil {
		_ = p.compiled.Close(ctx)
	}
	p.logger.Info("Plugin closed")
}

// instantiate creates a new WASM module instance from the compiled module.
func (p *Plugin) instantiate(ctx context.Context) (api.Module, error) {
	cfg := wazero.NewModuleConfig().
		WithName(""). // unnamed so multiple instances can coexist
		WithStartFunctions("_start", "_initialize")

	mod, err := p.runtime.InstantiateModule(ctx, p.compiled, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate module %s: %w", p.config.Name, err)
	}
	return mod, nil
}

// Middleware returns an http.Handler middleware that executes this plugin
// in the request and/or response phases based on its configured Phase.
func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Request phase
			if p.config.Phase == PhaseRequest || p.config.Phase == PhaseBoth {
				reqCtx := &RequestContext{
					Request:        r,
					ResponseWriter: w,
					Action:         ActionContinue,
				}

				action, err := p.ExecuteRequestPhase(ctx, reqCtx)
				if err != nil {
					p.logger.Error("WASM request phase error",
						zap.Error(err),
						zap.String("path", r.URL.Path),
					)
					// Fail-closed: reject request if configured, otherwise fail-open
					if p.config.FailClosed {
						http.Error(w, "Security policy error", http.StatusServiceUnavailable)
						return
					}
				}

				// The guest may have modified request headers
				r = reqCtx.Request

				if action == ActionPause {
					// Guest short-circuited — do not call next
					return
				}
			}

			// If response phase needed, wrap the response writer
			if p.config.Phase == PhaseResponse || p.config.Phase == PhaseBoth {
				rw := &wasmResponseWriter{
					ResponseWriter: w,
					plugin:         p,
					request:        r,
				}
				next.ServeHTTP(rw, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// wasmResponseWriter intercepts WriteHeader to run the response-phase plugin.
type wasmResponseWriter struct {
	http.ResponseWriter
	plugin      *Plugin
	request     *http.Request
	wroteHeader bool
}

func (rw *wasmResponseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true

	ctx := rw.request.Context()
	reqCtx := &RequestContext{
		Request:            rw.request,
		ResponseWriter:     rw.ResponseWriter,
		ResponseHeaders:    rw.Header(),
		ResponseStatusCode: code,
		Action:             ActionContinue,
	}

	_, err := rw.plugin.ExecuteResponsePhase(ctx, reqCtx)
	if err != nil {
		rw.plugin.logger.Error("WASM response phase error",
			zap.Error(err),
			zap.String("path", rw.request.URL.Path),
		)
	}

	rw.ResponseWriter.WriteHeader(code)
}

func (rw *wasmResponseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}
