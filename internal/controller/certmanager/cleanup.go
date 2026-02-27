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
	"errors"
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/log"
)
var (
	errErrorsCleaningUpCertificates = errors.New("errors cleaning up certificates")
)


// Cleanup handles removing cert-manager Certificate CRs when their parent
// ProxyGateway is deleted. While ownerReferences handle automatic garbage
// collection, this provides explicit cleanup for cross-namespace scenarios
// and audit logging.
type Cleanup struct {
	dynamicClient dynamic.Interface
}

// NewCleanup creates a new Cleanup handler.
func NewCleanup(dynamicClient dynamic.Interface) *Cleanup {
	return &Cleanup{
		dynamicClient: dynamicClient,
	}
}

// CleanupForGateway removes all cert-manager Certificates associated with a gateway.
// This is called when a ProxyGateway is deleted or when cert-manager annotations are removed.
func (c *Cleanup) CleanupForGateway(ctx context.Context, gateway types.NamespacedName) error {
	logger := log.FromContext(ctx)

	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	// List certificates with the gateway label
	list, err := c.dynamicClient.Resource(gvr).Namespace(gateway.Namespace).List(
		ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("novaedge.io/gateway=%s", gateway.Name),
		})
	if err != nil {
		return fmt.Errorf("failed to list certificates for gateway %s: %w", gateway, err)
	}

	if len(list.Items) == 0 {
		logger.V(1).Info("No cert-manager Certificates found for gateway", "gateway", gateway)
		return nil
	}

	var errs []error
	for _, cert := range list.Items {
		logger.Info("Cleaning up cert-manager Certificate",
			"certificate", cert.GetName(),
			"namespace", cert.GetNamespace(),
			"gateway", gateway.Name)

		err := c.dynamicClient.Resource(gvr).Namespace(cert.GetNamespace()).Delete(
			ctx, cert.GetName(), metav1.DeleteOptions{})
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to delete certificate %s: %w", cert.GetName(), err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %v", errErrorsCleaningUpCertificates, errs)
	}

	logger.Info("Cleaned up cert-manager Certificates for gateway",
		"gateway", gateway,
		"count", len(list.Items))
	return nil
}

// CleanupOrphaned finds and removes cert-manager Certificates that reference
// ProxyGateways that no longer exist.
func (c *Cleanup) CleanupOrphaned(ctx context.Context, namespace string) (int, error) {
	logger := log.FromContext(ctx)

	gvr := schema.GroupVersionResource{
		Group:    "cert-manager.io",
		Version:  "v1",
		Resource: "certificates",
	}

	list, err := c.dynamicClient.Resource(gvr).Namespace(namespace).List(
		ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/managed-by=novaedge",
		})
	if err != nil {
		return 0, fmt.Errorf("failed to list NovaEdge certificates: %w", err)
	}

	cleaned := 0
	for _, cert := range list.Items {
		ownerRefs := cert.GetOwnerReferences()
		if len(ownerRefs) == 0 {
			// No owner references -- might be orphaned
			logger.Info("Found orphaned cert-manager Certificate (no owner)",
				"name", cert.GetName(),
				"namespace", cert.GetNamespace())

			err := c.dynamicClient.Resource(gvr).Namespace(cert.GetNamespace()).Delete(
				ctx, cert.GetName(), metav1.DeleteOptions{})
			if err != nil {
				logger.Error(err, "Failed to delete orphaned certificate", "name", cert.GetName())
				continue
			}
			cleaned++
		}
	}

	if cleaned > 0 {
		logger.Info("Cleaned up orphaned cert-manager Certificates",
			"namespace", namespace,
			"count", cleaned)
	}

	return cleaned, nil
}
