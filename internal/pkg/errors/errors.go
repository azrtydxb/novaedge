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

// Package apperrors provides standardized error types and error handling utilities
// for NovaEdge components.
//
// # Error Handling Guidelines
//
// 1. Use specific error types for different categories of errors:
//   - NetworkError: For connection, timeout, DNS resolution failures
//   - ConfigError: For configuration parsing, validation failures
//   - ValidationError: For input validation, schema validation failures
//   - TLSError: For TLS handshake, certificate, cipher suite failures
//
// 2. Always wrap errors with context using fmt.Errorf and %w:
//
//	if err != nil {
//	    return fmt.Errorf("failed to connect to backend: %w", err)
//	}
//
// 3. Use errors.As() for error checking:
//
//	var netErr *NetworkError
//	if errors.As(err, &netErr) {
//	    // Handle network error
//	}
//
// 4. Add structured context to errors using WithField() and WithFields():
//
//	err := NewNetworkError("connection timeout").
//	    WithField("host", "backend.example.com").
//	    WithField("port", 8080)
//
// 5. Log errors at appropriate levels:
//   - ERROR: Unrecoverable failures requiring immediate attention
//   - WARN: Recoverable failures, degraded functionality
//   - INFO: Expected errors as part of normal operation (e.g., validation failures)
//   - DEBUG: Detailed error context for troubleshooting
package apperrors

import (
	"fmt"
	"strings"
)

// NetworkError represents network-related errors
type NetworkError struct {
	Op      string         // Operation that failed (e.g., "dial", "connect", "read")
	Host    string         // Host or address
	Port    int32          // Port number
	Message string         // Error message
	Err     error          // Underlying error
	Fields  map[string]any // Additional structured context
}

func (e *NetworkError) Error() string {
	var parts []string
	if e.Op != "" {
		parts = append(parts, e.Op)
	}
	if e.Host != "" {
		if e.Port > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", e.Host, e.Port))
		} else {
			parts = append(parts, e.Host)
		}
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}

	msg := strings.Join(parts, ": ")
	if e.Err != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Err)
	}
	return msg
}

func (e *NetworkError) Unwrap() error {
	return e.Err
}

// WithField adds a structured field to the error
func (e *NetworkError) WithField(key string, value any) *NetworkError {
	if e.Fields == nil {
		e.Fields = make(map[string]any)
	}
	e.Fields[key] = value
	return e
}

// WithFields adds multiple structured fields to the error
func (e *NetworkError) WithFields(fields map[string]any) *NetworkError {
	if e.Fields == nil {
		e.Fields = make(map[string]any)
	}
	for k, v := range fields {
		e.Fields[k] = v
	}
	return e
}

// NewNetworkError creates a new network error
func NewNetworkError(message string) *NetworkError {
	return &NetworkError{
		Message: message,
		Fields:  make(map[string]any),
	}
}

// ConfigError represents configuration-related errors
type ConfigError struct {
	Field   string         // Configuration field that caused error
	Value   any            // Invalid value
	Message string         // Error message
	Err     error          // Underlying error
	Fields  map[string]any // Additional structured context
}

func (e *ConfigError) Error() string {
	var parts []string
	if e.Field != "" {
		parts = append(parts, fmt.Sprintf("field '%s'", e.Field))
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Value != nil {
		parts = append(parts, fmt.Sprintf("value: %v", e.Value))
	}

	msg := strings.Join(parts, ": ")
	if e.Err != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Err)
	}
	return msg
}

func (e *ConfigError) Unwrap() error {
	return e.Err
}

// WithField adds a structured field to the error
func (e *ConfigError) WithField(key string, value any) *ConfigError {
	if e.Fields == nil {
		e.Fields = make(map[string]any)
	}
	e.Fields[key] = value
	return e
}

// WithFields adds multiple structured fields to the error
func (e *ConfigError) WithFields(fields map[string]any) *ConfigError {
	if e.Fields == nil {
		e.Fields = make(map[string]any)
	}
	for k, v := range fields {
		e.Fields[k] = v
	}
	return e
}

// NewConfigError creates a new configuration error
func NewConfigError(message string) *ConfigError {
	return &ConfigError{
		Message: message,
		Fields:  make(map[string]any),
	}
}

// ValidationError represents validation-related errors
type ValidationError struct {
	Field    string             // Field that failed validation
	Value    any                // Invalid value
	Rule     string             // Validation rule that failed
	Message  string             // Error message
	Err      error              // Underlying error
	Fields   map[string]any     // Additional structured context
	Children []*ValidationError // Nested validation errors
}

func (e *ValidationError) Error() string {
	var parts []string
	if e.Field != "" {
		parts = append(parts, fmt.Sprintf("field '%s'", e.Field))
	}
	if e.Rule != "" {
		parts = append(parts, fmt.Sprintf("rule '%s'", e.Rule))
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Value != nil {
		parts = append(parts, fmt.Sprintf("value: %v", e.Value))
	}

	msg := strings.Join(parts, ": ")
	if e.Err != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Err)
	}

	// Include child errors
	if len(e.Children) > 0 {
		childMsgs := make([]string, 0, len(e.Children))
		for _, child := range e.Children {
			childMsgs = append(childMsgs, child.Error())
		}
		msg = fmt.Sprintf("%s [%s]", msg, strings.Join(childMsgs, "; "))
	}

	return msg
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

// WithField adds a structured field to the error
func (e *ValidationError) WithField(key string, value any) *ValidationError {
	if e.Fields == nil {
		e.Fields = make(map[string]any)
	}
	e.Fields[key] = value
	return e
}

// WithFields adds multiple structured fields to the error
func (e *ValidationError) WithFields(fields map[string]any) *ValidationError {
	if e.Fields == nil {
		e.Fields = make(map[string]any)
	}
	for k, v := range fields {
		e.Fields[k] = v
	}
	return e
}

// AddChild adds a nested validation error
func (e *ValidationError) AddChild(child *ValidationError) *ValidationError {
	e.Children = append(e.Children, child)
	return e
}

// NewValidationError creates a new validation error
func NewValidationError(message string) *ValidationError {
	return &ValidationError{
		Message:  message,
		Fields:   make(map[string]any),
		Children: make([]*ValidationError, 0),
	}
}

// TLSError represents TLS-related errors
type TLSError struct {
	Op      string         // Operation that failed (e.g., "handshake", "verify")
	Host    string         // Host or SNI name
	Message string         // Error message
	Err     error          // Underlying error
	Fields  map[string]any // Additional structured context
}

func (e *TLSError) Error() string {
	var parts []string
	if e.Op != "" {
		parts = append(parts, e.Op)
	}
	if e.Host != "" {
		parts = append(parts, e.Host)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}

	msg := strings.Join(parts, ": ")
	if e.Err != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Err)
	}
	return msg
}

func (e *TLSError) Unwrap() error {
	return e.Err
}

// WithField adds a structured field to the error
func (e *TLSError) WithField(key string, value any) *TLSError {
	if e.Fields == nil {
		e.Fields = make(map[string]any)
	}
	e.Fields[key] = value
	return e
}

// WithFields adds multiple structured fields to the error
func (e *TLSError) WithFields(fields map[string]any) *TLSError {
	if e.Fields == nil {
		e.Fields = make(map[string]any)
	}
	for k, v := range fields {
		e.Fields[k] = v
	}
	return e
}

// NewTLSError creates a new TLS error
func NewTLSError(message string) *TLSError {
	return &TLSError{
		Message: message,
		Fields:  make(map[string]any),
	}
}
