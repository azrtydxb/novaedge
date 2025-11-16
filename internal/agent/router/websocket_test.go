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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"go.uber.org/zap/zaptest"
)

func TestNewWebSocketProxy(t *testing.T) {
	logger := zaptest.NewLogger(t)

	proxy := NewWebSocketProxy(logger)

	if proxy == nil {
		t.Fatal("Expected WebSocketProxy to be created")
	}

	if proxy.logger == nil {
		t.Error("Logger should be initialized")
	}

	if len(proxy.allowedOrigins) != 0 {
		t.Error("allowedOrigins should be empty for default constructor")
	}
}

func TestNewWebSocketProxyWithOrigins(t *testing.T) {
	logger := zaptest.NewLogger(t)
	allowedOrigins := []string{"http://example.com", "https://app.example.com"}

	proxy := NewWebSocketProxyWithOrigins(logger, allowedOrigins)

	if proxy == nil {
		t.Fatal("Expected WebSocketProxy to be created")
	}

	if len(proxy.allowedOrigins) != 2 {
		t.Errorf("Expected 2 allowed origins, got %d", len(proxy.allowedOrigins))
	}
}

func TestCheckOriginValidation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name           string
		allowedOrigins []string
		origin         string
		expected       bool
		description    string
	}{
		{
			name:           "exact_match",
			allowedOrigins: []string{"http://example.com"},
			origin:         "http://example.com",
			expected:       true,
			description:    "Exact origin match",
		},
		{
			name:           "no_match",
			allowedOrigins: []string{"http://example.com"},
			origin:         "http://different.com",
			expected:       false,
			description:    "Origin not in allowed list",
		},
		{
			name:           "wildcard_allow_all",
			allowedOrigins: []string{"*"},
			origin:         "http://any.com",
			expected:       true,
			description:    "Wildcard allows all",
		},
		{
			name:           "subdomain_wildcard",
			allowedOrigins: []string{"*.example.com"},
			origin:         "http://app.example.com",
			expected:       true,
			description:    "Subdomain wildcard match",
		},
		{
			name:           "subdomain_no_match",
			allowedOrigins: []string{"*.example.com"},
			origin:         "http://example.com",
			expected:       false,
			description:    "Subdomain wildcard non-match",
		},
		{
			name:           "empty_origin_header",
			allowedOrigins: []string{"http://example.com"},
			origin:         "",
			expected:       false,
			description:    "Missing Origin header",
		},
		{
			name:           "empty_allowed_list",
			allowedOrigins: []string{},
			origin:         "http://any.com",
			expected:       true,
			description:    "Empty allowed list permits all (insecure)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := NewWebSocketProxyWithOrigins(logger, tt.allowedOrigins)

			// Create a mock request
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			result := proxy.checkOrigin(req)

			if result != tt.expected {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expected, result)
			}
		})
	}
}

func TestIsWebSocketUpgrade(t *testing.T) {
	tests := []struct {
		name          string
		upgrade       string
		connection    string
		expected      bool
		description   string
	}{
		{
			name:        "valid_upgrade",
			upgrade:     "websocket",
			connection:  "Upgrade",
			expected:    true,
			description: "Valid WebSocket upgrade request",
		},
		{
			name:        "case_insensitive",
			upgrade:     "WebSocket",
			connection:  "upgrade",
			expected:    true,
			description: "Case insensitive upgrade header",
		},
		{
			name:        "missing_upgrade",
			upgrade:     "http",
			connection:  "Upgrade",
			expected:    false,
			description: "Missing Upgrade header",
		},
		{
			name:        "missing_connection",
			upgrade:     "websocket",
			connection:  "keep-alive",
			expected:    false,
			description: "Missing Connection upgrade",
		},
		{
			name:        "empty_headers",
			upgrade:     "",
			connection:  "",
			expected:    false,
			description: "Empty headers",
		},
		{
			name:        "connection_contains_upgrade",
			upgrade:     "websocket",
			connection:  "Upgrade, keep-alive",
			expected:    true,
			description: "Connection header contains 'upgrade'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.upgrade != "" {
				req.Header.Set("Upgrade", tt.upgrade)
			}
			if tt.connection != "" {
				req.Header.Set("Connection", tt.connection)
			}

			result := IsWebSocketUpgrade(req)

			if result != tt.expected {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expected, result)
			}
		})
	}
}

