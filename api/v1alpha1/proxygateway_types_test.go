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

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestProtocolType(t *testing.T) {
	tests := []struct {
		name     string
		protocol ProtocolType
		expected string
	}{
		{"HTTP", ProtocolTypeHTTP, "HTTP"},
		{"HTTPS", ProtocolTypeHTTPS, "HTTPS"},
		{"HTTP3", ProtocolTypeHTTP3, "HTTP3"},
		{"TCP", ProtocolTypeTCP, "TCP"},
		{"TLS", ProtocolTypeTLS, "TLS"},
		{"UDP", ProtocolTypeUDP, "UDP"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, ProtocolType(tt.expected), tt.protocol)
		})
	}
}

func TestTLSConfig(t *testing.T) {
	t.Run("with secret ref", func(t *testing.T) {
		tls := TLSConfig{
			SecretRef: &corev1.SecretReference{
				Name:      "test-secret",
				Namespace: "default",
			},
		}
		assert.Equal(t, "test-secret", tls.SecretRef.Name)
		assert.Equal(t, "default", tls.SecretRef.Namespace)
	})

	t.Run("with certificate ref", func(t *testing.T) {
		tls := TLSConfig{
			CertificateRef: &ObjectReference{
				Name:      "test-cert",
				Namespace: "default",
			},
		}
		assert.Equal(t, "test-cert", tls.CertificateRef.Name)
	})

	t.Run("with ACME", func(t *testing.T) {
		tls := TLSConfig{
			ACME: &InlineACMEConfig{
				Email: "test@example.com",
			},
		}
		assert.Equal(t, "test@example.com", tls.ACME.Email)
	})

	t.Run("with self-signed", func(t *testing.T) {
		tls := TLSConfig{
			SelfSigned: &InlineSelfSignedConfig{
				Validity:     "8760h",
				Organization: "Test Org",
			},
		}
		assert.Equal(t, "8760h", tls.SelfSigned.Validity)
		assert.Equal(t, "Test Org", tls.SelfSigned.Organization)
	})

	t.Run("with vault cert ref", func(t *testing.T) {
		tls := TLSConfig{
			VaultCertRef: &VaultCertReference{
				Path: "pki/issue/example",
				Role: "example-role",
			},
		}
		assert.Equal(t, "pki/issue/example", tls.VaultCertRef.Path)
		assert.Equal(t, "example-role", tls.VaultCertRef.Role)
	})

	t.Run("with min version and cipher suites", func(t *testing.T) {
		tls := TLSConfig{
			MinVersion:   "TLS1.3",
			CipherSuites: []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
		}
		assert.Equal(t, "TLS1.3", tls.MinVersion)
		assert.Len(t, tls.CipherSuites, 2)
	})
}

func TestVaultCertReference(t *testing.T) {
	ref := VaultCertReference{
		Path:            "pki/issue/example-com",
		Role:            "example-com-role",
		TTL:             "24h",
		CacheSecretName: "cached-cert",
	}

	assert.Equal(t, "pki/issue/example-com", ref.Path)
	assert.Equal(t, "example-com-role", ref.Role)
	assert.Equal(t, "24h", ref.TTL)
	assert.Equal(t, "cached-cert", ref.CacheSecretName)
}

func TestProxyGateway(t *testing.T) {
	gateway := ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: ProxyGatewaySpec{
			VIPRef: "test-vip",
			Listeners: []Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: ProtocolTypeHTTP,
				},
				{
					Name:     "https",
					Port:     443,
					Protocol: ProtocolTypeHTTPS,
					TLS: &TLSConfig{
						SecretRef: &corev1.SecretReference{
							Name: "test-tls",
						},
					},
				},
			},
		},
	}

	assert.Equal(t, "test-gateway", gateway.Name)
	assert.Equal(t, "default", gateway.Namespace)
	assert.Equal(t, "test-vip", gateway.Spec.VIPRef)
	assert.Len(t, gateway.Spec.Listeners, 2)
	assert.Equal(t, int32(80), gateway.Spec.Listeners[0].Port)
	assert.Equal(t, int32(443), gateway.Spec.Listeners[1].Port)
}

