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

package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Builder builds ConfigSnapshots from Kubernetes resources
type Builder struct {
	client client.Client
}

// NewBuilder creates a new snapshot builder
func NewBuilder(client client.Client) *Builder {
	return &Builder{
		client: client,
	}
}

// BuildSnapshot builds a complete ConfigSnapshot for a specific node
func (b *Builder) BuildSnapshot(ctx context.Context, nodeName string) (*pb.ConfigSnapshot, error) {
	logger := log.FromContext(ctx).WithValues("node", nodeName)
	logger.Info("Building config snapshot")

	startTime := time.Now()
	snapshot := &pb.ConfigSnapshot{
		GenerationTime: time.Now().Unix(),
	}

	// Build VIP assignments
	vips, err := b.buildVIPAssignments(ctx, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to build VIP assignments: %w", err)
	}
	snapshot.VipAssignments = vips

	// Build gateways
	gateways, err := b.buildGateways(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build gateways: %w", err)
	}
	snapshot.Gateways = gateways

	// Build routes
	routes, err := b.buildRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build routes: %w", err)
	}
	snapshot.Routes = routes

	// Build backends/clusters
	clusters, endpoints, err := b.buildClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build clusters: %w", err)
	}
	snapshot.Clusters = clusters
	snapshot.Endpoints = endpoints

	// Build policies
	policies, err := b.buildPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build policies: %w", err)
	}
	snapshot.Policies = policies

	// Generate version based on content hash
	snapshot.Version = b.generateVersion(snapshot)

	// Record metrics
	duration := time.Since(startTime).Seconds()
	sizeBytes := proto.Size(snapshot)
	resourceCounts := map[string]int{
		"gateways": len(snapshot.Gateways),
		"routes":   len(snapshot.Routes),
		"clusters": len(snapshot.Clusters),
		"vips":     len(snapshot.VipAssignments),
		"policies": len(snapshot.Policies),
	}
	RecordSnapshotBuild(nodeName, duration, sizeBytes, resourceCounts)

	logger.Info("Config snapshot built successfully",
		"version", snapshot.Version,
		"gateways", len(snapshot.Gateways),
		"routes", len(snapshot.Routes),
		"clusters", len(snapshot.Clusters),
		"vips", len(snapshot.VipAssignments),
		"policies", len(snapshot.Policies),
		"duration_ms", duration*1000,
		"size_bytes", sizeBytes)

	return snapshot, nil
}

// buildVIPAssignments builds VIP assignments for the node
func (b *Builder) buildVIPAssignments(ctx context.Context, nodeName string) ([]*pb.VIPAssignment, error) {
	vipList := &novaedgev1alpha1.ProxyVIPList{}
	if err := b.client.List(ctx, vipList); err != nil {
		return nil, err
	}

	var assignments []*pb.VIPAssignment
	for _, vip := range vipList.Items {
		// Check if this node should handle this VIP
		isActive := false

		switch vip.Spec.Mode {
		case novaedgev1alpha1.VIPModeL2ARP:
			// For L2ARP mode: only active if this node is the elected active node
			isActive = vip.Status.ActiveNode == nodeName

		case novaedgev1alpha1.VIPModeBGP, novaedgev1alpha1.VIPModeOSPF:
			// For BGP/OSPF mode: active if this node is in the announcing nodes list
			for _, announcingNode := range vip.Status.AnnouncingNodes {
				if announcingNode == nodeName {
					isActive = true
					break
				}
			}
		}

		// Only include assignment if this node should handle the VIP
		// (either as active node or as announcing node)
		if isActive {
			assignment := &pb.VIPAssignment{
				VipName:  vip.Name,
				Address:  vip.Spec.Address,
				Mode:     convertVIPMode(vip.Spec.Mode),
				Ports:    vip.Spec.Ports,
				IsActive: true,
			}

			// Add BGP config for BGP mode VIPs
			if vip.Spec.Mode == novaedgev1alpha1.VIPModeBGP && vip.Spec.BGPConfig != nil {
				assignment.BgpConfig = convertBGPConfig(vip.Spec.BGPConfig)
			}

			assignments = append(assignments, assignment)
		}
	}

	return assignments, nil
}

