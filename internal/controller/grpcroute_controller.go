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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	"github.com/azrtydxb/novaedge/internal/controller/snapshot"
)

// GRPCRouteReconciler reconciles Gateway API GRPCRoute resources
type GRPCRouteReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	ConfigServer *snapshot.Server
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=grpcroutes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=grpcroutes/status,verbs=get;update;patch

// Reconcile handles GRPCRoute create/update/delete events
func (r *GRPCRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the GRPCRoute instance
	grpcRoute := &gatewayv1.GRPCRoute{}
	err := r.Get(ctx, req.NamespacedName, grpcRoute)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("GRPCRoute resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get GRPCRoute")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling GRPCRoute", "name", grpcRoute.Name, "namespace", grpcRoute.Namespace)

	// Handle deletion
	if !grpcRoute.DeletionTimestamp.IsZero() {
		return r.handleGRPCRouteDeletion(ctx, grpcRoute)
	}

	// Check if any parent refs point to our Gateway
	hasNovaEdgeGateway := false
	for _, parentRef := range grpcRoute.Spec.ParentRefs {
		kind := kindGateway
		if parentRef.Kind != nil {
			kind = string(*parentRef.Kind)
		}
		if kind == kindGateway {
			gatewayNamespace := grpcRoute.Namespace
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
		logger.Info("GRPCRoute does not reference a NovaEdge Gateway, ignoring")
		return ctrl.Result{}, nil
	}

	// Translate GRPCRoute to ProxyRoute
	proxyRoute, err := TranslateGRPCRouteToProxyRoute(grpcRoute)
	if err != nil {
		logger.Error(err, "Failed to translate GRPCRoute to ProxyRoute")
		return r.updateGRPCRouteStatus(ctx, grpcRoute, metav1.Condition{
			Type:               string(gatewayv1.RouteConditionAccepted),
			Status:             metav1.ConditionFalse,
			Reason:             "Invalid",
			Message:            fmt.Sprintf("Translation failed: %v", err),
			ObservedGeneration: grpcRoute.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	// Reconcile backends for gRPC services
	if err := r.reconcileGRPCBackends(ctx, grpcRoute); err != nil {
		logger.Error(err, "Failed to reconcile gRPC backends")
		return r.updateGRPCRouteStatus(ctx, grpcRoute, metav1.Condition{
			Type:               string(gatewayv1.RouteConditionAccepted),
			Status:             metav1.ConditionFalse,
			Reason:             string(gatewayv1.RouteReasonBackendNotFound),
			Message:            fmt.Sprintf("Backend reconciliation failed: %v", err),
			ObservedGeneration: grpcRoute.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}

	// Create or update the ProxyRoute
	existingProxyRoute := &novaedgev1alpha1.ProxyRoute{}
	grpcRouteName := "grpc-" + grpcRoute.Name
	err = r.Get(ctx, types.NamespacedName{Name: grpcRouteName, Namespace: grpcRoute.Namespace}, existingProxyRoute)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Creating ProxyRoute for GRPCRoute", "name", grpcRouteName)
			if err := r.Create(ctx, proxyRoute); err != nil {
				logger.Error(err, "Failed to create ProxyRoute for GRPCRoute")
				return r.updateGRPCRouteStatus(ctx, grpcRoute, metav1.Condition{
					Type:               string(gatewayv1.RouteConditionAccepted),
					Status:             metav1.ConditionFalse,
					Reason:             "CreationFailed",
					Message:            fmt.Sprintf("Failed to create ProxyRoute: %v", err),
					ObservedGeneration: grpcRoute.Generation,
					LastTransitionTime: metav1.Now(),
				})
			}
		} else {
			logger.Error(err, "Failed to get ProxyRoute for GRPCRoute")
			return ctrl.Result{}, err
		}
	} else {
		logger.Info("Updating ProxyRoute for GRPCRoute", "name", grpcRouteName)
		existingProxyRoute.Spec = proxyRoute.Spec
		existingProxyRoute.Labels = proxyRoute.Labels
		existingProxyRoute.Annotations = proxyRoute.Annotations
		if err := r.Update(ctx, existingProxyRoute); err != nil {
			logger.Error(err, "Failed to update ProxyRoute for GRPCRoute")
			return r.updateGRPCRouteStatus(ctx, grpcRoute, metav1.Condition{
				Type:               string(gatewayv1.RouteConditionAccepted),
				Status:             metav1.ConditionFalse,
				Reason:             "UpdateFailed",
				Message:            fmt.Sprintf("Failed to update ProxyRoute: %v", err),
				ObservedGeneration: grpcRoute.Generation,
				LastTransitionTime: metav1.Now(),
			})
		}
	}

	// Update GRPCRoute status
	parentStatuses := make([]gatewayv1.RouteParentStatus, 0, len(grpcRoute.Spec.ParentRefs))
	for _, parentRef := range grpcRoute.Spec.ParentRefs {
		parentStatus := gatewayv1.RouteParentStatus{
			ParentRef:      parentRef,
			ControllerName: gatewayv1.GatewayController("novaedge.io/gateway-controller"),
			Conditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.RouteConditionAccepted),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.RouteReasonAccepted),
					Message:            "GRPCRoute has been accepted and translated to ProxyRoute",
					ObservedGeneration: grpcRoute.Generation,
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               string(gatewayv1.RouteConditionResolvedRefs),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.RouteReasonResolvedRefs),
					Message:            "All gRPC backend references have been resolved",
					ObservedGeneration: grpcRoute.Generation,
					LastTransitionTime: metav1.Now(),
				},
			},
		}
		parentStatuses = append(parentStatuses, parentStatus)
	}

	grpcRoute.Status.Parents = parentStatuses
	if err := r.Status().Update(ctx, grpcRoute); err != nil {
		logger.Error(err, "Failed to update GRPCRoute status")
		return ctrl.Result{}, err
	}

	triggerConfigUpdate(r.ConfigServer)

	logger.Info("Successfully reconciled GRPCRoute")
	return ctrl.Result{}, nil
}

