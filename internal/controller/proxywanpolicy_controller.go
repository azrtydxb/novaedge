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

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

// ProxyWANPolicyReconciler reconciles a ProxyWANPolicy object.
type ProxyWANPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxywanpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxywanpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxywanpolicies/finalizers,verbs=update

// Reconcile handles ProxyWANPolicy reconciliation.
func (r *ProxyWANPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	policy := &novaedgev1alpha1.ProxyWANPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ProxyWANPolicy deleted, triggering config update")
			TriggerConfigUpdate()
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get ProxyWANPolicy: %w", err)
	}

	// Validate required spec fields
	var validationErrors []string
	if policy.Spec.PathSelection.Strategy == "" {
		validationErrors = append(validationErrors, "spec.pathSelection.strategy is required")
	}

	// Validate strategy is a known value
	switch policy.Spec.PathSelection.Strategy {
	case novaedgev1alpha1.WANStrategyLowestLatency,
		novaedgev1alpha1.WANStrategyHighestBandwidth,
		novaedgev1alpha1.WANStrategyMostReliable,
		novaedgev1alpha1.WANStrategyLowestCost:
		// valid
	case "":
		// already caught above
	default:
		validationErrors = append(validationErrors,
			fmt.Sprintf("spec.pathSelection.strategy %q is not a supported value", policy.Spec.PathSelection.Strategy))
	}

	if len(validationErrors) > 0 {
		policy.Status.Phase = "Invalid"
		policy.Status.ObservedGeneration = policy.Generation

		setCondition(&policy.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: policy.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             ConditionReasonValidationFailed,
			Message:            fmt.Sprintf("Validation failed: %s", validationErrors[0]),
		})

		if err := r.Status().Update(ctx, policy); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update ProxyWANPolicy status: %w", err)
		}

		logger.Info("ProxyWANPolicy validation failed", "name", policy.Name, "errors", validationErrors)
		return ctrl.Result{}, nil
	}

	// Update status
	policy.Status.Phase = phaseActive
	policy.Status.ObservedGeneration = policy.Generation

	setCondition(&policy.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: policy.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             "Reconciled",
		Message:            "WAN policy configured successfully",
	})

	if err := r.Status().Update(ctx, policy); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update ProxyWANPolicy status: %w", err)
	}

	TriggerConfigUpdate()

	logger.Info("Reconciled ProxyWANPolicy", "name", policy.Name, "strategy", policy.Spec.PathSelection.Strategy)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProxyWANPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyWANPolicy{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
