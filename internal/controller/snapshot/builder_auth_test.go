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

package snapshot

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestBuildBasicAuthConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name      string
		policy    *novaedgev1alpha1.ProxyPolicy
		secret    *corev1.Secret
		wantErr   bool
		wantRealm string
		wantStrip bool
	}{
		{
			name: "valid basic auth config",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					BasicAuth: &novaedgev1alpha1.BasicAuthPolicyConfig{
						SecretRef: novaedgev1alpha1.LocalObjectReference{
							Name: "htpasswd-secret",
						},
						Realm:     "TestRealm",
						StripAuth: true,
					},
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "htpasswd-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"htpasswd": []byte("user:$apr1$ruca84Hq$HgjGuQqKEd4M9.hHv.OKC/"),
				},
			},
			wantErr:   false,
			wantRealm: "TestRealm",
			wantStrip: true,
		},
		{
			name: "basic auth with default realm",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					BasicAuth: &novaedgev1alpha1.BasicAuthPolicyConfig{
						SecretRef: novaedgev1alpha1.LocalObjectReference{
							Name: "htpasswd-secret",
						},
					},
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "htpasswd-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"htpasswd": []byte("user:password"),
				},
			},
			wantErr:   false,
			wantRealm: "Restricted",
			wantStrip: false,
		},
		{
			name: "missing secret",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					BasicAuth: &novaedgev1alpha1.BasicAuthPolicyConfig{
						SecretRef: novaedgev1alpha1.LocalObjectReference{
							Name: "missing-secret",
						},
					},
				},
			},
			secret:  nil,
			wantErr: true,
		},
		{
			name: "secret missing htpasswd key",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					BasicAuth: &novaedgev1alpha1.BasicAuthPolicyConfig{
						SecretRef: novaedgev1alpha1.LocalObjectReference{
							Name: "bad-secret",
						},
					},
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bad-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"wrong-key": []byte("data"),
				},
			},
			wantErr: true,
		},
		{
			name: "nil basic auth spec",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					BasicAuth: nil,
				},
			},
			secret:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []runtime.Object{tt.policy}
			if tt.secret != nil {
				objects = append(objects, tt.secret)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			builder := NewBuilder(fakeClient)
			config, err := builder.buildBasicAuthConfig(context.Background(), tt.policy)

			if (err != nil) != tt.wantErr {
				t.Errorf("buildBasicAuthConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if config.Realm != tt.wantRealm {
					t.Errorf("Realm = %v, want %v", config.Realm, tt.wantRealm)
				}
				if config.StripAuth != tt.wantStrip {
					t.Errorf("StripAuth = %v, want %v", config.StripAuth, tt.wantStrip)
				}
				if config.Htpasswd == "" {
					t.Error("Htpasswd should not be empty")
				}
			}
		})
	}
}

