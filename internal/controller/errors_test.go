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

package controller

import (
	"errors"
	"strings"
	"testing"
)

func TestReconcileError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *ReconcileError
		contains []string
	}{
		{
			name: "with underlying error",
			err: &ReconcileError{
				Resource:  "ProxyGateway",
				Namespace: "default",
				Name:      "test-gateway",
				Phase:     "initialize",
				Message:   "failed to initialize",
				Err:       errors.New("underlying error"),
			},
			contains: []string{"ProxyGateway", "default/test-gateway", "initialize", "failed to initialize", "underlying error"},
		},
		{
			name: "without underlying error",
			err: &ReconcileError{
				Resource:  "ProxyRoute",
				Namespace: "kube-system",
				Name:      "my-route",
				Phase:     "sync",
				Message:   "sync failed",
			},
			contains: []string{"ProxyRoute", "kube-system/my-route", "sync", "sync failed"},
		},
		{
			name: "without namespace",
			err: &ReconcileError{
				Resource: "ClusterResource",
				Name:     "cluster-name",
				Phase:    "delete",
				Message:  "deletion failed",
			},
			contains: []string{"ClusterResource", "cluster-name", "delete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.Error()
			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("Error() = %q, should contain %q", result, s)
				}
			}
		})
	}
}

func TestReconcileError_Unwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := &ReconcileError{
		Resource: "Test",
		Name:     "test",
		Err:      underlying,
	}

	unwrapped := err.Unwrap()
	if unwrapped != underlying {
		t.Error("Unwrap() should return underlying error")
	}

	// Test with no underlying error
	errNoUnderlying := &ReconcileError{
		Resource: "Test",
		Name:     "test",
	}

	if errNoUnderlying.Unwrap() != nil {
		t.Error("Unwrap() should return nil when no underlying error")
	}
}

func TestNewReconcileError(t *testing.T) {
	underlying := errors.New("underlying")
	err := NewReconcileError("ProxyGateway", "default", "test", "init", "message", underlying)

	if err.Resource != "ProxyGateway" {
		t.Errorf("Resource = %q, want %q", err.Resource, "ProxyGateway")
	}
	if err.Namespace != "default" {
		t.Errorf("Namespace = %q, want %q", err.Namespace, "default")
	}
	if err.Name != "test" {
		t.Errorf("Name = %q, want %q", err.Name, "test")
	}
	if err.Phase != "init" {
		t.Errorf("Phase = %q, want %q", err.Phase, "init")
	}
	if err.Message != "message" {
		t.Errorf("Message = %q, want %q", err.Message, "message")
	}
	if err.Err != underlying {
		t.Error("Err should be underlying error")
	}
}

func TestTranslationError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *TranslationError
		contains []string
	}{
		{
			name: "with underlying error",
			err: &TranslationError{
				SourceKind: "Ingress",
				TargetKind: "ProxyRoute",
				Namespace:  "default",
				Name:       "test-ingress",
				Message:    "failed to translate",
				Err:        errors.New("parse error"),
			},
			contains: []string{"Ingress", "ProxyRoute", "default/test-ingress", "failed to translate", "parse error"},
		},
		{
			name: "without underlying error",
			err: &TranslationError{
				SourceKind: "HTTPRoute",
				TargetKind: "ProxyRoute",
				Namespace:  "production",
				Name:       "api-route",
				Message:    "unsupported feature",
			},
			contains: []string{"HTTPRoute", "ProxyRoute", "production/api-route", "unsupported feature"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.Error()
			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("Error() = %q, should contain %q", result, s)
				}
			}
		})
	}
}

func TestTranslationError_Unwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := &TranslationError{
		SourceKind: "Ingress",
		TargetKind: "ProxyRoute",
		Err:        underlying,
	}

	unwrapped := err.Unwrap()
	if unwrapped != underlying {
		t.Error("Unwrap() should return underlying error")
	}

	// Test with no underlying error
	errNoUnderlying := &TranslationError{
		SourceKind: "Ingress",
		TargetKind: "ProxyRoute",
	}

	if errNoUnderlying.Unwrap() != nil {
		t.Error("Unwrap() should return nil when no underlying error")
	}
}

