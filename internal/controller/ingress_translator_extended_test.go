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

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestGetVIPRef(t *testing.T) {
	tests := []struct {
		name      string
		ingress   *networkingv1.Ingress
		expectVIP string
	}{
		{
			name: "with vip-ref annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ingress",
					Namespace:   "default",
					Annotations: map[string]string{AnnotationVIPRef: "custom-vip"},
				},
			},
			expectVIP: "custom-vip",
		},
		{
			name: "without vip-ref annotation - uses default",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ingress",
					Namespace: "default",
				},
			},
			expectVIP: DefaultVIPRef,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.getVIPRef(tt.ingress)
			if result != tt.expectVIP {
				t.Errorf("getVIPRef() = %v, want %v", result, tt.expectVIP)
			}
		})
	}
}

func TestGetIngressClassName(t *testing.T) {
	tests := []struct {
		name     string
		ingress  *networkingv1.Ingress
		expected string
	}{
		{
			name: "from spec",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: networkingv1.IngressSpec{
					IngressClassName: ptr.To("nginx"),
				},
			},
			expected: "nginx",
		},
		{
			name: "from annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": "traefik",
					},
				},
			},
			expected: "traefik",
		},
		{
			name: "spec takes precedence over annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": "traefik",
					},
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: ptr.To("nginx"),
				},
			},
			expected: "nginx",
		},
		{
			name: "no ingress class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.getIngressClassName(tt.ingress)
			if result != tt.expected {
				t.Errorf("getIngressClassName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetLBPolicy(t *testing.T) {
	tests := []struct {
		name         string
		ingress      *networkingv1.Ingress
		expectedName string
	}{
		{
			name: "roundrobin policy",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationLoadBalancing: "roundrobin"},
				},
			},
			expectedName: "RoundRobin",
		},
		{
			name: "p2c policy",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationLoadBalancing: "p2c"},
				},
			},
			expectedName: "P2C",
		},
		{
			name: "ewma policy",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationLoadBalancing: "ewma"},
				},
			},
			expectedName: "EWMA",
		},
		{
			name: "ringhash policy",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationLoadBalancing: "ringhash"},
				},
			},
			expectedName: "RingHash",
		},
		{
			name: "maglev policy",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationLoadBalancing: "maglev"},
				},
			},
			expectedName: "Maglev",
		},
		{
			name: "case insensitive",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationLoadBalancing: "ROUNDROBIN"},
				},
			},
			expectedName: "RoundRobin",
		},
		{
			name: "no annotation - defaults to roundrobin",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expectedName: "RoundRobin",
		},
		{
			name: "unknown policy - defaults to roundrobin",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationLoadBalancing: "unknown"},
				},
			},
			expectedName: "RoundRobin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.getLBPolicy(tt.ingress)
			if string(result) != tt.expectedName {
				t.Errorf("getLBPolicy() = %v, want %v", result, tt.expectedName)
			}
		})
	}
}

func TestCopyLabels(t *testing.T) {
	tests := []struct {
		name          string
		ingress       *networkingv1.Ingress
		expectedKeys  []string
		expectedValue string
	}{
		{
			name: "copies existing labels and adds tracking",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-ingress",
					Labels: map[string]string{"app": "myapp", "env": "prod"},
				},
			},
			expectedKeys:  []string{"app", "env", "novaedge.io/ingress-name", "novaedge.io/managed-by"},
			expectedValue: "myapp",
		},
		{
			name: "no existing labels - adds tracking only",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ingress",
				},
			},
			expectedKeys:  []string{"novaedge.io/ingress-name", "novaedge.io/managed-by"},
			expectedValue: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.copyLabels(tt.ingress)

			for _, key := range tt.expectedKeys {
				if _, exists := result[key]; !exists {
					t.Errorf("expected key %q not found in result", key)
				}
			}

			if tt.expectedValue != "" {
				if result["app"] != tt.expectedValue {
					t.Errorf("result[app] = %v, want %v", result["app"], tt.expectedValue)
				}
			}

			// Verify tracking labels
			if result["novaedge.io/ingress-name"] != tt.ingress.Name {
				t.Errorf("ingress-name label = %v, want %v", result["novaedge.io/ingress-name"], tt.ingress.Name)
			}
			if result["novaedge.io/managed-by"] != "ingress-controller" {
				t.Errorf("managed-by label = %v, want ingress-controller", result["novaedge.io/managed-by"])
			}
		})
	}
}

