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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	cache := NewSnapshotCache()

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

func newTestClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = novaedgev1alpha1.AddToScheme(scheme)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}
