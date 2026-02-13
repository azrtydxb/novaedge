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
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIsGRPCWebRequest_DetectsContentTypes verifies that all four gRPC-Web
// content types are correctly identified as gRPC-Web requests.
func TestIsGRPCWebRequest_DetectsContentTypes(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{
			name:        "application/grpc-web",
			contentType: ContentTypeGRPCWeb,
			want:        true,
		},
		{
			name:        "application/grpc-web+proto",
			contentType: ContentTypeGRPCWebProto,
			want:        true,
		},
		{
			name:        "application/grpc-web-text",
			contentType: ContentTypeGRPCWebText,
			want:        true,
		},
		{
			name:        "application/grpc-web-text+proto",
			contentType: ContentTypeGRPCWebTextProto,
			want:        true,
		},
		{
			name:        "with charset parameter",
			contentType: "application/grpc-web; charset=utf-8",
			want:        true,
		},
		{
			name:        "standard grpc is not grpc-web",
			contentType: ContentTypeGRPC,
			want:        false,
		},
		{
			name:        "regular json",
			contentType: "application/json",
			want:        false,
		},
		{
			name:        "empty content type",
			contentType: "",
			want:        false,
		},
		{
			name:        "text/html",
			contentType: "text/html",
			want:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/test.Service/Method", nil)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			got := IsGRPCWebRequest(req)
			if got != tc.want {
				t.Errorf("IsGRPCWebRequest(%q) = %v, want %v", tc.contentType, got, tc.want)
			}
		})
	}
}

// TestGRPCWebMiddleware_PassthroughNonGRPCWeb verifies that requests without
// gRPC-Web content types are forwarded to the next handler unmodified.
func TestGRPCWebMiddleware_PassthroughNonGRPCWeb(t *testing.T) {
	logger := testLogger()
	mw := NewGRPCWebMiddleware(nil, logger)

	var capturedContentType string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	})

	handler := mw.Wrap(backend)

	req := httptest.NewRequest(http.MethodPost, "/api/data", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	if result.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}

	if capturedContentType != "application/json" {
		t.Errorf("expected backend to receive application/json, got %q", capturedContentType)
	}

	body, _ := io.ReadAll(result.Body)
	if string(body) != "hello" {
		t.Errorf("expected body 'hello', got %q", string(body))
	}

	// Content-Type should NOT be rewritten for non-gRPC-Web responses.
	if ct := result.Header.Get("Content-Type"); ct == ContentTypeGRPCWeb {
		t.Errorf("non-gRPC-Web response should not have grpc-web content type, got %q", ct)
	}
}

// TestGRPCWebMiddleware_ConvertsContentType verifies that the middleware rewrites
// the request Content-Type from gRPC-Web to standard gRPC, and converts the
// response Content-Type back to gRPC-Web.
func TestGRPCWebMiddleware_ConvertsContentType(t *testing.T) {
	logger := testLogger()
	mw := NewGRPCWebMiddleware(nil, logger)

	var capturedContentType string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		w.Header().Set("Grpc-Status", "0")
		w.Header().Set("Grpc-Message", "OK")
		w.WriteHeader(http.StatusOK)
		// Write a minimal gRPC frame: flag(1) + length(4) + payload.
		frame := buildGRPCFrame([]byte("response-payload"))
		_, _ = w.Write(frame)
	})

	handler := mw.Wrap(backend)

	// Build a minimal gRPC-Web request body (flag + length + payload).
	reqBody := buildGRPCFrame([]byte("request-payload"))

	req := httptest.NewRequest(http.MethodPost, "/test.Service/Method", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", ContentTypeGRPCWeb)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	// Backend should receive standard gRPC content type.
	if capturedContentType != ContentTypeGRPC {
		t.Errorf("expected backend Content-Type %q, got %q", ContentTypeGRPC, capturedContentType)
	}

	// Response content type should be gRPC-Web.
	respCT := result.Header.Get("Content-Type")
	if respCT != ContentTypeGRPCWeb {
		t.Errorf("expected response Content-Type %q, got %q", ContentTypeGRPCWeb, respCT)
	}

	// Verify the CORS exposure headers are set.
	expose := result.Header.Get("Access-Control-Expose-Headers")
	if !strings.Contains(expose, "Grpc-Status") {
		t.Errorf("expected Access-Control-Expose-Headers to contain Grpc-Status, got %q", expose)
	}
}

