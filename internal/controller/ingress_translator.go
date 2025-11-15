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
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

const (
	// AnnotationRateLimit specifies rate limiting policy
	AnnotationRateLimit = "novaedge.io/rate-limit"
	// AnnotationCORS specifies CORS policy
	AnnotationCORS = "novaedge.io/cors"
	// AnnotationRewriteTarget specifies URL rewrite target
	AnnotationRewriteTarget = "novaedge.io/rewrite-target"
	// AnnotationLoadBalancing specifies load balancing algorithm
	AnnotationLoadBalancing = "novaedge.io/load-balancing"
	// AnnotationVIPRef specifies which VIP to use
	AnnotationVIPRef = "novaedge.io/vip-ref"

	// Default VIP reference if not specified
	DefaultVIPRef = "default-vip"
)

// IngressTranslator translates Kubernetes Ingress resources to NovaEdge CRDs
type IngressTranslator struct {
	namespace string
}

// NewIngressTranslator creates a new IngressTranslator
func NewIngressTranslator(namespace string) *IngressTranslator {
	return &IngressTranslator{
		namespace: namespace,
	}
}

// TranslationResult holds the CRDs created from an Ingress
type TranslationResult struct {
	Gateway  *novaedgev1alpha1.ProxyGateway
	Routes   []*novaedgev1alpha1.ProxyRoute
	Backends []*novaedgev1alpha1.ProxyBackend
}

// Translate converts an Ingress resource to NovaEdge CRDs
func (t *IngressTranslator) Translate(ingress *networkingv1.Ingress) (*TranslationResult, error) {
	result := &TranslationResult{
		Routes:   make([]*novaedgev1alpha1.ProxyRoute, 0),
		Backends: make([]*novaedgev1alpha1.ProxyBackend, 0),
	}

	// Create ProxyGateway from Ingress
	gateway, err := t.translateGateway(ingress)
	if err != nil {
		return nil, fmt.Errorf("failed to translate gateway: %w", err)
	}
	result.Gateway = gateway

	// Track unique backends to avoid duplicates
	backendMap := make(map[string]*novaedgev1alpha1.ProxyBackend)

	// Process each Ingress rule
	for ruleIdx, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}

		// Create ProxyRoute for this rule
		route := t.translateRoute(ingress, rule, ruleIdx)
		result.Routes = append(result.Routes, route)

		// Create ProxyBackends for each backend in the rule
		for pathIdx, path := range rule.HTTP.Paths {
			backendName := t.generateBackendName(ingress, ruleIdx, pathIdx)
			if _, exists := backendMap[backendName]; !exists {
				backend := t.translateBackend(ingress, path.Backend, backendName)
				backendMap[backendName] = backend
			}
		}
	}

	// Convert backend map to slice
	for _, backend := range backendMap {
		result.Backends = append(result.Backends, backend)
	}

	// Handle default backend if specified
	if ingress.Spec.DefaultBackend != nil {
		defaultBackendName := t.generateDefaultBackendName(ingress)
		if _, exists := backendMap[defaultBackendName]; !exists {
			backend := t.translateBackend(ingress, *ingress.Spec.DefaultBackend, defaultBackendName)
			result.Backends = append(result.Backends, backend)
		}
	}

	return result, nil
}

// translateGateway creates a ProxyGateway from an Ingress
func (t *IngressTranslator) translateGateway(ingress *networkingv1.Ingress) (*novaedgev1alpha1.ProxyGateway, error) {
	gateway := &novaedgev1alpha1.ProxyGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      t.generateGatewayName(ingress),
			Namespace: ingress.Namespace,
			Labels:    t.copyLabels(ingress),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ingress, networkingv1.SchemeGroupVersion.WithKind("Ingress")),
			},
		},
		Spec: novaedgev1alpha1.ProxyGatewaySpec{
			VIPRef:           t.getVIPRef(ingress),
			IngressClassName: t.getIngressClassName(ingress),
			Listeners:        make([]novaedgev1alpha1.Listener, 0),
		},
	}

	// Collect all unique hostnames from rules
	hostnamesMap := make(map[string]bool)
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != "" {
			hostnamesMap[rule.Host] = true
		}
	}

	hostnames := make([]string, 0, len(hostnamesMap))
	for host := range hostnamesMap {
		hostnames = append(hostnames, host)
	}

	// Create HTTP listener (port 80)
	httpListener := novaedgev1alpha1.Listener{
		Name:      "http",
		Port:      80,
		Protocol:  novaedgev1alpha1.ProtocolTypeHTTP,
		Hostnames: hostnames,
	}
	gateway.Spec.Listeners = append(gateway.Spec.Listeners, httpListener)

	// Create HTTPS listener (port 443) if TLS is configured
	if len(ingress.Spec.TLS) > 0 {
		httpsListener := t.createHTTPSListener(ingress)
		gateway.Spec.Listeners = append(gateway.Spec.Listeners, httpsListener)
	}

	return gateway, nil
}

