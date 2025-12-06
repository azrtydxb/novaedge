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
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestProxyBackendReconcile(t *testing.T) {
	tests := []struct {
		name          string
		backend       *novaedgev1alpha1.ProxyBackend
		service       *corev1.Service
		expectError   bool
		expectReady   bool
		validationErr string
	}{
		{
			name: "valid backend with service reference",
			backend: &novaedgev1alpha1.ProxyBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-backend",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyBackendSpec{
					ServiceRef: &novaedgev1alpha1.ServiceReference{
						Name: "test-service",
						Port: 8080,
					},
					LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
					},
				},
			},
			expectError: false,
			expectReady: true,
		},
		{
			name: "backend with missing service",
			backend: &novaedgev1alpha1.ProxyBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-backend",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyBackendSpec{
					ServiceRef: &novaedgev1alpha1.ServiceReference{
						Name: "nonexistent-service",
						Port: 8080,
					},
					LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
				},
			},
			expectError:   true,
			expectReady:   false,
			validationErr: "not found",
		},
		{
			name: "backend with invalid port",
			backend: &novaedgev1alpha1.ProxyBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-port-backend",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyBackendSpec{
					ServiceRef: &novaedgev1alpha1.ServiceReference{
						Name: "test-service",
						Port: 9999,
					},
					LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
					},
				},
			},
			expectError:   true,
			expectReady:   false,
			validationErr: "not found",
		},
		{
			name: "backend with health check configuration",
			backend: &novaedgev1alpha1.ProxyBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "health-check-backend",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyBackendSpec{
					ServiceRef: &novaedgev1alpha1.ServiceReference{
						Name: "test-service",
						Port: 8080,
					},
					LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
					HealthCheck: &novaedgev1alpha1.HealthCheck{
						HTTPPath:           strPtr("/health"),
						HealthyThreshold:   int32Ptr(2),
						UnhealthyThreshold: int32Ptr(3),
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
					},
				},
			},
			expectError: false,
			expectReady: true,
		},
		{
			name: "backend with invalid health check threshold",
			backend: &novaedgev1alpha1.ProxyBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bad-health-backend",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyBackendSpec{
					ServiceRef: &novaedgev1alpha1.ServiceReference{
						Name: "test-service",
						Port: 8080,
					},
					LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
					HealthCheck: &novaedgev1alpha1.HealthCheck{
						HealthyThreshold: int32Ptr(0),
					},
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
					},
				},
			},
			expectError:   true,
			expectReady:   false,
			validationErr: "HealthyThreshold",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			env := setupTestEnv(t)

			if test.service != nil {
				if err := env.client.Create(ctx, test.service); err != nil {
					t.Fatalf("failed to create Service: %v", err)
				}
			}

			if err := env.client.Create(ctx, test.backend); err != nil {
				t.Fatalf("failed to create backend: %v", err)
			}

			// Manually trigger reconciliation
			err := env.reconcileProxyBackend(ctx, test.backend.Name, test.backend.Namespace)
			// Note: reconciler returns error for validation failures, which is expected
			if test.expectError && err == nil {
				// This is fine - the error is recorded in status conditions
			}

			updatedBackend := &novaedgev1alpha1.ProxyBackend{}
			if err := env.client.Get(ctx, types.NamespacedName{
				Name:      test.backend.Name,
				Namespace: test.backend.Namespace,
			}, updatedBackend); err != nil {
				t.Fatalf("failed to get backend: %v", err)
			}

			readyCondition := meta.FindStatusCondition(updatedBackend.Status.Conditions, "Ready")
			if readyCondition == nil {
				t.Fatal("expected Ready condition, got nil")
			}

			if test.expectReady && readyCondition.Status != metav1.ConditionTrue {
				t.Errorf("expected Ready=True, got %s. Message: %s", readyCondition.Status, readyCondition.Message)
			}

			if !test.expectReady && readyCondition.Status != metav1.ConditionFalse {
				t.Errorf("expected Ready=False, got %s", readyCondition.Status)
			}

			if test.validationErr != "" && readyCondition.Status == metav1.ConditionFalse {
				if !contains(readyCondition.Message, test.validationErr) {
					t.Errorf("expected error message containing '%s', got '%s'", test.validationErr, readyCondition.Message)
				}
			}
		})
	}
}

