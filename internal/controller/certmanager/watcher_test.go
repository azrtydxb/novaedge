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

package certmanager

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestNewCertificateWatcher(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
	watcher := NewCertificateWatcher(dynamicClient)

	if watcher == nil {
		t.Fatal("expected watcher to be created")
	}
	if watcher.dynamicClient == nil {
		t.Error("expected dynamicClient to be set")
	}
	if watcher.stopCh == nil {
		t.Error("expected stopCh to be initialized")
	}
}

func TestCertificateWatcher_OnReady(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
	watcher := NewCertificateWatcher(dynamicClient)

	callbackCalled := false
	watcher.OnReady(func(namespace, name, secretName string) {
		callbackCalled = true
	})

	if watcher.onReady == nil {
		t.Error("expected onReady callback to be set")
	}

	// Trigger callback directly to verify it works
	if watcher.onReady != nil {
		watcher.onReady("test-ns", "test-cert", "test-secret")
	}

	if !callbackCalled {
		t.Error("expected callback to be called")
	}
}

func TestCertificateWatcher_OnFailed(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
	watcher := NewCertificateWatcher(dynamicClient)

	var capturedMessage string
	watcher.OnFailed(func(namespace, name, message string) {
		capturedMessage = message
	})

	if watcher.onFailed == nil {
		t.Error("expected onFailed callback to be set")
	}

	// Trigger callback
	if watcher.onFailed != nil {
		watcher.onFailed("test-ns", "test-cert", "validation failed")
	}

	if capturedMessage != "validation failed" {
		t.Errorf("expected message 'validation failed', got '%s'", capturedMessage)
	}
}

func TestCertificateWatcher_StartStop(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
	watcher := NewCertificateWatcher(dynamicClient)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start watcher
	err := watcher.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error starting watcher: %v", err)
	}

	if !watcher.running {
		t.Error("expected watcher to be running")
	}

	// Starting again should be no-op
	err = watcher.Start(ctx)
	if err != nil {
		t.Errorf("unexpected error on second start: %v", err)
	}

	// Stop watcher
	watcher.Stop()

	// Wait a bit for goroutine to exit
	time.Sleep(50 * time.Millisecond)

	if watcher.running {
		t.Error("expected watcher to be stopped")
	}

	// Stopping again should be safe
	watcher.Stop()
}

func TestCertificateWatcher_HandleCertificateEvent_Ready(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
	watcher := NewCertificateWatcher(dynamicClient)

	var readyCalled bool
	var capturedNamespace, capturedName, capturedSecret string

	watcher.OnReady(func(namespace, name, secretName string) {
		readyCalled = true
		capturedNamespace = namespace
		capturedName = name
		capturedSecret = secretName
	})

	// Create a certificate with Ready=True condition
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})
	cert.SetName("test-cert")
	cert.SetNamespace("test-ns")

	// Set spec.secretName
	_ = unstructured.SetNestedField(cert.Object, "test-secret", "spec", "secretName")

	// Set Ready condition
	conditions := []interface{}{
		map[string]interface{}{
			"type":    "Ready",
			"status":  "True",
			"message": "Certificate is ready",
		},
	}
	_ = unstructured.SetNestedSlice(cert.Object, conditions, "status", "conditions")

	// Handle the event
	ctx := context.Background()
	watcher.handleCertificateEvent(ctx, cert)

	if !readyCalled {
		t.Error("expected onReady callback to be called")
	}
	if capturedNamespace != "test-ns" {
		t.Errorf("expected namespace 'test-ns', got '%s'", capturedNamespace)
	}
	if capturedName != "test-cert" {
		t.Errorf("expected name 'test-cert', got '%s'", capturedName)
	}
	if capturedSecret != "test-secret" {
		t.Errorf("expected secret 'test-secret', got '%s'", capturedSecret)
	}
}

func TestCertificateWatcher_HandleCertificateEvent_Failed(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
	watcher := NewCertificateWatcher(dynamicClient)

	var failedCalled bool
	var capturedMessage string

	watcher.OnFailed(func(namespace, name, message string) {
		failedCalled = true
		capturedMessage = message
	})

	// Create a certificate with Ready=False and Issuing=False
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})
	cert.SetName("test-cert")
	cert.SetNamespace("test-ns")

	conditions := []interface{}{
		map[string]interface{}{
			"type":    "Ready",
			"status":  "False",
			"message": "Certificate validation failed",
		},
		map[string]interface{}{
			"type":   "Issuing",
			"status": "False",
		},
	}
	_ = unstructured.SetNestedSlice(cert.Object, conditions, "status", "conditions")

	// Handle the event
	ctx := context.Background()
	watcher.handleCertificateEvent(ctx, cert)

	if !failedCalled {
		t.Error("expected onFailed callback to be called")
	}
	if capturedMessage != "Certificate validation failed" {
		t.Errorf("expected message about validation failure, got '%s'", capturedMessage)
	}
}

func TestIsCertFailed(t *testing.T) {
	tests := []struct {
		name       string
		conditions []interface{}
		expected   bool
	}{
		{
			name: "issuing false indicates failure",
			conditions: []interface{}{
				map[string]interface{}{
					"type":   "Issuing",
					"status": "False",
				},
			},
			expected: true,
		},
		{
			name: "issuing true is not failure",
			conditions: []interface{}{
				map[string]interface{}{
					"type":   "Issuing",
					"status": "True",
				},
			},
			expected: false,
		},
		{
			name: "no issuing condition is not failure",
			conditions: []interface{}{
				map[string]interface{}{
					"type":   "Ready",
					"status": "False",
				},
			},
			expected: false,
		},
		{
			name:       "no conditions is not failure",
			conditions: nil,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cert := &unstructured.Unstructured{
				Object: make(map[string]interface{}),
			}
			if tt.conditions != nil {
				_ = unstructured.SetNestedSlice(cert.Object, tt.conditions, "status", "conditions")
			}

			result := isCertFailed(cert)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCertificateWatcher_HandleCertificateEvent_InvalidObject(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
	watcher := NewCertificateWatcher(dynamicClient)

	ctx := context.Background()

	// Pass invalid object (not *unstructured.Unstructured)
	watcher.handleCertificateEvent(ctx, "invalid object")

	// Should not panic, just return silently
}
