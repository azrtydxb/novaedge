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

// Package certmanager provides optional integration with cert-manager for
// automatic TLS certificate management in NovaEdge.
package certmanager

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// EnableMode defines the cert-manager enablement mode.
type EnableMode string

const (
	// EnableModeAuto auto-detects cert-manager CRDs.
	EnableModeAuto EnableMode = "auto"
	// EnableModeTrue requires cert-manager to be present.
	EnableModeTrue EnableMode = "true"
	// EnableModeFalse disables cert-manager integration entirely.
	EnableModeFalse EnableMode = "false"
)

// CertificateGVR is the GroupVersionResource for cert-manager Certificate.
var CertificateGVR = schema.GroupVersionResource{
	Group:    "cert-manager.io",
	Version:  "v1",
	Resource: "certificates",
}

// IssuerGVR is the GroupVersionResource for cert-manager Issuer.
var IssuerGVR = schema.GroupVersionResource{
	Group:    "cert-manager.io",
	Version:  "v1",
	Resource: "issuers",
}

// ClusterIssuerGVR is the GroupVersionResource for cert-manager ClusterIssuer.
var ClusterIssuerGVR = schema.GroupVersionResource{
	Group:    "cert-manager.io",
	Version:  "v1",
	Resource: "clusterissuers",
}

// Detector checks whether cert-manager CRDs are installed in the cluster.
type Detector struct {
	discoveryClient discovery.DiscoveryInterface
}

// NewDetector creates a new cert-manager CRD detector from a rest.Config.
func NewDetector(config *rest.Config) (*Detector, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}
	return &Detector{discoveryClient: dc}, nil
}

// NewDetectorFromClient creates a Detector from an existing discovery client.
func NewDetectorFromClient(dc discovery.DiscoveryInterface) *Detector {
	return &Detector{discoveryClient: dc}
}

// IsCertManagerInstalled checks if cert-manager CRDs are present in the cluster.
func (d *Detector) IsCertManagerInstalled(ctx context.Context) (bool, error) {
	logger := log.FromContext(ctx)

	_, resources, err := d.discoveryClient.ServerGroupsAndResources()
	if err != nil {
		// Discovery can return partial results with an error
		if !discovery.IsGroupDiscoveryFailedError(err) {
			return false, fmt.Errorf("failed to discover API resources: %w", err)
		}
		logger.V(1).Info("Partial discovery error (some groups unavailable)", "error", err)
	}

	for _, resourceList := range resources {
		if resourceList == nil {
			continue
		}
		gv, parseErr := schema.ParseGroupVersion(resourceList.GroupVersion)
		if parseErr != nil {
			continue
		}
		if gv.Group != "cert-manager.io" {
			continue
		}
		for _, r := range resourceList.APIResources {
			if r.Name == "certificates" {
				logger.Info("cert-manager CRDs detected",
					"groupVersion", resourceList.GroupVersion)
				return true, nil
			}
		}
	}

	logger.Info("cert-manager CRDs not found in cluster")
	return false, nil
}

// ShouldEnable determines whether cert-manager integration should be enabled
// based on the mode flag and actual CRD presence.
func (d *Detector) ShouldEnable(ctx context.Context, mode EnableMode) (bool, error) {
	switch mode {
	case EnableModeFalse:
		return false, nil
	case EnableModeTrue:
		installed, err := d.IsCertManagerInstalled(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to detect cert-manager: %w", err)
		}
		if !installed {
			return false, fmt.Errorf("cert-manager is required (--enable-cert-manager=true) but CRDs not found")
		}
		return true, nil
	case EnableModeAuto:
		installed, err := d.IsCertManagerInstalled(ctx)
		if err != nil {
			log.FromContext(ctx).Info("Failed to detect cert-manager, disabling", "error", err)
			return false, nil
		}
		return installed, nil
	default:
		return false, fmt.Errorf("invalid cert-manager enable mode: %s", mode)
	}
}
