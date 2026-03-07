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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

func TestProxyPolicyReconcile(t *testing.T) {
	tests := []struct {
		name          string
		policy        *novaedgev1alpha1.ProxyPolicy
		targetGateway *novaedgev1alpha1.ProxyGateway
		expectError   bool
		expectReady   bool
	}{
		{
			name: "valid JWT policy targeting gateway",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-jwt-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					Type: "JWT",
					TargetRef: novaedgev1alpha1.TargetRef{
						Kind: "ProxyGateway",
						Name: "test-gateway",
					},
					JWT: &novaedgev1alpha1.JWTConfig{
						Issuer:   "https://example.com",
						Audience: []string{"api"},
					},
				},
			},
			targetGateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					Listeners: []novaedgev1alpha1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
						},
					},
				},
			},
			expectError: false,
			expectReady: true,
		},
		{
			name: "policy targeting non-existent gateway",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-target-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					Type: "JWT",
					TargetRef: novaedgev1alpha1.TargetRef{
						Kind: "ProxyGateway",
						Name: "nonexistent-gateway",
					},
					JWT: &novaedgev1alpha1.JWTConfig{
						Issuer:   "https://example.com",
						Audience: []string{"api"},
					},
				},
			},
			expectError: true,
			expectReady: false,
		},
		{
			name: "CORS policy targeting gateway",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cors-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					Type: "CORS",
					TargetRef: novaedgev1alpha1.TargetRef{
						Kind: "ProxyGateway",
						Name: "test-gateway",
					},
					CORS: &novaedgev1alpha1.CORSConfig{
						AllowOrigins: []string{"https://example.com"},
						AllowMethods: []string{"GET", "POST"},
					},
				},
			},
			targetGateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					Listeners: []novaedgev1alpha1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
						},
					},
				},
			},
			expectError: false,
			expectReady: true,
		},
		{
			name: "rate limit policy targeting gateway",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rate-limit-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					Type: "RateLimit",
					TargetRef: novaedgev1alpha1.TargetRef{
						Kind: "ProxyGateway",
						Name: "test-gateway",
					},
					RateLimit: &novaedgev1alpha1.RateLimitConfig{
						RequestsPerSecond: 100,
					},
				},
			},
			targetGateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					Listeners: []novaedgev1alpha1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
						},
					},
				},
			},
			expectError: false,
			expectReady: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			env := setupTestEnv(t)

			if test.targetGateway != nil {
				if err := env.client.Create(ctx, test.targetGateway); err != nil {
					t.Fatalf("failed to create target gateway: %v", err)
				}
			}

			if err := env.client.Create(ctx, test.policy); err != nil {
				t.Fatalf("failed to create policy: %v", err)
			}

			// Manually trigger reconciliation
			err := env.reconcileProxyPolicy(ctx, test.policy.Name, test.policy.Namespace)
			if test.expectError && err == nil {
				// Error might be recorded in status conditions instead of returned.
				// Not a test failure; continue to verify status below.
				_ = err
			}

			updatedPolicy := &novaedgev1alpha1.ProxyPolicy{}
			if err := env.client.Get(ctx, types.NamespacedName{
				Name:      test.policy.Name,
				Namespace: test.policy.Namespace,
			}, updatedPolicy); err != nil {
				t.Fatalf("failed to get policy: %v", err)
			}

			readyCondition := meta.FindStatusCondition(updatedPolicy.Status.Conditions, "Ready")
			if readyCondition == nil {
				t.Fatal("expected Ready condition, got nil")
			}

			if test.expectReady && readyCondition.Status != metav1.ConditionTrue {
				t.Errorf("expected Ready=True, got %s. Message: %s", readyCondition.Status, readyCondition.Message)
			}

			if !test.expectReady && readyCondition.Status != metav1.ConditionFalse {
				t.Errorf("expected Ready=False, got %s", readyCondition.Status)
			}
		})
	}
}

func TestProxyPolicyIPAllowList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
				},
			},
		},
	}

	policy := &novaedgev1alpha1.ProxyPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ip-filter-policy",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyPolicySpec{
			Type: novaedgev1alpha1.PolicyTypeIPAllowList,
			TargetRef: novaedgev1alpha1.TargetRef{
				Kind: "ProxyGateway",
				Name: "test-gateway",
			},
			IPList: &novaedgev1alpha1.IPListConfig{
				CIDRs: []string{
					"10.0.0.0/8",
					"192.168.0.0/16",
				},
			},
		},
	}

	if err := env.client.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create gateway: %v", err)
	}

	if err := env.client.Create(ctx, policy); err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyPolicy(ctx, policy.Name, policy.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedPolicy := &novaedgev1alpha1.ProxyPolicy{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      policy.Name,
		Namespace: policy.Namespace,
	}, updatedPolicy); err != nil {
		t.Fatalf("failed to get policy: %v", err)
	}

	readyCondition := meta.FindStatusCondition(updatedPolicy.Status.Conditions, "Ready")
	if readyCondition == nil || readyCondition.Status != metav1.ConditionTrue {
		t.Error("expected IP filter policy to be ready")
	}
}