func TestShouldSSLRedirect(t *testing.T) {
	tests := []struct {
		name     string
		ingress  *networkingv1.Ingress
		expected bool
	}{
		{
			name: "force ssl redirect annotation true",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationForceSSLRedirect: "true"},
				},
			},
			expected: true,
		},
		{
			name: "ssl redirect annotation true",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationSSLRedirect: "true"},
				},
			},
			expected: true,
		},
		{
			name: "ssl redirect annotation false",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationSSLRedirect: "false"},
				},
				Spec: networkingv1.IngressSpec{
					TLS: []networkingv1.IngressTLS{{Hosts: []string{"example.com"}, SecretName: "tls-secret"}},
				},
			},
			expected: false,
		},
		{
			name: "default with TLS configured",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: networkingv1.IngressSpec{
					TLS: []networkingv1.IngressTLS{{Hosts: []string{"example.com"}, SecretName: "tls-secret"}},
				},
			},
			expected: true,
		},
		{
			name: "default without TLS",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expected: false,
		},
		{
			name: "force takes precedence over ssl-redirect",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						AnnotationForceSSLRedirect: "true",
						AnnotationSSLRedirect:      "false",
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.shouldSSLRedirect(tt.ingress)
			if result != tt.expected {
				t.Errorf("shouldSSLRedirect() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetWhitelistSourceRanges(t *testing.T) {
	tests := []struct {
		name     string
		ingress  *networkingv1.Ingress
		expected []string
	}{
		{
			name: "single range",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationWhitelistSourceRange: "10.0.0.0/8"},
				},
			},
			expected: []string{"10.0.0.0/8"},
		},
		{
			name: "multiple ranges",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationWhitelistSourceRange: "10.0.0.0/8, 192.168.0.0/16,172.16.0.0/12"},
				},
			},
			expected: []string{"10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12"},
		},
		{
			name: "no annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expected: nil,
		},
		{
			name: "empty annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationWhitelistSourceRange: ""},
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.getWhitelistSourceRanges(tt.ingress)
			if len(result) != len(tt.expected) {
				t.Errorf("getWhitelistSourceRanges() length = %v, want %v", len(result), len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("getWhitelistSourceRanges()[%d] = %v, want %v", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestGetProxyBodySize(t *testing.T) {
	tests := []struct {
		name     string
		ingress  *networkingv1.Ingress
		expected int64
	}{
		{
			name: "bytes only",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationProxyBodySize: "1024"},
				},
			},
			expected: 1024,
		},
		{
			name: "kilobytes",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationProxyBodySize: "10k"},
				},
			},
			expected: 10 * 1024,
		},
		{
			name: "megabytes",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationProxyBodySize: "50m"},
				},
			},
			expected: 50 * 1024 * 1024,
		},
		{
			name: "gigabytes",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationProxyBodySize: "1g"},
				},
			},
			expected: 1 * 1024 * 1024 * 1024,
		},
		{
			name: "uppercase suffix",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationProxyBodySize: "10M"},
				},
			},
			expected: 10 * 1024 * 1024,
		},
		{
			name: "no annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expected: 0,
		},
		{
			name: "invalid value",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationProxyBodySize: "invalid"},
				},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.getProxyBodySize(tt.ingress)
			if result != tt.expected {
				t.Errorf("getProxyBodySize() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetRequestHeaders(t *testing.T) {
	tests := []struct {
		name     string
		ingress  *networkingv1.Ingress
		expected map[string]string
	}{
		{
			name: "valid json headers",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationRequestHeaders: `{"X-Custom-Header": "value", "X-Another": "test"}`},
				},
			},
			expected: map[string]string{"X-Custom-Header": "value", "X-Another": "test"},
		},
		{
			name: "no annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expected: nil,
		},
		{
			name: "invalid json",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationRequestHeaders: "not-json"},
				},
			},
			expected: nil,
		},
		{
			name: "empty annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationRequestHeaders: ""},
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.getRequestHeaders(tt.ingress)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("getRequestHeaders() = %v, want nil", result)
				}
				return
			}
			if len(result) != len(tt.expected) {
				t.Errorf("getRequestHeaders() length = %v, want %v", len(result), len(tt.expected))
				return
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("getRequestHeaders()[%v] = %v, want %v", k, result[k], v)
				}
			}
		})
	}
}

