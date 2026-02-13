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
	"fmt"
	"net"
	"net/http"
	"sync"
)

// responseWriterWithStatus wraps http.ResponseWriter to capture status code
type responseWriterWithStatus struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriterWithStatus) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriterWithStatus) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// reset prepares the responseWriter for reuse
func (rw *responseWriterWithStatus) reset(w http.ResponseWriter) {
	rw.ResponseWriter = w
	rw.statusCode = http.StatusOK
	rw.written = false
}

// Flush implements http.Flusher by delegating to the underlying ResponseWriter.
func (rw *responseWriterWithStatus) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker for WebSocket upgrades and other connection takeovers.
func (rw *responseWriterWithStatus) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

// Unwrap returns the underlying ResponseWriter, required for http.ResponseController.
func (rw *responseWriterWithStatus) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// responseWriterPool is a sync.Pool for responseWriterWithStatus to reduce allocations
var responseWriterPool = sync.Pool{
	New: func() interface{} {
		return &responseWriterWithStatus{
			statusCode: http.StatusOK,
		}
	},
}

// getResponseWriter gets a responseWriter from the pool
func getResponseWriter(w http.ResponseWriter) *responseWriterWithStatus {
	rw, ok := responseWriterPool.Get().(*responseWriterWithStatus)
	if !ok {
		rw = &responseWriterWithStatus{statusCode: http.StatusOK}
	}
	rw.reset(w)
	return rw
}

// putResponseWriter returns a responseWriter to the pool
func putResponseWriter(rw *responseWriterWithStatus) {
	// Clear the underlying ResponseWriter reference to prevent memory leaks
	rw.ResponseWriter = nil
	responseWriterPool.Put(rw)
}
