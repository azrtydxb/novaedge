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
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewResponseFilter(t *testing.T) {
	filter := &pb.RouteFilter{
		Type: pb.RouteFilterType_RESPONSE_ADD_HEADER,
		ResponseAddHeaders: []*pb.HTTPHeader{
			{Name: "X-Custom", Value: "custom-value"},
		},
		ResponseSetHeaders: []*pb.HTTPHeader{
			{Name: "X-Set", Value: "set-value"},
		},
		ResponseRemoveHeaders: []string{"X-Remove"},
	}

	rf := NewResponseFilter(filter)

	if len(rf.AddHeaders) != 1 {
		t.Errorf("AddHeaders length = %d, want 1", len(rf.AddHeaders))
	}

	if len(rf.SetHeaders) != 1 {
		t.Errorf("SetHeaders length = %d, want 1", len(rf.SetHeaders))
	}

	if len(rf.RemoveHeaders) != 1 {
		t.Errorf("RemoveHeaders length = %d, want 1", len(rf.RemoveHeaders))
	}
}

func TestResponseFilter_HasModifications(t *testing.T) {
	tests := []struct {
		name     string
		filter   *ResponseFilter
		expected bool
	}{
		{
			name: "has add headers",
			filter: &ResponseFilter{
				AddHeaders: []*pb.HTTPHeader{{Name: "X-Test", Value: "test"}},
			},
			expected: true,
		},
		{
			name: "has set headers",
			filter: &ResponseFilter{
				SetHeaders: []*pb.HTTPHeader{{Name: "X-Test", Value: "test"}},
			},
			expected: true,
		},
		{
			name: "has remove headers",
			filter: &ResponseFilter{
				RemoveHeaders: []string{"X-Test"},
			},
			expected: true,
		},
		{
			name: "has all modifications",
			filter: &ResponseFilter{
				AddHeaders:    []*pb.HTTPHeader{{Name: "X-Add", Value: "add"}},
				SetHeaders:    []*pb.HTTPHeader{{Name: "X-Set", Value: "set"}},
				RemoveHeaders: []string{"X-Remove"},
			},
			expected: true,
		},
		{
			name:     "no modifications",
			filter:   &ResponseFilter{},
			expected: false,
		},
		{
			name: "empty slices",
			filter: &ResponseFilter{
				AddHeaders:    []*pb.HTTPHeader{},
				SetHeaders:    []*pb.HTTPHeader{},
				RemoveHeaders: []string{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.filter.HasModifications()
			if result != tt.expected {
				t.Errorf("HasModifications() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestResponseHeaderWriter_AddHeaders(t *testing.T) {
	filter := &ResponseFilter{
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-Custom-1", Value: "value1"},
			{Name: "X-Custom-2", Value: "value2"},
		},
	}

	w := httptest.NewRecorder()
	rw := NewResponseHeaderWriter(w, filter)

	// Write response
	rw.WriteHeader(http.StatusOK)
	_, _ = rw.Write([]byte("test body"))

	result := w.Result()
	defer result.Body.Close()

	if got := result.Header.Get("X-Custom-1"); got != "value1" {
		t.Errorf("X-Custom-1 = %q, want %q", got, "value1")
	}

	if got := result.Header.Get("X-Custom-2"); got != "value2" {
		t.Errorf("X-Custom-2 = %q, want %q", got, "value2")
	}
}

func TestResponseHeaderWriter_SetHeaders(t *testing.T) {
	filter := &ResponseFilter{
		SetHeaders: []*pb.HTTPHeader{
			{Name: "Content-Type", Value: "application/json"},
			{Name: "X-Custom", Value: "replaced"},
		},
	}

	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("X-Custom", "original")

	rw := NewResponseHeaderWriter(w, filter)

	rw.WriteHeader(http.StatusOK)

	result := w.Result()
	defer result.Body.Close()

	if got := result.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	if got := result.Header.Get("X-Custom"); got != "replaced" {
		t.Errorf("X-Custom = %q, want %q", got, "replaced")
	}
}

func TestResponseHeaderWriter_RemoveHeaders(t *testing.T) {
	filter := &ResponseFilter{
		RemoveHeaders: []string{"X-Remove-1", "X-Remove-2"},
	}

	w := httptest.NewRecorder()
	w.Header().Set("X-Remove-1", "should-be-removed")
	w.Header().Set("X-Remove-2", "also-removed")
	w.Header().Set("X-Keep", "should-stay")

	rw := NewResponseHeaderWriter(w, filter)

	rw.WriteHeader(http.StatusOK)

	result := w.Result()
	defer result.Body.Close()

	if got := result.Header.Get("X-Remove-1"); got != "" {
		t.Error("X-Remove-1 should be removed")
	}

	if got := result.Header.Get("X-Remove-2"); got != "" {
		t.Error("X-Remove-2 should be removed")
	}

	if got := result.Header.Get("X-Keep"); got != "should-stay" {
		t.Errorf("X-Keep = %q, want %q", got, "should-stay")
	}
}

func TestResponseHeaderWriter_OrderOfOperations(t *testing.T) {
	// Test that removal happens first, then set, then add
	filter := &ResponseFilter{
		RemoveHeaders: []string{"X-Test"},
		SetHeaders: []*pb.HTTPHeader{
			{Name: "X-Test", Value: "set-value"},
		},
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-Test", Value: "add-value"},
		},
	}

	w := httptest.NewRecorder()
	w.Header().Set("X-Test", "original")

	rw := NewResponseHeaderWriter(w, filter)

	rw.WriteHeader(http.StatusOK)

	result := w.Result()
	defer result.Body.Close()

	// Should have both set and add values
	values := result.Header.Values("X-Test")
	if len(values) != 2 {
		t.Fatalf("Expected 2 X-Test headers, got %d", len(values))
	}

	if values[0] != "set-value" {
		t.Errorf("First X-Test value = %q, want %q", values[0], "set-value")
	}

	if values[1] != "add-value" {
		t.Errorf("Second X-Test value = %q, want %q", values[1], "add-value")
	}
}

func TestResponseHeaderWriter_WriteWithoutHeader(t *testing.T) {
	filter := &ResponseFilter{
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-Auto-Added", Value: "auto"},
		},
	}

	w := httptest.NewRecorder()
	rw := NewResponseHeaderWriter(w, filter)

	// Write without calling WriteHeader explicitly
	_, _ = rw.Write([]byte("test body"))

	result := w.Result()
	defer result.Body.Close()

	if result.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", result.StatusCode, http.StatusOK)
	}

	if got := result.Header.Get("X-Auto-Added"); got != "auto" {
		t.Errorf("X-Auto-Added = %q, want %q", got, "auto")
	}
}

func TestResponseHeaderWriter_MultipleWriteHeaderCalls(t *testing.T) {
	filter := &ResponseFilter{
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-Test", Value: "test"},
		},
	}

	w := httptest.NewRecorder()
	rw := NewResponseHeaderWriter(w, filter)

	// First call should apply modifications
	rw.WriteHeader(http.StatusCreated)

	// Second call should be no-op
	rw.WriteHeader(http.StatusInternalServerError)

	result := w.Result()
	defer result.Body.Close()

	if result.StatusCode != http.StatusCreated {
		t.Errorf("Status = %d, want %d", result.StatusCode, http.StatusCreated)
	}

	if got := result.Header.Get("X-Test"); got != "test" {
		t.Errorf("X-Test = %q, want %q", got, "test")
	}
}

func TestResponseHeaderWriter_NilFilter(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Set("X-Original", "original")

	rw := NewResponseHeaderWriter(w, nil)

	rw.WriteHeader(http.StatusOK)
	_, _ = rw.Write([]byte("test"))

	result := w.Result()
	defer result.Body.Close()

	// Should not modify headers
	if got := result.Header.Get("X-Original"); got != "original" {
		t.Errorf("X-Original = %q, want %q", got, "original")
	}
}

func TestResponseHeaderWriter_Flush(t *testing.T) {
	filter := &ResponseFilter{
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-Flushed", Value: "yes"},
		},
	}

	w := httptest.NewRecorder()
	rw := NewResponseHeaderWriter(w, filter)

	// Flush should trigger header writes
	rw.Flush()

	result := w.Result()
	defer result.Body.Close()

	if result.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", result.StatusCode, http.StatusOK)
	}

	if got := result.Header.Get("X-Flushed"); got != "yes" {
		t.Errorf("X-Flushed = %q, want %q", got, "yes")
	}
}

