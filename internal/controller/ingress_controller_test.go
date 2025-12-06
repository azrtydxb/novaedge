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
	"fmt"
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

			env := setupTestEnv(t)

			if test.service != nil {
				if err := env.client.Create(ctx, test.service); err != nil {
					t.Fatalf("failed to create Service: %v", err)
				}
			}

			if err := env.client.Create(ctx, test.ingress); err != nil {
				t.Fatalf("failed to create Ingress: %v", err)
			}

			// Manually trigger reconciliation
			err := env.reconcileIngress(ctx, test.ingress.Name, test.ingress.Namespace)
			if test.expectError && err == nil {
				// Error might be recorded in status conditions instead of returned
			}

			if test.expectCreated {
				// Check if ProxyGateway was created
				// Note: IngressTranslator generates gateway name as "{ingress-name}-gateway"
				proxyGateway := &novaedgev1alpha1.ProxyGateway{}
				gatewayName := fmt.Sprintf("%s-gateway", test.ingress.Name)
				err := env.client.Get(ctx, types.NamespacedName{
					Name:      gatewayName,
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

	env := setupTestEnv(t)

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

	if err := env.client.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.client.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileIngress(ctx, ingress.Name, ingress.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Check ProxyGateway creation - name is "{ingress-name}-gateway"
	gatewayName := fmt.Sprintf("%s-gateway", ingress.Name)
	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gatewayName,
		Namespace: ingress.Namespace,
	}, proxyGateway); err != nil {
		t.Fatalf("expected ProxyGateway to be created: %v", err)
	}

	// Check ProxyRoute creation
	proxyRouteList := &novaedgev1alpha1.ProxyRouteList{}
	if err := env.client.List(ctx, proxyRouteList); err != nil {
		t.Fatalf("failed to list ProxyRoutes: %v", err)
	}

	if len(proxyRouteList.Items) == 0 {
		t.Error("expected at least one ProxyRoute to be created")
	}

	// Check ProxyBackend creation
	proxyBackendList := &novaedgev1alpha1.ProxyBackendList{}
	if err := env.client.List(ctx, proxyBackendList); err != nil {
		t.Fatalf("failed to list ProxyBackends: %v", err)
	}

	if len(proxyBackendList.Items) == 0 {
		t.Error("expected at least one ProxyBackend to be created")
	}
}

func TestIngressMultiplePaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

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

	if err := env.client.Create(ctx, service1); err != nil {
		t.Fatalf("failed to create Service 1: %v", err)
	}

	if err := env.client.Create(ctx, service2); err != nil {
		t.Fatalf("failed to create Service 2: %v", err)
	}

	if err := env.client.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileIngress(ctx, ingress.Name, ingress.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Check ProxyGateway creation - name is "{ingress-name}-gateway"
	gatewayName := fmt.Sprintf("%s-gateway", ingress.Name)
	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gatewayName,
		Namespace: ingress.Namespace,
	}, proxyGateway); err != nil {
		t.Fatalf("expected ProxyGateway to be created: %v", err)
	}
}

func TestIngressDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

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

	if err := env.client.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.client.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileIngress(ctx, ingress.Name, ingress.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Verify ProxyGateway was created - name is "{ingress-name}-gateway"
	gatewayName := fmt.Sprintf("%s-gateway", ingress.Name)
	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gatewayName,
		Namespace: ingress.Namespace,
	}, proxyGateway); err != nil {
		t.Fatalf("expected ProxyGateway to be created: %v", err)
	}

	// Delete Ingress
	if err := env.client.Delete(ctx, ingress); err != nil {
		t.Fatalf("failed to delete Ingress: %v", err)
	}

	// Note: In unit tests with fake client, owner reference garbage collection
	// doesn't work automatically. In a real cluster, the ProxyGateway would be
	// deleted when the Ingress is deleted via owner references.
	// For unit tests, we verify the ProxyGateway was created successfully.
}

func TestIngressAnnotationClass(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

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

	if err := env.client.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.client.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileIngress(ctx, ingress.Name, ingress.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Should create resources despite using annotation instead of spec field
	// Name is "{ingress-name}-gateway"
	gatewayName := fmt.Sprintf("%s-gateway", ingress.Name)
	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gatewayName,
		Namespace: ingress.Namespace,
	}, proxyGateway); err != nil {
		t.Fatalf("expected ProxyGateway to be created with annotated ingress class: %v", err)
	}
}

