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
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestBuildSnapshot(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)

	// Create test resources
	vip := &novaedgev1alpha1.ProxyVIP{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-vip",
		},
		Spec: novaedgev1alpha1.ProxyVIPSpec{
			Address: "203.0.113.10/32",
			Mode:    novaedgev1alpha1.VIPModeBGP,
			Ports:   []int32{80, 443},
		},
		Status: novaedgev1alpha1.ProxyVIPStatus{
			AnnouncingNodes: []string{"test-node"},
		},
	}

	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef:           "test-vip",
			IngressClassName: "novaedge",
			Listeners: []novaedgev1alpha1.Listener{
				{
					Name:      "http",
					Port:      80,
					Protocol:  novaedgev1alpha1.ProtocolTypeHTTP,
					Hostnames: []string{"example.com"},
				},
			},
		},
	}

	// Create fake client with test resources
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(vip, gateway).
		WithStatusSubresource(vip).
		Build()

	// Create builder and build snapshot
	builder := NewBuilder(fakeClient)
	snapshot, err := builder.BuildSnapshot(context.Background(), "test-node")

	if err != nil {
		t.Fatalf("Failed to build snapshot: %v", err)
	}

	// Verify snapshot
	if snapshot == nil {
		t.Fatal("Snapshot is nil")
	}

	if snapshot.Version == "" {
		t.Error("Snapshot version is empty")
	}

	if snapshot.GenerationTime == 0 {
		t.Error("Snapshot generation time is zero")
	}

	if len(snapshot.VipAssignments) != 1 {
		t.Errorf("Expected 1 VIP assignment, got %d", len(snapshot.VipAssignments))
	} else {
		if snapshot.VipAssignments[0].VipName != "test-vip" {
			t.Errorf("Expected VIP name 'test-vip', got '%s'", snapshot.VipAssignments[0].VipName)
		}
		if snapshot.VipAssignments[0].Mode != pb.VIPMode_BGP {
			t.Errorf("Expected VIP mode BGP, got %v", snapshot.VipAssignments[0].Mode)
		}
	}

	if len(snapshot.Gateways) != 1 {
		t.Errorf("Expected 1 gateway, got %d", len(snapshot.Gateways))
	} else {
		if snapshot.Gateways[0].Name != "test-gateway" {
			t.Errorf("Expected gateway name 'test-gateway', got '%s'", snapshot.Gateways[0].Name)
		}
		if len(snapshot.Gateways[0].Listeners) != 1 {
			t.Errorf("Expected 1 listener, got %d", len(snapshot.Gateways[0].Listeners))
		}
	}
}

func TestGenerateVersion(t *testing.T) {
	builder := &Builder{}

	snapshot1 := &pb.ConfigSnapshot{
		GenerationTime: 1000,
		Gateways: []*pb.Gateway{
			{Name: "gw1", Namespace: "default"},
		},
	}

	snapshot2 := &pb.ConfigSnapshot{
		GenerationTime: 2000,
		Gateways: []*pb.Gateway{
			{Name: "gw1", Namespace: "default"},
		},
	}

	snapshot3 := &pb.ConfigSnapshot{
		GenerationTime: 1000,
		Gateways: []*pb.Gateway{
			{Name: "gw2", Namespace: "default"},
		},
	}

	v1 := builder.generateVersion(snapshot1)
	v2 := builder.generateVersion(snapshot2)
	v3 := builder.generateVersion(snapshot3)

	// Same content, different timestamps should have different full versions
	if v1 == v2 {
		t.Error("Expected different versions for different timestamps")
	}

	// Different content should have different hash parts
	if v1[len("1000-"):] == v3[len("1000-"):] {
		t.Error("Expected different hash parts for different content")
	}
}

