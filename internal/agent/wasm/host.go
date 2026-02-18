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

	"github.com/tetratelabs/wazero/api"
	"go.uber.org/zap"
)

// hostModuleName is the WASM module name that exposes host functions.
const hostModuleName = "novaedge"

// maxUint32 is the maximum value for uint32, used for safe conversion checks.
const maxUint32 = 1<<32 - 1

// safeU32 safely converts a uint64 stack value to uint32.
// WASM i32 values are stored as uint64 on the Go side but are guaranteed
// by the WASM runtime to fit in 32 bits when the function signature
// declares ValueTypeI32.
func safeU32(v uint64) uint32 {
	if v > maxUint32 {
		return 0
	}
	return uint32(v)
}

// contextKey is an unexported type to avoid collisions in context.WithValue.
type contextKey int

const requestContextKey contextKey = 0

// withRequestContext stores a RequestContext in a context.Context.
func withRequestContext(ctx context.Context, rc *RequestContext) context.Context {
	return context.WithValue(ctx, requestContextKey, rc)
}

// getRequestContext retrieves the RequestContext from a context.Context.
func getRequestContext(ctx context.Context) (*RequestContext, bool) {
	rc, ok := ctx.Value(requestContextKey).(*RequestContext)
	return rc, ok
}

// registerHostModule registers the host function module with the WASM runtime.
func (r *Runtime) registerHostModule(ctx context.Context) error {
	builder := r.runtime.NewHostModuleBuilder(hostModuleName)

	// get_request_header(name_ptr, name_len, value_ptr, value_cap) -> value_len
	builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(hostGetRequestHeader), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export(hostFuncNames.GetRequestHeader)

	// set_request_header(name_ptr, name_len, value_ptr, value_len)
	builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(hostSetRequestHeader), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export(hostFuncNames.SetRequestHeader)

	// get_response_header(name_ptr, name_len, value_ptr, value_cap) -> value_len
	builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(hostGetResponseHeader), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export(hostFuncNames.GetResponseHeader)

	// set_response_header(name_ptr, name_len, value_ptr, value_len)
	builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(hostSetResponseHeader), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export(hostFuncNames.SetResponseHeader)

	// get_method(buf_ptr, buf_cap) -> method_len
	builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(hostGetMethod), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export(hostFuncNames.GetMethod)

	// get_path(buf_ptr, buf_cap) -> path_len
	builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(hostGetPath), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export(hostFuncNames.GetPath)

	// get_config_value(key_ptr, key_len, value_ptr, value_cap) -> value_len
	builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(hostGetConfigValue), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export(hostFuncNames.GetConfigValue)

	// log_message(level, msg_ptr, msg_len)
	builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(hostLogMessage), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export(hostFuncNames.LogMessage)

	// send_response(status_code, body_ptr, body_len) — short-circuits the chain
	builder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(hostSendResponse), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export(hostFuncNames.SendResponse)

	_, err := builder.Instantiate(ctx)
	return err
}

// --- Host function implementations ---

func hostGetRequestHeader(ctx context.Context, mod api.Module, stack []uint64) {
	rc, ok := getRequestContext(ctx)
	if !ok || rc.Request == nil {
		stack[0] = 0
		return
	}
	namePtr := safeU32(stack[0])
	nameLen := safeU32(stack[1])
	valPtr := safeU32(stack[2])
	valCap := safeU32(stack[3])

	mem := mod.Memory()
	nameBytes, ok := mem.Read(namePtr, nameLen)
	if !ok {
		zap.L().Debug("WASM host: memory read failed", zap.String("function", "hostGetRequestHeader"))
		stack[0] = 0
		return
	}

	value := rc.Request.Header.Get(string(nameBytes))
	written := writeStringToMemory(mem, valPtr, valCap, value)
	stack[0] = uint64(written)
}

func hostSetRequestHeader(ctx context.Context, mod api.Module, stack []uint64) {
	rc, ok := getRequestContext(ctx)
	if !ok || rc.Request == nil {
		return
	}
	namePtr := safeU32(stack[0])
	nameLen := safeU32(stack[1])
	valPtr := safeU32(stack[2])
	valLen := safeU32(stack[3])

	mem := mod.Memory()
	nameBytes, ok := mem.Read(namePtr, nameLen)
	if !ok {
		zap.L().Debug("WASM host: memory read failed", zap.String("function", "hostSetRequestHeader"))
		return
	}
	valBytes, ok := mem.Read(valPtr, valLen)
	if !ok {
		zap.L().Debug("WASM host: memory read failed", zap.String("function", "hostSetRequestHeader"))
		return
	}
	rc.Request.Header.Set(string(nameBytes), string(valBytes))
}

func hostGetResponseHeader(ctx context.Context, mod api.Module, stack []uint64) {
	rc, ok := getRequestContext(ctx)
	if !ok || rc.ResponseHeaders == nil {
		stack[0] = 0
		return
	}
	namePtr := safeU32(stack[0])
	nameLen := safeU32(stack[1])
	valPtr := safeU32(stack[2])
	valCap := safeU32(stack[3])

	mem := mod.Memory()
	nameBytes, ok := mem.Read(namePtr, nameLen)
	if !ok {
		zap.L().Debug("WASM host: memory read failed", zap.String("function", "hostGetResponseHeader"))
		stack[0] = 0
		return
	}

	value := rc.ResponseHeaders.Get(string(nameBytes))
	written := writeStringToMemory(mem, valPtr, valCap, value)
	stack[0] = uint64(written)
}