func TestResponseHeaderWriter_Header(t *testing.T) {
	filter := &ResponseFilter{}

	w := httptest.NewRecorder()
	rw := NewResponseHeaderWriter(w, filter)

	// Should return the underlying header
	rw.Header().Set("X-Test", "test")

	if got := w.Header().Get("X-Test"); got != "test" {
		t.Error("Header() should return underlying header map")
	}
}

// Mock types for interface tests
type mockHijacker struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (m *mockHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	m.hijacked = true
	return nil, nil, errors.New("mock hijack")
}

type mockPusher struct {
	*httptest.ResponseRecorder
	pushed     bool
	pushTarget string
}

func (m *mockPusher) Push(target string, opts *http.PushOptions) error {
	m.pushed = true
	m.pushTarget = target
	return nil
}

func TestResponseHeaderWriter_Hijack(t *testing.T) {
	filter := &ResponseFilter{}

	mock := &mockHijacker{ResponseRecorder: httptest.NewRecorder()}
	rw := NewResponseHeaderWriter(mock, filter)

	_, _, err := rw.Hijack()

	if err == nil {
		t.Error("Expected error from Hijack")
	}

	if !mock.hijacked {
		t.Error("Hijack should be called on underlying writer")
	}
}

func TestResponseHeaderWriter_HijackNotSupported(t *testing.T) {
	filter := &ResponseFilter{}

	w := httptest.NewRecorder()
	rw := NewResponseHeaderWriter(w, filter)

	_, _, err := rw.Hijack()

	if !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("Expected ErrNotSupported, got %v", err)
	}
}

