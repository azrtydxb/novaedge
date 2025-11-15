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

	corev1 "k8s.io/api/core/v1"
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

// HTTPRouteReconciler reconciles a Gateway API HTTPRoute object
type HTTPRouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop for HTTPRoute resources
func (r *HTTPRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the HTTPRoute instance
	httpRoute := &gatewayv1.HTTPRoute{}
	err := r.Get(ctx, req.NamespacedName, httpRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("HTTPRoute resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get HTTPRoute")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling HTTPRoute", "name", httpRoute.Name, "namespace", httpRoute.Namespace)

	// Handle deletion
	if !httpRoute.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, httpRoute)
	}

	// Check if any parent refs point to our Gateway
	hasNovaEdgeGateway := false
	for _, parentRef := range httpRoute.Spec.ParentRefs {
		if parentRef.Kind != nil && *parentRef.Kind == "Gateway" {
			// Get the Gateway to check its class
			gatewayNamespace := httpRoute.Namespace
			if parentRef.Namespace != nil {
				gatewayNamespace = string(*parentRef.Namespace)
			}

			gateway := &gatewayv1.Gateway{}
			err := r.Get(ctx, types.NamespacedName{
				Name:      string(parentRef.Name),
				Namespace: gatewayNamespace,
			}, gateway)
			if err == nil && string(gateway.Spec.GatewayClassName) == NovaEdgeGatewayClassName {
				hasNovaEdgeGateway = true
				break
			}
		}
	}

	if !hasNovaEdgeGateway {
		logger.Info("HTTPRoute does not reference a NovaEdge Gateway, ignoring")
		return ctrl.Result{}, nil
	}

	// Translate HTTPRoute to ProxyRoute
	proxyRoute, err := TranslateHTTPRouteToProxyRoute(httpRoute)
	if err != nil {
		logger.Error(err, "Failed to translate HTTPRoute to ProxyRoute")
		return r.updateHTTPRouteStatus(ctx, httpRoute, metav1.Condition{
			Type:               string(gatewayv1.RouteConditionAccepted),
			Status:             metav1.ConditionFalse,
			Reason:             "Invalid",
			Message:            fmt.Sprintf("Translation failed: %v", err),
			ObservedGeneration: httpRoute.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	// Create or update ProxyBackends for each backend reference
	if err := r.reconcileBackends(ctx, httpRoute); err != nil {
		logger.Error(err, "Failed to reconcile backends")
		return r.updateHTTPRouteStatus(ctx, httpRoute, metav1.Condition{
			Type:               string(gatewayv1.RouteConditionAccepted),
			Status:             metav1.ConditionFalse,
			Reason:             string(gatewayv1.RouteReasonBackendNotFound),
			Message:            fmt.Sprintf("Backend reconciliation failed: %v", err),
			ObservedGeneration: httpRoute.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	// Create or update the ProxyRoute
	existingProxyRoute := &novaedgev1alpha1.ProxyRoute{}
	err = r.Get(ctx, types.NamespacedName{Name: httpRoute.Name, Namespace: httpRoute.Namespace}, existingProxyRoute)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ProxyRoute
			logger.Info("Creating ProxyRoute", "name", proxyRoute.Name)
			if err := r.Create(ctx, proxyRoute); err != nil {
				logger.Error(err, "Failed to create ProxyRoute")
				return r.updateHTTPRouteStatus(ctx, httpRoute, metav1.Condition{
					Type:               string(gatewayv1.RouteConditionAccepted),
					Status:             metav1.ConditionFalse,
					Reason:             "CreationFailed",
					Message:            fmt.Sprintf("Failed to create ProxyRoute: %v", err),
					ObservedGeneration: httpRoute.Generation,
					LastTransitionTime: metav1.Now(),
				})
			}
		} else {
			logger.Error(err, "Failed to get ProxyRoute")
			return ctrl.Result{}, err
		}
	} else {
		// Update existing ProxyRoute
		logger.Info("Updating ProxyRoute", "name", proxyRoute.Name)
		existingProxyRoute.Spec = proxyRoute.Spec
		existingProxyRoute.Labels = proxyRoute.Labels
		existingProxyRoute.Annotations = proxyRoute.Annotations
		if err := r.Update(ctx, existingProxyRoute); err != nil {
			logger.Error(err, "Failed to update ProxyRoute")
			return r.updateHTTPRouteStatus(ctx, httpRoute, metav1.Condition{
				Type:               string(gatewayv1.RouteConditionAccepted),
				Status:             metav1.ConditionFalse,
				Reason:             "UpdateFailed",
				Message:            fmt.Sprintf("Failed to update ProxyRoute: %v", err),
				ObservedGeneration: httpRoute.Generation,
				LastTransitionTime: metav1.Now(),
			})
		}
	}

	// Update HTTPRoute status
	var parentStatuses []gatewayv1.RouteParentStatus
	for _, parentRef := range httpRoute.Spec.ParentRefs {
		parentStatus := gatewayv1.RouteParentStatus{
			ParentRef:      parentRef,
			ControllerName: gatewayv1.GatewayController("novaedge.io/gateway-controller"),
			Conditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.RouteConditionAccepted),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.RouteReasonAccepted),
					Message:            "HTTPRoute has been accepted and translated to ProxyRoute",
					ObservedGeneration: httpRoute.Generation,
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               string(gatewayv1.RouteConditionResolvedRefs),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.RouteReasonResolvedRefs),
					Message:            "All backend references have been resolved",
					ObservedGeneration: httpRoute.Generation,
					LastTransitionTime: metav1.Now(),
				},
			},
		}
		parentStatuses = append(parentStatuses, parentStatus)
	}

	httpRoute.Status.RouteStatus.Parents = parentStatuses

	if err := r.Status().Update(ctx, httpRoute); err != nil {
		logger.Error(err, "Failed to update HTTPRoute status")
		return ctrl.Result{}, err
	}

	// Trigger config update for all nodes
	TriggerConfigUpdate()

	logger.Info("Successfully reconciled HTTPRoute")
	return ctrl.Result{}, nil
}

