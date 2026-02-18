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
)

func TestSanitizeHeaderValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean value",
			input:    "value",
			expected: "value",
		},
		{
			name:     "value with CR",
			input:    "value\rinjection",
			expected: "valueinjection",
		},
		{
			name:     "value with LF",
			input:    "value\ninjection",
			expected: "valueinjection",
		},
		{
			name:     "value with CRLF",
			input:    "value\r\nSet-Cookie: injected=true",
			expected: "valueSet-Cookie: injected=true",
		},
		{
			name:     "multiple CRLF",
			input:    "val\r\nue\r\ntest\r\n",
			expected: "valuetest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeHeaderValue(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeHeaderValue(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHeaderModifierFilter_AddHeaders(t *testing.T) {
	filter := &pb.RouteFilter{
		Type: pb.RouteFilterType_ADD_HEADER,
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-Custom-Header", Value: "test-value"},
			{Name: "X-Another-Header", Value: "another-value"},
		},
	}

	f := NewHeaderModifierFilter(filter)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	newReq, shouldContinue := f.Apply(w, req)

	if !shouldContinue {
		t.Error("Expected filter to continue processing")
	}

	if got := newReq.Header.Get("X-Custom-Header"); got != "test-value" {
		t.Errorf("X-Custom-Header = %q, want %q", got, "test-value")
	}

	if got := newReq.Header.Get("X-Another-Header"); got != "another-value" {
		t.Errorf("X-Another-Header = %q, want %q", got, "another-value")
	}
}

func TestHeaderModifierFilter_AddHeadersSanitization(t *testing.T) {
	filter := &pb.RouteFilter{
		Type: pb.RouteFilterType_ADD_HEADER,
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-Injected", Value: "value\r\nX-Malicious: injected"},
		},
	}

	f := NewHeaderModifierFilter(filter)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	newReq, _ := f.Apply(w, req)

	got := newReq.Header.Get("X-Injected")
	if got != "valueX-Malicious: injected" {
		t.Errorf("Header value not properly sanitized: got %q", got)
	}
}

func TestHeaderModifierFilter_RemoveHeaders(t *testing.T) {
	filter := &pb.RouteFilter{
		Type:          pb.RouteFilterType_REMOVE_HEADER,
		RemoveHeaders: []string{"X-Remove-Me", "Authorization"},
	}

	f := NewHeaderModifierFilter(filter)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Remove-Me", "should-be-removed")
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Keep-Me", "should-stay")
	w := httptest.NewRecorder()

	newReq, shouldContinue := f.Apply(w, req)

	if !shouldContinue {
		t.Error("Expected filter to continue processing")
	}

	if newReq.Header.Get("X-Remove-Me") != "" {
		t.Error("X-Remove-Me header should be removed")
	}

	if newReq.Header.Get("Authorization") != "" {
		t.Error("Authorization header should be removed")
	}

	if got := newReq.Header.Get("X-Keep-Me"); got != "should-stay" {
		t.Errorf("X-Keep-Me = %q, want %q", got, "should-stay")
	}
}

func TestHeaderModifierFilter_AddAndRemove(t *testing.T) {
	filter := &pb.RouteFilter{
		Type: pb.RouteFilterType_ADD_HEADER,
		AddHeaders: []*pb.HTTPHeader{
			{Name: "X-New-Header", Value: "new-value"},
		},
		RemoveHeaders: []string{"X-Old-Header"},
	}

	f := NewHeaderModifierFilter(filter)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Old-Header", "old-value")
	w := httptest.NewRecorder()

	newReq, _ := f.Apply(w, req)

	if newReq.Header.Get("X-Old-Header") != "" {
		t.Error("X-Old-Header should be removed")
	}

	if got := newReq.Header.Get("X-New-Header"); got != "new-value" {
		t.Errorf("X-New-Header = %q, want %q", got, "new-value")
	}
}

