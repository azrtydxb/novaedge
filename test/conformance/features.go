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

package conformance

import (
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/gateway-api/pkg/features"
)

// NovaEdgeCoreGatewayFeatures lists the core Gateway features supported by NovaEdge.
var NovaEdgeCoreGatewayFeatures = []features.FeatureName{
	features.SupportGateway,
	features.SupportHTTPRoute,
}

// NovaEdgeExtendedHTTPRouteFeatures lists the extended HTTPRoute features supported by NovaEdge.
var NovaEdgeExtendedHTTPRouteFeatures = []features.FeatureName{
	features.SupportHTTPRouteHostRewrite,
	features.SupportHTTPRoutePathRewrite,
	features.SupportHTTPRoutePathRedirect,
	features.SupportHTTPRouteSchemeRedirect,
	features.SupportHTTPRouteResponseHeaderModification,
	features.SupportHTTPRouteRequestMirror,
	features.SupportHTTPRouteQueryParamMatching,
	features.SupportHTTPRouteMethodMatching,
	features.SupportHTTPRouteRequestTimeout,
	features.SupportHTTPRouteBackendTimeout,
	features.SupportHTTPRoutePortRedirect,
	features.SupportHTTPRouteBackendProtocolH2C,
	features.SupportHTTPRouteBackendProtocolWebSocket,
	features.SupportHTTPRouteRequestMultipleMirrors,
	features.SupportHTTPRouteParentRefPort,
}

// NovaEdgeExtendedGatewayFeatures lists the extended Gateway features supported by NovaEdge.
var NovaEdgeExtendedGatewayFeatures = []features.FeatureName{
	features.SupportGatewayPort8080,
	features.SupportGatewayHTTPListenerIsolation,
}

// NovaEdgeRouteFeatures lists route-type features beyond HTTPRoute.
var NovaEdgeRouteFeatures = []features.FeatureName{
	features.SupportTLSRoute,
	features.SupportGRPCRoute,
}

// NovaEdgeMeshFeatures lists service mesh features supported by NovaEdge.
var NovaEdgeMeshFeatures = []features.FeatureName{
	features.SupportMesh,
}

// AllNovaEdgeSupportedFeatures returns the complete set of Gateway API features
// supported by NovaEdge. This is the canonical source of truth for conformance
// testing and reporting.
func AllNovaEdgeSupportedFeatures() sets.Set[features.FeatureName] {
	all := make([]features.FeatureName, 0,
		len(NovaEdgeCoreGatewayFeatures)+
			len(NovaEdgeExtendedHTTPRouteFeatures)+
			len(NovaEdgeExtendedGatewayFeatures)+
			len(NovaEdgeRouteFeatures)+
			len(NovaEdgeMeshFeatures))

	all = append(all, NovaEdgeCoreGatewayFeatures...)
	all = append(all, NovaEdgeExtendedHTTPRouteFeatures...)
	all = append(all, NovaEdgeExtendedGatewayFeatures...)
	all = append(all, NovaEdgeRouteFeatures...)
	all = append(all, NovaEdgeMeshFeatures...)

	return sets.New[features.FeatureName](all...)
}
