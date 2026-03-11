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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// NovaEdgeControllerName is the controller name that NovaEdge uses for GatewayClass
const NovaEdgeControllerName = "novaedge.io/gateway-controller"

// GatewayClassReconciler reconciles a Gateway API GatewayClass object
type GatewayClassReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/status,verbs=get;update;patch

// Reconcile handles GatewayClass reconciliation
func (r *GatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the GatewayClass instance
	gatewayClass := &gatewayv1.GatewayClass{}
	err := r.Get(ctx, req.NamespacedName, gatewayClass)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("GatewayClass resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get GatewayClass")
		return ctrl.Result{}, err
	}

	// Only reconcile GatewayClasses that reference our controller
	if string(gatewayClass.Spec.ControllerName) != NovaEdgeControllerName {
		logger.Info("GatewayClass is not for NovaEdge controller, ignoring",
			"controllerName", gatewayClass.Spec.ControllerName,
			"expected", NovaEdgeControllerName)
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling GatewayClass", "name", gatewayClass.Name)

	// Set Accepted condition to True
	acceptedCondition := metav1.Condition{
		Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayClassReasonAccepted),
		Message:            "GatewayClass is accepted by the NovaEdge controller",
		ObservedGeneration: gatewayClass.Generation,
		LastTransitionTime: metav1.Now(),
	}

	// Set SupportedVersion condition
	supportedVersionCondition := metav1.Condition{
		Type:               string(gatewayv1.GatewayClassConditionStatusSupportedVersion),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayClassReasonSupportedVersion),
		Message:            "Gateway API v1.4.0 is supported",
		ObservedGeneration: gatewayClass.Generation,
		LastTransitionTime: metav1.Now(),
	}

	meta.SetStatusCondition(&gatewayClass.Status.Conditions, acceptedCondition)
	meta.SetStatusCondition(&gatewayClass.Status.Conditions, supportedVersionCondition)

	if err := r.Status().Update(ctx, gatewayClass); err != nil {
		logger.Error(err, "Failed to update GatewayClass status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled GatewayClass", "name", gatewayClass.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *GatewayClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.GatewayClass{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WithOptions(defaultControllerOptions()).
		Complete(r)
}