// reconcileBackends creates or updates ProxyBackend resources for HTTPRoute backend refs
func (r *HTTPRouteReconciler) reconcileBackends(ctx context.Context, httpRoute *gatewayv1.HTTPRoute) error {
	logger := log.FromContext(ctx)

	// Collect all unique backend refs from all rules
	backendRefs := make(map[string]gatewayv1.HTTPBackendRef)
	for _, rule := range httpRoute.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			// Only handle Service backend refs
			if backendRef.Kind == nil || *backendRef.Kind == "Service" {
				namespace := httpRoute.Namespace
				if backendRef.Namespace != nil {
					namespace = string(*backendRef.Namespace)
				}

				port := int32(80)
				if backendRef.Port != nil {
					port = int32(*backendRef.Port)
				}

				key := GenerateProxyBackendName(string(backendRef.Name), namespace, port)
				backendRefs[key] = backendRef
			}
		}
	}

	// Create or update ProxyBackend for each unique backend ref
	for backendName, backendRef := range backendRefs {
		namespace := httpRoute.Namespace
		if backendRef.Namespace != nil {
			namespace = string(*backendRef.Namespace)
		}

		port := int32(80)
		if backendRef.Port != nil {
			port = int32(*backendRef.Port)
		}

		// Verify Service exists
		service := &corev1.Service{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      string(backendRef.Name),
			Namespace: namespace,
		}, service)
		if err != nil {
			if errors.IsNotFound(err) {
				logger.Error(err, "Backend Service not found",
					"service", backendRef.Name,
					"namespace", namespace)
				return fmt.Errorf("service %s/%s not found", namespace, backendRef.Name)
			}
			return err
		}

		// Create ProxyBackend
		proxyBackend := &novaedgev1alpha1.ProxyBackend{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backendName,
				Namespace: namespace,
				Annotations: map[string]string{
					OwnerAnnotation: fmt.Sprintf("HTTPRoute/%s/%s", httpRoute.Namespace, httpRoute.Name),
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: gatewayv1.GroupVersion.String(),
						Kind:       "HTTPRoute",
						Name:       httpRoute.Name,
						UID:        httpRoute.UID,
						Controller: boolPtr(true),
					},
				},
			},
			Spec: novaedgev1alpha1.ProxyBackendSpec{
				ServiceRef: &novaedgev1alpha1.ServiceReference{
					Name: string(backendRef.Name),
					Port: port,
				},
				LBPolicy: novaedgev1alpha1.LBPolicyRoundRobin,
			},
		}

		// Check if ProxyBackend already exists
		existingBackend := &novaedgev1alpha1.ProxyBackend{}
		err = r.Get(ctx, types.NamespacedName{Name: backendName, Namespace: namespace}, existingBackend)
		if err != nil {
			if errors.IsNotFound(err) {
				// Create new ProxyBackend
				logger.Info("Creating ProxyBackend", "name", backendName, "namespace", namespace)
				if err := r.Create(ctx, proxyBackend); err != nil {
					logger.Error(err, "Failed to create ProxyBackend")
					return err
				}
			} else {
				return err
			}
		} else {
			// Update existing ProxyBackend
			logger.Info("Updating ProxyBackend", "name", backendName, "namespace", namespace)
			existingBackend.Spec = proxyBackend.Spec
			existingBackend.Annotations = proxyBackend.Annotations
			if err := r.Update(ctx, existingBackend); err != nil {
				logger.Error(err, "Failed to update ProxyBackend")
				return err
			}
		}
	}

	return nil
}