// buildGateways builds gateway configurations
func (b *Builder) buildGateways(ctx context.Context) ([]*pb.Gateway, error) {
	gatewayList := &novaedgev1alpha1.ProxyGatewayList{}
	if err := b.client.List(ctx, gatewayList); err != nil {
		return nil, err
	}

	var gateways []*pb.Gateway
	for _, gw := range gatewayList.Items {
		gateway := &pb.Gateway{
			Name:             gw.Name,
			Namespace:        gw.Namespace,
			VipRef:           gw.Spec.VIPRef,
			IngressClassName: gw.Spec.IngressClassName,
			Listeners:        make([]*pb.Listener, 0, len(gw.Spec.Listeners)),
		}

		for _, listener := range gw.Spec.Listeners {
			pbListener := &pb.Listener{
				Name:      listener.Name,
				Port:      listener.Port,
				Protocol:  convertProtocol(listener.Protocol),
				Hostnames: listener.Hostnames,
			}

			// Load TLS configuration if present
			if listener.TLS != nil {
				tlsConfig, err := b.loadTLSConfig(ctx, listener.TLS, gw.Namespace)
				if err != nil {
					log.FromContext(ctx).Error(err, "Failed to load TLS config", "listener", listener.Name)
					continue
				}
				pbListener.Tls = tlsConfig
			}

			gateway.Listeners = append(gateway.Listeners, pbListener)
		}

		gateways = append(gateways, gateway)
	}

	return gateways, nil
}

// buildRoutes builds route configurations
func (b *Builder) buildRoutes(ctx context.Context) ([]*pb.Route, error) {
	routeList := &novaedgev1alpha1.ProxyRouteList{}
	if err := b.client.List(ctx, routeList); err != nil {
		return nil, err
	}

	var routes []*pb.Route
	for _, r := range routeList.Items {
		route := &pb.Route{
			Name:      r.Name,
			Namespace: r.Namespace,
			Hostnames: r.Spec.Hostnames,
			Rules:     make([]*pb.RouteRule, 0, len(r.Spec.Rules)),
		}

		for _, rule := range r.Spec.Rules {
			pbRule := &pb.RouteRule{
				Matches: convertMatches(rule.Matches),
				Filters: convertFilters(rule.Filters),
				BackendRef: &pb.BackendRef{
					Name:      rule.BackendRef.Name,
					Namespace: getNamespace(rule.BackendRef.Namespace, r.Namespace),
					Weight:    getWeight(rule.BackendRef.Weight),
				},
			}
			route.Rules = append(route.Rules, pbRule)
		}

		routes = append(routes, route)
	}

	return routes, nil
}

// buildClusters builds backend cluster configurations and their endpoints
func (b *Builder) buildClusters(ctx context.Context) ([]*pb.Cluster, map[string]*pb.EndpointList, error) {
	backendList := &novaedgev1alpha1.ProxyBackendList{}
	if err := b.client.List(ctx, backendList); err != nil {
		return nil, nil, err
	}

	var clusters []*pb.Cluster
	endpoints := make(map[string]*pb.EndpointList)

	for _, backend := range backendList.Items {
		cluster := &pb.Cluster{
			Name:             backend.Name,
			Namespace:        backend.Namespace,
			LbPolicy:         convertLBPolicy(backend.Spec.LBPolicy),
			ConnectTimeoutMs: durationToMillis(backend.Spec.ConnectTimeout),
			IdleTimeoutMs:    durationToMillis(backend.Spec.IdleTimeout),
		}

		if backend.Spec.CircuitBreaker != nil {
			cluster.CircuitBreaker = convertCircuitBreaker(backend.Spec.CircuitBreaker)
		}

		if backend.Spec.HealthCheck != nil {
			cluster.HealthCheck = convertHealthCheck(backend.Spec.HealthCheck)
		}

		if backend.Spec.TLS != nil {
			cluster.Tls = &pb.BackendTLS{
				Enabled:            backend.Spec.TLS.Enabled,
				InsecureSkipVerify: backend.Spec.TLS.InsecureSkipVerify,
			}
			// TODO: Load CA cert from secret if specified
		}

		clusters = append(clusters, cluster)

		// Resolve endpoints for this backend
		if backend.Spec.ServiceRef != nil {
			endpointList, err := b.resolveServiceEndpoints(ctx, backend.Spec.ServiceRef, backend.Namespace)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to resolve endpoints", "backend", backend.Name)
				continue
			}
			clusterKey := fmt.Sprintf("%s/%s", backend.Namespace, backend.Name)
			endpoints[clusterKey] = endpointList
		}
	}

	return clusters, endpoints, nil
}

