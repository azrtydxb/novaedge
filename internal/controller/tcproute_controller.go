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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// TCPRouteReconciler reconciles Gateway API TCPRoute objects
type TCPRouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tcproutes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=tcproutes/status,verbs=get;update;patch

// Reconcile translates a TCPRoute into a NovaEdge ProxyRoute with L4 TCP annotations
func (r *TCPRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the TCPRoute
	tcpRoute := &gatewayv1alpha2.TCPRoute{}
	err := r.Get(ctx, req.NamespacedName, tcpRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("TCPRoute not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling TCPRoute", "name", tcpRoute.Name, "namespace", tcpRoute.Namespace)

	// Handle deletion
	if !tcpRoute.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, tcpRoute)
	}

	// Translate TCPRoute to ProxyRoute
	proxyRoute, err := TranslateTCPRouteToProxyRoute(tcpRoute)
	if err != nil {
		logger.Error(err, "Failed to translate TCPRoute")
		return r.updateRouteStatus(ctx, tcpRoute, metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "Invalid",
			Message:            fmt.Sprintf("Translation failed: %v", err),
			ObservedGeneration: tcpRoute.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	// Create or update ProxyRoute
	existing := &novaedgev1alpha1.ProxyRoute{}
	err = r.Get(ctx, types.NamespacedName{Name: tcpRoute.Name, Namespace: tcpRoute.Namespace}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating ProxyRoute from TCPRoute", "name", proxyRoute.Name)
			if err := r.Create(ctx, proxyRoute); err != nil {
				logger.Error(err, "Failed to create ProxyRoute from TCPRoute")
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, err
		}
	} else {
		existing.Spec = proxyRoute.Spec
		existing.Labels = proxyRoute.Labels
		existing.Annotations = proxyRoute.Annotations
		if err := r.Update(ctx, existing); err != nil {
			logger.Error(err, "Failed to update ProxyRoute from TCPRoute")
			return ctrl.Result{}, err
		}
	}

	// Update status to Accepted
	return r.updateRouteStatus(ctx, tcpRoute, metav1.Condition{
		Type:               "Accepted",
		Status:             metav1.ConditionTrue,
		Reason:             "Accepted",
		Message:            "TCPRoute accepted and translated to ProxyRoute",
		ObservedGeneration: tcpRoute.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

// handleDeletion cleans up when a TCPRoute is deleted
func (r *TCPRouteReconciler) handleDeletion(ctx context.Context, tcpRoute *gatewayv1alpha2.TCPRoute) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	proxyRoute := &novaedgev1alpha1.ProxyRoute{}
	err := r.Get(ctx, types.NamespacedName{Name: tcpRoute.Name, Namespace: tcpRoute.Namespace}, proxyRoute)
	if err == nil {
		logger.Info("Deleting ProxyRoute from TCPRoute deletion", "name", proxyRoute.Name)
		if err := r.Delete(ctx, proxyRoute); err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	} else if !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	TriggerConfigUpdate()
	return ctrl.Result{}, nil
}

// updateRouteStatus updates the TCPRoute status
func (r *TCPRouteReconciler) updateRouteStatus(ctx context.Context, tcpRoute *gatewayv1alpha2.TCPRoute, condition metav1.Condition) (ctrl.Result, error) {
	// TCPRoute uses RouteParentStatus for status
	if len(tcpRoute.Status.Parents) == 0 {
		tcpRoute.Status.Parents = make([]gatewayv1alpha2.RouteParentStatus, 1)
	}
	meta.SetStatusCondition(&tcpRoute.Status.Parents[0].Conditions, condition)
	if err := r.Status().Update(ctx, tcpRoute); err != nil {
		return ctrl.Result{}, err
	}

	TriggerConfigUpdate()
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *TCPRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1alpha2.TCPRoute{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&novaedgev1alpha1.ProxyRoute{}).
		Complete(r)
}
