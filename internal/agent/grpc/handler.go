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

package grpc

import (
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/protocol"
)

// gRPC-specific header constants
const (
	// gRPC content type prefixes
	grpcContentType        = "application/grpc"
	grpcWebContentType     = "application/grpc-web"
	grpcWebTextContentType = "application/grpc-web-text"

	// gRPC headers
	grpcStatusHeader         = "grpc-status"
	grpcMessageHeader        = "grpc-message"
	grpcEncodingHeader       = "grpc-encoding"
	grpcAcceptEncodingHeader = "grpc-accept-encoding"
	grpcTimeoutHeader        = "grpc-timeout"
	grpcUserAgentHeader      = "grpc-user-agent"
)

// IsGRPCRequest checks if an HTTP request is a gRPC request
// Delegates to protocol package for consistency
var IsGRPCRequest = protocol.IsGRPCRequest

// GRPCHandler handles gRPC-specific request/response proxying
type GRPCHandler struct {
	logger *zap.Logger
}

// NewGRPCHandler creates a new gRPC handler
func NewGRPCHandler(logger *zap.Logger) *GRPCHandler {
	return &GRPCHandler{
		logger: logger,
	}
}

// PrepareGRPCRequest prepares a gRPC request for proxying to backend
// This ensures all gRPC-specific headers and metadata are properly forwarded
func (h *GRPCHandler) PrepareGRPCRequest(r *http.Request) *http.Request {
	// gRPC headers that should be forwarded
	grpcHeaders := []string{
		grpcEncodingHeader,
		grpcAcceptEncodingHeader,
		grpcTimeoutHeader,
		grpcUserAgentHeader,
		"grpc-trace-bin",
		"grpc-tags-bin",
	}

	// Clone the request to avoid modifying the original
	clonedReq := r.Clone(r.Context())

	// Ensure gRPC-specific headers are preserved
	for _, header := range grpcHeaders {
		if value := r.Header.Get(header); value != "" {
			clonedReq.Header.Set(header, value)
		}
	}

	// Forward all custom metadata (headers starting with "grpc-")
	for key, values := range r.Header {
		if strings.HasPrefix(strings.ToLower(key), "grpc-") {
			for _, value := range values {
				clonedReq.Header.Add(key, value)
			}
		}
	}

	// Preserve Content-Type exactly as received
	if ct := r.Header.Get("Content-Type"); ct != "" {
		clonedReq.Header.Set("Content-Type", ct)
	}

	h.logger.Debug("Prepared gRPC request for backend",
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("content-type", r.Header.Get("Content-Type")),
	)

	return clonedReq
}

// HandleGRPCResponse handles gRPC-specific response processing
// This ensures gRPC status codes, trailers, and metadata are properly forwarded
func (h *GRPCHandler) HandleGRPCResponse(w http.ResponseWriter, backendResp *http.Response) error {
	// Copy all headers from backend response
	for key, values := range backendResp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write status code
	w.WriteHeader(backendResp.StatusCode)

	// Copy response body (handles streaming)
	// For gRPC, this includes all streaming frames
	written, err := io.Copy(w, backendResp.Body)
	if err != nil {
		h.logger.Error("Error copying gRPC response body",
			zap.Error(err),
			zap.Int64("bytes_written", written),
		)
		return err
	}

	// gRPC uses HTTP/2 trailers for final status
	// Copy trailers from backend response to client
	if backendResp.Trailer != nil {
		// Get the http.ResponseWriter's underlying trailer
		if trailer := w.Header(); trailer != nil {
			for key, values := range backendResp.Trailer {
				for _, value := range values {
					trailer.Add(key, value)
				}
			}
		}
	}

	h.logger.Debug("Completed gRPC response forwarding",
		zap.Int64("bytes_written", written),
		zap.Int("status_code", backendResp.StatusCode),
		zap.String("grpc-status", backendResp.Header.Get(grpcStatusHeader)),
	)

	return nil
}

// ValidateGRPCRequest performs gRPC-specific request validation
func (h *GRPCHandler) ValidateGRPCRequest(r *http.Request) error {
	// gRPC requires POST method
	if r.Method != http.MethodPost {
		h.logger.Warn("gRPC request with invalid method",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
		)
		return nil // Don't error, just log warning
	}

	// Validate Content-Type
	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, grpcContentType) &&
		!strings.HasPrefix(contentType, grpcWebContentType) &&
		!strings.HasPrefix(contentType, grpcWebTextContentType) {
		h.logger.Warn("gRPC request with invalid content-type",
			zap.String("content-type", contentType),
			zap.String("path", r.URL.Path),
		)
	}

	return nil
}

// GetGRPCMetadata extracts gRPC metadata from request headers
func (h *GRPCHandler) GetGRPCMetadata(r *http.Request) map[string][]string {
	metadata := make(map[string][]string)

	// Extract all headers that are considered gRPC metadata
	// This includes all headers except HTTP/2 pseudo-headers
	for key, values := range r.Header {
		// Skip HTTP/2 pseudo-headers (start with ":")
		if strings.HasPrefix(key, ":") {
			continue
		}

		// Include all other headers as metadata
		metadata[key] = values
	}

	return metadata
}

// IsGRPCStreaming determines if the request is a streaming gRPC call
// Note: This is a heuristic as we can't fully determine this without
// inspecting the protobuf service definition
func (h *GRPCHandler) IsGRPCStreaming(r *http.Request) bool {
	// gRPC streaming always uses chunked transfer encoding
	// or doesn't specify Content-Length
	contentLength := r.Header.Get("Content-Length")
	transferEncoding := r.Header.Get("Transfer-Encoding")

	isStreaming := transferEncoding == "chunked" || contentLength == ""

	if isStreaming {
		h.logger.Debug("Detected potential gRPC streaming request",
			zap.String("path", r.URL.Path),
			zap.String("transfer-encoding", transferEncoding),
			zap.String("content-length", contentLength),
		)
	}

	return isStreaming
}

// ExtractGRPCServiceMethod extracts the gRPC service and method from the request path
// gRPC paths follow the format: /package.Service/Method
func ExtractGRPCServiceMethod(path string) (service string, method string, ok bool) {
	// Remove leading slash
	if len(path) == 0 || path[0] != '/' {
		return "", "", false
	}

	path = path[1:]

	// Find the last slash that separates service from method
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash == -1 {
		return "", "", false
	}

	service = path[:lastSlash]
	method = path[lastSlash+1:]

	return service, method, true
}