func TestRedirectFilter_WithURL(t *testing.T) {
	filter := &pb.RouteFilter{
		Type:        pb.RouteFilterType_REQUEST_REDIRECT,
		RedirectUrl: "https://example.com/redirect",
	}

	f := NewRedirectFilter(filter)
	req := httptest.NewRequest(http.MethodGet, "/original", nil)
	w := httptest.NewRecorder()

	_, shouldContinue := f.Apply(w, req)

	if shouldContinue {
		t.Error("Expected filter to stop processing after redirect")
	}

	result := w.Result()
	defer result.Body.Close()

	if result.StatusCode != http.StatusFound {
		t.Errorf("Status = %d, want %d", result.StatusCode, http.StatusFound)
	}

	if location := result.Header.Get("Location"); location != "https://example.com/redirect" {
		t.Errorf("Location = %q, want %q", location, "https://example.com/redirect")
	}
}

func TestRedirectFilter_EmptyURL(t *testing.T) {
	filter := &pb.RouteFilter{
		Type:        pb.RouteFilterType_REQUEST_REDIRECT,
		RedirectUrl: "",
	}

	f := NewRedirectFilter(filter)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	_, shouldContinue := f.Apply(w, req)

	if !shouldContinue {
		t.Error("Expected filter to continue when redirect URL is empty")
	}

	result := w.Result()
	defer result.Body.Close()

	// Should not write any response
	if result.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", result.StatusCode, http.StatusOK)
	}
}

func TestURLRewriteFilter_SimplePath(t *testing.T) {
	filter := &pb.RouteFilter{
		Type:        pb.RouteFilterType_URL_REWRITE,
		RewritePath: "/new/path",
	}

	f := NewURLRewriteFilter(filter)
	req := httptest.NewRequest(http.MethodGet, "/old/path", nil)
	w := httptest.NewRecorder()

	newReq, shouldContinue := f.Apply(w, req)

	if !shouldContinue {
		t.Error("Expected filter to continue processing")
	}

	if newReq.URL.Path != "/new/path" {
		t.Errorf("Path = %q, want %q", newReq.URL.Path, "/new/path")
	}

	if newReq.RequestURI != "/new/path" {
		t.Errorf("RequestURI = %q, want %q", newReq.RequestURI, "/new/path")
	}
}

func TestURLRewriteFilter_PathWithQuery(t *testing.T) {
	filter := &pb.RouteFilter{
		Type:        pb.RouteFilterType_URL_REWRITE,
		RewritePath: "/rewritten",
	}

	f := NewURLRewriteFilter(filter)
	req := httptest.NewRequest(http.MethodGet, "/original?foo=bar&baz=qux", nil)
	w := httptest.NewRecorder()

	newReq, shouldContinue := f.Apply(w, req)

	if !shouldContinue {
		t.Error("Expected filter to continue processing")
	}

	if newReq.URL.Path != "/rewritten" {
		t.Errorf("Path = %q, want %q", newReq.URL.Path, "/rewritten")
	}

	if newReq.URL.RawQuery != "foo=bar&baz=qux" {
		t.Errorf("RawQuery = %q, want %q", newReq.URL.RawQuery, "foo=bar&baz=qux")
	}

	expectedURI := "/rewritten?foo=bar&baz=qux"
	if newReq.RequestURI != expectedURI {
		t.Errorf("RequestURI = %q, want %q", newReq.RequestURI, expectedURI)
	}
}

func TestURLRewriteFilter_EmptyPath(t *testing.T) {
	filter := &pb.RouteFilter{
		Type:        pb.RouteFilterType_URL_REWRITE,
		RewritePath: "",
	}

	f := NewURLRewriteFilter(filter)
	req := httptest.NewRequest(http.MethodGet, "/original", nil)
	originalPath := req.URL.Path
	w := httptest.NewRecorder()

	newReq, shouldContinue := f.Apply(w, req)

	if !shouldContinue {
		t.Error("Expected filter to continue when rewrite path is empty")
	}

	if newReq.URL.Path != originalPath {
		t.Errorf("Path should remain unchanged: got %q, want %q", newReq.URL.Path, originalPath)
	}
}