// handleDeletion handles cleanup when an HTTPRoute is deleted
func (r *HTTPRouteReconciler) handleDeletion(ctx context.Context, httpRoute *gatewayv1.HTTPRoute) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling HTTPRoute deletion", "name", httpRoute.Name)

	// Delete associated ProxyRoute if it exists
	proxyRoute := &novaedgev1alpha1.ProxyRoute{}
	err := r.Get(ctx, types.NamespacedName{Name: httpRoute.Name, Namespace: httpRoute.Namespace}, proxyRoute)
	if err == nil {
		// ProxyRoute exists, delete it
		logger.Info("Deleting associated ProxyRoute", "name", proxyRoute.Name)
		if err := r.Delete(ctx, proxyRoute); err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "Failed to delete ProxyRoute")
			return ctrl.Result{}, err
		}
	} else if !errors.IsNotFound(err) {
		logger.Error(err, "Failed to get ProxyRoute for deletion")
		return ctrl.Result{}, err
	}

	// ProxyBackends will be automatically deleted via owner references

	// Remove finalizer if it exists
	if controllerutil.ContainsFinalizer(httpRoute, "novaedge.io/httproute-finalizer") {
		controllerutil.RemoveFinalizer(httpRoute, "novaedge.io/httproute-finalizer")
		if err := r.Update(ctx, httpRoute); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// updateHTTPRouteStatus updates the HTTPRoute status with the given condition
func (r *HTTPRouteReconciler) updateHTTPRouteStatus(ctx context.Context, httpRoute *gatewayv1.HTTPRoute, condition metav1.Condition) (ctrl.Result, error) {
	// Update all parent statuses with the condition
	for i := range httpRoute.Status.RouteStatus.Parents {
		meta.SetStatusCondition(&httpRoute.Status.RouteStatus.Parents[i].Conditions, condition)
	}

	if err := r.Status().Update(ctx, httpRoute); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *HTTPRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.HTTPRoute{}).
		Owns(&novaedgev1alpha1.ProxyRoute{}).
		Owns(&novaedgev1alpha1.ProxyBackend{}).
		Complete(r)
}