func TestConvertVIPMode(t *testing.T) {
	tests := []struct {
		input    novaedgev1alpha1.VIPMode
		expected pb.VIPMode
	}{
		{novaedgev1alpha1.VIPModeL2ARP, pb.VIPMode_L2_ARP},
		{novaedgev1alpha1.VIPModeBGP, pb.VIPMode_BGP},
		{novaedgev1alpha1.VIPModeOSPF, pb.VIPMode_OSPF},
	}

	for _, tt := range tests {
		result := convertVIPMode(tt.input)
		if result != tt.expected {
			t.Errorf("convertVIPMode(%v) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestConvertProtocol(t *testing.T) {
	tests := []struct {
		input    novaedgev1alpha1.ProtocolType
		expected pb.Protocol
	}{
		{novaedgev1alpha1.ProtocolTypeHTTP, pb.Protocol_HTTP},
		{novaedgev1alpha1.ProtocolTypeHTTPS, pb.Protocol_HTTPS},
		{novaedgev1alpha1.ProtocolTypeTCP, pb.Protocol_TCP},
		{novaedgev1alpha1.ProtocolTypeTLS, pb.Protocol_TLS},
	}

	for _, tt := range tests {
		result := convertProtocol(tt.input)
		if result != tt.expected {
			t.Errorf("convertProtocol(%v) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestSnapshotCacheOperations(t *testing.T) {
	cache := NewCache()

	// Test Set and Get
	snapshot := &pb.ConfigSnapshot{
		Version:        "test-v1",
		GenerationTime: 1000,
	}

	cache.Set("node1", snapshot)
	retrieved, ok := cache.Get("node1")
	if !ok {
		t.Error("Expected to find snapshot in cache")
	}
	if retrieved.Version != "test-v1" {
		t.Errorf("Expected version 'test-v1', got '%s'", retrieved.Version)
	}

	// Test GetVersion
	version := cache.GetVersion("node1")
	if version != "test-v1" {
		t.Errorf("Expected version 'test-v1', got '%s'", version)
	}

	// Test cache size
	if cache.GetCacheSize() != 1 {
		t.Errorf("Expected cache size 1, got %d", cache.GetCacheSize())
	}

	// Test Clear
	cache.Clear()
	if cache.GetCacheSize() != 0 {
		t.Errorf("Expected cache size 0 after clear, got %d", cache.GetCacheSize())
	}
}

func TestBuildPoliciesSecurityHeaders(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)

	policy := &novaedgev1alpha1.ProxyPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sec-headers",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyPolicySpec{
			Type: novaedgev1alpha1.PolicyTypeSecurityHeaders,
			TargetRef: novaedgev1alpha1.TargetRef{
				Kind: "ProxyRoute",
				Name: "test-route",
			},
			SecurityHeaders: &novaedgev1alpha1.SecurityHeadersConfig{
				HSTS: &novaedgev1alpha1.HSTSConfig{
					Enabled:           true,
					MaxAge:            31536000,
					IncludeSubDomains: true,
					Preload:           true,
				},
				XFrameOptions:       "DENY",
				XContentTypeOptions: true,
				XXSSProtection:      "1; mode=block",
				ReferrerPolicy:      "no-referrer",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy).
		Build()

	builder := NewBuilder(fakeClient)
	snapshot, err := builder.BuildSnapshot(context.Background(), "test-node")
	if err != nil {
		t.Fatalf("Failed to build snapshot: %v", err)
	}

	if len(snapshot.Policies) != 1 {
		t.Fatalf("Expected 1 policy, got %d", len(snapshot.Policies))
	}

	p := snapshot.Policies[0]

	// Verify the policy type is correctly mapped (not UNSPECIFIED)
	if p.Type != pb.PolicyType_SECURITY_HEADERS {
		t.Errorf("Expected SECURITY_HEADERS, got %v", p.Type)
	}

	// Verify SecurityHeaders config is serialized
	if p.SecurityHeaders == nil {
		t.Fatal("SecurityHeaders config is nil")
	}

	if p.SecurityHeaders.Hsts == nil {
		t.Fatal("HSTS config is nil")
	}

	if !p.SecurityHeaders.Hsts.Enabled {
		t.Error("Expected HSTS enabled")
	}

	if p.SecurityHeaders.Hsts.MaxAgeSeconds != 31536000 {
		t.Errorf("Expected MaxAgeSeconds 31536000, got %d", p.SecurityHeaders.Hsts.MaxAgeSeconds)
	}

	if !p.SecurityHeaders.Hsts.IncludeSubdomains {
		t.Error("Expected IncludeSubdomains true")
	}

	if !p.SecurityHeaders.Hsts.Preload {
		t.Error("Expected Preload true")
	}

	if p.SecurityHeaders.XFrameOptions != "DENY" {
		t.Errorf("Expected XFrameOptions DENY, got %s", p.SecurityHeaders.XFrameOptions)
	}

	if !p.SecurityHeaders.XContentTypeOptions {
		t.Error("Expected XContentTypeOptions true")
	}

	if p.SecurityHeaders.XXssProtection != "1; mode=block" {
		t.Errorf("Expected XXssProtection '1; mode=block', got %s", p.SecurityHeaders.XXssProtection)
	}

	if p.SecurityHeaders.ReferrerPolicy != "no-referrer" {
		t.Errorf("Expected ReferrerPolicy 'no-referrer', got %s", p.SecurityHeaders.ReferrerPolicy)
	}
}

func TestResolveEndpointsTargetPort(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	// Service with port 80 -> targetPort 8080
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt32(8080),
				},
			},
		},
	}

	ready := true
	portName := "http"
	port8080 := int32(8080)
	es := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-svc-abc",
			Namespace: "default",
			Labels: map[string]string{
				"kubernetes.io/service-name": "my-svc",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses:  []string{"10.0.0.1"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			},
		},
		Ports: []discoveryv1.EndpointPort{
			{
				Name: &portName,
				Port: &port8080,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, es).
		Build()

	builder := NewBuilder(fakeClient)
	serviceRef := &novaedgev1alpha1.ServiceReference{
		Name: "my-svc",
		Port: 80, // Service port, NOT targetPort
	}

	result, err := builder.resolveServiceEndpoints(context.Background(), serviceRef, "default")
	if err != nil {
		t.Fatalf("resolveServiceEndpoints failed: %v", err)
	}

	if len(result.Endpoints) != 1 {
		t.Fatalf("Expected 1 endpoint, got %d", len(result.Endpoints))
	}

	// The endpoint port should be 8080 (the targetPort), not 80 (the service port)
	if result.Endpoints[0].Port != 8080 {
		t.Errorf("Expected endpoint port 8080 (targetPort), got %d", result.Endpoints[0].Port)
	}

	if result.Endpoints[0].Address != "10.0.0.1" {
		t.Errorf("Expected address 10.0.0.1, got %s", result.Endpoints[0].Address)
	}
}

func TestResolveEndpointsUnnamedPort(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	// Service with unnamed port: port 80 -> targetPort 8080
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unnamed-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       80,
					TargetPort: intstr.FromInt32(8080),
				},
			},
		},
	}

	ready := true
	port8080 := int32(8080)
	es := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unnamed-svc-abc",
			Namespace: "default",
			Labels: map[string]string{
				"kubernetes.io/service-name": "unnamed-svc",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses:  []string{"10.0.0.2"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			},
		},
		Ports: []discoveryv1.EndpointPort{
			{
				Port: &port8080,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, es).
		Build()

	builder := NewBuilder(fakeClient)
	serviceRef := &novaedgev1alpha1.ServiceReference{
		Name: "unnamed-svc",
		Port: 80,
	}

	result, err := builder.resolveServiceEndpoints(context.Background(), serviceRef, "default")
	if err != nil {
		t.Fatalf("resolveServiceEndpoints failed: %v", err)
	}

	if len(result.Endpoints) != 1 {
		t.Fatalf("Expected 1 endpoint, got %d", len(result.Endpoints))
	}

	// Should resolve to 8080 via targetPort number matching or single-port fallback
	if result.Endpoints[0].Port != 8080 {
		t.Errorf("Expected endpoint port 8080 (targetPort), got %d", result.Endpoints[0].Port)
	}
}
