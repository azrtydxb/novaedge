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

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

const (
	// IngressClassName is the ingress class that this controller handles
	IngressClassName = "novaedge"
	// IngressFinalizerName is the finalizer added to Ingress resources
	IngressFinalizerName = "novaedge.io/ingress-finalizer"
)

// IngressReconciler reconciles Kubernetes Ingress objects
type IngressReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/finalizers,verbs=update
// +kubebuilder:rbac:groups=novaedge.io,resources=proxygateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyroutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxybackends,verbs=get;list;watch;create;update;patch;delete

// Reconcile processes Ingress resources and translates them to NovaEdge CRDs
func (r *IngressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Ingress instance
	ingress := &networkingv1.Ingress{}
	err := r.Get(ctx, req.NamespacedName, ingress)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Ingress resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Ingress")
		return ctrl.Result{}, err
	}

	// Check if this Ingress is for NovaEdge
	if !r.shouldProcessIngress(ingress) {
		logger.Info("Ingress is not for NovaEdge, skipping", "ingressClass", r.getIngressClass(ingress))
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling Ingress", "name", ingress.Name, "namespace", ingress.Namespace)

	// Handle deletion
	if ingress.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, ingress)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(ingress, IngressFinalizerName) {
		controllerutil.AddFinalizer(ingress, IngressFinalizerName)
		if err := r.Update(ctx, ingress); err != nil {
			logger.Error(err, "Failed to add finalizer to Ingress")
			return ctrl.Result{}, err
		}
	}

	// Translate Ingress to CRDs
	translator := NewIngressTranslator(ingress.Namespace)
	result, err := translator.Translate(ingress)
	if err != nil {
		logger.Error(err, "Failed to translate Ingress to CRDs")
		return ctrl.Result{}, err
	}

	// Create or update ProxyGateway
	if err := r.reconcileGateway(ctx, result.Gateway); err != nil {
		logger.Error(err, "Failed to reconcile ProxyGateway")
		return ctrl.Result{}, err
	}

	// Create or update ProxyRoutes
	for _, route := range result.Routes {
		if err := r.reconcileRoute(ctx, route); err != nil {
			logger.Error(err, "Failed to reconcile ProxyRoute", "route", route.Name)
			return ctrl.Result{}, err
		}
	}

	// Create or update ProxyBackends
	for _, backend := range result.Backends {
		if err := r.reconcileBackend(ctx, backend); err != nil {
			logger.Error(err, "Failed to reconcile ProxyBackend", "backend", backend.Name)
			return ctrl.Result{}, err
		}
	}

	// Trigger config update for all nodes
	TriggerConfigUpdate()

	logger.Info("Successfully reconciled Ingress",
		"gateway", result.Gateway.Name,
		"routes", len(result.Routes),
		"backends", len(result.Backends))

	return ctrl.Result{}, nil
}

// shouldProcessIngress checks if this Ingress should be processed by NovaEdge
func (r *IngressReconciler) shouldProcessIngress(ingress *networkingv1.Ingress) bool {
	ingressClass := r.getIngressClass(ingress)
	return ingressClass == IngressClassName
}

// getIngressClass returns the ingress class for the given Ingress
func (r *IngressReconciler) getIngressClass(ingress *networkingv1.Ingress) string {
	// Check spec field first (preferred)
	if ingress.Spec.IngressClassName != nil {
		return *ingress.Spec.IngressClassName
	}
	// Fallback to annotation
	if className, exists := ingress.Annotations["kubernetes.io/ingress.class"]; exists {
		return className
	}
	return ""
}

// handleDeletion handles cleanup when an Ingress is deleted
func (r *IngressReconciler) handleDeletion(ctx context.Context, ingress *networkingv1.Ingress) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(ingress, IngressFinalizerName) {
		return ctrl.Result{}, nil
	}

	logger.Info("Handling Ingress deletion", "name", ingress.Name)

	// Delete owned resources (Gateway, Routes, Backends)
	// These will be automatically deleted due to owner references,
	// but we can also explicitly delete them if needed

	// Remove finalizer
	controllerutil.RemoveFinalizer(ingress, IngressFinalizerName)
	if err := r.Update(ctx, ingress); err != nil {
		logger.Error(err, "Failed to remove finalizer from Ingress")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully cleaned up Ingress resources")
	return ctrl.Result{}, nil
}

// reconcileGateway creates or updates a ProxyGateway
func (r *IngressReconciler) reconcileGateway(ctx context.Context, desired *novaedgev1alpha1.ProxyGateway) error {
	logger := log.FromContext(ctx)

	existing := &novaedgev1alpha1.ProxyGateway{}
	err := r.Get(ctx, client.ObjectKey{
		Name:      desired.Name,
		Namespace: desired.Namespace,
	}, existing)

	if err != nil {
		if errors.IsNotFound(err) {
			// Create new gateway
			logger.Info("Creating ProxyGateway", "name", desired.Name)
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create ProxyGateway: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get ProxyGateway: %w", err)
	}

	// Update existing gateway
	logger.Info("Updating ProxyGateway", "name", desired.Name)
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update ProxyGateway: %w", err)
	}

	return nil
}

// reconcileRoute creates or updates a ProxyRoute
func (r *IngressReconciler) reconcileRoute(ctx context.Context, desired *novaedgev1alpha1.ProxyRoute) error {
	logger := log.FromContext(ctx)

	existing := &novaedgev1alpha1.ProxyRoute{}
	err := r.Get(ctx, client.ObjectKey{
		Name:      desired.Name,
		Namespace: desired.Namespace,
	}, existing)

	if err != nil {
		if errors.IsNotFound(err) {
			// Create new route
			logger.Info("Creating ProxyRoute", "name", desired.Name)
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create ProxyRoute: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get ProxyRoute: %w", err)
	}

	// Update existing route
	logger.Info("Updating ProxyRoute", "name", desired.Name)
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update ProxyRoute: %w", err)
	}

	return nil
}

// reconcileBackend creates or updates a ProxyBackend
func (r *IngressReconciler) reconcileBackend(ctx context.Context, desired *novaedgev1alpha1.ProxyBackend) error {
	logger := log.FromContext(ctx)

	existing := &novaedgev1alpha1.ProxyBackend{}
	err := r.Get(ctx, client.ObjectKey{
		Name:      desired.Name,
		Namespace: desired.Namespace,
	}, existing)

	if err != nil {
		if errors.IsNotFound(err) {
			// Create new backend
			logger.Info("Creating ProxyBackend", "name", desired.Name)
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create ProxyBackend: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to get ProxyBackend: %w", err)
	}

	// Update existing backend
	logger.Info("Updating ProxyBackend", "name", desired.Name)
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update ProxyBackend: %w", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *IngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Owns(&novaedgev1alpha1.ProxyGateway{}).
		Owns(&novaedgev1alpha1.ProxyRoute{}).
		Owns(&novaedgev1alpha1.ProxyBackend{}).
		Complete(r)
}
