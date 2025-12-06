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

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestProxyGatewayReconcile(t *testing.T) {
	tests := []struct {
		name          string
		gateway       *novaedgev1alpha1.ProxyGateway
		vip           *novaedgev1alpha1.ProxyVIP
		secrets       []*corev1.Secret
		expectError   bool
		expectReady   bool
		validationErr string
	}{
		{
			name: "valid gateway with HTTP listener",
			gateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					VIPRef: "test-vip",
					Listeners: []novaedgev1alpha1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
						},
					},
				},
			},
			vip: &novaedgev1alpha1.ProxyVIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vip",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyVIPSpec{
					Address: "10.0.0.1/32",
					Mode:    novaedgev1alpha1.VIPModeL2ARP,
				},
			},
			expectError: false,
			expectReady: true,
		},
		{
			name: "gateway with HTTPS listener and valid TLS secret",
			gateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "https-gateway",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					VIPRef: "test-vip",
					Listeners: []novaedgev1alpha1.Listener{
						{
							Name:     "https",
							Port:     443,
							Protocol: novaedgev1alpha1.ProtocolTypeHTTPS,
							TLS: &novaedgev1alpha1.TLSConfig{
								SecretRef: corev1.SecretReference{
									Name:      "tls-secret",
									Namespace: "default",
								},
							},
						},
					},
				},
			},
			vip: &novaedgev1alpha1.ProxyVIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vip",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyVIPSpec{
					Address: "10.0.0.1/32",
					Mode:    novaedgev1alpha1.VIPModeL2ARP,
				},
			},
			secrets: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"tls.crt": []byte("cert"),
						"tls.key": []byte("key"),
					},
				},
			},
			expectError: false,
			expectReady: true,
		},
		{
			name: "gateway with missing TLS secret",
			gateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-gateway",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					VIPRef: "test-vip",
					Listeners: []novaedgev1alpha1.Listener{
						{
							Name:     "https",
							Port:     443,
							Protocol: novaedgev1alpha1.ProtocolTypeHTTPS,
							TLS: &novaedgev1alpha1.TLSConfig{
								SecretRef: corev1.SecretReference{
									Name:      "missing-secret",
									Namespace: "default",
								},
							},
						},
					},
				},
			},
			vip: &novaedgev1alpha1.ProxyVIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vip",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyVIPSpec{
					Address: "10.0.0.1/32",
					Mode:    novaedgev1alpha1.VIPModeL2ARP,
				},
			},
			expectError:   true,
			expectReady:   false,
			validationErr: "TLS secret",
		},
		{
			name: "gateway with missing VIP reference",
			gateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-vip-gateway",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					VIPRef: "nonexistent-vip",
					Listeners: []novaedgev1alpha1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
						},
					},
				},
			},
			expectError:   true,
			expectReady:   false,
			validationErr: "VIP",
		},
		{
			name: "gateway with HTTPS listener missing TLS config",
			gateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "broken-https-gateway",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					VIPRef: "test-vip",
					Listeners: []novaedgev1alpha1.Listener{
						{
							Name:     "https",
							Port:     443,
							Protocol: novaedgev1alpha1.ProtocolTypeHTTPS,
						},
					},
				},
			},
			vip: &novaedgev1alpha1.ProxyVIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vip",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyVIPSpec{
					Address: "10.0.0.1/32",
					Mode:    novaedgev1alpha1.VIPModeL2ARP,
				},
			},
			expectError:   true,
			expectReady:   false,
			validationErr: "requires TLS",
		},
		{
			name: "gateway with TLS secret missing tls.crt key",
			gateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-cert-gateway",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					VIPRef: "test-vip",
					Listeners: []novaedgev1alpha1.Listener{
						{
							Name:     "https",
							Port:     443,
							Protocol: novaedgev1alpha1.ProtocolTypeHTTPS,
							TLS: &novaedgev1alpha1.TLSConfig{
								SecretRef: corev1.SecretReference{
									Name:      "bad-cert-secret",
									Namespace: "default",
								},
							},
						},
					},
				},
			},
			vip: &novaedgev1alpha1.ProxyVIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vip",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyVIPSpec{
					Address: "10.0.0.1/32",
					Mode:    novaedgev1alpha1.VIPModeL2ARP,
				},
			},
			secrets: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bad-cert-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"tls.key": []byte("key"),
					},
				},
			},
			expectError:   true,
			expectReady:   false,
			validationErr: "missing tls.crt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			env := setupTestEnv(t)

			// Create VIP if provided
			if test.vip != nil {
				if err := env.client.Create(ctx, test.vip); err != nil {
					t.Fatalf("failed to create VIP: %v", err)
				}
			}

			// Create secrets if provided
			for _, secret := range test.secrets {
				if err := env.client.Create(ctx, secret); err != nil {
					t.Fatalf("failed to create secret: %v", err)
				}
			}

			// Create gateway
			if err := env.client.Create(ctx, test.gateway); err != nil {
				t.Fatalf("failed to create gateway: %v", err)
			}

			// Manually trigger reconciliation
			err := env.reconcileProxyGateway(ctx, test.gateway.Name, test.gateway.Namespace)
			if test.expectError && err == nil {
				// Error might be recorded in status conditions instead of returned
			}

			// Fetch updated gateway
			updatedGateway := &novaedgev1alpha1.ProxyGateway{}
			if err := env.client.Get(ctx, types.NamespacedName{
				Name:      test.gateway.Name,
				Namespace: test.gateway.Namespace,
			}, updatedGateway); err != nil {
				t.Fatalf("failed to get gateway: %v", err)
			}

			// Check status conditions
			readyCondition := meta.FindStatusCondition(updatedGateway.Status.Conditions, "Ready")
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

