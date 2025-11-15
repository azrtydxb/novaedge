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
	"net"

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

// ProxyPolicyReconciler reconciles a ProxyPolicy object
type ProxyPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxypolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxypolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxypolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=novaedge.io,resources=proxygateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxyroutes,verbs=get;list;watch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxybackends,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *ProxyPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ProxyPolicy instance
	policy := &novaedgev1alpha1.ProxyPolicy{}
	err := r.Get(ctx, req.NamespacedName, policy)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ProxyPolicy resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ProxyPolicy")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling ProxyPolicy", "name", policy.Name, "type", policy.Spec.Type, "target", policy.Spec.TargetRef.Name)

	// Validate and update status
	if err := r.validateAndUpdateStatus(ctx, policy); err != nil {
		logger.Error(err, "Failed to validate policy")
		return ctrl.Result{Requeue: true}, err
	}

	// Trigger config update for all nodes
	TriggerConfigUpdate()

	return ctrl.Result{}, nil
}

// validateAndUpdateStatus validates the policy and updates its status
func (r *ProxyPolicyReconciler) validateAndUpdateStatus(ctx context.Context, policy *novaedgev1alpha1.ProxyPolicy) error {
	logger := log.FromContext(ctx)
	var validationErrors []string

	// Validate targetRef exists
	targetNamespace := policy.Namespace
	if policy.Spec.TargetRef.Namespace != nil {
		targetNamespace = *policy.Spec.TargetRef.Namespace
	}

	switch policy.Spec.TargetRef.Kind {
	case "ProxyGateway":
		gateway := &novaedgev1alpha1.ProxyGateway{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      policy.Spec.TargetRef.Name,
			Namespace: targetNamespace,
		}, gateway); err != nil {
			if errors.IsNotFound(err) {
				validationErrors = append(validationErrors,
					fmt.Sprintf("Target ProxyGateway %s not found", policy.Spec.TargetRef.Name))
			} else {
				logger.Error(err, "Failed to get target gateway", "gateway", policy.Spec.TargetRef.Name)
			}
		}
	case "ProxyRoute":
		route := &novaedgev1alpha1.ProxyRoute{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      policy.Spec.TargetRef.Name,
			Namespace: targetNamespace,
		}, route); err != nil {
			if errors.IsNotFound(err) {
				validationErrors = append(validationErrors,
					fmt.Sprintf("Target ProxyRoute %s not found", policy.Spec.TargetRef.Name))
			} else {
				logger.Error(err, "Failed to get target route", "route", policy.Spec.TargetRef.Name)
			}
		}
	case "ProxyBackend":
		backend := &novaedgev1alpha1.ProxyBackend{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      policy.Spec.TargetRef.Name,
			Namespace: targetNamespace,
		}, backend); err != nil {
			if errors.IsNotFound(err) {
				validationErrors = append(validationErrors,
					fmt.Sprintf("Target ProxyBackend %s not found", policy.Spec.TargetRef.Name))
			} else {
				logger.Error(err, "Failed to get target backend", "backend", policy.Spec.TargetRef.Name)
			}
		}
	default:
		validationErrors = append(validationErrors,
			fmt.Sprintf("Invalid target kind %s", policy.Spec.TargetRef.Kind))
	}

	// Validate policy configuration based on type
	switch policy.Spec.Type {
	case novaedgev1alpha1.PolicyTypeRateLimit:
		if policy.Spec.RateLimit == nil {
			validationErrors = append(validationErrors, "RateLimit configuration is required for RateLimit policy type")
		} else if policy.Spec.RateLimit.RequestsPerSecond <= 0 {
			validationErrors = append(validationErrors, "RateLimit RequestsPerSecond must be > 0")
		}
	case novaedgev1alpha1.PolicyTypeJWT:
		if policy.Spec.JWT == nil {
			validationErrors = append(validationErrors, "JWT configuration is required for JWT policy type")
		} else {
			if policy.Spec.JWT.Issuer == "" && policy.Spec.JWT.JWKSUri == "" {
				validationErrors = append(validationErrors, "JWT policy must have either issuer or jwksUri set")
			}
		}
	case novaedgev1alpha1.PolicyTypeIPAllowList, novaedgev1alpha1.PolicyTypeIPDenyList:
		if policy.Spec.IPList == nil {
			validationErrors = append(validationErrors, "IPList configuration is required for IP allow/deny list policy type")
		} else if len(policy.Spec.IPList.CIDRs) == 0 {
			validationErrors = append(validationErrors, "IPList CIDRs must not be empty")
		} else {
			// Validate CIDRs are valid
			for _, cidr := range policy.Spec.IPList.CIDRs {
				if _, _, err := net.ParseCIDR(cidr); err != nil {
					validationErrors = append(validationErrors,
						fmt.Sprintf("Invalid CIDR %s: %v", cidr, err))
				}
			}
		}
	case novaedgev1alpha1.PolicyTypeCORS:
		if policy.Spec.CORS == nil {
			validationErrors = append(validationErrors, "CORS configuration is required for CORS policy type")
		} else if len(policy.Spec.CORS.AllowOrigins) == 0 {
			validationErrors = append(validationErrors, "CORS AllowOrigins must not be empty")
		}
	default:
		validationErrors = append(validationErrors,
			fmt.Sprintf("Invalid policy type %s", policy.Spec.Type))
	}

	// Update status conditions
	condition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: policy.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if len(validationErrors) > 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ValidationFailed"
		condition.Message = fmt.Sprintf("Validation errors: %v", validationErrors)
		logger.Info("Policy validation failed", "errors", validationErrors)
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "Valid"
		condition.Message = "Policy configuration is valid"
	}

	// Update status
	meta.SetStatusCondition(&policy.Status.Conditions, condition)
	policy.Status.ObservedGeneration = policy.Generation

	if err := r.Status().Update(ctx, policy); err != nil {
		logger.Error(err, "Failed to update policy status")
		return err
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("validation failed: %v", validationErrors)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ProxyPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyPolicy{}).
		Complete(r)
}
