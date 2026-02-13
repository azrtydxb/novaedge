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

// Package grpc provides gRPC-specific request handling, status code mapping,
// and metadata forwarding for the reverse proxy.
package grpc

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
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

// Code represents standard gRPC status codes
type Code int

const (
	// CodeOK indicates the operation was successful
	CodeOK Code = 0
	// CodeCancelled indicates the operation was cancelled
	CodeCancelled Code = 1
	// CodeUnknown indicates an unknown error
	CodeUnknown Code = 2
	// CodeInvalidArgument indicates invalid arguments
	CodeInvalidArgument Code = 3
	// CodeDeadlineExceeded indicates deadline exceeded
	CodeDeadlineExceeded Code = 4
	// CodeNotFound indicates the requested entity was not found
	CodeNotFound Code = 5
	// CodeAlreadyExists indicates the entity already exists
	CodeAlreadyExists Code = 6
	// CodePermissionDenied indicates permission was denied
	CodePermissionDenied Code = 7
	// CodeResourceExhausted indicates resources are exhausted
	CodeResourceExhausted Code = 8
	// CodeFailedPrecondition indicates a failed precondition
	CodeFailedPrecondition Code = 9
	// CodeAborted indicates the operation was aborted
	CodeAborted Code = 10
	// CodeOutOfRange indicates an out of range value
	CodeOutOfRange Code = 11
	// CodeUnimplemented indicates the operation is not implemented
	CodeUnimplemented Code = 12
	// CodeInternal indicates an internal error
	CodeInternal Code = 13
	// CodeUnavailable indicates the service is unavailable
	CodeUnavailable Code = 14
	// CodeDataLoss indicates data loss
	CodeDataLoss Code = 15
	// CodeUnauthenticated indicates the caller is not authenticated
	CodeUnauthenticated Code = 16
)

// IsGRPCRequest checks if an HTTP request is a gRPC request
// Delegates to protocol package for consistency
var IsGRPCRequest = protocol.IsGRPCRequest

// Handler handles gRPC-specific request/response proxying
type Handler struct {
	logger *zap.Logger
}

// NewHandler creates a new gRPC handler
func NewHandler(logger *zap.Logger) *Handler {
	return &Handler{
		logger: logger,
	}
}

