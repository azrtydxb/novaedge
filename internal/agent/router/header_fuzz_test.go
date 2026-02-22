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
	"net/http"
	"strings"
	"sync"
	"testing"
)

// FuzzParseHeader fuzzes header parsing to ensure no panics or crashes.
func FuzzParseHeader(f *testing.F) {
	// Add corpus seeds with valid and edge-case headers
	seeds := []string{
		"Content-Type: application/json\r\n",
		"X-Custom-Header: value\r\n",
		"Host: example.com\r\n",
		"User-Agent: test/1.0\r\n",
		"",               // Empty
		"\r\n",           // Just CRLF
		": value\r\n",    // Empty name
		"name: \r\n",     // Empty value
		"name:value\r\n", // No space after colon
		"Name: Value with spaces\r\n",
		"Name: Value\r\n with continuation\r\n",
		strings.Repeat("A", 10000) + ": value\r\n",     // Long name
		"name: " + strings.Repeat("B", 10000) + "\r\n", // Long value
		"\x00\x01\x02: value\r\n",                      // Binary in name
		"name: \x00\x01\x02\r\n",                       // Binary in value
		"Unicode: 你好世界\r\n",                            // Unicode
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Parse the header - should not panic
		reader := bufio.NewReader(strings.NewReader(input))
		_, err := http.ReadRequest(reader)
		if err != nil {
			// Error is acceptable, panic is not
			return
		}
	})
}

// FuzzParsePath fuzzes path parsing and normalization.
func FuzzParsePath(f *testing.F) {
	seeds := []string{
		"/",
		"/path",
		"/path/to/resource",
		"/path?a=b",
		"/path#fragment",
		"/path/with%20space",
		"/path/../traversal",
		"/path/./current",
		"//double/slash",
		"/path/with/unicode/你好",
		"",                         // Empty
		"relative/path",            // Relative
		"\\windows\\path",          // Windows-style
		"/path\x00null",            // Null byte
		strings.Repeat("/a", 1000), // Long path
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, path string) {
		// NormalizePath should not panic on any input
		normalized := NormalizePath(path)
		_ = normalized // Just ensure no panic
	})
}

// NormalizePath normalizes a URL path.
// This is a simple implementation for fuzzing purposes.
func NormalizePath(path string) string {
	if path == "" {
		return "/"
	}

	// Ensure leading slash
	if path[0] != '/' {
		path = "/" + path
	}

	// Remove null bytes
	path = strings.ReplaceAll(path, "\x00", "")

	// Basic path traversal prevention
	for strings.Contains(path, "..") {
		path = strings.Replace(path, "..", "", 1)
	}

	return path
}

// FuzzParseURL fuzzes URL parsing.
func FuzzParseURL(f *testing.F) {
	seeds := []string{
		"http://example.com",
		"https://example.com/path",
		"http://example.com:8080",
		"http://user:pass@example.com",
		"http://example.com?a=b&c=d",
		"http://example.com#fragment",
		"http://192.168.1.1",
		"http://[::1]",
		"http://example.com/path/with%20space",
		"", // Empty
		"http://",
		"http://example.com:invalid",
		"http://\x00null",
		"http://example.com/" + strings.Repeat("a", 1000),
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, urlStr string) {
		// URL parsing should not panic
		req, err := http.NewRequest(http.MethodGet, urlStr, nil)
		if err != nil {
			// Error is acceptable
			return
		}
		if req != nil {
			_ = req.URL.String()
		}
	})
}

// FuzzMatchHost fuzzes host matching patterns.
func FuzzMatchHost(f *testing.F) {
	seeds := []struct {
		pattern string
		host    string
	}{
		{"example.com", "example.com"},
		{"*.example.com", "www.example.com"},
		{"*.example.com", "api.example.com"},
		{"example.com", "other.com"},
		{"", ""},
		{"*", "anything"},
		{"*.com", "example.com"},
		{"www.*.com", "www.test.com"},
		{strings.Repeat("a", 100) + ".com", "test.com"},
	}

	for _, seed := range seeds {
		f.Add(seed.pattern, seed.host)
	}

	f.Fuzz(func(t *testing.T, pattern, host string) {
		// MatchHost should not panic on any input
		matched := MatchHost(pattern, host)
		_ = matched
	})
}

// MatchHost matches a host against a pattern.
// This is a simple implementation for fuzzing purposes.
func MatchHost(pattern, host string) bool {
	if pattern == "" {
		return host == ""
	}
	if pattern == "*" {
		return true
	}

	// Handle wildcard patterns
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // Keep the dot
		if strings.HasSuffix(host, suffix) {
			return true
		}
		// Also match exact domain (e.g., *.example.com matches example.com)
		if host == pattern[2:] {
			return true
		}
		return false
	}

	return pattern == host
}

// bufioReaderPool is a pool for bufio.Reader objects.
var bufioReaderPool = sync.Pool{
	New: func() interface{} {
		return bufio.NewReader(nil)
	},
}
