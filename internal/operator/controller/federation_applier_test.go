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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	"github.com/azrtydxb/novaedge/internal/controller/federation"
)

func TestNewFederationResourceApplier(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := zap.NewNop()

	applier := NewFederationResourceApplier(fakeClient, scheme, logger)
	assert.NotNil(t, applier)
	assert.NotNil(t, applier.client)
	assert.NotNil(t, applier.scheme)
	assert.NotNil(t, applier.logger)
}

func TestFederationResourceApplier_Apply_UnknownKind(_ *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := zap.NewNop()

	applier := NewFederationResourceApplier(fakeClient, scheme, logger)

	key := federation.ResourceKey{
		Kind:      "UnknownKind",
		Name:      "test",
		Namespace: "default",
	}

	// Should not panic or error for unknown kinds
	applier.Apply(context.Background(), key, federation.ChangeTypeCreated, nil)
}

func TestFederationResourceApplier_Apply_ProxyGateway(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := zap.NewNop()

	applier := NewFederationResourceApplier(fakeClient, scheme, logger)

	gateway := &novaedgev1alpha1.ProxyGateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ProxyGateway",
			APIVersion: "novaedge.piwi3910.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: "HTTP",
				},
			},
		},
	}

	data, err := json.Marshal(gateway)
	require.NoError(t, err)

	key := federation.ResourceKey{
		Kind:      "ProxyGateway",
		Name:      "test-gateway",
		Namespace: "default",
	}

	applier.Apply(context.Background(), key, federation.ChangeTypeCreated, data)

	// Verify the gateway was created
	var createdGateway novaedgev1alpha1.ProxyGateway
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-gateway"}, &createdGateway)
	assert.NoError(t, err)
	assert.Equal(t, "test-gateway", createdGateway.Name)
}

func TestFederationResourceApplier_Apply_ProxyRoute(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := zap.NewNop()

	applier := NewFederationResourceApplier(fakeClient, scheme, logger)

	route := &novaedgev1alpha1.ProxyRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ProxyRoute",
			APIVersion: "novaedge.piwi3910.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyRouteSpec{
			Hostnames: []string{"example.com"},
		},
	}

	data, err := json.Marshal(route)
	require.NoError(t, err)

	key := federation.ResourceKey{
		Kind:      "ProxyRoute",
		Name:      "test-route",
		Namespace: "default",
	}

	applier.Apply(context.Background(), key, federation.ChangeTypeCreated, data)

	// Verify the route was created
	var createdRoute novaedgev1alpha1.ProxyRoute
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-route"}, &createdRoute)
	assert.NoError(t, err)
	assert.Equal(t, "test-route", createdRoute.Name)
}

func TestFederationResourceApplier_Apply_ProxyBackend(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := zap.NewNop()

	applier := NewFederationResourceApplier(fakeClient, scheme, logger)

	backend := &novaedgev1alpha1.ProxyBackend{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ProxyBackend",
			APIVersion: "novaedge.piwi3910.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
	}

	data, err := json.Marshal(backend)
	require.NoError(t, err)

	key := federation.ResourceKey{
		Kind:      "ProxyBackend",
		Name:      "test-backend",
		Namespace: "default",
	}

	applier.Apply(context.Background(), key, federation.ChangeTypeCreated, data)

	// Verify the backend was created
	var createdBackend novaedgev1alpha1.ProxyBackend
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-backend"}, &createdBackend)
	assert.NoError(t, err)
	assert.Equal(t, "test-backend", createdBackend.Name)
}

func TestFederationResourceApplier_Apply_ProxyPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := zap.NewNop()

	applier := NewFederationResourceApplier(fakeClient, scheme, logger)

	policy := &novaedgev1alpha1.ProxyPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ProxyPolicy",
			APIVersion: "novaedge.piwi3910.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: "default",
		},
	}

	data, err := json.Marshal(policy)
	require.NoError(t, err)

	key := federation.ResourceKey{
		Kind:      "ProxyPolicy",
		Name:      "test-policy",
		Namespace: "default",
	}

	applier.Apply(context.Background(), key, federation.ChangeTypeCreated, data)

	// Verify the policy was created
	var createdPolicy novaedgev1alpha1.ProxyPolicy
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-policy"}, &createdPolicy)
	assert.NoError(t, err)
	assert.Equal(t, "test-policy", createdPolicy.Name)
}

func TestFederationResourceApplier_Apply_Delete(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create an existing gateway
	existingGateway := &novaedgev1alpha1.ProxyGateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ProxyGateway",
			APIVersion: "novaedge.piwi3910.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
			Labels: map[string]string{
				FederationOriginLabel: "remote-cluster",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingGateway).Build()
	logger := zap.NewNop()

	applier := NewFederationResourceApplier(fakeClient, scheme, logger)

	key := federation.ResourceKey{
		Kind:      "ProxyGateway",
		Name:      "test-gateway",
		Namespace: "default",
	}

	// Delete the gateway
	applier.Apply(context.Background(), key, federation.ChangeTypeDeleted, nil)

	// Verify the gateway was deleted
	var deletedGateway novaedgev1alpha1.ProxyGateway
	err := fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-gateway"}, &deletedGateway)
	assert.Error(t, err) // Should not find the gateway
}

func TestFederationResourceApplier_Apply_ServiceEndpoints(_ *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := zap.NewNop()

	applier := NewFederationResourceApplier(fakeClient, scheme, logger)

	key := federation.ResourceKey{
		Kind:      "ServiceEndpoints",
		Name:      "test-service",
		Namespace: "default",
	}

	// Should not panic or error for ServiceEndpoints (just logs)
	applier.Apply(context.Background(), key, federation.ChangeTypeCreated, nil)
}

func TestFederationOriginLabel(t *testing.T) {
	assert.Equal(t, "novaedge.io/federation-origin", FederationOriginLabel)
}

func TestFederationResourceApplier_Apply_ConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := zap.NewNop()

	applier := NewFederationResourceApplier(fakeClient, scheme, logger)

	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "default",
		},
		Data: map[string]string{
			"key": "value",
		},
	}

	data, err := json.Marshal(configMap)
	require.NoError(t, err)

	key := federation.ResourceKey{
		Kind:      "ConfigMap",
		Name:      "test-configmap",
		Namespace: "default",
	}

	applier.Apply(context.Background(), key, federation.ChangeTypeCreated, data)

	// Verify the configmap was created
	var createdConfigMap corev1.ConfigMap
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-configmap"}, &createdConfigMap)
	assert.NoError(t, err)
	assert.Equal(t, "test-configmap", createdConfigMap.Name)
}

func TestFederationResourceApplier_Apply_Secret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := zap.NewNop()

	applier := NewFederationResourceApplier(fakeClient, scheme, logger)

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"password": []byte("secretvalue"),
		},
	}

	data, err := json.Marshal(secret)
	require.NoError(t, err)

	key := federation.ResourceKey{
		Kind:      "Secret",
		Name:      "test-secret",
		Namespace: "default",
	}

	applier.Apply(context.Background(), key, federation.ChangeTypeCreated, data)

	// Verify the secret was created
	var createdSecret corev1.Secret
	err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test-secret"}, &createdSecret)
	assert.NoError(t, err)
	assert.Equal(t, "test-secret", createdSecret.Name)
}