func TestBuildFilters(t *testing.T) {
	tests := []struct {
		name           string
		pbFilters      []*pb.RouteFilter
		expectedCount  int
		expectedTypes  []string
	}{
		{
			name:          "empty filters",
			pbFilters:     []*pb.RouteFilter{},
			expectedCount: 0,
		},
		{
			name: "single header filter",
			pbFilters: []*pb.RouteFilter{
				{
					Type: pb.RouteFilterType_ADD_HEADER,
					AddHeaders: []*pb.HTTPHeader{
						{Name: "X-Test", Value: "test"},
					},
				},
			},
			expectedCount: 1,
			expectedTypes: []string{"*router.HeaderModifierFilter"},
		},
		{
			name: "single redirect filter",
			pbFilters: []*pb.RouteFilter{
				{
					Type:        pb.RouteFilterType_REQUEST_REDIRECT,
					RedirectUrl: "https://example.com",
				},
			},
			expectedCount: 1,
			expectedTypes: []string{"*router.RedirectFilter"},
		},
		{
			name: "single rewrite filter",
			pbFilters: []*pb.RouteFilter{
				{
					Type:        pb.RouteFilterType_URL_REWRITE,
					RewritePath: "/new",
				},
			},
			expectedCount: 1,
			expectedTypes: []string{"*router.URLRewriteFilter"},
		},
		{
			name: "multiple filters",
			pbFilters: []*pb.RouteFilter{
				{
					Type: pb.RouteFilterType_ADD_HEADER,
					AddHeaders: []*pb.HTTPHeader{
						{Name: "X-First", Value: "first"},
					},
				},
				{
					Type:        pb.RouteFilterType_URL_REWRITE,
					RewritePath: "/api",
				},
				{
					Type:          pb.RouteFilterType_REMOVE_HEADER,
					RemoveHeaders: []string{"X-Remove"},
				},
			},
			expectedCount: 3,
			expectedTypes: []string{
				"*router.HeaderModifierFilter",
				"*router.URLRewriteFilter",
				"*router.HeaderModifierFilter",
			},
		},
		{
			name: "unknown filter type",
			pbFilters: []*pb.RouteFilter{
				{
					Type: pb.RouteFilterType_ROUTE_FILTER_TYPE_UNSPECIFIED,
				},
				{
					Type: pb.RouteFilterType_ADD_HEADER,
					AddHeaders: []*pb.HTTPHeader{
						{Name: "X-Valid", Value: "valid"},
					},
				},
			},
			expectedCount: 1,
			expectedTypes: []string{"*router.HeaderModifierFilter"},
		},
		{
			name: "response filter types should be skipped",
			pbFilters: []*pb.RouteFilter{
				{
					Type: pb.RouteFilterType_RESPONSE_ADD_HEADER,
					ResponseAddHeaders: []*pb.HTTPHeader{
						{Name: "X-Response", Value: "response"},
					},
				},
				{
					Type: pb.RouteFilterType_ADD_HEADER,
					AddHeaders: []*pb.HTTPHeader{
						{Name: "X-Request", Value: "request"},
					},
				},
			},
			expectedCount: 1,
			expectedTypes: []string{"*router.HeaderModifierFilter"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters := buildFilters(tt.pbFilters)

			if len(filters) != tt.expectedCount {
				t.Errorf("buildFilters() returned %d filters, want %d", len(filters), tt.expectedCount)
			}

			for i, filter := range filters {
				if i >= len(tt.expectedTypes) {
					break
				}
				filterType := getTypeName(filter)
				if filterType != tt.expectedTypes[i] {
					t.Errorf("Filter[%d] type = %q, want %q", i, filterType, tt.expectedTypes[i])
				}
			}
		})
	}
}

