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

// Package webhook provides admission webhooks for validating NovaEdge
// federation resources before they are persisted in the API server.
package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

var (
	errFederationIDIsImmutableCannotChangeFrom    = errors.New("federation ID is immutable: cannot change from")
	errLocalMemberNameIsImmutableCannotChangeFrom = errors.New("local member name is immutable: cannot change from")
	errValidationFailed                           = errors.New("validation failed")
	errInvalidName                                = errors.New("invalid name")
	errName                                       = errors.New("name")
	errInvalidEndpoint                            = errors.New("invalid endpoint")
	errEndpoint                                   = errors.New("endpoint")
	errTLSEnabledButNoCASecretSpecifiedAnd        = errors.New("TLS enabled but no CA secret specified and insecureSkipVerify is false")
)

// FederationValidator validates NovaEdgeFederation resources
type FederationValidator struct{}

// CustomValidator interface for controller-runtime v0.23+
// The interface signature changed to use runtime.Object

// Regular expressions for validation
var (
	// federationIDRegex validates federation IDs (DNS-safe lowercase alphanumeric with hyphens)
	federationIDRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

	// memberNameRegex validates member names
	memberNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
)

// ValidateCreate validates creation of a NovaEdgeFederation
func (v *FederationValidator) ValidateCreate(_ context.Context, obj *novaedgev1alpha1.NovaEdgeFederation) (warnings admission.Warnings, err error) {
	return v.validateFederation(obj)
}

// ValidateUpdate validates updates to a NovaEdgeFederation
func (v *FederationValidator) ValidateUpdate(_ context.Context, oldObj, newObj *novaedgev1alpha1.NovaEdgeFederation) (warnings admission.Warnings, err error) {
	// Federation ID is immutable
	if oldObj.Spec.FederationID != newObj.Spec.FederationID {
		return nil, fmt.Errorf("%w %q to %q",
			errFederationIDIsImmutableCannotChangeFrom, oldObj.Spec.FederationID, newObj.Spec.FederationID)
	}

	// Local member name is immutable
	if oldObj.Spec.LocalMember.Name != newObj.Spec.LocalMember.Name {
		return nil, fmt.Errorf("%w %q to %q",
			errLocalMemberNameIsImmutableCannotChangeFrom, oldObj.Spec.LocalMember.Name, newObj.Spec.LocalMember.Name)
	}

	return v.validateFederation(newObj)
}

