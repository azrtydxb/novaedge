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

// Package conformance implements Gateway API conformance testing for NovaEdge.
package conformance

import (
	"encoding/json"
	"fmt"
	"time"

	confv1 "sigs.k8s.io/gateway-api/conformance/apis/v1"
)

// NovaEdgeConformanceProfile defines the NovaEdge conformance profile metadata.
type NovaEdgeConformanceProfile struct {
	// Implementation details
	Organization string
	Project      string
	Version      string
	URL          string
	Contact      []string

	// Conformance details
	GatewayAPIVersion string
	GatewayAPIChannel string
	Date              string
}

// DefaultProfile returns the default NovaEdge conformance profile.
func DefaultProfile() *NovaEdgeConformanceProfile {
	return &NovaEdgeConformanceProfile{
		Organization:      "novaedge",
		Project:           "novaedge",
		Version:           "v0.1.0",
		URL:               "https://github.com/piwi3910/novaedge",
		Contact:           []string{"@piwi3910"},
		GatewayAPIVersion: "v1.4.0",
		GatewayAPIChannel: "standard",
		Date:              time.Now().Format(time.RFC3339),
	}
}

// SupportedResources returns the list of Gateway API resources supported by NovaEdge.
func SupportedResources() []string {
	return []string{
		"GatewayClass",
		"Gateway",
		"HTTPRoute",
		"GRPCRoute",
		"TLSRoute",
		"ReferenceGrant",
	}
}

// SupportedCoreFeatures returns the list of core features supported by NovaEdge.
func SupportedCoreFeatures() []string {
	return []string{
		"Gateway",
		"HTTPRoute",
		"HTTPRouteHostRewrite",
		"HTTPRoutePathRewrite",
		"HTTPRoutePathRedirect",
		"HTTPRouteSchemeRedirect",
		"HTTPRouteResponseHeaderModification",
		"HTTPRouteRequestMirror",
	}
}

// SupportedExtendedFeatures returns the list of extended features supported by NovaEdge.
func SupportedExtendedFeatures() []string {
	return []string{
		"GatewayPort8080",
		"GatewayHTTPListenerIsolation",
		"HTTPRouteQueryParamMatching",
		"HTTPRouteMethodMatching",
		"HTTPRouteRequestTimeout",
		"HTTPRouteBackendTimeout",
		"HTTPRoutePortRedirect",
		"HTTPRouteBackendProtocolH2C",
		"HTTPRouteBackendProtocolWebSocket",
		"HTTPRouteRequestMultipleMirrors",
		"HTTPRouteParentRefPort",
		"TLSRoute",
		"GRPCRoute",
		"Mesh",
	}
}

// PlannedFeatures returns the list of features planned for future implementation.
func PlannedFeatures() []string {
	return []string{
		"TCPRoute",
		"UDPRoute",
		"ReferenceGrant",
		"GatewayStaticAddresses",
		"GatewayInfrastructurePropagation",
		"HTTPRouteBackendRequestHeaderModification",
		"HTTPRouteRequestPercentageMirror",
		"HTTPRouteCORS",
	}
}

// FormatReport formats a conformance report as a JSON string for output.
func FormatReport(report *confv1.ConformanceReport) (string, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal conformance report: %w", err)
	}
	return string(data), nil
}