// TestGRPCWebMiddleware_TextVariantBase64 verifies that the grpc-web-text variant
// decodes base64 request bodies and encodes response bodies in base64.
func TestGRPCWebMiddleware_TextVariantBase64(t *testing.T) {
	logger := testLogger()
	mw := NewGRPCWebMiddleware(nil, logger)

	requestPayload := []byte("text-request-data")
	responsePayload := []byte("text-response-data")

	var capturedBody []byte
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
		frame := buildGRPCFrame(responsePayload)
		_, _ = w.Write(frame)
	})

	handler := mw.Wrap(backend)

	// Build the request: gRPC frame then base64-encode it.
	reqFrame := buildGRPCFrame(requestPayload)
	b64Body := base64.StdEncoding.EncodeToString(reqFrame)

	req := httptest.NewRequest(http.MethodPost, "/test.Service/TextMethod",
		strings.NewReader(b64Body))
	req.Header.Set("Content-Type", ContentTypeGRPCWebText)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	// Verify the backend received decoded binary gRPC frame.
	expectedFrame := buildGRPCFrame(requestPayload)
	if !bytes.Equal(capturedBody, expectedFrame) {
		t.Errorf("backend received unexpected body:\n  got:  %x\n  want: %x", capturedBody, expectedFrame)
	}

	// Response Content-Type should be grpc-web-text.
	respCT := result.Header.Get("Content-Type")
	if respCT != ContentTypeGRPCWebText {
		t.Errorf("expected response Content-Type %q, got %q", ContentTypeGRPCWebText, respCT)
	}

	// Response body should be base64-encoded.
	respBody, _ := io.ReadAll(result.Body)
	decoded, err := base64.StdEncoding.DecodeString(string(respBody))
	if err != nil {
		t.Fatalf("response body is not valid base64: %v", err)
	}

	// The decoded body should start with the response frame.
	expectedRespFrame := buildGRPCFrame(responsePayload)
	if !bytes.HasPrefix(decoded, expectedRespFrame) {
		t.Errorf("decoded response does not start with expected frame:\n  got:  %x\n  want prefix: %x",
			decoded, expectedRespFrame)
	}
}

// TestGRPCWebMiddleware_CORSPreflight verifies that OPTIONS preflight requests
// for gRPC-Web are handled correctly with appropriate CORS headers.
func TestGRPCWebMiddleware_CORSPreflight(t *testing.T) {
	logger := testLogger()
	config := &GRPCWebConfig{
		AllowedOrigins:   []string{"https://app.example.com", "*"},
		AllowCredentials: true,
	}
	mw := NewGRPCWebMiddleware(config, logger)

	backendCalled := false
	backend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backendCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Wrap(backend)

	req := httptest.NewRequest(http.MethodOptions, "/test.Service/Method", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type, x-grpc-web")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	if backendCalled {
		t.Error("backend should not be called for CORS preflight")
	}

	if result.StatusCode != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", result.StatusCode)
	}

	allowOrigin := result.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin != "https://app.example.com" {
		t.Errorf("expected Access-Control-Allow-Origin %q, got %q",
			"https://app.example.com", allowOrigin)
	}

	allowMethods := result.Header.Get("Access-Control-Allow-Methods")
	if !strings.Contains(allowMethods, "POST") {
		t.Errorf("expected Access-Control-Allow-Methods to contain POST, got %q", allowMethods)
	}

	allowHeaders := result.Header.Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowHeaders, "Content-Type") {
		t.Errorf("expected Access-Control-Allow-Headers to contain Content-Type, got %q", allowHeaders)
	}

	allowCreds := result.Header.Get("Access-Control-Allow-Credentials")
	if allowCreds != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials 'true', got %q", allowCreds)
	}
}