func TestProxyGatewayList(t *testing.T) {
	list := ProxyGatewayList{
		Items: []ProxyGateway{
			{ObjectMeta: metav1.ObjectMeta{Name: "gateway-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "gateway-2"}},
		},
	}

	assert.Len(t, list.Items, 2)
	assert.Equal(t, "gateway-1", list.Items[0].Name)
	assert.Equal(t, "gateway-2", list.Items[1].Name)
}

func TestListener(t *testing.T) {
	listener := Listener{
		Name:     "test-listener",
		Port:     8080,
		Protocol: ProtocolTypeHTTP,
		Hostnames: []string{
			"example.com",
			"api.example.com",
		},
		SSLRedirect:        true,
		MaxRequestBodySize: 10485760,
		OCSPStapling:       true,
	}

	assert.Equal(t, "test-listener", listener.Name)
	assert.Equal(t, int32(8080), listener.Port)
	assert.Equal(t, ProtocolTypeHTTP, listener.Protocol)
	assert.Len(t, listener.Hostnames, 2)
	assert.True(t, listener.SSLRedirect)
	assert.Equal(t, int64(10485760), listener.MaxRequestBodySize)
	assert.True(t, listener.OCSPStapling)
}

func TestProxyGatewayStatus(t *testing.T) {
	status := ProxyGatewayStatus{
		Conditions: []metav1.Condition{
			{
				Type:   "Ready",
				Status: metav1.ConditionTrue,
				Reason: "Configured",
			},
		},
		ListenerStatus: []ListenerStatus{
			{
				Name:  "http",
				Ready: true,
			},
		},
		ObservedGeneration: 1,
	}

	assert.Len(t, status.Conditions, 1)
	assert.Equal(t, "Ready", status.Conditions[0].Type)
	assert.Len(t, status.ListenerStatus, 1)
	assert.Equal(t, "http", status.ListenerStatus[0].Name)
	assert.True(t, status.ListenerStatus[0].Ready)
	assert.Equal(t, int64(1), status.ObservedGeneration)
}

func TestListenerStatus(t *testing.T) {
	status := ListenerStatus{
		Name:  "https",
		Ready: true,
	}

	assert.Equal(t, "https", status.Name)
	assert.True(t, status.Ready)
}

func TestInlineACMEConfig(t *testing.T) {
	config := InlineACMEConfig{
		Email:         "admin@example.com",
		Server:        "https://acme-v02.api.letsencrypt.org/directory",
		ChallengeType: "dns-01",
		DNSProvider:   "cloudflare",
	}

	assert.Equal(t, "admin@example.com", config.Email)
	assert.Equal(t, "https://acme-v02.api.letsencrypt.org/directory", config.Server)
	assert.Equal(t, "dns-01", config.ChallengeType)
	assert.Equal(t, "cloudflare", config.DNSProvider)
}

func TestInlineSelfSignedConfig(t *testing.T) {
	config := InlineSelfSignedConfig{
		Validity:     "8760h",
		Organization: "Test Organization",
	}

	assert.Equal(t, "8760h", config.Validity)
	assert.Equal(t, "Test Organization", config.Organization)
}

func TestCertManagerAnnotations(t *testing.T) {
	assert.Equal(t, "cert-manager.io/cluster-issuer", AnnotationCertManagerClusterIssuer)
	assert.Equal(t, "cert-manager.io/issuer", AnnotationCertManagerIssuer)
}

func TestProxyGatewaySpec(t *testing.T) {
	spec := ProxyGatewaySpec{
		VIPRef:            "my-vip",
		IngressClassName:  "novaedge",
		LoadBalancerClass: "novaedge.io/proxy",
		Listeners: []Listener{
			{
				Name:     "http",
				Port:     80,
				Protocol: ProtocolTypeHTTP,
			},
		},
	}

	assert.Equal(t, "my-vip", spec.VIPRef)
	assert.Equal(t, "novaedge", spec.IngressClassName)
	assert.Equal(t, "novaedge.io/proxy", spec.LoadBalancerClass)
	assert.Len(t, spec.Listeners, 1)
}

func TestClientAuthMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     ClientAuthMode
		expected string
	}{
		{"None", ClientAuthModeNone, "none"},
		{"Optional", ClientAuthModeOptional, "optional"},
		{"Require", ClientAuthModeRequire, "require"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, ClientAuthMode(tt.expected), tt.mode)
		})
	}
}

