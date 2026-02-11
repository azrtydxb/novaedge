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

	gateways := make([]*pb.Gateway, 0, len(gatewayList.Items))
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

			// Load SNI TLS certificates map if present
			if len(listener.TLSCertificates) > 0 {
				pbListener.TlsCertificates = make(map[string]*pb.TLSConfig)
				for hostname, tlsConfig := range listener.TLSCertificates {
					pbTLSConfig, err := b.loadTLSConfig(ctx, &tlsConfig, gw.Namespace)
					if err != nil {
						log.FromContext(ctx).Error(err, "Failed to load SNI TLS config",
							"listener", listener.Name,
							"hostname", hostname)
						continue
					}
					pbListener.TlsCertificates[hostname] = pbTLSConfig
				}
			}

			// Load QUIC configuration if present (for HTTP/3)
			if listener.QUIC != nil {
				pbListener.Quic = &pb.QUICConfig{
					MaxIdleTimeout: listener.QUIC.MaxIdleTimeout,
					MaxBiStreams:   listener.QUIC.MaxBiStreams,
					MaxUniStreams:  listener.QUIC.MaxUniStreams,
					Enable_0Rtt:    listener.QUIC.Enable0RTT,
				}
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

	routes := make([]*pb.Route, 0, len(routeList.Items))
	for _, r := range routeList.Items {
		route := &pb.Route{
			Name:      r.Name,
			Namespace: r.Namespace,
			Hostnames: r.Spec.Hostnames,
			Rules:     make([]*pb.RouteRule, 0, len(r.Spec.Rules)),
		}

		for _, rule := range r.Spec.Rules {
			// Convert all backend refs with weights
			backendRefs := make([]*pb.BackendRef, 0, len(rule.BackendRefs))
			for _, backendRef := range rule.BackendRefs {
				backendRefs = append(backendRefs, &pb.BackendRef{
					Name:      backendRef.Name,
					Namespace: getNamespace(backendRef.Namespace, r.Namespace),
					Weight:    getWeight(backendRef.Weight),
				})
			}

			pbRule := &pb.RouteRule{
				Matches:     convertMatches(rule.Matches),
				Filters:     convertFilters(rule.Filters),
				BackendRefs: backendRefs,
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

	clusters := make([]*pb.Cluster, 0, len(backendList.Items))
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

			// Load CA cert from secret if specified
			if backend.Spec.TLS.CACertSecretRef != nil && *backend.Spec.TLS.CACertSecretRef != "" {
				secret := &corev1.Secret{}
				secretName := *backend.Spec.TLS.CACertSecretRef
				if err := b.client.Get(ctx, types.NamespacedName{
					Namespace: backend.Namespace,
					Name:      secretName,
				}, secret); err != nil {
					log.FromContext(ctx).Error(err, "Failed to load CA cert secret",
						"backend", backend.Name,
						"secret", secretName,
					)
				} else {
					// Try common CA cert keys
					if caCert, ok := secret.Data["ca.crt"]; ok {
						cluster.Tls.CaCert = caCert
					} else if caCert, ok := secret.Data["tls.crt"]; ok {
						cluster.Tls.CaCert = caCert
					} else if caCert, ok := secret.Data["ca-bundle.crt"]; ok {
						cluster.Tls.CaCert = caCert
					}
				}
			}
		}

		// Session affinity configuration
		if backend.Spec.SessionAffinity != nil {
			cluster.SessionAffinity = convertSessionAffinity(backend.Spec.SessionAffinity)
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

	policies := make([]*pb.Policy, 0, len(policyList.Items))
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

		if p.Spec.SecurityHeaders != nil {
			shConfig := &pb.SecurityHeadersConfig{
				ContentSecurityPolicy:     p.Spec.SecurityHeaders.ContentSecurityPolicy,
				XFrameOptions:             p.Spec.SecurityHeaders.XFrameOptions,
				XContentTypeOptions:       p.Spec.SecurityHeaders.XContentTypeOptions,
				XXssProtection:            p.Spec.SecurityHeaders.XXSSProtection,
				ReferrerPolicy:            p.Spec.SecurityHeaders.ReferrerPolicy,
				PermissionsPolicy:         p.Spec.SecurityHeaders.PermissionsPolicy,
				CrossOriginEmbedderPolicy: p.Spec.SecurityHeaders.CrossOriginEmbedderPolicy,
				CrossOriginOpenerPolicy:   p.Spec.SecurityHeaders.CrossOriginOpenerPolicy,
				CrossOriginResourcePolicy: p.Spec.SecurityHeaders.CrossOriginResourcePolicy,
			}
			if p.Spec.SecurityHeaders.HSTS != nil {
				shConfig.Hsts = &pb.HSTSConfig{
					Enabled:           p.Spec.SecurityHeaders.HSTS.Enabled,
					MaxAgeSeconds:     p.Spec.SecurityHeaders.HSTS.MaxAge,
					IncludeSubdomains: p.Spec.SecurityHeaders.HSTS.IncludeSubDomains,
					Preload:           p.Spec.SecurityHeaders.HSTS.Preload,
				}
			}
			policy.SecurityHeaders = shConfig
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

	// Resolve the target port name and numeric targetPort from the Service spec.
	// EndpointSlices contain targetPort values (not Service ports), so we need
	// to find the port name or numeric targetPort that corresponds to serviceRef.Port.
	var targetPortName string
	var targetPortNumber int32
	for _, sp := range svc.Spec.Ports {
		if sp.Port == serviceRef.Port {
			targetPortName = sp.Name
			// TargetPort can be a string (port name) or int (port number).
			// When it is numeric, use it for direct matching against EndpointSlice ports.
			if sp.TargetPort.IntValue() > 0 {
				targetPortNumber = int32(sp.TargetPort.IntValue())
			}
			break
		}
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

			// Find the matching port in the EndpointSlice.
			// Priority: 1) match by port name, 2) match by targetPort number,
			// 3) fall back to service port number for unnamed single-port services.
			var port int32
			for _, p := range es.Ports {
				if p.Port == nil {
					continue
				}
				// Named port match: links Service port name to EndpointSlice port name
				if targetPortName != "" && p.Name != nil && *p.Name == targetPortName {
					port = *p.Port
					break
				}
				// Numeric targetPort match: the Service explicitly sets targetPort
				if targetPortNumber > 0 && *p.Port == targetPortNumber {
					port = *p.Port
					break
				}
				// Fallback for unnamed ports: direct port number match
				if targetPortName == "" && targetPortNumber == 0 && *p.Port == serviceRef.Port {
					port = *p.Port
					break
				}
			}
			// Final fallback: if the EndpointSlice has exactly one port, use it.
			// This handles the common case of a single-port Service where the port
			// name is empty and targetPort differs from port.
			if port == 0 && len(es.Ports) == 1 && es.Ports[0].Port != nil {
				port = *es.Ports[0].Port
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
	parts := make([]string, 0, len(snapshot.Gateways)+len(snapshot.Routes)+len(snapshot.Clusters)+len(snapshot.VipAssignments)+len(snapshot.Policies))

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