// buildPolicies builds policy configurations
func (b *Builder) buildPolicies(ctx context.Context) ([]*pb.Policy, error) {
	policyList := &novaedgev1alpha1.ProxyPolicyList{}
	if err := b.client.List(ctx, policyList); err != nil {
		return nil, err
	}

	var policies []*pb.Policy
	for _, p := range policyList.Items {
		policy := &pb.Policy{
			Name:      p.Name,
			Namespace: p.Namespace,
			Type:      convertPolicyType(p.Spec.Type),
			TargetRef: &pb.TargetRef{
				Kind:      p.Spec.TargetRef.Kind,
				Name:      p.Spec.TargetRef.Name,
				Namespace: getNamespace(p.Spec.TargetRef.Namespace, p.Namespace),
			},
		}

		// Add policy-specific configuration
		if p.Spec.RateLimit != nil {
			policy.RateLimit = &pb.RateLimitConfig{
				RequestsPerSecond: p.Spec.RateLimit.RequestsPerSecond,
				Burst:             getInt32(p.Spec.RateLimit.Burst),
				Key:               p.Spec.RateLimit.Key,
			}
		}

		if p.Spec.JWT != nil {
			policy.Jwt = &pb.JWTConfig{
				Issuer:       p.Spec.JWT.Issuer,
				Audience:     p.Spec.JWT.Audience,
				JwksUri:      p.Spec.JWT.JWKSUri,
				HeaderName:   p.Spec.JWT.HeaderName,
				HeaderPrefix: p.Spec.JWT.HeaderPrefix,
			}
		}

		if p.Spec.IPList != nil {
			policy.IpList = &pb.IPListConfig{
				Cidrs:        p.Spec.IPList.CIDRs,
				SourceHeader: getString(p.Spec.IPList.SourceHeader),
			}
		}

		if p.Spec.CORS != nil {
			policy.Cors = &pb.CORSConfig{
				AllowOrigins:     p.Spec.CORS.AllowOrigins,
				AllowMethods:     p.Spec.CORS.AllowMethods,
				AllowHeaders:     p.Spec.CORS.AllowHeaders,
				ExposeHeaders:    p.Spec.CORS.ExposeHeaders,
				MaxAgeSeconds:    durationToSeconds(p.Spec.CORS.MaxAge),
				AllowCredentials: p.Spec.CORS.AllowCredentials,
			}
		}

		policies = append(policies, policy)
	}

	return policies, nil
}

