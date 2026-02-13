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
	"encoding/base64"
	"encoding/binary"
	"io"
	"math"
	"net"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// gRPC-Web content types.
const (
	// ContentTypeGRPCWeb is the binary gRPC-Web content type.
	ContentTypeGRPCWeb = "application/grpc-web"

	// ContentTypeGRPCWebProto is the binary gRPC-Web+proto content type.
	ContentTypeGRPCWebProto = "application/grpc-web+proto"

	// ContentTypeGRPCWebText is the base64-encoded gRPC-Web content type.
	ContentTypeGRPCWebText = "application/grpc-web-text"

	// ContentTypeGRPCWebTextProto is the base64-encoded gRPC-Web+proto content type.
	ContentTypeGRPCWebTextProto = "application/grpc-web-text+proto"

	// ContentTypeGRPC is the standard gRPC content type sent to the backend.
	ContentTypeGRPC = "application/grpc"

	// grpcWebTrailerFlag is the gRPC frame flag byte indicating a trailer frame (0x80).
	grpcWebTrailerFlag = 0x80
)

// GRPCWebConfig holds configuration for the gRPC-Web middleware.
type GRPCWebConfig struct {
	// AllowedOrigins specifies origins permitted for CORS preflight.
	// If empty, no CORS headers are set (the caller should handle CORS separately).
	AllowedOrigins []string

	// AllowCredentials controls the Access-Control-Allow-Credentials header.
	AllowCredentials bool
}

// DefaultGRPCWebConfig returns a sensible default gRPC-Web configuration.
func DefaultGRPCWebConfig() *GRPCWebConfig {
	return &GRPCWebConfig{}
}

// GRPCWebMiddleware translates gRPC-Web requests into standard gRPC requests so
// that browser clients can communicate with gRPC backends through the proxy.
// Non-gRPC-Web requests are passed through to the next handler unmodified.
type GRPCWebMiddleware struct {
	config *GRPCWebConfig
	logger *zap.Logger
}

// NewGRPCWebMiddleware creates a new GRPCWebMiddleware with the given configuration.
func NewGRPCWebMiddleware(config *GRPCWebConfig, logger *zap.Logger) *GRPCWebMiddleware {
	if config == nil {
		config = DefaultGRPCWebConfig()
	}
	return &GRPCWebMiddleware{
		config: config,
		logger: logger.With(zap.String("component", "grpc-web")),
	}
}

// IsGRPCWebRequest reports whether the request carries a gRPC-Web content type.
func IsGRPCWebRequest(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return isGRPCWebContentType(ct)
}

// isGRPCWebContentType checks if a Content-Type value is a recognised gRPC-Web type.
func isGRPCWebContentType(ct string) bool {
	// Strip optional parameters (e.g. charset).
	if idx := strings.IndexByte(ct, ';'); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}
	switch ct {
	case ContentTypeGRPCWeb, ContentTypeGRPCWebProto,
		ContentTypeGRPCWebText, ContentTypeGRPCWebTextProto:
		return true
	default:
		return false
	}
}

// isTextEncoding returns true when the content type is the base64-encoded text variant.
func isTextEncoding(ct string) bool {
	if idx := strings.IndexByte(ct, ';'); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}
	return ct == ContentTypeGRPCWebText || ct == ContentTypeGRPCWebTextProto
}

// Wrap returns an http.Handler that intercepts gRPC-Web requests, translates them
// into native gRPC, and converts the response back to gRPC-Web format.
func (m *GRPCWebMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle CORS preflight for gRPC-Web (browsers send OPTIONS).
		if r.Method == http.MethodOptions && isGRPCWebPreflight(r) {
			m.handleCORSPreflight(w, r)
			return
		}

		if !IsGRPCWebRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		origContentType := r.Header.Get("Content-Type")
		textMode := isTextEncoding(origContentType)

		m.logger.Debug("translating gRPC-Web request",
			zap.String("path", r.URL.Path),
			zap.String("content_type", origContentType),
			zap.Bool("text_mode", textMode),
		)

		// Rewrite request Content-Type to standard gRPC.
		r.Header.Set("Content-Type", ContentTypeGRPC)

		// For the text variant, decode the base64-encoded request body.
		if textMode {
			r.Body = io.NopCloser(base64.NewDecoder(base64.StdEncoding, r.Body))
		}

		// Determine the response content type to send back to the client.
		respContentType := ContentTypeGRPCWeb
		if textMode {
			respContentType = ContentTypeGRPCWebText
		}

		// Wrap the response writer to capture gRPC trailers and convert them.
		gw := &grpcWebResponseWriter{
			ResponseWriter:  w,
			textMode:        textMode,
			respContentType: respContentType,
			bodyBuf:         &bytes.Buffer{},
		}

		next.ServeHTTP(gw, r)

		// Flush any buffered trailer frame and write final output.
		gw.finish()
	})
}

// isGRPCWebPreflight checks whether an OPTIONS request is a CORS preflight for gRPC-Web.
func isGRPCWebPreflight(r *http.Request) bool {
	reqMethod := r.Header.Get("Access-Control-Request-Method")
	if reqMethod == "" {
		return false
	}
	reqHeaders := r.Header.Get("Access-Control-Request-Headers")
	// Browsers typically include content-type and x-grpc-web in the preflight.
	lower := strings.ToLower(reqHeaders)
	return strings.Contains(lower, "content-type") ||
		strings.Contains(lower, "x-grpc-web") ||
		strings.Contains(lower, "x-user-agent")
}