func TestGetRemoveRequestHeaders(t *testing.T) {
	tests := []struct {
		name     string
		ingress  *networkingv1.Ingress
		expected []string
	}{
		{
			name: "single header",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationRemoveRequestHeaders: "X-Remove-Me"},
				},
			},
			expected: []string{"X-Remove-Me"},
		},
		{
			name: "multiple headers",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationRemoveRequestHeaders: "X-Header-1, X-Header-2, X-Header-3"},
				},
			},
			expected: []string{"X-Header-1", "X-Header-2", "X-Header-3"},
		},
		{
			name: "no annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expected: nil,
		},
		{
			name: "empty annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationRemoveRequestHeaders: ""},
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.getRemoveRequestHeaders(tt.ingress)
			if len(result) != len(tt.expected) {
				t.Errorf("getRemoveRequestHeaders() length = %v, want %v", len(result), len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("getRemoveRequestHeaders()[%d] = %v, want %v", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestGetCanaryWeight(t *testing.T) {
	tests := []struct {
		name     string
		ingress  *networkingv1.Ingress
		expected int32
	}{
		{
			name: "valid weight 50",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationCanaryWeight: "50"},
				},
			},
			expected: 50,
		},
		{
			name: "valid weight 0",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationCanaryWeight: "0"},
				},
			},
			expected: 0,
		},
		{
			name: "valid weight 100",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationCanaryWeight: "100"},
				},
			},
			expected: 100,
		},
		{
			name: "weight over 100 - invalid",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationCanaryWeight: "150"},
				},
			},
			expected: 0,
		},
		{
			name: "negative weight - invalid",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationCanaryWeight: "-10"},
				},
			},
			expected: 0,
		},
		{
			name: "no annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expected: 0,
		},
		{
			name: "invalid value",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationCanaryWeight: "invalid"},
				},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.getCanaryWeight(tt.ingress)
			if result != tt.expected {
				t.Errorf("getCanaryWeight() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetCanaryHeader(t *testing.T) {
	tests := []struct {
		name            string
		ingress         *networkingv1.Ingress
		expectedHeader  string
		expectedValue   string
	}{
		{
			name: "header with custom value",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						AnnotationCanaryHeader:      "X-Canary",
						AnnotationCanaryHeaderValue: "v1",
					},
				},
			},
			expectedHeader:  "X-Canary",
			expectedValue:   "v1",
		},
		{
			name: "header with default value",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationCanaryHeader: "X-Canary"},
				},
			},
			expectedHeader:  "X-Canary",
			expectedValue:   "true",
		},
		{
			name: "no canary header",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expectedHeader:  "",
			expectedValue:   "",
		},
		{
			name: "empty canary header",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationCanaryHeader: ""},
				},
			},
			expectedHeader:  "",
			expectedValue:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			header, value := translator.getCanaryHeader(tt.ingress)
			if header != tt.expectedHeader {
				t.Errorf("getCanaryHeader() header = %v, want %v", header, tt.expectedHeader)
			}
			if value != tt.expectedValue {
				t.Errorf("getCanaryHeader() value = %v, want %v", value, tt.expectedValue)
			}
		})
	}
}

func TestUseRegexPathMatching(t *testing.T) {
	tests := []struct {
		name     string
		ingress  *networkingv1.Ingress
		expected bool
	}{
		{
			name: "enabled",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationUseRegex: "true"},
				},
			},
			expected: true,
		},
		{
			name: "disabled",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationUseRegex: "false"},
				},
			},
			expected: false,
		},
		{
			name: "no annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expected: false,
		},
		{
			name: "case insensitive true",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationUseRegex: "TRUE"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.useRegexPathMatching(tt.ingress)
			if result != tt.expected {
				t.Errorf("useRegexPathMatching() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetMirrorConfig(t *testing.T) {
	tests := []struct {
		name            string
		ingress         *networkingv1.Ingress
		expectBackend   bool
		backendName     string
		expectPercent   bool
		percentValue    int32
	}{
		{
			name: "with backend and percent",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						AnnotationMirrorBackend:  "mirror-backend",
						AnnotationMirrorPercent:  "50",
					},
				},
			},
			expectBackend: true,
			backendName:   "mirror-backend",
			expectPercent: true,
			percentValue:  50,
		},
		{
			name: "with backend only",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationMirrorBackend: "mirror-backend"},
				},
			},
			expectBackend: true,
			backendName:   "mirror-backend",
			expectPercent: false,
		},
		{
			name: "no mirror config",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expectBackend: false,
			expectPercent: false,
		},
		{
			name: "invalid percent - out of range",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						AnnotationMirrorBackend: "mirror-backend",
						AnnotationMirrorPercent:  "150",
					},
				},
			},
			expectBackend: true,
			backendName:   "mirror-backend",
			expectPercent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			backend, percent := translator.getMirrorConfig(tt.ingress)

			if tt.expectBackend {
				if backend == nil {
					t.Error("expected backend, got nil")
				} else if backend.Name != tt.backendName {
					t.Errorf("backend.Name = %v, want %v", backend.Name, tt.backendName)
				}
			} else if backend != nil {
				t.Errorf("expected no backend, got %v", backend)
			}

			if tt.expectPercent {
				if percent == nil {
					t.Error("expected percent, got nil")
				} else if *percent != tt.percentValue {
					t.Errorf("percent = %v, want %v", *percent, tt.percentValue)
				}
			} else if percent != nil {
				t.Errorf("expected no percent, got %v", *percent)
			}
		})
	}
}

