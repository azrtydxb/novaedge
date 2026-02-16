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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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

	// AnnotationSSLRedirect forces HTTPS redirect (default: true when TLS is configured)
	AnnotationSSLRedirect = "novaedge.io/ssl-redirect"
	// AnnotationForceSSLRedirect forces HTTPS redirect even without TLS configured
	AnnotationForceSSLRedirect = "novaedge.io/force-ssl-redirect"

	// AnnotationProxyConnectTimeout sets the backend connection timeout
	AnnotationProxyConnectTimeout = "novaedge.io/proxy-connect-timeout"
	// AnnotationProxyReadTimeout sets the backend read timeout
	AnnotationProxyReadTimeout = "novaedge.io/proxy-read-timeout"
	// AnnotationProxySendTimeout sets the backend send timeout
	AnnotationProxySendTimeout = "novaedge.io/proxy-send-timeout"

	// AnnotationProxyBodySize sets the maximum allowed request body size
	AnnotationProxyBodySize = "novaedge.io/proxy-body-size"

	// AnnotationWhitelistSourceRange restricts access to specified IP ranges
	AnnotationWhitelistSourceRange = "novaedge.io/whitelist-source-range"

	// AnnotationBackendProtocol specifies backend protocol (HTTP, HTTPS, gRPC, gRPCS)
	AnnotationBackendProtocol = "novaedge.io/backend-protocol"

	// AnnotationSessionAffinity enables session affinity (cookie-based)
	AnnotationSessionAffinity = "novaedge.io/session-affinity"
	// AnnotationSessionAffinityCookieName sets the session affinity cookie name
	AnnotationSessionAffinityCookieName = "novaedge.io/session-affinity-cookie-name"
	// AnnotationSessionAffinityCookieTTL sets the session affinity cookie TTL
	AnnotationSessionAffinityCookieTTL = "novaedge.io/session-affinity-cookie-ttl"

	// AnnotationUpstreamHashBy specifies the key for consistent hashing
	AnnotationUpstreamHashBy = "novaedge.io/upstream-hash-by"

	// AnnotationRequestHeaders adds request headers (JSON map)
	AnnotationRequestHeaders = "novaedge.io/request-headers"
	// AnnotationResponseHeaders adds response headers (JSON map)
	AnnotationResponseHeaders = "novaedge.io/response-headers"
	// AnnotationRemoveRequestHeaders removes request headers (comma-separated)
	AnnotationRemoveRequestHeaders = "novaedge.io/remove-request-headers"
	// AnnotationRemoveResponseHeaders removes response headers (comma-separated)
	AnnotationRemoveResponseHeaders = "novaedge.io/remove-response-headers"

	// AnnotationCanaryWeight specifies traffic weight for canary deployments (0-100)
	AnnotationCanaryWeight = "novaedge.io/canary-weight"
	// AnnotationCanaryHeader specifies header-based canary routing
	AnnotationCanaryHeader = "novaedge.io/canary-header"
	// AnnotationCanaryHeaderValue specifies the header value for canary routing
	AnnotationCanaryHeaderValue = "novaedge.io/canary-header-value"

	// AnnotationRetryAttempts specifies the number of retry attempts
	AnnotationRetryAttempts = "novaedge.io/retry-attempts"
	// AnnotationRetryOn specifies conditions that trigger a retry
	AnnotationRetryOn = "novaedge.io/retry-on"
	// AnnotationRetryPerTryTimeout specifies the timeout for each retry
	AnnotationRetryPerTryTimeout = "novaedge.io/retry-per-try-timeout"

	// AnnotationCircuitBreakerMaxConnections sets max connections for circuit breaker
	AnnotationCircuitBreakerMaxConnections = "novaedge.io/circuit-breaker-max-connections"
	// AnnotationCircuitBreakerMaxPendingRequests sets max pending requests
	AnnotationCircuitBreakerMaxPendingRequests = "novaedge.io/circuit-breaker-max-pending-requests"
	// AnnotationCircuitBreakerMaxRequests sets max parallel requests
	AnnotationCircuitBreakerMaxRequests = "novaedge.io/circuit-breaker-max-requests"
	// AnnotationCircuitBreakerConsecutiveFailures sets failures before circuit opens
	AnnotationCircuitBreakerConsecutiveFailures = "novaedge.io/circuit-breaker-consecutive-failures"

	// AnnotationCORSAllowOrigins sets allowed CORS origins (comma-separated)
	AnnotationCORSAllowOrigins = "novaedge.io/cors-allow-origins"
	// AnnotationCORSAllowMethods sets allowed CORS methods
	AnnotationCORSAllowMethods = "novaedge.io/cors-allow-methods"
	// AnnotationCORSAllowHeaders sets allowed CORS headers
	AnnotationCORSAllowHeaders = "novaedge.io/cors-allow-headers"
	// AnnotationCORSExposeHeaders sets CORS expose headers
	AnnotationCORSExposeHeaders = "novaedge.io/cors-expose-headers"
	// AnnotationCORSMaxAge sets CORS max age
	AnnotationCORSMaxAge = "novaedge.io/cors-max-age"
	// AnnotationCORSAllowCredentials enables CORS credentials
	AnnotationCORSAllowCredentials = "novaedge.io/cors-allow-credentials" //nolint:gosec // G101: not a credential, CORS annotation key

	// AnnotationGRPCBackend marks backend as gRPC
	AnnotationGRPCBackend = "novaedge.io/grpc-backend"
	// AnnotationGRPCHealthCheck enables gRPC health check
	AnnotationGRPCHealthCheck = "novaedge.io/grpc-health-check"

	// AnnotationTracingEnabled enables request tracing
	AnnotationTracingEnabled = "novaedge.io/tracing-enabled"
	// AnnotationTracingSamplingRate sets tracing sampling rate (0-100)
	AnnotationTracingSamplingRate = "novaedge.io/tracing-sampling-rate"

	// AnnotationAccessLogEnabled enables access logging
	AnnotationAccessLogEnabled = "novaedge.io/access-log-enabled"
	// AnnotationAccessLogFormat sets access log format (json, common, combined)
	AnnotationAccessLogFormat = "novaedge.io/access-log-format"

	// AnnotationCustomErrorPages sets custom error pages (JSON format)
	AnnotationCustomErrorPages = "novaedge.io/custom-error-pages"

	// AnnotationMirrorBackend specifies backend to mirror requests to
	AnnotationMirrorBackend = "novaedge.io/mirror-backend"
	// AnnotationMirrorPercent specifies percentage of requests to mirror
	AnnotationMirrorPercent = "novaedge.io/mirror-percent"

	// AnnotationRateLimitRPS sets rate limit requests per second
	AnnotationRateLimitRPS = "novaedge.io/rate-limit-rps"
	// AnnotationRateLimitBurst sets rate limit burst size
	AnnotationRateLimitBurst = "novaedge.io/rate-limit-burst"
	// AnnotationRateLimitKey sets rate limit key (source-ip, header, etc.)
	AnnotationRateLimitKey = "novaedge.io/rate-limit-key"

	// AnnotationHealthCheckPath sets the health check path
	AnnotationHealthCheckPath = "novaedge.io/health-check-path"
	// AnnotationHealthCheckInterval sets health check interval
	AnnotationHealthCheckInterval = "novaedge.io/health-check-interval"

	// AnnotationUseRegex enables regex path matching
	AnnotationUseRegex = "novaedge.io/use-regex"

	// DefaultVIPRef is the VIP reference used when none is explicitly specified.
	DefaultVIPRef = "default-vip"

	// affinityCookie is the session affinity cookie type value
	affinityCookie = "cookie"
	// annotationValueTrue is the string "true" used in annotation comparisons
	annotationValueTrue = "true"
)

