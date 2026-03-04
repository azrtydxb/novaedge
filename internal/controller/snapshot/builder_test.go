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

const (
	testPortNameHTTP  = "http"
	testEndpointAddr1 = "10.0.0.1"
	testLabelTrue     = "true"
)

// newBuildContextForTest creates a buildContext with the given backends, services, and nodes.
func newBuildContextForTest(backends []novaedgev1alpha1.ProxyBackend, services []corev1.Service) *buildContext {
	bc := &buildContext{
		nodes:      make(map[string]*corev1.Node),
		secrets:    make(map[string]*corev1.Secret),
		configMaps: make(map[string]*corev1.ConfigMap),
	}
	bc.backends = backends
	bc.services = services
	return bc
}

func TestBuildSnapshot(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

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
					Name:      testPortNameHTTP,
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

	// Same content, different timestamps should produce the same version (content-based)
	if v1 != v2 {
		t.Errorf("Expected same version for same content regardless of timestamp, got %q and %q", v1, v2)
	}

	// Different content should produce different versions
	if v1 == v3 {
		t.Error("Expected different versions for different content")
	}

	// BGP config change should produce a different version (same VIP name/address, different LocalAs)
	snapshotBGP1 := &pb.ConfigSnapshot{
		VipAssignments: []*pb.VIPAssignment{
			{
				VipName: "vip-bgp",
				Address: "10.0.0.100",
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65000,
					RouterId: "1.2.3.4",
				},
			},
		},
	}
	snapshotBGP2 := &pb.ConfigSnapshot{
		VipAssignments: []*pb.VIPAssignment{
			{
				VipName: "vip-bgp",
				Address: "10.0.0.100",
				BgpConfig: &pb.BGPConfig{
					LocalAs:  65012,
					RouterId: "1.2.3.4",
				},
			},
		},
	}

	vBGP1 := builder.generateVersion(snapshotBGP1)
	vBGP2 := builder.generateVersion(snapshotBGP2)

	if vBGP1 == vBGP2 {
		t.Error("Expected different versions when BGP LocalAs changes, but got the same version")
	}

	// WAN links should be hashed even with zero L4 listeners
	snapshotWAN := &pb.ConfigSnapshot{
		WanLinks: []*pb.WANLink{
			{Namespace: "default", Name: "wan1"},
		},
	}
	snapshotNoWAN := &pb.ConfigSnapshot{}

	vWAN := builder.generateVersion(snapshotWAN)
	vNoWAN := builder.generateVersion(snapshotNoWAN)

	if vWAN == vNoWAN {
		t.Error("Expected different versions when WAN links differ with no L4 listeners")
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
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

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
					Name:       testPortNameHTTP,
					Port:       80,
					TargetPort: intstr.FromInt32(8080),
				},
			},
		},
	}

	ready := true
	portName := testPortNameHTTP
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
				Addresses:  []string{testEndpointAddr1},
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

	bc := newBuildContextForTest(nil, nil)
	result, err := builder.resolveServiceEndpoints(context.Background(), serviceRef, "default", bc)
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

	if result.Endpoints[0].Address != testEndpointAddr1 {
		t.Errorf("Expected address 10.0.0.1, got %s", result.Endpoints[0].Address)
	}
}

// TestResolveServiceEndpointsNilReadyTreatedAsReady verifies that endpoints
// with a nil Ready condition are treated as ready, per the Kubernetes API
// convention (nil means ready).
func TestResolveServiceEndpointsNilReadyTreatedAsReady(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-svc",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       testPortNameHTTP,
					Port:       80,
					TargetPort: intstr.FromInt32(8080),
				},
			},
		},
	}

	readyTrue := true
	readyFalse := false
	portName := testPortNameHTTP
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
				// nil Ready — should be treated as ready per K8s convention
				Addresses:  []string{"10.0.0.1"},
				Conditions: discoveryv1.EndpointConditions{Ready: nil},
			},
			{
				// explicit true — should be ready
				Addresses:  []string{"10.0.0.2"},
				Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue},
			},
			{
				// explicit false — should NOT be ready
				Addresses:  []string{"10.0.0.3"},
				Conditions: discoveryv1.EndpointConditions{Ready: &readyFalse},
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
		Port: 80,
	}

	bc := newBuildContextForTest(nil, nil)
	result, err := builder.resolveServiceEndpoints(context.Background(), serviceRef, "default", bc)
	if err != nil {
		t.Fatalf("resolveServiceEndpoints failed: %v", err)
	}

	if len(result.Endpoints) != 3 {
		t.Fatalf("Expected 3 endpoints, got %d", len(result.Endpoints))
	}

	// Endpoint with nil Ready should be treated as ready
	if !result.Endpoints[0].Ready {
		t.Errorf("Endpoint 0 (nil Ready): expected Ready=true, got Ready=false")
	}

	// Endpoint with explicit true should be ready
	if !result.Endpoints[1].Ready {
		t.Errorf("Endpoint 1 (Ready=true): expected Ready=true, got Ready=false")
	}

	// Endpoint with explicit false should NOT be ready
	if result.Endpoints[2].Ready {
		t.Errorf("Endpoint 2 (Ready=false): expected Ready=false, got Ready=true")
	}
}