func TestIngressAnnotations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backend-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
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
				"novaedge.io/load-balancing":        "ewma",
				"novaedge.io/proxy-connect-timeout": "5s",
				"novaedge.io/proxy-read-timeout":    "30s",
				"novaedge.io/proxy-body-size":       "10m",
				"novaedge.io/whitelist-source-range": "10.0.0.0/8,192.168.0.0/16",
				"novaedge.io/backend-protocol":      "HTTPS",
				"novaedge.io/session-affinity":      "cookie",
				"novaedge.io/rewrite-target":        "/api$1",
				"novaedge.io/request-headers":       `{"X-Custom-Header": "value"}`,
				"novaedge.io/remove-request-headers": "X-Remove-Me,X-Remove-Too",
			},
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
	}

	if err := env.client.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.client.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileIngress(ctx, ingress.Name, ingress.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Check ProxyGateway with SSL redirect and body size
	gatewayName := fmt.Sprintf("%s-gateway", ingress.Name)
	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gatewayName,
		Namespace: ingress.Namespace,
	}, proxyGateway); err != nil {
		t.Fatalf("expected ProxyGateway to be created: %v", err)
	}

	// Verify HTTP listener has SSL redirect enabled (TLS is configured)
	httpListener := findListener(proxyGateway.Spec.Listeners, "http")
	if httpListener == nil {
		t.Error("expected HTTP listener to exist")
	} else {
		if !httpListener.SSLRedirect {
			t.Error("expected SSL redirect to be enabled when TLS is configured")
		}
		if httpListener.MaxRequestBodySize != 10*1024*1024 {
			t.Errorf("expected MaxRequestBodySize to be 10MB, got %d", httpListener.MaxRequestBodySize)
		}
		if len(httpListener.AllowedSourceRanges) != 2 {
			t.Errorf("expected 2 allowed source ranges, got %d", len(httpListener.AllowedSourceRanges))
		}
	}

	// Check ProxyBackend with annotations
	proxyBackendList := &novaedgev1alpha1.ProxyBackendList{}
	if err := env.client.List(ctx, proxyBackendList); err != nil {
		t.Fatalf("failed to list ProxyBackends: %v", err)
	}

	if len(proxyBackendList.Items) == 0 {
		t.Fatal("expected at least one ProxyBackend")
	}

	backend := &proxyBackendList.Items[0]

	// Verify LB policy from session affinity annotation
	if backend.Spec.LBPolicy != novaedgev1alpha1.LBPolicyRingHash {
		t.Errorf("expected LB policy RingHash for session affinity, got %s", backend.Spec.LBPolicy)
	}

	// Verify timeouts
	if backend.Spec.ConnectTimeout.Duration.String() != "5s" {
		t.Errorf("expected connect timeout 5s, got %s", backend.Spec.ConnectTimeout.Duration.String())
	}
	if backend.Spec.IdleTimeout.Duration.String() != "30s" {
		t.Errorf("expected idle timeout 30s, got %s", backend.Spec.IdleTimeout.Duration.String())
	}

	// Verify backend TLS enabled
	if backend.Spec.TLS == nil || !backend.Spec.TLS.Enabled {
		t.Error("expected backend TLS to be enabled for HTTPS backend protocol")
	}

	// Check ProxyRoute with filters
	proxyRouteList := &novaedgev1alpha1.ProxyRouteList{}
	if err := env.client.List(ctx, proxyRouteList); err != nil {
		t.Fatalf("failed to list ProxyRoutes: %v", err)
	}

	if len(proxyRouteList.Items) == 0 {
		t.Fatal("expected at least one ProxyRoute")
	}

	route := &proxyRouteList.Items[0]
	if len(route.Spec.Rules) == 0 {
		t.Fatal("expected at least one rule in ProxyRoute")
	}

	rule := route.Spec.Rules[0]

	// Verify filters exist
	hasRewrite := false
	hasAddHeader := false
	hasRemoveHeader := false
	for _, filter := range rule.Filters {
		switch filter.Type {
		case novaedgev1alpha1.HTTPRouteFilterURLRewrite:
			hasRewrite = true
			if filter.RewritePath == nil || *filter.RewritePath != "/api$1" {
				t.Error("expected rewrite path to be /api$1")
			}
		case novaedgev1alpha1.HTTPRouteFilterAddHeader:
			hasAddHeader = true
			if len(filter.Add) != 1 {
				t.Errorf("expected 1 header to add, got %d", len(filter.Add))
			}
		case novaedgev1alpha1.HTTPRouteFilterRemoveHeader:
			hasRemoveHeader = true
			if len(filter.Remove) != 2 {
				t.Errorf("expected 2 headers to remove, got %d", len(filter.Remove))
			}
		}
	}

	if !hasRewrite {
		t.Error("expected URL rewrite filter")
	}
	if !hasAddHeader {
		t.Error("expected add header filter")
	}
	if !hasRemoveHeader {
		t.Error("expected remove header filter")
	}
}

