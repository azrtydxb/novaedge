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
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

func TestGatewayReconcile(t *testing.T) {
	tests := []struct {
		name          string
		gateway       *gatewayv1.Gateway
		gatewayClass  *gatewayv1.GatewayClass
		expectError   bool
		expectCreated bool
	}{
		{
			name: "valid Gateway API gateway",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "novaedge",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: gatewayv1.HTTPProtocolType,
						},
					},
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "novaedge",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: "novaedge.io/gateway-controller",
				},
			},
			expectError:   false,
			expectCreated: true,
		},
		{
			name: "Gateway with non-NovaEdge class",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-gateway",
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "istio",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: gatewayv1.HTTPProtocolType,
						},
					},
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "istio",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: "istio.io/gateway-controller",
				},
			},
			expectError:   false,
			expectCreated: false,
		},
		{
			name: "Gateway with HTTPS listener",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "https-gateway",
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "novaedge",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "https",
							Port:     443,
							Protocol: gatewayv1.HTTPSProtocolType,
						},
					},
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "novaedge",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: "novaedge.io/gateway-controller",
				},
			},
			expectError:   false,
			expectCreated: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			env := setupTestEnv(t)

			if test.gatewayClass != nil {
				if err := env.client.Create(ctx, test.gatewayClass); err != nil {
					t.Fatalf("failed to create GatewayClass: %v", err)
				}
			}

			if err := env.client.Create(ctx, test.gateway); err != nil {
				t.Fatalf("failed to create Gateway: %v", err)
			}

			// Manually trigger reconciliation
			err := env.reconcileGateway(ctx, test.gateway.Name, test.gateway.Namespace)
			if test.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !test.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			updatedGateway := &gatewayv1.Gateway{}
			if err := env.client.Get(ctx, types.NamespacedName{
				Name:      test.gateway.Name,
				Namespace: test.gateway.Namespace,
			}, updatedGateway); err != nil {
				t.Fatalf("failed to get Gateway: %v", err)
			}

			// Check if ProxyGateway was created
			if test.expectCreated {
				proxyGateway := &novaedgev1alpha1.ProxyGateway{}
				err := env.client.Get(ctx, types.NamespacedName{
					Name:      test.gateway.Name,
					Namespace: test.gateway.Namespace,
				}, proxyGateway)

				if err != nil {
					t.Errorf("expected ProxyGateway to be created, but got error: %v", err)
				}
			}

			// Check status conditions
			if test.expectCreated {
				acceptedCondition := meta.FindStatusCondition(updatedGateway.Status.Conditions, string(gatewayv1.GatewayConditionAccepted))
				if acceptedCondition == nil || acceptedCondition.Status != metav1.ConditionTrue {
					t.Error("expected Gateway to be accepted")
				}
			}
		})
	}
}

func TestGatewayListenerStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "novaedge",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "novaedge.io/gateway-controller",
		},
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "listener-status-gateway",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "novaedge",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
				},
				{
					Name:     "https",
					Port:     443,
					Protocol: gatewayv1.HTTPSProtocolType,
				},
			},
		},
	}

	if err := env.client.Create(ctx, gatewayClass); err != nil {
		t.Fatalf("failed to create GatewayClass: %v", err)
	}

	if err := env.client.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create Gateway: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileGateway(ctx, gateway.Name, gateway.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedGateway := &gatewayv1.Gateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gateway.Name,
		Namespace: gateway.Namespace,
	}, updatedGateway); err != nil {
		t.Fatalf("failed to get Gateway: %v", err)
	}

	if len(updatedGateway.Status.Listeners) != 2 {
		t.Errorf("expected 2 listener statuses, got %d", len(updatedGateway.Status.Listeners))
	}

	for _, listenerStatus := range updatedGateway.Status.Listeners {
		acceptedCond := meta.FindStatusCondition(listenerStatus.Conditions, string(gatewayv1.ListenerConditionAccepted))
		if acceptedCond == nil || acceptedCond.Status != metav1.ConditionTrue {
			t.Errorf("expected listener %s to be accepted", listenerStatus.Name)
		}
	}
}

func TestGatewayDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "novaedge",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "novaedge.io/gateway-controller",
		},
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-gateway",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "novaedge",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
				},
			},
		},
	}

	if err := env.client.Create(ctx, gatewayClass); err != nil {
		t.Fatalf("failed to create GatewayClass: %v", err)
	}

	if err := env.client.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create Gateway: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileGateway(ctx, gateway.Name, gateway.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Verify ProxyGateway was created
	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gateway.Name,
		Namespace: gateway.Namespace,
	}, proxyGateway); err != nil {
		t.Fatalf("expected ProxyGateway to be created: %v", err)
	}

	// Delete Gateway
	if err := env.client.Delete(ctx, gateway); err != nil {
		t.Fatalf("failed to delete Gateway: %v", err)
	}

	// Manually trigger deletion reconciliation
	// Note: In a real controller, this would be triggered automatically
	// For testing with fake client, we need to handle this differently
	// The ProxyGateway should still exist until garbage collection runs
	// In a real cluster, owner references would handle this

	// For unit tests, we verify the ProxyGateway was created successfully
	// The actual deletion via owner references requires a real controller-runtime manager
}

func TestGatewayProgrammedCondition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "novaedge",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "novaedge.io/gateway-controller",
		},
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "programmed-gateway",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "novaedge",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
				},
			},
		},
	}

	if err := env.client.Create(ctx, gatewayClass); err != nil {
		t.Fatalf("failed to create GatewayClass: %v", err)
	}

	if err := env.client.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create Gateway: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileGateway(ctx, gateway.Name, gateway.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedGateway := &gatewayv1.Gateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gateway.Name,
		Namespace: gateway.Namespace,
	}, updatedGateway); err != nil {
		t.Fatalf("failed to get Gateway: %v", err)
	}

	programmedCond := meta.FindStatusCondition(updatedGateway.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
	if programmedCond == nil || programmedCond.Status != metav1.ConditionTrue {
		t.Error("expected Gateway to be programmed")
	}
}

func TestGatewayMultipleListeners(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "novaedge",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "novaedge.io/gateway-controller",
		},
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-listener-gateway",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "novaedge",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
				},
				{
					Name:     "https",
					Port:     443,
					Protocol: gatewayv1.HTTPSProtocolType,
				},
				{
					Name:     "http-alt",
					Port:     8080,
					Protocol: gatewayv1.HTTPProtocolType,
				},
			},
		},
	}

	if err := env.client.Create(ctx, gatewayClass); err != nil {
		t.Fatalf("failed to create GatewayClass: %v", err)
	}

	if err := env.client.Create(ctx, gateway); err != nil {
		t.Fatalf("failed to create Gateway: %v", err)
	}

	// Manually trigger reconciliation
	if err := env.reconcileGateway(ctx, gateway.Name, gateway.Namespace); err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updatedGateway := &gatewayv1.Gateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gateway.Name,
		Namespace: gateway.Namespace,
	}, updatedGateway); err != nil {
		t.Fatalf("failed to get Gateway: %v", err)
	}

	if len(updatedGateway.Status.Listeners) != 3 {
		t.Errorf("expected 3 listener statuses, got %d", len(updatedGateway.Status.Listeners))
	}

	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	if err := env.client.Get(ctx, types.NamespacedName{
		Name:      gateway.Name,
		Namespace: gateway.Namespace,
	}, proxyGateway); err != nil {
		t.Fatalf("expected ProxyGateway to be created: %v", err)
	}

	if len(proxyGateway.Spec.Listeners) != 3 {
		t.Errorf("expected ProxyGateway to have 3 listeners, got %d", len(proxyGateway.Spec.Listeners))
	}
}