func TestBuildClustersECMPAutoPromoteToMaglev(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	backend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			LBPolicy: "", // unspecified
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backend).Build()
	builder := NewBuilder(fakeClient)
	bc := newBuildContextForTest([]novaedgev1alpha1.ProxyBackend{*backend}, nil)

	clusters, _, err := builder.buildClusters(context.Background(), map[string]struct{}{"default/test-backend": {}}, bc)
	if err != nil {
		t.Fatalf("buildClusters failed: %v", err)
	}

	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}

	if clusters[0].LbPolicy != pb.LoadBalancingPolicy_MAGLEV {
		t.Errorf("expected MAGLEV, got %v", clusters[0].LbPolicy)
	}
}

func TestBuildClustersECMPAutoPromotesRoundRobin(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	backend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backend).Build()
	builder := NewBuilder(fakeClient)
	bc := newBuildContextForTest([]novaedgev1alpha1.ProxyBackend{*backend}, nil)

	clusters, _, err := builder.buildClusters(context.Background(), map[string]struct{}{"default/test-backend": {}}, bc)
	if err != nil {
		t.Fatalf("buildClusters failed: %v", err)
	}

	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster (auto-promoted), got %d", len(clusters))
	}
	if clusters[0].LbPolicy != pb.LoadBalancingPolicy_MAGLEV {
		t.Errorf("expected Maglev after auto-promotion, got %s", clusters[0].LbPolicy.String())
	}
}

func TestBuildClustersECMPRejectsNonHashLB(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	backend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			LBPolicy: novaedgev1alpha1.LBPolicyLeastConn,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backend).Build()
	builder := NewBuilder(fakeClient)
	bc := newBuildContextForTest([]novaedgev1alpha1.ProxyBackend{*backend}, nil)

	clusters, _, err := builder.buildClusters(context.Background(), map[string]struct{}{"default/test-backend": {}}, bc)
	if err != nil {
		t.Fatalf("buildClusters failed: %v", err)
	}

	if len(clusters) != 0 {
		t.Errorf("expected 0 clusters (rejected), got %d", len(clusters))
	}
}

func TestBuildClustersNonECMPAllowsAnyLB(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	backend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backend).Build()
	builder := NewBuilder(fakeClient)
	bc := newBuildContextForTest([]novaedgev1alpha1.ProxyBackend{*backend}, nil)

	clusters, _, err := builder.buildClusters(context.Background(), nil, bc)
	if err != nil {
		t.Fatalf("buildClusters failed: %v", err)
	}

	if len(clusters) != 1 {
		t.Errorf("expected 1 cluster, got %d", len(clusters))
	}
}

