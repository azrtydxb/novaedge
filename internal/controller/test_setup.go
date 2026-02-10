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
	"strings"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// testEnv holds the test environment
type testEnv struct {
	client                 client.Client
	scheme                 *runtime.Scheme
	gatewayReconciler      *GatewayReconciler
	httpRouteReconciler    *HTTPRouteReconciler
	ingressReconciler      *IngressReconciler
	proxyBackendReconciler *ProxyBackendReconciler
	proxyGatewayReconciler *ProxyGatewayReconciler
	proxyRouteReconciler   *ProxyRouteReconciler
	proxyVIPReconciler     *ProxyVIPReconciler
	proxyPolicyReconciler  *ProxyPolicyReconciler
}

// setupTestEnv creates a full test environment with all reconcilers
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	scheme := runtime.NewScheme()

	// Add all schemes
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}

	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add NovaEdge scheme: %v", err)
	}

	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("failed to add Gateway API scheme: %v", err)
	}

	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add networking scheme: %v", err)
	}

	// Create fake client with status subresource support
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(
			&novaedgev1alpha1.ProxyGateway{},
			&novaedgev1alpha1.ProxyBackend{},
			&novaedgev1alpha1.ProxyRoute{},
			&novaedgev1alpha1.ProxyVIP{},
			&novaedgev1alpha1.ProxyPolicy{},
			&gatewayv1.Gateway{},
			&gatewayv1.HTTPRoute{},
			&networkingv1.Ingress{},
		).
		Build()

	env := &testEnv{
		client: k8sClient,
		scheme: scheme,
	}

	// Initialize all reconcilers
	env.gatewayReconciler = &GatewayReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	env.httpRouteReconciler = &HTTPRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	env.ingressReconciler = &IngressReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	env.proxyBackendReconciler = &ProxyBackendReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	env.proxyGatewayReconciler = &ProxyGatewayReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	env.proxyRouteReconciler = &ProxyRouteReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	env.proxyVIPReconciler = &ProxyVIPReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	env.proxyPolicyReconciler = &ProxyPolicyReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	return env
}

// reconcileGateway manually triggers reconciliation for a Gateway
func (e *testEnv) reconcileGateway(ctx context.Context, name, namespace string) error {
	_, err := e.gatewayReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	return err
}

// reconcileHTTPRoute manually triggers reconciliation for an HTTPRoute
func (e *testEnv) reconcileHTTPRoute(ctx context.Context, name, namespace string) error {
	_, err := e.httpRouteReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	return err
}

// reconcileIngress manually triggers reconciliation for an Ingress
func (e *testEnv) reconcileIngress(ctx context.Context, name, namespace string) error {
	_, err := e.ingressReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	return err
}

// reconcileProxyBackend manually triggers reconciliation for a ProxyBackend
func (e *testEnv) reconcileProxyBackend(ctx context.Context, name, namespace string) error {
	_, err := e.proxyBackendReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	return err
}

// reconcileProxyGateway manually triggers reconciliation for a ProxyGateway
func (e *testEnv) reconcileProxyGateway(ctx context.Context, name, namespace string) error {
	_, err := e.proxyGatewayReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	return err
}

// reconcileProxyVIP manually triggers reconciliation for a ProxyVIP
func (e *testEnv) reconcileProxyVIP(ctx context.Context, name, namespace string) error {
	_, err := e.proxyVIPReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	return err
}

// reconcileProxyPolicy manually triggers reconciliation for a ProxyPolicy
func (e *testEnv) reconcileProxyPolicy(ctx context.Context, name, namespace string) error {
	_, err := e.proxyPolicyReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	return err
}

// Helper functions for test data creation
func strPtr(s string) *string {
	return &s
}

func int32Ptr(v int32) *int32 {
	return &v
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