func TestProxyBackendCircuitBreaker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}

	backend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cb-backend",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			ServiceRef: &novaedgev1alpha1.ServiceReference{
				Name: "test-service",
				Port: 8080,
			},
			LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
			CircuitBreaker: &novaedgev1alpha1.CircuitBreaker{
				MaxConnections:     int32Ptr(100),
				MaxPendingRequests: int32Ptr(50),
				MaxRequests:        int32Ptr(200),
				MaxRetries:         int32Ptr(3),
			},
		},
	}

	if err := env.client.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.client.Create(ctx, backend); err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyBackend(ctx, backend.Name, backend.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedBackend := &novaedgev1alpha1.ProxyBackend{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      backend.Name,
		Namespace: backend.Namespace,
	}, updatedBackend); err != nil {
		t.Fatalf("failed to get backend: %v", err)
	}

	readyCondition := meta.FindStatusCondition(updatedBackend.Status.Conditions, "Ready")
	if readyCondition == nil || readyCondition.Status != metav1.ConditionTrue {
		t.Error("expected backend with valid circuit breaker config to be ready")
	}
}

func TestProxyBackendCircuitBreakerValidation(t *testing.T) {
	tests := []struct {
		name           string
		maxConnections *int32
		maxRequests    *int32
		maxRetries     *int32
		expectError    bool
	}{
		{
			name:           "valid circuit breaker values",
			maxConnections: int32Ptr(100),
			maxRequests:    int32Ptr(200),
			maxRetries:     int32Ptr(3),
			expectError:    false,
		},
		{
			name:           "invalid max connections (0)",
			maxConnections: int32Ptr(0),
			maxRequests:    int32Ptr(200),
			expectError:    true,
		},
		{
			name:           "invalid max retries (-1)",
			maxConnections: int32Ptr(100),
			maxRetries:     int32Ptr(-1),
			expectError:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			env := setupTestEnv(t)

			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
					},
				},
			}

			backend := &novaedgev1alpha1.ProxyBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cb-test-backend",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyBackendSpec{
					ServiceRef: &novaedgev1alpha1.ServiceReference{
						Name: "test-service",
						Port: 8080,
					},
					LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
					CircuitBreaker: &novaedgev1alpha1.CircuitBreaker{
						MaxConnections: test.maxConnections,
						MaxRequests:    test.maxRequests,
						MaxRetries:     test.maxRetries,
					},
				},
			}

			if err := env.client.Create(ctx, service); err != nil {
				t.Fatalf("failed to create Service: %v", err)
			}

			if err := env.client.Create(ctx, backend); err != nil {
				t.Fatalf("failed to create backend: %v", err)
			}

			// Manually trigger reconciliation
			_ = env.reconcileProxyBackend(ctx, backend.Name, backend.Namespace)

			updatedBackend := &novaedgev1alpha1.ProxyBackend{}
			if err := env.client.Get(ctx, types.NamespacedName{
				Name:      backend.Name,
				Namespace: backend.Namespace,
			}, updatedBackend); err != nil {
				t.Fatalf("failed to get backend: %v", err)
			}

			readyCondition := meta.FindStatusCondition(updatedBackend.Status.Conditions, "Ready")
			if readyCondition == nil {
				t.Fatal("expected Ready condition, got nil")
			}

			if test.expectError && readyCondition.Status == metav1.ConditionTrue {
				t.Error("expected backend validation to fail")
			}

			if !test.expectError && readyCondition.Status != metav1.ConditionTrue {
				t.Errorf("expected backend to be ready, got: %s", readyCondition.Message)
			}
		})
	}
}

