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
	"fmt"
)

// Common controller errors
var (
	// ErrResourceNotFound indicates a Kubernetes resource was not found
	ErrResourceNotFound = errors.New("resource not found")

	// ErrSnapshotBuildFailed indicates snapshot building failed
	ErrSnapshotBuildFailed = errors.New("snapshot build failed")

	// ErrInvalidSpec indicates a resource spec is invalid
	ErrInvalidSpec = errors.New("invalid resource spec")

	// ErrReconciliationFailed indicates reconciliation failed
	ErrReconciliationFailed = errors.New("reconciliation failed")

	// errService is a sentinel for service-related errors.
	errService = errors.New("service")

	// errValidationFailed is a sentinel for validation failures.
	errValidationFailed = errors.New("validation failed")
)

// ReconcileError represents a reconciliation error with additional context
type ReconcileError struct {
	Resource  string // Resource kind (e.g., "ProxyGateway")
	Namespace string // Resource namespace
	Name      string // Resource name
	Phase     string // Reconciliation phase that failed
	Message   string // Human-readable error message
	Err       error  // Underlying error, if any
}

func (e *ReconcileError) Error() string {
	resource := fmt.Sprintf("%s/%s", e.Namespace, e.Name)
	if e.Namespace == "" {
		resource = e.Name
	}
	if e.Err != nil {
		return fmt.Sprintf("reconcile error %s %s (phase: %s): %s: %v", e.Resource, resource, e.Phase, e.Message, e.Err)
	}
	return fmt.Sprintf("reconcile error %s %s (phase: %s): %s", e.Resource, resource, e.Phase, e.Message)
}

func (e *ReconcileError) Unwrap() error {
	return e.Err
}

// NewReconcileError creates a new reconciliation error
func NewReconcileError(resource, namespace, name, phase, message string, err error) *ReconcileError {
	return &ReconcileError{
		Resource:  resource,
		Namespace: namespace,
		Name:      name,
		Phase:     phase,
		Message:   message,
		Err:       err,
	}
}

// TranslationError represents an error during resource translation
type TranslationError struct {
	SourceKind string // Source resource kind (e.g., "Ingress", "HTTPRoute")
	TargetKind string // Target resource kind (e.g., "ProxyRoute")
	Namespace  string // Resource namespace
	Name       string // Resource name
	Message    string // Human-readable error message
	Err        error  // Underlying error, if any
}

func (e *TranslationError) Error() string {
	resource := fmt.Sprintf("%s/%s", e.Namespace, e.Name)
	if e.Err != nil {
		return fmt.Sprintf("translation error %s->%s %s: %s: %v", e.SourceKind, e.TargetKind, resource, e.Message, e.Err)
	}
	return fmt.Sprintf("translation error %s->%s %s: %s", e.SourceKind, e.TargetKind, resource, e.Message)
}

func (e *TranslationError) Unwrap() error {
	return e.Err
}

// NewTranslationError creates a new translation error
func NewTranslationError(sourceKind, targetKind, namespace, name, message string, err error) *TranslationError {
	return &TranslationError{
		SourceKind: sourceKind,
		TargetKind: targetKind,
		Namespace:  namespace,
		Name:       name,
		Message:    message,
		Err:        err,
	}
}

// SnapshotError represents an error during snapshot building
type SnapshotError struct {
	Node    string // Node name for which snapshot is being built
	Version string // Snapshot version
	Message string // Human-readable error message
	Err     error  // Underlying error, if any
}

func (e *SnapshotError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("snapshot error node=%s version=%s: %s: %v", e.Node, e.Version, e.Message, e.Err)
	}
	return fmt.Sprintf("snapshot error node=%s version=%s: %s", e.Node, e.Version, e.Message)
}

func (e *SnapshotError) Unwrap() error {
	return e.Err
}

// NewSnapshotError creates a new snapshot error
func NewSnapshotError(node, version, message string, err error) *SnapshotError {
	return &SnapshotError{
		Node:    node,
		Version: version,
		Message: message,
		Err:     err,
	}
}