// reconcileGRPCBackends creates or updates ProxyBackend resources for GRPCRoute backend refs
func (r *GRPCRouteReconciler) reconcileGRPCBackends(ctx context.Context, grpcRoute *gatewayv1.GRPCRoute) error {
	logger := log.FromContext(ctx)

	backendRefs := make(map[string]gatewayv1.GRPCBackendRef)
	for _, rule := range grpcRoute.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			if backendRef.Kind == nil || *backendRef.Kind == "Service" {
				namespace := grpcRoute.Namespace
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

	for backendName, backendRef := range backendRefs {
		namespace := grpcRoute.Namespace
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
				logger.Error(err, "gRPC backend Service not found",
					"service", backendRef.Name,
					"namespace", namespace)
				return fmt.Errorf("%w: %s/%s not found", errService, namespace, backendRef.Name)
			}
			return err
		}

		proxyBackend := &novaedgev1alpha1.ProxyBackend{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backendName,
				Namespace: namespace,
				Annotations: map[string]string{
					OwnerAnnotation: fmt.Sprintf("GRPCRoute/%s/%s", grpcRoute.Namespace, grpcRoute.Name),
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: gatewayv1.GroupVersion.String(),
						Kind:       "GRPCRoute",
						Name:       grpcRoute.Name,
						UID:        grpcRoute.UID,
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

		existingBackend := &novaedgev1alpha1.ProxyBackend{}
		err = r.Get(ctx, types.NamespacedName{Name: backendName, Namespace: namespace}, existingBackend)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("Creating ProxyBackend for gRPC service", "name", backendName)
				if err := r.Create(ctx, proxyBackend); err != nil {
					logger.Error(err, "Failed to create ProxyBackend for gRPC service")
					return err
				}
			} else {
				return err
			}
		} else {
			logger.Info("Updating ProxyBackend for gRPC service", "name", backendName)
			existingBackend.Spec = proxyBackend.Spec
			existingBackend.Annotations = proxyBackend.Annotations
			if err := r.Update(ctx, existingBackend); err != nil {
				logger.Error(err, "Failed to update ProxyBackend for gRPC service")
				return err
			}
		}
	}

	return nil
}

// handleGRPCRouteDeletion handles cleanup when a GRPCRoute is deleted
func (r *GRPCRouteReconciler) handleGRPCRouteDeletion(ctx context.Context, grpcRoute *gatewayv1.GRPCRoute) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling GRPCRoute deletion", "name", grpcRoute.Name)

	grpcRouteName := "grpc-" + grpcRoute.Name
	proxyRoute := &novaedgev1alpha1.ProxyRoute{}
	err := r.Get(ctx, types.NamespacedName{Name: grpcRouteName, Namespace: grpcRoute.Namespace}, proxyRoute)
	if err == nil {
		logger.Info("Deleting associated ProxyRoute", "name", proxyRoute.Name)
		if err := r.Delete(ctx, proxyRoute); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "Failed to delete ProxyRoute")
			return ctrl.Result{}, err
		}
	} else if !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to get ProxyRoute for deletion")
		return ctrl.Result{}, err
	}

	if controllerutil.ContainsFinalizer(grpcRoute, "novaedge.io/grpcroute-finalizer") {
		controllerutil.RemoveFinalizer(grpcRoute, "novaedge.io/grpcroute-finalizer")
		if err := r.Update(ctx, grpcRoute); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// updateGRPCRouteStatus updates the GRPCRoute status with the given condition
func (r *GRPCRouteReconciler) updateGRPCRouteStatus(ctx context.Context, grpcRoute *gatewayv1.GRPCRoute, condition metav1.Condition) (ctrl.Result, error) {
	for i := range grpcRoute.Status.Parents {
		meta.SetStatusCondition(&grpcRoute.Status.Parents[i].Conditions, condition)
	}

	if err := r.Status().Update(ctx, grpcRoute); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *GRPCRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.GRPCRoute{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&novaedgev1alpha1.ProxyRoute{}).
		Owns(&novaedgev1alpha1.ProxyBackend{}).
		Complete(r)
}
