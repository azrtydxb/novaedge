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

package protocol

import (
	"net/http"
	"testing"
)

func TestIsGRPCRequest_HTTP2WithGRPCContentType(t *testing.T) {
	req, _ := http.NewRequest("POST", "/test.Service/Method", nil)
	req.ProtoMajor = 2
	req.Header.Set("Content-Type", "application/grpc")

	if !IsGRPCRequest(req) {
		t.Error("expected HTTP/2 request with application/grpc to be detected as gRPC")
	}
}

func TestIsGRPCRequest_HTTP2WithGRPCProto(t *testing.T) {
	req, _ := http.NewRequest("POST", "/test.Service/Method", nil)
	req.ProtoMajor = 2
	req.Header.Set("Content-Type", "application/grpc+proto")

	if !IsGRPCRequest(req) {
		t.Error("expected HTTP/2 request with application/grpc+proto to be detected as gRPC")
	}
}

func TestIsGRPCRequest_HTTP1_NotGRPC(t *testing.T) {
	req, _ := http.NewRequest("POST", "/test.Service/Method", nil)
	req.ProtoMajor = 1
	req.Header.Set("Content-Type", "application/grpc")

	if IsGRPCRequest(req) {
		t.Error("HTTP/1.1 request should not be detected as gRPC even with grpc content type")
	}
}

func TestIsGRPCRequest_HTTP2WithJSONContentType(t *testing.T) {
	req, _ := http.NewRequest("POST", "/api/endpoint", nil)
	req.ProtoMajor = 2
	req.Header.Set("Content-Type", "application/json")

	if IsGRPCRequest(req) {
		t.Error("HTTP/2 request with application/json should not be detected as gRPC")
	}
}

func TestIsGRPCRequest_HTTP2NoContentType(t *testing.T) {
	req, _ := http.NewRequest("GET", "/healthz", nil)
	req.ProtoMajor = 2

	if IsGRPCRequest(req) {
		t.Error("HTTP/2 request without content type should not be detected as gRPC")
	}
}

func TestIsWebSocketUpgrade_Valid(t *testing.T) {
	req, _ := http.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "upgrade")

	if !IsWebSocketUpgrade(req) {
		t.Error("expected WebSocket upgrade to be detected")
	}
}

func TestIsWebSocketUpgrade_CaseInsensitiveConnection(t *testing.T) {
	req, _ := http.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")

	if !IsWebSocketUpgrade(req) {
		t.Error("expected WebSocket upgrade with mixed case Connection header to be detected")
	}
}

func TestIsWebSocketUpgrade_NoUpgradeHeader(t *testing.T) {
	req, _ := http.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "upgrade")

	if IsWebSocketUpgrade(req) {
		t.Error("should not detect WebSocket without Upgrade header")
	}
}

func TestIsWebSocketUpgrade_NoConnectionHeader(t *testing.T) {
	req, _ := http.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "websocket")

	if IsWebSocketUpgrade(req) {
		t.Error("should not detect WebSocket without Connection header")
	}
}

func TestIsWebSocketUpgrade_WrongUpgradeValue(t *testing.T) {
	req, _ := http.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "h2c")
	req.Header.Set("Connection", "upgrade")

	if IsWebSocketUpgrade(req) {
		t.Error("should not detect WebSocket with non-websocket Upgrade header")
	}
}

func TestIsWebSocketUpgrade_NormalHTTPRequest(t *testing.T) {
	req, _ := http.NewRequest("GET", "/api/data", nil)

	if IsWebSocketUpgrade(req) {
		t.Error("normal HTTP request should not be detected as WebSocket upgrade")
	}
}
