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
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
)

var (
	errGatewayClass                                   = errors.New("gateway class")
	errUnsupportedProtocol                            = errors.New("unsupported gateway protocol")
	errListener                                       = errors.New("listener")
	errRuleHasNoBackendRefs                           = errors.New("rule has no backend refs")
	errUnsupportedPathMatchType                       = errors.New("unsupported path match type")
	errUnsupportedHeaderMatchType                     = errors.New("unsupported header match type")
	errUnsupportedQueryParamMatchType                 = errors.New("unsupported query param match type")
	errRequestHeaderModifierFilterHasNoConfiguration  = errors.New("RequestHeaderModifier filter has no configuration")
	errResponseHeaderModifierFilterHasNoConfiguration = errors.New("ResponseHeaderModifier filter has no configuration")
	errRequestRedirectFilterHasNoConfiguration        = errors.New("RequestRedirect filter has no configuration")
	errURLRewriteFilterHasNoConfiguration             = errors.New("URLRewrite filter has no configuration")
	errRequestMirrorFilterHasNoConfiguration          = errors.New("RequestMirror filter has no configuration")
	errUnsupportedFilterType                          = errors.New("unsupported filter type")
	errGRPCRuleHasNoBackendRefs                       = errors.New("gRPC rule has no backend refs")
)

const (
	// NovaEdgeGatewayClassName is the GatewayClass name that NovaEdge handles
	NovaEdgeGatewayClassName = "novaedge"
	// OwnerAnnotation marks resources as owned by Gateway API translation
	OwnerAnnotation = "novaedge.io/gateway-api-owner"
)

// TranslateGatewayToProxyGateway translates a Gateway API Gateway to a NovaEdge ProxyGateway
func TranslateGatewayToProxyGateway(gateway *gatewayv1.Gateway, _ string) (*novaedgev1alpha1.ProxyGateway, error) {
	// Only translate gateways with our GatewayClass
	if string(gateway.Spec.GatewayClassName) != NovaEdgeGatewayClassName {
		return nil, fmt.Errorf("%w: %s is not supported, expected %s", errGatewayClass, gateway.Spec.GatewayClassName, NovaEdgeGatewayClassName)
	}

	// Translate listeners
	listeners := make([]novaedgev1alpha1.Listener, 0, len(gateway.Spec.Listeners))
	for _, gwListener := range gateway.Spec.Listeners {
		listener, err := translateListener(gwListener, gateway.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to translate listener %s: %w", gwListener.Name, err)
		}
		listeners = append(listeners, listener)
	}

	// Create ProxyGateway
	proxyGateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.Name,
			Namespace: gateway.Namespace,
			Labels:    gateway.Labels,
			Annotations: map[string]string{
				OwnerAnnotation: fmt.Sprintf("Gateway/%s/%s", gateway.Namespace, gateway.Name),
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       kindGateway,
					Name:       gateway.Name,
					UID:        gateway.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			Listeners: listeners,
		},
	}

	return proxyGateway, nil
}

// translateListener translates a Gateway API listener to a NovaEdge listener
func translateListener(gwListener gatewayv1.Listener, namespace string) (novaedgev1alpha1.Listener, error) {
	listener := novaedgev1alpha1.Listener{
		Name: string(gwListener.Name),
		Port: gwListener.Port,
	}

	// Translate protocol
	switch gwListener.Protocol {
	case gatewayv1.HTTPProtocolType:
		listener.Protocol = novaedgev1alpha1.ProtocolTypeHTTP
	case gatewayv1.HTTPSProtocolType:
		listener.Protocol = novaedgev1alpha1.ProtocolTypeHTTPS
	case gatewayv1.TLSProtocolType:
		listener.Protocol = novaedgev1alpha1.ProtocolTypeTLS
	case gatewayv1.TCPProtocolType:
		listener.Protocol = novaedgev1alpha1.ProtocolTypeTCP
	case gatewayv1.UDPProtocolType:
		return listener, fmt.Errorf("%w: %s", errUnsupportedProtocol, gwListener.Protocol)
	default:
		return listener, fmt.Errorf("%w: %s", errUnsupportedProtocol, gwListener.Protocol)
	}

	// Translate hostnames
	if gwListener.Hostname != nil {
		listener.Hostnames = []string{string(*gwListener.Hostname)}
	}

	// Translate TLS configuration
	if gwListener.TLS != nil {
		if len(gwListener.TLS.CertificateRefs) == 0 {
			return listener, fmt.Errorf("%w: %s has TLS configured but no certificate refs", errListener, gwListener.Name)
		}

		// Use the first certificate ref
		certRef := gwListener.TLS.CertificateRefs[0]

		// Determine namespace for secret
		secretNamespace := namespace
		if certRef.Namespace != nil {
			secretNamespace = string(*certRef.Namespace)
		}

		listener.TLS = &novaedgev1alpha1.TLSConfig{
			SecretRef: &corev1.SecretReference{
				Name:      string(certRef.Name),
				Namespace: secretNamespace,
			},
		}
	}

	return listener, nil
}

