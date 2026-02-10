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

package errors

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// --- NetworkError Tests ---

func TestNewNetworkError(t *testing.T) {
	err := NewNetworkError("connection timeout")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Message != "connection timeout" {
		t.Errorf("expected message 'connection timeout', got %q", err.Message)
	}
	if err.Fields == nil {
		t.Error("expected non-nil Fields map")
	}
}

func TestNetworkError_Error_MessageOnly(t *testing.T) {
	err := NewNetworkError("connection timeout")
	if err.Error() != "connection timeout" {
		t.Errorf("expected 'connection timeout', got %q", err.Error())
	}
}

func TestNetworkError_Error_WithOpAndHost(t *testing.T) {
	err := &NetworkError{
		Op:      "dial",
		Host:    "backend.example.com",
		Port:    8080,
		Message: "connection refused",
	}
	result := err.Error()
	if !strings.Contains(result, "dial") {
		t.Errorf("expected 'dial' in error, got %q", result)
	}
	if !strings.Contains(result, "backend.example.com:8080") {
		t.Errorf("expected 'backend.example.com:8080' in error, got %q", result)
	}
	if !strings.Contains(result, "connection refused") {
		t.Errorf("expected 'connection refused' in error, got %q", result)
	}
}

func TestNetworkError_Error_HostWithoutPort(t *testing.T) {
	err := &NetworkError{
		Host:    "backend.example.com",
		Message: "DNS failure",
	}
	result := err.Error()
	if strings.Contains(result, ":0") {
		t.Errorf("should not include port 0 in error: %q", result)
	}
}

func TestNetworkError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("inner error")
	err := &NetworkError{
		Message: "outer",
		Err:     inner,
	}

	if !errors.Is(err, inner) {
		t.Error("expected Unwrap to return inner error")
	}
}

func TestNetworkError_WithField(t *testing.T) {
	err := NewNetworkError("timeout").
		WithField("host", "10.0.0.1").
		WithField("port", 8080)

	if err.Fields["host"] != "10.0.0.1" {
		t.Errorf("expected field 'host' to be '10.0.0.1', got %v", err.Fields["host"])
	}
	if err.Fields["port"] != 8080 {
		t.Errorf("expected field 'port' to be 8080, got %v", err.Fields["port"])
	}
}

func TestNetworkError_WithFields(t *testing.T) {
	err := NewNetworkError("timeout").
		WithFields(map[string]interface{}{
			"host":    "10.0.0.1",
			"port":    8080,
			"attempt": 3,
		})

	if len(err.Fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(err.Fields))
	}
}

// --- ConfigError Tests ---

func TestNewConfigError(t *testing.T) {
	err := NewConfigError("invalid configuration")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Message != "invalid configuration" {
		t.Errorf("expected message 'invalid configuration', got %q", err.Message)
	}
}

func TestConfigError_Error_WithFieldAndValue(t *testing.T) {
	err := &ConfigError{
		Field:   "maxRetries",
		Value:   -1,
		Message: "must be positive",
	}
	result := err.Error()
	if !strings.Contains(result, "maxRetries") {
		t.Errorf("expected field name in error: %q", result)
	}
	if !strings.Contains(result, "must be positive") {
		t.Errorf("expected message in error: %q", result)
	}
	if !strings.Contains(result, "-1") {
		t.Errorf("expected value in error: %q", result)
	}
}

func TestConfigError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("parse error")
	err := &ConfigError{
		Message: "config",
		Err:     inner,
	}

	if !errors.Is(err, inner) {
		t.Error("expected Unwrap to return inner error")
	}
}

func TestConfigError_WithField(t *testing.T) {
	err := NewConfigError("bad config").
		WithField("file", "config.yaml")

	if err.Fields["file"] != "config.yaml" {
		t.Errorf("expected field 'file' to be 'config.yaml', got %v", err.Fields["file"])
	}
}

func TestConfigError_WithFields(t *testing.T) {
	err := NewConfigError("bad config").
		WithFields(map[string]interface{}{
			"file": "config.yaml",
			"line": 42,
		})

	if len(err.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(err.Fields))
	}
}

// --- ValidationError Tests ---

func TestNewValidationError(t *testing.T) {
	err := NewValidationError("validation failed")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Message != "validation failed" {
		t.Errorf("expected message 'validation failed', got %q", err.Message)
	}
	if err.Children == nil {
		t.Error("expected non-nil Children slice")
	}
}

func TestValidationError_Error_WithFieldAndRule(t *testing.T) {
	err := &ValidationError{
		Field:   "email",
		Rule:    "required",
		Message: "field is required",
	}
	result := err.Error()
	if !strings.Contains(result, "email") {
		t.Errorf("expected field name in error: %q", result)
	}
	if !strings.Contains(result, "required") {
		t.Errorf("expected rule in error: %q", result)
	}
}

func TestValidationError_AddChild(t *testing.T) {
	parent := NewValidationError("parent error")
	child1 := NewValidationError("child 1")
	child2 := NewValidationError("child 2")

	parent.AddChild(child1).AddChild(child2)

	if len(parent.Children) != 2 {
		t.Errorf("expected 2 children, got %d", len(parent.Children))
	}

	// Error message should include children
	result := parent.Error()
	if !strings.Contains(result, "child 1") {
		t.Errorf("expected child error in parent message: %q", result)
	}
	if !strings.Contains(result, "child 2") {
		t.Errorf("expected child error in parent message: %q", result)
	}
}

