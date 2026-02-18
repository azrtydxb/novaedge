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
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestProxyWANLinkReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	tests := []struct {
		name    string
		link    *novaedgev1alpha1.ProxyWANLink
		wantErr bool
	}{
		{
			name: "valid WAN link",
			link: &novaedgev1alpha1.ProxyWANLink{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-wan-link",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyWANLinkSpec{
					Site:      "remote-site-1",
					Interface: "eth0",
					Provider:  "ISP1",
					Bandwidth: "100Mbps",
				},
			},
			wantErr: false,
		},
		{
			name: "WAN link with tunnel endpoint",
			link: &novaedgev1alpha1.ProxyWANLink{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tunnel-wan-link",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyWANLinkSpec{
					Site:      "remote-site-2",
					Interface: "eth1",
					Provider:  "ISP2",
					Bandwidth: "1Gbps",
					TunnelEndpoint: &novaedgev1alpha1.WANTunnelEndpoint{
						PublicIP: "192.168.1.100",
						Port:     51820,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "WAN link with SLA config",
			link: &novaedgev1alpha1.ProxyWANLink{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sla-wan-link",
					Namespace: "default",
				},
				Spec: novaedgev1alpha1.ProxyWANLinkSpec{
					Site:      "secure-site",
					Interface: "eth2",
					Provider:  "ISP3",
					Bandwidth: "500Mbps",
					SLA: &novaedgev1alpha1.WANLinkSLA{
						MaxLatency:    &metav1.Duration{Duration: 50000000},
						MaxJitter:     &metav1.Duration{Duration: 10000000},
						MaxPacketLoss: float64Ptr(0.01),
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.link).
				WithStatusSubresource(&novaedgev1alpha1.ProxyWANLink{}).
				Build()

			reconciler := &ProxyWANLinkReconciler{
				Client: k8sClient,
				Scheme: scheme,
			}

			ctx := context.Background()
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.link.Name,
					Namespace: tt.link.Namespace,
				},
			})

			if (err != nil) != tt.wantErr {
				t.Errorf("ProxyWANLinkReconciler.Reconcile() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify status was updated
				updatedLink := &novaedgev1alpha1.ProxyWANLink{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      tt.link.Name,
					Namespace: tt.link.Namespace,
				}, updatedLink); err != nil {
					t.Errorf("failed to get updated link: %v", err)
				}

				if updatedLink.Status.Phase != phaseActive {
					t.Errorf("expected phase Active, got %s", updatedLink.Status.Phase)
				}

				if !updatedLink.Status.Healthy {
					t.Error("expected link to be healthy")
				}
			}
		})
	}
}

func TestProxyWANLinkReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &ProxyWANLinkReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent",
			Namespace: "default",
		},
	})

	if err != nil {
		t.Errorf("unexpected error for non-existent ProxyWANLink: %v", err)
	}
}

func TestProxyWANLinkStatusUpdate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	link := &novaedgev1alpha1.ProxyWANLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "status-test-link",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyWANLinkSpec{
			Site:      "test-site",
			Interface: "eth0",
			Provider:  "ISP",
			Bandwidth: "100Mbps",
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(link).
		WithStatusSubresource(&novaedgev1alpha1.ProxyWANLink{}).
		Build()

	reconciler := &ProxyWANLinkReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      link.Name,
			Namespace: link.Namespace,
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify the status conditions were set
	updatedLink := &novaedgev1alpha1.ProxyWANLink{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      link.Name,
		Namespace: link.Namespace,
	}, updatedLink); err != nil {
		t.Errorf("failed to get updated link: %v", err)
	}

	if len(updatedLink.Status.Conditions) == 0 {
		t.Error("expected conditions to be set")
	}

	foundReady := false
	for _, cond := range updatedLink.Status.Conditions {
		if cond.Type == "Ready" {
			foundReady = true
			if cond.Status != metav1.ConditionTrue {
				t.Errorf("expected Ready condition to be True, got %s", cond.Status)
			}
		}
	}

	if !foundReady {
		t.Error("expected Ready condition to be present")
	}
}

func TestProxyWANLinkMultipleLinks(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	link1 := &novaedgev1alpha1.ProxyWANLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wan-link-1",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyWANLinkSpec{
			Site:      "site-1",
			Interface: "eth0",
			Provider:  "ISP1",
			Bandwidth: "100Mbps",
		},
	}

	link2 := &novaedgev1alpha1.ProxyWANLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wan-link-2",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.ProxyWANLinkSpec{
			Site:      "site-2",
			Interface: "eth1",
			Provider:  "ISP2",
			Bandwidth: "200Mbps",
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(link1, link2).
		WithStatusSubresource(&novaedgev1alpha1.ProxyWANLink{}).
		Build()

	reconciler := &ProxyWANLinkReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()

	// Reconcile first link
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      link1.Name,
			Namespace: link1.Namespace,
		},
	})
	if err != nil {
		t.Errorf("unexpected error reconciling link1: %v", err)
	}

	// Reconcile second link
	_, err = reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      link2.Name,
			Namespace: link2.Namespace,
		},
	})
	if err != nil {
		t.Errorf("unexpected error reconciling link2: %v", err)
	}
}

func TestProxyWANLinkWithLabels(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	link := &novaedgev1alpha1.ProxyWANLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "labeled-wan-link",
			Namespace: "default",
			Labels: map[string]string{
				"environment": "production",
				"region":      "us-west",
			},
		},
		Spec: novaedgev1alpha1.ProxyWANLinkSpec{
			Site:      "prod-site",
			Interface: "eth0",
			Provider:  "ISP",
			Bandwidth: "1Gbps",
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(link).
		WithStatusSubresource(&novaedgev1alpha1.ProxyWANLink{}).
		Build()

	reconciler := &ProxyWANLinkReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      link.Name,
			Namespace: link.Namespace,
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func float64Ptr(v float64) *float64 {
	return &v
}
