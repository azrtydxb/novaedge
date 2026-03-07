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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

func TestProxyWANPolicyReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	tests := []struct {
		name    string
		policy  *novaedgev1alpha1.ProxyWANPolicy
		wantErr bool
	}{
		{
			name: "valid WAN policy with lowest latency strategy",
			policy: &novaedgev1alpha1.ProxyWANPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-wan-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyWANPolicySpec{
					PathSelection: novaedgev1alpha1.WANPathSelection{
						Strategy: novaedgev1alpha1.WANStrategyLowestLatency,
						Failover: true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "WAN policy with highest bandwidth strategy",
			policy: &novaedgev1alpha1.ProxyWANPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bandwidth-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyWANPolicySpec{
					PathSelection: novaedgev1alpha1.WANPathSelection{
						Strategy: novaedgev1alpha1.WANStrategyHighestBandwidth,
						Failover: true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "WAN policy with match criteria",
			policy: &novaedgev1alpha1.ProxyWANPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "matched-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyWANPolicySpec{
					Match: novaedgev1alpha1.WANPolicyMatch{
						Hosts: []string{"api.example.com", "app.example.com"},
						Paths: []string{"/api", "/v1"},
						Headers: map[string]string{
							"X-Tenant": "premium",
						},
					},
					PathSelection: novaedgev1alpha1.WANPathSelection{
						Strategy: novaedgev1alpha1.WANStrategyMostReliable,
						Failover: true,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "WAN policy with DSCP class",
			policy: &novaedgev1alpha1.ProxyWANPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dscp-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyWANPolicySpec{
					PathSelection: novaedgev1alpha1.WANPathSelection{
						Strategy:  novaedgev1alpha1.WANStrategyLowestCost,
						Failover:  false,
						DSCPClass: "EF",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.policy).
				WithStatusSubresource(&novaedgev1alpha1.ProxyWANPolicy{}).
				Build()

			reconciler := &ProxyWANPolicyReconciler{
				Client: k8sClient,
				Scheme: scheme,
			}

			ctx := context.Background()
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.policy.Name,
					Namespace: tt.policy.Namespace,
				},
			})

			if (err != nil) != tt.wantErr {
				t.Errorf("ProxyWANPolicyReconciler.Reconcile() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify status was updated
				updatedPolicy := &novaedgev1alpha1.ProxyWANPolicy{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      tt.policy.Name,
					Namespace: tt.policy.Namespace,
				}, updatedPolicy); err != nil {
					t.Errorf("failed to get updated policy: %v", err)
				}

				if updatedPolicy.Status.Phase != phaseActive {
					t.Errorf("expected phase Active, got %s", updatedPolicy.Status.Phase)
				}
			}
		})
	}
}

func TestProxyWANPolicyReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &ProxyWANPolicyReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent",
			Namespace: "default",
		},
	})

	if err != nil {
		t.Errorf("unexpected error for non-existent ProxyWANPolicy: %v", err)
	}
}

func TestProxyWANPolicyStatusUpdate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	policy := &novaedgev1alpha1.ProxyWANPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "status-test-policy",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyWANPolicySpec{
			PathSelection: novaedgev1alpha1.WANPathSelection{
				Strategy: novaedgev1alpha1.WANStrategyLowestLatency,
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(policy).
		WithStatusSubresource(&novaedgev1alpha1.ProxyWANPolicy{}).
		Build()

	reconciler := &ProxyWANPolicyReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      policy.Name,
			Namespace: policy.Namespace,
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify the status conditions were set
	updatedPolicy := &novaedgev1alpha1.ProxyWANPolicy{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      policy.Name,
		Namespace: policy.Namespace,
	}, updatedPolicy); err != nil {
		t.Errorf("failed to get updated policy: %v", err)
	}

	if len(updatedPolicy.Status.Conditions) == 0 {
		t.Error("expected conditions to be set")
	}

	foundReady := false
	for _, cond := range updatedPolicy.Status.Conditions {
		if cond.Type == "Ready" {
			foundReady = true
			if cond.Status != metav1.ConditionTrue {
				t.Errorf("expected Ready condition to be True, got %s", cond.Status)
			}
		}
	}

	if !foundReady {
		t.Error("expected Ready condition to be present")
	}
}

func TestProxyWANPolicyMultiplePolicies(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	policy1 := &novaedgev1alpha1.ProxyWANPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wan-policy-1",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyWANPolicySpec{
			PathSelection: novaedgev1alpha1.WANPathSelection{
				Strategy: novaedgev1alpha1.WANStrategyLowestLatency,
			},
		},
	}

	policy2 := &novaedgev1alpha1.ProxyWANPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wan-policy-2",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyWANPolicySpec{
			PathSelection: novaedgev1alpha1.WANPathSelection{
				Strategy: novaedgev1alpha1.WANStrategyHighestBandwidth,
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(policy1, policy2).
		WithStatusSubresource(&novaedgev1alpha1.ProxyWANPolicy{}).
		Build()

	reconciler := &ProxyWANPolicyReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()

	// Reconcile first policy
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      policy1.Name,
			Namespace: policy1.Namespace,
		},
	})
	if err != nil {
		t.Errorf("unexpected error reconciling policy1: %v", err)
	}

	// Reconcile second policy
	_, err = reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      policy2.Name,
			Namespace: policy2.Namespace,
		},
	})
	if err != nil {
		t.Errorf("unexpected error reconciling policy2: %v", err)
	}
}

func TestProxyWANPolicyWithLabels(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	policy := &novaedgev1alpha1.ProxyWANPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "labeled-wan-policy",
			Namespace: "default",
			Labels: map[string]string{
				"environment": "production",
				"region":      "us-east",
			},
		},
		Spec: novaedgev1alpha1.ProxyWANPolicySpec{
			PathSelection: novaedgev1alpha1.WANPathSelection{
				Strategy: novaedgev1alpha1.WANStrategyMostReliable,
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(policy).
		WithStatusSubresource(&novaedgev1alpha1.ProxyWANPolicy{}).
		Build()

	reconciler := &ProxyWANPolicyReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      policy.Name,
			Namespace: policy.Namespace,
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