func TestBuildForwardAuthConfig(t *testing.T) {
	tests := []struct {
		name            string
		spec            *novaedgev1alpha1.ForwardAuthPolicyConfig
		wantAddress     string
		wantTimeoutMs   int64
		wantCacheTTLSec int64
		wantAuthHeaders int
		wantRespHeaders int
	}{
		{
			name: "complete forward auth config",
			spec: &novaedgev1alpha1.ForwardAuthPolicyConfig{
				Address:         "http://auth.example.com",
				Timeout:         "10s",
				CacheTTL:        "5m",
				AuthHeaders:     []string{"Authorization", "X-API-Key"},
				ResponseHeaders: []string{"X-User", "X-Email"},
			},
			wantAddress:     "http://auth.example.com",
			wantTimeoutMs:   10000,
			wantCacheTTLSec: 300,
			wantAuthHeaders: 2,
			wantRespHeaders: 2,
		},
		{
			name: "default timeout",
			spec: &novaedgev1alpha1.ForwardAuthPolicyConfig{
				Address: "http://auth.example.com",
			},
			wantAddress:   "http://auth.example.com",
			wantTimeoutMs: 5000, // default 5s
		},
		{
			name: "invalid timeout uses default",
			spec: &novaedgev1alpha1.ForwardAuthPolicyConfig{
				Address: "http://auth.example.com",
				Timeout: "invalid",
			},
			wantAddress:   "http://auth.example.com",
			wantTimeoutMs: 0, // invalid timeout results in 0
		},
		{
			name: "no cache TTL",
			spec: &novaedgev1alpha1.ForwardAuthPolicyConfig{
				Address: "http://auth.example.com",
			},
			wantAddress:     "http://auth.example.com",
			wantTimeoutMs:   5000, // default timeout applies
			wantCacheTTLSec: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := &Builder{}
			config := builder.buildForwardAuthConfig(tt.spec)

			if config.Address != tt.wantAddress {
				t.Errorf("Address = %v, want %v", config.Address, tt.wantAddress)
			}
			if tt.wantTimeoutMs > 0 && config.TimeoutMs != tt.wantTimeoutMs {
				t.Errorf("TimeoutMs = %v, want %v", config.TimeoutMs, tt.wantTimeoutMs)
			}
			if tt.wantCacheTTLSec > 0 && config.CacheTtlSeconds != tt.wantCacheTTLSec {
				t.Errorf("CacheTtlSeconds = %v, want %v", config.CacheTtlSeconds, tt.wantCacheTTLSec)
			}
			if tt.wantAuthHeaders > 0 && len(config.AuthHeaders) != tt.wantAuthHeaders {
				t.Errorf("AuthHeaders count = %v, want %v", len(config.AuthHeaders), tt.wantAuthHeaders)
			}
			if tt.wantRespHeaders > 0 && len(config.ResponseHeaders) != tt.wantRespHeaders {
				t.Errorf("ResponseHeaders count = %v, want %v", len(config.ResponseHeaders), tt.wantRespHeaders)
			}
		})
	}
}

func TestBuildOIDCConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name          string
		policy        *novaedgev1alpha1.ProxyPolicy
		secrets       []*corev1.Secret
		wantErr       bool
		wantIssuerURL string
		wantKeycloak  bool
	}{
		{
			name: "valid OIDC config",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					OIDC: &novaedgev1alpha1.OIDCPolicyConfig{
						Provider:    "generic",
						IssuerURL:   "https://idp.example.com",
						ClientID:    "test-client",
						RedirectURL: "https://app.example.com/callback",
						Scopes:      []string{"openid", "profile", "email"},
						ClientSecretRef: novaedgev1alpha1.LocalObjectReference{
							Name: "client-secret",
						},
						SessionSecretRef: novaedgev1alpha1.LocalObjectReference{
							Name: "session-secret",
						},
					},
				},
			},
			secrets: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "client-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"client-secret": []byte("secret123"),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "session-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"session-secret": []byte("session-key-32-bytes-long-val"),
					},
				},
			},
			wantErr:       false,
			wantIssuerURL: "https://idp.example.com",
			wantKeycloak:  false,
		},
		{
			name: "Keycloak OIDC config",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					OIDC: &novaedgev1alpha1.OIDCPolicyConfig{
						Provider:    "keycloak",
						ClientID:    "test-client",
						RedirectURL: "https://app.example.com/callback",
						ClientSecretRef: novaedgev1alpha1.LocalObjectReference{
							Name: "client-secret",
						},
						SessionSecretRef: novaedgev1alpha1.LocalObjectReference{
							Name: "session-secret",
						},
						Keycloak: &novaedgev1alpha1.KeycloakPolicyConfig{
							ServerURL:  "https://keycloak.example.com/",
							Realm:      "myrealm",
							RoleClaim:  "roles",
							GroupClaim: "groups",
						},
					},
				},
			},
			secrets: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "client-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"client-secret": []byte("secret123"),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "session-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"session-secret": []byte("session-key-32-bytes-long-val"),
					},
				},
			},
			wantErr:       false,
			wantIssuerURL: "https://keycloak.example.com/realms/myrealm",
			wantKeycloak:  true,
		},
		{
			name: "missing client secret",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					OIDC: &novaedgev1alpha1.OIDCPolicyConfig{
						Provider:  "generic",
						IssuerURL: "https://idp.example.com",
						ClientID:  "test-client",
						ClientSecretRef: novaedgev1alpha1.LocalObjectReference{
							Name: "missing-secret",
						},
						SessionSecretRef: novaedgev1alpha1.LocalObjectReference{
							Name: "session-secret",
						},
					},
				},
			},
			secrets: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "session-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"session-secret": []byte("session-key"),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "nil OIDC spec",
			policy: &novaedgev1alpha1.ProxyPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyPolicySpec{
					OIDC: nil,
				},
			},
			secrets: nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []runtime.Object{tt.policy}
			for _, s := range tt.secrets {
				objects = append(objects, s)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			builder := NewBuilder(fakeClient)
			config, err := builder.buildOIDCConfig(context.Background(), tt.policy)

			if (err != nil) != tt.wantErr {
				t.Errorf("buildOIDCConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if config.IssuerUrl != tt.wantIssuerURL {
					t.Errorf("IssuerUrl = %v, want %v", config.IssuerUrl, tt.wantIssuerURL)
				}
				if (config.Keycloak != nil) != tt.wantKeycloak {
					t.Errorf("Keycloak presence = %v, want %v", config.Keycloak != nil, tt.wantKeycloak)
				}
				if config.ClientSecret == "" {
					t.Error("ClientSecret should not be empty")
				}
				if len(config.SessionSecret) == 0 {
					t.Error("SessionSecret should not be empty")
				}
			}
		})
	}
}