// ServicePortResolver resolves service port names to port numbers
type ServicePortResolver func(namespace, serviceName, portName string) (int32, error)

// VIPModeResolver resolves a VIP reference name to its mode (e.g. "BGP", "OSPF", "L2ARP")
type VIPModeResolver func(vipRef string) string

// IngressTranslator translates Kubernetes Ingress resources to NovaEdge CRDs
type IngressTranslator struct {
	namespace           string
	servicePortResolver ServicePortResolver
	defaultVIPRef       string
	vipModeResolver     VIPModeResolver
}

// NewIngressTranslator creates a new IngressTranslator
func NewIngressTranslator(namespace string) *IngressTranslator {
	return &IngressTranslator{
		namespace:     namespace,
		defaultVIPRef: DefaultVIPRef,
	}
}

// NewIngressTranslatorWithResolver creates a new IngressTranslator with a service port resolver
func NewIngressTranslatorWithResolver(namespace string, resolver ServicePortResolver) *IngressTranslator {
	return &IngressTranslator{
		namespace:           namespace,
		servicePortResolver: resolver,
		defaultVIPRef:       DefaultVIPRef,
	}
}

// NewIngressTranslatorWithOptions creates a new IngressTranslator with a service port resolver and configurable default VIP ref
func NewIngressTranslatorWithOptions(namespace string, resolver ServicePortResolver, defaultVIPRef string, vipModeResolver VIPModeResolver) *IngressTranslator {
	vipRef := defaultVIPRef
	if vipRef == "" {
		vipRef = DefaultVIPRef
	}
	return &IngressTranslator{
		namespace:           namespace,
		servicePortResolver: resolver,
		defaultVIPRef:       vipRef,
		vipModeResolver:     vipModeResolver,
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
	result.Gateway = t.translateGateway(ingress)

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
func (t *IngressTranslator) translateGateway(ingress *networkingv1.Ingress) *novaedgev1alpha1.ProxyGateway {
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

	// Get common listener settings from annotations
	bodySize := t.getProxyBodySize(ingress)
	allowedRanges := t.getWhitelistSourceRanges(ingress)
	sslRedirect := t.shouldSSLRedirect(ingress)

	// Create HTTP listener (port 80)
	httpListener := novaedgev1alpha1.Listener{
		Name:                "http",
		Port:                80,
		Protocol:            novaedgev1alpha1.ProtocolTypeHTTP,
		Hostnames:           hostnames,
		SSLRedirect:         sslRedirect,
		MaxRequestBodySize:  bodySize,
		AllowedSourceRanges: allowedRanges,
	}
	gateway.Spec.Listeners = append(gateway.Spec.Listeners, httpListener)

	// Create HTTPS listener (port 443) if TLS is configured
	if len(ingress.Spec.TLS) > 0 {
		httpsListener := t.createHTTPSListener(ingress)
		httpsListener.MaxRequestBodySize = bodySize
		httpsListener.AllowedSourceRanges = allowedRanges
		gateway.Spec.Listeners = append(gateway.Spec.Listeners, httpsListener)
	}

	// Apply tracing configuration
	if tracing := t.getTracingConfig(ingress); tracing != nil {
		gateway.Spec.Tracing = tracing
	}

	// Apply access log configuration
	if accessLog := t.getAccessLogConfig(ingress); accessLog != nil {
		gateway.Spec.AccessLog = accessLog
	}

	// Apply custom error pages
	if errorPages := t.getCustomErrorPages(ingress); len(errorPages) > 0 {
		gateway.Spec.CustomErrorPages = errorPages
	}

	return gateway
}

// createHTTPSListener creates an HTTPS listener from Ingress TLS configuration
// Supports multiple TLS certificates via SNI when multiple TLS entries are present
func (t *IngressTranslator) createHTTPSListener(ingress *networkingv1.Ingress) novaedgev1alpha1.Listener {
	// Collect all TLS hostnames
	totalHosts := 0
	for _, tls := range ingress.Spec.TLS {
		totalHosts += len(tls.Hosts)
	}
	tlsHostnames := make([]string, 0, totalHosts)
	for _, tls := range ingress.Spec.TLS {
		tlsHostnames = append(tlsHostnames, tls.Hosts...)
	}

	listener := novaedgev1alpha1.Listener{
		Name:      "https",
		Port:      443,
		Protocol:  novaedgev1alpha1.ProtocolTypeHTTPS,
		Hostnames: tlsHostnames,
	}

	// Always use SNI with TLSCertificates map for full multi-certificate support
	// This allows the agent to select the correct certificate based on SNI hostname
	tlsCertificates := make(map[string]novaedgev1alpha1.TLSConfig)
	hasAnyCertificate := false

	for _, tls := range ingress.Spec.TLS {
		if tls.SecretName == "" {
			continue
		}

		// Map each host to its corresponding TLS certificate secret
		for _, host := range tls.Hosts {
			tlsCertificates[host] = novaedgev1alpha1.TLSConfig{
				SecretRef: &corev1.SecretReference{
					Name:      tls.SecretName,
					Namespace: ingress.Namespace,
				},
				MinVersion: "TLS1.3", // Use TLS 1.3 for better security
			}
			hasAnyCertificate = true
		}

		// If no specific hosts are defined, use wildcard "*" for this certificate
		if len(tls.Hosts) == 0 && tls.SecretName != "" {
			tlsCertificates["*"] = novaedgev1alpha1.TLSConfig{
				SecretRef: &corev1.SecretReference{
					Name:      tls.SecretName,
					Namespace: ingress.Namespace,
				},
				MinVersion: "TLS1.3",
			}
			hasAnyCertificate = true
		}
	}

	// Set TLSCertificates map for SNI support
	if hasAnyCertificate {
		listener.TLSCertificates = tlsCertificates
	}

	// For backward compatibility, also set the TLS field with the first certificate
	// This serves as a default fallback for older agent versions
	if len(ingress.Spec.TLS) > 0 && ingress.Spec.TLS[0].SecretName != "" {
		listener.TLS = &novaedgev1alpha1.TLSConfig{
			SecretRef: &corev1.SecretReference{
				Name:      ingress.Spec.TLS[0].SecretName,
				Namespace: ingress.Namespace,
			},
			MinVersion: "TLS1.3",
		}
	}

	return listener
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
	// Set canary weight if specified
	weight := t.getCanaryWeight(ingress)
	var weightPtr *int32
	if weight > 0 {
		weightPtr = &weight
	}

	rule := novaedgev1alpha1.HTTPRouteRule{
		Matches: []novaedgev1alpha1.HTTPRouteMatch{},
		Filters: []novaedgev1alpha1.HTTPRouteFilter{},
		BackendRefs: []novaedgev1alpha1.BackendRef{
			{
				Name:   t.generateBackendName(ingress, ruleIdx, pathIdx),
				Weight: weightPtr,
			},
		},
	}

	// Convert path match type (with regex support)
	pathMatch := t.convertPathMatchWithIngress(ingress, path)
	if pathMatch != nil {
		match := novaedgev1alpha1.HTTPRouteMatch{
			Path: pathMatch,
		}

		// Add canary header match if specified
		canaryHeader, canaryValue := t.getCanaryHeader(ingress)
		if canaryHeader != "" {
			match.Headers = []novaedgev1alpha1.HTTPHeaderMatch{
				{
					Name:  canaryHeader,
					Value: canaryValue,
				},
			}
		}

		rule.Matches = append(rule.Matches, match)
	}

	// Add rewrite filter if annotation present
	if rewriteTarget, exists := ingress.Annotations[AnnotationRewriteTarget]; exists {
		rule.Filters = append(rule.Filters, novaedgev1alpha1.HTTPRouteFilter{
			Type:        novaedgev1alpha1.HTTPRouteFilterURLRewrite,
			RewritePath: &rewriteTarget,
		})
	}

	// Add request header filters
	requestHeaders := t.getRequestHeaders(ingress)
	if len(requestHeaders) > 0 {
		addHeaders := make([]novaedgev1alpha1.HTTPHeader, 0, len(requestHeaders))
		for name, value := range requestHeaders {
			addHeaders = append(addHeaders, novaedgev1alpha1.HTTPHeader{
				Name:  name,
				Value: value,
			})
		}
		rule.Filters = append(rule.Filters, novaedgev1alpha1.HTTPRouteFilter{
			Type: novaedgev1alpha1.HTTPRouteFilterAddHeader,
			Add:  addHeaders,
		})
	}

	// Add remove request header filters
	removeHeaders := t.getRemoveRequestHeaders(ingress)
	if len(removeHeaders) > 0 {
		rule.Filters = append(rule.Filters, novaedgev1alpha1.HTTPRouteFilter{
			Type:   novaedgev1alpha1.HTTPRouteFilterRemoveHeader,
			Remove: removeHeaders,
		})
	}

	// Add request mirror filter if configured
	if mirrorBackend, mirrorPercent := t.getMirrorConfig(ingress); mirrorBackend != nil {
		rule.Filters = append(rule.Filters, novaedgev1alpha1.HTTPRouteFilter{
			Type:          novaedgev1alpha1.HTTPRouteFilterRequestMirror,
			MirrorBackend: mirrorBackend,
			MirrorPercent: mirrorPercent,
		})
	}

	return rule
}

// convertPathMatchWithIngress converts path with regex support from annotation
func (t *IngressTranslator) convertPathMatchWithIngress(ingress *networkingv1.Ingress, path networkingv1.HTTPIngressPath) *novaedgev1alpha1.HTTPPathMatch {
	if path.Path == "" {
		return nil
	}

	pathMatch := &novaedgev1alpha1.HTTPPathMatch{
		Value: path.Path,
	}

	// Check if regex path matching is enabled
	if t.useRegexPathMatching(ingress) {
		pathMatch.Type = novaedgev1alpha1.PathMatchRegularExpression
		return pathMatch
	}

	// Convert path type
	if path.PathType != nil {
		switch *path.PathType {
		case networkingv1.PathTypeExact:
			pathMatch.Type = novaedgev1alpha1.PathMatchExact
		case networkingv1.PathTypePrefix:
			pathMatch.Type = novaedgev1alpha1.PathMatchPathPrefix
		case networkingv1.PathTypeImplementationSpecific:
			// For implementation-specific, check if path looks like regex
			if strings.ContainsAny(path.Path, "^$.*+?[](){}|\\") {
				pathMatch.Type = novaedgev1alpha1.PathMatchRegularExpression
			} else {
				pathMatch.Type = novaedgev1alpha1.PathMatchPathPrefix
			}
		}
	} else {
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
			Port:      t.getServicePort(ingress, backend.Service),
		}
	}

	// Apply timeout annotations
	t.applyTimeoutAnnotations(ingress, proxyBackend)

	// Apply backend protocol annotation
	t.applyBackendProtocol(ingress, proxyBackend)

	// Apply session affinity annotation
	t.applySessionAffinity(ingress, proxyBackend)

	// Apply upstream hash annotation (for consistent hashing)
	t.applyUpstreamHash(ingress, proxyBackend)

	// Apply retry policy
	t.applyRetryPolicy(ingress, proxyBackend)

	// Apply circuit breaker
	t.applyCircuitBreaker(ingress, proxyBackend)

	// Apply session affinity with cookie config
	t.applySessionAffinityConfig(ingress, proxyBackend)

	// Apply health check configuration
	t.applyHealthCheck(ingress, proxyBackend)

	// Apply gRPC backend configuration
	t.applyGRPCBackend(ingress, proxyBackend)

	return proxyBackend
}

// getServicePort extracts the port number from IngressServiceBackend
func (t *IngressTranslator) getServicePort(ingress *networkingv1.Ingress, service *networkingv1.IngressServiceBackend) int32 {
	if service.Port.Number != 0 {
		return service.Port.Number
	}
	// If port is specified by name, try to resolve it
	if service.Port.Name != "" && t.servicePortResolver != nil {
		port, err := t.servicePortResolver(ingress.Namespace, service.Name, service.Port.Name)
		if err == nil {
			return port
		}
		// Fall through to default if resolution fails
	}
	// Default to 80 if we can't resolve
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
	return t.defaultVIPRef
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
	// Explicit annotation always wins
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
	// Auto-detect from VIP mode: BGP/OSPF require hash-based LB for ECMP
	if t.vipModeResolver != nil {
		vipRef := t.getVIPRef(ingress)
		mode := t.vipModeResolver(vipRef)
		if mode == string(novaedgev1alpha1.VIPModeBGP) || mode == string(novaedgev1alpha1.VIPModeOSPF) {
			return novaedgev1alpha1.LBPolicyMaglev
		}
	}
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

// applyTimeoutAnnotations applies timeout annotations to the backend
func (t *IngressTranslator) applyTimeoutAnnotations(ingress *networkingv1.Ingress, backend *novaedgev1alpha1.ProxyBackend) {
	// Connect timeout
	if timeout, exists := ingress.Annotations[AnnotationProxyConnectTimeout]; exists {
		if duration, err := time.ParseDuration(timeout); err == nil {
			backend.Spec.ConnectTimeout = metav1.Duration{Duration: duration}
		}
	}

	// Read timeout (maps to IdleTimeout in our model)
	if timeout, exists := ingress.Annotations[AnnotationProxyReadTimeout]; exists {
		if duration, err := time.ParseDuration(timeout); err == nil {
			backend.Spec.IdleTimeout = metav1.Duration{Duration: duration}
		}
	}

	// Send timeout - we don't have a separate field, but can use IdleTimeout
	// if read timeout wasn't specified
	if _, hasRead := ingress.Annotations[AnnotationProxyReadTimeout]; !hasRead {
		if timeout, exists := ingress.Annotations[AnnotationProxySendTimeout]; exists {
			if duration, err := time.ParseDuration(timeout); err == nil {
				backend.Spec.IdleTimeout = metav1.Duration{Duration: duration}
			}
		}
	}
}

// applyBackendProtocol applies backend protocol annotation
func (t *IngressTranslator) applyBackendProtocol(ingress *networkingv1.Ingress, backend *novaedgev1alpha1.ProxyBackend) {
	protocol, exists := ingress.Annotations[AnnotationBackendProtocol]
	if !exists {
		return
	}

	switch strings.ToUpper(protocol) {
	case "HTTPS", "GRPCS":
		// Enable TLS for backend connections
		backend.Spec.TLS = &novaedgev1alpha1.BackendTLSConfig{
			Enabled: true,
		}
	case "HTTP", "GRPC":
		// No TLS needed
	}
}

// applySessionAffinity applies session affinity annotations
func (t *IngressTranslator) applySessionAffinity(ingress *networkingv1.Ingress, backend *novaedgev1alpha1.ProxyBackend) {
	affinity, exists := ingress.Annotations[AnnotationSessionAffinity]
	if !exists || strings.ToLower(affinity) != affinityCookie {
		return
	}

	// When session affinity is enabled, switch to RingHash for consistent hashing
	// The actual cookie handling is done at the route level
	backend.Spec.LBPolicy = novaedgev1alpha1.LBPolicyRingHash
}

// applyUpstreamHash applies upstream hash annotation for consistent hashing
func (t *IngressTranslator) applyUpstreamHash(ingress *networkingv1.Ingress, backend *novaedgev1alpha1.ProxyBackend) {
	hashBy, exists := ingress.Annotations[AnnotationUpstreamHashBy]
	if !exists {
		return
	}

	// When upstream hash is specified, use appropriate hashing algorithm
	switch strings.ToLower(hashBy) {
	case "$request_uri", "$uri", "uri":
		backend.Spec.LBPolicy = novaedgev1alpha1.LBPolicyRingHash
	case "$remote_addr", "$binary_remote_addr", "ip":
		backend.Spec.LBPolicy = novaedgev1alpha1.LBPolicyRingHash
	case "header", affinityCookie:
		backend.Spec.LBPolicy = novaedgev1alpha1.LBPolicyRingHash
	default:
		// Use Maglev for other consistent hashing needs
		backend.Spec.LBPolicy = novaedgev1alpha1.LBPolicyMaglev
	}
}

// shouldSSLRedirect checks if SSL redirect should be enabled
func (t *IngressTranslator) shouldSSLRedirect(ingress *networkingv1.Ingress) bool {
	// Check force-ssl-redirect annotation (always redirects)
	if forceRedirect, exists := ingress.Annotations[AnnotationForceSSLRedirect]; exists {
		return strings.ToLower(forceRedirect) == annotationValueTrue
	}

	// Check ssl-redirect annotation
	if sslRedirect, exists := ingress.Annotations[AnnotationSSLRedirect]; exists {
		return strings.ToLower(sslRedirect) == annotationValueTrue
	}

	// Default: redirect if TLS is configured
	return len(ingress.Spec.TLS) > 0
}

// getWhitelistSourceRanges parses the whitelist source range annotation
func (t *IngressTranslator) getWhitelistSourceRanges(ingress *networkingv1.Ingress) []string {
	ranges, exists := ingress.Annotations[AnnotationWhitelistSourceRange]
	if !exists || ranges == "" {
		return nil
	}

	// Split by comma and trim whitespace
	parts := strings.Split(ranges, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// getProxyBodySize parses the proxy body size annotation
func (t *IngressTranslator) getProxyBodySize(ingress *networkingv1.Ingress) int64 {
	sizeStr, exists := ingress.Annotations[AnnotationProxyBodySize]
	if !exists || sizeStr == "" {
		return 0 // 0 means unlimited
	}

	// Parse size with unit suffix (e.g., "10m", "1g", "500k")
	sizeStr = strings.ToLower(strings.TrimSpace(sizeStr))
	multiplier := int64(1)

	switch {
	case strings.HasSuffix(sizeStr, "k"):
		multiplier = 1024
		sizeStr = strings.TrimSuffix(sizeStr, "k")
	case strings.HasSuffix(sizeStr, "m"):
		multiplier = 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "m")
	case strings.HasSuffix(sizeStr, "g"):
		multiplier = 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "g")
	}

	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return 0
	}
	return size * multiplier
}

// getRequestHeaders parses request headers annotation (JSON map)
func (t *IngressTranslator) getRequestHeaders(ingress *networkingv1.Ingress) map[string]string {
	headersJSON, exists := ingress.Annotations[AnnotationRequestHeaders]
	if !exists || headersJSON == "" {
		return nil
	}

	var headers map[string]string
	if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
		return nil
	}
	return headers
}