// TranslateHTTPRouteToProxyRoute translates a Gateway API HTTPRoute to a NovaEdge ProxyRoute
func TranslateHTTPRouteToProxyRoute(httpRoute *gatewayv1.HTTPRoute) (*novaedgev1alpha1.ProxyRoute, error) {
	// Translate hostnames
	hostnames := make([]string, 0, len(httpRoute.Spec.Hostnames))
	for _, hostname := range httpRoute.Spec.Hostnames {
		hostnames = append(hostnames, string(hostname))
	}

	// Translate rules
	rules := make([]novaedgev1alpha1.HTTPRouteRule, 0, len(httpRoute.Spec.Rules))
	for i, gwRule := range httpRoute.Spec.Rules {
		rule, err := translateHTTPRouteRule(gwRule, httpRoute.Namespace, i)
		if err != nil {
			return nil, fmt.Errorf("failed to translate rule %d: %w", i, err)
		}
		rules = append(rules, rule)
	}

	// Create ProxyRoute
	proxyRoute := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      httpRoute.Name,
			Namespace: httpRoute.Namespace,
			Labels:    httpRoute.Labels,
			Annotations: map[string]string{
				OwnerAnnotation: fmt.Sprintf("HTTPRoute/%s/%s", httpRoute.Namespace, httpRoute.Name),
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "HTTPRoute",
					Name:       httpRoute.Name,
					UID:        httpRoute.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: novaedgev1alpha1.ProxyRouteSpec{
			Hostnames: hostnames,
			Rules:     rules,
		},
	}

	return proxyRoute, nil
}

// translateHTTPRouteRule translates a Gateway API HTTPRouteRule to a NovaEdge HTTPRouteRule
func translateHTTPRouteRule(gwRule gatewayv1.HTTPRouteRule, namespace string, _ int) (novaedgev1alpha1.HTTPRouteRule, error) {
	rule := novaedgev1alpha1.HTTPRouteRule{}

	// Translate matches
	for _, gwMatch := range gwRule.Matches {
		match, err := translateHTTPRouteMatch(gwMatch)
		if err != nil {
			return rule, fmt.Errorf("failed to translate match: %w", err)
		}
		rule.Matches = append(rule.Matches, match)
	}

	// Translate filters
	for _, gwFilter := range gwRule.Filters {
		filter, err := translateHTTPRouteFilter(gwFilter)
		if err != nil {
			return rule, fmt.Errorf("failed to translate filter: %w", err)
		}
		rule.Filters = append(rule.Filters, filter)
	}

	// Translate backend refs with weights
	if len(gwRule.BackendRefs) == 0 {
		return rule, errRuleHasNoBackendRefs
	}

	// Translate all backend refs
	rule.BackendRefs = make([]novaedgev1alpha1.BackendRef, 0, len(gwRule.BackendRefs))
	for _, gwBackendRef := range gwRule.BackendRefs {
		// Determine namespace for backend
		backendNamespace := namespace
		if gwBackendRef.Namespace != nil {
			backendNamespace = string(*gwBackendRef.Namespace)
		}

		backendRef := novaedgev1alpha1.BackendRef{
			Name:      string(gwBackendRef.Name),
			Namespace: &backendNamespace,
		}

		// Copy weight if specified, otherwise default to 1
		if gwBackendRef.Weight != nil {
			backendRef.Weight = gwBackendRef.Weight
		} else {
			defaultWeight := int32(1)
			backendRef.Weight = &defaultWeight
		}

		// Translate per-backend filters
		for _, gwFilter := range gwBackendRef.Filters {
			filter, err := translateHTTPRouteFilter(gwFilter)
			if err != nil {
				return rule, fmt.Errorf("failed to translate backend filter: %w", err)
			}
			rule.Filters = append(rule.Filters, filter)
		}

		rule.BackendRefs = append(rule.BackendRefs, backendRef)
	}

	return rule, nil
}

