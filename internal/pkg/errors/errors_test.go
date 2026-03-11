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

package apperrors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStandardErrors(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		msg   string
		isErr error
	}{
		{"ErrInvalidConfig", ErrInvalidConfig, "invalid configuration", ErrInvalidConfig},
		{"ErrMissingConfig", ErrMissingConfig, "missing required configuration", ErrMissingConfig},
		{"ErrConfigParse", ErrConfigParse, "failed to parse configuration", ErrConfigParse},
		{"ErrConfigValidation", ErrConfigValidation, "configuration validation failed", ErrConfigValidation},
		{"ErrConnectionFailed", ErrConnectionFailed, "connection failed", ErrConnectionFailed},
		{"ErrConnectionTimeout", ErrConnectionTimeout, "connection timeout", ErrConnectionTimeout},
		{"ErrConnectionRefused", ErrConnectionRefused, "connection refused", ErrConnectionRefused},
		{"ErrDNSResolution", ErrDNSResolution, "DNS resolution failed", ErrDNSResolution},
		{"ErrNetworkUnreachable", ErrNetworkUnreachable, "network unreachable", ErrNetworkUnreachable},
		{"ErrTLSHandshake", ErrTLSHandshake, "TLS handshake failed", ErrTLSHandshake},
		{"ErrTLSCertificate", ErrTLSCertificate, "TLS certificate error", ErrTLSCertificate},
		{"ErrTLSVerification", ErrTLSVerification, "TLS verification failed", ErrTLSVerification},
		{"ErrInvalidCipherSuite", ErrInvalidCipherSuite, "invalid cipher suite", ErrInvalidCipherSuite},
		{"ErrValidationFailed", ErrValidationFailed, "validation failed", ErrValidationFailed},
		{"ErrInvalidInput", ErrInvalidInput, "invalid input", ErrInvalidInput},
		{"ErrInvalidFormat", ErrInvalidFormat, "invalid format", ErrInvalidFormat},
		{"ErrMissingField", ErrMissingField, "missing required field", ErrMissingField},
		{"ErrNotFound", ErrNotFound, "resource not found", ErrNotFound},
		{"ErrAlreadyExists", ErrAlreadyExists, "resource already exists", ErrAlreadyExists},
		{"ErrTimeout", ErrTimeout, "operation timeout", ErrTimeout},
		{"ErrCancelled", ErrCancelled, "operation cancelled", ErrCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.msg, tt.err.Error())
			assert.True(t, errors.Is(tt.err, tt.isErr))
		})
	}
}

