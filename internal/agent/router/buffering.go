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
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// defaultMemoryThreshold is the body size threshold in bytes at which buffering
// switches from memory to a temporary file. Default: 1MB.
const defaultMemoryThreshold int64 = 1 << 20

// RequestBufferingMiddleware buffers the entire request body before forwarding.
// This enables retry support since the body becomes re-readable.
// If maxSize > 0 and the body exceeds it, a 413 Payload Too Large is returned.
type RequestBufferingMiddleware struct {
	config *pb.BufferingConfig
}

// NewRequestBufferingMiddleware creates a new request buffering middleware.
func NewRequestBufferingMiddleware(config *pb.BufferingConfig) *RequestBufferingMiddleware {
	return &RequestBufferingMiddleware{config: config}
}

// Wrap returns an http.Handler that buffers the request body.
func (m *RequestBufferingMiddleware) Wrap(next http.Handler) http.Handler {
	if m.config == nil || !m.config.RequestBuffering {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody {
			next.ServeHTTP(w, r)
			return
		}

		buffered, err := m.bufferBody(r.Body, r.ContentLength)
		if err != nil {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		r.Body = buffered
		if seeker, ok := buffered.(io.ReadSeeker); ok {
			// Store content length for the buffered body
			pos, _ := seeker.Seek(0, io.SeekEnd)
			_, _ = seeker.Seek(0, io.SeekStart)
			r.ContentLength = pos
		}

		next.ServeHTTP(w, r)
	})
}

// bufferBody reads the entire body into a buffer (memory or temp file).
func (m *RequestBufferingMiddleware) bufferBody(body io.ReadCloser, contentLength int64) (io.ReadCloser, error) {
	defer func() { _ = body.Close() }()

	maxSize := m.config.MaxBufferSize
	threshold := m.config.MemoryThreshold
	if threshold <= 0 {
		threshold = defaultMemoryThreshold
	}

	// If content length is known and exceeds max, reject immediately
	if maxSize > 0 && contentLength > maxSize {
		return nil, errBodyTooLarge
	}

	// Use a limited reader if max size is set
	var reader io.Reader = body
	if maxSize > 0 {
		reader = io.LimitReader(body, maxSize+1) // +1 to detect overflow
	}

	// Buffer into memory first
	var buf bytes.Buffer
	n, err := io.CopyN(&buf, reader, threshold+1)
	if err != nil && err != io.EOF {
		return nil, err
	}

	// If body fits in memory
	if n <= threshold {
		if maxSize > 0 && n > maxSize {
			return nil, errBodyTooLarge
		}
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	}

	// Body exceeds memory threshold; spill to temp file
	tmpFile, ferr := os.CreateTemp("", "novaedge-buf-*")
	if ferr != nil {
		return nil, ferr
	}

	// Write memory buffer to file
	if _, werr := tmpFile.Write(buf.Bytes()); werr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(filepath.Clean(tmpFile.Name()))
		return nil, werr
	}

	// Copy remaining data from reader to file
	written, cerr := io.Copy(tmpFile, reader)
	if cerr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(filepath.Clean(tmpFile.Name()))
		return nil, cerr
	}

	totalSize := n + written
	if maxSize > 0 && totalSize > maxSize {
		_ = tmpFile.Close()
		_ = os.Remove(filepath.Clean(tmpFile.Name()))
		return nil, errBodyTooLarge
	}

	// Seek back to beginning for reading
	if _, serr := tmpFile.Seek(0, io.SeekStart); serr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(filepath.Clean(tmpFile.Name()))
		return nil, serr
	}

	return &tempFileReadCloser{File: tmpFile}, nil
}

// errBodyTooLarge indicates the request body exceeded the maximum buffer size.
var errBodyTooLarge = &bodyTooLargeError{}

type bodyTooLargeError struct{}

func (e *bodyTooLargeError) Error() string {
	return "body exceeds maximum buffer size"
}

// tempFileReadCloser wraps an os.File that is automatically deleted on Close.
type tempFileReadCloser struct {
	*os.File
}

// Close closes and removes the temporary file.
func (t *tempFileReadCloser) Close() error {
	name := filepath.Clean(t.File.Name())
	cerr := t.File.Close()
	rerr := os.Remove(name)
	if cerr != nil {
		return cerr
	}
	return rerr
}

// ResponseBufferingMiddleware buffers the entire response body before sending it.
// This enables response transformations and ensures the complete response
// is available before committing to the client.
type ResponseBufferingMiddleware struct {
	config *pb.BufferingConfig
}

// NewResponseBufferingMiddleware creates a new response buffering middleware.
func NewResponseBufferingMiddleware(config *pb.BufferingConfig) *ResponseBufferingMiddleware {
	return &ResponseBufferingMiddleware{config: config}
}

// Wrap returns an http.Handler that buffers the response body.
func (m *ResponseBufferingMiddleware) Wrap(next http.Handler) http.Handler {
	if m.config == nil || !m.config.ResponseBuffering {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bw := &bufferedResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
			maxSize:        m.config.MaxBufferSize,
		}
		next.ServeHTTP(bw, r)
		bw.flush()
	})
}

// bufferedResponseWriter captures the response body in a buffer.
type bufferedResponseWriter struct {
	http.ResponseWriter
	buf         bytes.Buffer
	statusCode  int
	wroteHeader bool
	maxSize     int64
	overflowed  bool
}

// WriteHeader captures the status code.
func (bw *bufferedResponseWriter) WriteHeader(code int) {
	if bw.wroteHeader {
		return
	}
	bw.wroteHeader = true
	bw.statusCode = code
}

// Write buffers response data.
func (bw *bufferedResponseWriter) Write(b []byte) (int, error) {
	if !bw.wroteHeader {
		bw.WriteHeader(http.StatusOK)
	}
	if bw.overflowed {
		return len(b), nil // discard; already overflowed
	}
	if bw.maxSize > 0 && int64(bw.buf.Len()+len(b)) > bw.maxSize {
		bw.overflowed = true
		return len(b), nil
	}
	return bw.buf.Write(b)
}

// Hijack implements http.Hijacker.
func (bw *bufferedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := bw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Unwrap returns the underlying ResponseWriter.
func (bw *bufferedResponseWriter) Unwrap() http.ResponseWriter {
	return bw.ResponseWriter
}

// flush writes the buffered response to the underlying writer.
func (bw *bufferedResponseWriter) flush() {
	// Copy headers (they were set on the underlying writer already by the handler)
	bw.ResponseWriter.WriteHeader(bw.statusCode)
	if bw.buf.Len() > 0 {
		_, _ = bw.ResponseWriter.Write(bw.buf.Bytes())
	}
}
