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
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
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
	logger := log.FromContext(ctx)

	// Fetch the ProxyBackend instance
	backend := &novaedgev1alpha1.ProxyBackend{}
	err := r.Get(ctx, req.NamespacedName, backend)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ProxyBackend resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ProxyBackend")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling ProxyBackend", "name", backend.Name, "lbPolicy", backend.Spec.LBPolicy)

	// Validate and update status
	if err := r.validateAndUpdateStatus(ctx, backend); err != nil {
		logger.Error(err, "Failed to validate backend")
		return ctrl.Result{Requeue: true}, err
	}

	// Trigger config update for all nodes
	TriggerConfigUpdate()

	return ctrl.Result{}, nil
}

// validateAndUpdateStatus validates the backend and updates its status
func (r *ProxyBackendReconciler) validateAndUpdateStatus(ctx context.Context, backend *novaedgev1alpha1.ProxyBackend) error {
	logger := log.FromContext(ctx)
	var validationErrors []string

	// Validate serviceRef exists
	if backend.Spec.ServiceRef != nil {
		serviceNamespace := backend.Namespace
		if backend.Spec.ServiceRef.Namespace != nil {
			serviceNamespace = *backend.Spec.ServiceRef.Namespace
		}

		service := &corev1.Service{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      backend.Spec.ServiceRef.Name,
			Namespace: serviceNamespace,
		}, service); err != nil {
			if errors.IsNotFound(err) {
				validationErrors = append(validationErrors,
					fmt.Sprintf("Service %s not found", backend.Spec.ServiceRef.Name))
			} else {
				logger.Error(err, "Failed to get service", "service", backend.Spec.ServiceRef.Name)
			}
		} else {
			// Validate that the port exists in the service
			portFound := false
			for _, port := range service.Spec.Ports {
				if port.Port == backend.Spec.ServiceRef.Port {
					portFound = true
					break
				}
			}
			if !portFound {
				validationErrors = append(validationErrors,
					fmt.Sprintf("Port %d not found in service %s", backend.Spec.ServiceRef.Port, backend.Spec.ServiceRef.Name))
			}
		}
	}

	// Validate health check configuration if present
	if backend.Spec.HealthCheck != nil {
		if backend.Spec.HealthCheck.HealthyThreshold != nil && *backend.Spec.HealthCheck.HealthyThreshold < 1 {
			validationErrors = append(validationErrors, "HealthyThreshold must be >= 1")
		}
		if backend.Spec.HealthCheck.UnhealthyThreshold != nil && *backend.Spec.HealthCheck.UnhealthyThreshold < 1 {
			validationErrors = append(validationErrors, "UnhealthyThreshold must be >= 1")
		}
	}

	// Validate circuit breaker configuration if present
	if backend.Spec.CircuitBreaker != nil {
		if backend.Spec.CircuitBreaker.MaxConnections != nil && *backend.Spec.CircuitBreaker.MaxConnections < 1 {
			validationErrors = append(validationErrors, "CircuitBreaker MaxConnections must be >= 1")
		}
		if backend.Spec.CircuitBreaker.MaxPendingRequests != nil && *backend.Spec.CircuitBreaker.MaxPendingRequests < 1 {
			validationErrors = append(validationErrors, "CircuitBreaker MaxPendingRequests must be >= 1")
		}
		if backend.Spec.CircuitBreaker.MaxRequests != nil && *backend.Spec.CircuitBreaker.MaxRequests < 1 {
			validationErrors = append(validationErrors, "CircuitBreaker MaxRequests must be >= 1")
		}
		if backend.Spec.CircuitBreaker.MaxRetries != nil && *backend.Spec.CircuitBreaker.MaxRetries < 0 {
			validationErrors = append(validationErrors, "CircuitBreaker MaxRetries must be >= 0")
		}
	}

	// Validate TLS CA cert secret if specified
	if backend.Spec.TLS != nil && backend.Spec.TLS.Enabled && backend.Spec.TLS.CACertSecretRef != nil {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      *backend.Spec.TLS.CACertSecretRef,
			Namespace: backend.Namespace,
		}, secret); err != nil {
			if errors.IsNotFound(err) {
				validationErrors = append(validationErrors,
					fmt.Sprintf("TLS CA cert secret %s not found", *backend.Spec.TLS.CACertSecretRef))
			}
		} else {
			// Validate secret contains required keys
			if _, ok := secret.Data["ca.crt"]; !ok {
				validationErrors = append(validationErrors,
					fmt.Sprintf("TLS CA cert secret %s missing ca.crt", *backend.Spec.TLS.CACertSecretRef))
			}
		}
	}

	// Update status conditions
	condition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: backend.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if len(validationErrors) > 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ValidationFailed"
		condition.Message = fmt.Sprintf("Validation errors: %v", validationErrors)
		logger.Info("Backend validation failed", "errors", validationErrors)
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "Valid"
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
		return fmt.Errorf("validation failed: %v", validationErrors)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ProxyBackendReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyBackend{}).
		Complete(r)
}