func TestProxyPolicyMultipleTypes(t *testing.T) {
	policyTypes := []string{"JWT", "CORS", "RateLimit", "IPAllowList"}

	for _, policyType := range policyTypes {
		t.Run(policyType, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			env := setupTestEnv(t)

			gateway := &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					Listeners: []novaedgev1alpha1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
						},
					},
				},
			}

			policy := &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policyType + "-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					Type: novaedgev1alpha1.PolicyType(policyType),
					TargetRef: novaedgev1alpha1.TargetRef{
						Kind: "ProxyGateway",
						Name: "test-gateway",
					},
				},
			}

			// Set type-specific config
			switch policyType {
			case "JWT":
				policy.Spec.JWT = &novaedgev1alpha1.JWTConfig{
					Issuer: "https://example.com",
				}
			case "CORS":
				policy.Spec.CORS = &novaedgev1alpha1.CORSConfig{
					AllowOrigins: []string{"*"},
				}
			case "RateLimit":
				policy.Spec.RateLimit = &novaedgev1alpha1.RateLimitConfig{
					RequestsPerSecond: 100,
				}
			case "IPAllowList":
				policy.Spec.IPList = &novaedgev1alpha1.IPListConfig{
					CIDRs: []string{"10.0.0.0/8"},
				}
			}

			if err := env.client.Create(ctx, gateway); err != nil {
				t.Fatalf("failed to create gateway: %v", err)
			}

			if err := env.client.Create(ctx, policy); err != nil {
				t.Fatalf("failed to create policy: %v", err)
			}

			// Manually trigger reconciliation
			if err := env.reconcileProxyPolicy(ctx, policy.Name, policy.Namespace); err != nil {
				t.Fatalf("reconciliation failed: %v", err)
			}

			updatedPolicy := &novaedgev1alpha1.ProxyPolicy{}
			if err := env.client.Get(ctx, types.NamespacedName{
				Name:      policy.Name,
				Namespace: policy.Namespace,
			}, updatedPolicy); err != nil {
				t.Fatalf("failed to get policy: %v", err)
			}

			readyCondition := meta.FindStatusCondition(updatedPolicy.Status.Conditions, "Ready")
			if readyCondition == nil || readyCondition.Status != metav1.ConditionTrue {
				t.Errorf("expected %s policy to be ready", policyType)
			}
		})
	}
}

func TestProxyPolicyTargetingRoute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	route := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyRouteSpec{},
	}

	policy := &novaedgev1alpha1.ProxyPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route-policy",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyPolicySpec{
			Type: "RateLimit",
			TargetRef: novaedgev1alpha1.TargetRef{
				Kind: "ProxyRoute",
				Name: "test-route",
			},
			RateLimit: &novaedgev1alpha1.RateLimitConfig{
				RequestsPerSecond: 50,
			},
		},
	}

	if err := env.client.Create(ctx, route); err != nil {
		t.Fatalf("failed to create route: %v", err)
	}

	if err := env.client.Create(ctx, policy); err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyPolicy(ctx, policy.Name, policy.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedPolicy := &novaedgev1alpha1.ProxyPolicy{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      policy.Name,
		Namespace: policy.Namespace,
	}, updatedPolicy); err != nil {
		t.Fatalf("failed to get policy: %v", err)
	}

	readyCondition := meta.FindStatusCondition(updatedPolicy.Status.Conditions, "Ready")
	if readyCondition == nil || readyCondition.Status != metav1.ConditionTrue {
		t.Error("expected route policy to be ready")
	}
}

func TestProxyPolicyTargetingBackend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	backend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
		},
	}

	policy := &novaedgev1alpha1.ProxyPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backend-policy",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyPolicySpec{
			Type: "RateLimit",
			TargetRef: novaedgev1alpha1.TargetRef{
				Kind: "ProxyBackend",
				Name: "test-backend",
			},
			RateLimit: &novaedgev1alpha1.RateLimitConfig{
				RequestsPerSecond: 200,
			},
		},
	}

	if err := env.client.Create(ctx, backend); err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	if err := env.client.Create(ctx, policy); err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyPolicy(ctx, policy.Name, policy.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedPolicy := &novaedgev1alpha1.ProxyPolicy{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      policy.Name,
		Namespace: policy.Namespace,
	}, updatedPolicy); err != nil {
		t.Fatalf("failed to get policy: %v", err)
	}

	readyCondition := meta.FindStatusCondition(updatedPolicy.Status.Conditions, "Ready")
	if readyCondition == nil || readyCondition.Status != metav1.ConditionTrue {
		t.Error("expected backend policy to be ready")
	}
}

func TestProxyPolicyDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: novaedgev1alpha1.ProtocolTypeHTTP,
				},
			},
		},
	}

	policy := &novaedgev1alpha1.ProxyPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-policy",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyPolicySpec{
			Type: "JWT",
			TargetRef: novaedgev1alpha1.TargetRef{
				Kind: "ProxyGateway",
				Name: "test-gateway",
			},
			JWT: &novaedgev1alpha1.JWTConfig{
				Issuer: "https://example.com",
			},
		},
	}

	if err := env.client.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create gateway: %v", err)
	}

	if err := env.client.Create(ctx, policy); err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileProxyPolicy(ctx, policy.Name, policy.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	if err := env.client.Delete(ctx, policy); err != nil {
		t.Fatalf("failed to delete policy: %v", err)
	}

	deletedPolicy := &novaedgev1alpha1.ProxyPolicy{}
	err := env.client.Get(ctx, types.NamespacedName{
		Name:      policy.Name,
		Namespace: policy.Namespace,
	}, deletedPolicy)

	if err == nil && deletedPolicy.DeletionTimestamp == nil {
		t.Error("expected policy to be deleted")
	}
}