// createHTTPSListener creates an HTTPS listener from Ingress TLS configuration
func (t *IngressTranslator) createHTTPSListener(ingress *networkingv1.Ingress) novaedgev1alpha1.Listener {
	// Collect all TLS hostnames
	tlsHostnames := make([]string, 0)
	for _, tls := range ingress.Spec.TLS {
		tlsHostnames = append(tlsHostnames, tls.Hosts...)
	}

	// Use the first TLS secret as the primary certificate
	// TODO: Support multiple certificates via SNI in future iterations
	var tlsConfig *novaedgev1alpha1.TLSConfig
	if len(ingress.Spec.TLS) > 0 && ingress.Spec.TLS[0].SecretName != "" {
		tlsConfig = &novaedgev1alpha1.TLSConfig{
			SecretRef: corev1.SecretReference{
				Name:      ingress.Spec.TLS[0].SecretName,
				Namespace: ingress.Namespace,
			},
			MinVersion: "TLS1.2",
		}
	}

	return novaedgev1alpha1.Listener{
		Name:      "https",
		Port:      443,
		Protocol:  novaedgev1alpha1.ProtocolTypeHTTPS,
		TLS:       tlsConfig,
		Hostnames: tlsHostnames,
	}
}

// translateRoute creates a ProxyRoute from an Ingress rule
func (t *IngressTranslator) translateRoute(ingress *networkingv1.Ingress, rule networkingv1.IngressRule, ruleIdx int) *novaedgev1alpha1.ProxyRoute {
	route := &novaedgev1alpha1.ProxyRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      t.generateRouteName(ingress, ruleIdx),
			Namespace: ingress.Namespace,
			Labels:    t.copyLabels(ingress),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ingress, networkingv1.SchemeGroupVersion.WithKind("Ingress")),
			},
		},
		Spec: novaedgev1alpha1.ProxyRouteSpec{
			Hostnames: []string{},
			Rules:     make([]novaedgev1alpha1.HTTPRouteRule, 0),
		},
	}

	// Set hostname if specified
	if rule.Host != "" {
		route.Spec.Hostnames = []string{rule.Host}
	}

	// Create route rules for each path
	if rule.HTTP != nil {
		for pathIdx, path := range rule.HTTP.Paths {
			routeRule := t.translateRouteRule(ingress, path, ruleIdx, pathIdx)
			route.Spec.Rules = append(route.Spec.Rules, routeRule)
		}
	}

	return route
}

// translateRouteRule creates an HTTPRouteRule from an Ingress path
func (t *IngressTranslator) translateRouteRule(ingress *networkingv1.Ingress, path networkingv1.HTTPIngressPath, ruleIdx, pathIdx int) novaedgev1alpha1.HTTPRouteRule {
	rule := novaedgev1alpha1.HTTPRouteRule{
		Matches: []novaedgev1alpha1.HTTPRouteMatch{},
		Filters: []novaedgev1alpha1.HTTPRouteFilter{},
		BackendRefs: []novaedgev1alpha1.BackendRef{
			{
				Name: t.generateBackendName(ingress, ruleIdx, pathIdx),
			},
		},
	}

	// Convert path match type
	pathMatch := t.convertPathMatch(path)
	if pathMatch != nil {
		rule.Matches = append(rule.Matches, novaedgev1alpha1.HTTPRouteMatch{
			Path: pathMatch,
		})
	}

	// Add rewrite filter if annotation present
	if rewriteTarget, exists := ingress.Annotations[AnnotationRewriteTarget]; exists {
		rule.Filters = append(rule.Filters, novaedgev1alpha1.HTTPRouteFilter{
			Type:        novaedgev1alpha1.HTTPRouteFilterURLRewrite,
			RewritePath: &rewriteTarget,
		})
	}

	return rule
}

