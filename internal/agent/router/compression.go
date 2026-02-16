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
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Compression algorithm identifiers used in Accept-Encoding and Content-Encoding headers.
const (
	encodingGzip   = "gzip"
	encodingBrotli = "br"
)

// defaultMinCompressionSize is the default minimum body size before compression triggers (1KB).
const defaultMinCompressionSize int64 = 1024

// defaultGzipLevel is the default gzip compression level.
const defaultGzipLevel = gzip.DefaultCompression

// defaultBrotliLevel is the default brotli compression level.
const defaultBrotliLevel = 4

// defaultExcludedTypes is the default set of content types to skip for compression.
// These types are already compressed or gain minimal benefit from compression.
var defaultExcludedTypes = []string{
	"image/*",
	"video/*",
	"audio/*",
	"application/zip",
	"application/gzip",
	"application/x-gzip",
	"application/x-brotli",
	"application/octet-stream",
}

// CompressionMiddleware compresses HTTP responses based on the client's Accept-Encoding header.
type CompressionMiddleware struct {
	config *pb.CompressionConfig
}

// NewCompressionMiddleware creates a new CompressionMiddleware from configuration.
// If config is nil or disabled, the middleware passes requests through unchanged.
func NewCompressionMiddleware(config *pb.CompressionConfig) *CompressionMiddleware {
	return &CompressionMiddleware{config: config}
}

// Wrap returns an http.Handler middleware that applies response compression.
func (cm *CompressionMiddleware) Wrap(next http.Handler) http.Handler {
	if cm.config == nil || !cm.config.Enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Determine which encoding the client accepts and that we support
		encoding := cm.negotiateEncoding(r)
		if encoding == "" {
			// Client doesn't accept any encoding we support; pass through
			next.ServeHTTP(w, r)
			return
		}

		// Wrap the response writer with compression
		cw := getCompressWriter(w, encoding, cm.config, cm.gzipLevel(), cm.brotliLevel())
		defer cw.finish()

		next.ServeHTTP(cw, r)
	})
}

// negotiateEncoding selects the best compression encoding from the client's Accept-Encoding.
func (cm *CompressionMiddleware) negotiateEncoding(r *http.Request) string {
	accept := r.Header.Get("Accept-Encoding")
	if accept == "" {
		return ""
	}

	algorithms := cm.config.Algorithms
	if len(algorithms) == 0 {
		algorithms = []string{encodingGzip, encodingBrotli}
	}

	// Check each configured algorithm in preference order
	for _, algo := range algorithms {
		if strings.Contains(accept, algo) {
			return algo
		}
	}
	return ""
}

// gzipLevel returns the configured gzip compression level.
func (cm *CompressionMiddleware) gzipLevel() int {
	if cm.config.Level > 0 && cm.config.Level <= 9 {
		return int(cm.config.Level)
	}
	return defaultGzipLevel
}

// brotliLevel returns the configured brotli compression level.
func (cm *CompressionMiddleware) brotliLevel() int {
	if cm.config.Level >= 0 && cm.config.Level <= 11 {
		return int(cm.config.Level)
	}
	return defaultBrotliLevel
}

// compressResponseWriter wraps http.ResponseWriter to compress response bodies.
type compressResponseWriter struct {
	http.ResponseWriter
	encoding     string
	config       *pb.CompressionConfig
	gzipLevel    int
	brotliLevel  int
	buf          bytes.Buffer
	writer       io.WriteCloser
	wroteHeader  bool
	statusCode   int
	headersSent  bool
	skipCompress bool
}

// compressWriterPool reduces allocations for compressResponseWriter.
var compressWriterPool = sync.Pool{
	New: func() interface{} {
		return &compressResponseWriter{}
	},
}

// gzipWriterPool reuses gzip.Writer instances to reduce GC pressure.
var gzipWriterPool sync.Pool

// brotliWriterPool reuses brotli.Writer instances to reduce GC pressure.
var brotliWriterPool sync.Pool

func getCompressWriter(w http.ResponseWriter, encoding string, config *pb.CompressionConfig, gzipLvl, brotliLvl int) *compressResponseWriter {
	cw, ok := compressWriterPool.Get().(*compressResponseWriter)
	if !ok {
		cw = &compressResponseWriter{}
	}
	cw.ResponseWriter = w
	cw.encoding = encoding
	cw.config = config
	cw.gzipLevel = gzipLvl
	cw.brotliLevel = brotliLvl
	cw.buf.Reset()
	cw.writer = nil
	cw.wroteHeader = false
	cw.statusCode = http.StatusOK
	cw.headersSent = false
	cw.skipCompress = false
	return cw
}

func putCompressWriter(cw *compressResponseWriter) {
	cw.ResponseWriter = nil
	cw.config = nil
	cw.writer = nil
	cw.buf.Reset()
	compressWriterPool.Put(cw)
}

// WriteHeader captures the status code and applies Vary header.
func (cw *compressResponseWriter) WriteHeader(code int) {
	if cw.wroteHeader {
		return
	}
	cw.wroteHeader = true
	cw.statusCode = code

	// Always set Vary: Accept-Encoding regardless of whether we compress
	cw.ResponseWriter.Header().Add("Vary", "Accept-Encoding")

	// Check if we should skip compression for this response
	if cw.shouldSkip() {
		cw.skipCompress = true
		cw.ResponseWriter.WriteHeader(code)
		cw.headersSent = true
		return
	}
}

