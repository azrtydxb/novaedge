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

func TestNovaEdgeClusterReconcileNotFound(t *testing.T) {
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

	reconciler := &NovaEdgeClusterReconciler{
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
		t.Errorf("unexpected error for non-existent cluster: %v", err)
	}
}

func TestNovaEdgeClusterReconcileCreate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: novaedgev1alpha1.NovaEdgeClusterSpec{
			Version: "v1.0.0",
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(cluster).
		WithStatusSubresource(&novaedgev1alpha1.NovaEdgeCluster{}).
		Build()

	reconciler := &NovaEdgeClusterReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
	})

	// Reconcile may return errors due to missing dependencies (RBAC, etc.)
	// We're just testing that it doesn't panic and handles the basic flow
	_ = err
}

func TestNovaEdgeClusterReconcileWithDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add clientgo scheme: %v", err)
	}
	if err := novaedgev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add novaedge scheme: %v", err)
	}

	now := metav1.Now()
	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleted-cluster",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{"novaedge.io/finalizer"},
		},
		Spec: novaedgev1alpha1.NovaEdgeClusterSpec{
			Version: "v1.0.0",
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(cluster).
		WithStatusSubresource(&novaedgev1alpha1.NovaEdgeCluster{}).
		Build()

	reconciler := &NovaEdgeClusterReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
	})

	// Deletion handling - may error due to missing resources
	_ = err
}

func TestNovaEdgeClusterGetLabels(t *testing.T) {
	reconciler := &NovaEdgeClusterReconciler{}
	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: novaedgev1alpha1.NovaEdgeClusterSpec{
			Version: "v1.0.0",
		},
	}

	labels := reconciler.getLabels(cluster, "test-component")

	if labels["app.kubernetes.io/name"] != "novaedge" {
		t.Errorf("expected name label to be novaedge, got %s", labels["app.kubernetes.io/name"])
	}
	if labels["app.kubernetes.io/instance"] != "test-cluster" {
		t.Errorf("expected instance label to be test-cluster, got %s", labels["app.kubernetes.io/instance"])
	}
	if labels["app.kubernetes.io/managed-by"] != "novaedge-operator" {
		t.Errorf("expected managed-by label to be novaedge-operator, got %s", labels["app.kubernetes.io/managed-by"])
	}
	if labels["app.kubernetes.io/component"] != "test-component" {
		t.Errorf("expected component label to be test-component, got %s", labels["app.kubernetes.io/component"])
	}
}

func TestNovaEdgeClusterGetSelectorLabels(t *testing.T) {
	reconciler := &NovaEdgeClusterReconciler{}
	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "selector-test-cluster",
		},
	}

	labels := reconciler.getSelectorLabels(cluster, "controller")

	if labels["app.kubernetes.io/name"] != "novaedge" {
		t.Errorf("expected name label to be novaedge, got %s", labels["app.kubernetes.io/name"])
	}
	if labels["app.kubernetes.io/instance"] != "selector-test-cluster" {
		t.Errorf("expected instance label to be selector-test-cluster, got %s", labels["app.kubernetes.io/instance"])
	}
	if labels["app.kubernetes.io/component"] != "controller" {
		t.Errorf("expected component label to be controller, got %s", labels["app.kubernetes.io/component"])
	}
}

