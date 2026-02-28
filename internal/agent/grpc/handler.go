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
