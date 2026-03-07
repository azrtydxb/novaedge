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
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

var (
	errPort = errors.New("port")
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
	Scheme        *runtime.Scheme
	IngressClass  string // Configurable ingress class name to watch
	DefaultVIPRef string // Configurable default VIP reference for Ingress resources without explicit annotation
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
		if apierrors.IsNotFound(err) {
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

	// Translate Ingress to CRDs with service port resolver and configurable default VIP
	translator := NewIngressTranslatorWithOptions(ingress.Namespace, r.resolveServicePort, r.DefaultVIPRef)
	result, err := translator.Translate(ctx, ingress)
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

	// Update Ingress status with LoadBalancer IP
	if err := r.updateIngressStatus(ctx, ingress, result.Gateway.Spec.VIPRef); err != nil {
		logger.Error(err, "Failed to update Ingress status")
		// Don't return error, status update is not critical
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
	expected := r.IngressClass
	if expected == "" {
		expected = IngressClassName
	}
	return ingressClass == expected
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

// reconcileResource creates or updates a NovaEdge proxy resource.
// The kind parameter is used for logging (e.g. "ProxyGateway").
func (r *IngressReconciler) reconcileResource(ctx context.Context, kind string, desired, existing client.Object, applySpec func()) error {
	logger := log.FromContext(ctx)

	err := r.Get(ctx, client.ObjectKey{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}, existing)

	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Creating "+kind, "name", desired.GetName())
			if err := r.Create(ctx, desired); err != nil {
				return fmt.Errorf("failed to create %s: %w", kind, err)
			}
			return nil
		}
		return fmt.Errorf("failed to get %s: %w", kind, err)
	}

	logger.Info("Updating "+kind, "name", desired.GetName())
	applySpec()
	existing.SetLabels(desired.GetLabels())
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update %s: %w", kind, err)
	}

	return nil
}

// reconcileGateway creates or updates a ProxyGateway
func (r *IngressReconciler) reconcileGateway(ctx context.Context, desired *novaedgev1alpha1.ProxyGateway) error {
	existing := &novaedgev1alpha1.ProxyGateway{}
	return r.reconcileResource(ctx, "ProxyGateway", desired, existing, func() { existing.Spec = desired.Spec })
}

// reconcileRoute creates or updates a ProxyRoute
func (r *IngressReconciler) reconcileRoute(ctx context.Context, desired *novaedgev1alpha1.ProxyRoute) error {
	existing := &novaedgev1alpha1.ProxyRoute{}
	return r.reconcileResource(ctx, "ProxyRoute", desired, existing, func() { existing.Spec = desired.Spec })
}

// reconcileBackend creates or updates a ProxyBackend
func (r *IngressReconciler) reconcileBackend(ctx context.Context, desired *novaedgev1alpha1.ProxyBackend) error {
	existing := &novaedgev1alpha1.ProxyBackend{}
	return r.reconcileResource(ctx, "ProxyBackend", desired, existing, func() { existing.Spec = desired.Spec })
}

// SetupWithManager sets up the controller with the Manager.
// Configures exponential backoff rate limiting (5ms base, 1000s max) to
// prevent API server overload during error storms.
func (r *IngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&novaedgev1alpha1.ProxyGateway{}).
		Owns(&novaedgev1alpha1.ProxyRoute{}).
		Owns(&novaedgev1alpha1.ProxyBackend{}).
		WithOptions(controller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Millisecond, 1000*time.Second),
		}).
		Complete(r)
}

// resolveServicePort resolves a service port name to its port number
func (r *IngressReconciler) resolveServicePort(ctx context.Context, namespace, serviceName, portName string) (int32, error) {
	svc := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: serviceName}, svc); err != nil {
		return 0, fmt.Errorf("failed to get service %s/%s: %w", namespace, serviceName, err)
	}

	// Find the port by name
	for _, port := range svc.Spec.Ports {
		if port.Name == portName {
			return port.Port, nil
		}
	}

	return 0, fmt.Errorf("%w: %s not found in service %s/%s", errPort, portName, namespace, serviceName)
}

// updateIngressStatus is a no-op placeholder. VIP-based status has been removed;
// the Ingress status is now set by the external LoadBalancer controller.
func (r *IngressReconciler) updateIngressStatus(_ context.Context, _ *networkingv1.Ingress, _ string) error {
	return nil
}
