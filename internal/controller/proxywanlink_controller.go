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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	"github.com/azrtydxb/novaedge/internal/controller/snapshot"
)

const (
	// phaseActive represents the active status phase
	phaseActive = "Active"
)

// ProxyWANLinkReconciler reconciles a ProxyWANLink object.
type ProxyWANLinkReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	ConfigServer *snapshot.Server
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxywanlinks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxywanlinks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxywanlinks/finalizers,verbs=update

// Reconcile handles ProxyWANLink reconciliation.
func (r *ProxyWANLinkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	link := &novaedgev1alpha1.ProxyWANLink{}
	if err := r.Get(ctx, req.NamespacedName, link); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ProxyWANLink deleted, triggering config update")
			triggerConfigUpdate(r.ConfigServer)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get ProxyWANLink: %w", err)
	}

	// Validate required spec fields
	var validationErrors []string
	if link.Spec.Site == "" {
		validationErrors = append(validationErrors, "spec.site is required")
	}
	if link.Spec.Interface == "" {
		validationErrors = append(validationErrors, "spec.interface is required")
	}
	if link.Spec.Provider == "" {
		validationErrors = append(validationErrors, "spec.provider is required")
	}
	if link.Spec.Bandwidth == "" {
		validationErrors = append(validationErrors, "spec.bandwidth is required")
	}

	if len(validationErrors) > 0 {
		link.Status.Phase = "Invalid"
		link.Status.ObservedGeneration = link.Generation
		link.Status.Healthy = false

		meta.SetStatusCondition(&link.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: link.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             ConditionReasonValidationFailed,
			Message:            fmt.Sprintf("Validation failed: %s", validationErrors[0]),
		})

		if err := r.Status().Update(ctx, link); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update ProxyWANLink status: %w", err)
		}

		logger.Info("ProxyWANLink validation failed", "name", link.Name, "errors", validationErrors)
		return ctrl.Result{}, nil
	}

	// Update status
	link.Status.Phase = phaseActive
	link.Status.ObservedGeneration = link.Generation
	link.Status.Healthy = true

	meta.SetStatusCondition(&link.Status.Conditions, metav1.Condition{
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

	triggerConfigUpdate(r.ConfigServer)

	logger.Info("Reconciled ProxyWANLink", "name", link.Name, "site", link.Spec.Site)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProxyWANLinkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyWANLink{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
