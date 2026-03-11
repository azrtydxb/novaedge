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
	"net/http"
	"time"
)

// Phase indicates at which point in the request lifecycle a plugin executes.
type Phase int

const (
	// PhaseRequest is invoked on the incoming request before routing/backend.
	PhaseRequest Phase = iota
	// PhaseResponse is invoked on the outgoing response from the backend.
	PhaseResponse
	// PhaseBoth executes in both request and response phases.
	PhaseBoth
)

// String returns a human-readable name for the phase.
func (p Phase) String() string {
	switch p {
	case PhaseRequest:
		return "request"
	case PhaseResponse:
		return "response"
	case PhaseBoth:
		return "both"
	default:
		return "unknown"
	}
}

// ParsePhase converts a string to a Phase.
func ParsePhase(s string) Phase {
	switch s {
	case "request":
		return PhaseRequest
	case "response":
		return PhaseResponse
	case "both":
		return PhaseBoth
	default:
		return PhaseRequest
	}
}

// Action indicates the result of a WASM plugin invocation.
type Action int

const (
	// ActionContinue tells the host to continue processing.
	ActionContinue Action = iota
	// ActionPause tells the host to stop further pipeline processing
	// (the plugin has already written a response).
	ActionPause
)

// PluginConfig holds static configuration passed to a WASM plugin.
type PluginConfig struct {
	// Name is the unique name of the plugin instance.
	Name string
	// Source identifies where the WASM binary comes from (ConfigMap ref, etc.).
	Source string
	// WASMBytes is the raw WASM module binary.
	WASMBytes []byte
	// Config is an arbitrary key-value configuration map exposed to the guest.
	Config map[string]string
	// Phase determines when the plugin executes.
	Phase Phase
	// Priority determines ordering (lower = earlier in the chain).
	Priority int
	// MaxMemoryPages caps the WASM linear memory in 64KiB pages (0 = default 256 = 16MB).
	MaxMemoryPages uint32
	// FailClosed causes the middleware to return 503 on WASM execution errors
	// instead of failing open and forwarding the request to the next handler.
	FailClosed bool
	// ExecutionTimeout is the maximum time a single plugin invocation may run.
	// Zero means use the default timeout (5 seconds).
	ExecutionTimeout time.Duration
}

// RequestContext is an opaque handle passed through the WASM execution that
// carries per-request state between the Go host and the WASM guest.
type RequestContext struct {
	// Request is the in-flight HTTP request (headers may be mutated by the plugin).
	Request *http.Request
	// ResponseWriter is the downstream response writer for short-circuit responses.
	ResponseWriter http.ResponseWriter
	// ResponseHeaders holds response headers that may be mutated by response-phase plugins.
	ResponseHeaders http.Header
	// ResponseStatusCode holds the response status code for response-phase plugins.
	ResponseStatusCode int
	// Action is set by the guest to indicate what the host should do next.
	Action Action
	// PluginConfig gives the guest access to its static configuration.
	PluginConfig map[string]string
}

// hostFuncNames enumerates the ABI function names exposed from Go to WASM guests.
var hostFuncNames = struct {
	GetRequestHeader  string
	SetRequestHeader  string
	GetResponseHeader string
	SetResponseHeader string
	GetMethod         string
	GetPath           string
	GetConfigValue    string
	LogMessage        string
	SendResponse      string
}{
	GetRequestHeader:  "get_request_header",
	SetRequestHeader:  "set_request_header",
	GetResponseHeader: "get_response_header",
	SetResponseHeader: "set_response_header",
	GetMethod:         "get_method",
	GetPath:           "get_path",
	GetConfigValue:    "get_config_value",
	LogMessage:        "log_message",
	SendResponse:      "send_response",
}

// guestExportNames enumerates the function names the WASM guest must export.
var guestExportNames = struct {
	OnRequestHeaders  string
	OnResponseHeaders string
	Malloc            string
	Free              string
}{
	OnRequestHeaders:  "on_request_headers",
	OnResponseHeaders: "on_response_headers",
	Malloc:            "malloc",
	Free:              "free",
}
