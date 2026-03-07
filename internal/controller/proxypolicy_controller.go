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
		if apierrors.IsNotFound(err) {
			logger.Info("ProxyPolicy resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ProxyPolicy")
		return ctrl.Result{}, err
	}

	// Skip if already reconciled this generation (ObservedGeneration > 0
	// ensures first-ever reconciliation always proceeds)
	if policy.Status.ObservedGeneration != 0 && policy.Status.ObservedGeneration == policy.Generation {
		return ctrl.Result{}, nil
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

// validateTargetRef validates the policy's target reference exists.
func (r *ProxyPolicyReconciler) validateTargetRef(ctx context.Context, policy *novaedgev1alpha1.ProxyPolicy) []string {
	logger := log.FromContext(ctx)
	var errs []string

	targetNamespace := policy.Namespace
	if policy.Spec.TargetRef.Namespace != nil {
		targetNamespace = *policy.Spec.TargetRef.Namespace
	}

	nn := types.NamespacedName{Name: policy.Spec.TargetRef.Name, Namespace: targetNamespace}
	var obj client.Object

	switch policy.Spec.TargetRef.Kind {
	case "ProxyGateway":
		obj = &novaedgev1alpha1.ProxyGateway{}
	case "ProxyRoute":
		obj = &novaedgev1alpha1.ProxyRoute{}
	case "ProxyBackend":
		obj = &novaedgev1alpha1.ProxyBackend{}
	default:
		return append(errs, fmt.Sprintf("Invalid target kind %s", policy.Spec.TargetRef.Kind))
	}

	if err := r.Get(ctx, nn, obj); err != nil {
		if apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Sprintf("Target %s %s not found", policy.Spec.TargetRef.Kind, policy.Spec.TargetRef.Name))
		} else {
			logger.Error(err, "Failed to get target", "kind", policy.Spec.TargetRef.Kind, "name", policy.Spec.TargetRef.Name)
		}
	}
	return errs
}

// policyValidator is a function that validates a specific policy type's configuration.
type policyValidator func(spec *novaedgev1alpha1.ProxyPolicySpec) string

// policyValidators maps policy types to their validation functions.
// Each validator returns an error message if validation fails, or "" if valid.
var policyValidators = map[novaedgev1alpha1.PolicyType]policyValidator{
	novaedgev1alpha1.PolicyTypeRateLimit: func(spec *novaedgev1alpha1.ProxyPolicySpec) string {
		if spec.RateLimit == nil {
			return "RateLimit configuration is required for RateLimit policy type"
		}
		if spec.RateLimit.RequestsPerSecond <= 0 {
			return "RateLimit RequestsPerSecond must be > 0"
		}
		return ""
	},
	novaedgev1alpha1.PolicyTypeJWT: func(spec *novaedgev1alpha1.ProxyPolicySpec) string {
		if spec.JWT == nil {
			return "JWT configuration is required for JWT policy type"
		}
		if spec.JWT.Issuer == "" && spec.JWT.JWKSUri == "" {
			return "JWT policy must have either issuer or jwksUri set"
		}
		return ""
	},
	novaedgev1alpha1.PolicyTypeCORS: func(spec *novaedgev1alpha1.ProxyPolicySpec) string {
		if spec.CORS == nil {
			return "CORS configuration is required for CORS policy type"
		}
		if len(spec.CORS.AllowOrigins) == 0 {
			return "CORS AllowOrigins must not be empty"
		}
		return ""
	},
	novaedgev1alpha1.PolicyTypeWAF: func(spec *novaedgev1alpha1.ProxyPolicySpec) string {
		if spec.WAF == nil {
			return "WAF configuration is required for WAF policy type"
		}
		return ""
	},
	novaedgev1alpha1.PolicyTypeBasicAuth: func(spec *novaedgev1alpha1.ProxyPolicySpec) string {
		if spec.BasicAuth == nil {
			return "BasicAuth configuration is required for BasicAuth policy type"
		}
		if spec.BasicAuth.SecretRef.Name == "" {
			return "BasicAuth SecretRef name must not be empty"
		}
		return ""
	},
	novaedgev1alpha1.PolicyTypeSecurityHeaders: func(spec *novaedgev1alpha1.ProxyPolicySpec) string {
		if spec.SecurityHeaders == nil {
			return "SecurityHeaders configuration is required for SecurityHeaders policy type"
		}
		return ""
	},
	novaedgev1alpha1.PolicyTypeForwardAuth: func(spec *novaedgev1alpha1.ProxyPolicySpec) string {
		if spec.ForwardAuth == nil {
			return "ForwardAuth configuration is required for ForwardAuth policy type"
		}
		if spec.ForwardAuth.Address == "" {
			return "ForwardAuth Address must not be empty"
		}
		return ""
	},
	novaedgev1alpha1.PolicyTypeOIDC: func(spec *novaedgev1alpha1.ProxyPolicySpec) string {
		if spec.OIDC == nil {
			return "OIDC configuration is required for OIDC policy type"
		}
		if spec.OIDC.ClientID == "" {
			return "OIDC ClientID must not be empty"
		}
		return ""
	},
	novaedgev1alpha1.PolicyTypeWASMPlugin: func(spec *novaedgev1alpha1.ProxyPolicySpec) string {
		if spec.WASMPlugin == nil {
			return "WASMPlugin configuration is required for WASMPlugin policy type"
		}
		if spec.WASMPlugin.Source == "" {
			return "WASMPlugin Source must not be empty"
		}
		return ""
	},
	novaedgev1alpha1.PolicyTypeDistributedRateLimit: func(spec *novaedgev1alpha1.ProxyPolicySpec) string {
		if spec.DistributedRateLimit == nil {
			return "DistributedRateLimit configuration is required for DistributedRateLimit policy type"
		}
		if spec.DistributedRateLimit.RequestsPerSecond <= 0 {
			return "DistributedRateLimit RequestsPerSecond must be > 0"
		}
		return ""
	},
	novaedgev1alpha1.PolicyTypeMeshAuthorization: func(spec *novaedgev1alpha1.ProxyPolicySpec) string {
		if spec.MeshAuthorization == nil {
			return "MeshAuthorization configuration is required for MeshAuthorization policy type"
		}
		if len(spec.MeshAuthorization.Rules) == 0 {
			return "MeshAuthorization Rules must not be empty"
		}
		return ""
	},
}