func TestLoadSecretValue(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name      string
		secret    *corev1.Secret
		key       string
		wantValue string
		wantErr   bool
	}{
		{
			name: "load existing key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"key1": []byte("value1"),
					"key2": []byte("value2"),
				},
			},
			key:       "key1",
			wantValue: "value1",
			wantErr:   false,
		},
		{
			name: "missing key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"key1": []byte("value1"),
				},
			},
			key:     "missing-key",
			wantErr: true,
		},
		{
			name:    "missing secret",
			secret:  nil,
			key:     "any-key",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []runtime.Object
			if tt.secret != nil {
				objects = append(objects, tt.secret)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			builder := NewBuilder(fakeClient)
			value, err := builder.loadSecretValue(context.Background(), "default", "test-secret", tt.key)

			if (err != nil) != tt.wantErr {
				t.Errorf("loadSecretValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && value != tt.wantValue {
				t.Errorf("loadSecretValue() = %v, want %v", value, tt.wantValue)
			}
		})
	}
}

func TestLoadSecretBytes(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"binary-key": []byte{0x01, 0x02, 0x03, 0x04},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(secret).
		Build()

	builder := NewBuilder(fakeClient)

	t.Run("load binary data", func(t *testing.T) {
		data, err := builder.loadSecretBytes(context.Background(), "default", "test-secret", "binary-key")
		if err != nil {
			t.Errorf("loadSecretBytes() error = %v", err)
			return
		}
		if len(data) != 4 {
			t.Errorf("Expected 4 bytes, got %d", len(data))
		}
	})

	t.Run("missing key", func(t *testing.T) {
		_, err := builder.loadSecretBytes(context.Background(), "default", "test-secret", "missing-key")
		if err == nil {
			t.Error("Expected error for missing key")
		}
	})
}

func TestTrimTrailingSlash(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/", "https://example.com"},
		{"https://example.com//", "https://example.com"},
		{"https://example.com", "https://example.com"},
		{"/path/to/resource/", "/path/to/resource"},
		{"", ""},
		{"/", ""},
		{"///", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := trimTrailingSlash(tt.input)
			if got != tt.want {
				t.Errorf("trimTrailingSlash(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