func TestNewTranslationError(t *testing.T) {
	underlying := errors.New("underlying")
	err := NewTranslationError("Ingress", "ProxyRoute", "default", "test", "message", underlying)

	if err.SourceKind != "Ingress" {
		t.Errorf("SourceKind = %q, want %q", err.SourceKind, "Ingress")
	}
	if err.TargetKind != "ProxyRoute" {
		t.Errorf("TargetKind = %q, want %q", err.TargetKind, "ProxyRoute")
	}
	if err.Namespace != "default" {
		t.Errorf("Namespace = %q, want %q", err.Namespace, "default")
	}
	if err.Name != "test" {
		t.Errorf("Name = %q, want %q", err.Name, "test")
	}
	if err.Message != "message" {
		t.Errorf("Message = %q, want %q", err.Message, "message")
	}
	if err.Err != underlying {
		t.Error("Err should be underlying error")
	}
}

func TestSnapshotError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *SnapshotError
		contains []string
	}{
		{
			name: "with underlying error",
			err: &SnapshotError{
				Node:    "node-1",
				Version: "v1.2.3",
				Message: "build failed",
				Err:     errors.New("out of memory"),
			},
			contains: []string{"node=node-1", "version=v1.2.3", "build failed", "out of memory"},
		},
		{
			name: "without underlying error",
			err: &SnapshotError{
				Node:    "node-2",
				Version: "v2.0.0",
				Message: "timeout",
			},
			contains: []string{"node=node-2", "version=v2.0.0", "timeout"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.Error()
			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("Error() = %q, should contain %q", result, s)
				}
			}
		})
	}
}

func TestSnapshotError_Unwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := &SnapshotError{
		Node: "test-node",
		Err:  underlying,
	}

	unwrapped := err.Unwrap()
	if unwrapped != underlying {
		t.Error("Unwrap() should return underlying error")
	}

	// Test with no underlying error
	errNoUnderlying := &SnapshotError{
		Node: "test-node",
	}

	if errNoUnderlying.Unwrap() != nil {
		t.Error("Unwrap() should return nil when no underlying error")
	}
}

func TestNewSnapshotError(t *testing.T) {
	underlying := errors.New("underlying")
	err := NewSnapshotError("node-1", "v1.0.0", "message", underlying)

	if err.Node != "node-1" {
		t.Errorf("Node = %q, want %q", err.Node, "node-1")
	}
	if err.Version != "v1.0.0" {
		t.Errorf("Version = %q, want %q", err.Version, "v1.0.0")
	}
	if err.Message != "message" {
		t.Errorf("Message = %q, want %q", err.Message, "message")
	}
	if err.Err != underlying {
		t.Error("Err should be underlying error")
	}
}

func TestCommonErrors(t *testing.T) {
	// Verify common errors are defined
	if ErrResourceNotFound == nil {
		t.Error("ErrResourceNotFound should not be nil")
	}
	if ErrSnapshotBuildFailed == nil {
		t.Error("ErrSnapshotBuildFailed should not be nil")
	}
	if ErrInvalidSpec == nil {
		t.Error("ErrInvalidSpec should not be nil")
	}
	if ErrReconciliationFailed == nil {
		t.Error("ErrReconciliationFailed should not be nil")
	}

	// Verify error messages
	if ErrResourceNotFound.Error() != "resource not found" {
		t.Errorf("ErrResourceNotFound.Error() = %q, want %q", ErrResourceNotFound.Error(), "resource not found")
	}
}

func TestErrorIs(t *testing.T) {
	// Test errors.Is compatibility
	underlying := errors.New("underlying")
	err := NewReconcileError("Test", "default", "test", "init", "msg", underlying)

	if !errors.Is(err, underlying) {
		t.Error("errors.Is should find underlying error")
	}

	// Test with wrapped translation error
	transErr := NewTranslationError("Ingress", "ProxyRoute", "default", "test", "msg", underlying)
	if !errors.Is(transErr, underlying) {
		t.Error("errors.Is should find underlying error in TranslationError")
	}

	// Test with wrapped snapshot error
	snapErr := NewSnapshotError("node-1", "v1", "msg", underlying)
	if !errors.Is(snapErr, underlying) {
		t.Error("errors.Is should find underlying error in SnapshotError")
	}
}

func TestErrorAs(t *testing.T) {
	// Test errors.As compatibility
	original := &ReconcileError{
		Resource:  "Test",
		Namespace: "default",
		Name:      "test",
	}

	var target *ReconcileError
	if !errors.As(original, &target) {
		t.Error("errors.As should extract ReconcileError")
	}
	if target.Resource != "Test" {
		t.Errorf("target.Resource = %q, want %q", target.Resource, "Test")
	}
}
