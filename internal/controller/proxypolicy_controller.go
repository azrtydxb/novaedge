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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// ProxyPolicyReconciler reconciles a ProxyPolicy object
type ProxyPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxypolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxypolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxypolicies/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop
func (r *ProxyPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ProxyPolicy instance
	policy := &novaedgev1alpha1.ProxyPolicy{}
	err := r.Get(ctx, req.NamespacedName, policy)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ProxyPolicy resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ProxyPolicy")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling ProxyPolicy", "name", policy.Name, "type", policy.Spec.Type, "target", policy.Spec.TargetRef.Name)

	// TODO: Implement policy reconciliation logic
	// - Validate target reference exists
	// - Validate policy configuration based on type
	// - Build policy configuration for snapshot

	// Trigger config update for all nodes
	TriggerConfigUpdate()

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ProxyPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyPolicy{}).
		Complete(r)
}
