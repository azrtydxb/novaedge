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
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestTLSRouteReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1alpha2.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	tests := []struct {
		name        string
		tlsRoute    *gatewayv1alpha2.TLSRoute
		expectError bool
	}{
		{
			name: "valid TLSRoute",
			tlsRoute: &gatewayv1alpha2.TLSRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tls-route",
					Namespace: "default",
				},
				Spec: gatewayv1alpha2.TLSRouteSpec{
					CommonRouteSpec: gatewayv1alpha2.CommonRouteSpec{
						ParentRefs: []gatewayv1alpha2.ParentReference{
							{
								Name: "test-gateway",
							},
						},
					},
					Hostnames: []gatewayv1alpha2.Hostname{
						"example.com",
					},
					Rules: []gatewayv1alpha2.TLSRouteRule{
						{
							BackendRefs: []gatewayv1alpha2.BackendRef{
								{
									BackendObjectReference: gatewayv1alpha2.BackendObjectReference{
										Name: "tls-service",
										Port: int32Ptr(443),
									},
								},
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "TLSRoute with multiple hostnames",
			tlsRoute: &gatewayv1alpha2.TLSRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-hostname-tls-route",
					Namespace: "default",
				},
				Spec: gatewayv1alpha2.TLSRouteSpec{
					CommonRouteSpec: gatewayv1alpha2.CommonRouteSpec{
						ParentRefs: []gatewayv1alpha2.ParentReference{
							{
								Name: "test-gateway",
							},
						},
					},
					Hostnames: []gatewayv1alpha2.Hostname{
						"example.com",
						"api.example.com",
						"*.example.com",
					},
					Rules: []gatewayv1alpha2.TLSRouteRule{
						{
							BackendRefs: []gatewayv1alpha2.BackendRef{
								{
									BackendObjectReference: gatewayv1alpha2.BackendObjectReference{
										Name: "tls-service",
										Port: int32Ptr(443),
									},
								},
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "TLSRoute with multiple backends",
			tlsRoute: &gatewayv1alpha2.TLSRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-backend-tls-route",
					Namespace: "default",
				},
				Spec: gatewayv1alpha2.TLSRouteSpec{
					CommonRouteSpec: gatewayv1alpha2.CommonRouteSpec{
						ParentRefs: []gatewayv1alpha2.ParentReference{
							{
								Name: "test-gateway",
							},
						},
					},
					Hostnames: []gatewayv1alpha2.Hostname{
						"secure.example.com",
					},
					Rules: []gatewayv1alpha2.TLSRouteRule{
						{
							BackendRefs: []gatewayv1alpha2.BackendRef{
								{
									BackendObjectReference: gatewayv1alpha2.BackendObjectReference{
										Name: "tls-service-1",
										Port: int32Ptr(443),
									},
								},
								{
									BackendObjectReference: gatewayv1alpha2.BackendObjectReference{
										Name: "tls-service-2",
										Port: int32Ptr(443),
									},
								},
							},
						},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.tlsRoute).
				WithStatusSubresource(&gatewayv1alpha2.TLSRoute{}, &novaedgev1alpha1.ProxyRoute{}).
				Build()

			reconciler := &TLSRouteReconciler{
				Client: k8sClient,
				Scheme: scheme,
			}

			ctx := context.Background()
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.tlsRoute.Name,
					Namespace: tt.tlsRoute.Namespace,
				},
			})

			if tt.expectError && err == nil {
				t.Errorf("expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestTLSRouteReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1alpha2.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &TLSRouteReconciler{
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
		t.Errorf("unexpected error for non-existent TLSRoute: %v", err)
	}
}

func TestTLSRouteDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1alpha2.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	now := metav1.Now()
	tlsRoute := &gatewayv1alpha2.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleted-tls-route",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{"novaedge.io/tlsroute-finalizer"},
		},
		Spec: gatewayv1alpha2.TLSRouteSpec{
			CommonRouteSpec: gatewayv1alpha2.CommonRouteSpec{
				ParentRefs: []gatewayv1alpha2.ParentReference{
					{
						Name: "test-gateway",
					},
				},
			},
		},
	}

	proxyRoute := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deleted-tls-route",
			Namespace: "default",
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(tlsRoute, proxyRoute).
		WithStatusSubresource(&gatewayv1alpha2.TLSRoute{}).
		Build()

	reconciler := &TLSRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      tlsRoute.Name,
			Namespace: tlsRoute.Namespace,
		},
	})

	if err != nil {
		t.Errorf("unexpected error during deletion: %v", err)
	}

	// Verify proxy route was deleted
	deletedRoute := &novaedgev1alpha1.ProxyRoute{}
	err = k8sClient.Get(ctx, types.NamespacedName{Name: "deleted-tls-route", Namespace: "default"}, deletedRoute)
	if err == nil {
		t.Error("expected ProxyRoute to be deleted")
	}
}

func TestTLSRouteUpdateStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1alpha2.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	tlsRoute := &gatewayv1alpha2.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "status-test-route",
			Namespace: "default",
		},
		Spec: gatewayv1alpha2.TLSRouteSpec{
			CommonRouteSpec: gatewayv1alpha2.CommonRouteSpec{
				ParentRefs: []gatewayv1alpha2.ParentReference{
					{Name: "test-gateway"},
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(tlsRoute).
		WithStatusSubresource(&gatewayv1alpha2.TLSRoute{}).
		Build()

	reconciler := &TLSRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	condition := metav1.Condition{
		Type:               "Accepted",
		Status:             metav1.ConditionTrue,
		Reason:             "Accepted",
		Message:            "TLSRoute accepted",
		ObservedGeneration: 1,
		LastTransitionTime: metav1.Now(),
	}

	_, err := reconciler.updateRouteStatus(ctx, tlsRoute, condition)
	if err != nil {
		t.Errorf("unexpected error updating status: %v", err)
	}
}

func TestTLSRouteHandleDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1alpha2.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	now := metav1.Now()
	tlsRoute := &gatewayv1alpha2.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "handle-delete-route",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{"novaedge.io/tlsroute-finalizer"},
		},
	}

	proxyRoute := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "handle-delete-route",
			Namespace: "default",
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(tlsRoute, proxyRoute).
		Build()

	reconciler := &TLSRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.handleDeletion(ctx, tlsRoute)
	if err != nil {
		t.Errorf("unexpected error handling deletion: %v", err)
	}
}

func TestTLSRouteWithExistingProxyRoute(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1alpha2.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	tlsRoute := &gatewayv1alpha2.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-proxy-route",
			Namespace: "default",
		},
		Spec: gatewayv1alpha2.TLSRouteSpec{
			CommonRouteSpec: gatewayv1alpha2.CommonRouteSpec{
				ParentRefs: []gatewayv1alpha2.ParentReference{
					{Name: "test-gateway"},
				},
			},
			Hostnames: []gatewayv1alpha2.Hostname{
				"example.com",
			},
			Rules: []gatewayv1alpha2.TLSRouteRule{
				{
					BackendRefs: []gatewayv1alpha2.BackendRef{
						{
							BackendObjectReference: gatewayv1alpha2.BackendObjectReference{
								Name: "tls-service",
								Port: int32Ptr(443),
							},
						},
					},
				},
			},
		},
	}

	existingProxyRoute := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-proxy-route",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyRouteSpec{
			Hostnames: []string{"old.example.com"},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(tlsRoute, existingProxyRoute).
		WithStatusSubresource(&gatewayv1alpha2.TLSRoute{}, &novaedgev1alpha1.ProxyRoute{}).
		Build()

	reconciler := &TLSRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      tlsRoute.Name,
			Namespace: tlsRoute.Namespace,
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTLSRouteWithWildcardHostname(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1alpha2.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	tlsRoute := &gatewayv1alpha2.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wildcard-tls-route",
			Namespace: "default",
		},
		Spec: gatewayv1alpha2.TLSRouteSpec{
			CommonRouteSpec: gatewayv1alpha2.CommonRouteSpec{
				ParentRefs: []gatewayv1alpha2.ParentReference{
					{Name: "test-gateway"},
				},
			},
			Hostnames: []gatewayv1alpha2.Hostname{
				"*.example.com",
			},
			Rules: []gatewayv1alpha2.TLSRouteRule{
				{
					BackendRefs: []gatewayv1alpha2.BackendRef{
						{
							BackendObjectReference: gatewayv1alpha2.BackendObjectReference{
								Name: "wildcard-service",
								Port: int32Ptr(443),
							},
						},
					},
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(tlsRoute).
		WithStatusSubresource(&gatewayv1alpha2.TLSRoute{}, &novaedgev1alpha1.ProxyRoute{}).
		Build()

	reconciler := &TLSRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      tlsRoute.Name,
			Namespace: tlsRoute.Namespace,
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
