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
	"net"
	"net/http"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// ResponseFilter defines response header modification rules
type ResponseFilter struct {
	AddHeaders    []*pb.HTTPHeader // Headers to add (append to existing)
	SetHeaders    []*pb.HTTPHeader // Headers to set (replace existing)
	RemoveHeaders []string         // Headers to remove
}

// NewResponseFilter creates a new response filter from RouteFilter
func NewResponseFilter(filter *pb.RouteFilter) *ResponseFilter {
	return &ResponseFilter{
		AddHeaders:    filter.ResponseAddHeaders,
		SetHeaders:    filter.ResponseSetHeaders,
		RemoveHeaders: filter.ResponseRemoveHeaders,
	}
}

// HasModifications returns true if this filter has any response modifications
func (rf *ResponseFilter) HasModifications() bool {
	return len(rf.AddHeaders) > 0 || len(rf.SetHeaders) > 0 || len(rf.RemoveHeaders) > 0
}

// ResponseHeaderWriter wraps http.ResponseWriter to intercept and modify response headers
type ResponseHeaderWriter struct {
	http.ResponseWriter
	filter        *ResponseFilter
	wroteHeader   bool
	statusCode    int
	headersCopied bool
}

// NewResponseHeaderWriter creates a new response header writer
func NewResponseHeaderWriter(w http.ResponseWriter, filter *ResponseFilter) *ResponseHeaderWriter {
	return &ResponseHeaderWriter{
		ResponseWriter: w,
		filter:         filter,
		statusCode:     http.StatusOK,
	}
}

// Header returns the header map that will be modified before being sent
func (rw *ResponseHeaderWriter) Header() http.Header {
	return rw.ResponseWriter.Header()
}

// applyHeaderModifications applies the header modifications
func (rw *ResponseHeaderWriter) applyHeaderModifications() {
	if rw.headersCopied || rw.filter == nil {
		return
	}
	rw.headersCopied = true

	header := rw.ResponseWriter.Header()

	// First, remove headers (before add/set)
	for _, name := range rw.filter.RemoveHeaders {
		header.Del(name)
	}

	// Set headers (replaces any existing value)
	for _, h := range rw.filter.SetHeaders {
		header.Set(h.Name, h.Value)
	}

	// Add headers (appends to existing)
	for _, h := range rw.filter.AddHeaders {
		header.Add(h.Name, h.Value)
	}

	// Track metrics
	metrics.ResponseHeadersModifiedTotal.Inc()
}

// WriteHeader captures the status code and applies header modifications
func (rw *ResponseHeaderWriter) WriteHeader(statusCode int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.statusCode = statusCode

	// Apply header modifications before writing the status
	rw.applyHeaderModifications()

	rw.ResponseWriter.WriteHeader(statusCode)
}

// Write writes the body and ensures headers are sent first
func (rw *ResponseHeaderWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher
func (rw *ResponseHeaderWriter) Flush() {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker for WebSocket support
func (rw *ResponseHeaderWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Push implements http.Pusher for HTTP/2 server push
func (rw *ResponseHeaderWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := rw.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

// Unwrap returns the underlying ResponseWriter (for ResponseController)
func (rw *ResponseHeaderWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// ResponseHeaderModifierFilter is an HTTP middleware for response header modification
type ResponseHeaderModifierFilter struct {
	filter *ResponseFilter
}

// NewResponseHeaderModifierFilter creates a new response header modifier filter
func NewResponseHeaderModifierFilter(filter *pb.RouteFilter) *ResponseHeaderModifierFilter {
	return &ResponseHeaderModifierFilter{
		filter: NewResponseFilter(filter),
	}
}

// HandleResponseHeaders returns middleware that modifies response headers
func HandleResponseHeaders(filter *ResponseFilter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if filter == nil || !filter.HasModifications() {
				next.ServeHTTP(w, r)
				return
			}

			// Wrap the response writer to intercept header writes
			rw := NewResponseHeaderWriter(w, filter)
			next.ServeHTTP(rw, r)
		})
	}
}

// collectResponseFilters collects all response filters from a list of route filters
func collectResponseFilters(filters []*pb.RouteFilter) *ResponseFilter {
	combined := &ResponseFilter{
		AddHeaders:    make([]*pb.HTTPHeader, 0),
		SetHeaders:    make([]*pb.HTTPHeader, 0),
		RemoveHeaders: make([]string, 0),
	}

	for _, f := range filters {
		switch f.Type {
		case pb.RouteFilterType_RESPONSE_ADD_HEADER:
			combined.AddHeaders = append(combined.AddHeaders, f.ResponseAddHeaders...)
		case pb.RouteFilterType_RESPONSE_REMOVE_HEADER:
			combined.RemoveHeaders = append(combined.RemoveHeaders, f.ResponseRemoveHeaders...)
		case pb.RouteFilterType_RESPONSE_SET_HEADER:
			combined.SetHeaders = append(combined.SetHeaders, f.ResponseSetHeaders...)
		}
	}

	return combined
}