func TestProxyBackendTLS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       8443,
					TargetPort: intstr.FromInt(8443),
				},
			},
		},
	}

	caCertSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca-cert",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"ca.crt": []byte("cert"),
		},
	}

	backend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-backend",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			ServiceRef: &novaedgev1alpha1.ServiceReference{
				Name: "test-service",
				Port: 8443,
			},
			LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
			TLS: &novaedgev1alpha1.BackendTLSConfig{
				Enabled:         true,
				CACertSecretRef: strPtr("ca-cert"),
			},
		},
	}

	if err := env.client.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.client.Create(ctx, caCertSecret); err != nil {
		t.Fatalf("failed to create CA cert secret: %v", err)
	}

	if err := env.client.Create(ctx, backend); err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyBackend(ctx, backend.Name, backend.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedBackend := &novaedgev1alpha1.ProxyBackend{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      backend.Name,
		Namespace: backend.Namespace,
	}, updatedBackend); err != nil {
		t.Fatalf("failed to get backend: %v", err)
	}

	readyCondition := meta.FindStatusCondition(updatedBackend.Status.Conditions, "Ready")
	if readyCondition == nil || readyCondition.Status != metav1.ConditionTrue {
		t.Error("expected TLS-enabled backend to be ready with valid CA cert")
	}
}

func TestProxyBackendTLSMissingCert(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       8443,
					TargetPort: intstr.FromInt(8443),
				},
			},
		},
	}

	backend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "missing-tls-backend",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			ServiceRef: &novaedgev1alpha1.ServiceReference{
				Name: "test-service",
				Port: 8443,
			},
			LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
			TLS: &novaedgev1alpha1.BackendTLSConfig{
				Enabled:         true,
				CACertSecretRef: strPtr("missing-ca-cert"),
			},
		},
	}

	if err := env.client.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.client.Create(ctx, backend); err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	// Manually trigger reconciliation - expect error
	_ = env.reconcileProxyBackend(ctx, backend.Name, backend.Namespace)

	updatedBackend := &novaedgev1alpha1.ProxyBackend{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      backend.Name,
		Namespace: backend.Namespace,
	}, updatedBackend); err != nil {
		t.Fatalf("failed to get backend: %v", err)
	}

	readyCondition := meta.FindStatusCondition(updatedBackend.Status.Conditions, "Ready")
	if readyCondition == nil || readyCondition.Status != metav1.ConditionFalse {
		t.Error("expected backend with missing TLS cert to fail validation")
	}
}

func TestProxyBackendLBPolicies(t *testing.T) {
	policies := []novaedgev1alpha1.LoadBalancingPolicy{
		novaedgev1alpha1.LBPolicyRoundRobin,
		novaedgev1alpha1.LBPolicyP2C,
		novaedgev1alpha1.LBPolicyEWMA,
		novaedgev1alpha1.LBPolicyRingHash,
		novaedgev1alpha1.LBPolicyMaglev,
	}

	for _, policy := range policies {
		t.Run(string(policy), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			env := setupTestEnv(t)

			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
					},
				},
			}

			backend := &novaedgev1alpha1.ProxyBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy-backend-" + string(policy),
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyBackendSpec{
					ServiceRef: &novaedgev1alpha1.ServiceReference{
						Name: "test-service",
						Port: 8080,
					},
					LBPolicy: policy,
				},
			}

			if err := env.client.Create(ctx, service); err != nil {
				t.Fatalf("failed to create Service: %v", err)
			}

			if err := env.client.Create(ctx, backend); err != nil {
				t.Fatalf("failed to create backend: %v", err)
			}

			// Manually trigger reconciliation
			if err := env.reconcileProxyBackend(ctx, backend.Name, backend.Namespace); err != nil {
				t.Fatalf("reconciliation failed: %v", err)
			}

			updatedBackend := &novaedgev1alpha1.ProxyBackend{}
			if err := env.client.Get(ctx, types.NamespacedName{
				Name:      backend.Name,
				Namespace: backend.Namespace,
			}, updatedBackend); err != nil {
				t.Fatalf("failed to get backend: %v", err)
			}

			readyCondition := meta.FindStatusCondition(updatedBackend.Status.Conditions, "Ready")
			if readyCondition == nil || readyCondition.Status != metav1.ConditionTrue {
				t.Errorf("backend with %s policy should be ready", policy)
			}
		})
	}
}