func TestBuildClustersECMPAllowsHashLB(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	backend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backend",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			LBPolicy: novaedgev1alpha1.LBPolicyMaglev,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backend).Build()
	builder := NewBuilder(fakeClient)
	bc := newBuildContextForTest([]novaedgev1alpha1.ProxyBackend{*backend}, nil)

	clusters, _, err := builder.buildClusters(context.Background(), map[string]struct{}{"default/test-backend": {}}, bc)
	if err != nil {
		t.Fatalf("buildClusters failed: %v", err)
	}

	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}

	if clusters[0].LbPolicy != pb.LoadBalancingPolicy_MAGLEV {
		t.Errorf("expected MAGLEV, got %v", clusters[0].LbPolicy)
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

	bc := newBuildContextForTest(nil, nil)
	result, err := builder.resolveServiceEndpoints(context.Background(), serviceRef, "default", bc)
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

func TestBuildInternalServices(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	ready := true
	port80 := int32(80)
	portName := testPortNameHTTP
	protocol := corev1.ProtocolTCP

	// Mesh-enabled service with endpoints
	meshSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web",
			Namespace: "app",
			Annotations: map[string]string{
				"novaedge.io/mesh": "enabled",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.10",
			Ports: []corev1.ServicePort{
				{Name: testPortNameHTTP, Port: 80, TargetPort: intstr.FromInt32(8080), Protocol: corev1.ProtocolTCP},
			},
		},
	}

	meshES := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-abc",
			Namespace: "app",
			Labels:    map[string]string{"kubernetes.io/service-name": "web"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{Name: &portName, Port: &port80, Protocol: &protocol},
		},
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"10.244.0.5"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
			{Addresses: []string{"10.244.0.6"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
		},
	}

	// Non-mesh service (no annotation)
	plainSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db",
			Namespace: "app",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.20",
			Ports: []corev1.ServicePort{
				{Name: "postgres", Port: 5432, Protocol: corev1.ProtocolTCP},
			},
		},
	}

	// Headless mesh service (should be skipped)
	headlessSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headless",
			Namespace: "app",
			Annotations: map[string]string{
				"novaedge.io/mesh": "enabled",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{Name: testPortNameHTTP, Port: 80, Protocol: corev1.ProtocolTCP},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(meshSvc, plainSvc, headlessSvc, meshES).
		Build()

	builder := NewBuilder(fakeClient)
	bc := newBuildContextForTest(nil, []corev1.Service{*meshSvc, *plainSvc, *headlessSvc})
	services := builder.buildInternalServices(context.Background(), bc)

	// Only the mesh-enabled non-headless service should be included
	if len(services) != 1 {
		t.Fatalf("Expected 1 internal service, got %d", len(services))
	}

	svc := services[0]
	if svc.Name != "web" || svc.Namespace != "app" {
		t.Errorf("Expected web/app, got %s/%s", svc.Name, svc.Namespace)
	}
	if svc.ClusterIp != "10.96.0.10" {
		t.Errorf("Expected ClusterIP 10.96.0.10, got %s", svc.ClusterIp)
	}
	if !svc.MeshEnabled {
		t.Error("Expected MeshEnabled=true")
	}
	if len(svc.Ports) != 1 {
		t.Fatalf("Expected 1 port, got %d", len(svc.Ports))
	}
	if svc.Ports[0].Port != 80 || svc.Ports[0].Name != testPortNameHTTP {
		t.Errorf("Expected port 80/http, got %d/%s", svc.Ports[0].Port, svc.Ports[0].Name)
	}
	if len(svc.Endpoints) != 2 {
		t.Fatalf("Expected 2 endpoints, got %d", len(svc.Endpoints))
	}
	if svc.LbPolicy != pb.LoadBalancingPolicy_ROUND_ROBIN {
		t.Errorf("Expected ROUND_ROBIN LB policy, got %s", svc.LbPolicy.String())
	}
}

// mockFederationProvider is a test double for FederationStateProvider.
type mockFederationProvider struct {
	active          bool
	federationID    string
	localMember     string
	vectorClock     map[string]int64
	remoteEndpoints map[string][]*pb.ServiceEndpoints // key: "namespace/serviceName"
}

func (m *mockFederationProvider) GetFederationID() string          { return m.federationID }
func (m *mockFederationProvider) GetLocalMemberName() string       { return m.localMember }
func (m *mockFederationProvider) GetVectorClock() map[string]int64 { return m.vectorClock }
func (m *mockFederationProvider) IsActive() bool                   { return m.active }
func (m *mockFederationProvider) GetRemoteEndpoints(namespace, serviceName string) []*pb.ServiceEndpoints {
	key := namespace + "/" + serviceName
	return m.remoteEndpoints[key]
}

