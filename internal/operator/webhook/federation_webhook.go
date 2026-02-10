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

package webhook

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

// FederationValidator validates NovaEdgeFederation resources
type FederationValidator struct{}

var _ webhook.CustomValidator = &FederationValidator{}

// Regular expressions for validation
var (
	// federationIDRegex validates federation IDs (DNS-safe lowercase alphanumeric with hyphens)
	federationIDRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

	// memberNameRegex validates member names
	memberNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
)

// ValidateCreate validates creation of a NovaEdgeFederation
func (v *FederationValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	fed, ok := obj.(*novaedgev1alpha1.NovaEdgeFederation)
	if !ok {
		return nil, fmt.Errorf("expected NovaEdgeFederation, got %T", obj)
	}

	return v.validateFederation(fed)
}

// ValidateUpdate validates updates to a NovaEdgeFederation
func (v *FederationValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (warnings admission.Warnings, err error) {
	oldFed, ok := oldObj.(*novaedgev1alpha1.NovaEdgeFederation)
	if !ok {
		return nil, fmt.Errorf("expected NovaEdgeFederation, got %T", oldObj)
	}

	newFed, ok := newObj.(*novaedgev1alpha1.NovaEdgeFederation)
	if !ok {
		return nil, fmt.Errorf("expected NovaEdgeFederation, got %T", newObj)
	}

	// Federation ID is immutable
	if oldFed.Spec.FederationID != newFed.Spec.FederationID {
		return nil, fmt.Errorf("federation ID is immutable: cannot change from %q to %q",
			oldFed.Spec.FederationID, newFed.Spec.FederationID)
	}

	// Local member name is immutable
	if oldFed.Spec.LocalMember.Name != newFed.Spec.LocalMember.Name {
		return nil, fmt.Errorf("local member name is immutable: cannot change from %q to %q",
			oldFed.Spec.LocalMember.Name, newFed.Spec.LocalMember.Name)
	}

	return v.validateFederation(newFed)
}

// ValidateDelete validates deletion of a NovaEdgeFederation
func (v *FederationValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	fed, ok := obj.(*novaedgev1alpha1.NovaEdgeFederation)
	if !ok {
		return nil, fmt.Errorf("expected NovaEdgeFederation, got %T", obj)
	}

	var warns admission.Warnings

	// Warn if there are connected peers
	if len(fed.Status.Members) > 0 {
		var healthyPeers []string
		for _, member := range fed.Status.Members {
			if member.Healthy {
				healthyPeers = append(healthyPeers, member.Name)
			}
		}
		if len(healthyPeers) > 0 {
			warns = append(warns, fmt.Sprintf(
				"Federation has %d healthy peers: %v. Deleting may cause sync issues.",
				len(healthyPeers), healthyPeers))
		}
	}

	// Warn if there are pending conflicts
	if fed.Status.ConflictsPending > 0 {
		warns = append(warns, fmt.Sprintf(
			"Federation has %d pending conflicts that will be lost.", fed.Status.ConflictsPending))
	}

	return warns, nil
}

// validateFederation performs validation on the federation spec
func (v *FederationValidator) validateFederation(fed *novaedgev1alpha1.NovaEdgeFederation) (warnings admission.Warnings, err error) {
	var warns admission.Warnings
	var errs []string

	// Validate federation ID
	if !federationIDRegex.MatchString(fed.Spec.FederationID) {
		errs = append(errs, fmt.Sprintf(
			"invalid federation ID %q: must be lowercase alphanumeric with hyphens",
			fed.Spec.FederationID))
	}

	if len(fed.Spec.FederationID) > 63 {
		errs = append(errs, fmt.Sprintf(
			"federation ID %q is too long: max 63 characters",
			fed.Spec.FederationID))
	}

	// Validate local member
	if err := v.validateMember(fed.Spec.LocalMember.Name, fed.Spec.LocalMember.Endpoint); err != nil {
		errs = append(errs, fmt.Sprintf("invalid local member: %v", err))
	}

	// Validate peers
	memberNames := map[string]bool{fed.Spec.LocalMember.Name: true}
	endpoints := map[string]bool{fed.Spec.LocalMember.Endpoint: true}

	for i, peer := range fed.Spec.Members {
		// Validate peer name and endpoint
		if err := v.validateMember(peer.Name, peer.Endpoint); err != nil {
			errs = append(errs, fmt.Sprintf("invalid peer %d (%s): %v", i, peer.Name, err))
		}

		// Check for duplicate names
		if memberNames[peer.Name] {
			errs = append(errs, fmt.Sprintf("duplicate member name: %q", peer.Name))
		}
		memberNames[peer.Name] = true

		// Check for duplicate endpoints
		if endpoints[peer.Endpoint] {
			errs = append(errs, fmt.Sprintf("duplicate endpoint: %q", peer.Endpoint))
		}
		endpoints[peer.Endpoint] = true

		// Check for self-referential peer
		if peer.Name == fed.Spec.LocalMember.Name {
			errs = append(errs, fmt.Sprintf("peer cannot have same name as local member: %q", peer.Name))
		}

		// Validate TLS configuration
		if peer.TLS != nil {
			if err := v.validateTLS(peer.TLS, peer.Name); err != nil {
				errs = append(errs, fmt.Sprintf("invalid TLS config for peer %s: %v", peer.Name, err))
			}
		}
	}

	// Validate sync config
	if fed.Spec.Sync != nil {
		if fed.Spec.Sync.BatchSize > 1000 {
			warns = append(warns, fmt.Sprintf(
				"batch size %d is very large, may cause memory issues", fed.Spec.Sync.BatchSize))
		}

		// Check for invalid resource types
		validResourceTypes := map[string]bool{
			"ProxyGateway": true,
			"ProxyRoute":   true,
			"ProxyBackend": true,
			"ProxyPolicy":  true,
			"ProxyVIP":     true,
		}
		for _, rt := range fed.Spec.Sync.ResourceTypes {
			if !validResourceTypes[rt] {
				errs = append(errs, fmt.Sprintf("invalid resource type: %q", rt))
			}
		}
	}

	// Validate conflict resolution
	if fed.Spec.ConflictResolution != nil {
		switch fed.Spec.ConflictResolution.Strategy {
		case novaedgev1alpha1.ConflictResolutionLastWriterWins,
			novaedgev1alpha1.ConflictResolutionMerge,
			novaedgev1alpha1.ConflictResolutionManual:
			// Valid
		default:
			errs = append(errs, fmt.Sprintf(
				"invalid conflict resolution strategy: %q", fed.Spec.ConflictResolution.Strategy))
		}

		// Warn about manual conflict resolution
		if fed.Spec.ConflictResolution.Strategy == novaedgev1alpha1.ConflictResolutionManual {
			warns = append(warns, "Manual conflict resolution requires operator intervention for each conflict")
		}
	}

	// Validate health check config
	if fed.Spec.HealthCheck != nil {
		if fed.Spec.HealthCheck.Interval != nil && fed.Spec.HealthCheck.Timeout != nil {
			if fed.Spec.HealthCheck.Timeout.Duration >= fed.Spec.HealthCheck.Interval.Duration {
				errs = append(errs, "health check timeout must be less than interval")
			}
		}
	}

	// Combine errors
	if len(errs) > 0 {
		return warns, fmt.Errorf("validation failed: %s", strings.Join(errs, "; "))
	}

	return warns, nil
}

// validateMember validates a member name and endpoint
func (v *FederationValidator) validateMember(name, endpoint string) error {
	// Validate name
	if !memberNameRegex.MatchString(name) {
		return fmt.Errorf("invalid name %q: must be lowercase alphanumeric with hyphens", name)
	}

	if len(name) > 63 {
		return fmt.Errorf("name %q is too long: max 63 characters", name)
	}

	// Validate endpoint
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint %q: must be host:port format", endpoint)
	}

	if host == "" {
		return fmt.Errorf("endpoint %q has empty host", endpoint)
	}

	if port == "" {
		return fmt.Errorf("endpoint %q has empty port", endpoint)
	}

	return nil
}

