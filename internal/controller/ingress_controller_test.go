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
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestIngressReconcile(t *testing.T) {
	tests := []struct {
		name          string
		ingress       *networkingv1.Ingress
		service       *corev1.Service
		expectError   bool
		expectCreated bool
	}{
		{
			name: "valid ingress with HTTP",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ingress",
					Namespace: "default",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: strPtr("novaedge"),
					Rules: []networkingv1.IngressRule{
						{
							Host: "example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/",
											PathType: (*networkingv1.PathType)(strPtr(string(networkingv1.PathTypePrefix))),
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "backend-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
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
			expectError:   false,
			expectCreated: true,
		},
		{
			name: "ingress with non-novaedge class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-ingress",
					Namespace: "default",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: strPtr("nginx"),
					Rules: []networkingv1.IngressRule{
						{
							Host: "example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/",
											PathType: (*networkingv1.PathType)(strPtr(string(networkingv1.PathTypePrefix))),
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "backend-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
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
			expectError:   false,
			expectCreated: false,
		},
		{
			name: "ingress with HTTPS",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "https-ingress",
					Namespace: "default",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: strPtr("novaedge"),
					TLS: []networkingv1.IngressTLS{
						{
							Hosts:      []string{"example.com"},
							SecretName: "tls-secret",
						},
					},
					Rules: []networkingv1.IngressRule{
						{
							Host: "example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/",
											PathType: (*networkingv1.PathType)(strPtr(string(networkingv1.PathTypePrefix))),
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "backend-service",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
										},
									},
								},
							},
						},
					},
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
			expectError:   false,
			expectCreated: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			k8sClient := setupTestEnvironment(t)

			if test.service != nil {
				if err := k8sClient.Create(ctx, test.service); err != nil {
					t.Fatalf("failed to create Service: %v", err)
				}
			}

			if err := k8sClient.Create(ctx, test.ingress); err != nil {
				t.Fatalf("failed to create Ingress: %v", err)
			}

			time.Sleep(500 * time.Millisecond)

			if test.expectCreated {
				// Check if ProxyGateway was created
				proxyGateway := &novaedgev1alpha1.ProxyGateway{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      test.ingress.Name,
					Namespace: test.ingress.Namespace,
				}, proxyGateway)

				if err != nil {
					t.Errorf("expected ProxyGateway to be created, but got error: %v", err)
				}
			}
		})
	}
}

func TestIngressProxyResourceCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := setupTestEnvironment(t)

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

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: "default",
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: strPtr("novaedge"),
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: (*networkingv1.PathType)(strPtr(string(networkingv1.PathTypePrefix))),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "backend-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 8080,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := k8sClient.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Check ProxyGateway creation
	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      ingress.Name,
		Namespace: ingress.Namespace,
	}, proxyGateway); err != nil {
		t.Fatalf("expected ProxyGateway to be created: %v", err)
	}

	// Check ProxyRoute creation
	proxyRouteList := &novaedgev1alpha1.ProxyRouteList{}
	if err := k8sClient.List(ctx, proxyRouteList); err != nil {
		t.Fatalf("failed to list ProxyRoutes: %v", err)
	}

	if len(proxyRouteList.Items) == 0 {
		t.Error("expected at least one ProxyRoute to be created")
	}

	// Check ProxyBackend creation
	proxyBackendList := &novaedgev1alpha1.ProxyBackendList{}
	if err := k8sClient.List(ctx, proxyBackendList); err != nil {
		t.Fatalf("failed to list ProxyBackends: %v", err)
	}

	if len(proxyBackendList.Items) == 0 {
		t.Error("expected at least one ProxyBackend to be created")
	}
}

func TestIngressMultiplePaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := setupTestEnvironment(t)

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

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-path-ingress",
			Namespace: "default",
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: strPtr("novaedge"),
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/api",
									PathType: (*networkingv1.PathType)(strPtr(string(networkingv1.PathTypePrefix))),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "api-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 8080,
											},
										},
									},
								},
								{
									Path:     "/",
									PathType: (*networkingv1.PathType)(strPtr(string(networkingv1.PathTypePrefix))),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "web-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 3000,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, service1); err != nil {
		t.Fatalf("failed to create Service 1: %v", err)
	}

	if err := k8sClient.Create(ctx, service2); err != nil {
		t.Fatalf("failed to create Service 2: %v", err)
	}

	if err := k8sClient.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      ingress.Name,
		Namespace: ingress.Namespace,
	}, proxyGateway); err != nil {
		t.Fatalf("expected ProxyGateway to be created: %v", err)
	}
}

func TestIngressDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := setupTestEnvironment(t)

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

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-ingress",
			Namespace: "default",
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: strPtr("novaedge"),
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: (*networkingv1.PathType)(strPtr(string(networkingv1.PathTypePrefix))),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "backend-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 8080,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := k8sClient.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Verify ProxyGateway was created
	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      ingress.Name,
		Namespace: ingress.Namespace,
	}, proxyGateway); err != nil {
		t.Fatalf("expected ProxyGateway to be created: %v", err)
	}

	// Delete Ingress
	if err := k8sClient.Delete(ctx, ingress); err != nil {
		t.Fatalf("failed to delete Ingress: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// ProxyGateway should be deleted (via owner reference)
	deletedGateway := &novaedgev1alpha1.ProxyGateway{}
	err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      ingress.Name,
		Namespace: ingress.Namespace,
	}, deletedGateway)

	if err == nil && deletedGateway.DeletionTimestamp == nil {
		t.Error("expected ProxyGateway to be deleted when Ingress is deleted")
	}
}

func TestIngressAnnotationClass(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	k8sClient := setupTestEnvironment(t)

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

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "annotated-ingress",
			Namespace: "default",
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "novaedge",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: (*networkingv1.PathType)(strPtr(string(networkingv1.PathTypePrefix))),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "backend-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 8080,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := k8sClient.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Should create resources despite using annotation instead of spec field
	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      ingress.Name,
		Namespace: ingress.Namespace,
	}, proxyGateway); err != nil {
		t.Fatalf("expected ProxyGateway to be created with annotated ingress class: %v", err)
	}
}