func TestBuildClustersFederationInactive(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	ready := true
	port8080 := int32(8080)
	portName := testPortNameHTTP

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: testPortNameHTTP, Port: 80, TargetPort: intstr.FromInt32(8080)},
			},
		},
	}
	es := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-svc-abc", Namespace: "default",
			Labels: map[string]string{"kubernetes.io/service-name": "my-svc"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{testEndpointAddr1}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
		},
		Ports: []discoveryv1.EndpointPort{{Name: &portName, Port: &port8080}},
	}
	backend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "test-backend", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			ServiceRef: &novaedgev1alpha1.ServiceReference{Name: "my-svc", Port: 80},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(svc, es, backend).Build()
	builder := NewBuilder(fakeClient)

	// Federation provider is set but inactive
	builder.SetFederationProvider(&mockFederationProvider{
		active: false,
		remoteEndpoints: map[string][]*pb.ServiceEndpoints{
			"default/my-svc": {
				{
					ServiceName: "my-svc",
					Namespace:   "default",
					ClusterName: "remote-1",
					Region:      "eu-west-1",
					Endpoints: []*pb.Endpoint{
						{Address: "10.1.0.1", Port: 8080, Ready: true},
					},
				},
			},
		},
	})

	bc := newBuildContextForTest([]novaedgev1alpha1.ProxyBackend{*backend}, nil)
	_, endpoints, err := builder.buildClusters(context.Background(), nil, bc)
	if err != nil {
		t.Fatalf("buildClusters failed: %v", err)
	}

	epList := endpoints["default/test-backend"]
	if epList == nil {
		t.Fatal("Expected endpoint list for default/test-backend")
	}
	// Only local endpoint should be present
	if len(epList.Endpoints) != 1 {
		t.Fatalf("Expected 1 endpoint (local only), got %d", len(epList.Endpoints))
	}
	if epList.Endpoints[0].Address != testEndpointAddr1 {
		t.Errorf("Expected local address 10.0.0.1, got %s", epList.Endpoints[0].Address)
	}
}

func TestBuildClustersFederationMergesRemoteEndpoints(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	ready := true
	port8080 := int32(8080)
	portName := testPortNameHTTP

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: testPortNameHTTP, Port: 80, TargetPort: intstr.FromInt32(8080)},
			},
		},
	}
	es := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-svc-abc", Namespace: "default",
			Labels: map[string]string{"kubernetes.io/service-name": "my-svc"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{testEndpointAddr1}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
		},
		Ports: []discoveryv1.EndpointPort{{Name: &portName, Port: &port8080}},
	}
	backend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "test-backend", Namespace: "default"},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			ServiceRef: &novaedgev1alpha1.ServiceReference{Name: "my-svc", Port: 80},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(svc, es, backend).Build()
	builder := NewBuilder(fakeClient)

	builder.SetFederationProvider(&mockFederationProvider{
		active: true,
		remoteEndpoints: map[string][]*pb.ServiceEndpoints{
			"default/my-svc": {
				{
					ServiceName: "my-svc",
					Namespace:   "default",
					ClusterName: "remote-1",
					Region:      "eu-west-1",
					Zone:        "eu-west-1a",
					Endpoints: []*pb.Endpoint{
						{Address: "10.1.0.1", Port: 8080, Ready: true, Labels: map[string]string{"custom": "label"}},
						{Address: "10.1.0.2", Port: 8080, Ready: false},
					},
				},
				{
					ServiceName: "my-svc",
					Namespace:   "default",
					ClusterName: "remote-2",
					Region:      "us-east-1",
					Zone:        "",
					Endpoints: []*pb.Endpoint{
						{Address: "10.2.0.1", Port: 8080, Ready: true},
					},
				},
			},
		},
	})

	bc := newBuildContextForTest([]novaedgev1alpha1.ProxyBackend{*backend}, nil)
	_, endpoints, err := builder.buildClusters(context.Background(), nil, bc)
	if err != nil {
		t.Fatalf("buildClusters failed: %v", err)
	}

	epList := endpoints["default/test-backend"]
	if epList == nil {
		t.Fatal("Expected endpoint list for default/test-backend")
	}

	// 1 local + 2 from remote-1 + 1 from remote-2 = 4
	if len(epList.Endpoints) != 4 {
		t.Fatalf("Expected 4 endpoints (1 local + 3 remote), got %d", len(epList.Endpoints))
	}

	// Verify local endpoint (first)
	if epList.Endpoints[0].Address != testEndpointAddr1 {
		t.Errorf("Expected first endpoint to be local 10.0.0.1, got %s", epList.Endpoints[0].Address)
	}

	// Check remote endpoint labels
	remoteEP1 := epList.Endpoints[1] // 10.1.0.1 from remote-1
	if remoteEP1.Address != "10.1.0.1" {
		t.Errorf("Expected remote endpoint 10.1.0.1, got %s", remoteEP1.Address)
	}
	if remoteEP1.Labels["novaedge.io/remote"] != testLabelTrue {
		t.Error("Expected novaedge.io/remote=true label on remote endpoint")
	}
	if remoteEP1.Labels["novaedge.io/cluster"] != "remote-1" {
		t.Errorf("Expected novaedge.io/cluster=remote-1, got %s", remoteEP1.Labels["novaedge.io/cluster"])
	}
	if remoteEP1.Labels["novaedge.io/region"] != "eu-west-1" {
		t.Errorf("Expected novaedge.io/region=eu-west-1, got %s", remoteEP1.Labels["novaedge.io/region"])
	}
	if remoteEP1.Labels["novaedge.io/zone"] != "eu-west-1a" {
		t.Errorf("Expected novaedge.io/zone=eu-west-1a, got %s", remoteEP1.Labels["novaedge.io/zone"])
	}
	// Verify existing labels are preserved
	if remoteEP1.Labels["custom"] != "label" {
		t.Errorf("Expected custom=label to be preserved, got %s", remoteEP1.Labels["custom"])
	}

	// Check remote-2 endpoint (no zone)
	remoteEP3 := epList.Endpoints[3] // 10.2.0.1 from remote-2
	if remoteEP3.Labels["novaedge.io/cluster"] != "remote-2" {
		t.Errorf("Expected novaedge.io/cluster=remote-2, got %s", remoteEP3.Labels["novaedge.io/cluster"])
	}
	if remoteEP3.Labels["novaedge.io/region"] != "us-east-1" {
		t.Errorf("Expected novaedge.io/region=us-east-1, got %s", remoteEP3.Labels["novaedge.io/region"])
	}
	if _, hasZone := remoteEP3.Labels["novaedge.io/zone"]; hasZone {
		t.Error("Expected no novaedge.io/zone label when zone is empty")
	}
	if remoteEP3.Labels["novaedge.io/remote"] != testLabelTrue {
		t.Error("Expected novaedge.io/remote=true label on remote-2 endpoint")
	}
}

