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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestHTTPRouteReconcile(t *testing.T) {
	tests := []struct {
		name         string
		httpRoute    *gatewayv1.HTTPRoute
		gateway      *gatewayv1.Gateway
		gatewayClass *gatewayv1.GatewayClass
		service      *corev1.Service
		expectError  bool
		expectReady  bool
	}{
		{
			name: "valid HTTPRoute with single backend",
			httpRoute: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "test-gateway",
							},
						},
					},
					Rules: []gatewayv1.HTTPRouteRule{
						{
							Matches: []gatewayv1.HTTPRouteMatch{
								{
									Path: &gatewayv1.HTTPPathMatch{
										Type:  (*gatewayv1.PathMatchType)(strPtr(string(gatewayv1.PathMatchPathPrefix))),
										Value: strPtr("/api"),
									},
								},
							},
							BackendRefs: []gatewayv1.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: "backend-service",
											Port: (*gatewayv1.PortNumber)(int32Ptr(8080)),
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
							Name:     "http",
							Port:     80,
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
					Name:      "backend-service",
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
			name: "HTTPRoute with missing backend service",
			httpRoute: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-backend-route",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "test-gateway",
							},
						},
					},
					Rules: []gatewayv1.HTTPRouteRule{
						{
							BackendRefs: []gatewayv1.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: "nonexistent-service",
											Port: (*gatewayv1.PortNumber)(int32Ptr(8080)),
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
							Name:     "http",
							Port:     80,
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
			expectError: true,
			expectReady: false,
		},
		{
			name: "HTTPRoute with non-NovaEdge gateway",
			httpRoute: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-gateway-route",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "istio-gateway",
							},
						},
					},
					Rules: []gatewayv1.HTTPRouteRule{
						{
							BackendRefs: []gatewayv1.HTTPBackendRef{
								{
									BackendRef: gatewayv1.BackendRef{
										BackendObjectReference: gatewayv1.BackendObjectReference{
											Name: "backend-service",
											Port: (*gatewayv1.PortNumber)(int32Ptr(8080)),
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
					Name:      "istio-gateway",
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "istio",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: gatewayv1.HTTPProtocolType,
						},
					},
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "istio",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: "istio.io/gateway-controller",
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backend-service",
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
			expectReady: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			k8sClient := setupTestEnvironment(t)

			if test.gatewayClass != nil {
				if err := k8sClient.Create(ctx, test.gatewayClass); err != nil {
					t.Fatalf("failed to create GatewayClass: %v", err)
				}
			}

			if test.gateway != nil {
				if err := k8sClient.Create(ctx, test.gateway); err != nil {
					t.Fatalf("failed to create Gateway: %v", err)
				}
			}

			if test.service != nil {
				if err := k8sClient.Create(ctx, test.service); err != nil {
					t.Fatalf("failed to create Service: %v", err)
				}
			}

			if err := k8sClient.Create(ctx, test.httpRoute); err != nil {
				t.Fatalf("failed to create HTTPRoute: %v", err)
			}

			time.Sleep(500 * time.Millisecond)

			updatedRoute := &gatewayv1.HTTPRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      test.httpRoute.Name,
				Namespace: test.httpRoute.Namespace,
			}, updatedRoute); err != nil {
				t.Fatalf("failed to get HTTPRoute: %v", err)
			}

			if test.expectReady {
				if len(updatedRoute.Status.RouteStatus.Parents) == 0 {
					t.Error("expected parent status to be set")
				} else {
					for _, parentStatus := range updatedRoute.Status.RouteStatus.Parents {
						acceptedCond := meta.FindStatusCondition(parentStatus.Conditions, string(gatewayv1.RouteConditionAccepted))
						if acceptedCond == nil || acceptedCond.Status != metav1.ConditionTrue {
							t.Errorf("expected route to be accepted, got status: %v", acceptedCond)
						}
					}
				}
			}
		})
	}
}

func TestHTTPRouteProxyBackendCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := setupTestEnvironment(t)

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "novaedge",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "novaedge.io/gateway-controller",
		},
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "novaedge",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
				},
			},
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backend-service",
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

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name: "test-gateway",
					},
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: "backend-service",
									Port: (*gatewayv1.PortNumber)(int32Ptr(8080)),
								},
							},
						},
					},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, gatewayClass); err != nil {
		t.Fatalf("failed to create GatewayClass: %v", err)
	}

	if err := k8sClient.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create Gateway: %v", err)
	}

	if err := k8sClient.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := k8sClient.Create(ctx, httpRoute); err != nil {
		t.Fatalf("failed to create HTTPRoute: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Verify ProxyRoute was created
	proxyRoute := &novaedgev1alpha1.ProxyRoute{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      httpRoute.Name,
		Namespace: httpRoute.Namespace,
	}, proxyRoute); err != nil {
		t.Fatalf("expected ProxyRoute to be created: %v", err)
	}

	// Verify ProxyBackend was created
	backendName := GenerateProxyBackendName("backend-service", "default", 8080)
	proxyBackend := &novaedgev1alpha1.ProxyBackend{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      backendName,
		Namespace: "default",
	}, proxyBackend); err != nil {
		t.Fatalf("expected ProxyBackend to be created: %v", err)
	}

	if proxyBackend.Spec.ServiceRef == nil || proxyBackend.Spec.ServiceRef.Name != "backend-service" {
		t.Error("expected ProxyBackend to reference correct service")
	}
}

func TestHTTPRouteMultipleRules(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := setupTestEnvironment(t)

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "novaedge",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "novaedge.io/gateway-controller",
		},
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "novaedge",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
				},
			},
		},
	}

	service1 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-service",
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

	service2 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       3000,
					TargetPort: intstr.FromInt(3000),
				},
			},
		},
	}

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-rule-route",
			Namespace: "default",
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name: "test-gateway",
					},
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  (*gatewayv1.PathMatchType)(strPtr(string(gatewayv1.PathMatchPathPrefix))),
								Value: strPtr("/api"),
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: "api-service",
									Port: (*gatewayv1.PortNumber)(int32Ptr(8080)),
								},
							},
						},
					},
				},
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  (*gatewayv1.PathMatchType)(strPtr(string(gatewayv1.PathMatchPathPrefix))),
								Value: strPtr("/"),
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: "web-service",
									Port: (*gatewayv1.PortNumber)(int32Ptr(3000)),
								},
							},
						},
					},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, gatewayClass); err != nil {
		t.Fatalf("failed to create GatewayClass: %v", err)
	}

	if err := k8sClient.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create Gateway: %v", err)
	}

	if err := k8sClient.Create(ctx, service1); err != nil {
		t.Fatalf("failed to create Service 1: %v", err)
	}

	if err := k8sClient.Create(ctx, service2); err != nil {
		t.Fatalf("failed to create Service 2: %v", err)
	}

	if err := k8sClient.Create(ctx, httpRoute); err != nil {
		t.Fatalf("failed to create HTTPRoute: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Verify both ProxyBackends were created
	backend1Name := GenerateProxyBackendName("api-service", "default", 8080)
	backend2Name := GenerateProxyBackendName("web-service", "default", 3000)

	backend1 := &novaedgev1alpha1.ProxyBackend{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      backend1Name,
		Namespace: "default",
	}, backend1); err != nil {
		t.Fatalf("expected first ProxyBackend to be created: %v", err)
	}

	backend2 := &novaedgev1alpha1.ProxyBackend{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      backend2Name,
		Namespace: "default",
	}, backend2); err != nil {
		t.Fatalf("expected second ProxyBackend to be created: %v", err)
	}
}

func TestHTTPRouteDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := setupTestEnvironment(t)

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "novaedge",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "novaedge.io/gateway-controller",
		},
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "novaedge",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
				},
			},
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backend-service",
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

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-route",
			Namespace: "default",
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name: "test-gateway",
					},
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: "backend-service",
									Port: (*gatewayv1.PortNumber)(int32Ptr(8080)),
								},
							},
						},
					},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, gatewayClass); err != nil {
		t.Fatalf("failed to create GatewayClass: %v", err)
	}

	if err := k8sClient.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create Gateway: %v", err)
	}

	if err := k8sClient.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := k8sClient.Create(ctx, httpRoute); err != nil {
		t.Fatalf("failed to create HTTPRoute: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Verify ProxyRoute was created
	proxyRoute := &novaedgev1alpha1.ProxyRoute{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      httpRoute.Name,
		Namespace: httpRoute.Namespace,
	}, proxyRoute); err != nil {
		t.Fatalf("expected ProxyRoute to be created: %v", err)
	}

	// Delete HTTPRoute
	if err := k8sClient.Delete(ctx, httpRoute); err != nil {
		t.Fatalf("failed to delete HTTPRoute: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// ProxyRoute should be deleted (via owner reference)
	deletedRoute := &novaedgev1alpha1.ProxyRoute{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      httpRoute.Name,
		Namespace: httpRoute.Namespace,
	}, deletedRoute)

	if err == nil && deletedRoute.DeletionTimestamp == nil {
		t.Error("expected ProxyRoute to be deleted when HTTPRoute is deleted")
	}
}