// resolveServiceEndpoints resolves endpoints from a ServiceReference
func (b *Builder) resolveServiceEndpoints(ctx context.Context, serviceRef *novaedgev1alpha1.ServiceReference, defaultNamespace string) (*pb.EndpointList, error) {
	namespace := getNamespace(serviceRef.Namespace, defaultNamespace)

	// Get the Service
	svc := &corev1.Service{}
	if err := b.client.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      serviceRef.Name,
	}, svc); err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	// Get EndpointSlices for the Service
	endpointSliceList := &discoveryv1.EndpointSliceList{}
	if err := b.client.List(ctx, endpointSliceList, client.InNamespace(namespace),
		client.MatchingLabels{
			"kubernetes.io/service-name": serviceRef.Name,
		}); err != nil {
		return nil, fmt.Errorf("failed to list endpoint slices: %w", err)
	}

	var endpoints []*pb.Endpoint
	for _, es := range endpointSliceList.Items {
		for _, ep := range es.Endpoints {
			if len(ep.Addresses) == 0 {
				continue
			}

			// Find the matching port
			var port int32
			for _, p := range es.Ports {
				if p.Port != nil && *p.Port == serviceRef.Port {
					port = *p.Port
					break
				}
			}

			if port == 0 {
				continue
			}

			ready := ep.Conditions.Ready != nil && *ep.Conditions.Ready

			for _, addr := range ep.Addresses {
				endpoints = append(endpoints, &pb.Endpoint{
					Address: addr,
					Port:    port,
					Ready:   ready,
				})
			}
		}
	}

	return &pb.EndpointList{Endpoints: endpoints}, nil
}

// loadTLSConfig loads TLS certificates from Kubernetes Secret
func (b *Builder) loadTLSConfig(ctx context.Context, tls *novaedgev1alpha1.TLSConfig, defaultNamespace string) (*pb.TLSConfig, error) {
	namespace := tls.SecretRef.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	secret := &corev1.Secret{}
	if err := b.client.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      tls.SecretRef.Name,
	}, secret); err != nil {
		return nil, fmt.Errorf("failed to get TLS secret: %w", err)
	}

	cert, ok := secret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("tls.crt not found in secret")
	}

	key, ok := secret.Data["tls.key"]
	if !ok {
		return nil, fmt.Errorf("tls.key not found in secret")
	}

	return &pb.TLSConfig{
		Cert:         cert,
		Key:          key,
		MinVersion:   tls.MinVersion,
		CipherSuites: tls.CipherSuites,
	}, nil
}

// generateVersion generates a version string based on content hash
func (b *Builder) generateVersion(snapshot *pb.ConfigSnapshot) string {
	// Create a deterministic string representation
	var parts []string

	// Add all component counts and names
	for _, gw := range snapshot.Gateways {
		parts = append(parts, fmt.Sprintf("gw:%s/%s", gw.Namespace, gw.Name))
	}
	for _, r := range snapshot.Routes {
		parts = append(parts, fmt.Sprintf("route:%s/%s", r.Namespace, r.Name))
	}
	for _, c := range snapshot.Clusters {
		parts = append(parts, fmt.Sprintf("cluster:%s/%s", c.Namespace, c.Name))
	}
	for _, vip := range snapshot.VipAssignments {
		parts = append(parts, fmt.Sprintf("vip:%s:%s", vip.VipName, vip.Address))
	}
	for _, p := range snapshot.Policies {
		parts = append(parts, fmt.Sprintf("policy:%s/%s", p.Namespace, p.Name))
	}

	// Sort for determinism
	sort.Strings(parts)

	// Hash the concatenated parts
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
	}
	hash := hex.EncodeToString(h.Sum(nil))

	// Return timestamp + hash prefix for readability
	return fmt.Sprintf("%d-%s", snapshot.GenerationTime, hash[:16])
}

// Helper functions

func convertVIPMode(mode novaedgev1alpha1.VIPMode) pb.VIPMode {
	switch mode {
	case novaedgev1alpha1.VIPModeL2ARP:
		return pb.VIPMode_L2_ARP
	case novaedgev1alpha1.VIPModeBGP:
		return pb.VIPMode_BGP
	case novaedgev1alpha1.VIPModeOSPF:
		return pb.VIPMode_OSPF
	default:
		return pb.VIPMode_VIP_MODE_UNSPECIFIED
	}
}

