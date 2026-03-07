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
	"math"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	"github.com/azrtydxb/novaedge/internal/controller/ipam"
)

// ProxyIPPoolReconciler reconciles a ProxyIPPool object
type ProxyIPPoolReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Allocator *ipam.Allocator
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxyippools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyippools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyippools/finalizers,verbs=update

// Reconcile handles ProxyIPPool reconciliation
func (r *ProxyIPPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	pool := &novaedgev1alpha1.ProxyIPPool{}
	err := r.Get(ctx, req.NamespacedName, pool)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ProxyIPPool resource not found, removing from allocator")
			if r.Allocator != nil {
				r.Allocator.RemovePool(req.Name)
			}
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ProxyIPPool")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling ProxyIPPool",
		"name", pool.Name,
		"cidrs", pool.Spec.CIDRs,
		"addresses", pool.Spec.Addresses,
		"autoAssign", pool.Spec.AutoAssign,
	)

	// Register/update pool in allocator
	if r.Allocator != nil {
		if err := r.Allocator.AddPool(pool.Name, pool.Spec.CIDRs, pool.Spec.Addresses); err != nil {
			logger.Error(err, "Failed to register IP pool")

			// Update status with error condition
			condition := metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				ObservedGeneration: pool.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             "PoolConfigError",
				Message:            err.Error(),
			}
			setCondition(&pool.Status.Conditions, condition)
			if statusErr := r.Status().Update(ctx, pool); statusErr != nil {
				logger.Error(statusErr, "Failed to update pool status")
			}

			return ctrl.Result{}, err
		}

		// Get pool stats
		allocated, available, statsErr := r.Allocator.GetPoolStats(pool.Name)
		if statsErr == nil {
			if allocated > math.MaxInt32 {
				allocated = math.MaxInt32
			}
			if available > math.MaxInt32 {
				available = math.MaxInt32
			}
			pool.Status.Allocated = int32(allocated) //nolint:gosec // bounds-checked above
			pool.Status.Available = int32(available) //nolint:gosec // bounds-checked above
		}

		// Get allocations
		allocations, allocErr := r.Allocator.GetPoolAllocations(pool.Name)
		if allocErr == nil {
			pool.Status.Allocations = make([]novaedgev1alpha1.IPAllocation, 0, len(allocations))
			for addr, vipName := range allocations {
				pool.Status.Allocations = append(pool.Status.Allocations, novaedgev1alpha1.IPAllocation{
					Address: addr,
					VIPRef:  vipName,
				})
			}
		}
	}

	// Update status
	pool.Status.ObservedGeneration = pool.Generation
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: pool.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             "PoolReady",
		Message:            "IP pool is configured and available for allocation",
	}
	setCondition(&pool.Status.Conditions, condition)

	if err := r.Status().Update(ctx, pool); err != nil {
		logger.Error(err, "Failed to update pool status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ProxyIPPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyIPPool{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
