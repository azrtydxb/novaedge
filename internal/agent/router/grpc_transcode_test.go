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
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// buildTestFileDescriptorSet creates a minimal FileDescriptorSet with a single
// service "test.TestService" containing a unary method "Echo" that takes and
// returns a message with a single string field "message".
func buildTestFileDescriptorSet() ([]byte, protoreflect.MessageDescriptor, protoreflect.MessageDescriptor) {
	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	labelOptional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	fieldNum := int32(1)

	fdp := &descriptorpb.FileDescriptorProto{
		Name:    strPtr("test.proto"),
		Package: strPtr("test"),
		Syntax:  strPtr("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: strPtr("EchoRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   strPtr("message"),
						Number: &fieldNum,
						Type:   &stringType,
						Label:  &labelOptional,
					},
				},
			},
			{
				Name: strPtr("EchoResponse"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   strPtr("message"),
						Number: &fieldNum,
						Type:   &stringType,
						Label:  &labelOptional,
					},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: strPtr("TestService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       strPtr("Echo"),
						InputType:  strPtr(".test.EchoRequest"),
						OutputType: strPtr(".test.EchoResponse"),
					},
				},
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{fdp},
	}

	fdsBytes, err := proto.Marshal(fds)
	if err != nil {
		panic("failed to marshal test FileDescriptorSet: " + err.Error())
	}

	// Build descriptors to return message descriptors for test helpers.
	files, err := protodesc.NewFiles(fds)
	if err != nil {
		panic("failed to create test file registry: " + err.Error())
	}

	fd, err := files.FindFileByPath("test.proto")
	if err != nil {
		panic("failed to find test.proto: " + err.Error())
	}

	inputDesc := fd.Messages().ByName("EchoRequest")
	outputDesc := fd.Messages().ByName("EchoResponse")

	return fdsBytes, inputDesc, outputDesc
}

func strPtr(s string) *string {
	return &s
}

func TestGRPCTranscodeMiddleware_NonMatchingPassthrough(t *testing.T) {
	fdsBytes, _, _ := buildTestFileDescriptorSet()

	config := &GRPCTranscodeConfig{
		ProtoDescriptors: fdsBytes,
		Services: map[string]string{
			"POST /v1/echo": "test.TestService/Echo",
		},
	}

	logger := zap.NewNop()
	mw := NewGRPCTranscodeMiddleware(config, logger)

	backendCalled := false
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("passthrough"))
	})

	handler := mw.Wrap(backend)

	// Request a path that does not match any mapping.
	req := httptest.NewRequest(http.MethodGet, "/not-mapped", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !backendCalled {
		t.Error("expected backend to be called for non-matching request")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "passthrough" {
		t.Errorf("expected body 'passthrough', got %q", rec.Body.String())
	}
}

func TestGRPCTranscodeMiddleware_ContentTypeValidation(t *testing.T) {
	fdsBytes, _, _ := buildTestFileDescriptorSet()

	config := &GRPCTranscodeConfig{
		ProtoDescriptors: fdsBytes,
		Services: map[string]string{
			"POST /v1/echo": "test.TestService/Echo",
		},
	}

	logger := zap.NewNop()
	mw := NewGRPCTranscodeMiddleware(config, logger)

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/echo", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected status 415 for wrong content type, got %d", rec.Code)
	}
}