// getRemoveRequestHeaders parses remove request headers annotation (comma-separated)
func (t *IngressTranslator) getRemoveRequestHeaders(ingress *networkingv1.Ingress) []string {
	headers, exists := ingress.Annotations[AnnotationRemoveRequestHeaders]
	if !exists || headers == "" {
		return nil
	}

	parts := strings.Split(headers, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// getCanaryWeight parses the canary weight annotation
func (t *IngressTranslator) getCanaryWeight(ingress *networkingv1.Ingress) int32 {
	weightStr, exists := ingress.Annotations[AnnotationCanaryWeight]
	if !exists || weightStr == "" {
		return 0
	}

	weight, err := strconv.ParseInt(weightStr, 10, 32)
	if err != nil || weight < 0 || weight > 100 {
		return 0
	}
	return int32(weight)
}

// getCanaryHeader returns the canary header configuration
func (t *IngressTranslator) getCanaryHeader(ingress *networkingv1.Ingress) (string, string) {
	header, exists := ingress.Annotations[AnnotationCanaryHeader]
	if !exists || header == "" {
		return "", ""
	}

	value := ingress.Annotations[AnnotationCanaryHeaderValue]
	if value == "" {
		value = annotationValueTrue // Default value if not specified
	}

	return header, value
}

// applyRetryPolicy applies retry annotations to the backend
func (t *IngressTranslator) applyRetryPolicy(ingress *networkingv1.Ingress, backend *novaedgev1alpha1.ProxyBackend) {
	attemptsStr, hasAttempts := ingress.Annotations[AnnotationRetryAttempts]
	retryOn := ingress.Annotations[AnnotationRetryOn]
	perTryTimeoutStr := ingress.Annotations[AnnotationRetryPerTryTimeout]

	if !hasAttempts && retryOn == "" && perTryTimeoutStr == "" {
		return
	}

	policy := &novaedgev1alpha1.RetryPolicy{}

	if hasAttempts {
		if attempts, err := strconv.ParseInt(attemptsStr, 10, 32); err == nil && attempts >= 0 && attempts <= 10 {
			numRetries := int32(attempts)
			policy.NumRetries = &numRetries
		}
	}

	if retryOn != "" {
		policy.RetryOn = retryOn
	}

	if perTryTimeoutStr != "" {
		if duration, err := time.ParseDuration(perTryTimeoutStr); err == nil {
			policy.PerTryTimeout = metav1.Duration{Duration: duration}
		}
	}

	backend.Spec.RetryPolicy = policy
}

// applyCircuitBreaker applies circuit breaker annotations to the backend
func (t *IngressTranslator) applyCircuitBreaker(ingress *networkingv1.Ingress, backend *novaedgev1alpha1.ProxyBackend) {
	maxConnsStr := ingress.Annotations[AnnotationCircuitBreakerMaxConnections]
	maxPendingStr := ingress.Annotations[AnnotationCircuitBreakerMaxPendingRequests]
	maxRequestsStr := ingress.Annotations[AnnotationCircuitBreakerMaxRequests]
	consecutiveFailuresStr := ingress.Annotations[AnnotationCircuitBreakerConsecutiveFailures]

	if maxConnsStr == "" && maxPendingStr == "" && maxRequestsStr == "" && consecutiveFailuresStr == "" {
		return
	}

	cb := &novaedgev1alpha1.CircuitBreaker{}

	if maxConnsStr != "" {
		if val, err := strconv.ParseInt(maxConnsStr, 10, 32); err == nil && val > 0 {
			v := int32(val)
			cb.MaxConnections = &v
		}
	}

	if maxPendingStr != "" {
		if val, err := strconv.ParseInt(maxPendingStr, 10, 32); err == nil && val > 0 {
			v := int32(val)
			cb.MaxPendingRequests = &v
		}
	}

	if maxRequestsStr != "" {
		if val, err := strconv.ParseInt(maxRequestsStr, 10, 32); err == nil && val > 0 {
			v := int32(val)
			cb.MaxRequests = &v
		}
	}

	if consecutiveFailuresStr != "" {
		if val, err := strconv.ParseInt(consecutiveFailuresStr, 10, 32); err == nil && val > 0 {
			v := int32(val)
			cb.ConsecutiveFailures = &v
		}
	}

	backend.Spec.CircuitBreaker = cb
}

// applySessionAffinityConfig applies session affinity cookie configuration
func (t *IngressTranslator) applySessionAffinityConfig(ingress *networkingv1.Ingress, backend *novaedgev1alpha1.ProxyBackend) {
	affinity, exists := ingress.Annotations[AnnotationSessionAffinity]
	if !exists || strings.ToLower(affinity) != affinityCookie {
		return
	}

	config := &novaedgev1alpha1.SessionAffinityConfig{
		Type: "Cookie",
	}

	if cookieName := ingress.Annotations[AnnotationSessionAffinityCookieName]; cookieName != "" {
		config.CookieName = cookieName
	}

	if ttlStr := ingress.Annotations[AnnotationSessionAffinityCookieTTL]; ttlStr != "" {
		if duration, err := time.ParseDuration(ttlStr); err == nil {
			config.CookieTTL = metav1.Duration{Duration: duration}
		}
	}

	backend.Spec.SessionAffinity = config
	// Also ensure consistent hashing is used
	backend.Spec.LBPolicy = novaedgev1alpha1.LBPolicyRingHash
}

// applyHealthCheck applies health check annotations to the backend
func (t *IngressTranslator) applyHealthCheck(ingress *networkingv1.Ingress, backend *novaedgev1alpha1.ProxyBackend) {
	healthPath := ingress.Annotations[AnnotationHealthCheckPath]
	intervalStr := ingress.Annotations[AnnotationHealthCheckInterval]
	isGRPC := strings.ToLower(ingress.Annotations[AnnotationGRPCHealthCheck]) == annotationValueTrue

	if healthPath == "" && intervalStr == "" && !isGRPC {
		return
	}

	hc := &novaedgev1alpha1.HealthCheck{}

	if healthPath != "" {
		hc.HTTPPath = &healthPath
	}

	if intervalStr != "" {
		if duration, err := time.ParseDuration(intervalStr); err == nil {
			hc.Interval = metav1.Duration{Duration: duration}
		}
	}

	backend.Spec.HealthCheck = hc
}

// applyGRPCBackend applies gRPC-specific configuration
func (t *IngressTranslator) applyGRPCBackend(ingress *networkingv1.Ingress, backend *novaedgev1alpha1.ProxyBackend) {
	isGRPC := strings.ToLower(ingress.Annotations[AnnotationGRPCBackend]) == annotationValueTrue
	protocol := strings.ToUpper(ingress.Annotations[AnnotationBackendProtocol])

	switch {
	case isGRPC || protocol == "GRPC":
		backend.Spec.Protocol = "gRPC"
	case protocol == "GRPCS":
		backend.Spec.Protocol = "gRPCS"
		if backend.Spec.TLS == nil {
			backend.Spec.TLS = &novaedgev1alpha1.BackendTLSConfig{Enabled: true}
		}
	case protocol == "HTTP2":
		backend.Spec.Protocol = "HTTP2"
	}
}

// getTracingConfig returns tracing configuration from annotations
func (t *IngressTranslator) getTracingConfig(ingress *networkingv1.Ingress) *novaedgev1alpha1.TracingConfig {
	enabledStr := ingress.Annotations[AnnotationTracingEnabled]
	samplingRateStr := ingress.Annotations[AnnotationTracingSamplingRate]

	// Default enabled if any tracing annotation is present
	if enabledStr == "" && samplingRateStr == "" {
		return nil
	}

	config := &novaedgev1alpha1.TracingConfig{
		Enabled:            true,
		GenerateRequestID:  true,
		PropagateRequestID: true,
		RequestIDHeader:    "X-Request-ID",
	}

	if enabledStr != "" {
		config.Enabled = strings.ToLower(enabledStr) == annotationValueTrue
	}

	if samplingRateStr != "" {
		if rate, err := strconv.ParseInt(samplingRateStr, 10, 32); err == nil && rate >= 0 && rate <= 100 {
			r := int32(rate)
			config.SamplingRate = &r
		}
	}

	return config
}

// getAccessLogConfig returns access log configuration from annotations
func (t *IngressTranslator) getAccessLogConfig(ingress *networkingv1.Ingress) *novaedgev1alpha1.AccessLogConfig {
	enabledStr := ingress.Annotations[AnnotationAccessLogEnabled]
	format := ingress.Annotations[AnnotationAccessLogFormat]

	if enabledStr == "" && format == "" {
		return nil
	}

	config := &novaedgev1alpha1.AccessLogConfig{
		Enabled: true,
		Format:  "json",
	}

	if enabledStr != "" {
		config.Enabled = strings.ToLower(enabledStr) == annotationValueTrue
	}

	if format != "" {
		config.Format = format
	}

	return config
}

// getCustomErrorPages parses custom error pages from annotation
func (t *IngressTranslator) getCustomErrorPages(ingress *networkingv1.Ingress) []novaedgev1alpha1.CustomErrorPage {
	pagesJSON := ingress.Annotations[AnnotationCustomErrorPages]
	if pagesJSON == "" {
		return nil
	}

	var pages []novaedgev1alpha1.CustomErrorPage
	if err := json.Unmarshal([]byte(pagesJSON), &pages); err != nil {
		return nil
	}
	return pages
}

// getMirrorConfig returns mirror backend configuration from annotations
func (t *IngressTranslator) getMirrorConfig(ingress *networkingv1.Ingress) (*novaedgev1alpha1.BackendRef, *int32) {
	mirrorBackend := ingress.Annotations[AnnotationMirrorBackend]
	if mirrorBackend == "" {
		return nil, nil
	}

	backend := &novaedgev1alpha1.BackendRef{
		Name: mirrorBackend,
	}

	var percent *int32
	if percentStr := ingress.Annotations[AnnotationMirrorPercent]; percentStr != "" {
		if p, err := strconv.ParseInt(percentStr, 10, 32); err == nil && p >= 0 && p <= 100 {
			pVal := int32(p)
			percent = &pVal
		}
	}

	return backend, percent
}

// useRegexPathMatching checks if regex path matching should be used
func (t *IngressTranslator) useRegexPathMatching(ingress *networkingv1.Ingress) bool {
	return strings.ToLower(ingress.Annotations[AnnotationUseRegex]) == annotationValueTrue
}