// TestGRPCWebMiddleware_CORSPreflightForbidden verifies that preflight with
// an unknown origin is rejected when AllowedOrigins does not match.
func TestGRPCWebMiddleware_CORSPreflightForbidden(t *testing.T) {
	logger := testLogger()
	config := &GRPCWebConfig{
		AllowedOrigins: []string{"https://trusted.example.com"},
	}
	mw := NewGRPCWebMiddleware(config, logger)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/test.Service/Method", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	if result.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403 for disallowed origin, got %d", result.StatusCode)
	}
}

// TestGRPCWebMiddleware_CORSPreflightNoOrigin verifies that preflight without
// an Origin header returns 204 without CORS headers.
func TestGRPCWebMiddleware_CORSPreflightNoOrigin(t *testing.T) {
	logger := testLogger()
	config := &GRPCWebConfig{
		AllowedOrigins: []string{"*"},
	}
	mw := NewGRPCWebMiddleware(config, logger)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/test.Service/Method", nil)
	// No Origin header.
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	if result.StatusCode != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", result.StatusCode)
	}

	if result.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("expected no CORS headers without Origin")
	}
}

// TestGRPCWebMiddleware_CORSWildcardOrigin verifies that wildcard origin returns
// "*" as the allowed origin.
func TestGRPCWebMiddleware_CORSWildcardOrigin(t *testing.T) {
	logger := testLogger()
	config := &GRPCWebConfig{
		AllowedOrigins: []string{"*"},
	}
	mw := NewGRPCWebMiddleware(config, logger)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/test.Service/Method", nil)
	req.Header.Set("Origin", "https://any-origin.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	allowOrigin := result.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin != "*" {
		t.Errorf("expected Access-Control-Allow-Origin '*', got %q", allowOrigin)
	}
}

// TestGRPCWebMiddleware_TrailerFrame verifies that gRPC trailers (grpc-status,
// grpc-message) are serialised as a gRPC-Web trailer frame in the response body.
func TestGRPCWebMiddleware_TrailerFrame(t *testing.T) {
	logger := testLogger()
	mw := NewGRPCWebMiddleware(nil, logger)

	backend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Grpc-Status", "0")
		w.Header().Set("Grpc-Message", "OK")
		w.WriteHeader(http.StatusOK)
		frame := buildGRPCFrame([]byte("data"))
		_, _ = w.Write(frame)
	})

	handler := mw.Wrap(backend)

	req := httptest.NewRequest(http.MethodPost, "/test.Service/Method",
		bytes.NewReader(buildGRPCFrame([]byte("req"))))
	req.Header.Set("Content-Type", ContentTypeGRPCWeb)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	body, _ := io.ReadAll(result.Body)

	// The body should contain the data frame followed by a trailer frame.
	// Data frame: flag=0x00 + 4-byte length + "data"
	dataFrame := buildGRPCFrame([]byte("data"))
	if !bytes.HasPrefix(body, dataFrame) {
		t.Errorf("response body should start with data frame, got %x", body[:min(len(body), 20)])
	}

	// Find the trailer frame after the data frame.
	trailerPart := body[len(dataFrame):]
	if len(trailerPart) < 5 {
		t.Fatalf("expected trailer frame after data frame, remaining bytes: %d", len(trailerPart))
	}

	// Trailer frame flag should be 0x80.
	if trailerPart[0] != grpcWebTrailerFlag {
		t.Errorf("expected trailer flag 0x80, got 0x%02x", trailerPart[0])
	}

	// Parse trailer length.
	trailerLen := binary.BigEndian.Uint32(trailerPart[1:5])
	if int(trailerLen) != len(trailerPart)-5 {
		t.Errorf("trailer length mismatch: header says %d, actual payload %d",
			trailerLen, len(trailerPart)-5)
	}

	// Verify trailer content contains grpc-status.
	trailerContent := string(trailerPart[5:])
	if !strings.Contains(trailerContent, "grpc-status: 0") {
		t.Errorf("trailer should contain 'grpc-status: 0', got %q", trailerContent)
	}
	if !strings.Contains(trailerContent, "grpc-message: OK") {
		t.Errorf("trailer should contain 'grpc-message: OK', got %q", trailerContent)
	}
}