// handleCORSPreflight writes CORS preflight response headers.
func (m *GRPCWebMiddleware) handleCORSPreflight(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	allowedOrigin := m.matchOrigin(origin)
	if allowedOrigin == "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers",
		"Content-Type, X-Grpc-Web, X-User-Agent, Grpc-Timeout, Authorization")
	w.Header().Set("Access-Control-Max-Age", "86400")

	if m.config.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	w.WriteHeader(http.StatusNoContent)
}

// matchOrigin returns the allowed origin string if the provided origin is permitted,
// or an empty string otherwise.
func (m *GRPCWebMiddleware) matchOrigin(origin string) string {
	if len(m.config.AllowedOrigins) == 0 {
		return ""
	}
	for _, allowed := range m.config.AllowedOrigins {
		if allowed == "*" {
			return "*"
		}
		if allowed == origin {
			return origin
		}
	}
	return ""
}

// grpcWebResponseWriter intercepts the gRPC response and converts it back to
// gRPC-Web format. For the text variant, it buffers all binary gRPC frames and
// base64-encodes the entire body (data frames + trailer frame) as a single
// contiguous base64 stream, as required by the gRPC-Web specification.
type grpcWebResponseWriter struct {
	http.ResponseWriter
	textMode        bool
	respContentType string
	wroteHeader     bool
	statusCode      int
	bodyBuf         *bytes.Buffer // accumulates binary gRPC frames
}

// WriteHeader sets the response content type to the gRPC-Web variant and
// captures the status code. The actual status code is forwarded during finish().
func (gw *grpcWebResponseWriter) WriteHeader(code int) {
	if gw.wroteHeader {
		return
	}
	gw.wroteHeader = true
	gw.statusCode = code
}

// Write buffers the binary gRPC frame data. The data is written to the client
// during finish(), after trailers have been appended.
func (gw *grpcWebResponseWriter) Write(b []byte) (int, error) {
	if !gw.wroteHeader {
		gw.WriteHeader(http.StatusOK)
	}
	return gw.bodyBuf.Write(b)
}

// Flush implements http.Flusher. Since we buffer everything, this is a no-op
// until finish() is called.
func (gw *grpcWebResponseWriter) Flush() {
	// Intentionally a no-op during buffering; flushing happens in finish().
}

// Hijack implements http.Hijacker.
func (gw *grpcWebResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := gw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Unwrap returns the underlying ResponseWriter.
func (gw *grpcWebResponseWriter) Unwrap() http.ResponseWriter {
	return gw.ResponseWriter
}

// finish appends the gRPC-Web trailer frame to the buffered body, then writes
// the complete response to the client. For the text variant, the entire body
// (data frames + trailer frame) is base64-encoded as a single string.
func (gw *grpcWebResponseWriter) finish() {
	// Collect and append trailer frame.
	trailers := gw.collectTrailers()
	if len(trailers) > 0 {
		trailerFrame := buildTrailerFrame(trailers)
		gw.bodyBuf.Write(trailerFrame)
	}

	// Set response headers.
	gw.ResponseWriter.Header().Set("Content-Type", gw.respContentType)
	gw.ResponseWriter.Header().Set("Access-Control-Expose-Headers",
		"Grpc-Status, Grpc-Message, Grpc-Status-Details-Bin")

	statusCode := gw.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	gw.ResponseWriter.WriteHeader(statusCode)

	// Write the body, base64-encoding if text mode.
	binaryBody := gw.bodyBuf.Bytes()
	if gw.textMode && len(binaryBody) > 0 {
		encoded := base64.StdEncoding.EncodeToString(binaryBody)
		_, _ = gw.ResponseWriter.Write([]byte(encoded))
	} else {
		_, _ = gw.ResponseWriter.Write(binaryBody)
	}

	if f, ok := gw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// buildTrailerFrame serialises HTTP trailers into a gRPC-Web trailer frame:
// 1-byte flag (0x80) + 4-byte big-endian length + "key: value\r\n" pairs.
func buildTrailerFrame(trailers http.Header) []byte {
	var trailerBuf bytes.Buffer
	for key, values := range trailers {
		for _, v := range values {
			trailerBuf.WriteString(key)
			trailerBuf.WriteString(": ")
			trailerBuf.WriteString(v)
			trailerBuf.WriteString("\r\n")
		}
	}

	payload := trailerBuf.Bytes()
	frame := make([]byte, 5+len(payload))
	frame[0] = grpcWebTrailerFlag
	binary.BigEndian.PutUint32(frame[1:5], safeIntToUint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}

// collectTrailers gathers gRPC trailing metadata from the response.
// It looks for grpc-status, grpc-message, grpc-status-details-bin headers
// and headers with the "Trailer:" prefix convention.
func (gw *grpcWebResponseWriter) collectTrailers() http.Header {
	trailers := make(http.Header)

	for key, values := range gw.Header() {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "trailer:") {
			trailerKey := strings.TrimPrefix(lower, "trailer:")
			trailerKey = strings.TrimSpace(trailerKey)
			trailers[trailerKey] = values
			continue
		}
		// Capture grpc-status and grpc-message if set as regular headers
		// (common when the backend is an HTTP/1.1 gRPC server or the response
		// writer doesn't support true HTTP/2 trailers).
		if lower == "grpc-status" || lower == "grpc-message" || lower == "grpc-status-details-bin" {
			trailers[lower] = values
		}
	}

	return trailers
}

// safeIntToUint32 safely converts an int to uint32 with clamping.
func safeIntToUint32(v int) uint32 {
	if v < 0 {
		return 0
	}
	if v > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v)
}
