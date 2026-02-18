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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestGRPCRouteReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	tests := []struct {
		name         string
		grpcRoute    *gatewayv1.GRPCRoute
		gateway      *gatewayv1.Gateway
		gatewayClass *gatewayv1.GatewayClass
		service      *corev1.Service
		expectError  bool
	}{
		{
			name: "valid GRPCRoute with single backend",
			grpcRoute: &gatewayv1.GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-grpc-route",
					Namespace: "default",
				},
				Spec: gatewayv1.GRPCRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "test-gateway",
							},
						},
					},
					Rules: []gatewayv1.GRPCRouteRule{
						{
							BackendRefs: []gatewayv1.GRPCBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: "grpc-service",
											Port: int32Ptr(9090),
										},
									},
								},
							},
						},
					},
				},
			},
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "novaedge",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "grpc",
							Port:     9090,
							Protocol: gatewayv1.HTTPProtocolType,
						},
					},
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "novaedge",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: "novaedge.io/gateway-controller",
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "grpc-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:       9090,
							TargetPort: intstr.FromInt(9090),
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "GRPCRoute with missing backend service",
			grpcRoute: &gatewayv1.GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-backend-route",
					Namespace: "default",
				},
				Spec: gatewayv1.GRPCRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "test-gateway",
							},
						},
					},
					Rules: []gatewayv1.GRPCRouteRule{
						{
							BackendRefs: []gatewayv1.GRPCBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: "nonexistent-service",
											Port: int32Ptr(9090),
										},
									},
								},
							},
						},
					},
				},
			},
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "novaedge",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "grpc",
							Port:     9090,
							Protocol: gatewayv1.HTTPProtocolType,
						},
					},
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "novaedge",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: "novaedge.io/gateway-controller",
				},
			},
			service:     nil,
			expectError: false, // Error is handled internally via status update
		},
		{
			name: "GRPCRoute without NovaEdge gateway",
			grpcRoute: &gatewayv1.GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-gateway-route",
					Namespace: "default",
				},
				Spec: gatewayv1.GRPCRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "other-gateway",
							},
						},
					},
					Rules: []gatewayv1.GRPCRouteRule{
						{
							BackendRefs: []gatewayv1.GRPCBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: "grpc-service",
											Port: int32Ptr(9090),
										},
									},
								},
							},
						},
					},
				},
			},
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-gateway",
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "other-class",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "grpc",
							Port:     9090,
							Protocol: gatewayv1.HTTPProtocolType,
						},
					},
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-class",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: "other.io/controller",
				},
			},
			service:     nil,
			expectError: false, // No error, just ignored
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build initial objects
			objs := []runtime.Object{tt.grpcRoute}
			if tt.gateway != nil {
				objs = append(objs, tt.gateway)
			}
			if tt.gatewayClass != nil {
				objs = append(objs, tt.gatewayClass)
			}
			if tt.service != nil {
				objs = append(objs, tt.service)
			}

			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(
					&novaedgev1alpha1.ProxyGateway{},
					&novaedgev1alpha1.ProxyBackend{},
					&novaedgev1alpha1.ProxyRoute{},
					&gatewayv1.GRPCRoute{},
				).
				WithRuntimeObjects(objs...).
				Build()

			reconciler := &GRPCRouteReconciler{
				Client: k8sClient,
				Scheme: scheme,
			}

			ctx := context.Background()
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.grpcRoute.Name,
					Namespace: tt.grpcRoute.Namespace,
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

func TestGRPCRouteReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &GRPCRouteReconciler{
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
		t.Errorf("unexpected error for non-existent GRPCRoute: %v", err)
	}
}

func TestGRPCRouteDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	now := metav1.Now()
	grpcRoute := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleted-grpc-route",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{"novaedge.io/grpcroute-finalizer"},
		},
		Spec: gatewayv1.GRPCRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name: "test-gateway",
					},
				},
			},
		},
	}

	proxyRoute := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grpc-deleted-grpc-route",
			Namespace: "default",
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(grpcRoute, proxyRoute).
		WithStatusSubresource(&gatewayv1.GRPCRoute{}).
		Build()

	reconciler := &GRPCRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      grpcRoute.Name,
			Namespace: grpcRoute.Namespace,
		},
	})

	if err != nil {
		t.Errorf("unexpected error during deletion: %v", err)
	}
}

func TestReconcileGRPCBackends(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grpc-backend-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       9090,
					TargetPort: intstr.FromInt(9090),
				},
			},
		},
	}

	grpcRoute := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-grpc-route",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: gatewayv1.GRPCRouteSpec{
			Rules: []gatewayv1.GRPCRouteRule{
				{
					BackendRefs: []gatewayv1.GRPCBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: "grpc-backend-service",
									Port: int32Ptr(9090),
								},
							},
						},
					},
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(service).
		Build()

	reconciler := &GRPCRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	err := reconciler.reconcileGRPCBackends(ctx, grpcRoute)

	if err != nil {
		t.Errorf("unexpected error reconciling gRPC backends: %v", err)
	}

	// Verify ProxyBackend was created
	backendName := GenerateProxyBackendName("grpc-backend-service", "default", 9090)
	proxyBackend := &novaedgev1alpha1.ProxyBackend{}
	err = k8sClient.Get(ctx, types.NamespacedName{Name: backendName, Namespace: "default"}, proxyBackend)
	if err != nil {
		t.Errorf("expected ProxyBackend to be created: %v", err)
	}
}

