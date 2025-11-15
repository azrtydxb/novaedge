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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

const (
	// NovaEdgeGatewayClassName is the GatewayClass name that NovaEdge handles
	NovaEdgeGatewayClassName = "novaedge"
	// OwnerAnnotation marks resources as owned by Gateway API translation
	OwnerAnnotation = "novaedge.io/gateway-api-owner"
)

// TranslateGatewayToProxyGateway translates a Gateway API Gateway to a NovaEdge ProxyGateway
func TranslateGatewayToProxyGateway(gateway *gatewayv1.Gateway, vipName string) (*novaedgev1alpha1.ProxyGateway, error) {
	// Only translate gateways with our GatewayClass
	if string(gateway.Spec.GatewayClassName) != NovaEdgeGatewayClassName {
		return nil, fmt.Errorf("gateway class %s is not supported, expected %s", gateway.Spec.GatewayClassName, NovaEdgeGatewayClassName)
	}

	// Translate listeners
	var listeners []novaedgev1alpha1.Listener
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
					Kind:       "Gateway",
					Name:       gateway.Name,
					UID:        gateway.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef:    vipName,
			Listeners: listeners,
		},
	}

	return proxyGateway, nil
}

// translateListener translates a Gateway API listener to a NovaEdge listener
func translateListener(gwListener gatewayv1.Listener, namespace string) (novaedgev1alpha1.Listener, error) {
	listener := novaedgev1alpha1.Listener{
		Name: string(gwListener.Name),
		Port: int32(gwListener.Port),
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
	default:
		return listener, fmt.Errorf("unsupported protocol: %s", gwListener.Protocol)
	}

	// Translate hostnames
	if gwListener.Hostname != nil {
		listener.Hostnames = []string{string(*gwListener.Hostname)}
	}

	// Translate TLS configuration
	if gwListener.TLS != nil {
		if len(gwListener.TLS.CertificateRefs) == 0 {
			return listener, fmt.Errorf("listener %s has TLS configured but no certificate refs", gwListener.Name)
		}

		// Use the first certificate ref
		certRef := gwListener.TLS.CertificateRefs[0]

		// Determine namespace for secret
		secretNamespace := namespace
		if certRef.Namespace != nil {
			secretNamespace = string(*certRef.Namespace)
		}

		listener.TLS = &novaedgev1alpha1.TLSConfig{
			SecretRef: corev1.SecretReference{
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
	var hostnames []string
	for _, hostname := range httpRoute.Spec.Hostnames {
		hostnames = append(hostnames, string(hostname))
	}

	// Translate rules
	var rules []novaedgev1alpha1.HTTPRouteRule
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
func translateHTTPRouteRule(gwRule gatewayv1.HTTPRouteRule, namespace string, ruleIndex int) (novaedgev1alpha1.HTTPRouteRule, error) {
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

	// Translate backend refs
	// Gateway API supports multiple backend refs with weights, but we'll use the first one for now
	// TODO: Support multiple backends with weighted load balancing
	if len(gwRule.BackendRefs) == 0 {
		return rule, fmt.Errorf("rule has no backend refs")
	}

	backendRef := gwRule.BackendRefs[0]

	// Determine namespace for backend
	backendNamespace := namespace
	if backendRef.Namespace != nil {
		backendNamespace = string(*backendRef.Namespace)
	}

	rule.BackendRef = novaedgev1alpha1.BackendRef{
		Name:      string(backendRef.Name),
		Namespace: &backendNamespace,
	}

	if backendRef.Weight != nil {
		rule.BackendRef.Weight = backendRef.Weight
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

		switch *gwMatch.Path.Type {
		case gatewayv1.PathMatchExact:
			pathMatch.Type = novaedgev1alpha1.PathMatchExact
		case gatewayv1.PathMatchPathPrefix:
			pathMatch.Type = novaedgev1alpha1.PathMatchPathPrefix
		case gatewayv1.PathMatchRegularExpression:
			pathMatch.Type = novaedgev1alpha1.PathMatchRegularExpression
		default:
			return match, fmt.Errorf("unsupported path match type: %v", gwMatch.Path.Type)
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
				return match, fmt.Errorf("unsupported header match type: %v", gwHeader.Type)
			}
		} else {
			headerMatch.Type = novaedgev1alpha1.HeaderMatchExact
		}

		match.Headers = append(match.Headers, headerMatch)
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
	filter := novaedgev1alpha1.HTTPRouteFilter{}

	switch gwFilter.Type {
	case gatewayv1.HTTPRouteFilterRequestHeaderModifier:
		if gwFilter.RequestHeaderModifier == nil {
			return filter, fmt.Errorf("RequestHeaderModifier filter has no configuration")
		}

		// Handle header additions
		if len(gwFilter.RequestHeaderModifier.Add) > 0 {
			filter.Type = novaedgev1alpha1.HTTPRouteFilterAddHeader
			for _, header := range gwFilter.RequestHeaderModifier.Add {
				filter.Add = append(filter.Add, novaedgev1alpha1.HTTPHeader{
					Name:  string(header.Name),
					Value: header.Value,
				})
			}
		}

		// Handle header removals
		if len(gwFilter.RequestHeaderModifier.Remove) > 0 {
			filter.Type = novaedgev1alpha1.HTTPRouteFilterRemoveHeader
			filter.Remove = gwFilter.RequestHeaderModifier.Remove
		}

	case gatewayv1.HTTPRouteFilterRequestRedirect:
		if gwFilter.RequestRedirect == nil {
			return filter, fmt.Errorf("RequestRedirect filter has no configuration")
		}

		filter.Type = novaedgev1alpha1.HTTPRouteFilterRequestRedirect

		// Build redirect URL from components
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

	case gatewayv1.HTTPRouteFilterURLRewrite:
		if gwFilter.URLRewrite == nil {
			return filter, fmt.Errorf("URLRewrite filter has no configuration")
		}

		filter.Type = novaedgev1alpha1.HTTPRouteFilterURLRewrite

		if gwFilter.URLRewrite.Path != nil {
			if gwFilter.URLRewrite.Path.Type == gatewayv1.FullPathHTTPPathModifier && gwFilter.URLRewrite.Path.ReplaceFullPath != nil {
				filter.RewritePath = gwFilter.URLRewrite.Path.ReplaceFullPath
			} else if gwFilter.URLRewrite.Path.Type == gatewayv1.PrefixMatchHTTPPathModifier && gwFilter.URLRewrite.Path.ReplacePrefixMatch != nil {
				filter.RewritePath = gwFilter.URLRewrite.Path.ReplacePrefixMatch
			}
		}

	default:
		return filter, fmt.Errorf("unsupported filter type: %v", gwFilter.Type)
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
