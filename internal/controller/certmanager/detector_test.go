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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDetector_IsCertManagerInstalled_Found(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	fakeDiscovery, ok := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		t.Fatal("failed to cast to FakeDiscovery")
	}

	fakeDiscovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "cert-manager.io/v1",
			APIResources: []metav1.APIResource{
				{Name: "certificates", Kind: "Certificate"},
				{Name: "issuers", Kind: "Issuer"},
				{Name: "clusterissuers", Kind: "ClusterIssuer"},
			},
		},
	}

	detector := NewDetectorFromClient(fakeDiscovery)
	installed, err := detector.IsCertManagerInstalled(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !installed {
		t.Error("expected cert-manager to be detected as installed")
	}
}

func TestDetector_IsCertManagerInstalled_NotFound(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	fakeDiscovery, ok := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		t.Fatal("failed to cast to FakeDiscovery")
	}

	fakeDiscovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{
				{Name: "deployments", Kind: "Deployment"},
			},
		},
	}

	detector := NewDetectorFromClient(fakeDiscovery)
	installed, err := detector.IsCertManagerInstalled(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if installed {
		t.Error("expected cert-manager to NOT be detected as installed")
	}
}

func TestDetector_ShouldEnable_False(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	fakeDiscovery, ok := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		t.Fatal("failed to cast to FakeDiscovery")
	}

	detector := NewDetectorFromClient(fakeDiscovery)
	enabled, err := detector.ShouldEnable(context.Background(), EnableModeFalse)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Error("expected disabled when mode is false")
	}
}

func TestDetector_ShouldEnable_True_Missing(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	fakeDiscovery, ok := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		t.Fatal("failed to cast to FakeDiscovery")
	}

	fakeDiscovery.Resources = []*metav1.APIResourceList{}

	detector := NewDetectorFromClient(fakeDiscovery)
	_, err := detector.ShouldEnable(context.Background(), EnableModeTrue)
	if err == nil {
		t.Error("expected error when cert-manager required but not found")
	}
}

func TestDetector_ShouldEnable_Auto_Found(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	fakeDiscovery, ok := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		t.Fatal("failed to cast to FakeDiscovery")
	}

	fakeDiscovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "cert-manager.io/v1",
			APIResources: []metav1.APIResource{
				{Name: "certificates", Kind: "Certificate"},
			},
		},
	}

	detector := NewDetectorFromClient(fakeDiscovery)
	enabled, err := detector.ShouldEnable(context.Background(), EnableModeAuto)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled {
		t.Error("expected enabled when auto mode and cert-manager found")
	}
}

// Ensure discovery.DiscoveryInterface is referenced.
var _ discovery.DiscoveryInterface = nil
