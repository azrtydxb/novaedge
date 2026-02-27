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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	"github.com/piwi3910/novaedge/internal/controller/certmanager"
	"github.com/piwi3910/novaedge/internal/controller/vault"
)

var (
	errVaultCertificateErrors = errors.New("vault certificate errors")
)

// ProxyGatewayReconciler reconciles a ProxyGateway object
type ProxyGatewayReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	ControllerClass string

	// CertManager handles cert-manager Certificate CR creation (optional).
	// When non-nil, gateways with cert-manager.io annotations will have
	// Certificate resources created automatically.
	CertManager *certmanager.CertificateManager

	// VaultPKI handles Vault PKI certificate issuance (optional).
	// When non-nil, listeners with VaultCertRef will have certificates
	// issued from Vault and cached in Kubernetes Secrets.
	VaultPKI *vault.PKIManager
}

// +kubebuilder:rbac:groups=novaedge.io,resources=proxygateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=proxygateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=proxygateways/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *ProxyGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ProxyGateway instance
	gateway := &novaedgev1alpha1.ProxyGateway{}
	err := r.Get(ctx, req.NamespacedName, gateway)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ProxyGateway resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ProxyGateway")
		return ctrl.Result{}, err
	}

	// Skip if already reconciled this generation (ObservedGeneration > 0
	// ensures first-ever reconciliation always proceeds)
	if gateway.Status.ObservedGeneration != 0 && gateway.Status.ObservedGeneration == gateway.Generation {
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling ProxyGateway", "name", gateway.Name, "vipRef", gateway.Spec.VIPRef)

	// Check loadBalancerClass: only reconcile gateways matching our class
	if !r.shouldReconcileGateway(gateway) {
		logger.Info("Skipping gateway with non-matching loadBalancerClass",
			"class", gateway.Spec.LoadBalancerClass,
			"controllerClass", r.ControllerClass)
		return ctrl.Result{}, nil
	}

	// Ensure cert-manager Certificate CRs for gateways with cert-manager annotations
	if r.CertManager != nil {
		if err := r.CertManager.EnsureCertificate(ctx, gateway); err != nil {
			logger.Error(err, "Failed to ensure cert-manager certificate", "gateway", gateway.Name)
			// Continue reconciliation; certificate creation is best-effort
		}
	}

	// Issue Vault PKI certificates for listeners with VaultCertRef
	if r.VaultPKI != nil {
		if err := r.ensureVaultCertificates(ctx, gateway); err != nil {
			logger.Error(err, "Failed to ensure Vault PKI certificates", "gateway", gateway.Name)
			// Continue reconciliation; Vault certificate issuance is best-effort
		}
	}

	// Validate and update status
	if err := r.validateAndUpdateStatus(ctx, gateway); err != nil {
		logger.Error(err, "Failed to validate gateway")
		return ctrl.Result{RequeueAfter: time.Second}, err
	}

	// Trigger config update for all nodes
	TriggerConfigUpdate()

	return ctrl.Result{}, nil
}

// ensureVaultCertificates issues certificates from Vault PKI for listeners that reference VaultCertRef,
// and stores the resulting cert/key in a Kubernetes Secret.
func (r *ProxyGatewayReconciler) ensureVaultCertificates(ctx context.Context, gateway *novaedgev1alpha1.ProxyGateway) error {
	logger := log.FromContext(ctx)
	var errs []string

	for _, listener := range gateway.Spec.Listeners {
		if listener.TLS == nil || listener.TLS.VaultCertRef == nil {
			continue
		}

		vaultRef := listener.TLS.VaultCertRef
		var commonName string
		if len(listener.Hostnames) > 0 {
			commonName = listener.Hostnames[0]
		} else {
			commonName = fmt.Sprintf("%s.%s.svc", gateway.Name, gateway.Namespace)
		}

		pkiReq := &vault.PKIRequest{
			MountPath:  vaultRef.Path,
			Role:       vaultRef.Role,
			CommonName: commonName,
			TTL:        vaultRef.TTL,
		}

		// Add additional hostnames as SANs
		if len(listener.Hostnames) > 1 {
			pkiReq.AltNames = listener.Hostnames[1:]
		}

		cert, err := r.VaultPKI.IssueCertificate(ctx, pkiReq)
		if err != nil {
			errs = append(errs, fmt.Sprintf("listener %s: %v", listener.Name, err))
			continue
		}

		// Store the certificate in a Kubernetes Secret
		secretName := vaultRef.CacheSecretName
		if secretName == "" {
			secretName = fmt.Sprintf("%s-%s-vault-tls", gateway.Name, listener.Name)
		}

		certPEM, keyPEM := cert.CertToPEM()
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: gateway.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "novaedge",
					"novaedge.io/gateway":          gateway.Name,
				},
			},
			Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{
				"tls.crt": certPEM,
				"tls.key": keyPEM,
			},
		}

		existing := &corev1.Secret{}
		err = r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: gateway.Namespace}, existing)
		switch {
		case apierrors.IsNotFound(err):
			if createErr := r.Create(ctx, secret); createErr != nil {
				errs = append(errs, fmt.Sprintf("listener %s: failed to create secret: %v", listener.Name, createErr))
				continue
			}
			logger.Info("Created Vault PKI certificate secret",
				"secret", secretName, "listener", listener.Name, "commonName", commonName)
		case err != nil:
			errs = append(errs, fmt.Sprintf("listener %s: failed to get secret: %v", listener.Name, err))
			continue
		default:
			existing.Data = secret.Data
			if updateErr := r.Update(ctx, existing); updateErr != nil {
				errs = append(errs, fmt.Sprintf("listener %s: failed to update secret: %v", listener.Name, updateErr))
				continue
			}
			logger.Info("Updated Vault PKI certificate secret",
				"secret", secretName, "listener", listener.Name, "commonName", commonName)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %s", errVaultCertificateErrors, strings.Join(errs, "; "))
	}
	return nil
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
			if apierrors.IsNotFound(err) {
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
				if apierrors.IsNotFound(err) {
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
		condition.Reason = ConditionReasonValidationFailed
		condition.Message = fmt.Sprintf("Validation errors: %v", validationErrors)
		logger.Info("Gateway validation failed", "errors", validationErrors)
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = ConditionReasonValid
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
		return fmt.Errorf("%w: %v", errValidationFailed, validationErrors)
	}

	return nil
}

// DefaultControllerClass is the default loadBalancerClass handled by this controller.
const DefaultControllerClass = "novaedge.io/proxy"

// shouldReconcileGateway checks if this gateway matches the controller's loadBalancerClass.
func (r *ProxyGatewayReconciler) shouldReconcileGateway(gateway *novaedgev1alpha1.ProxyGateway) bool {
	controllerClass := r.ControllerClass
	if controllerClass == "" {
		controllerClass = DefaultControllerClass
	}

	gwClass := gateway.Spec.LoadBalancerClass
	// If gateway has no class set, reconcile if controller uses the default class
	if gwClass == "" {
		return controllerClass == DefaultControllerClass
	}

	return gwClass == controllerClass
}

// SetupWithManager sets up the controller with the Manager.
// Configures exponential backoff rate limiting (5ms base, 1000s max) to
// prevent API server overload during error storms.
func (r *ProxyGatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.ProxyGateway{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WithOptions(controller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Millisecond, 1000*time.Second),
		}).
		Complete(r)
}
