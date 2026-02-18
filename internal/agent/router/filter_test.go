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

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
	"github.com/stretchr/testify/assert"
)

func TestSanitizeHeaderValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal value",
			input:    "test-value",
			expected: "test-value",
		},
		{
			name:     "value with CR",
			input:    "test\rvalue",
			expected: "testvalue",
		},
		{
			name:     "value with LF",
			input:    "test\nvalue",
			expected: "testvalue",
		},
		{
			name:     "value with CRLF",
			input:    "test\r\nvalue",
			expected: "testvalue",
		},
		{
			name:     "value with multiple CRLF",
			input:    "test\r\n\r\nvalue",
			expected: "testvalue",
		},
		{
			name:     "empty value",
			input:    "",
			expected: "",
		},
		{
			name:     "only CRLF",
			input:    "\r\n",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeHeaderValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewHeaderModifierFilter(t *testing.T) {
	filter := &pb.RouteFilter{
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-Custom", Value: "test"},
		},
		RemoveHeaders: []string{"X-Remove"},
	}

	hmf := NewHeaderModifierFilter(filter)
	assert.NotNil(t, hmf)
	assert.Equal(t, filter, hmf.filter)
}

func TestHeaderModifierFilter_Apply_AddHeaders(t *testing.T) {
	filter := &pb.RouteFilter{
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-Custom", Value: "test-value"},
			{Name: "X-Another", Value: "another-value"},
		},
	}

	hmf := NewHeaderModifierFilter(filter)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	modifiedReq, proceed := hmf.Apply(rec, req)
	assert.True(t, proceed)
	assert.Equal(t, "test-value", modifiedReq.Header.Get("X-Custom"))
	assert.Equal(t, "another-value", modifiedReq.Header.Get("X-Another"))
}

func TestHeaderModifierFilter_Apply_RemoveHeaders(t *testing.T) {
	filter := &pb.RouteFilter{
		RemoveHeaders: []string{"X-Remove", "X-Another-Remove"},
	}

	hmf := NewHeaderModifierFilter(filter)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Remove", "should-be-removed")
	req.Header.Set("X-Another-Remove", "also-removed")
	req.Header.Set("X-Keep", "should-remain")
	rec := httptest.NewRecorder()

	modifiedReq, proceed := hmf.Apply(rec, req)
	assert.True(t, proceed)
	assert.Empty(t, modifiedReq.Header.Get("X-Remove"))
	assert.Empty(t, modifiedReq.Header.Get("X-Another-Remove"))
	assert.Equal(t, "should-remain", modifiedReq.Header.Get("X-Keep"))
}

func TestHeaderModifierFilter_Apply_SanitizeHeaders(t *testing.T) {
	filter := &pb.RouteFilter{
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-Injected", Value: "test\r\nX-Evil: malicious"},
		},
	}

	hmf := NewHeaderModifierFilter(filter)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	modifiedReq, proceed := hmf.Apply(rec, req)
	assert.True(t, proceed)
	// CRLF should be sanitized
	assert.Equal(t, "testX-Evil: malicious", modifiedReq.Header.Get("X-Injected"))
}

func TestNewRedirectFilter(t *testing.T) {
	filter := &pb.RouteFilter{
		RedirectUrl: "https://example.com",
	}

	rf := NewRedirectFilter(filter)
	assert.NotNil(t, rf)
	assert.Equal(t, filter, rf.filter)
}

func TestRedirectFilter_Apply_WithRedirect(t *testing.T) {
	filter := &pb.RouteFilter{
		RedirectUrl: "https://example.com/new-location",
	}

	rf := NewRedirectFilter(filter)
	req := httptest.NewRequest("GET", "/old-location", nil)
	rec := httptest.NewRecorder()

	modifiedReq, proceed := rf.Apply(rec, req)
	assert.False(t, proceed) // Should stop processing
	assert.NotNil(t, modifiedReq)
	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "https://example.com/new-location", rec.Header().Get("Location"))
}

func TestRedirectFilter_Apply_NoRedirect(t *testing.T) {
	filter := &pb.RouteFilter{
		RedirectUrl: "",
	}

	rf := NewRedirectFilter(filter)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	modifiedReq, proceed := rf.Apply(rec, req)
	assert.True(t, proceed) // Should continue processing
	assert.NotNil(t, modifiedReq)
}