// TestGRPCWebMiddleware_NonPreflightOptionsPassthrough verifies that an OPTIONS
// request that is not a gRPC-Web preflight is passed through to the backend.
func TestGRPCWebMiddleware_NonPreflightOptionsPassthrough(t *testing.T) {
	logger := testLogger()
	mw := NewGRPCWebMiddleware(nil, logger)

	backendCalled := false
	backend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backendCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Wrap(backend)

	// OPTIONS without Access-Control-Request-Method is not a preflight.
	req := httptest.NewRequest(http.MethodOptions, "/api/info", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !backendCalled {
		t.Error("non-preflight OPTIONS should be passed to backend")
	}
}

// TestGRPCWebMiddleware_GRPCWebProtoContentType verifies that the +proto
// variant is also correctly handled.
func TestGRPCWebMiddleware_GRPCWebProtoContentType(t *testing.T) {
	logger := testLogger()
	mw := NewGRPCWebMiddleware(nil, logger)

	var capturedContentType string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Wrap(backend)

	req := httptest.NewRequest(http.MethodPost, "/test.Service/Method",
		bytes.NewReader(buildGRPCFrame([]byte("proto-data"))))
	req.Header.Set("Content-Type", ContentTypeGRPCWebProto)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedContentType != ContentTypeGRPC {
		t.Errorf("expected backend Content-Type %q, got %q", ContentTypeGRPC, capturedContentType)
	}

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	// Response should use the binary grpc-web variant (not text).
	respCT := result.Header.Get("Content-Type")
	if respCT != ContentTypeGRPCWeb {
		t.Errorf("expected response Content-Type %q, got %q", ContentTypeGRPCWeb, respCT)
	}
}

// TestIsTextEncoding verifies the text encoding detection helper.
func TestIsTextEncoding(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{ContentTypeGRPCWebText, true},
		{ContentTypeGRPCWebTextProto, true},
		{"application/grpc-web-text; charset=utf-8", true},
		{ContentTypeGRPCWeb, false},
		{ContentTypeGRPCWebProto, false},
		{ContentTypeGRPC, false},
		{"application/json", false},
	}

	for _, tc := range tests {
		t.Run(tc.ct, func(t *testing.T) {
			got := isTextEncoding(tc.ct)
			if got != tc.want {
				t.Errorf("isTextEncoding(%q) = %v, want %v", tc.ct, got, tc.want)
			}
		})
	}
}

// TestGRPCWebMiddleware_DefaultConfig verifies that NewGRPCWebMiddleware
// handles a nil config gracefully.
func TestGRPCWebMiddleware_DefaultConfig(t *testing.T) {
	logger := testLogger()
	mw := NewGRPCWebMiddleware(nil, logger)
	if mw == nil {
		t.Fatal("NewGRPCWebMiddleware(nil, logger) returned nil")
	}
	if mw.config == nil {
		t.Fatal("expected default config, got nil")
	}
}

// buildGRPCFrame constructs a gRPC length-prefixed frame:
// 1-byte flag + 4-byte big-endian length + payload.
func buildGRPCFrame(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	frame[0] = 0x00
	binary.BigEndian.PutUint32(frame[1:5], safeIntToUint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}