func TestMergeRemoteEndpointLabels(t *testing.T) {
	// Test with nil existing labels
	labels := mergeRemoteEndpointLabels(nil, "cluster-a", "us-east-1", "us-east-1a")
	if labels["novaedge.io/remote"] != testLabelTrue {
		t.Error("Expected novaedge.io/remote=true")
	}
	if labels["novaedge.io/cluster"] != "cluster-a" {
		t.Errorf("Expected cluster-a, got %s", labels["novaedge.io/cluster"])
	}
	if labels["novaedge.io/region"] != "us-east-1" {
		t.Errorf("Expected us-east-1, got %s", labels["novaedge.io/region"])
	}
	if labels["novaedge.io/zone"] != "us-east-1a" {
		t.Errorf("Expected us-east-1a, got %s", labels["novaedge.io/zone"])
	}

	// Test preserving existing labels
	existing := map[string]string{"foo": "bar", "baz": "qux"}
	labels = mergeRemoteEndpointLabels(existing, "cluster-b", "", "")
	if labels["foo"] != "bar" || labels["baz"] != "qux" {
		t.Error("Expected existing labels to be preserved")
	}
	if labels["novaedge.io/remote"] != testLabelTrue {
		t.Error("Expected novaedge.io/remote=true")
	}
	if _, hasRegion := labels["novaedge.io/region"]; hasRegion {
		t.Error("Expected no region label when region is empty")
	}
	if _, hasZone := labels["novaedge.io/zone"]; hasZone {
		t.Error("Expected no zone label when zone is empty")
	}
}

func TestBuildInternalServicesEmpty(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	// No services at all
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	builder := NewBuilder(fakeClient)
	bc := newBuildContextForTest(nil, nil)
	services := builder.buildInternalServices(context.Background(), bc)
	if len(services) != 0 {
		t.Errorf("Expected 0 internal services, got %d", len(services))
	}
}