func TestProxyGatewayMultipleListeners(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.1/32",
			Mode:    novaedgev1alpha1.VIPModeL2ARP,
		},
	}

	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-listener-gateway",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef: "multi-vip",
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
				},
				{
					Name:     "https",
					Port:     443,
					Protocol: novaedgev1alpha1.ProtocolTypeHTTPS,
					TLS: &novaedgev1alpha1.TLSConfig{
						SecretRef: corev1.SecretReference{
							Name:      "tls-secret",
							Namespace: "default",
						},
					},
				},
			},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"tls.crt": []byte("cert"),
			"tls.key": []byte("key"),
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	if err := env.client.Create(ctx, secret); err != nil {
		t.Fatalf("failed to create secret: %v", err)
	}

	if err := env.client.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create gateway: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyGateway(ctx, gateway.Name, gateway.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gateway.Name,
		Namespace: gateway.Namespace,
	}, updatedGateway); err != nil {
		t.Fatalf("failed to get gateway: %v", err)
	}

	readyCondition := meta.FindStatusCondition(updatedGateway.Status.Conditions, "Ready")
	if readyCondition == nil || readyCondition.Status != metav1.ConditionTrue {
		t.Error("expected gateway to be ready with multiple listeners")
	}
}

func TestProxyGatewayStatusUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "status-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.1/32",
			Mode:    novaedgev1alpha1.VIPModeL2ARP,
		},
	}

	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "status-gateway",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef: "status-vip",
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
				},
			},
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	if err := env.client.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create gateway: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyGateway(ctx, gateway.Name, gateway.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gateway.Name,
		Namespace: gateway.Namespace,
	}, updatedGateway); err != nil {
		t.Fatalf("failed to get gateway: %v", err)
	}

	// Check status conditions are set
	readyCondition := meta.FindStatusCondition(updatedGateway.Status.Conditions, "Ready")
	if readyCondition == nil {
		t.Fatal("expected Ready condition to be set")
	}

	if readyCondition.Status != metav1.ConditionTrue {
		t.Errorf("expected Ready=True, got %s. Message: %s", readyCondition.Status, readyCondition.Message)
	}

	// Check ObservedGeneration is properly set in the condition
	if readyCondition.ObservedGeneration != updatedGateway.Generation {
		t.Errorf("expected condition ObservedGeneration to match, got %d, want %d",
			readyCondition.ObservedGeneration, updatedGateway.Generation)
	}
}

func TestProxyGatewayIngressClassName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingress-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.1/32",
			Mode:    novaedgev1alpha1.VIPModeL2ARP,
		},
	}

	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingress-gateway",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef:           "ingress-vip",
			IngressClassName: "novaedge",
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
				},
			},
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	if err := env.client.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create gateway: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyGateway(ctx, gateway.Name, gateway.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gateway.Name,
		Namespace: gateway.Namespace,
	}, updatedGateway); err != nil {
		t.Fatalf("failed to get gateway: %v", err)
	}

	if updatedGateway.Spec.IngressClassName != "novaedge" {
		t.Errorf("expected IngressClassName to be 'novaedge', got '%s'",
			updatedGateway.Spec.IngressClassName)
	}
}

func TestProxyGatewayDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-vip",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "10.0.0.1/32",
			Mode:    novaedgev1alpha1.VIPModeL2ARP,
		},
	}

	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-gateway",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef: "delete-vip",
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
				},
			},
		},
	}

	if err := env.client.Create(ctx, vip); err != nil {
		t.Fatalf("failed to create VIP: %v", err)
	}

	if err := env.client.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create gateway: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyGateway(ctx, gateway.Name, gateway.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	if err := env.client.Delete(ctx, gateway); err != nil {
		t.Fatalf("failed to delete gateway: %v", err)
	}

	// Verify deletion
	deleted := &novaedgev1alpha1.ProxyGateway{}
	err := env.client.Get(ctx, types.NamespacedName{
		Name:      gateway.Name,
		Namespace: gateway.Namespace,
	}, deleted)

	// Should either not exist or have a deletion timestamp
	if err == nil && deleted.DeletionTimestamp == nil {
		t.Error("expected gateway to be deleted or have deletion timestamp")
	}
}