// convertPathMatch converts Ingress path type to ProxyRoute path match
func (t *IngressTranslator) convertPathMatch(path networkingv1.HTTPIngressPath) *novaedgev1alpha1.HTTPPathMatch {
	if path.Path == "" {
		return nil
	}

	pathMatch := &novaedgev1alpha1.HTTPPathMatch{
		Value: path.Path,
	}

	// Convert path type
	if path.PathType != nil {
		switch *path.PathType {
		case networkingv1.PathTypeExact:
			pathMatch.Type = novaedgev1alpha1.PathMatchExact
		case networkingv1.PathTypePrefix:
			pathMatch.Type = novaedgev1alpha1.PathMatchPathPrefix
		case networkingv1.PathTypeImplementationSpecific:
			// Default to prefix for implementation-specific
			pathMatch.Type = novaedgev1alpha1.PathMatchPathPrefix
		}
	} else {
		// Default to prefix if not specified
		pathMatch.Type = novaedgev1alpha1.PathMatchPathPrefix
	}

	return pathMatch
}

// translateBackend creates a ProxyBackend from an Ingress backend
func (t *IngressTranslator) translateBackend(ingress *networkingv1.Ingress, backend networkingv1.IngressBackend, backendName string) *novaedgev1alpha1.ProxyBackend {
	proxyBackend := &novaedgev1alpha1.ProxyBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backendName,
			Namespace: ingress.Namespace,
			Labels:    t.copyLabels(ingress),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ingress, networkingv1.SchemeGroupVersion.WithKind("Ingress")),
			},
		},
		Spec: novaedgev1alpha1.ProxyBackendSpec{
			LBPolicy: t.getLBPolicy(ingress),
		},
	}

	// Set service reference if backend is a service
	if backend.Service != nil {
		namespace := ingress.Namespace
		proxyBackend.Spec.ServiceRef = &novaedgev1alpha1.ServiceReference{
			Name:      backend.Service.Name,
			Namespace: &namespace,
			Port:      t.getServicePort(backend.Service),
		}
	}

	return proxyBackend
}

// getServicePort extracts the port number from IngressServiceBackend
func (t *IngressTranslator) getServicePort(service *networkingv1.IngressServiceBackend) int32 {
	if service.Port.Number != 0 {
		return service.Port.Number
	}
	// If port is specified by name, we default to 80
	// The controller will need to resolve the actual port from the Service
	return 80
}

// Helper functions for name generation

func (t *IngressTranslator) generateGatewayName(ingress *networkingv1.Ingress) string {
	return fmt.Sprintf("%s-gateway", ingress.Name)
}

func (t *IngressTranslator) generateRouteName(ingress *networkingv1.Ingress, ruleIdx int) string {
	return fmt.Sprintf("%s-route-%d", ingress.Name, ruleIdx)
}

func (t *IngressTranslator) generateBackendName(ingress *networkingv1.Ingress, ruleIdx, pathIdx int) string {
	return fmt.Sprintf("%s-backend-%d-%d", ingress.Name, ruleIdx, pathIdx)
}

func (t *IngressTranslator) generateDefaultBackendName(ingress *networkingv1.Ingress) string {
	return fmt.Sprintf("%s-backend-default", ingress.Name)
}

// Helper functions for extracting configuration

func (t *IngressTranslator) getVIPRef(ingress *networkingv1.Ingress) string {
	if vipRef, exists := ingress.Annotations[AnnotationVIPRef]; exists {
		return vipRef
	}
	return DefaultVIPRef
}

func (t *IngressTranslator) getIngressClassName(ingress *networkingv1.Ingress) string {
	if ingress.Spec.IngressClassName != nil {
		return *ingress.Spec.IngressClassName
	}
	// Fallback to annotation if spec field not set
	if className, exists := ingress.Annotations["kubernetes.io/ingress.class"]; exists {
		return className
	}
	return ""
}

func (t *IngressTranslator) getLBPolicy(ingress *networkingv1.Ingress) novaedgev1alpha1.LoadBalancingPolicy {
	if lbPolicy, exists := ingress.Annotations[AnnotationLoadBalancing]; exists {
		switch strings.ToLower(lbPolicy) {
		case "roundrobin":
			return novaedgev1alpha1.LBPolicyRoundRobin
		case "p2c":
			return novaedgev1alpha1.LBPolicyP2C
		case "ewma":
			return novaedgev1alpha1.LBPolicyEWMA
		case "ringhash":
			return novaedgev1alpha1.LBPolicyRingHash
		case "maglev":
			return novaedgev1alpha1.LBPolicyMaglev
		}
	}
	// Default to RoundRobin
	return novaedgev1alpha1.LBPolicyRoundRobin
}

func (t *IngressTranslator) copyLabels(ingress *networkingv1.Ingress) map[string]string {
	labels := make(map[string]string)
	for k, v := range ingress.Labels {
		labels[k] = v
	}
	// Add tracking label
	labels["novaedge.io/ingress-name"] = ingress.Name
	labels["novaedge.io/managed-by"] = "ingress-controller"
	return labels
}
