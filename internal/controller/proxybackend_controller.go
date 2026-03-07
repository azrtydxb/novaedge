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

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

// ProxyBackendReconciler reconciles a ProxyBackend object
type ProxyBackendReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxybackends,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxybackends/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxybackends/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *ProxyBackendReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	backend := &novaedgev1alpha1.ProxyBackend{}
	return reconcileWithGenerationCheck(ctx, r.Client, req, backend, "ProxyBackend",
		func() int64 { return backend.Status.ObservedGeneration },
		func() []interface{} { return []interface{}{"name", backend.Name, "lbPolicy", backend.Spec.LBPolicy} },
		func() error { return r.validateAndUpdateStatus(ctx, backend) },
	)
}

// validateServiceRef validates the backend's service reference exists and the port is valid.
func (r *ProxyBackendReconciler) validateServiceRef(ctx context.Context, backend *novaedgev1alpha1.ProxyBackend) []string {
	if backend.Spec.ServiceRef == nil {
		return nil
	}

	serviceNamespace := backend.Namespace
	if backend.Spec.ServiceRef.Namespace != nil {
		serviceNamespace = *backend.Spec.ServiceRef.Namespace
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      backend.Spec.ServiceRef.Name,
		Namespace: serviceNamespace,
	}, service); err != nil {
		if apierrors.IsNotFound(err) {
			return []string{fmt.Sprintf("Service %s not found", backend.Spec.ServiceRef.Name)}
		}
		log.FromContext(ctx).Error(err, "Failed to get service", "service", backend.Spec.ServiceRef.Name)
		return nil
	}

	for _, port := range service.Spec.Ports {
		if port.Port == backend.Spec.ServiceRef.Port {
			return nil
		}
	}
	return []string{fmt.Sprintf("Port %d not found in service %s", backend.Spec.ServiceRef.Port, backend.Spec.ServiceRef.Name)}
}

// validateHealthCheck validates the backend's health check configuration.
func validateHealthCheck(hc *novaedgev1alpha1.HealthCheck) []string {
	if hc == nil {
		return nil
	}
	var errs []string
	if hc.HealthyThreshold != nil && *hc.HealthyThreshold < 1 {
		errs = append(errs, "HealthyThreshold must be >= 1")
	}
	if hc.UnhealthyThreshold != nil && *hc.UnhealthyThreshold < 1 {
		errs = append(errs, "UnhealthyThreshold must be >= 1")
	}
	return errs
}

// validateCircuitBreaker validates the backend's circuit breaker configuration.
func validateCircuitBreaker(cb *novaedgev1alpha1.CircuitBreaker) []string {
	if cb == nil {
		return nil
	}
	var errs []string
	if cb.MaxConnections != nil && *cb.MaxConnections < 1 {
		errs = append(errs, "CircuitBreaker MaxConnections must be >= 1")
	}
	if cb.MaxPendingRequests != nil && *cb.MaxPendingRequests < 1 {
		errs = append(errs, "CircuitBreaker MaxPendingRequests must be >= 1")
	}
	if cb.MaxRequests != nil && *cb.MaxRequests < 1 {
		errs = append(errs, "CircuitBreaker MaxRequests must be >= 1")
	}
	if cb.MaxRetries != nil && *cb.MaxRetries < 0 {
		errs = append(errs, "CircuitBreaker MaxRetries must be >= 0")
	}
	return errs
}

// validateTLSCACert validates the backend's TLS CA cert secret reference.
func (r *ProxyBackendReconciler) validateTLSCACert(ctx context.Context, backend *novaedgev1alpha1.ProxyBackend) []string {
	if backend.Spec.TLS == nil || !backend.Spec.TLS.Enabled || backend.Spec.TLS.CACertSecretRef == nil {
		return nil
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      *backend.Spec.TLS.CACertSecretRef,
		Namespace: backend.Namespace,
	}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return []string{fmt.Sprintf("TLS CA cert secret %s not found", *backend.Spec.TLS.CACertSecretRef)}
		}
		return nil
	}

	if _, ok := secret.Data["ca.crt"]; !ok {
		return []string{fmt.Sprintf("TLS CA cert secret %s missing ca.crt", *backend.Spec.TLS.CACertSecretRef)}
	}
	return nil
}

// validateAndUpdateStatus validates the backend and updates its status
func (r *ProxyBackendReconciler) validateAndUpdateStatus(ctx context.Context, backend *novaedgev1alpha1.ProxyBackend) error {
	logger := log.FromContext(ctx)

	validationErrors := make([]string, 0, 4)
	validationErrors = append(validationErrors, r.validateServiceRef(ctx, backend)...)
	validationErrors = append(validationErrors, validateHealthCheck(backend.Spec.HealthCheck)...)
	validationErrors = append(validationErrors, validateCircuitBreaker(backend.Spec.CircuitBreaker)...)
	validationErrors = append(validationErrors, r.validateTLSCACert(ctx, backend)...)

	// Update status conditions
	condition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: backend.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if len(validationErrors) > 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = ConditionReasonValidationFailed
		condition.Message = fmt.Sprintf("Validation errors: %v", validationErrors)
		logger.Info("Backend validation failed", "errors", validationErrors)
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = ConditionReasonValid
		condition.Message = "Backend configuration is valid"
	}

	// Update status
	meta.SetStatusCondition(&backend.Status.Conditions, condition)
	backend.Status.ObservedGeneration = backend.Generation

	if err := r.Status().Update(ctx, backend); err != nil {
		logger.Error(err, "Failed to update backend status")
		return err
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("%w: %v", errValidationFailed, validationErrors)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ProxyBackendReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyBackend{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