// Write buffers or compresses the response body.
func (cw *compressResponseWriter) Write(b []byte) (int, error) {
	if !cw.wroteHeader {
		cw.WriteHeader(http.StatusOK)
	}

	if cw.skipCompress {
		return cw.ResponseWriter.Write(b)
	}

	// Buffer the data until we know the total size (for minSize check)
	if cw.writer == nil {
		n, err := cw.buf.Write(b)
		if err != nil {
			return n, err
		}

		minSize := cw.minSize()
		if int64(cw.buf.Len()) >= minSize {
			// Body meets minimum size threshold; start compressing
			if err := cw.startCompression(); err != nil {
				// Fallback: write uncompressed
				cw.skipCompress = true
				cw.ResponseWriter.WriteHeader(cw.statusCode)
				cw.headersSent = true
				return cw.ResponseWriter.Write(cw.buf.Bytes())
			}
			// Flush buffered data through the compressor
			_, err = cw.writer.Write(cw.buf.Bytes())
			cw.buf.Reset()
			return n, err
		}
		return n, nil
	}

	// Already compressing; write directly to the compressor
	return cw.writer.Write(b)
}

// Flush implements http.Flusher.
func (cw *compressResponseWriter) Flush() {
	if cw.writer != nil {
		if f, ok := cw.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
	}
}

// Hijack implements http.Hijacker for WebSocket support.
func (cw *compressResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := cw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Unwrap returns the underlying ResponseWriter.
func (cw *compressResponseWriter) Unwrap() http.ResponseWriter {
	return cw.ResponseWriter
}

// finish flushes any buffered data and closes the compression writer.
func (cw *compressResponseWriter) finish() {
	defer putCompressWriter(cw)

	if cw.skipCompress {
		return
	}

	if cw.writer != nil {
		// Close the compressor (flushes remaining data)
		if err := cw.writer.Close(); err != nil {
			zap.L().Warn("failed to close compression writer", zap.Error(err))
		}
		// Return the compressor writer to its pool for reuse
		switch w := cw.writer.(type) {
		case *gzip.Writer:
			gzipWriterPool.Put(w)
		case *brotli.Writer:
			brotliWriterPool.Put(w)
		}
		return
	}

	// Data was buffered but didn't meet minSize; write uncompressed
	if cw.buf.Len() > 0 {
		if !cw.headersSent {
			cw.ResponseWriter.WriteHeader(cw.statusCode)
			cw.headersSent = true
		}
		_, _ = cw.ResponseWriter.Write(cw.buf.Bytes())
		return
	}

	// No data written at all; just send headers if not already sent
	if !cw.headersSent {
		cw.ResponseWriter.WriteHeader(cw.statusCode)
	}
}

// startCompression initializes the compression writer and sets response headers.
func (cw *compressResponseWriter) startCompression() error {
	// Set Content-Encoding header
	cw.ResponseWriter.Header().Set("Content-Encoding", cw.encoding)

	// Remove Content-Length since we're streaming compressed data
	cw.ResponseWriter.Header().Del("Content-Length")

	// Write status code
	cw.ResponseWriter.WriteHeader(cw.statusCode)
	cw.headersSent = true

	// Create the compression writer, reusing pooled instances when available
	switch cw.encoding {
	case encodingGzip:
		if pooled := gzipWriterPool.Get(); pooled != nil {
			gw, ok := pooled.(*gzip.Writer)
			if !ok {
				return gzip.ErrHeader
			}
			gw.Reset(cw.ResponseWriter)
			cw.writer = gw
		} else {
			gw, err := gzip.NewWriterLevel(cw.ResponseWriter, cw.gzipLevel)
			if err != nil {
				return err
			}
			cw.writer = gw
		}
	case encodingBrotli:
		if pooled := brotliWriterPool.Get(); pooled != nil {
			bw, ok := pooled.(*brotli.Writer)
			if !ok {
				cw.writer = brotli.NewWriterLevel(cw.ResponseWriter, cw.brotliLevel)
			} else {
				bw.Reset(cw.ResponseWriter)
				cw.writer = bw
			}
		} else {
			cw.writer = brotli.NewWriterLevel(cw.ResponseWriter, cw.brotliLevel)
		}
	default:
		// Should not happen; fallback to identity
		cw.skipCompress = true
	}
	return nil
}

// shouldSkip determines if compression should be skipped for this response.
func (cw *compressResponseWriter) shouldSkip() bool {
	// Skip compression for informational, no-content, and not-modified responses
	if cw.statusCode == http.StatusNoContent || cw.statusCode == http.StatusNotModified {
		return true
	}
	if cw.statusCode < 200 {
		return true
	}

	// Skip if response already has Content-Encoding
	if cw.ResponseWriter.Header().Get("Content-Encoding") != "" {
		return true
	}

	// Check content type against exclusion list
	contentType := cw.ResponseWriter.Header().Get("Content-Type")
	if contentType != "" && cw.isExcludedType(contentType) {
		return true
	}

	return false
}

// isExcludedType checks if the content type matches any excluded pattern.
func (cw *compressResponseWriter) isExcludedType(contentType string) bool {
	// Strip parameters (e.g., "text/html; charset=utf-8" -> "text/html")
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	excludeTypes := cw.config.ExcludeTypes
	if len(excludeTypes) == 0 {
		excludeTypes = defaultExcludedTypes
	}

	for _, pattern := range excludeTypes {
		// Use path.Match for glob-style matching (e.g., "image/*")
		if matched, err := path.Match(pattern, contentType); err == nil && matched {
			return true
		}
	}
	return false
}

// minSize returns the configured minimum body size for compression.
func (cw *compressResponseWriter) minSize() int64 {
	if cw.config.MinSize > 0 {
		return cw.config.MinSize
	}
	return defaultMinCompressionSize
}