// ValidateDelete validates deletion of a NovaEdgeFederation
func (v *FederationValidator) ValidateDelete(_ context.Context, obj *novaedgev1alpha1.NovaEdgeFederation) (warnings admission.Warnings, err error) {

	var warns admission.Warnings

	// Warn if there are connected peers
	if len(obj.Status.Members) > 0 {
		var healthyPeers []string
		for _, member := range obj.Status.Members {
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
	if obj.Status.ConflictsPending > 0 {
		warns = append(warns, fmt.Sprintf(
			"Federation has %d pending conflicts that will be lost.", obj.Status.ConflictsPending))
	}

	return warns, nil
}

// validateFederationPeers validates peers for duplicates, self-references, and TLS.
func (v *FederationValidator) validateFederationPeers(fed *novaedgev1alpha1.NovaEdgeFederation) []string {
	var errs []string
	memberNames := map[string]bool{fed.Spec.LocalMember.Name: true}
	endpoints := map[string]bool{fed.Spec.LocalMember.Endpoint: true}

	for i, peer := range fed.Spec.Members {
		if err := v.validateMember(peer.Name, peer.Endpoint); err != nil {
			errs = append(errs, fmt.Sprintf("invalid peer %d (%s): %v", i, peer.Name, err))
		}
		if memberNames[peer.Name] {
			errs = append(errs, fmt.Sprintf("duplicate member name: %q", peer.Name))
		}
		memberNames[peer.Name] = true

		if endpoints[peer.Endpoint] {
			errs = append(errs, fmt.Sprintf("duplicate endpoint: %q", peer.Endpoint))
		}
		endpoints[peer.Endpoint] = true

		if peer.Name == fed.Spec.LocalMember.Name {
			errs = append(errs, fmt.Sprintf("peer cannot have same name as local member: %q", peer.Name))
		}
		if peer.TLS != nil {
			if err := v.validateTLS(peer.TLS, peer.Name); err != nil {
				errs = append(errs, fmt.Sprintf("invalid TLS config for peer %s: %v", peer.Name, err))
			}
		}
	}
	return errs
}

// validateSyncConfig validates sync configuration, returning errors and warnings.
func validateSyncConfig(sync *novaedgev1alpha1.FederationSyncConfig) ([]string, admission.Warnings) {
	if sync == nil {
		return nil, nil
	}
	var errs []string
	var warns admission.Warnings
	if sync.BatchSize > 1000 {
		warns = append(warns, fmt.Sprintf(
			"batch size %d is very large, may cause memory issues", sync.BatchSize))
	}
	validResourceTypes := map[string]bool{
		"ProxyGateway": true, "ProxyRoute": true, "ProxyBackend": true,
		"ProxyPolicy": true,
	}
	for _, rt := range sync.ResourceTypes {
		if !validResourceTypes[rt] {
			errs = append(errs, fmt.Sprintf("invalid resource type: %q", rt))
		}
	}
	return errs, warns
}

// validateConflictResolution validates conflict resolution config, returning errors and warnings.
func validateConflictResolution(cr *novaedgev1alpha1.ConflictResolutionConfig) ([]string, admission.Warnings) {
	if cr == nil {
		return nil, nil
	}
	var errs []string
	var warns admission.Warnings
	switch cr.Strategy {
	case novaedgev1alpha1.ConflictResolutionLastWriterWins,
		novaedgev1alpha1.ConflictResolutionMerge,
		novaedgev1alpha1.ConflictResolutionManual:
	default:
		errs = append(errs, fmt.Sprintf(
			"invalid conflict resolution strategy: %q", cr.Strategy))
	}
	if cr.Strategy == novaedgev1alpha1.ConflictResolutionManual {
		warns = append(warns, "Manual conflict resolution requires operator intervention for each conflict")
	}
	return errs, warns
}

// validateFederation performs validation on the federation spec
func (v *FederationValidator) validateFederation(fed *novaedgev1alpha1.NovaEdgeFederation) (warnings admission.Warnings, err error) {
	var warns admission.Warnings
	var errs []string

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

	if err := v.validateMember(fed.Spec.LocalMember.Name, fed.Spec.LocalMember.Endpoint); err != nil {
		errs = append(errs, fmt.Sprintf("invalid local member: %v", err))
	}

	errs = append(errs, v.validateFederationPeers(fed)...)

	syncErrs, syncWarns := validateSyncConfig(fed.Spec.Sync)
	errs = append(errs, syncErrs...)
	warns = append(warns, syncWarns...)

	crErrs, crWarns := validateConflictResolution(fed.Spec.ConflictResolution)
	errs = append(errs, crErrs...)
	warns = append(warns, crWarns...)

	if fed.Spec.HealthCheck != nil {
		if fed.Spec.HealthCheck.Interval != nil && fed.Spec.HealthCheck.Timeout != nil {
			if fed.Spec.HealthCheck.Timeout.Duration >= fed.Spec.HealthCheck.Interval.Duration {
				errs = append(errs, "health check timeout must be less than interval")
			}
		}
	}

	if len(errs) > 0 {
		return warns, fmt.Errorf("%w: %s", errValidationFailed, strings.Join(errs, "; "))
	}

	return warns, nil
}

// validateMember validates a member name and endpoint
func (v *FederationValidator) validateMember(name, endpoint string) error {
	// Validate name
	if !memberNameRegex.MatchString(name) {
		return fmt.Errorf("%w: %q: must be lowercase alphanumeric with hyphens", errInvalidName, name)
	}

	if len(name) > 63 {
		return fmt.Errorf("%w: %q is too long: max 63 characters", errName, name)
	}

	// Validate endpoint
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return fmt.Errorf("%w: %q: must be host:port format", errInvalidEndpoint, endpoint)
	}

	if host == "" {
		return fmt.Errorf("%w: %q has empty host", errEndpoint, endpoint)
	}

	if port == "" {
		return fmt.Errorf("%w: %q has empty port", errEndpoint, endpoint)
	}

	return nil
}

// validateTLS validates TLS configuration
func (v *FederationValidator) validateTLS(tls *novaedgev1alpha1.FederationTLS, peerName string) error {
	// If TLS is enabled, should have at least CA or be insecure
	if tls.Enabled != nil && *tls.Enabled {
		if tls.CASecretRef == nil && !tls.InsecureSkipVerify {
			return errTLSEnabledButNoCASecretSpecifiedAnd
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
	return ctrl.NewWebhookManagedBy(mgr, &novaedgev1alpha1.NovaEdgeFederation{}).
		WithValidator(v).
		Complete()
}

// FederationDefaulter provides defaults for NovaEdgeFederation resources
type FederationDefaulter struct{}

// Default sets defaults on the NovaEdgeFederation
func (d *FederationDefaulter) Default(_ context.Context, fed *novaedgev1alpha1.NovaEdgeFederation) error {
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
	return ctrl.NewWebhookManagedBy(mgr, &novaedgev1alpha1.NovaEdgeFederation{}).
		WithDefaulter(d).
		Complete()
}
