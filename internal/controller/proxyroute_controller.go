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
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// ProxyRouteReconciler reconciles a ProxyRoute object
type ProxyRouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxyroutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyroutes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyroutes/finalizers,verbs=update
// +kubebuilder:rbac:groups=novaedge.io,resources=proxybackends,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *ProxyRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ProxyRoute instance
	route := &novaedgev1alpha1.ProxyRoute{}
	err := r.Get(ctx, req.NamespacedName, route)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ProxyRoute resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ProxyRoute")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling ProxyRoute", "name", route.Name, "hostnames", route.Spec.Hostnames)

	// Validate and update status
	if err := r.validateAndUpdateStatus(ctx, route); err != nil {
		logger.Error(err, "Failed to validate route")
		return ctrl.Result{Requeue: true}, err
	}

	// Trigger config update for all nodes
	TriggerConfigUpdate()

	return ctrl.Result{}, nil
}

// validateAndUpdateStatus validates the route and updates its status
func (r *ProxyRouteReconciler) validateAndUpdateStatus(ctx context.Context, route *novaedgev1alpha1.ProxyRoute) error {
	logger := log.FromContext(ctx)
	var validationErrors []string

	// Validate backend references exist
	for i, rule := range route.Spec.Rules {
		backendNamespace := route.Namespace
		if rule.BackendRef.Namespace != nil {
			backendNamespace = *rule.BackendRef.Namespace
		}

		backend := &novaedgev1alpha1.ProxyBackend{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      rule.BackendRef.Name,
			Namespace: backendNamespace,
		}, backend); err != nil {
			if errors.IsNotFound(err) {
				validationErrors = append(validationErrors,
					fmt.Sprintf("Backend %s not found for rule %d", rule.BackendRef.Name, i))
			} else {
				logger.Error(err, "Failed to get backend", "backend", rule.BackendRef.Name)
			}
		}
	}

	// Validate match conditions are well-formed
	for i, rule := range route.Spec.Rules {
		for j, match := range rule.Matches {
			// Validate path match if present
			if match.Path != nil {
				if match.Path.Value == "" {
					validationErrors = append(validationErrors,
						fmt.Sprintf("Rule %d match %d has empty path value", i, j))
				}
			}

			// Validate header matches
			for k, header := range match.Headers {
				if header.Name == "" {
					validationErrors = append(validationErrors,
						fmt.Sprintf("Rule %d match %d header %d has empty name", i, j, k))
				}
			}
		}
	}

	// Update status conditions
	condition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: route.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if len(validationErrors) > 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ValidationFailed"
		condition.Message = fmt.Sprintf("Validation errors: %v", validationErrors)
		logger.Info("Route validation failed", "errors", validationErrors)
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "Valid"
		condition.Message = "Route configuration is valid"
	}

	// Update status
	meta.SetStatusCondition(&route.Status.Conditions, condition)
	route.Status.ObservedGeneration = route.Generation

	if err := r.Status().Update(ctx, route); err != nil {
		logger.Error(err, "Failed to update route status")
		return err
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("validation failed: %v", validationErrors)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ProxyRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyRoute{}).
		Complete(r)
}