func TestResponseHeaderWriter_Push(t *testing.T) {
	filter := &ResponseFilter{}

	mock := &mockPusher{ResponseRecorder: httptest.NewRecorder()}
	rw := NewResponseHeaderWriter(mock, filter)

	err := rw.Push("/resource", nil)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !mock.pushed {
		t.Error("Push should be called on underlying writer")
	}

	if mock.pushTarget != "/resource" {
		t.Errorf("pushTarget = %q, want %q", mock.pushTarget, "/resource")
	}
}

func TestResponseHeaderWriter_PushNotSupported(t *testing.T) {
	filter := &ResponseFilter{}

	w := httptest.NewRecorder()
	rw := NewResponseHeaderWriter(w, filter)

	err := rw.Push("/resource", nil)

	if !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("Expected ErrNotSupported, got %v", err)
	}
}

func TestResponseHeaderWriter_Unwrap(t *testing.T) {
	filter := &ResponseFilter{}

	w := httptest.NewRecorder()
	rw := NewResponseHeaderWriter(w, filter)

	unwrapped := rw.Unwrap()

	if unwrapped != w {
		t.Error("Unwrap should return underlying ResponseWriter")
	}
}

func TestHandleResponseHeaders_WithModifications(t *testing.T) {
	filter := &ResponseFilter{
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-Middleware", Value: "applied"},
		},
	}

	middleware := HandleResponseHeaders(filter)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Handler", "set")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("response"))
	})

	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	result := w.Result()
	defer result.Body.Close()

	if got := result.Header.Get("X-Middleware"); got != "applied" {
		t.Errorf("X-Middleware = %q, want %q", got, "applied")
	}

	if got := result.Header.Get("X-Handler"); got != "set" {
		t.Errorf("X-Handler = %q, want %q", got, "set")
	}
}

func TestHandleResponseHeaders_NoModifications(t *testing.T) {
	filter := &ResponseFilter{}

	middleware := HandleResponseHeaders(filter)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Original", "original")
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	result := w.Result()
	defer result.Body.Close()

	if got := result.Header.Get("X-Original"); got != "original" {
		t.Errorf("X-Original = %q, want %q", got, "original")
	}
}

func TestHandleResponseHeaders_NilFilter(t *testing.T) {
	middleware := HandleResponseHeaders(nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "test")
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	result := w.Result()
	defer result.Body.Close()

	if got := result.Header.Get("X-Test"); got != "test" {
		t.Error("Should pass through without modifications")
	}
}

