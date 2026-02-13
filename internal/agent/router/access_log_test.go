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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestAccessLog_Disabled(t *testing.T) {
	logger := zap.NewNop()
	middleware, _ := NewAccessLogMiddleware(nil, logger)

	if middleware.IsEnabled() {
		t.Error("Expected middleware to be disabled when config is nil")
	}
}

func TestAccessLog_CLFFormat(t *testing.T) {
	logger := zap.NewNop()

	// Capture output
	var buf bytes.Buffer
	config := &pb.AccessLogConfig{
		Enabled:    true,
		Format:     "clf",
		Output:     "stdout",
		SampleRate: 1.0,
	}

	middleware, err := NewAccessLogMiddleware(config, logger)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}
	middleware.writer = &buf

	if !middleware.IsEnabled() {
		t.Fatal("Expected middleware to be enabled")
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	})

	wrapped := middleware.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	req.Header.Set("User-Agent", "TestClient/1.0")
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	output := buf.String()
	if !strings.Contains(output, "192.168.1.100") {
		t.Errorf("CLF log should contain client IP, got %q", output)
	}
	if !strings.Contains(output, "GET /api/v1/users") {
		t.Errorf("CLF log should contain method and URI, got %q", output)
	}
	if !strings.Contains(output, "200") {
		t.Errorf("CLF log should contain status code, got %q", output)
	}
	if !strings.Contains(output, "TestClient/1.0") {
		t.Errorf("CLF log should contain user agent, got %q", output)
	}
}

func TestAccessLog_JSONFormat(t *testing.T) {
	logger := zap.NewNop()

	var buf bytes.Buffer
	config := &pb.AccessLogConfig{
		Enabled:    true,
		Format:     "json",
		Output:     "stdout",
		SampleRate: 1.0,
	}

	middleware, err := NewAccessLogMiddleware(config, logger)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}
	middleware.writer = &buf

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})

	wrapped := middleware.Wrap(handler)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/resources", nil)
	req.Header.Set("X-Request-ID", "req-456")
	req.Header.Set("User-Agent", "TestAgent")
	req.RemoteAddr = "10.0.0.1:8080"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	output := buf.String()

	var entry AccessLogEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &entry); err != nil {
		t.Fatalf("Failed to parse JSON access log: %v\nOutput: %q", err, output)
	}

	if entry.ClientIP != "10.0.0.1" {
		t.Errorf("Expected client IP 10.0.0.1, got %q", entry.ClientIP)
	}
	if entry.Method != "POST" {
		t.Errorf("Expected method POST, got %q", entry.Method)
	}
	if entry.StatusCode != 201 {
		t.Errorf("Expected status 201, got %d", entry.StatusCode)
	}
	if entry.RequestID != "req-456" {
		t.Errorf("Expected request ID req-456, got %q", entry.RequestID)
	}
	if entry.UserAgent != "TestAgent" {
		t.Errorf("Expected user agent TestAgent, got %q", entry.UserAgent)
	}
	if entry.Duration <= 0 {
		t.Error("Expected positive duration")
	}
}

func TestAccessLog_CustomTemplate(t *testing.T) {
	logger := zap.NewNop()

	var buf bytes.Buffer
	config := &pb.AccessLogConfig{
		Enabled:    true,
		Format:     "custom",
		Template:   "{{.Method}} {{.URI}} {{.StatusCode}} {{.RequestID}}",
		Output:     "stdout",
		SampleRate: 1.0,
	}

	middleware, err := NewAccessLogMiddleware(config, logger)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}
	middleware.writer = &buf

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-ID", "custom-id")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	output := strings.TrimSpace(buf.String())
	expected := "GET /test 200 custom-id"
	if output != expected {
		t.Errorf("Expected custom template output %q, got %q", expected, output)
	}
}