func TestGetTracingConfig(t *testing.T) {
	tests := []struct {
		name          string
		ingress       *networkingv1.Ingress
		expectNil     bool
		expectEnabled bool
		expectRate    int32
	}{
		{
			name: "tracing enabled",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationTracingEnabled: "true"},
				},
			},
			expectNil:     false,
			expectEnabled: true,
		},
		{
			name: "tracing disabled",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationTracingEnabled: "false"},
				},
			},
			expectNil:     false,
			expectEnabled: false,
		},
		{
			name: "with sampling rate",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationTracingSamplingRate: "50"},
				},
			},
			expectNil:     false,
			expectEnabled: true,
			expectRate:    50,
		},
		{
			name: "no tracing config",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.getTracingConfig(tt.ingress)

			if tt.expectNil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Error("expected config, got nil")
				return
			}

			if result.Enabled != tt.expectEnabled {
				t.Errorf("Enabled = %v, want %v", result.Enabled, tt.expectEnabled)
			}

			if tt.expectRate > 0 {
				if result.SamplingRate == nil || *result.SamplingRate != tt.expectRate {
					t.Errorf("SamplingRate = %v, want %v", result.SamplingRate, tt.expectRate)
				}
			}
		})
	}
}

func TestGetAccessLogConfig(t *testing.T) {
	tests := []struct {
		name          string
		ingress       *networkingv1.Ingress
		expectNil     bool
		expectEnabled bool
		expectFormat  string
	}{
		{
			name: "access log enabled json",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationAccessLogEnabled: "true", AnnotationAccessLogFormat: "json"},
				},
			},
			expectNil:     false,
			expectEnabled: true,
			expectFormat:  "json",
		},
		{
			name: "access log disabled",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationAccessLogEnabled: "false"},
				},
			},
			expectNil:     false,
			expectEnabled: false,
			expectFormat:  "json", // default
		},
		{
			name: "access log with common format",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationAccessLogFormat: "common"},
				},
			},
			expectNil:     false,
			expectEnabled: true,
			expectFormat:  "common",
		},
		{
			name: "no access log config",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.getAccessLogConfig(tt.ingress)

			if tt.expectNil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Error("expected config, got nil")
				return
			}

			if result.Enabled != tt.expectEnabled {
				t.Errorf("Enabled = %v, want %v", result.Enabled, tt.expectEnabled)
			}

			if result.Format != tt.expectFormat {
				t.Errorf("Format = %v, want %v", result.Format, tt.expectFormat)
			}
		})
	}
}

func TestGetCustomErrorPages(t *testing.T) {
	tests := []struct {
		name      string
		ingress   *networkingv1.Ingress
		expectNil bool
	}{
		{
			name: "valid error pages json",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationCustomErrorPages: `[{"codes":[404,500],"path":"/error.html"}]`},
				},
			},
			expectNil: false,
		},
		{
			name: "invalid json",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationCustomErrorPages: "invalid"},
				},
			},
			expectNil: true,
		},
		{
			name: "no annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expectNil: true,
		},
		{
			name: "empty annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test",
					Annotations: map[string]string{AnnotationCustomErrorPages: ""},
				},
			},
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewIngressTranslator("default")
			result := translator.getCustomErrorPages(tt.ingress)

			if tt.expectNil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			} else {
				if result == nil {
					t.Error("expected error pages, got nil")
				}
			}
		})
	}
}

func TestGenerateNames(t *testing.T) {
	translator := NewIngressTranslator("default")
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ingress", Namespace: "test-ns"},
	}

	t.Run("gateway name", func(t *testing.T) {
		result := translator.generateGatewayName(ingress)
		expected := "my-ingress-gateway"
		if result != expected {
			t.Errorf("generateGatewayName() = %v, want %v", result, expected)
		}
	})

	t.Run("route name", func(t *testing.T) {
		result := translator.generateRouteName(ingress, 0)
		expected := "my-ingress-route-0"
		if result != expected {
			t.Errorf("generateRouteName() = %v, want %v", result, expected)
		}
	})

	t.Run("backend name", func(t *testing.T) {
		result := translator.generateBackendName(ingress, 1, 2)
		expected := "my-ingress-backend-1-2"
		if result != expected {
			t.Errorf("generateBackendName() = %v, want %v", result, expected)
		}
	})

	t.Run("default backend name", func(t *testing.T) {
		result := translator.generateDefaultBackendName(ingress)
		expected := "my-ingress-backend-default"
		if result != expected {
			t.Errorf("generateDefaultBackendName() = %v, want %v", result, expected)
		}
	})
}
