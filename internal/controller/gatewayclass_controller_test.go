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
	ctrl "sigs.k8s.io/controller-runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestGatewayClassReconcile_Accepted(t *testing.T) {
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

	if err := env.client.Create(ctx, gatewayClass); err != nil {
		t.Fatalf("failed to create GatewayClass: %v", err)
	}

	reconciler := &GatewayClassReconciler{
		Client: env.client,
		Scheme: env.scheme,
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "novaedge"},
	})
	if err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Verify status conditions
	updated := &gatewayv1.GatewayClass{}
	if err := env.client.Get(ctx, types.NamespacedName{Name: "novaedge"}, updated); err != nil {
		t.Fatalf("failed to get GatewayClass: %v", err)
	}

	acceptedCond := meta.FindStatusCondition(updated.Status.Conditions, string(gatewayv1.GatewayClassConditionStatusAccepted))
	if acceptedCond == nil {
		t.Fatal("expected Accepted condition to be set")
	}
	if acceptedCond.Status != metav1.ConditionTrue {
		t.Errorf("expected Accepted=True, got %s", acceptedCond.Status)
	}
	if acceptedCond.Reason != string(gatewayv1.GatewayClassReasonAccepted) {
		t.Errorf("expected reason %s, got %s", gatewayv1.GatewayClassReasonAccepted, acceptedCond.Reason)
	}
}

func TestGatewayClassReconcile_IgnoresOtherControllers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "istio",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "istio.io/gateway-controller",
		},
	}

	if err := env.client.Create(ctx, gatewayClass); err != nil {
		t.Fatalf("failed to create GatewayClass: %v", err)
	}

	reconciler := &GatewayClassReconciler{
		Client: env.client,
		Scheme: env.scheme,
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "istio"},
	})
	if err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	// Verify no status conditions were set
	updated := &gatewayv1.GatewayClass{}
	if err := env.client.Get(ctx, types.NamespacedName{Name: "istio"}, updated); err != nil {
		t.Fatalf("failed to get GatewayClass: %v", err)
	}

	if len(updated.Status.Conditions) != 0 {
		t.Errorf("expected no conditions for non-NovaEdge GatewayClass, got %d", len(updated.Status.Conditions))
	}
}

func TestGatewayClassReconcile_NotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	env := setupTestEnv(t)

	reconciler := &GatewayClassReconciler{
		Client: env.client,
		Scheme: env.scheme,
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent"},
	})
	if err != nil {
		t.Fatalf("reconciliation should not fail for missing GatewayClass: %v", err)
	}
}

func TestGatewayClassReconcile_SupportedVersionCondition(t *testing.T) {
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

	if err := env.client.Create(ctx, gatewayClass); err != nil {
		t.Fatalf("failed to create GatewayClass: %v", err)
	}

	reconciler := &GatewayClassReconciler{
		Client: env.client,
		Scheme: env.scheme,
	}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "novaedge"},
	})
	if err != nil {
		t.Fatalf("reconciliation failed: %v", err)
	}

	updated := &gatewayv1.GatewayClass{}
	if err := env.client.Get(ctx, types.NamespacedName{Name: "novaedge"}, updated); err != nil {
		t.Fatalf("failed to get GatewayClass: %v", err)
	}

	versionCond := meta.FindStatusCondition(updated.Status.Conditions, string(gatewayv1.GatewayClassConditionStatusSupportedVersion))
	if versionCond == nil {
		t.Fatal("expected SupportedVersion condition to be set")
	}
	if versionCond.Status != metav1.ConditionTrue {
		t.Errorf("expected SupportedVersion=True, got %s", versionCond.Status)
	}
}