func hostSetResponseHeader(ctx context.Context, mod api.Module, stack []uint64) {
	rc, ok := getRequestContext(ctx)
	if !ok {
		return
	}
	namePtr := safeU32(stack[0])
	nameLen := safeU32(stack[1])
	valPtr := safeU32(stack[2])
	valLen := safeU32(stack[3])

	mem := mod.Memory()
	nameBytes, ok := mem.Read(namePtr, nameLen)
	if !ok {
		zap.L().Debug("WASM host: memory read failed", zap.String("function", "hostSetResponseHeader"))
		return
	}
	valBytes, ok := mem.Read(valPtr, valLen)
	if !ok {
		zap.L().Debug("WASM host: memory read failed", zap.String("function", "hostSetResponseHeader"))
		return
	}
	if rc.ResponseHeaders == nil {
		return
	}
	rc.ResponseHeaders.Set(string(nameBytes), string(valBytes))
}

func hostGetMethod(ctx context.Context, mod api.Module, stack []uint64) {
	rc, ok := getRequestContext(ctx)
	if !ok || rc.Request == nil {
		stack[0] = 0
		return
	}
	bufPtr := safeU32(stack[0])
	bufCap := safeU32(stack[1])

	mem := mod.Memory()
	written := writeStringToMemory(mem, bufPtr, bufCap, rc.Request.Method)
	stack[0] = uint64(written)
}

func hostGetPath(ctx context.Context, mod api.Module, stack []uint64) {
	rc, ok := getRequestContext(ctx)
	if !ok || rc.Request == nil {
		stack[0] = 0
		return
	}
	bufPtr := safeU32(stack[0])
	bufCap := safeU32(stack[1])

	mem := mod.Memory()
	written := writeStringToMemory(mem, bufPtr, bufCap, rc.Request.URL.Path)
	stack[0] = uint64(written)
}

func hostGetConfigValue(ctx context.Context, mod api.Module, stack []uint64) {
	rc, ok := getRequestContext(ctx)
	if !ok || rc.PluginConfig == nil {
		stack[0] = 0
		return
	}
	keyPtr := safeU32(stack[0])
	keyLen := safeU32(stack[1])
	valPtr := safeU32(stack[2])
	valCap := safeU32(stack[3])

	mem := mod.Memory()
	keyBytes, ok := mem.Read(keyPtr, keyLen)
	if !ok {
		zap.L().Debug("WASM host: memory read failed", zap.String("function", "hostGetConfigValue"))
		stack[0] = 0
		return
	}

	value, exists := rc.PluginConfig[string(keyBytes)]
	if !exists {
		stack[0] = 0
		return
	}
	written := writeStringToMemory(mem, valPtr, valCap, value)
	stack[0] = uint64(written)
}

func hostLogMessage(_ context.Context, mod api.Module, stack []uint64) {
	level := safeU32(stack[0])
	msgPtr := safeU32(stack[1])
	msgLen := safeU32(stack[2])

	mem := mod.Memory()
	msgBytes, ok := mem.Read(msgPtr, msgLen)
	if !ok {
		zap.L().Debug("WASM host: memory read failed", zap.String("function", "hostLogMessage"))
		return
	}

	msg := string(msgBytes)

	// Use a package-level logger if we had one; for now use zap global.
	// In production the logger is injected via Plugin.
	switch level {
	case 0: // debug
		zap.L().Debug("wasm plugin", zap.String("msg", msg))
	case 1: // info
		zap.L().Info("wasm plugin", zap.String("msg", msg))
	case 2: // warn
		zap.L().Warn("wasm plugin", zap.String("msg", msg))
	default: // error
		zap.L().Error("wasm plugin", zap.String("msg", msg))
	}
}

func hostSendResponse(ctx context.Context, mod api.Module, stack []uint64) {
	rc, ok := getRequestContext(ctx)
	if !ok || rc.ResponseWriter == nil {
		return
	}
	statusCode := safeU32(stack[0])
	bodyPtr := safeU32(stack[1])
	bodyLen := safeU32(stack[2])

	mem := mod.Memory()
	bodyBytes, ok := mem.Read(bodyPtr, bodyLen)
	if !ok {
		zap.L().Debug("WASM host: memory read failed", zap.String("function", "hostSendResponse"))
		bodyBytes = []byte("WASM plugin error: could not read body from memory")
	}

	rc.ResponseWriter.WriteHeader(int(statusCode))
	_, _ = rc.ResponseWriter.Write(bodyBytes)
	rc.Action = ActionPause
}

// writeStringToMemory copies a Go string into WASM linear memory, returning
// the number of bytes written (capped at bufCap). If the value is larger than
// bufCap it is truncated. Returns 0 on failure.
func writeStringToMemory(mem api.Memory, ptr, bufCap uint32, value string) uint32 {
	if bufCap == 0 || value == "" {
		return 0
	}
	b := []byte(value)
	if len(b) > int(bufCap) {
		b = b[:bufCap]
	}
	if !mem.Write(ptr, b) {
		zap.L().Debug("WASM host: memory write failed", zap.String("function", "writeStringToMemory"))
		return 0
	}
	if len(b) > int(maxUint32) {
		return 0
	}
	return uint32(len(b)) //nolint:gosec // length is bounded by bufCap which is uint32
}
