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

// ProxyGatewayReconciler reconciles a ProxyGateway object
type ProxyGatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxygateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxygateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxygateways/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *ProxyGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ProxyGateway instance
	gateway := &novaedgev1alpha1.ProxyGateway{}
	err := r.Get(ctx, req.NamespacedName, gateway)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ProxyGateway resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ProxyGateway")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling ProxyGateway", "name", gateway.Name, "vipRef", gateway.Spec.VIPRef)

	// Validate and update status
	if err := r.validateAndUpdateStatus(ctx, gateway); err != nil {
		logger.Error(err, "Failed to validate gateway")
		return ctrl.Result{Requeue: true}, err
	}

	// Trigger config update for all nodes
	TriggerConfigUpdate()

	return ctrl.Result{}, nil
}

// validateAndUpdateStatus validates the gateway and updates its status
func (r *ProxyGatewayReconciler) validateAndUpdateStatus(ctx context.Context, gateway *novaedgev1alpha1.ProxyGateway) error {
	logger := log.FromContext(ctx)
	var validationErrors []string

	// Validate VIPRef exists
	if gateway.Spec.VIPRef != "" {
		vip := &novaedgev1alpha1.ProxyVIP{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      gateway.Spec.VIPRef,
			Namespace: gateway.Namespace,
		}, vip); err != nil {
			if errors.IsNotFound(err) {
				validationErrors = append(validationErrors, fmt.Sprintf("VIP %s not found", gateway.Spec.VIPRef))
			} else {
				logger.Error(err, "Failed to get VIP", "vip", gateway.Spec.VIPRef)
			}
		}
	}

	// Validate TLS secrets for HTTPS listeners
	for _, listener := range gateway.Spec.Listeners {
		if listener.Protocol == "HTTPS" || listener.Protocol == "TLS" {
			if listener.TLS == nil || listener.TLS.SecretRef.Name == "" {
				validationErrors = append(validationErrors,
					fmt.Sprintf("Listener %s requires TLS secret but none specified", listener.Name))
				continue
			}

			// Check if secret exists
			secret := &corev1.Secret{}
			secretNamespace := listener.TLS.SecretRef.Namespace
			if secretNamespace == "" {
				secretNamespace = gateway.Namespace
			}

			if err := r.Get(ctx, types.NamespacedName{
				Name:      listener.TLS.SecretRef.Name,
				Namespace: secretNamespace,
			}, secret); err != nil {
				if errors.IsNotFound(err) {
					validationErrors = append(validationErrors,
						fmt.Sprintf("TLS secret %s not found for listener %s",
							listener.TLS.SecretRef.Name, listener.Name))
				}
			} else {
				// Validate secret contains required keys
				if _, ok := secret.Data["tls.crt"]; !ok {
					validationErrors = append(validationErrors,
						fmt.Sprintf("TLS secret %s missing tls.crt", listener.TLS.SecretRef.Name))
				}
				if _, ok := secret.Data["tls.key"]; !ok {
					validationErrors = append(validationErrors,
						fmt.Sprintf("TLS secret %s missing tls.key", listener.TLS.SecretRef.Name))
				}
			}
		}
	}

	// Update status conditions
	condition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: gateway.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if len(validationErrors) > 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ValidationFailed"
		condition.Message = fmt.Sprintf("Validation errors: %v", validationErrors)
		logger.Info("Gateway validation failed", "errors", validationErrors)
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "Valid"
		condition.Message = "Gateway configuration is valid"
	}

	// Update status
	meta.SetStatusCondition(&gateway.Status.Conditions, condition)
	gateway.Status.ObservedGeneration = gateway.Generation

	if err := r.Status().Update(ctx, gateway); err != nil {
		logger.Error(err, "Failed to update gateway status")
		return err
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("validation failed: %v", validationErrors)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ProxyGatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyGateway{}).
		Complete(r)
}
