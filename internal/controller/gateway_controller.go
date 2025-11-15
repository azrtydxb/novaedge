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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// GatewayReconciler reconciles a Gateway API Gateway object
type GatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop for Gateway resources
func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Gateway instance
	gateway := &gatewayv1.Gateway{}
	err := r.Get(ctx, req.NamespacedName, gateway)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Gateway resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Gateway")
		return ctrl.Result{}, err
	}

	// Check if this Gateway is for our GatewayClass
	if string(gateway.Spec.GatewayClassName) != NovaEdgeGatewayClassName {
		logger.Info("Gateway is not for NovaEdge GatewayClass, ignoring",
			"gatewayClass", gateway.Spec.GatewayClassName,
			"expected", NovaEdgeGatewayClassName)
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling Gateway", "name", gateway.Name, "namespace", gateway.Namespace)

	// Handle deletion
	if !gateway.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, gateway)
	}

	// Translate Gateway to ProxyGateway
	// First, we need to find or create a ProxyVIP for this gateway
	vipName := fmt.Sprintf("%s-vip", gateway.Name)
	proxyGateway, err := TranslateGatewayToProxyGateway(gateway, vipName)
	if err != nil {
		logger.Error(err, "Failed to translate Gateway to ProxyGateway")
		return r.updateGatewayStatus(ctx, gateway, metav1.Condition{
			Type:               string(gatewayv1.GatewayConditionAccepted),
			Status:             metav1.ConditionFalse,
			Reason:             "Invalid",
			Message:            fmt.Sprintf("Translation failed: %v", err),
			ObservedGeneration: gateway.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	// Create or update the ProxyGateway
	existingProxyGateway := &novaedgev1alpha1.ProxyGateway{}
	err = r.Get(ctx, types.NamespacedName{Name: gateway.Name, Namespace: gateway.Namespace}, existingProxyGateway)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ProxyGateway
			logger.Info("Creating ProxyGateway", "name", proxyGateway.Name)
			if err := r.Create(ctx, proxyGateway); err != nil {
				logger.Error(err, "Failed to create ProxyGateway")
				return r.updateGatewayStatus(ctx, gateway, metav1.Condition{
					Type:               string(gatewayv1.GatewayConditionAccepted),
					Status:             metav1.ConditionFalse,
					Reason:             "CreationFailed",
					Message:            fmt.Sprintf("Failed to create ProxyGateway: %v", err),
					ObservedGeneration: gateway.Generation,
					LastTransitionTime: metav1.Now(),
				})
			}
		} else {
			logger.Error(err, "Failed to get ProxyGateway")
			return ctrl.Result{}, err
		}
	} else {
		// Update existing ProxyGateway
		logger.Info("Updating ProxyGateway", "name", proxyGateway.Name)
		existingProxyGateway.Spec = proxyGateway.Spec
		existingProxyGateway.Labels = proxyGateway.Labels
		existingProxyGateway.Annotations = proxyGateway.Annotations
		if err := r.Update(ctx, existingProxyGateway); err != nil {
			logger.Error(err, "Failed to update ProxyGateway")
			return r.updateGatewayStatus(ctx, gateway, metav1.Condition{
				Type:               string(gatewayv1.GatewayConditionAccepted),
				Status:             metav1.ConditionFalse,
				Reason:             "UpdateFailed",
				Message:            fmt.Sprintf("Failed to update ProxyGateway: %v", err),
				ObservedGeneration: gateway.Generation,
				LastTransitionTime: metav1.Now(),
			})
		}
	}

	// Update Gateway status to Accepted and Programmed
	acceptedCondition := metav1.Condition{
		Type:               string(gatewayv1.GatewayConditionAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayReasonAccepted),
		Message:            "Gateway has been accepted and translated to ProxyGateway",
		ObservedGeneration: gateway.Generation,
		LastTransitionTime: metav1.Now(),
	}

	programmedCondition := metav1.Condition{
		Type:               string(gatewayv1.GatewayConditionProgrammed),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayReasonProgrammed),
		Message:            "Gateway is programmed and ready",
		ObservedGeneration: gateway.Generation,
		LastTransitionTime: metav1.Now(),
	}

	// Update listener status
	var listenerStatuses []gatewayv1.ListenerStatus
	for _, listener := range gateway.Spec.Listeners {
		listenerStatus := gatewayv1.ListenerStatus{
			Name: listener.Name,
			SupportedKinds: []gatewayv1.RouteGroupKind{
				{
					Group: (*gatewayv1.Group)(&gatewayv1.GroupVersion.Group),
					Kind:  "HTTPRoute",
				},
			},
			Conditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.ListenerConditionAccepted),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.ListenerReasonAccepted),
					Message:            "Listener is accepted",
					ObservedGeneration: gateway.Generation,
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               string(gatewayv1.ListenerConditionProgrammed),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.ListenerReasonProgrammed),
					Message:            "Listener is programmed",
					ObservedGeneration: gateway.Generation,
					LastTransitionTime: metav1.Now(),
				},
			},
			AttachedRoutes: r.countAttachedRoutes(ctx, gateway.Name, gateway.Namespace, string(listener.Name)),
		}
		listenerStatuses = append(listenerStatuses, listenerStatus)
	}

	gateway.Status.Listeners = listenerStatuses
	meta.SetStatusCondition(&gateway.Status.Conditions, acceptedCondition)
	meta.SetStatusCondition(&gateway.Status.Conditions, programmedCondition)

	if err := r.Status().Update(ctx, gateway); err != nil {
		logger.Error(err, "Failed to update Gateway status")
		return ctrl.Result{}, err
	}

	// Trigger config update for all nodes
	TriggerConfigUpdate()

	logger.Info("Successfully reconciled Gateway")
	return ctrl.Result{}, nil
}

