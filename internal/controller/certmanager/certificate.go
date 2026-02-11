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

package certmanager

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// CertificateManager handles creation of cert-manager Certificate CRs
// from ProxyGateway resources with cert-manager annotations.
type CertificateManager struct {
	dynamicClient dynamic.Interface
}

// NewCertificateManager creates a new CertificateManager.
func NewCertificateManager(dynamicClient dynamic.Interface) *CertificateManager {
	return &CertificateManager{
		dynamicClient: dynamicClient,
	}
}

// EnsureCertificate creates or updates a cert-manager Certificate CR for a ProxyGateway.
// It reads cert-manager.io/cluster-issuer or cert-manager.io/issuer annotations,
// collects hostnames from listeners, and creates the Certificate resource.
func (m *CertificateManager) EnsureCertificate(ctx context.Context, gateway *novaedgev1alpha1.ProxyGateway) error {
	logger := log.FromContext(ctx)

	issuerName, issuerKind, found := extractIssuerFromAnnotations(gateway)
	if !found {
		logger.V(1).Info("No cert-manager annotations found on gateway", "gateway", gateway.Name)
		return nil
	}

	// Collect DNS names from all listeners with hostnames
	dnsNames := collectDNSNames(gateway)
	if len(dnsNames) == 0 {
		logger.Info("No hostnames found on gateway listeners, skipping certificate creation",
			"gateway", gateway.Name)
		return nil
	}

	// Determine the secret name from the first HTTPS listener's secretRef
	secretName := determineCertSecretName(gateway)

	certName := fmt.Sprintf("%s-tls", gateway.Name)

	cert := buildCertificateUnstructured(
		certName,
		gateway.Namespace,
		dnsNames,
		secretName,
		issuerName,
		issuerKind,
		gateway,
	)

	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	// Try to get existing certificate
	existing, err := m.dynamicClient.Resource(gvr).Namespace(gateway.Namespace).Get(
		ctx, certName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check existing certificate: %w", err)
	}
	if err == nil {
		// Update existing certificate
		cert.SetResourceVersion(existing.GetResourceVersion())
		_, err = m.dynamicClient.Resource(gvr).Namespace(gateway.Namespace).Update(
			ctx, cert, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update Certificate %s: %w", certName, err)
		}
		logger.Info("Updated cert-manager Certificate", "name", certName, "namespace", gateway.Namespace)
		return nil
	}

	// Create new certificate (only on NotFound)
	_, err = m.dynamicClient.Resource(gvr).Namespace(gateway.Namespace).Create(
		ctx, cert, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create Certificate %s: %w", certName, err)
	}

	logger.Info("Created cert-manager Certificate",
		"name", certName,
		"namespace", gateway.Namespace,
		"dnsNames", dnsNames,
		"issuer", issuerName,
		"issuerKind", issuerKind)

	return nil
}

// GetCertificateStatus returns the Ready condition status for a cert-manager Certificate.
func (m *CertificateManager) GetCertificateStatus(ctx context.Context, namespace, name string) (bool, string, error) {
	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	cert, err := m.dynamicClient.Resource(gvr).Namespace(namespace).Get(
		ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, "", fmt.Errorf("failed to get Certificate %s/%s: %w", namespace, name, err)
	}

	return extractReadyCondition(cert)
}

// extractIssuerFromAnnotations reads cert-manager issuer annotations from a ProxyGateway.
func extractIssuerFromAnnotations(gateway *novaedgev1alpha1.ProxyGateway) (name, kind string, found bool) {
	annotations := gateway.GetAnnotations()
	if annotations == nil {
		return "", "", false
	}

	if issuer, ok := annotations[novaedgev1alpha1.AnnotationCertManagerClusterIssuer]; ok && issuer != "" {
		return issuer, "ClusterIssuer", true
	}
	if issuer, ok := annotations[novaedgev1alpha1.AnnotationCertManagerIssuer]; ok && issuer != "" {
		return issuer, "Issuer", true
	}
	return "", "", false
}

