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
	"testing"
)

// FuzzDetectProtocol fuzzes protocol detection with arbitrary byte inputs.
func FuzzDetectProtocol(f *testing.F) {
	// Add corpus seeds with various protocol signatures
	seeds := [][]byte{
		// HTTP/1.1 requests
		[]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
		[]byte("POST /api HTTP/1.1\r\nContent-Length: 0\r\n\r\n"),
		[]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"),
		// HTTP/2 connection preface
		[]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"),
		// TLS handshake
		[]byte{0x16, 0x03, 0x01, 0x00, 0x05}, // TLS 1.0 ClientHello
		[]byte{0x16, 0x03, 0x03, 0x00, 0x05}, // TLS 1.2 ClientHello
		// WebSocket upgrade
		[]byte("GET /ws HTTP/1.1\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"),
		// gRPC (HTTP/2 based)
		[]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"),
		// Empty
		[]byte{},
		// Single byte
		[]byte{0},
		[]byte{255},
		// Random bytes
		[]byte{0x00, 0x01, 0x02, 0x03},
		[]byte{0xFF, 0xFE, 0xFD, 0xFC},
		// Very long input
		make([]byte, 10000),
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// DetectProtocol should not panic on any input
		protocol := DetectProtocol(data)
		_ = protocol // Just ensure no panic
	})
}

// Protocol represents a detected protocol type.
type Protocol int

const (
	ProtocolUnknown Protocol = iota
	ProtocolHTTP1
	ProtocolHTTP2
	ProtocolTLS
	ProtocolWebSocket
	ProtocolGRPC
)

// DetectProtocol attempts to detect the protocol from the first few bytes.
func DetectProtocol(data []byte) Protocol {
	if len(data) == 0 {
		return ProtocolUnknown
	}

	// Check for TLS handshake (starts with 0x16, followed by version)
	if len(data) >= 3 {
		if data[0] == 0x16 && data[1] == 0x03 {
			return ProtocolTLS
		}
	}

	// Check for HTTP/2 connection preface
	if len(data) >= 4 {
		if string(data[:4]) == "PRI " {
			return ProtocolHTTP2
		}
	}

	// Check for HTTP/1.x
	if len(data) >= 4 {
		prefix := string(data[:4])
		if prefix == "GET " || prefix == "POST" || prefix == "PUT " ||
			prefix == "DELE" || prefix == "HEAD" || prefix == "OPTI" ||
			prefix == "PATC" || prefix == "HTTP" {
			// Check for WebSocket upgrade
			dataStr := string(data)
			if contains(dataStr, "Upgrade: websocket") || contains(dataStr, "Upgrade: WebSocket") {
				return ProtocolWebSocket
			}
			return ProtocolHTTP1
		}
	}

	return ProtocolUnknown
}

// contains checks if s contains substr (case-sensitive).
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// FuzzParseHTTPMethod fuzzes HTTP method parsing.
func FuzzParseHTTPMethod(f *testing.F) {
	seeds := []string{
		"GET",
		"POST",
		"PUT",
		"DELETE",
		"HEAD",
		"OPTIONS",
		"PATCH",
		"CONNECT",
		"TRACE",
		"",           // Empty
		"GET ",       // With space
		" GET",       // Leading space
		"get",        // Lowercase
		"Get",        // Mixed case
		"G\x00T",     // Null byte
		"AAAAAAAAAA", // Long invalid
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, method string) {
		// IsValidMethod should not panic
		valid := IsValidMethod(method)
		_ = valid
	})
}

// IsValidMethod checks if a string is a valid HTTP method.
func IsValidMethod(method string) bool {
	if len(method) == 0 {
		return false
	}

	// HTTP methods should be uppercase letters
	for _, c := range method {
		if c < 'A' || c > 'Z' {
			return false
		}
	}

	// Check length (max 16 bytes per HTTP spec)
	if len(method) > 16 {
		return false
	}

	return true
}

// FuzzParseStatusCode fuzzes HTTP status code parsing.
func FuzzParseStatusCode(f *testing.F) {
	seeds := []int{
		200,
		201,
		301,
		400,
		404,
		500,
		0,    // Zero
		-1,   // Negative
		99,   // Too low
		1000, // Too high
		999,  // Max valid
		100,  // Min valid
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, code int) {
		// IsValidStatusCode should not panic
		valid := IsValidStatusCode(code)
		_ = valid
	})
}

// IsValidStatusCode checks if an integer is a valid HTTP status code.
func IsValidStatusCode(code int) bool {
	return code >= 100 && code <= 999
}
