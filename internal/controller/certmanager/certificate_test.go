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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestExtractIssuerFromAnnotations_ClusterIssuer(t *testing.T) {
	gw := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				novaedgev1alpha1.AnnotationCertManagerClusterIssuer: "letsencrypt-prod",
			},
		},
	}

	name, kind, found := extractIssuerFromAnnotations(gw)
	if !found {
		t.Error("expected to find issuer annotation")
	}
	if name != "letsencrypt-prod" {
		t.Errorf("expected issuer name 'letsencrypt-prod', got '%s'", name)
	}
	if kind != "ClusterIssuer" {
		t.Errorf("expected kind 'ClusterIssuer', got '%s'", kind)
	}
}

func TestExtractIssuerFromAnnotations_Issuer(t *testing.T) {
	gw := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				novaedgev1alpha1.AnnotationCertManagerIssuer: "my-issuer",
			},
		},
	}

	name, kind, found := extractIssuerFromAnnotations(gw)
	if !found {
		t.Error("expected to find issuer annotation")
	}
	if name != "my-issuer" {
		t.Errorf("expected issuer name 'my-issuer', got '%s'", name)
	}
	if kind != "Issuer" {
		t.Errorf("expected kind 'Issuer', got '%s'", kind)
	}
}

func TestExtractIssuerFromAnnotations_None(t *testing.T) {
	gw := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"unrelated": "annotation",
			},
		},
	}

	_, _, found := extractIssuerFromAnnotations(gw)
	if found {
		t.Error("expected no issuer annotation found")
	}
}

func TestCollectDNSNames(t *testing.T) {
	gw := &novaedgev1alpha1.ProxyGateway{
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:      "https",
					Hostnames: []string{"example.com", "www.example.com"},
				},
				{
					Name:      "https-api",
					Hostnames: []string{"api.example.com", "example.com"}, // duplicate
				},
			},
		},
	}

	dnsNames := collectDNSNames(gw)
	if len(dnsNames) != 3 {
		t.Errorf("expected 3 unique DNS names, got %d: %v", len(dnsNames), dnsNames)
	}
}

func TestCollectDNSNames_Empty(t *testing.T) {
	gw := &novaedgev1alpha1.ProxyGateway{
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			Listeners: []novaedgev1alpha1.Listener{
				{Name: "http", Port: 80},
			},
		},
	}

	dnsNames := collectDNSNames(gw)
	if len(dnsNames) != 0 {
		t.Errorf("expected 0 DNS names, got %d", len(dnsNames))
	}
}

func TestBuildCertificateUnstructured(t *testing.T) {
	gw := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-gateway",
			Namespace: "default",
			UID:       types.UID("test-uid-12345"),
		},
	}

	cert := buildCertificateUnstructured(
		"my-gateway-tls",
		"default",
		[]string{"example.com", "www.example.com"},
		"my-secret",
		"letsencrypt-prod",
		"ClusterIssuer",
		gw,
	)

	if cert.GetName() != "my-gateway-tls" {
		t.Errorf("expected name 'my-gateway-tls', got '%s'", cert.GetName())
	}
	if cert.GetNamespace() != "default" {
		t.Errorf("expected namespace 'default', got '%s'", cert.GetNamespace())
	}

	// Check owner references
	ownerRefs := cert.GetOwnerReferences()
	if len(ownerRefs) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(ownerRefs))
	}
	if ownerRefs[0].Name != "my-gateway" {
		t.Errorf("expected owner name 'my-gateway', got '%s'", ownerRefs[0].Name)
	}

	// Check labels
	labels := cert.GetLabels()
	if labels["novaedge.io/gateway"] != "my-gateway" {
		t.Errorf("expected gateway label 'my-gateway', got '%s'", labels["novaedge.io/gateway"])
	}
}

func TestDetermineCertSecretName(t *testing.T) {
	tests := []struct {
		name     string
		gateway  *novaedgev1alpha1.ProxyGateway
		expected string
	}{
		{
			name: "with secret ref",
			gateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "my-gw"},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					Listeners: []novaedgev1alpha1.Listener{
						{
							Name: "https",
							TLS: &novaedgev1alpha1.TLSConfig{
								SecretRef: &corev1.SecretReference{
									Name: "my-tls-secret",
								},
							},
						},
					},
				},
			},
			expected: "my-tls-secret",
		},
		{
			name: "without secret ref",
			gateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "my-gw"},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					Listeners: []novaedgev1alpha1.Listener{
						{Name: "http", Port: 80},
					},
				},
			},
			expected: "my-gw-tls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineCertSecretName(tt.gateway)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}