// collectDNSNames extracts unique hostnames from all gateway listeners.
func collectDNSNames(gateway *novaedgev1alpha1.ProxyGateway) []string {
	seen := make(map[string]struct{})
	var dnsNames []string

	for _, listener := range gateway.Spec.Listeners {
		for _, hostname := range listener.Hostnames {
			if _, ok := seen[hostname]; !ok {
				seen[hostname] = struct{}{}
				dnsNames = append(dnsNames, hostname)
			}
		}
	}

	return dnsNames
}

// determineCertSecretName derives the TLS secret name from the gateway.
func determineCertSecretName(gateway *novaedgev1alpha1.ProxyGateway) string {
	for _, listener := range gateway.Spec.Listeners {
		if listener.TLS != nil && listener.TLS.SecretRef != nil && listener.TLS.SecretRef.Name != "" {
			return listener.TLS.SecretRef.Name
		}
	}
	return fmt.Sprintf("%s-tls", gateway.Name)
}

// buildCertificateUnstructured builds an unstructured cert-manager Certificate CR.
func buildCertificateUnstructured(
	name, namespace string,
	dnsNames []string,
	secretName string,
	issuerName, issuerKind string,
	owner *novaedgev1alpha1.ProxyGateway,
) *unstructured.Unstructured {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	})
	cert.SetName(name)
	cert.SetNamespace(namespace)

	// Set labels
	cert.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "novaedge",
		"novaedge.io/gateway":          owner.Name,
	})

	// Set owner reference for cleanup
	ownerRef := metav1.OwnerReference{
		APIVersion: "novaedge.io/v1alpha1",
		Kind:       "ProxyGateway",
		Name:       owner.Name,
		UID:        owner.UID,
	}
	cert.SetOwnerReferences([]metav1.OwnerReference{ownerRef})

	// Build spec
	spec := map[string]interface{}{
		"dnsNames":   toInterfaceSlice(dnsNames),
		"secretName": secretName,
		"issuerRef": map[string]interface{}{
			"name":  issuerName,
			"kind":  issuerKind,
			"group": "cert-manager.io",
		},
	}

	if err := unstructured.SetNestedMap(cert.Object, spec, "spec"); err != nil {
		// This should not fail for well-formed maps
		return cert
	}

	return cert
}

// toInterfaceSlice converts a string slice to interface slice for unstructured objects.
func toInterfaceSlice(ss []string) []interface{} {
	result := make([]interface{}, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}

// extractReadyCondition extracts the Ready condition from a cert-manager Certificate.
func extractReadyCondition(cert *unstructured.Unstructured) (bool, string, error) {
	conditions, found, err := unstructured.NestedSlice(cert.Object, "status", "conditions")
	if err != nil {
		return false, "", fmt.Errorf("failed to read conditions: %w", err)
	}
	if !found {
		return false, "No conditions", nil
	}

	for _, c := range conditions {
		condMap, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _, _ := unstructured.NestedString(condMap, "type")
		if condType != "Ready" {
			continue
		}
		status, _, _ := unstructured.NestedString(condMap, "status")
		message, _, _ := unstructured.NestedString(condMap, "message")
		return status == "True", message, nil
	}

	return false, "Ready condition not found", nil
}

// DeleteCertificate deletes a cert-manager Certificate CR.
func (m *CertificateManager) DeleteCertificate(ctx context.Context, namespace, name string) error {
	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	err := m.dynamicClient.Resource(gvr).Namespace(namespace).Delete(
		ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete Certificate %s/%s: %w", namespace, name, err)
	}

	log.FromContext(ctx).Info("Deleted cert-manager Certificate",
		"name", name, "namespace", namespace)
	return nil
}

// ListCertificatesForGateway lists cert-manager Certificates owned by a gateway.
func (m *CertificateManager) ListCertificatesForGateway(ctx context.Context, gateway types.NamespacedName) ([]unstructured.Unstructured, error) {
	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	list, err := m.dynamicClient.Resource(gvr).Namespace(gateway.Namespace).List(
		ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("novaedge.io/gateway=%s", gateway.Name),
		})
	if err != nil {
		return nil, fmt.Errorf("failed to list certificates for gateway %s: %w", gateway, err)
	}

	return list.Items, nil
}