// translateHTTPRouteMatch translates a Gateway API HTTPRouteMatch to a NovaEdge HTTPRouteMatch
func translateHTTPRouteMatch(gwMatch gatewayv1.HTTPRouteMatch) (novaedgev1alpha1.HTTPRouteMatch, error) {
	match := novaedgev1alpha1.HTTPRouteMatch{}

	// Translate path match
	if gwMatch.Path != nil {
		pathMatch := &novaedgev1alpha1.HTTPPathMatch{
			Value: *gwMatch.Path.Value,
		}

		pathType := gatewayv1.PathMatchPathPrefix
		if gwMatch.Path.Type != nil {
			pathType = *gwMatch.Path.Type
		}

		switch pathType {
		case gatewayv1.PathMatchExact:
			pathMatch.Type = novaedgev1alpha1.PathMatchExact
		case gatewayv1.PathMatchPathPrefix:
			pathMatch.Type = novaedgev1alpha1.PathMatchPathPrefix
		case gatewayv1.PathMatchRegularExpression:
			pathMatch.Type = novaedgev1alpha1.PathMatchRegularExpression
		default:
			return match, fmt.Errorf("%w: %v", errUnsupportedPathMatchType, gwMatch.Path.Type)
		}

		match.Path = pathMatch
	}

	// Translate headers
	for _, gwHeader := range gwMatch.Headers {
		headerMatch := novaedgev1alpha1.HTTPHeaderMatch{
			Name:  string(gwHeader.Name),
			Value: gwHeader.Value,
		}

		if gwHeader.Type != nil {
			switch *gwHeader.Type {
			case gatewayv1.HeaderMatchExact:
				headerMatch.Type = novaedgev1alpha1.HeaderMatchExact
			case gatewayv1.HeaderMatchRegularExpression:
				headerMatch.Type = novaedgev1alpha1.HeaderMatchRegularExpression
			default:
				return match, fmt.Errorf("%w: %v", errUnsupportedHeaderMatchType, gwHeader.Type)
			}
		} else {
			headerMatch.Type = novaedgev1alpha1.HeaderMatchExact
		}

		match.Headers = append(match.Headers, headerMatch)
	}

	// Translate query parameters
	for _, gwQueryParam := range gwMatch.QueryParams {
		queryParamMatch := novaedgev1alpha1.HTTPQueryParamMatch{
			Name:  string(gwQueryParam.Name),
			Value: gwQueryParam.Value,
		}

		if gwQueryParam.Type != nil {
			switch *gwQueryParam.Type {
			case gatewayv1.QueryParamMatchExact:
				queryParamMatch.Type = novaedgev1alpha1.HeaderMatchExact
			case gatewayv1.QueryParamMatchRegularExpression:
				queryParamMatch.Type = novaedgev1alpha1.HeaderMatchRegularExpression
			default:
				return match, fmt.Errorf("%w: %v", errUnsupportedQueryParamMatchType, gwQueryParam.Type)
			}
		} else {
			queryParamMatch.Type = novaedgev1alpha1.HeaderMatchExact
		}

		match.QueryParams = append(match.QueryParams, queryParamMatch)
	}

	// Translate method
	if gwMatch.Method != nil {
		method := string(*gwMatch.Method)
		match.Method = &method
	}

	return match, nil
}

// translateHTTPRouteFilter translates a Gateway API HTTPRouteFilter to a NovaEdge HTTPRouteFilter
func translateHTTPRouteFilter(gwFilter gatewayv1.HTTPRouteFilter) (novaedgev1alpha1.HTTPRouteFilter, error) {
	switch gwFilter.Type {
	case gatewayv1.HTTPRouteFilterRequestHeaderModifier:
		return translateRequestHeaderModifierFilter(gwFilter)
	case gatewayv1.HTTPRouteFilterResponseHeaderModifier:
		return translateResponseHeaderModifierFilter(gwFilter)
	case gatewayv1.HTTPRouteFilterRequestRedirect:
		return translateRequestRedirectFilter(gwFilter)
	case gatewayv1.HTTPRouteFilterURLRewrite:
		return translateURLRewriteFilter(gwFilter)
	case gatewayv1.HTTPRouteFilterRequestMirror:
		return translateRequestMirrorFilter(gwFilter)
	case gatewayv1.HTTPRouteFilterCORS,
		gatewayv1.HTTPRouteFilterExternalAuth,
		gatewayv1.HTTPRouteFilterExtensionRef:
		return novaedgev1alpha1.HTTPRouteFilter{}, fmt.Errorf("%w: %v", errUnsupportedFilterType, gwFilter.Type)
	default:
		return novaedgev1alpha1.HTTPRouteFilter{}, fmt.Errorf("%w: %v", errUnsupportedFilterType, gwFilter.Type)
	}
}