func TestBuildBackendWebSocketURL(t *testing.T) {
	tests := []struct {
		name        string
		backendURL  string
		requestPath string
		expected    string
		description string
	}{
		{
			name:        "http_to_ws",
			backendURL:  "http://backend.example.com:8080",
			requestPath: "/chat",
			expected:    "ws://backend.example.com:8080/chat",
			description: "HTTP to WS conversion",
		},
		{
			name:        "https_to_wss",
			backendURL:  "https://backend.example.com:8080",
			requestPath: "/chat",
			expected:    "wss://backend.example.com:8080/chat",
			description: "HTTPS to WSS conversion",
		},
		{
			name:        "with_query_params",
			backendURL:  "http://backend.example.com",
			requestPath: "/chat?room=123",
			expected:    "ws://backend.example.com/chat?room=123",
			description: "Preserve query parameters",
		},
		{
			name:        "root_path",
			backendURL:  "http://backend.example.com",
			requestPath: "/",
			expected:    "ws://backend.example.com/",
			description: "Root path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.requestPath, nil)

			result := buildBackendWebSocketURL(tt.backendURL, req)

			if result != tt.expected {
				t.Errorf("%s: expected %s, got %s", tt.description, tt.expected, result)
			}
		})
	}
}

func TestMatchWildcardOrigin(t *testing.T) {
	tests := []struct {
		pattern     string
		origin      string
		expected    bool
		description string
	}{
		{
			pattern:     "*.example.com",
			origin:      "http://app.example.com",
			expected:    true,
			description: "Subdomain wildcard match",
		},
		{
			pattern:     "*.example.com",
			origin:      "http://example.com",
			expected:    false,
			description: "Wildcard doesn't match base domain",
		},
		{
			pattern:     "*.example.com",
			origin:      "http://deep.sub.example.com",
			expected:    true,
			description: "Deep subdomain match",
		},
		{
			pattern:     "*",
			origin:      "http://any.com",
			expected:    false,
			description: "Single wildcard is prefix wildcard not match-all",
		},
		{
			pattern:     "example.com",
			origin:      "http://example.com",
			expected:    false,
			description: "Non-wildcard pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			result := matchWildcardOrigin(tt.pattern, tt.origin)

			if result != tt.expected {
				t.Errorf("Pattern %s against %s: expected %v, got %v",
					tt.pattern, tt.origin, tt.expected, result)
			}
		})
	}
}

func TestCopyWebSocketHeaders(t *testing.T) {
	src := make(http.Header)
	src.Set("Sec-WebSocket-Key", "test-key-123")
	src.Set("Sec-WebSocket-Version", "13")
	src.Set("Origin", "http://example.com")
	src.Set("X-Custom-Header", "custom-value")
	src.Set("X-Another-Header", "another-value")
	src.Set("Authorization", "Bearer token")

	dst := make(http.Header)
	copyWebSocketHeaders(src, dst)

	tests := []struct {
		header   string
		expected bool
		description string
	}{
		{"Sec-WebSocket-Key", true, "Should copy WebSocket key"},
		{"Sec-WebSocket-Version", true, "Should copy WebSocket version"},
		{"Origin", true, "Should copy Origin"},
		{"X-Custom-Header", true, "Should copy custom X- header"},
		{"X-Another-Header", true, "Should copy another custom X- header"},
		{"Authorization", false, "Should not copy Authorization"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			hasHeader := dst.Get(tt.header) != ""

			if hasHeader != tt.expected {
				if tt.expected {
					t.Errorf("Expected header %s to be copied", tt.header)
				} else {
					t.Errorf("Expected header %s not to be copied", tt.header)
				}
			}
		})
	}
}

func TestProxyWebSocketConnectionLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	proxy := NewWebSocketProxyWithOrigins(logger, []string{"http://localhost"})

	t.Run("backend_connection_failure", func(t *testing.T) {
		// Create a request with invalid backend URL
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/ws", nil)
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Origin", "http://localhost")

		// ProxyWebSocket will try to connect to non-existent backend
		err := proxy.ProxyWebSocket(w, req, "http://nonexistent.local:9999")

		// Should return error for failed backend connection
		if err == nil {
			t.Error("Expected error when backend connection fails")
		}
	})

	t.Run("origin_validation_rejects_invalid", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/ws", nil)
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Origin", "http://malicious.com") // Not in allowed list

		err := proxy.ProxyWebSocket(w, req, "http://backend.local:8080")

		// Should fail upgrade due to origin rejection
		if err == nil {
			t.Error("Expected error when origin is not allowed")
		}
	})
}

func TestWebSocketUpgraderConfiguration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	proxy := NewWebSocketProxyWithOrigins(logger, []string{"http://example.com"})

	t.Run("upgrader_read_buffer_size", func(t *testing.T) {
		if proxy.upgrader.ReadBufferSize != 4096 {
			t.Errorf("Expected read buffer size 4096, got %d", proxy.upgrader.ReadBufferSize)
		}
	})

	t.Run("upgrader_write_buffer_size", func(t *testing.T) {
		if proxy.upgrader.WriteBufferSize != 4096 {
			t.Errorf("Expected write buffer size 4096, got %d", proxy.upgrader.WriteBufferSize)
		}
	})

	t.Run("upgrader_check_origin_set", func(t *testing.T) {
		if proxy.upgrader.CheckOrigin == nil {
			t.Error("CheckOrigin function should be set")
		}
	})
}

