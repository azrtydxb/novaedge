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
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// setupTestEnvironment creates a fake Kubernetes client with all required schemes
func setupTestEnvironment(t *testing.T) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()

	// Add all schemes
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}

	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add NovaEdge scheme: %v", err)
	}

	if err := gatewayv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add Gateway API scheme: %v", err)
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
		).
		Build()

	return k8sClient
}

// Helper functions for test data creation
func strPtr(s string) *string {
	return &s
}

func int32Ptr(v int32) *int32 {
	return &v
}
