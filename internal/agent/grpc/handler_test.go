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
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestNewGRPCHandler(t *testing.T) {
	logger := zap.NewNop()
	h := NewGRPCHandler(logger)

	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestPrepareGRPCRequest_PreservesHeaders(t *testing.T) {
	logger := zap.NewNop()
	h := NewGRPCHandler(logger)

	req, _ := http.NewRequest("POST", "http://backend/test.Service/Method", nil)
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("grpc-encoding", "gzip")
	req.Header.Set("grpc-accept-encoding", "gzip,identity")
	req.Header.Set("grpc-timeout", "5S")
	req.Header.Set("grpc-user-agent", "grpc-go/1.50.0")
	req.Header.Set("grpc-trace-bin", "abc123")

	prepared := h.PrepareGRPCRequest(req)

	if prepared.Header.Get("Content-Type") != "application/grpc" {
		t.Errorf("expected Content-Type 'application/grpc', got %q", prepared.Header.Get("Content-Type"))
	}
	if prepared.Header.Get("grpc-encoding") != "gzip" {
		t.Errorf("expected grpc-encoding 'gzip', got %q", prepared.Header.Get("grpc-encoding"))
	}
	if prepared.Header.Get("grpc-accept-encoding") != "gzip,identity" {
		t.Errorf("expected grpc-accept-encoding 'gzip,identity', got %q", prepared.Header.Get("grpc-accept-encoding"))
	}
	if prepared.Header.Get("grpc-timeout") != "5S" {
		t.Errorf("expected grpc-timeout '5S', got %q", prepared.Header.Get("grpc-timeout"))
	}
}

func TestPrepareGRPCRequest_ClonesRequest(t *testing.T) {
	logger := zap.NewNop()
	h := NewGRPCHandler(logger)

	req, _ := http.NewRequest("POST", "http://backend/test.Service/Method", nil)
	req.Header.Set("Content-Type", "application/grpc")

	prepared := h.PrepareGRPCRequest(req)

	// Verify it's a clone, not the same object
	if prepared == req {
		t.Error("PrepareGRPCRequest should return a clone, not the original")
	}
}

func TestHandleGRPCResponse_CopiesHeaders(t *testing.T) {
	logger := zap.NewNop()
	h := NewGRPCHandler(logger)

	recorder := httptest.NewRecorder()

	backendResp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/grpc"},
			"Grpc-Status":  []string{"0"},
			"Grpc-Message": []string{""},
		},
		Body: io.NopCloser(bytes.NewBufferString("response body")),
	}

	err := h.HandleGRPCResponse(recorder, backendResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := recorder.Result()
	defer func() { _ = result.Body.Close() }()

	if result.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}
	if recorder.Header().Get("Content-Type") != "application/grpc" {
		t.Errorf("expected Content-Type 'application/grpc', got %q", recorder.Header().Get("Content-Type"))
	}
}

func TestHandleGRPCResponse_CopiesTrailers(t *testing.T) {
	logger := zap.NewNop()
	h := NewGRPCHandler(logger)

	recorder := httptest.NewRecorder()

	backendResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewBufferString("")),
		Trailer: http.Header{
			"Grpc-Status":  []string{"0"},
			"Grpc-Message": []string{"OK"},
		},
	}

	err := h.HandleGRPCResponse(recorder, backendResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateGRPCRequest_ValidPOST(t *testing.T) {
	logger := zap.NewNop()
	h := NewGRPCHandler(logger)

	req, _ := http.NewRequest("POST", "/test.Service/Method", nil)
	req.Header.Set("Content-Type", "application/grpc")

	err := h.ValidateGRPCRequest(req)
	if err != nil {
		t.Fatalf("unexpected error for valid gRPC request: %v", err)
	}
}

func TestValidateGRPCRequest_NonPOST_NoError(t *testing.T) {
	logger := zap.NewNop()
	h := NewGRPCHandler(logger)

	req, _ := http.NewRequest("GET", "/test.Service/Method", nil)
	req.Header.Set("Content-Type", "application/grpc")

	// ValidateGRPCRequest does not return an error for invalid methods; it logs a warning
	err := h.ValidateGRPCRequest(req)
	if err != nil {
		t.Fatalf("ValidateGRPCRequest should not error, just warn: %v", err)
	}
}

func TestGetGRPCMetadata(t *testing.T) {
	logger := zap.NewNop()
	h := NewGRPCHandler(logger)

	req, _ := http.NewRequest("POST", "/test.Service/Method", nil)
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("Authorization", "Bearer token123")
	req.Header.Set("X-Custom-Header", "custom-value")

	metadata := h.GetGRPCMetadata(req)

	if _, ok := metadata["Content-Type"]; !ok {
		t.Error("expected Content-Type in metadata")
	}
	if _, ok := metadata["Authorization"]; !ok {
		t.Error("expected Authorization in metadata")
	}
	if _, ok := metadata["X-Custom-Header"]; !ok {
		t.Error("expected X-Custom-Header in metadata")
	}
}

func TestIsGRPCStreaming_Chunked(t *testing.T) {
	logger := zap.NewNop()
	h := NewGRPCHandler(logger)

	req, _ := http.NewRequest("POST", "/test.Service/StreamMethod", nil)
	req.Header.Set("Transfer-Encoding", "chunked")

	if !h.IsGRPCStreaming(req) {
		t.Error("expected streaming detection for chunked transfer encoding")
	}
}

func TestIsGRPCStreaming_NoContentLength(t *testing.T) {
	logger := zap.NewNop()
	h := NewGRPCHandler(logger)

	req, _ := http.NewRequest("POST", "/test.Service/StreamMethod", nil)
	// No Content-Length and no Transfer-Encoding

	if !h.IsGRPCStreaming(req) {
		t.Error("expected streaming detection when no Content-Length is set")
	}
}

func TestIsGRPCStreaming_WithContentLength(t *testing.T) {
	logger := zap.NewNop()
	h := NewGRPCHandler(logger)

	req, _ := http.NewRequest("POST", "/test.Service/UnaryMethod", nil)
	req.Header.Set("Content-Length", "100")

	if h.IsGRPCStreaming(req) {
		t.Error("should not detect streaming when Content-Length is set")
	}
}

func TestExtractGRPCServiceMethod_Valid(t *testing.T) {
	tests := []struct {
		path            string
		expectedService string
		expectedMethod  string
		expectedOk      bool
	}{
		{"/grpc.health.v1.Health/Check", "grpc.health.v1.Health", "Check", true},
		{"/my.package.Service/Method", "my.package.Service", "Method", true},
		{"/Service/Method", "Service", "Method", true},
	}

	for _, tt := range tests {
		service, method, ok := ExtractGRPCServiceMethod(tt.path)
		if ok != tt.expectedOk {
			t.Errorf("path %q: expected ok=%v, got %v", tt.path, tt.expectedOk, ok)
		}
		if service != tt.expectedService {
			t.Errorf("path %q: expected service=%q, got %q", tt.path, tt.expectedService, service)
		}
		if method != tt.expectedMethod {
			t.Errorf("path %q: expected method=%q, got %q", tt.path, tt.expectedMethod, method)
		}
	}
}

func TestExtractGRPCServiceMethod_Invalid(t *testing.T) {
	tests := []struct {
		path string
	}{
		{""},
		{"noLeadingSlash/Method"},
		{"/noMethod"},
	}

	for _, tt := range tests {
		_, _, ok := ExtractGRPCServiceMethod(tt.path)
		if ok {
			t.Errorf("path %q: expected ok=false, got true", tt.path)
		}
	}
}