func TestIsWebSocketUpgradeTableDriven(t *testing.T) {
	tests := []struct {
		name        string
		setupReq    func(*http.Request)
		expected    bool
		description string
	}{
		{
			name: "valid_request",
			setupReq: func(req *http.Request) {
				req.Header.Set("Upgrade", "websocket")
				req.Header.Set("Connection", "Upgrade")
			},
			expected:    true,
			description: "Valid WebSocket upgrade",
		},
		{
			name: "http_upgrade",
			setupReq: func(req *http.Request) {
				req.Header.Set("Upgrade", "h2c")
				req.Header.Set("Connection", "Upgrade")
			},
			expected:    false,
			description: "HTTP/2 upgrade not WebSocket",
		},
		{
			name: "lowercase_websocket",
			setupReq: func(req *http.Request) {
				req.Header.Set("Upgrade", "websocket")
				req.Header.Set("Connection", "upgrade")
			},
			expected:    true,
			description: "Lowercase connection header",
		},
		{
			name: "multiple_connection_values",
			setupReq: func(req *http.Request) {
				req.Header.Set("Upgrade", "websocket")
				req.Header.Set("Connection", "keep-alive, upgrade")
			},
			expected:    true,
			description: "Multiple connection values including upgrade",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			tt.setupReq(req)

			result := IsWebSocketUpgrade(req)

			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestOriginValidationTableDriven(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name           string
		allowedOrigins []string
		origin         string
		expected       bool
		description    string
	}{
		{
			name:           "permissive_empty_list",
			allowedOrigins: []string{},
			origin:         "http://any-domain.com",
			expected:       true,
			description:    "Empty allowed list allows all (for dev)",
		},
		{
			name:           "multiple_exact_matches",
			allowedOrigins: []string{"http://app1.com", "http://app2.com", "http://app3.com"},
			origin:         "http://app2.com",
			expected:       true,
			description:    "Exact match in multiple allowed origins",
		},
		{
			name:           "mixed_patterns",
			allowedOrigins: []string{"http://exact.com", "*.example.com", "*"},
			origin:         "http://sub.example.com",
			expected:       true,
			description:    "Match wildcard pattern in mixed list",
		},
		{
			name:           "case_sensitive_match",
			allowedOrigins: []string{"http://Example.COM"},
			origin:         "http://example.com",
			expected:       false,
			description:    "Origin matching is case sensitive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			proxy := NewWebSocketProxyWithOrigins(logger, tt.allowedOrigins)

			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			result := proxy.checkOrigin(req)

			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHeaderCopyingTableDriven(t *testing.T) {
	tests := []struct {
		name        string
		headerName  string
		headerValue string
		shouldCopy  bool
		description string
	}{
		{
			name:        "websocket_protocol",
			headerName:  "Sec-WebSocket-Protocol",
			headerValue: "chat",
			shouldCopy:  true,
			description: "WebSocket protocol header",
		},
		{
			name:        "websocket_extensions",
			headerName:  "Sec-WebSocket-Extensions",
			headerValue: "permessage-deflate",
			shouldCopy:  true,
			description: "WebSocket extensions header",
		},
		{
			name:        "custom_x_header",
			headerName:  "X-Custom-Value",
			headerValue: "custom",
			shouldCopy:  true,
			description: "Custom X-* header",
		},
		{
			name:        "lowercase_x_header",
			headerName:  "x-lowercase-header",
			headerValue: "value",
			shouldCopy:  true,
			description: "Lowercase x-* header",
		},
		{
			name:        "authorization_not_copied",
			headerName:  "Authorization",
			headerValue: "Bearer token",
			shouldCopy:  false,
			description: "Authorization header should not be copied",
		},
		{
			name:        "content_type_not_copied",
			headerName:  "Content-Type",
			headerValue: "application/json",
			shouldCopy:  false,
			description: "Content-Type header should not be copied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			src := make(http.Header)
			src.Set(tt.headerName, tt.headerValue)

			dst := make(http.Header)
			copyWebSocketHeaders(src, dst)

			hasCopied := dst.Get(tt.headerName) != ""

			if hasCopied != tt.shouldCopy {
				if tt.shouldCopy {
					t.Errorf("Expected header %s to be copied", tt.headerName)
				} else {
					t.Errorf("Expected header %s not to be copied", tt.headerName)
				}
			}
		})
	}
}

func TestBuildBackendURLTableDriven(t *testing.T) {
	tests := []struct {
		name        string
		backendURL  string
		path        string
		query       string
		expected    string
		description string
	}{
		{
			name:        "http_simple",
			backendURL:  "http://backend.local:8080",
			path:        "/ws",
			query:       "",
			expected:    "ws://backend.local:8080/ws",
			description: "Simple HTTP to WS",
		},
		{
			name:        "https_simple",
			backendURL:  "https://backend.local:8443",
			path:        "/ws",
			query:       "",
			expected:    "wss://backend.local:8443/ws",
			description: "Simple HTTPS to WSS",
		},
		{
			name:        "with_query",
			backendURL:  "http://backend.local",
			path:        "/chat",
			query:       "?room=lobby&token=abc123",
			expected:    "ws://backend.local/chat?room=lobby&token=abc123",
			description: "Preserve query string",
		},
		{
			name:        "root_path",
			backendURL:  "http://backend.local",
			path:        "/",
			query:       "",
			expected:    "ws://backend.local/",
			description: "Root path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			fullPath := tt.path + tt.query
			req := httptest.NewRequest("GET", fullPath, nil)

			result := buildBackendWebSocketURL(tt.backendURL, req)

			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestWebSocketOriginPatterns(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("subdomain_variance", func(t *testing.T) {
		proxy := NewWebSocketProxyWithOrigins(logger, []string{"*.app.example.com"})

		tests := []struct {
			origin   string
			expected bool
		}{
			{"http://api.app.example.com", true},
			{"http://web.app.example.com", true},
			{"http://app.example.com", false},
			{"http://example.com", false},
			{"http://other.com", false},
		}

		for _, tt := range tests {
			req := httptest.NewRequest("GET", "/ws", nil)
			req.Header.Set("Origin", tt.origin)

			result := proxy.checkOrigin(req)

			if result != tt.expected {
				t.Errorf("Origin %s: expected %v, got %v", tt.origin, tt.expected, result)
			}
		}
	})
}

func TestMatchWildcardOriginTableDriven(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		origin      string
		expected    bool
		description string
	}{
		{
			name:        "exact_subdomain",
			pattern:     "*.example.com",
			origin:      "http://app.example.com",
			expected:    true,
			description: "Exact subdomain match",
		},
		{
			name:        "deep_subdomain",
			pattern:     "*.example.com",
			origin:      "http://api.service.example.com",
			expected:    true,
			description: "Deep subdomain match (suffix match)",
		},
		{
			name:        "no_subdomain_match",
			pattern:     "*.example.com",
			origin:      "http://example.com",
			expected:    false,
			description: "Base domain should not match",
		},
		{
			name:        "different_domain",
			pattern:     "*.example.com",
			origin:      "http://app.other.com",
			expected:    false,
			description: "Different domain",
		},
		{
			name:        "no_wildcard_pattern",
			pattern:     "example.com",
			origin:      "http://example.com",
			expected:    false,
			description: "Non-wildcard pattern should not match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			result := matchWildcardOrigin(tt.pattern, tt.origin)

			if result != tt.expected {
				t.Errorf("Pattern %s vs %s: expected %v, got %v",
					tt.pattern, tt.origin, tt.expected, result)
			}
		})
	}
}

func TestWebSocketProxyLogging(t *testing.T) {
	logger := zaptest.NewLogger(t)
	proxy := NewWebSocketProxyWithOrigins(logger, []string{"http://example.com"})

	t.Run("logs_on_creation", func(t *testing.T) {
		if proxy.logger == nil {
			t.Error("Logger should be available for logging")
		}
	})

	t.Run("logs_on_origin_validation", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ws", nil)
		req.Header.Set("Origin", "http://rejected.com")

		// Should not panic during logging
		_ = proxy.checkOrigin(req)
	})
}

func TestWebSocketProtocolCompliance(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("valid_upgrade_headers", func(t *testing.T) {
		validHeaders := map[string]string{
			"Upgrade":                "websocket",
			"Connection":             "Upgrade",
			"Sec-WebSocket-Key":      "dGhlIHNhbXBsZSBub25jZQ==",
			"Sec-WebSocket-Version":  "13",
			"Sec-WebSocket-Protocol": "chat",
		}

		req := httptest.NewRequest("GET", "/ws", nil)
		for k, v := range validHeaders {
			req.Header.Set(k, v)
		}

		if !IsWebSocketUpgrade(req) {
			t.Error("Valid WebSocket upgrade request should be recognized")
		}
	})

	t.Run("case_insensitive_upgrade", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ws", nil)
		req.Header.Set("Upgrade", "WEBSOCKET")
		req.Header.Set("Connection", "upgrade")

		if !IsWebSocketUpgrade(req) {
			t.Error("WebSocket upgrade should be case-insensitive")
		}
	})
}
