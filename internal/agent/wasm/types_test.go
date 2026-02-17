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
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPhaseString(t *testing.T) {
	tests := []struct {
		name     string
		phase    Phase
		expected string
	}{
		{
			name:     "request phase",
			phase:    PhaseRequest,
			expected: "request",
		},
		{
			name:     "response phase",
			phase:    PhaseResponse,
			expected: "response",
		},
		{
			name:     "both phase",
			phase:    PhaseBoth,
			expected: "both",
		},
		{
			name:     "unknown phase",
			phase:    Phase(99),
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.phase.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParsePhaseFromTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Phase
	}{
		{
			name:     "parse request",
			input:    "request",
			expected: PhaseRequest,
		},
		{
			name:     "parse response",
			input:    "response",
			expected: PhaseResponse,
		},
		{
			name:     "parse both",
			input:    "both",
			expected: PhaseBoth,
		},
		{
			name:     "parse unknown defaults to request",
			input:    "unknown",
			expected: PhaseRequest,
		},
		{
			name:     "parse empty defaults to request",
			input:    "",
			expected: PhaseRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParsePhase(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPluginConfig(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		config := PluginConfig{}
		assert.Empty(t, config.Name)
		assert.Empty(t, config.Source)
		assert.Nil(t, config.WASMBytes)
		assert.Nil(t, config.Config)
		assert.Zero(t, config.Phase)
		assert.Zero(t, config.Priority)
		assert.Zero(t, config.MaxMemoryPages)
		assert.False(t, config.FailClosed)
		assert.Zero(t, config.ExecutionTimeout)
	})

	t.Run("with values", func(t *testing.T) {
		config := PluginConfig{
			Name:             "test-plugin",
			Source:           "configmap:test",
			WASMBytes:        []byte{0x00, 0x61, 0x73, 0x6d},
			Config:           map[string]string{"key": "value"},
			Phase:            PhaseRequest,
			Priority:         100,
			MaxMemoryPages:   256,
			FailClosed:       true,
			ExecutionTimeout: 10 * time.Second,
		}

		assert.Equal(t, "test-plugin", config.Name)
		assert.Equal(t, "configmap:test", config.Source)
		assert.Equal(t, []byte{0x00, 0x61, 0x73, 0x6d}, config.WASMBytes)
		assert.Equal(t, map[string]string{"key": "value"}, config.Config)
		assert.Equal(t, PhaseRequest, config.Phase)
		assert.Equal(t, 100, config.Priority)
		assert.Equal(t, uint32(256), config.MaxMemoryPages)
		assert.True(t, config.FailClosed)
		assert.Equal(t, 10*time.Second, config.ExecutionTimeout)
	})
}

func TestRequestContext(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		ctx := RequestContext{}
		assert.Nil(t, ctx.Request)
		assert.Nil(t, ctx.ResponseWriter)
		assert.Nil(t, ctx.ResponseHeaders)
		assert.Zero(t, ctx.ResponseStatusCode)
		assert.Zero(t, ctx.Action)
		assert.Nil(t, ctx.PluginConfig)
	})

	t.Run("with values", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rw := httptest.NewRecorder()
		respHeaders := http.Header{"X-Custom": []string{"value"}}

		ctx := RequestContext{
			Request:            req,
			ResponseWriter:     rw,
			ResponseHeaders:    respHeaders,
			ResponseStatusCode: http.StatusOK,
			Action:             ActionPause,
			PluginConfig:       map[string]string{"key": "value"},
		}

		assert.Equal(t, req, ctx.Request)
		assert.Equal(t, rw, ctx.ResponseWriter)
		assert.Equal(t, respHeaders, ctx.ResponseHeaders)
		assert.Equal(t, http.StatusOK, ctx.ResponseStatusCode)
		assert.Equal(t, ActionPause, ctx.Action)
		assert.Equal(t, map[string]string{"key": "value"}, ctx.PluginConfig)
	})
}

func TestActionConstants(t *testing.T) {
	// Verify Action constants have expected values
	assert.Equal(t, Action(0), ActionContinue)
	assert.Equal(t, Action(1), ActionPause)
}

func TestPhaseConstants(t *testing.T) {
	// Verify Phase constants have expected values
	assert.Equal(t, Phase(0), PhaseRequest)
	assert.Equal(t, Phase(1), PhaseResponse)
	assert.Equal(t, Phase(2), PhaseBoth)
}

func TestHostFuncNames(t *testing.T) {
	// Verify host function names are set correctly
	assert.Equal(t, "get_request_header", hostFuncNames.GetRequestHeader)
	assert.Equal(t, "set_request_header", hostFuncNames.SetRequestHeader)
	assert.Equal(t, "get_response_header", hostFuncNames.GetResponseHeader)
	assert.Equal(t, "set_response_header", hostFuncNames.SetResponseHeader)
	assert.Equal(t, "get_method", hostFuncNames.GetMethod)
	assert.Equal(t, "get_path", hostFuncNames.GetPath)
	assert.Equal(t, "get_config_value", hostFuncNames.GetConfigValue)
	assert.Equal(t, "log_message", hostFuncNames.LogMessage)
	assert.Equal(t, "send_response", hostFuncNames.SendResponse)
}

func TestGuestExportNames(t *testing.T) {
	// Verify guest export names are set correctly
	assert.Equal(t, "on_request_headers", guestExportNames.OnRequestHeaders)
	assert.Equal(t, "on_response_headers", guestExportNames.OnResponseHeaders)
	assert.Equal(t, "malloc", guestExportNames.Malloc)
	assert.Equal(t, "free", guestExportNames.Free)
}