func TestCollectResponseFilters(t *testing.T) {
	tests := []struct {
		name                string
		filters             []*pb.RouteFilter
		expectedAddCount    int
		expectedSetCount    int
		expectedRemoveCount int
	}{
		{
			name:    "empty filters",
			filters: []*pb.RouteFilter{},
		},
		{
			name: "single add header filter",
			filters: []*pb.RouteFilter{
				{
					Type: pb.RouteFilterType_RESPONSE_ADD_HEADER,
					ResponseAddHeaders: []*pb.HTTPHeader{
						{Name: "X-Add-1", Value: "value1"},
						{Name: "X-Add-2", Value: "value2"},
					},
				},
			},
			expectedAddCount: 2,
		},
		{
			name: "single set header filter",
			filters: []*pb.RouteFilter{
				{
					Type: pb.RouteFilterType_RESPONSE_SET_HEADER,
					ResponseSetHeaders: []*pb.HTTPHeader{
						{Name: "X-Set", Value: "set-value"},
					},
				},
			},
			expectedSetCount: 1,
		},
		{
			name: "single remove header filter",
			filters: []*pb.RouteFilter{
				{
					Type:                  pb.RouteFilterType_RESPONSE_REMOVE_HEADER,
					ResponseRemoveHeaders: []string{"X-Remove-1", "X-Remove-2"},
				},
			},
			expectedRemoveCount: 2,
		},
		{
			name: "multiple filter types",
			filters: []*pb.RouteFilter{
				{
					Type: pb.RouteFilterType_RESPONSE_ADD_HEADER,
					ResponseAddHeaders: []*pb.HTTPHeader{
						{Name: "X-Add", Value: "add"},
					},
				},
				{
					Type: pb.RouteFilterType_RESPONSE_SET_HEADER,
					ResponseSetHeaders: []*pb.HTTPHeader{
						{Name: "X-Set", Value: "set"},
					},
				},
				{
					Type:                  pb.RouteFilterType_RESPONSE_REMOVE_HEADER,
					ResponseRemoveHeaders: []string{"X-Remove"},
				},
			},
			expectedAddCount:    1,
			expectedSetCount:    1,
			expectedRemoveCount: 1,
		},
		{
			name: "multiple filters of same type",
			filters: []*pb.RouteFilter{
				{
					Type: pb.RouteFilterType_RESPONSE_ADD_HEADER,
					ResponseAddHeaders: []*pb.HTTPHeader{
						{Name: "X-Add-1", Value: "value1"},
					},
				},
				{
					Type: pb.RouteFilterType_RESPONSE_ADD_HEADER,
					ResponseAddHeaders: []*pb.HTTPHeader{
						{Name: "X-Add-2", Value: "value2"},
						{Name: "X-Add-3", Value: "value3"},
					},
				},
			},
			expectedAddCount: 3,
		},
		{
			name: "non-response filters ignored",
			filters: []*pb.RouteFilter{
				{
					Type: pb.RouteFilterType_ADD_HEADER,
					AddHeaders: []*pb.HTTPHeader{
						{Name: "X-Request", Value: "request"},
					},
				},
				{
					Type: pb.RouteFilterType_RESPONSE_ADD_HEADER,
					ResponseAddHeaders: []*pb.HTTPHeader{
						{Name: "X-Response", Value: "response"},
					},
				},
			},
			expectedAddCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			combined := collectResponseFilters(tt.filters)

			if len(combined.AddHeaders) != tt.expectedAddCount {
				t.Errorf("AddHeaders count = %d, want %d", len(combined.AddHeaders), tt.expectedAddCount)
			}

			if len(combined.SetHeaders) != tt.expectedSetCount {
				t.Errorf("SetHeaders count = %d, want %d", len(combined.SetHeaders), tt.expectedSetCount)
			}

			if len(combined.RemoveHeaders) != tt.expectedRemoveCount {
				t.Errorf("RemoveHeaders count = %d, want %d", len(combined.RemoveHeaders), tt.expectedRemoveCount)
			}
		})
	}
}