func translateRequestHeaderModifierFilter(gwFilter gatewayv1.HTTPRouteFilter) (novaedgev1alpha1.HTTPRouteFilter, error) {
	filter := novaedgev1alpha1.HTTPRouteFilter{}
	if gwFilter.RequestHeaderModifier == nil {
		return filter, errRequestHeaderModifierFilterHasNoConfiguration
	}

	if len(gwFilter.RequestHeaderModifier.Add) > 0 {
		filter.Type = novaedgev1alpha1.HTTPRouteFilterAddHeader
		for _, header := range gwFilter.RequestHeaderModifier.Add {
			filter.Add = append(filter.Add, novaedgev1alpha1.HTTPHeader{Name: string(header.Name), Value: header.Value})
		}
	}

	if len(gwFilter.RequestHeaderModifier.Set) > 0 {
		filter.Type = novaedgev1alpha1.HTTPRouteFilterAddHeader
		for _, header := range gwFilter.RequestHeaderModifier.Set {
			filter.Add = append(filter.Add, novaedgev1alpha1.HTTPHeader{Name: string(header.Name), Value: header.Value})
		}
	}

	if len(gwFilter.RequestHeaderModifier.Remove) > 0 {
		if filter.Type == "" {
			filter.Type = novaedgev1alpha1.HTTPRouteFilterRemoveHeader
		}
		filter.Remove = gwFilter.RequestHeaderModifier.Remove
	}

	if filter.Type == "" {
		filter.Type = novaedgev1alpha1.HTTPRouteFilterAddHeader
	}
	return filter, nil
}

func translateResponseHeaderModifierFilter(gwFilter gatewayv1.HTTPRouteFilter) (novaedgev1alpha1.HTTPRouteFilter, error) {
	filter := novaedgev1alpha1.HTTPRouteFilter{}
	if gwFilter.ResponseHeaderModifier == nil {
		return filter, errResponseHeaderModifierFilterHasNoConfiguration
	}

	filter.Type = novaedgev1alpha1.HTTPRouteFilterResponseAddHeader
	for _, header := range gwFilter.ResponseHeaderModifier.Add {
		filter.ResponseAdd = append(filter.ResponseAdd, novaedgev1alpha1.HTTPHeader{Name: string(header.Name), Value: header.Value})
	}
	for _, header := range gwFilter.ResponseHeaderModifier.Set {
		filter.ResponseSet = append(filter.ResponseSet, novaedgev1alpha1.HTTPHeader{Name: string(header.Name), Value: header.Value})
	}
	filter.ResponseRemove = gwFilter.ResponseHeaderModifier.Remove
	return filter, nil
}

// resolvePathModifier extracts the rewrite path from a Gateway API path modifier.
func resolvePathModifier(pathMod *gatewayv1.HTTPPathModifier) *string {
	if pathMod == nil {
		return nil
	}
	if pathMod.Type == gatewayv1.FullPathHTTPPathModifier && pathMod.ReplaceFullPath != nil {
		return pathMod.ReplaceFullPath
	}
	if pathMod.Type == gatewayv1.PrefixMatchHTTPPathModifier && pathMod.ReplacePrefixMatch != nil {
		return pathMod.ReplacePrefixMatch
	}
	return nil
}