func convertBGPConfig(config *novaedgev1alpha1.BGPConfig) *pb.BGPConfig {
	if config == nil {
		return nil
	}

	pbConfig := &pb.BGPConfig{
		LocalAs:     config.LocalAS,
		RouterId:    config.RouterID,
		Peers:       make([]*pb.BGPPeer, 0, len(config.Peers)),
		Communities: config.Communities,
	}

	if config.LocalPreference != nil {
		pbConfig.LocalPreference = *config.LocalPreference
	}

	for _, peer := range config.Peers {
		pbPeer := &pb.BGPPeer{
			Address: peer.Address,
			As:      peer.AS,
			Port:    uint32(peer.Port),
		}
		pbConfig.Peers = append(pbConfig.Peers, pbPeer)
	}

	return pbConfig
}

func convertProtocol(protocol novaedgev1alpha1.ProtocolType) pb.Protocol {
	switch protocol {
	case novaedgev1alpha1.ProtocolTypeHTTP:
		return pb.Protocol_HTTP
	case novaedgev1alpha1.ProtocolTypeHTTPS:
		return pb.Protocol_HTTPS
	case novaedgev1alpha1.ProtocolTypeTCP:
		return pb.Protocol_TCP
	case novaedgev1alpha1.ProtocolTypeTLS:
		return pb.Protocol_TLS
	default:
		return pb.Protocol_PROTOCOL_UNSPECIFIED
	}
}

func convertLBPolicy(policy novaedgev1alpha1.LoadBalancingPolicy) pb.LoadBalancingPolicy {
	switch policy {
	case novaedgev1alpha1.LBPolicyRoundRobin:
		return pb.LoadBalancingPolicy_ROUND_ROBIN
	case novaedgev1alpha1.LBPolicyP2C:
		return pb.LoadBalancingPolicy_P2C
	case novaedgev1alpha1.LBPolicyEWMA:
		return pb.LoadBalancingPolicy_EWMA
	case novaedgev1alpha1.LBPolicyRingHash:
		return pb.LoadBalancingPolicy_RING_HASH
	case novaedgev1alpha1.LBPolicyMaglev:
		return pb.LoadBalancingPolicy_MAGLEV
	default:
		return pb.LoadBalancingPolicy_LB_POLICY_UNSPECIFIED
	}
}

func convertPolicyType(policyType novaedgev1alpha1.PolicyType) pb.PolicyType {
	switch policyType {
	case novaedgev1alpha1.PolicyTypeRateLimit:
		return pb.PolicyType_RATE_LIMIT
	case novaedgev1alpha1.PolicyTypeJWT:
		return pb.PolicyType_JWT
	case novaedgev1alpha1.PolicyTypeIPAllowList:
		return pb.PolicyType_IP_ALLOW_LIST
	case novaedgev1alpha1.PolicyTypeIPDenyList:
		return pb.PolicyType_IP_DENY_LIST
	case novaedgev1alpha1.PolicyTypeCORS:
		return pb.PolicyType_CORS
	default:
		return pb.PolicyType_POLICY_TYPE_UNSPECIFIED
	}
}

func convertMatches(matches []novaedgev1alpha1.HTTPRouteMatch) []*pb.RouteMatch {
	var result []*pb.RouteMatch
	for _, m := range matches {
		pbMatch := &pb.RouteMatch{
			Method: getString(m.Method),
		}

		if m.Path != nil {
			pbMatch.Path = &pb.PathMatch{
				Type:  convertPathMatchType(m.Path.Type),
				Value: m.Path.Value,
			}
		}

		for _, h := range m.Headers {
			pbMatch.Headers = append(pbMatch.Headers, &pb.HeaderMatch{
				Type:  convertHeaderMatchType(h.Type),
				Name:  h.Name,
				Value: h.Value,
			})
		}

		result = append(result, pbMatch)
	}
	return result
}

