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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
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
		if apierrors.IsNotFound(err) {
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

	// Build parent statuses for each parent ref
	parentStatuses := make([]gatewayv1.RouteParentStatus, 0, len(httpRoute.Spec.ParentRefs))
	hasNovaEdgeGateway := false

	for _, parentRef := range httpRoute.Spec.ParentRefs {
		parentStatus := gatewayv1.RouteParentStatus{
			ParentRef:      parentRef,
			ControllerName: gatewayv1.GatewayController(NovaEdgeControllerName),
			Conditions:     make([]metav1.Condition, 0, 2),
		}

		// Per Gateway API spec, Kind defaults to "Gateway" when nil
		kind := kindGateway
		if parentRef.Kind != nil {
			kind = string(*parentRef.Kind)
		}

		if kind != kindGateway {
			parentStatus.Conditions = append(parentStatus.Conditions, metav1.Condition{
				Type:               string(gatewayv1.RouteConditionAccepted),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.RouteReasonNotAllowedByListeners),
				Message:            fmt.Sprintf("Unsupported parent kind: %s", kind),
				ObservedGeneration: httpRoute.Generation,
				LastTransitionTime: metav1.Now(),
			})
			parentStatuses = append(parentStatuses, parentStatus)
			continue
		}

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

		if err != nil {
			if apierrors.IsNotFound(err) {
				parentStatus.Conditions = append(parentStatus.Conditions, metav1.Condition{
					Type:               string(gatewayv1.RouteConditionAccepted),
					Status:             metav1.ConditionFalse,
					Reason:             "GatewayNotFound",
					Message:            fmt.Sprintf("Gateway %s/%s not found", gatewayNamespace, parentRef.Name),
					ObservedGeneration: httpRoute.Generation,
					LastTransitionTime: metav1.Now(),
				})
			} else {
				parentStatus.Conditions = append(parentStatus.Conditions, metav1.Condition{
					Type:               string(gatewayv1.RouteConditionAccepted),
					Status:             metav1.ConditionFalse,
					Reason:             "GatewayError",
					Message:            fmt.Sprintf("Failed to get Gateway: %v", err),
					ObservedGeneration: httpRoute.Generation,
					LastTransitionTime: metav1.Now(),
				})
			}
			parentStatuses = append(parentStatuses, parentStatus)
			continue
		}

		// Check if this is a NovaEdge gateway
		if string(gateway.Spec.GatewayClassName) != NovaEdgeGatewayClassName {
			// Not our gateway - skip it entirely (don't set status for other controllers)
			continue
		}

		hasNovaEdgeGateway = true

		// Validate listener attachment if SectionName is specified
		listenerMatch := true
		if parentRef.SectionName != nil {
			listenerMatch = false
			for _, listener := range gateway.Spec.Listeners {
				if string(listener.Name) == string(*parentRef.SectionName) {
					listenerMatch = true
					break
				}
			}
		}

		if !listenerMatch {
			parentStatus.Conditions = append(parentStatus.Conditions, metav1.Condition{
				Type:               string(gatewayv1.RouteConditionAccepted),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.RouteReasonNoMatchingParent),
				Message:            fmt.Sprintf("No listener matches SectionName %s", *parentRef.SectionName),
				ObservedGeneration: httpRoute.Generation,
				LastTransitionTime: metav1.Now(),
			})
			parentStatuses = append(parentStatuses, parentStatus)
			continue
		}

		// Route is accepted by this parent
		parentStatus.Conditions = append(parentStatus.Conditions, metav1.Condition{
			Type:               string(gatewayv1.RouteConditionAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.RouteReasonAccepted),
			Message:            "HTTPRoute has been accepted",
			ObservedGeneration: httpRoute.Generation,
			LastTransitionTime: metav1.Now(),
		})

		parentStatuses = append(parentStatuses, parentStatus)
	}

	if !hasNovaEdgeGateway {
		logger.Info("HTTPRoute does not reference a NovaEdge Gateway, ignoring")
		return ctrl.Result{}, nil
	}

	// Translate HTTPRoute to ProxyRoute
	proxyRoute, err := TranslateHTTPRouteToProxyRoute(httpRoute)
	if err != nil {
		logger.Error(err, "Failed to translate HTTPRoute to ProxyRoute")
		// Update all parent statuses with the error
		for i := range parentStatuses {
			meta.SetStatusCondition(&parentStatuses[i].Conditions, metav1.Condition{
				Type:               string(gatewayv1.RouteConditionAccepted),
				Status:             metav1.ConditionFalse,
				Reason:             "TranslationFailed",
				Message:            fmt.Sprintf("Translation failed: %v", err),
				ObservedGeneration: httpRoute.Generation,
				LastTransitionTime: metav1.Now(),
			})
		}
		httpRoute.Status.Parents = parentStatuses
		if statusErr := r.Status().Update(ctx, httpRoute); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile backends and track resolved refs status
	resolvedRefs := true
	resolvedRefsMessage := "All backend references have been resolved"
	resolvedRefsReason := string(gatewayv1.RouteReasonResolvedRefs)

	if err := r.reconcileBackends(ctx, httpRoute); err != nil {
		logger.Error(err, "Failed to reconcile backends")
		resolvedRefs = false
		resolvedRefsMessage = fmt.Sprintf("Backend reconciliation failed: %v", err)
		resolvedRefsReason = string(gatewayv1.RouteReasonBackendNotFound)
	}

	// Create or update the ProxyRoute
	existingProxyRoute := &novaedgev1alpha1.ProxyRoute{}
	err = r.Get(ctx, types.NamespacedName{Name: httpRoute.Name, Namespace: httpRoute.Namespace}, existingProxyRoute)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new ProxyRoute
			logger.Info("Creating ProxyRoute", "name", proxyRoute.Name)
			if err := r.Create(ctx, proxyRoute); err != nil {
				logger.Error(err, "Failed to create ProxyRoute")
				return ctrl.Result{}, err
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
			return ctrl.Result{}, err
		}
	}

	// Set ResolvedRefs condition on all parent statuses
	resolvedRefsStatus := metav1.ConditionTrue
	if !resolvedRefs {
		resolvedRefsStatus = metav1.ConditionFalse
	}

	for i := range parentStatuses {
		meta.SetStatusCondition(&parentStatuses[i].Conditions, metav1.Condition{
			Type:               string(gatewayv1.RouteConditionResolvedRefs),
			Status:             resolvedRefsStatus,
			Reason:             resolvedRefsReason,
			Message:            resolvedRefsMessage,
			ObservedGeneration: httpRoute.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	httpRoute.Status.Parents = parentStatuses

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
					port = *backendRef.Port
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
			port = *backendRef.Port
		}

		// Verify Service exists
		service := &corev1.Service{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      string(backendRef.Name),
			Namespace: namespace,
		}, service)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.Error(err, "Backend Service not found",
					"service", backendRef.Name,
					"namespace", namespace)
				return fmt.Errorf("%w: %s/%s not found", errService, namespace, backendRef.Name)
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
			if apierrors.IsNotFound(err) {
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

// handleDeletion handles cleanup when an HTTPRoute is deleted.
// ProxyBackends will be automatically deleted via owner references.
func (r *HTTPRouteReconciler) handleDeletion(ctx context.Context, httpRoute *gatewayv1.HTTPRoute) (ctrl.Result, error) {
	return handleResourceDeletion(ctx, r.Client, httpRoute, &novaedgev1alpha1.ProxyRoute{}, "HTTPRoute", "novaedge.io/httproute-finalizer")
}

// SetupWithManager sets up the controller with the Manager
func (r *HTTPRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.HTTPRoute{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&novaedgev1alpha1.ProxyRoute{}).
		Owns(&novaedgev1alpha1.ProxyBackend{}).
		Complete(r)
}