func translateRequestRedirectFilter(gwFilter gatewayv1.HTTPRouteFilter) (novaedgev1alpha1.HTTPRouteFilter, error) {
	filter := novaedgev1alpha1.HTTPRouteFilter{}
	if gwFilter.RequestRedirect == nil {
		return filter, errRequestRedirectFilterHasNoConfiguration
	}

	filter.Type = novaedgev1alpha1.HTTPRouteFilterRequestRedirect

	if gwFilter.RequestRedirect.Scheme != nil || gwFilter.RequestRedirect.Hostname != nil || gwFilter.RequestRedirect.Port != nil {
		scheme := "http"
		if gwFilter.RequestRedirect.Scheme != nil {
			scheme = *gwFilter.RequestRedirect.Scheme
		}
		hostname := ""
		if gwFilter.RequestRedirect.Hostname != nil {
			hostname = string(*gwFilter.RequestRedirect.Hostname)
		}
		port := ""
		if gwFilter.RequestRedirect.Port != nil {
			port = fmt.Sprintf(":%d", *gwFilter.RequestRedirect.Port)
		}
		redirectURL := fmt.Sprintf("%s://%s%s", scheme, hostname, port)
		filter.RedirectURL = &redirectURL
	}

	filter.RewritePath = resolvePathModifier(gwFilter.RequestRedirect.Path)
	return filter, nil
}

func translateURLRewriteFilter(gwFilter gatewayv1.HTTPRouteFilter) (novaedgev1alpha1.HTTPRouteFilter, error) {
	filter := novaedgev1alpha1.HTTPRouteFilter{}
	if gwFilter.URLRewrite == nil {
		return filter, errURLRewriteFilterHasNoConfiguration
	}

	filter.Type = novaedgev1alpha1.HTTPRouteFilterURLRewrite

	if gwFilter.URLRewrite.Hostname != nil {
		hostname := string(*gwFilter.URLRewrite.Hostname)
		filter.Add = append(filter.Add, novaedgev1alpha1.HTTPHeader{Name: "Host", Value: hostname})
	}

	filter.RewritePath = resolvePathModifier(gwFilter.URLRewrite.Path)
	return filter, nil
}

func translateRequestMirrorFilter(gwFilter gatewayv1.HTTPRouteFilter) (novaedgev1alpha1.HTTPRouteFilter, error) {
	filter := novaedgev1alpha1.HTTPRouteFilter{}
	if gwFilter.RequestMirror == nil {
		return filter, errRequestMirrorFilterHasNoConfiguration
	}

	filter.Type = novaedgev1alpha1.HTTPRouteFilterRequestMirror

	mirrorNamespace := ""
	if gwFilter.RequestMirror.BackendRef.Namespace != nil {
		mirrorNamespace = string(*gwFilter.RequestMirror.BackendRef.Namespace)
	}

	filter.MirrorBackend = &novaedgev1alpha1.BackendRef{
		Name:      string(gwFilter.RequestMirror.BackendRef.Name),
		Namespace: &mirrorNamespace,
	}

	if gwFilter.RequestMirror.Percent != nil {
		percent := *gwFilter.RequestMirror.Percent
		filter.MirrorPercent = &percent
	}
	return filter, nil
}

// GenerateProxyBackendName generates a ProxyBackend name from service reference
func GenerateProxyBackendName(serviceName, namespace string, port int32) string {
	return fmt.Sprintf("%s-%s-%d", namespace, serviceName, port)
}

// boolPtr returns a pointer to a bool value
func boolPtr(b bool) *bool {
	return &b
}

// TranslateGRPCRouteToProxyRoute translates a Gateway API GRPCRoute to a NovaEdge ProxyRoute
func TranslateGRPCRouteToProxyRoute(grpcRoute *gatewayv1.GRPCRoute) (*novaedgev1alpha1.ProxyRoute, error) {
	// Translate hostnames
	hostnames := make([]string, 0, len(grpcRoute.Spec.Hostnames))
	for _, hostname := range grpcRoute.Spec.Hostnames {
		hostnames = append(hostnames, string(hostname))
	}

	// Translate rules
	rules := make([]novaedgev1alpha1.HTTPRouteRule, 0, len(grpcRoute.Spec.Rules))
	for i, gwRule := range grpcRoute.Spec.Rules {
		rule, err := translateGRPCRouteRule(gwRule, grpcRoute.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to translate gRPC rule %d: %w", i, err)
		}
		rules = append(rules, rule)
	}

	proxyRoute := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grpc-" + grpcRoute.Name,
			Namespace: grpcRoute.Namespace,
			Labels:    grpcRoute.Labels,
			Annotations: map[string]string{
				OwnerAnnotation:          fmt.Sprintf("GRPCRoute/%s/%s", grpcRoute.Namespace, grpcRoute.Name),
				"novaedge.io/route-type": "grpc",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "GRPCRoute",
					Name:       grpcRoute.Name,
					UID:        grpcRoute.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: novaedgev1alpha1.ProxyRouteSpec{
			Hostnames: hostnames,
			Rules:     rules,
		},
	}

	return proxyRoute, nil
}

