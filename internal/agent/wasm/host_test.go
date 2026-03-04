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
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHostModuleName(t *testing.T) {
	assert.Equal(t, "novaedge", hostModuleName)
}

func TestMaxUint32(t *testing.T) {
	assert.Equal(t, uint64(0xFFFFFFFF), uint64(maxUint32))
}

func TestSafeU32(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		expected uint32
	}{
		{
			name:     "zero",
			input:    0,
			expected: 0,
		},
		{
			name:     "small value",
			input:    42,
			expected: 42,
		},
		{
			name:     "max uint32",
			input:    maxUint32,
			expected: uint32(maxUint32),
		},
		{
			name:     "max uint32 + 1",
			input:    maxUint32 + 1,
			expected: 0,
		},
		{
			name:     "large value overflows",
			input:    uint64(0x100000000),
			expected: 0,
		},
		{
			name:     "very large value",
			input:    uint64(0xFFFFFFFFFFFFFFFF),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safeU32(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWithRequestContext(t *testing.T) {
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "GET", "/test", nil)
	rc := &RequestContext{
		Request: req,
	}

	newCtx := withRequestContext(ctx, rc)
	assert.NotNil(t, newCtx)

	retrieved, ok := getRequestContext(newCtx)
	assert.True(t, ok)
	assert.NotNil(t, retrieved)
	assert.Equal(t, "GET", retrieved.Request.Method)
	assert.Equal(t, "/test", retrieved.Request.URL.Path)
}

func TestGetRequestContext_Missing(t *testing.T) {
	ctx := context.Background()

	retrieved, ok := getRequestContext(ctx)
	assert.False(t, ok)
	assert.Nil(t, retrieved)
}

func TestGetRequestContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), requestContextKey, "not a request context")

	retrieved, ok := getRequestContext(ctx)
	assert.False(t, ok)
	assert.Nil(t, retrieved)
}

func TestRequestContext_Fields(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/v1/test", nil)
	req.Header.Set("Content-Type", "application/json")

	rc := &RequestContext{
		Request:            req,
		ResponseHeaders:    http.Header{"X-Custom": []string{"value"}},
		ResponseStatusCode: 200,
		Action:             ActionContinue,
		PluginConfig:       map[string]string{"key": "config-value"},
	}

	assert.Equal(t, "POST", rc.Request.Method)
	assert.Equal(t, "/api/v1/test", rc.Request.URL.Path)
	assert.Equal(t, "application/json", rc.Request.Header.Get("Content-Type"))
	assert.Equal(t, "value", rc.ResponseHeaders.Get("X-Custom"))
	assert.Equal(t, 200, rc.ResponseStatusCode)
	assert.Equal(t, ActionContinue, rc.Action)
	assert.Equal(t, "config-value", rc.PluginConfig["key"])
}

func TestHostFuncNamesValues(t *testing.T) {
	// Test that host function names have expected values
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