// handleDeletion handles cleanup when a Gateway is deleted
func (r *GatewayReconciler) handleDeletion(ctx context.Context, gateway *gatewayv1.Gateway) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling Gateway deletion", "name", gateway.Name)

	// Delete associated ProxyGateway if it exists
	proxyGateway := &novaedgev1alpha1.ProxyGateway{}
	err := r.Get(ctx, types.NamespacedName{Name: gateway.Name, Namespace: gateway.Namespace}, proxyGateway)
	if err == nil {
		// ProxyGateway exists, delete it
		logger.Info("Deleting associated ProxyGateway", "name", proxyGateway.Name)
		if err := r.Delete(ctx, proxyGateway); err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "Failed to delete ProxyGateway")
			return ctrl.Result{}, err
		}
	} else if !errors.IsNotFound(err) {
		logger.Error(err, "Failed to get ProxyGateway for deletion")
		return ctrl.Result{}, err
	}

	// Remove finalizer if it exists
	if controllerutil.ContainsFinalizer(gateway, "novaedge.io/gateway-finalizer") {
		controllerutil.RemoveFinalizer(gateway, "novaedge.io/gateway-finalizer")
		if err := r.Update(ctx, gateway); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// updateGatewayStatus updates the Gateway status with the given condition
func (r *GatewayReconciler) updateGatewayStatus(ctx context.Context, gateway *gatewayv1.Gateway, condition metav1.Condition) (ctrl.Result, error) {
	meta.SetStatusCondition(&gateway.Status.Conditions, condition)
	if err := r.Status().Update(ctx, gateway); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// countAttachedRoutes counts how many HTTPRoutes reference this gateway listener
func (r *GatewayReconciler) countAttachedRoutes(ctx context.Context, gatewayName, gatewayNamespace, listenerName string) int32 {
	// List all HTTPRoute resources from Gateway API in the same namespace
	routeList := &gatewayv1.HTTPRouteList{}
	if err := r.List(ctx, routeList, client.InNamespace(gatewayNamespace)); err != nil {
		// Log error but don't fail - return 0
		return 0
	}

	count := int32(0)
	for _, route := range routeList.Items {
		// Check if this route references our gateway
		for _, parentRef := range route.Spec.ParentRefs {
			// Match gateway name and listener name (if specified)
			if string(parentRef.Name) == gatewayName {
				// If listener name is specified in parentRef, it must match
				if parentRef.SectionName != nil && string(*parentRef.SectionName) != "" {
					if string(*parentRef.SectionName) == listenerName {
						count++
						break // Count this route only once per gateway
					}
				} else {
					// No specific listener specified - count it
					count++
					break
				}
			}
		}
	}

	return count
}

// SetupWithManager sets up the controller with the Manager
func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.Gateway{}).
		Owns(&novaedgev1alpha1.ProxyGateway{}).
		Complete(r)
}