func TestReconcileGRPCBackendsMissingService(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	grpcRoute := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-grpc-route",
			Namespace: "default",
		},
		Spec: gatewayv1.GRPCRouteSpec{
			Rules: []gatewayv1.GRPCRouteRule{
				{
					BackendRefs: []gatewayv1.GRPCBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: "nonexistent-service",
									Port: int32Ptr(9090),
								},
							},
						},
					},
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &GRPCRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	err := reconciler.reconcileGRPCBackends(ctx, grpcRoute)

	if err == nil {
		t.Error("expected error for missing service but got nil")
	}
}

func TestUpdateGRPCRouteStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	grpcRoute := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-grpc-route",
			Namespace: "default",
		},
		Spec: gatewayv1.GRPCRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{Name: "test-gateway"},
				},
			},
		},
		Status: gatewayv1.GRPCRouteStatus{
			RouteStatus: gatewayv1.RouteStatus{
				Parents: []gatewayv1.RouteParentStatus{
					{
						ParentRef: gatewayv1.ParentReference{Name: "test-gateway"},
					},
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(grpcRoute).
		WithStatusSubresource(&gatewayv1.GRPCRoute{}).
		Build()

	reconciler := &GRPCRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	condition := metav1.Condition{
		Type:               "Accepted",
		Status:             metav1.ConditionTrue,
		Reason:             "Accepted",
		Message:            "GRPCRoute accepted",
		ObservedGeneration: 1,
		LastTransitionTime: metav1.Now(),
	}

	_, err := reconciler.updateGRPCRouteStatus(ctx, grpcRoute, condition)
	if err != nil {
		t.Errorf("unexpected error updating status: %v", err)
	}
}

func TestHandleGRPCRouteDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	now := metav1.Now()
	grpcRoute := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleted-grpc-route",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{"novaedge.io/grpcroute-finalizer"},
		},
	}

	proxyRoute := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grpc-deleted-grpc-route",
			Namespace: "default",
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(grpcRoute, proxyRoute).
		Build()

	reconciler := &GRPCRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.handleGRPCRouteDeletion(ctx, grpcRoute)
	if err != nil {
		t.Errorf("unexpected error handling deletion: %v", err)
	}

	// Verify proxy route was deleted
	deletedRoute := &novaedgev1alpha1.ProxyRoute{}
	err = k8sClient.Get(ctx, types.NamespacedName{Name: "grpc-deleted-grpc-route", Namespace: "default"}, deletedRoute)
	if err == nil {
		t.Error("expected ProxyRoute to be deleted")
	}
}

func TestGRPCRouteWithMultipleBackends(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	service1 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grpc-service-1",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Port: 9090, TargetPort: intstr.FromInt(9090)},
			},
		},
	}

	service2 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grpc-service-2",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Port: 9091, TargetPort: intstr.FromInt(9091)},
			},
		},
	}

	grpcRoute := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-backend-route",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: gatewayv1.GRPCRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{Name: "test-gateway"},
				},
			},
			Rules: []gatewayv1.GRPCRouteRule{
				{
					BackendRefs: []gatewayv1.GRPCBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: "grpc-service-1",
									Port: int32Ptr(9090),
								},
							},
						},
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: "grpc-service-2",
									Port: int32Ptr(9091),
								},
							},
						},
					},
				},
			},
		},
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "novaedge",
		},
	}

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "novaedge",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "novaedge.io/gateway-controller",
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(grpcRoute, gateway, gatewayClass, service1, service2).
		WithStatusSubresource(&gatewayv1.GRPCRoute{}, &novaedgev1alpha1.ProxyRoute{}).
		Build()

	reconciler := &GRPCRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      grpcRoute.Name,
			Namespace: grpcRoute.Namespace,
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGRPCRouteWithNamespaceRef(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("failed to add gateway API scheme: %v", err)
	}

	otherNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "other-namespace",
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grpc-service",
			Namespace: "other-namespace",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Port: 9090, TargetPort: intstr.FromInt(9090)},
			},
		},
	}

	grpcRoute := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cross-ns-route",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: gatewayv1.GRPCRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{Name: "test-gateway"},
				},
			},
			Rules: []gatewayv1.GRPCRouteRule{
				{
					BackendRefs: []gatewayv1.GRPCBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name:      "grpc-service",
									Namespace: (*gatewayv1.Namespace)(strPtr("other-namespace")),
									Port:      int32Ptr(9090),
								},
							},
						},
					},
				},
			},
		},
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "novaedge",
		},
	}

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "novaedge",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "novaedge.io/gateway-controller",
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(grpcRoute, gateway, gatewayClass, service, otherNS).
		WithStatusSubresource(&gatewayv1.GRPCRoute{}, &novaedgev1alpha1.ProxyRoute{}).
		Build()

	reconciler := &GRPCRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      grpcRoute.Name,
			Namespace: grpcRoute.Namespace,
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
