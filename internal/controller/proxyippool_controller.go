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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	"github.com/azrtydxb/novaedge/internal/controller/ipam"
	novanetv1alpha1 "github.com/azrtydxb/novanet/api/v1alpha1"
)

const (
	// proxyIPPoolFinalizer is the finalizer added to ProxyIPPool resources
	// to ensure cleanup of the corresponding NovaNet IPPool CRD on deletion.
	proxyIPPoolFinalizer = "novaedge.io/ippool-cleanup"
)

// ProxyIPPoolReconciler reconciles a ProxyIPPool object
type ProxyIPPoolReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Allocator ipam.Client
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxyippools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyippools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyippools/finalizers,verbs=update
// +kubebuilder:rbac:groups=novanet.io,resources=ippools,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles ProxyIPPool reconciliation
func (r *ProxyIPPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	pool := &novaedgev1alpha1.ProxyIPPool{}
	err := r.Get(ctx, req.NamespacedName, pool)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ProxyIPPool resource not found, cleaning up NovaNet IPPool")
			// Delete the corresponding NovaNet IPPool if it exists.
			novanetPool := &novanetv1alpha1.IPPool{}
			novanetPool.Name = req.Name
			if delErr := r.Delete(ctx, novanetPool); delErr != nil && !errors.IsNotFound(delErr) {
				logger.Error(delErr, "Failed to delete NovaNet IPPool")
			}
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ProxyIPPool")
		return ctrl.Result{}, err
	}

	// Handle deletion with finalizer.
	if !pool.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(pool, proxyIPPoolFinalizer) {
			// Delete the corresponding NovaNet IPPool.
			novanetPool := &novanetv1alpha1.IPPool{}
			novanetPool.Name = pool.Name
			if delErr := r.Delete(ctx, novanetPool); delErr != nil && !errors.IsNotFound(delErr) {
				logger.Error(delErr, "Failed to delete NovaNet IPPool during finalization")
				return ctrl.Result{}, delErr
			}

			// Remove the finalizer.
			controllerutil.RemoveFinalizer(pool, proxyIPPoolFinalizer)
			if err := r.Update(ctx, pool); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(pool, proxyIPPoolFinalizer) {
		controllerutil.AddFinalizer(pool, proxyIPPoolFinalizer)
		if err := r.Update(ctx, pool); err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.Info("Reconciling ProxyIPPool",
		"name", pool.Name,
		"cidrs", pool.Spec.CIDRs,
		"addresses", pool.Spec.Addresses,
		"autoAssign", pool.Spec.AutoAssign,
	)

	// Sync ProxyIPPool spec to a NovaNet IPPool CRD.
	if err := r.syncNovanetIPPool(ctx, pool); err != nil {
		logger.Error(err, "Failed to sync NovaNet IPPool")

		condition := metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: pool.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             "IPPoolSyncError",
			Message:            err.Error(),
		}
		setCondition(&pool.Status.Conditions, condition)
		if statusErr := r.Status().Update(ctx, pool); statusErr != nil {
			logger.Error(statusErr, "Failed to update pool status")
		}

		return ctrl.Result{}, err
	}

	// Query stats from IPAM service.
	if r.Allocator != nil {
		r.updatePoolStatus(ctx, pool)
	}

	// Update status.
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

// updatePoolStatus queries the IPAM service for pool stats and allocations,
// then updates the ProxyIPPool status fields.
func (r *ProxyIPPoolReconciler) updatePoolStatus(ctx context.Context, pool *novaedgev1alpha1.ProxyIPPool) {
	allocated, available, statsErr := r.Allocator.GetPoolStats(ctx, pool.Name)
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

	allocations, allocErr := r.Allocator.GetPoolAllocations(ctx, pool.Name)
	if allocErr == nil {
		pool.Status.Allocations = make([]novaedgev1alpha1.IPAllocation, 0, len(allocations))
		for addr, resource := range allocations {
			pool.Status.Allocations = append(pool.Status.Allocations, novaedgev1alpha1.IPAllocation{
				Address: addr,
				VIPRef:  resource,
			})
		}
	}
}

// syncNovanetIPPool creates or updates the corresponding NovaNet IPPool CRD
// from a ProxyIPPool spec. This ensures NovaNet's IPAM service knows about the pool.
func (r *ProxyIPPoolReconciler) syncNovanetIPPool(ctx context.Context, pool *novaedgev1alpha1.ProxyIPPool) error {
	novanetPool := &novanetv1alpha1.IPPool{}
	err := r.Get(ctx, types.NamespacedName{Name: pool.Name}, novanetPool)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// Create new NovaNet IPPool.
		novanetPool = &novanetv1alpha1.IPPool{
			ObjectMeta: metav1.ObjectMeta{
				Name: pool.Name,
				Labels: map[string]string{
					"novaedge.io/managed-by": "proxyippool",
				},
			},
			Spec: novanetv1alpha1.IPPoolSpec{
				Type:       novanetv1alpha1.IPPoolTypeLoadBalancerVIP,
				CIDRs:      pool.Spec.CIDRs,
				Addresses:  pool.Spec.Addresses,
				AutoAssign: pool.Spec.AutoAssign,
				Owner:      "novaedge",
			},
		}

		return r.Create(ctx, novanetPool)
	}

	// Update existing NovaNet IPPool if spec changed.
	novanetPool.Spec.CIDRs = pool.Spec.CIDRs
	novanetPool.Spec.Addresses = pool.Spec.Addresses
	novanetPool.Spec.AutoAssign = pool.Spec.AutoAssign

	return r.Update(ctx, novanetPool)
}

// SetupWithManager sets up the controller with the Manager
func (r *ProxyIPPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyIPPool{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