func TestValidationError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("format error")
	err := &ValidationError{
		Message: "validation",
		Err:     inner,
	}

	if !errors.Is(err, inner) {
		t.Error("expected Unwrap to return inner error")
	}
}

func TestValidationError_WithField(t *testing.T) {
	err := NewValidationError("bad input").
		WithField("field", "username")

	if err.Fields["field"] != "username" {
		t.Errorf("expected field 'field' to be 'username', got %v", err.Fields["field"])
	}
}

func TestValidationError_WithFields(t *testing.T) {
	err := NewValidationError("bad input").
		WithFields(map[string]interface{}{
			"field": "username",
			"rule":  "min_length",
		})

	if len(err.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(err.Fields))
	}
}

// --- TLSError Tests ---

func TestNewTLSError(t *testing.T) {
	err := NewTLSError("handshake failed")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Message != "handshake failed" {
		t.Errorf("expected message 'handshake failed', got %q", err.Message)
	}
}

func TestTLSError_Error_WithOpAndHost(t *testing.T) {
	err := &TLSError{
		Op:      "handshake",
		Host:    "secure.example.com",
		Message: "certificate expired",
	}
	result := err.Error()
	if !strings.Contains(result, "handshake") {
		t.Errorf("expected 'handshake' in error: %q", result)
	}
	if !strings.Contains(result, "secure.example.com") {
		t.Errorf("expected host in error: %q", result)
	}
	if !strings.Contains(result, "certificate expired") {
		t.Errorf("expected message in error: %q", result)
	}
}

func TestTLSError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("x509: certificate signed by unknown authority")
	err := &TLSError{
		Message: "TLS error",
		Err:     inner,
	}

	if !errors.Is(err, inner) {
		t.Error("expected Unwrap to return inner error")
	}
}

func TestTLSError_WithField(t *testing.T) {
	err := NewTLSError("bad cert").
		WithField("sni", "example.com")

	if err.Fields["sni"] != "example.com" {
		t.Errorf("expected field 'sni' to be 'example.com', got %v", err.Fields["sni"])
	}
}

func TestTLSError_WithFields(t *testing.T) {
	err := NewTLSError("bad cert").
		WithFields(map[string]interface{}{
			"sni":    "example.com",
			"cipher": "TLS_AES_128_GCM_SHA256",
		})

	if len(err.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(err.Fields))
	}
}

// --- Standard Error Variables Tests ---

func TestStandardErrorVariables(t *testing.T) {
	// Verify all standard errors are properly defined
	standardErrors := []struct {
		name string
		err  error
	}{
		{"ErrInvalidConfig", ErrInvalidConfig},
		{"ErrMissingConfig", ErrMissingConfig},
		{"ErrConfigParse", ErrConfigParse},
		{"ErrConfigValidation", ErrConfigValidation},
		{"ErrConnectionFailed", ErrConnectionFailed},
		{"ErrConnectionTimeout", ErrConnectionTimeout},
		{"ErrConnectionRefused", ErrConnectionRefused},
		{"ErrDNSResolution", ErrDNSResolution},
		{"ErrNetworkUnreachable", ErrNetworkUnreachable},
		{"ErrTLSHandshake", ErrTLSHandshake},
		{"ErrTLSCertificate", ErrTLSCertificate},
		{"ErrTLSVerification", ErrTLSVerification},
		{"ErrInvalidCipherSuite", ErrInvalidCipherSuite},
		{"ErrValidationFailed", ErrValidationFailed},
		{"ErrInvalidInput", ErrInvalidInput},
		{"ErrInvalidFormat", ErrInvalidFormat},
		{"ErrMissingField", ErrMissingField},
		{"ErrNotFound", ErrNotFound},
		{"ErrAlreadyExists", ErrAlreadyExists},
		{"ErrTimeout", ErrTimeout},
		{"ErrCancelled", ErrCancelled},
	}

	for _, tt := range standardErrors {
		if tt.err == nil {
			t.Errorf("%s should not be nil", tt.name)
		}
		if tt.err.Error() == "" {
			t.Errorf("%s should have a non-empty message", tt.name)
		}
	}
}

func TestErrorTypeAssertion(t *testing.T) {
	// Test that errors.As works with our custom types
	var netErr *NetworkError
	err := &NetworkError{Message: "connection failed"}
	if !errors.As(err, &netErr) {
		t.Error("errors.As should work with NetworkError")
	}

	var cfgErr *ConfigError
	err2 := &ConfigError{Message: "bad config"}
	if !errors.As(err2, &cfgErr) {
		t.Error("errors.As should work with ConfigError")
	}

	var valErr *ValidationError
	err3 := &ValidationError{Message: "invalid"}
	if !errors.As(err3, &valErr) {
		t.Error("errors.As should work with ValidationError")
	}

	var tlsErr *TLSError
	err4 := &TLSError{Message: "TLS failure"}
	if !errors.As(err4, &tlsErr) {
		t.Error("errors.As should work with TLSError")
	}
}

func TestWrappedErrorChain(t *testing.T) {
	// Test error wrapping chain
	base := ErrConnectionTimeout
	wrapped := fmt.Errorf("failed to connect: %w", base)
	netErr := &NetworkError{
		Message: "upstream failed",
		Err:     wrapped,
	}

	// Should be able to unwrap to find the base error
	if !errors.Is(netErr, base) {
		t.Error("should be able to find base error through chain")
	}
}