// validatePolicyConfig validates the policy-specific configuration based on type.
func validatePolicyConfig(policy *novaedgev1alpha1.ProxyPolicy) []string {
	// IP list policies share a separate multi-error validator.
	if policy.Spec.Type == novaedgev1alpha1.PolicyTypeIPAllowList || policy.Spec.Type == novaedgev1alpha1.PolicyTypeIPDenyList {
		return validateIPListPolicy(policy)
	}

	validator, ok := policyValidators[policy.Spec.Type]
	if !ok {
		return []string{fmt.Sprintf("Invalid policy type %s", policy.Spec.Type)}
	}

	if msg := validator(&policy.Spec); msg != "" {
		return []string{msg}
	}
	return nil
}

// validateIPListPolicy validates IPList-type policy configuration.
func validateIPListPolicy(policy *novaedgev1alpha1.ProxyPolicy) []string {
	var errs []string
	switch {
	case policy.Spec.IPList == nil:
		errs = append(errs, "IPList configuration is required for IP allow/deny list policy type")
	case len(policy.Spec.IPList.CIDRs) == 0:
		errs = append(errs, "IPList CIDRs must not be empty")
	default:
		for _, cidr := range policy.Spec.IPList.CIDRs {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				errs = append(errs, fmt.Sprintf("Invalid CIDR %s: %v", cidr, err))
			}
		}
	}
	return errs
}

// validateAndUpdateStatus validates the policy and updates its status
func (r *ProxyPolicyReconciler) validateAndUpdateStatus(ctx context.Context, policy *novaedgev1alpha1.ProxyPolicy) error {
	logger := log.FromContext(ctx)

	validationErrors := make([]string, 0, 2)
	validationErrors = append(validationErrors, r.validateTargetRef(ctx, policy)...)
	validationErrors = append(validationErrors, validatePolicyConfig(policy)...)

	// Update status conditions
	condition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: policy.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if len(validationErrors) > 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = ConditionReasonValidationFailed
		condition.Message = fmt.Sprintf("Validation errors: %v", validationErrors)
		logger.Info("Policy validation failed", "errors", validationErrors)
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = ConditionReasonValid
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
		return fmt.Errorf("%w: %v", errValidationFailed, validationErrors)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ProxyPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyPolicy{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