func TestNetworkError(t *testing.T) {
	t.Run("Error with all fields", func(t *testing.T) {
		err := &NetworkError{
			Op:      "dial",
			Host:    "example.com",
			Port:    8080,
			Message: "connection refused",
			Err:     errors.New("underlying error"),
		}
		expected := "dial: example.com:8080: connection refused: underlying error"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("Error with no port", func(t *testing.T) {
		err := &NetworkError{
			Op:      "connect",
			Host:    "example.com",
			Message: "timeout",
		}
		expected := "connect: example.com: timeout"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("Error with minimal fields", func(t *testing.T) {
		err := &NetworkError{
			Message: "network error",
		}
		assert.Equal(t, "network error", err.Error())
	})

	t.Run("Error with port but no host", func(t *testing.T) {
		err := &NetworkError{
			Port:    8080,
			Message: "connection failed",
		}
		assert.Equal(t, "connection failed", err.Error())
	})

	t.Run("Unwrap returns underlying error", func(t *testing.T) {
		underlying := errors.New("underlying")
		err := &NetworkError{
			Message: "network error",
			Err:     underlying,
		}
		assert.Equal(t, underlying, err.Unwrap())
	})

	t.Run("WithField adds field", func(t *testing.T) {
		err := NewNetworkError("test")
		result := err.WithField("key", "value")
		assert.Equal(t, err, result)
		assert.Equal(t, "value", err.Fields["key"])
	})

	t.Run("WithFields adds multiple fields", func(t *testing.T) {
		err := NewNetworkError("test")
		fields := map[string]any{
			"key1": "value1",
			"key2": 42,
		}
		result := err.WithFields(fields)
		assert.Equal(t, err, result)
		assert.Equal(t, "value1", err.Fields["key1"])
		assert.Equal(t, 42, err.Fields["key2"])
	})

	t.Run("WithField on nil Fields map", func(t *testing.T) {
		err := &NetworkError{}
		result := err.WithField("key", "value")
		assert.Equal(t, err, result)
		assert.Equal(t, "value", err.Fields["key"])
	})

	t.Run("WithFields on nil Fields map", func(t *testing.T) {
		err := &NetworkError{}
		result := err.WithFields(map[string]any{"key": "value"})
		assert.Equal(t, err, result)
		assert.Equal(t, "value", err.Fields["key"])
	})

	t.Run("NewNetworkError creates error with initialized Fields", func(t *testing.T) {
		err := NewNetworkError("test error")
		assert.Equal(t, "test error", err.Message)
		assert.NotNil(t, err.Fields)
	})

	t.Run("errors.As works with NetworkError", func(t *testing.T) {
		underlying := &NetworkError{
			Op:      "dial",
			Message: "connection failed",
		}
		wrapped := fmt.Errorf("wrapped: %w", underlying)

		var netErr *NetworkError
		assert.True(t, errors.As(wrapped, &netErr))
		assert.Equal(t, "dial", netErr.Op)
	})
}

func TestConfigError(t *testing.T) {
	t.Run("Error with all fields", func(t *testing.T) {
		err := &ConfigError{
			Field:   "timeout",
			Value:   "invalid",
			Message: "must be a duration",
			Err:     errors.New("parse error"),
		}
		expected := "field 'timeout': must be a duration: value: invalid: parse error"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("Error with minimal fields", func(t *testing.T) {
		err := &ConfigError{
			Message: "config error",
		}
		assert.Equal(t, "config error", err.Error())
	})

	t.Run("Error with field only", func(t *testing.T) {
		err := &ConfigError{
			Field: "server",
		}
		assert.Equal(t, "field 'server'", err.Error())
	})

	t.Run("Error with field and message", func(t *testing.T) {
		err := &ConfigError{
			Field:   "port",
			Message: "out of range",
		}
		assert.Equal(t, "field 'port': out of range", err.Error())
	})

	t.Run("Unwrap returns underlying error", func(t *testing.T) {
		underlying := errors.New("underlying")
		err := &ConfigError{
			Message: "config error",
			Err:     underlying,
		}
		assert.Equal(t, underlying, err.Unwrap())
	})

	t.Run("WithField adds field", func(t *testing.T) {
		err := NewConfigError("test")
		result := err.WithField("key", "value")
		assert.Equal(t, err, result)
		assert.Equal(t, "value", err.Fields["key"])
	})

	t.Run("WithFields adds multiple fields", func(t *testing.T) {
		err := NewConfigError("test")
		fields := map[string]any{
			"key1": "value1",
			"key2": 42,
		}
		result := err.WithFields(fields)
		assert.Equal(t, err, result)
		assert.Equal(t, "value1", err.Fields["key1"])
		assert.Equal(t, 42, err.Fields["key2"])
	})

	t.Run("WithField on nil Fields map", func(t *testing.T) {
		err := &ConfigError{}
		result := err.WithField("key", "value")
		assert.Equal(t, err, result)
		assert.Equal(t, "value", err.Fields["key"])
	})

	t.Run("NewConfigError creates error with initialized Fields", func(t *testing.T) {
		err := NewConfigError("test error")
		assert.Equal(t, "test error", err.Message)
		assert.NotNil(t, err.Fields)
	})

	t.Run("errors.As works with ConfigError", func(t *testing.T) {
		underlying := &ConfigError{
			Field:   "timeout",
			Message: "invalid",
		}
		wrapped := fmt.Errorf("wrapped: %w", underlying)

		var cfgErr *ConfigError
		assert.True(t, errors.As(wrapped, &cfgErr))
		assert.Equal(t, "timeout", cfgErr.Field)
	})
}

func TestValidationError(t *testing.T) {
	t.Run("Error with all fields", func(t *testing.T) {
		err := &ValidationError{
			Field:   "email",
			Rule:    "email_format",
			Value:   "invalid",
			Message: "must be valid email",
			Err:     errors.New("regex mismatch"),
		}
		expected := "field 'email': rule 'email_format': must be valid email: value: invalid: regex mismatch"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("Error with minimal fields", func(t *testing.T) {
		err := &ValidationError{
			Message: "validation error",
		}
		assert.Equal(t, "validation error", err.Error())
	})

	t.Run("Error with field and rule", func(t *testing.T) {
		err := &ValidationError{
			Field: "name",
			Rule:  "required",
		}
		assert.Equal(t, "field 'name': rule 'required'", err.Error())
	})

	t.Run("Error with children", func(t *testing.T) {
		err := &ValidationError{
			Field:   "address",
			Message: "invalid address",
			Children: []*ValidationError{
				{Field: "street", Message: "required"},
				{Field: "city", Message: "required"},
			},
		}
		errStr := err.Error()
		assert.Contains(t, errStr, "field 'address': invalid address")
		assert.Contains(t, errStr, "field 'street': required")
		assert.Contains(t, errStr, "field 'city': required")
	})

	t.Run("Unwrap returns underlying error", func(t *testing.T) {
		underlying := errors.New("underlying")
		err := &ValidationError{
			Message: "validation error",
			Err:     underlying,
		}
		assert.Equal(t, underlying, err.Unwrap())
	})

	t.Run("WithField adds field", func(t *testing.T) {
		err := NewValidationError("test")
		result := err.WithField("key", "value")
		assert.Equal(t, err, result)
		assert.Equal(t, "value", err.Fields["key"])
	})

	t.Run("WithFields adds multiple fields", func(t *testing.T) {
		err := NewValidationError("test")
		fields := map[string]any{
			"key1": "value1",
			"key2": 42,
		}
		result := err.WithFields(fields)
		assert.Equal(t, err, result)
		assert.Equal(t, "value1", err.Fields["key1"])
		assert.Equal(t, 42, err.Fields["key2"])
	})

	t.Run("WithField on nil Fields map", func(t *testing.T) {
		err := &ValidationError{}
		result := err.WithField("key", "value")
		assert.Equal(t, err, result)
		assert.Equal(t, "value", err.Fields["key"])
	})

	t.Run("AddChild adds child error", func(t *testing.T) {
		err := NewValidationError("parent")
		child := NewValidationError("child")
		result := err.AddChild(child)
		assert.Equal(t, err, result)
		assert.Len(t, err.Children, 1)
		assert.Equal(t, child, err.Children[0])
	})

	t.Run("NewValidationError creates error with initialized fields", func(t *testing.T) {
		err := NewValidationError("test error")
		assert.Equal(t, "test error", err.Message)
		assert.NotNil(t, err.Fields)
		assert.NotNil(t, err.Children)
	})

	t.Run("errors.As works with ValidationError", func(t *testing.T) {
		underlying := &ValidationError{
			Field:   "email",
			Message: "invalid",
		}
		wrapped := fmt.Errorf("wrapped: %w", underlying)

		var valErr *ValidationError
		assert.True(t, errors.As(wrapped, &valErr))
		assert.Equal(t, "email", valErr.Field)
	})
}

func TestTLSError(t *testing.T) {
	t.Run("Error with all fields", func(t *testing.T) {
		err := &TLSError{
			Op:      "handshake",
			Host:    "example.com",
			Message: "certificate verify failed",
			Err:     errors.New("x509: certificate signed by unknown authority"),
		}
		expected := "handshake: example.com: certificate verify failed: x509: certificate signed by unknown authority"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("Error with minimal fields", func(t *testing.T) {
		err := &TLSError{
			Message: "tls error",
		}
		assert.Equal(t, "tls error", err.Error())
	})

	t.Run("Error with op only", func(t *testing.T) {
		err := &TLSError{
			Op: "verify",
		}
		assert.Equal(t, "verify", err.Error())
	})

	t.Run("Error with op and host", func(t *testing.T) {
		err := &TLSError{
			Op:   "handshake",
			Host: "secure.example.com",
		}
		assert.Equal(t, "handshake: secure.example.com", err.Error())
	})

	t.Run("Unwrap returns underlying error", func(t *testing.T) {
		underlying := errors.New("underlying")
		err := &TLSError{
			Message: "tls error",
			Err:     underlying,
		}
		assert.Equal(t, underlying, err.Unwrap())
	})

	t.Run("WithField adds field", func(t *testing.T) {
		err := NewTLSError("test")
		result := err.WithField("key", "value")
		assert.Equal(t, err, result)
		assert.Equal(t, "value", err.Fields["key"])
	})

	t.Run("WithFields adds multiple fields", func(t *testing.T) {
		err := NewTLSError("test")
		fields := map[string]any{
			"key1": "value1",
			"key2": 42,
		}
		result := err.WithFields(fields)
		assert.Equal(t, err, result)
		assert.Equal(t, "value1", err.Fields["key1"])
		assert.Equal(t, 42, err.Fields["key2"])
	})

	t.Run("WithField on nil Fields map", func(t *testing.T) {
		err := &TLSError{}
		result := err.WithField("key", "value")
		assert.Equal(t, err, result)
		assert.Equal(t, "value", err.Fields["key"])
	})

	t.Run("NewTLSError creates error with initialized Fields", func(t *testing.T) {
		err := NewTLSError("test error")
		assert.Equal(t, "test error", err.Message)
		assert.NotNil(t, err.Fields)
	})

	t.Run("errors.As works with TLSError", func(t *testing.T) {
		underlying := &TLSError{
			Op:      "handshake",
			Message: "failed",
		}
		wrapped := fmt.Errorf("wrapped: %w", underlying)

		var tlsErr *TLSError
		assert.True(t, errors.As(wrapped, &tlsErr))
		assert.Equal(t, "handshake", tlsErr.Op)
	})
}

func TestErrorWrappingAndUnwrapping(t *testing.T) {
	t.Run("NetworkError supports errors.Is", func(t *testing.T) {
		underlying := ErrConnectionFailed
		err := &NetworkError{
			Message: "failed to connect",
			Err:     underlying,
		}
		assert.True(t, errors.Is(err, underlying))
	})

	t.Run("ConfigError supports errors.Is", func(t *testing.T) {
		underlying := ErrInvalidConfig
		err := &ConfigError{
			Message: "bad config",
			Err:     underlying,
		}
		assert.True(t, errors.Is(err, underlying))
	})

	t.Run("ValidationError supports errors.Is", func(t *testing.T) {
		underlying := ErrValidationFailed
		err := &ValidationError{
			Message: "validation failed",
			Err:     underlying,
		}
		assert.True(t, errors.Is(err, underlying))
	})

	t.Run("TLSError supports errors.Is", func(t *testing.T) {
		underlying := ErrTLSHandshake
		err := &TLSError{
			Message: "handshake failed",
			Err:     underlying,
		}
		assert.True(t, errors.Is(err, underlying))
	})
}

func TestErrorChaining(t *testing.T) {
	t.Run("NetworkError chaining", func(t *testing.T) {
		err := NewNetworkError("connection failed").
			WithField("host", "example.com").
			WithField("port", 8080).
			WithFields(map[string]any{
				"timeout": 30,
				"retry":   true,
			})

		assert.Equal(t, "example.com", err.Fields["host"])
		assert.Equal(t, 8080, err.Fields["port"])
		assert.Equal(t, 30, err.Fields["timeout"])
		assert.Equal(t, true, err.Fields["retry"])
	})

	t.Run("ConfigError chaining", func(t *testing.T) {
		err := NewConfigError("invalid config").
			WithField("file", "config.yaml").
			WithFields(map[string]any{
				"line": 10,
			})

		assert.Equal(t, "config.yaml", err.Fields["file"])
		assert.Equal(t, 10, err.Fields["line"])
	})

	t.Run("ValidationError chaining with children", func(t *testing.T) {
		child1 := NewValidationError("child1").WithField("index", 0)
		child2 := NewValidationError("child2").WithField("index", 1)

		err := NewValidationError("parent").
			WithField("type", "array").
			AddChild(child1).
			AddChild(child2)

		assert.Equal(t, "array", err.Fields["type"])
		assert.Len(t, err.Children, 2)
		assert.Equal(t, child1, err.Children[0])
		assert.Equal(t, child2, err.Children[1])
	})

	t.Run("TLSError chaining", func(t *testing.T) {
		err := NewTLSError("TLS failed").
			WithField("version", "TLS1.3").
			WithFields(map[string]any{
				"cipher": "AES-256-GCM",
			})

		assert.Equal(t, "TLS1.3", err.Fields["version"])
		assert.Equal(t, "AES-256-GCM", err.Fields["cipher"])
	})
}
