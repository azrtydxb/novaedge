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
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

const (
	// phaseActive represents the active status phase
	phaseActive = "Active"
)

// ProxyWANLinkReconciler reconciles a ProxyWANLink object.
type ProxyWANLinkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxywanlinks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxywanlinks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxywanlinks/finalizers,verbs=update

// Reconcile handles ProxyWANLink reconciliation.
func (r *ProxyWANLinkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	link := &novaedgev1alpha1.ProxyWANLink{}
	if err := r.Get(ctx, req.NamespacedName, link); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ProxyWANLink deleted, triggering config update")
			TriggerConfigUpdate()
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get ProxyWANLink: %w", err)
	}

	// Update status
	link.Status.Phase = phaseActive
	link.Status.ObservedGeneration = link.Generation
	link.Status.Healthy = true

	setCondition(&link.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: link.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             "Reconciled",
		Message:            "WAN link configured successfully",
	})

	if err := r.Status().Update(ctx, link); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update ProxyWANLink status: %w", err)
	}

	TriggerConfigUpdate()

	logger.Info("Reconciled ProxyWANLink", "name", link.Name, "site", link.Spec.Site)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProxyWANLinkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyWANLink{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