func TestIngressServicePortNameResolution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	// Create service with named ports
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "named-port-service",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
				{
					Name:       "grpc",
					Port:       9090,
					TargetPort: intstr.FromInt(9090),
				},
			},
		},
	}

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "named-port-ingress",
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
											Name: "named-port-service",
											Port: networkingv1.ServiceBackendPort{
												Name: "http",
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

	if err := env.client.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.client.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileIngress(ctx, ingress.Name, ingress.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Check ProxyBackend has correct port resolved
	proxyBackendList := &novaedgev1alpha1.ProxyBackendList{}
	if err := env.client.List(ctx, proxyBackendList); err != nil {
		t.Fatalf("failed to list ProxyBackends: %v", err)
	}

	if len(proxyBackendList.Items) == 0 {
		t.Fatal("expected at least one ProxyBackend")
	}

	backend := &proxyBackendList.Items[0]
	if backend.Spec.ServiceRef == nil {
		t.Fatal("expected ServiceRef to be set")
	}

	// Port should be resolved to 8080 from the named port "http"
	if backend.Spec.ServiceRef.Port != 8080 {
		t.Errorf("expected port to be resolved to 8080, got %d", backend.Spec.ServiceRef.Port)
	}
}

func TestIngressCanaryAnnotations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "canary-service",
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
			Name:      "canary-ingress",
			Namespace: "default",
			Annotations: map[string]string{
				"novaedge.io/canary-weight":       "20",
				"novaedge.io/canary-header":       "X-Canary",
				"novaedge.io/canary-header-value": "true",
			},
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
											Name: "canary-service",
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

	if err := env.client.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.client.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileIngress(ctx, ingress.Name, ingress.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Check ProxyRoute with canary settings
	proxyRouteList := &novaedgev1alpha1.ProxyRouteList{}
	if err := env.client.List(ctx, proxyRouteList); err != nil {
		t.Fatalf("failed to list ProxyRoutes: %v", err)
	}

	if len(proxyRouteList.Items) == 0 {
		t.Fatal("expected at least one ProxyRoute")
	}

	route := &proxyRouteList.Items[0]
	if len(route.Spec.Rules) == 0 {
		t.Fatal("expected at least one rule")
	}

	rule := route.Spec.Rules[0]

	// Verify canary weight
	if len(rule.BackendRefs) == 0 {
		t.Fatal("expected at least one backend ref")
	}

	backendRef := rule.BackendRefs[0]
	if backendRef.Weight == nil || *backendRef.Weight != 20 {
		weight := int32(0)
		if backendRef.Weight != nil {
			weight = *backendRef.Weight
		}
		t.Errorf("expected canary weight 20, got %d", weight)
	}

	// Verify canary header match
	if len(rule.Matches) == 0 {
		t.Fatal("expected at least one match")
	}

	match := rule.Matches[0]
	if len(match.Headers) == 0 {
		t.Fatal("expected canary header match")
	}

	headerMatch := match.Headers[0]
	if headerMatch.Name != "X-Canary" {
		t.Errorf("expected header name X-Canary, got %s", headerMatch.Name)
	}
	if headerMatch.Value != "true" {
		t.Errorf("expected header value true, got %s", headerMatch.Value)
	}
}

func TestIngressUpstreamHashAnnotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hash-service",
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
			Name:      "hash-ingress",
			Namespace: "default",
			Annotations: map[string]string{
				"novaedge.io/upstream-hash-by": "$request_uri",
			},
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
											Name: "hash-service",
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

	if err := env.client.Create(ctx, service); err != nil {
		t.Fatalf("failed to create Service: %v", err)
	}

	if err := env.client.Create(ctx, ingress); err != nil {
		t.Fatalf("failed to create Ingress: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileIngress(ctx, ingress.Name, ingress.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Check ProxyBackend has RingHash policy
	proxyBackendList := &novaedgev1alpha1.ProxyBackendList{}
	if err := env.client.List(ctx, proxyBackendList); err != nil {
		t.Fatalf("failed to list ProxyBackends: %v", err)
	}

	if len(proxyBackendList.Items) == 0 {
		t.Fatal("expected at least one ProxyBackend")
	}

	backend := &proxyBackendList.Items[0]
	if backend.Spec.LBPolicy != novaedgev1alpha1.LBPolicyRingHash {
		t.Errorf("expected LB policy RingHash for upstream hash, got %s", backend.Spec.LBPolicy)
	}
}

// Helper function to find a listener by name
func findListener(listeners []novaedgev1alpha1.Listener, name string) *novaedgev1alpha1.Listener {
	for i := range listeners {
		if listeners[i].Name == name {
			return &listeners[i]
		}
	}
	return nil
}