func TestApplyPrebuiltFilters(t *testing.T) {
	tests := []struct {
		name             string
		filters          []Filter
		setupReq         func(*http.Request)
		expectContinue   bool
		validateReq      func(*testing.T, *http.Request)
		validateResponse func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "no filters",
			filters:        []Filter{},
			setupReq:       func(r *http.Request) {},
			expectContinue: true,
			validateReq:    func(t *testing.T, r *http.Request) {},
		},
		{
			name: "header modifier continues",
			filters: []Filter{
				NewHeaderModifierFilter(&pb.RouteFilter{
					Type: pb.RouteFilterType_ADD_HEADER,
					AddHeaders: []*pb.HTTPHeader{
						{Name: "X-Test", Value: "test"},
					},
				}),
			},
			setupReq:       func(r *http.Request) {},
			expectContinue: true,
			validateReq: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("X-Test"); got != "test" {
					t.Errorf("X-Test = %q, want %q", got, "test")
				}
			},
		},
		{
			name: "redirect stops processing",
			filters: []Filter{
				NewHeaderModifierFilter(&pb.RouteFilter{
					Type: pb.RouteFilterType_ADD_HEADER,
					AddHeaders: []*pb.HTTPHeader{
						{Name: "X-First", Value: "first"},
					},
				}),
				NewRedirectFilter(&pb.RouteFilter{
					Type:        pb.RouteFilterType_REQUEST_REDIRECT,
					RedirectUrl: "https://redirect.com",
				}),
				NewHeaderModifierFilter(&pb.RouteFilter{
					Type: pb.RouteFilterType_ADD_HEADER,
					AddHeaders: []*pb.HTTPHeader{
						{Name: "X-Should-Not-Apply", Value: "never"},
					},
				}),
			},
			setupReq:       func(r *http.Request) {},
			expectContinue: false,
			validateReq: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("X-First"); got != "first" {
					t.Errorf("X-First header should be set")
				}
				if got := r.Header.Get("X-Should-Not-Apply"); got != "" {
					t.Errorf("X-Should-Not-Apply should not be set after redirect")
				}
			},
			validateResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				if w.Code != http.StatusFound {
					t.Errorf("Status = %d, want %d", w.Code, http.StatusFound)
				}
			},
		},
		{
			name: "multiple filters in sequence",
			filters: []Filter{
				NewHeaderModifierFilter(&pb.RouteFilter{
					Type: pb.RouteFilterType_ADD_HEADER,
					AddHeaders: []*pb.HTTPHeader{
						{Name: "X-Step-1", Value: "one"},
					},
				}),
				NewURLRewriteFilter(&pb.RouteFilter{
					Type:        pb.RouteFilterType_URL_REWRITE,
					RewritePath: "/rewritten",
				}),
				NewHeaderModifierFilter(&pb.RouteFilter{
					Type: pb.RouteFilterType_ADD_HEADER,
					AddHeaders: []*pb.HTTPHeader{
						{Name: "X-Step-2", Value: "two"},
					},
				}),
			},
			setupReq:       func(r *http.Request) {},
			expectContinue: true,
			validateReq: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("X-Step-1"); got != "one" {
					t.Errorf("X-Step-1 = %q, want %q", got, "one")
				}
				if got := r.Header.Get("X-Step-2"); got != "two" {
					t.Errorf("X-Step-2 = %q, want %q", got, "two")
				}
				if r.URL.Path != "/rewritten" {
					t.Errorf("Path = %q, want %q", r.URL.Path, "/rewritten")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()

			if tt.setupReq != nil {
				tt.setupReq(req)
			}

			newReq, shouldContinue := applyPrebuiltFilters(tt.filters, w, req)

			if shouldContinue != tt.expectContinue {
				t.Errorf("shouldContinue = %v, want %v", shouldContinue, tt.expectContinue)
			}

			if tt.validateReq != nil {
				tt.validateReq(t, newReq)
			}

			if tt.validateResponse != nil {
				tt.validateResponse(t, w)
			}
		})
	}
}

// Helper function to get the type name of a filter
func getTypeName(f Filter) string {
	switch f.(type) {
	case *HeaderModifierFilter:
		return "*router.HeaderModifierFilter"
	case *RedirectFilter:
		return "*router.RedirectFilter"
	case *URLRewriteFilter:
		return "*router.URLRewriteFilter"
	default:
		return "unknown"
	}
}
