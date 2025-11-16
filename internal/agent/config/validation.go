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

// Package config provides configuration validation utilities for NovaEdge agent.
package config

import (
	"fmt"
	"net"
	"regexp"

	pkgerrors "github.com/piwi3910/novaedge/internal/pkg/errors"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Validator provides configuration validation
type Validator struct{}

// NewValidator creates a new configuration validator
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateSnapshot validates a complete configuration snapshot
func (v *Validator) ValidateSnapshot(snapshot *Snapshot) error {
	if snapshot == nil || snapshot.ConfigSnapshot == nil {
		return pkgerrors.NewValidationError("snapshot cannot be nil")
	}

	if snapshot.Version == "" {
		return pkgerrors.NewValidationError("version is required").
			WithField("field", "version")
	}

	// Validate gateways
	for i, gateway := range snapshot.Gateways {
		if err := v.ValidateGateway(gateway); err != nil {
			return pkgerrors.NewValidationError("invalid gateway").
				WithField("index", i).
				WithField("gateway", fmt.Sprintf("%s/%s", gateway.Namespace, gateway.Name))
		}
	}

	// Validate clusters
	for i, cluster := range snapshot.Clusters {
		if err := v.ValidateCluster(cluster); err != nil {
			return pkgerrors.NewValidationError("invalid cluster").
				WithField("index", i).
				WithField("cluster", fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name"))
		}
	}

	return nil
}

// ValidateGateway validates a Gateway configuration
func (v *Validator) ValidateGateway(gateway *pb.Gateway) error {
	if gateway == nil {
		return pkgerrors.NewValidationError("gateway cannot be nil")
	}

	if gateway.Namespace == "" {
		return pkgerrors.NewValidationError("namespace is required").
			WithField("field", "namespace")
	}

	if gateway.Name == "" {
		return pkgerrors.NewValidationError("name is required").
			WithField("field", "name")
	}

	return nil
}

// ValidateCluster validates a Cluster configuration
func (v *Validator) ValidateCluster(cluster *pb.Cluster) error {
	if cluster == nil {
		return pkgerrors.NewValidationError("cluster cannot be nil")
	}

	if cluster.Namespace == "" {
		return pkgerrors.NewValidationError("namespace is required").
			WithField("field", "namespace")
	}

	if cluster.Name == "" {
		return pkgerrors.NewValidationError("name is required").
			WithField("field", "name")
	}

	// Validate endpoints
	for i, endpoint := range cluster.Endpoints {
		if err := v.ValidateEndpoint(endpoint); err != nil {
			return pkgerrors.NewValidationError("invalid endpoint").
				WithField("index", i)
		}
	}

	return nil
}

// ValidateEndpoint validates an Endpoint configuration
func (v *Validator) ValidateEndpoint(endpoint *pb.Endpoint) error {
	if endpoint == nil {
		return pkgerrors.NewValidationError("endpoint cannot be nil")
	}

	if endpoint.Address == "" {
		return pkgerrors.NewValidationError("address is required").
			WithField("field", "address")
	}

	// Validate address format (IP or hostname)
	if net.ParseIP(endpoint.Address) == nil {
		if !isValidHostname(endpoint.Address) {
			return pkgerrors.NewValidationError("invalid address format").
				WithField("field", "address").
				WithField("value", endpoint.Address)
		}
	}

	if endpoint.Port <= 0 || endpoint.Port > 65535 {
		return pkgerrors.NewValidationError("port must be between 1 and 65535").
			WithField("field", "port").
			WithField("value", endpoint.Port)
	}

	return nil
}

// isValidHostname checks if a string is a valid hostname
func isValidHostname(hostname string) bool {
	if len(hostname) == 0 || len(hostname) > 253 {
		return false
	}

	hostnameRegex := regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	return hostnameRegex.MatchString(hostname)
}
