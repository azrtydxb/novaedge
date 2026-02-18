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

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestExtractIssuerFromAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantName    string
		wantKind    string
		wantFound   bool
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			wantName:    "",
			wantKind:    "",
			wantFound:   false,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			wantName:    "",
			wantKind:    "",
			wantFound:   false,
		},
		{
			name: "cluster issuer",
			annotations: map[string]string{
				novaedgev1alpha1.AnnotationCertManagerClusterIssuer: "letsencrypt-prod",
			},
			wantName:  "letsencrypt-prod",
			wantKind:  "ClusterIssuer",
			wantFound: true,
		},
		{
			name: "namespaced issuer",
			annotations: map[string]string{
				novaedgev1alpha1.AnnotationCertManagerIssuer: "my-issuer",
			},
			wantName:  "my-issuer",
			wantKind:  "Issuer",
			wantFound: true,
		},
		{
			name: "cluster issuer takes precedence",
			annotations: map[string]string{
				novaedgev1alpha1.AnnotationCertManagerClusterIssuer: "cluster-issuer",
				novaedgev1alpha1.AnnotationCertManagerIssuer:        "namespace-issuer",
			},
			wantName:  "cluster-issuer",
			wantKind:  "ClusterIssuer",
			wantFound: true,
		},
		{
			name: "empty cluster issuer value",
			annotations: map[string]string{
				novaedgev1alpha1.AnnotationCertManagerClusterIssuer: "",
			},
			wantName:  "",
			wantKind:  "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}

			name, kind, found := extractIssuerFromAnnotations(gateway)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantFound, found)
		})
	}
}

func TestCollectDNSNames(t *testing.T) {
	tests := []struct {
		name      string
		listeners []novaedgev1alpha1.Listener
		want      []string
	}{
		{
			name:      "no listeners",
			listeners: nil,
			want:      nil,
		},
		{
			name: "single listener with hostnames",
			listeners: []novaedgev1alpha1.Listener{
				{
					Hostnames: []string{"example.com", "www.example.com"},
				},
			},
			want: []string{"example.com", "www.example.com"},
		},
		{
			name: "multiple listeners with hostnames",
			listeners: []novaedgev1alpha1.Listener{
				{
					Hostnames: []string{"api.example.com"},
				},
				{
					Hostnames: []string{"app.example.com"},
				},
			},
			want: []string{"api.example.com", "app.example.com"},
		},
		{
			name: "duplicate hostnames are deduplicated",
			listeners: []novaedgev1alpha1.Listener{
				{
					Hostnames: []string{"example.com", "www.example.com"},
				},
				{
					Hostnames: []string{"example.com", "api.example.com"},
				},
			},
			want: []string{"example.com", "www.example.com", "api.example.com"},
		},
		{
			name: "empty hostnames",
			listeners: []novaedgev1alpha1.Listener{
				{
					Hostnames: []string{},
				},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &novaedgev1alpha1.ProxyGateway{
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					Listeners: tt.listeners,
				},
			}

			got := collectDNSNames(gateway)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetermineCertSecretName(t *testing.T) {
	tests := []struct {
		name    string
		gateway *novaedgev1alpha1.ProxyGateway
		want    string
	}{
		{
			name: "no TLS config",
			gateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					Listeners: []novaedgev1alpha1.Listener{
						{
							Hostnames: []string{"example.com"},
						},
					},
				},
			},
			want: "test-gateway-tls",
		},
		{
			name: "with TLS secret ref",
			gateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					Listeners: []novaedgev1alpha1.Listener{
						{
							Hostnames: []string{"example.com"},
							TLS: &novaedgev1alpha1.TLSConfig{
								SecretRef: &corev1.SecretReference{
									Name: "custom-tls-secret",
								},
							},
						},
					},
				},
			},
			want: "custom-tls-secret",
		},
		{
			name: "multiple listeners uses first with TLS",
			gateway: &novaedgev1alpha1.ProxyGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gateway",
				},
				Spec: novaedgev1alpha1.ProxyGatewaySpec{
					Listeners: []novaedgev1alpha1.Listener{
						{
							Hostnames: []string{"example.com"},
						},
						{
							Hostnames: []string{"api.example.com"},
							TLS: &novaedgev1alpha1.TLSConfig{
								SecretRef: &corev1.SecretReference{
									Name: "api-tls-secret",
								},
							},
						},
					},
				},
			},
			want: "api-tls-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineCertSecretName(tt.gateway)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildCertificateUnstructured(t *testing.T) {
	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
			UID:       "test-uid",
		},
	}

	dnsNames := []string{"example.com", "www.example.com"}
	secretName := "test-tls-secret"
	issuerName := "letsencrypt-prod"
	issuerKind := "ClusterIssuer"

	cert := buildCertificateUnstructured(
		"test-cert",
		"default",
		dnsNames,
		secretName,
		issuerName,
		issuerKind,
		gateway,
	)

	assert.Equal(t, "test-cert", cert.GetName())
	assert.Equal(t, "default", cert.GetNamespace())
	assert.Equal(t, "cert-manager.io/v1", cert.GetAPIVersion())
	assert.Equal(t, "Certificate", cert.GetKind())

	labels := cert.GetLabels()
	assert.Equal(t, "novaedge", labels["app.kubernetes.io/managed-by"])
	assert.Equal(t, "test-gateway", labels["novaedge.io/gateway"])

	ownerRefs := cert.GetOwnerReferences()
	assert.Len(t, ownerRefs, 1)
	assert.Equal(t, "ProxyGateway", ownerRefs[0].Kind)
	assert.Equal(t, "test-gateway", ownerRefs[0].Name)
	assert.Equal(t, "test-uid", string(ownerRefs[0].UID))
}

func TestToInterfaceSlice(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []interface{}
	}{
		{
			name:  "empty slice",
			input: []string{},
			want:  []interface{}{},
		},
		{
			name:  "single element",
			input: []string{"one"},
			want:  []interface{}{"one"},
		},
		{
			name:  "multiple elements",
			input: []string{"one", "two", "three"},
			want:  []interface{}{"one", "two", "three"},
		},
		{
			name:  "nil slice",
			input: nil,
			want:  []interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toInterfaceSlice(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractReadyCondition(t *testing.T) {
	tests := []struct {
		name       string
		cert       *unstructured.Unstructured
		wantReady  bool
		wantReason string
		wantErr    bool
	}{
		{
			name: "no conditions",
			cert: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"status": map[string]interface{}{},
				},
			},
			wantReady:  false,
			wantReason: "No conditions",
			wantErr:    false,
		},
		{
			name: "ready condition true",
			cert: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":    "Ready",
								"status":  "True",
								"message": "Issued",
							},
						},
					},
				},
			},
			wantReady:  true,
			wantReason: "Issued",
			wantErr:    false,
		},
		{
			name: "ready condition false",
			cert: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":    "Ready",
								"status":  "False",
								"message": "Pending",
							},
						},
					},
				},
			},
			wantReady:  false,
			wantReason: "Pending",
			wantErr:    false,
		},
		{
			name: "no ready condition",
			cert: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "OtherCondition",
								"status": "True",
							},
						},
					},
				},
			},
			wantReady:  false,
			wantReason: "Ready condition not found",
			wantErr:    false,
		},
		{
			name: "no status field",
			cert: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			wantReady:  false,
			wantReason: "No conditions",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready, reason, err := extractReadyCondition(tt.cert)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantReady, ready)
				assert.Equal(t, tt.wantReason, reason)
			}
		})
	}
}