func TestGRPCTranscodeMiddleware_JSONToProtobufConversion(t *testing.T) {
	fdsBytes, inputDesc, outputDesc := buildTestFileDescriptorSet()

	config := &GRPCTranscodeConfig{
		ProtoDescriptors: fdsBytes,
		Services: map[string]string{
			"POST /v1/echo": "test.TestService/Echo",
		},
	}

	logger := zap.NewNop()
	mw := NewGRPCTranscodeMiddleware(config, logger)

	// Simulate a gRPC backend that receives protobuf and returns protobuf.
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify gRPC headers.
		if ct := r.Header.Get("Content-Type"); ct != "application/grpc" {
			t.Errorf("expected Content-Type application/grpc, got %q", ct)
		}
		if te := r.Header.Get("Te"); te != "trailers" {
			t.Errorf("expected Te: trailers, got %q", te)
		}

		// Read the gRPC-framed request.
		body, err := readAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}

		// Decode gRPC frame.
		payload, err := decodeGRPCFrame(body)
		if err != nil {
			t.Fatalf("failed to decode gRPC frame: %v", err)
		}

		// Unmarshal the request protobuf.
		reqMsg := dynamicpb.NewMessage(inputDesc)
		if unmarshalErr := proto.Unmarshal(payload, reqMsg); unmarshalErr != nil {
			t.Fatalf("failed to unmarshal request: %v", unmarshalErr)
		}

		// Verify the message field.
		msgField := reqMsg.Get(inputDesc.Fields().ByName("message"))
		if msgField.String() != "hello" {
			t.Errorf("expected message 'hello', got %q", msgField.String())
		}

		// Build a response message.
		respMsg := dynamicpb.NewMessage(outputDesc)
		respMsg.Set(outputDesc.Fields().ByName("message"), protoreflect.ValueOfString("world"))

		respBytes, marshalErr := proto.Marshal(respMsg)
		if marshalErr != nil {
			t.Fatalf("failed to marshal response: %v", marshalErr)
		}

		// Write gRPC-framed response.
		respFrame := encodeGRPCFrame(respBytes)
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respFrame)
	})

	handler := mw.Wrap(backend)

	jsonBody := `{"message":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/echo", bytes.NewReader([]byte(jsonBody)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	respCT := rec.Header().Get("Content-Type")
	if respCT != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", respCT)
	}

	body := rec.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"world"`)) {
		t.Errorf("expected response to contain 'world', got %q", body)
	}
}

func TestGRPCTranscodeMiddleware_ErrorCodeMapping(t *testing.T) {
	fdsBytes, _, _ := buildTestFileDescriptorSet()

	config := &GRPCTranscodeConfig{
		ProtoDescriptors: fdsBytes,
		Services: map[string]string{
			"POST /v1/echo": "test.TestService/Echo",
		},
	}

	logger := zap.NewNop()
	mw := NewGRPCTranscodeMiddleware(config, logger)

	// Backend returns gRPC NOT_FOUND error.
	backend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "5")
		w.Header().Set("Grpc-Message", "resource not found")
		w.WriteHeader(http.StatusOK)
	})

	handler := mw.Wrap(backend)

	req := httptest.NewRequest(http.MethodPost, "/v1/echo", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected HTTP 404 for gRPC NOT_FOUND, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !bytes.Contains([]byte(body), []byte("resource not found")) {
		t.Errorf("expected error message in body, got %q", body)
	}
}

func TestGRPCStatusCodeMappings(t *testing.T) {
	tests := []struct {
		grpcCode int
		httpCode int
	}{
		{0, 200},
		{1, 499},
		{2, 500},
		{3, 400},
		{4, 504},
		{5, 404},
		{6, 409},
		{7, 403},
		{8, 429},
		{9, 400},
		{10, 409},
		{11, 400},
		{12, 501},
		{13, 500},
		{14, 503},
		{15, 500},
		{16, 401},
		{99, 500}, // unknown maps to 500
	}

	for _, tt := range tests {
		got := grpcStatusToHTTP(tt.grpcCode)
		if got != tt.httpCode {
			t.Errorf("grpcStatusToHTTP(%d) = %d, want %d", tt.grpcCode, got, tt.httpCode)
		}
	}
}

func TestEncodeDecodeGRPCFrame(t *testing.T) {
	original := []byte("hello world")
	frame := encodeGRPCFrame(original)

	// Verify frame structure.
	if frame[0] != 0 {
		t.Errorf("expected compressed flag 0, got %d", frame[0])
	}

	msgLen := binary.BigEndian.Uint32(frame[1:5])
	if int(msgLen) != len(original) {
		t.Errorf("expected length %d, got %d", len(original), msgLen)
	}

	// Decode.
	decoded, err := decodeGRPCFrame(frame)
	if err != nil {
		t.Fatalf("decodeGRPCFrame failed: %v", err)
	}

	if !bytes.Equal(decoded, original) {
		t.Errorf("decoded %q, want %q", decoded, original)
	}
}

func TestDecodeGRPCFrame_Empty(t *testing.T) {
	decoded, err := decodeGRPCFrame(nil)
	if err != nil {
		t.Fatalf("decodeGRPCFrame(nil) error: %v", err)
	}
	if decoded != nil {
		t.Errorf("expected nil for empty input, got %v", decoded)
	}
}

func TestDecodeGRPCFrame_TooShort(t *testing.T) {
	_, err := decodeGRPCFrame([]byte{0, 1, 2})
	if err == nil {
		t.Error("expected error for too-short frame")
	}
}

// readAll reads all bytes from an io.Reader, used in tests.
func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	return buf.Bytes(), err
}