// PrepareGRPCRequest prepares a gRPC request for proxying to backend
// This ensures all gRPC-specific headers and metadata are properly forwarded
func (h *Handler) PrepareGRPCRequest(r *http.Request) *http.Request {
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
func (h *Handler) HandleGRPCResponse(w http.ResponseWriter, backendResp *http.Response) error {
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
func (h *Handler) ValidateGRPCRequest(r *http.Request) error {
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
func (h *Handler) GetGRPCMetadata(r *http.Request) map[string][]string {
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
func (h *Handler) IsGRPCStreaming(r *http.Request) bool {
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

// HTTPStatusToCode maps HTTP status codes to gRPC status codes
func HTTPStatusToCode(httpStatus int) Code {
	switch httpStatus {
	case http.StatusOK:
		return CodeOK
	case http.StatusBadRequest:
		return CodeInvalidArgument
	case http.StatusUnauthorized:
		return CodeUnauthenticated
	case http.StatusForbidden:
		return CodePermissionDenied
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusConflict:
		return CodeAlreadyExists
	case http.StatusTooManyRequests:
		return CodeResourceExhausted
	case http.StatusInternalServerError:
		return CodeInternal
	case http.StatusNotImplemented:
		return CodeUnimplemented
	case http.StatusServiceUnavailable:
		return CodeUnavailable
	case http.StatusGatewayTimeout:
		return CodeDeadlineExceeded
	default:
		return CodeUnknown
	}
}

// CodeToHTTPStatus maps gRPC status codes to HTTP status codes
func CodeToHTTPStatus(code Code) int {
	switch code {
	case CodeOK:
		return http.StatusOK
	case CodeCancelled:
		return 499 // Client Closed Request
	case CodeInvalidArgument:
		return http.StatusBadRequest
	case CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	case CodeNotFound:
		return http.StatusNotFound
	case CodeAlreadyExists:
		return http.StatusConflict
	case CodePermissionDenied:
		return http.StatusForbidden
	case CodeResourceExhausted:
		return http.StatusTooManyRequests
	case CodeFailedPrecondition:
		return http.StatusBadRequest
	case CodeAborted:
		return http.StatusConflict
	case CodeOutOfRange:
		return http.StatusBadRequest
	case CodeUnimplemented:
		return http.StatusNotImplemented
	case CodeInternal:
		return http.StatusInternalServerError
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	case CodeDataLoss:
		return http.StatusInternalServerError
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// WriteGRPCError writes a gRPC error response with proper trailers
func (h *Handler) WriteGRPCError(w http.ResponseWriter, code Code, message string) {
	w.Header().Set("Content-Type", grpcContentType)
	w.Header().Set(grpcStatusHeader, strconv.Itoa(int(code)))
	w.Header().Set(grpcMessageHeader, message)
	w.WriteHeader(http.StatusOK) // gRPC always uses 200 OK at the HTTP level

	h.logger.Debug("Wrote gRPC error response",
		zap.Int("grpc_code", int(code)),
		zap.String("message", message),
	)
}

// MatchesGRPCService checks if a request path matches a gRPC service name
func MatchesGRPCService(path, serviceName string) bool {
	service, _, ok := ExtractGRPCServiceMethod(path)
	if !ok {
		return false
	}
	return service == serviceName
}

// MatchesGRPCMethod checks if a request path matches a gRPC service and method
func MatchesGRPCMethod(path, serviceName, methodName string) bool {
	service, method, ok := ExtractGRPCServiceMethod(path)
	if !ok {
		return false
	}
	return service == serviceName && method == methodName
}

// IsGRPCHealthCheck checks if the request is a gRPC health check
// per the gRPC health checking protocol (grpc.health.v1.Health/Check)
func IsGRPCHealthCheck(path string) bool {
	return path == "/grpc.health.v1.Health/Check" || path == "/grpc.health.v1.Health/Watch"
}

// ForwardGRPCMetadata copies gRPC metadata from source to destination headers
func ForwardGRPCMetadata(src, dst http.Header) {
	for key, values := range src {
		lowerKey := strings.ToLower(key)
		// Forward grpc-* headers and custom metadata
		if strings.HasPrefix(lowerKey, "grpc-") ||
			strings.HasPrefix(lowerKey, "x-") ||
			lowerKey == "authorization" ||
			lowerKey == "user-agent" {
			for _, value := range values {
				dst.Add(key, value)
			}
		}
	}
}

// CodeName returns the string name of a gRPC status code
func CodeName(code Code) string {
	names := map[Code]string{
		CodeOK:                 "OK",
		CodeCancelled:          "CANCELLED",
		CodeUnknown:            "UNKNOWN",
		CodeInvalidArgument:    "INVALID_ARGUMENT",
		CodeDeadlineExceeded:   "DEADLINE_EXCEEDED",
		CodeNotFound:           "NOT_FOUND",
		CodeAlreadyExists:      "ALREADY_EXISTS",
		CodePermissionDenied:   "PERMISSION_DENIED",
		CodeResourceExhausted:  "RESOURCE_EXHAUSTED",
		CodeFailedPrecondition: "FAILED_PRECONDITION",
		CodeAborted:            "ABORTED",
		CodeOutOfRange:         "OUT_OF_RANGE",
		CodeUnimplemented:      "UNIMPLEMENTED",
		CodeInternal:           "INTERNAL",
		CodeUnavailable:        "UNAVAILABLE",
		CodeDataLoss:           "DATA_LOSS",
		CodeUnauthenticated:    "UNAUTHENTICATED",
	}
	if name, ok := names[code]; ok {
		return name
	}
	return fmt.Sprintf("CODE_%d", code)
}
