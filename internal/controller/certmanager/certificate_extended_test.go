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

package certmanager

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

func TestCertificateManager_EnsureCertificate_Create(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
	manager := NewCertificateManager(dynamicClient)

	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
			UID:       types.UID("test-uid-123"),
			Annotations: map[string]string{
				novaedgev1alpha1.AnnotationCertManagerClusterIssuer: "letsencrypt-prod",
			},
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:      "https",
					Protocol:  "HTTPS",
					Port:      443,
					Hostnames: []string{"example.com", "www.example.com"},
					TLS: &novaedgev1alpha1.TLSConfig{
						SecretRef: &corev1.SecretReference{
							Name: "tls-secret",
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	err := manager.EnsureCertificate(ctx, gateway)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify certificate was created
	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	cert, err := dynamicClient.Resource(gvr).Namespace("default").Get(ctx, "test-gateway-tls", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get created certificate: %v", err)
	}

	// Verify certificate fields
	if cert.GetName() != "test-gateway-tls" {
		t.Errorf("expected name 'test-gateway-tls', got '%s'", cert.GetName())
	}

	secretName, _, _ := unstructured.NestedString(cert.Object, "spec", "secretName")
	if secretName != "tls-secret" {
		t.Errorf("expected secretName 'tls-secret', got '%s'", secretName)
	}

	dnsNames, _, _ := unstructured.NestedStringSlice(cert.Object, "spec", "dnsNames")
	if len(dnsNames) != 2 {
		t.Errorf("expected 2 DNS names, got %d", len(dnsNames))
	}
}

func TestCertificateManager_EnsureCertificate_Update(t *testing.T) {
	// Create existing certificate
	existingCert := &unstructured.Unstructured{}
	existingCert.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})
	existingCert.SetName("test-gateway-tls")
	existingCert.SetNamespace("default")
	existingCert.SetResourceVersion("1")

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, existingCert)
	manager := NewCertificateManager(dynamicClient)

	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
			UID:       types.UID("test-uid-123"),
			Annotations: map[string]string{
				novaedgev1alpha1.AnnotationCertManagerIssuer: "my-issuer",
			},
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:      "https",
					Hostnames: []string{"newdomain.com"},
				},
			},
		},
	}

	ctx := context.Background()
	err := manager.EnsureCertificate(ctx, gateway)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify certificate was updated (not recreated)
	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	cert, err := dynamicClient.Resource(gvr).Namespace("default").Get(ctx, "test-gateway-tls", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated certificate: %v", err)
	}

	// Resource version should have been set for update
	if cert.GetResourceVersion() == "" {
		t.Error("expected resourceVersion to be set for update")
	}
}

func TestCertificateManager_EnsureCertificate_NoAnnotations(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
	manager := NewCertificateManager(dynamicClient)

	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:      "http",
					Hostnames: []string{"example.com"},
				},
			},
		},
	}

	ctx := context.Background()
	err := manager.EnsureCertificate(ctx, gateway)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no certificate was created
	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	_, err = dynamicClient.Resource(gvr).Namespace("default").Get(ctx, "test-gateway-tls", metav1.GetOptions{})
	if err == nil || !apierrors.IsNotFound(err) {
		t.Error("expected certificate not to be created when annotations missing")
	}
}

func TestCertificateManager_EnsureCertificate_NoHostnames(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
	manager := NewCertificateManager(dynamicClient)

	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
			Annotations: map[string]string{
				novaedgev1alpha1.AnnotationCertManagerClusterIssuer: "letsencrypt-prod",
			},
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name: "http",
					Port: 80,
				},
			},
		},
	}

	ctx := context.Background()
	err := manager.EnsureCertificate(ctx, gateway)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no certificate was created
	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	_, err = dynamicClient.Resource(gvr).Namespace("default").Get(ctx, "test-gateway-tls", metav1.GetOptions{})
	if err == nil || !apierrors.IsNotFound(err) {
		t.Error("expected certificate not to be created when no hostnames")
	}
}

func TestCertificateManager_GetCertificateStatus_Ready(t *testing.T) {
	// Create a ready certificate
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})
	cert.SetName("test-cert")
	cert.SetNamespace("default")

	conditions := []interface{}{
		map[string]interface{}{
			"type":    "Ready",
			"status":  "True",
			"message": "Certificate is up to date",
		},
	}
	_ = unstructured.SetNestedSlice(cert.Object, conditions, "status", "conditions")

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, cert)
	manager := NewCertificateManager(dynamicClient)

	ctx := context.Background()
	ready, message, err := manager.GetCertificateStatus(ctx, "default", "test-cert")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ready {
		t.Error("expected certificate to be ready")
	}
	if message != "Certificate is up to date" {
		t.Errorf("unexpected message: %s", message)
	}
}

func TestCertificateManager_GetCertificateStatus_NotReady(t *testing.T) {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})
	cert.SetName("test-cert")
	cert.SetNamespace("default")

	conditions := []interface{}{
		map[string]interface{}{
			"type":    "Ready",
			"status":  "False",
			"message": "Waiting for validation",
		},
	}
	_ = unstructured.SetNestedSlice(cert.Object, conditions, "status", "conditions")

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, cert)
	manager := NewCertificateManager(dynamicClient)

	ctx := context.Background()
	ready, message, err := manager.GetCertificateStatus(ctx, "default", "test-cert")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ready {
		t.Error("expected certificate not to be ready")
	}
	if message != "Waiting for validation" {
		t.Errorf("unexpected message: %s", message)
	}
}

func TestCertificateManager_DeleteCertificate(t *testing.T) {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})
	cert.SetName("test-cert")
	cert.SetNamespace("default")

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, cert)
	manager := NewCertificateManager(dynamicClient)

	ctx := context.Background()
	err := manager.DeleteCertificate(ctx, "default", "test-cert")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify deletion
	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	_, err = dynamicClient.Resource(gvr).Namespace("default").Get(ctx, "test-cert", metav1.GetOptions{})
	if err == nil || !apierrors.IsNotFound(err) {
		t.Error("expected certificate to be deleted")
	}
}

func TestCertificateManager_ListCertificatesForGateway(t *testing.T) {
	cert1 := createTestCertificate("cert1", "my-gateway")
	cert2 := createTestCertificate("cert2", "my-gateway")
	cert3 := createTestCertificate("cert3", "other-gateway")

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, cert1, cert2, cert3)
	manager := NewCertificateManager(dynamicClient)

	ctx := context.Background()
	gateway := types.NamespacedName{Namespace: "default", Name: "my-gateway"}

	certs, err := manager.ListCertificatesForGateway(ctx, gateway)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(certs) != 2 {
		t.Errorf("expected 2 certificates, got %d", len(certs))
	}

	// Verify correct certificates were returned
	names := make(map[string]bool)
	for _, cert := range certs {
		names[cert.GetName()] = true
	}

	if !names["cert1"] || !names["cert2"] {
		t.Error("expected cert1 and cert2 to be in results")
	}
	if names["cert3"] {
		t.Error("did not expect cert3 to be in results")
	}
}