func convertFilters(filters []novaedgev1alpha1.HTTPRouteFilter) []*pb.RouteFilter {
	var result []*pb.RouteFilter
	for _, f := range filters {
		pbFilter := &pb.RouteFilter{
			Type:        convertFilterType(f.Type),
			RedirectUrl: getString(f.RedirectURL),
			RewritePath: getString(f.RewritePath),
		}

		for _, h := range f.Add {
			pbFilter.AddHeaders = append(pbFilter.AddHeaders, &pb.HTTPHeader{
				Name:  h.Name,
				Value: h.Value,
			})
		}

		pbFilter.RemoveHeaders = f.Remove

		result = append(result, pbFilter)
	}
	return result
}

func convertPathMatchType(matchType novaedgev1alpha1.PathMatchType) pb.PathMatchType {
	switch matchType {
	case novaedgev1alpha1.PathMatchExact:
		return pb.PathMatchType_EXACT
	case novaedgev1alpha1.PathMatchPathPrefix:
		return pb.PathMatchType_PATH_PREFIX
	case novaedgev1alpha1.PathMatchRegularExpression:
		return pb.PathMatchType_REGULAR_EXPRESSION
	default:
		return pb.PathMatchType_PATH_MATCH_TYPE_UNSPECIFIED
	}
}

func convertHeaderMatchType(matchType novaedgev1alpha1.HeaderMatchType) pb.HeaderMatchType {
	switch matchType {
	case novaedgev1alpha1.HeaderMatchExact:
		return pb.HeaderMatchType_HEADER_EXACT
	case novaedgev1alpha1.HeaderMatchRegularExpression:
		return pb.HeaderMatchType_HEADER_REGULAR_EXPRESSION
	default:
		return pb.HeaderMatchType_HEADER_MATCH_TYPE_UNSPECIFIED
	}
}

func convertFilterType(filterType novaedgev1alpha1.HTTPRouteFilterType) pb.RouteFilterType {
	switch filterType {
	case novaedgev1alpha1.HTTPRouteFilterAddHeader:
		return pb.RouteFilterType_ADD_HEADER
	case novaedgev1alpha1.HTTPRouteFilterRemoveHeader:
		return pb.RouteFilterType_REMOVE_HEADER
	case novaedgev1alpha1.HTTPRouteFilterRequestRedirect:
		return pb.RouteFilterType_REQUEST_REDIRECT
	case novaedgev1alpha1.HTTPRouteFilterURLRewrite:
		return pb.RouteFilterType_URL_REWRITE
	default:
		return pb.RouteFilterType_ROUTE_FILTER_TYPE_UNSPECIFIED
	}
}

func convertCircuitBreaker(cb *novaedgev1alpha1.CircuitBreaker) *pb.CircuitBreaker {
	return &pb.CircuitBreaker{
		MaxConnections:     getInt32(cb.MaxConnections),
		MaxPendingRequests: getInt32(cb.MaxPendingRequests),
		MaxRequests:        getInt32(cb.MaxRequests),
		MaxRetries:         getInt32(cb.MaxRetries),
	}
}

func convertHealthCheck(hc *novaedgev1alpha1.HealthCheck) *pb.HealthCheck {
	return &pb.HealthCheck{
		IntervalMs:         durationToMillis(hc.Interval),
		TimeoutMs:          durationToMillis(hc.Timeout),
		HealthyThreshold:   getInt32(hc.HealthyThreshold),
		UnhealthyThreshold: getInt32(hc.UnhealthyThreshold),
		HttpPath:           getString(hc.HTTPPath),
	}
}

func durationToMillis(d metav1.Duration) int64 {
	return d.Duration.Milliseconds()
}

func durationToSeconds(d *metav1.Duration) int64 {
	if d == nil {
		return 0
	}
	return int64(d.Seconds())
}

func getNamespace(ns *string, defaultNs string) string {
	if ns != nil && *ns != "" {
		return *ns
	}
	return defaultNs
}

func getWeight(w *int32) int32 {
	if w != nil {
		return *w
	}
	return 1
}

func getInt32(v *int32) int32 {
	if v != nil {
		return *v
	}
	return 0
}

func getString(v *string) string {
	if v != nil {
		return *v
	}
	return ""
}