func TestClientAuthConfig(t *testing.T) {
	config := ClientAuthConfig{
		Mode:               ClientAuthModeRequire,
		CACertRef:          &corev1.SecretReference{Name: "ca-cert"},
		RequiredCNPatterns: []string{".*\\.example\\.com"},
		RequiredSANs:       []string{"spiffe://example.com/service"},
	}

	assert.Equal(t, ClientAuthModeRequire, config.Mode)
	assert.Equal(t, "ca-cert", config.CACertRef.Name)
	assert.Len(t, config.RequiredCNPatterns, 1)
	assert.Len(t, config.RequiredSANs, 1)
}

func TestProxyProtocolConfig(t *testing.T) {
	config := ProxyProtocolConfig{
		Enabled:      true,
		Version:      2,
		TrustedCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16"},
	}

	assert.True(t, config.Enabled)
	assert.Equal(t, int32(2), config.Version)
	assert.Len(t, config.TrustedCIDRs, 2)
}

func TestQUICConfig(t *testing.T) {
	config := QUICConfig{
		MaxIdleTimeout: "30s",
		MaxBiStreams:   100,
		MaxUniStreams:  100,
		Enable0RTT:     true,
	}

	assert.Equal(t, "30s", config.MaxIdleTimeout)
	assert.Equal(t, int64(100), config.MaxBiStreams)
	assert.Equal(t, int64(100), config.MaxUniStreams)
	assert.True(t, config.Enable0RTT)
}

func TestTracingConfig(t *testing.T) {
	rate := int32(50)
	config := TracingConfig{
		Enabled:         true,
		SamplingRate:    &rate,
		RequestIDHeader: "X-Request-ID",
	}

	assert.True(t, config.Enabled)
	assert.Equal(t, int32(50), *config.SamplingRate)
	assert.Equal(t, "X-Request-ID", config.RequestIDHeader)
}

func TestGatewayCacheConfig(t *testing.T) {
	config := GatewayCacheConfig{
		Enabled:      true,
		MaxSize:      "256Mi",
		DefaultTTL:   "5m",
		MaxTTL:       "1h",
		MaxEntrySize: "1Mi",
	}

	assert.True(t, config.Enabled)
	assert.Equal(t, "256Mi", config.MaxSize)
	assert.Equal(t, "5m", config.DefaultTTL)
	assert.Equal(t, "1h", config.MaxTTL)
	assert.Equal(t, "1Mi", config.MaxEntrySize)
}

func TestCompressionConfig(t *testing.T) {
	config := CompressionConfig{
		Enabled:      true,
		Level:        6,
		Algorithms:   []string{"gzip", "br"},
		ExcludeTypes: []string{"image/*", "video/*"},
	}

	assert.True(t, config.Enabled)
	assert.Equal(t, int32(6), config.Level)
	assert.Len(t, config.Algorithms, 2)
	assert.Len(t, config.ExcludeTypes, 2)
}

func TestRedirectSchemeConfig(t *testing.T) {
	config := RedirectSchemeConfig{
		Enabled:    true,
		Scheme:     "https",
		Port:       443,
		StatusCode: 301,
		Exclusions: []string{"/.well-known/"},
	}

	assert.True(t, config.Enabled)
	assert.Equal(t, "https", config.Scheme)
	assert.Equal(t, int32(443), config.Port)
	assert.Equal(t, int32(301), config.StatusCode)
	assert.Len(t, config.Exclusions, 1)
}