func TestAccessLog_FileOutput(t *testing.T) {
	logger := zap.NewNop()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	config := &pb.AccessLogConfig{
		Enabled:    true,
		Format:     "clf",
		Output:     "file",
		FilePath:   logPath,
		MaxSize:    "10Mi",
		MaxBackups: 3,
		SampleRate: 1.0,
	}

	middleware, err := NewAccessLogMiddleware(config, logger)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}
	defer middleware.Close()

	if !middleware.IsEnabled() {
		t.Fatal("Expected middleware to be enabled")
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	// Read the log file
	data, err := os.ReadFile(filepath.Clean(logPath))
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "127.0.0.1") {
		t.Errorf("Log file should contain client IP, got %q", content)
	}
	if !strings.Contains(content, "GET /test") {
		t.Errorf("Log file should contain request details, got %q", content)
	}
}

func TestAccessLog_Sampling(t *testing.T) {
	logger := zap.NewNop()

	var buf bytes.Buffer
	config := &pb.AccessLogConfig{
		Enabled:    true,
		Format:     "clf",
		Output:     "stdout",
		SampleRate: 0.0, // Sample nothing
	}

	middleware, err := NewAccessLogMiddleware(config, logger)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}
	// Since sampleRate <= 0 defaults to 1.0, let's test by setting it directly
	middleware.sampleRate = 0.0

	middleware.writer = &buf

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Wrap(handler)

	// Send 100 requests
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
	}

	// With 0% sample rate, nothing should be logged
	if buf.Len() > 0 {
		t.Errorf("Expected no log entries with 0%% sample rate, got %d bytes", buf.Len())
	}
}

func TestAccessLog_StatusCodeFilter(t *testing.T) {
	logger := zap.NewNop()

	var buf bytes.Buffer
	config := &pb.AccessLogConfig{
		Enabled:           true,
		Format:            "json",
		Output:            "stdout",
		FilterStatusCodes: []int32{500, 503},
		SampleRate:        1.0,
	}

	middleware, err := NewAccessLogMiddleware(config, logger)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}
	middleware.writer = &buf

	tests := []struct {
		statusCode int
		shouldLog  bool
	}{
		{200, false},
		{404, false},
		{500, true},
		{503, true},
	}

	for _, tt := range tests {
		buf.Reset()

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tt.statusCode)
		})

		wrapped := middleware.Wrap(handler)
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		hasOutput := buf.Len() > 0
		if hasOutput != tt.shouldLog {
			t.Errorf("Status %d: expected logged=%v, got logged=%v", tt.statusCode, tt.shouldLog, hasOutput)
		}
	}
}

func TestAccessLog_ExtractClientIP(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expected   string
	}{
		{
			name:       "X-Forwarded-For single",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.50"},
			remoteAddr: "127.0.0.1:1234",
			expected:   "203.0.113.50",
		},
		{
			name:       "X-Forwarded-For chain",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.50, 70.41.3.18, 150.172.238.178"},
			remoteAddr: "127.0.0.1:1234",
			expected:   "203.0.113.50",
		},
		{
			name:       "X-Real-IP",
			headers:    map[string]string{"X-Real-IP": "10.0.0.5"},
			remoteAddr: "127.0.0.1:1234",
			expected:   "10.0.0.5",
		},
		{
			name:       "RemoteAddr fallback",
			headers:    map[string]string{},
			remoteAddr: "192.168.1.1:54321",
			expected:   "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			ip := extractClientIP(req)
			if ip != tt.expected {
				t.Errorf("Expected IP %q, got %q", tt.expected, ip)
			}
		})
	}
}

func TestParseByteSizeAccessLog(t *testing.T) {
	tests := []struct {
		input     string
		expected  int64
		expectErr bool
	}{
		{"100Mi", 100 * 1024 * 1024, false},
		{"1Gi", 1024 * 1024 * 1024, false},
		{"512Ki", 512 * 1024, false},
		{"1M", 1000 * 1000, false},
		{"1G", 1000 * 1000 * 1000, false},
		{"1024", 1024, false},
		{"", 0, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		result, err := parseByteSize(tt.input)
		if tt.expectErr {
			if err == nil {
				t.Errorf("parseByteSize(%q) expected error, got nil", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseByteSize(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if result != tt.expected {
			t.Errorf("parseByteSize(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}