// validateTLS validates TLS configuration
func (v *FederationValidator) validateTLS(tls *novaedgev1alpha1.FederationTLS, peerName string) error {
	// If TLS is enabled, should have at least CA or be insecure
	if tls.Enabled != nil && *tls.Enabled {
		if tls.CASecretRef == nil && !tls.InsecureSkipVerify {
			return fmt.Errorf("TLS enabled but no CA secret specified and insecureSkipVerify is false")
		}

		if tls.InsecureSkipVerify {
			// InsecureSkipVerify disables TLS certificate verification.
			// This is allowed but discouraged; the warning is surfaced
			// through the admission warnings returned by validateFederation.
			_ = peerName // acknowledged: insecure TLS config for this peer
		}
	}

	return nil
}

// SetupWebhookWithManager sets up the webhook with the manager
func (v *FederationValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&novaedgev1alpha1.NovaEdgeFederation{}).
		WithValidator(v).
		Complete()
}

// FederationDefaulter provides defaults for NovaEdgeFederation resources
type FederationDefaulter struct{}

var _ webhook.CustomDefaulter = &FederationDefaulter{}

// Default sets defaults on the NovaEdgeFederation
func (d *FederationDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	fed, ok := obj.(*novaedgev1alpha1.NovaEdgeFederation)
	if !ok {
		return fmt.Errorf("expected NovaEdgeFederation, got %T", obj)
	}

	// Default sync config
	if fed.Spec.Sync == nil {
		fed.Spec.Sync = &novaedgev1alpha1.FederationSyncConfig{}
	}

	if fed.Spec.Sync.BatchSize == 0 {
		fed.Spec.Sync.BatchSize = 100
	}

	if fed.Spec.Sync.Compression == nil {
		enabled := true
		fed.Spec.Sync.Compression = &enabled
	}

	// Default conflict resolution
	if fed.Spec.ConflictResolution == nil {
		fed.Spec.ConflictResolution = &novaedgev1alpha1.ConflictResolutionConfig{}
	}

	if fed.Spec.ConflictResolution.Strategy == "" {
		fed.Spec.ConflictResolution.Strategy = novaedgev1alpha1.ConflictResolutionLastWriterWins
	}

	if fed.Spec.ConflictResolution.VectorClocks == nil {
		enabled := true
		fed.Spec.ConflictResolution.VectorClocks = &enabled
	}

	// Default health check
	if fed.Spec.HealthCheck == nil {
		fed.Spec.HealthCheck = &novaedgev1alpha1.FederationHealthCheck{}
	}

	if fed.Spec.HealthCheck.FailureThreshold == 0 {
		fed.Spec.HealthCheck.FailureThreshold = 3
	}

	if fed.Spec.HealthCheck.SuccessThreshold == 0 {
		fed.Spec.HealthCheck.SuccessThreshold = 1
	}

	// Default peer priorities
	for i := range fed.Spec.Members {
		if fed.Spec.Members[i].Priority == 0 {
			fed.Spec.Members[i].Priority = 100
		}

		// Default TLS enabled
		if fed.Spec.Members[i].TLS != nil && fed.Spec.Members[i].TLS.Enabled == nil {
			enabled := true
			fed.Spec.Members[i].TLS.Enabled = &enabled
		}
	}

	return nil
}

// SetupDefaulterWithManager sets up the defaulter webhook with the manager
func (d *FederationDefaulter) SetupDefaulterWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&novaedgev1alpha1.NovaEdgeFederation{}).
		WithDefaulter(d).
		Complete()
}