// translateGRPCRouteRule translates a Gateway API GRPCRouteRule to a NovaEdge HTTPRouteRule
func translateGRPCRouteRule(gwRule gatewayv1.GRPCRouteRule, namespace string) (novaedgev1alpha1.HTTPRouteRule, error) {
	rule := novaedgev1alpha1.HTTPRouteRule{}

	// Translate gRPC matches into HTTP matches
	// gRPC uses path-based matching: /<service>/<method>
	for _, gwMatch := range gwRule.Matches {
		match := novaedgev1alpha1.HTTPRouteMatch{}

		// gRPC service and method are encoded as path matches
		if gwMatch.Method != nil {
			pathValue := "/"
			if gwMatch.Method.Service != nil {
				pathValue += *gwMatch.Method.Service
			}
			if gwMatch.Method.Method != nil {
				pathValue += "/" + *gwMatch.Method.Method
			}

			matchType := novaedgev1alpha1.PathMatchExact
			if gwMatch.Method.Type != nil && *gwMatch.Method.Type == gatewayv1.GRPCMethodMatchRegularExpression {
				matchType = novaedgev1alpha1.PathMatchRegularExpression
			}

			match.Path = &novaedgev1alpha1.HTTPPathMatch{
				Type:  matchType,
				Value: pathValue,
			}
		}

		// Translate gRPC header matches
		for _, gwHeader := range gwMatch.Headers {
			headerMatch := novaedgev1alpha1.HTTPHeaderMatch{
				Name:  string(gwHeader.Name),
				Value: gwHeader.Value,
			}

			if gwHeader.Type != nil {
				switch *gwHeader.Type {
				case gatewayv1.GRPCHeaderMatchExact:
					headerMatch.Type = novaedgev1alpha1.HeaderMatchExact
				case gatewayv1.GRPCHeaderMatchRegularExpression:
					headerMatch.Type = novaedgev1alpha1.HeaderMatchRegularExpression
				default:
					headerMatch.Type = novaedgev1alpha1.HeaderMatchExact
				}
			} else {
				headerMatch.Type = novaedgev1alpha1.HeaderMatchExact
			}

			match.Headers = append(match.Headers, headerMatch)
		}

		// Force POST method for gRPC (all gRPC calls use POST)
		postMethod := "POST"
		match.Method = &postMethod

		// Add gRPC content-type header match
		match.Headers = append(match.Headers, novaedgev1alpha1.HTTPHeaderMatch{
			Name:  "Content-Type",
			Type:  novaedgev1alpha1.HeaderMatchExact,
			Value: "application/grpc",
		})

		rule.Matches = append(rule.Matches, match)
	}

	// If no matches specified, create a wildcard match for all gRPC traffic
	if len(gwRule.Matches) == 0 {
		postMethod := "POST"
		rule.Matches = append(rule.Matches, novaedgev1alpha1.HTTPRouteMatch{
			Method: &postMethod,
			Headers: []novaedgev1alpha1.HTTPHeaderMatch{
				{
					Name:  "Content-Type",
					Type:  novaedgev1alpha1.HeaderMatchExact,
					Value: "application/grpc",
				},
			},
		})
	}

	// Translate backend refs
	if len(gwRule.BackendRefs) == 0 {
		return rule, errGRPCRuleHasNoBackendRefs
	}

	rule.BackendRefs = make([]novaedgev1alpha1.BackendRef, 0, len(gwRule.BackendRefs))
	for _, gwBackendRef := range gwRule.BackendRefs {
		backendNamespace := namespace
		if gwBackendRef.Namespace != nil {
			backendNamespace = string(*gwBackendRef.Namespace)
		}

		port := int32(80)
		if gwBackendRef.Port != nil {
			port = *gwBackendRef.Port
		}

		backendRef := novaedgev1alpha1.BackendRef{
			Name:      GenerateProxyBackendName(string(gwBackendRef.Name), backendNamespace, port),
			Namespace: &backendNamespace,
		}

		if gwBackendRef.Weight != nil {
			backendRef.Weight = gwBackendRef.Weight
		} else {
			defaultWeight := int32(1)
			backendRef.Weight = &defaultWeight
		}

		rule.BackendRefs = append(rule.BackendRefs, backendRef)
	}

	return rule, nil
}