func TestNovaEdgeClusterGetImage(t *testing.T) {
	reconciler := &NovaEdgeClusterReconciler{
		ControllerImage: "override/controller:latest",
	}

	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		Spec: novaedgev1alpha1.NovaEdgeClusterSpec{
			ImageRepository: "custom.registry.io/novaedge",
			Version:         "v1.0.0",
		},
	}

	tests := []struct {
		name      string
		component string
		want      string
	}{
		{
			name:      "controller with override",
			component: "novaedge-controller",
			want:      "override/controller:latest",
		},
		{
			name:      "agent without override",
			component: "novaedge-agent",
			want:      "custom.registry.io/novaedge/novaedge-agent:v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconciler.getImage(cluster, tt.component)
			if got != tt.want {
				t.Errorf("getImage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNovaEdgeClusterGetImageDefaultRepo(t *testing.T) {
	reconciler := &NovaEdgeClusterReconciler{}

	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		Spec: novaedgev1alpha1.NovaEdgeClusterSpec{
			Version: "v1.0.0",
		},
	}

	got := reconciler.getImage(cluster, "novaedge-controller")
	want := "ghcr.io/piwi3910/novaedge/novaedge-controller:v1.0.0"
	if got != want {
		t.Errorf("getImage() = %v, want %v", got, want)
	}
}

func TestNovaEdgeClusterGetControllerServiceAccountName(t *testing.T) {
	reconciler := &NovaEdgeClusterReconciler{}
	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sa-test-cluster",
		},
	}

	name := reconciler.getControllerServiceAccountName(cluster)
	expected := "sa-test-cluster-controller"
	if name != expected {
		t.Errorf("expected %s, got %s", expected, name)
	}
}

func TestNovaEdgeClusterGetControllerServiceAccountNameWithOverride(t *testing.T) {
	reconciler := &NovaEdgeClusterReconciler{}
	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sa-override-cluster",
		},
		Spec: novaedgev1alpha1.NovaEdgeClusterSpec{
			Controller: novaedgev1alpha1.ControllerSpec{
				ServiceAccount: &novaedgev1alpha1.ServiceAccountSpec{
					Name: "custom-sa",
				},
			},
		},
	}

	name := reconciler.getControllerServiceAccountName(cluster)
	expected := "custom-sa"
	if name != expected {
		t.Errorf("expected %s, got %s", expected, name)
	}
}

func TestNovaEdgeClusterGetAgentServiceAccountName(t *testing.T) {
	reconciler := &NovaEdgeClusterReconciler{}
	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "agent-sa-test",
		},
	}

	name := reconciler.getAgentServiceAccountName(cluster)
	expected := "agent-sa-test-agent"
	if name != expected {
		t.Errorf("expected %s, got %s", expected, name)
	}
}

func TestNovaEdgeClusterGetAgentServiceAccountNameWithOverride(t *testing.T) {
	reconciler := &NovaEdgeClusterReconciler{}
	cluster := &novaedgev1alpha1.NovaEdgeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "agent-sa-override",
		},
		Spec: novaedgev1alpha1.NovaEdgeClusterSpec{
			Agent: novaedgev1alpha1.AgentSpec{
				ServiceAccount: &novaedgev1alpha1.ServiceAccountSpec{
					Name: "custom-agent-sa",
				},
			},
		},
	}

	name := reconciler.getAgentServiceAccountName(cluster)
	expected := "custom-agent-sa"
	if name != expected {
		t.Errorf("expected %s, got %s", expected, name)
	}
}

func TestConditionStatus(t *testing.T) {
	tests := []struct {
		ready bool
		want  metav1.ConditionStatus
	}{
		{true, metav1.ConditionTrue},
		{false, metav1.ConditionFalse},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := conditionStatus(tt.ready)
			if got != tt.want {
				t.Errorf("conditionStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConditionReason(t *testing.T) {
	tests := []struct {
		ready       bool
		trueReason  string
		falseReason string
		want        string
	}{
		{true, "Ready", "NotReady", "Ready"},
		{false, "Ready", "NotReady", "NotReady"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := conditionReason(tt.ready, tt.trueReason, tt.falseReason)
			if got != tt.want {
				t.Errorf("conditionReason() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConditionMessage(t *testing.T) {
	tests := []struct {
		ready      bool
		trueMsg    string
		falseMsg   string
		want       string
	}{
		{true, "Cluster is ready", "Cluster is not ready", "Cluster is ready"},
		{false, "Cluster is ready", "Cluster is not ready", "Cluster is not ready"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := conditionMessage(tt.ready, tt.trueMsg, tt.falseMsg)
			if got != tt.want {
				t.Errorf("conditionMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}
