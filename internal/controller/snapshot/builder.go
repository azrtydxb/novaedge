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

// Package snapshot builds and distributes versioned ConfigSnapshot objects
// to NovaEdge node agents, containing gateways, routes, backends, policies,
// VIP assignments, TLS certificates, and L4 configurations.
package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	agentconfig "github.com/azrtydxb/novaedge/internal/agent/config"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

var (
	errNoCACertificateFoundInSecret  = errors.New("no CA certificate found in secret")
	errTLSSecret                     = errors.New("TLS secret")
	errTLSCrtNotFoundInSecret        = errors.New("tls.crt not found in secret")
	errTLSKeyNotFoundInSecret        = errors.New("tls.key not found in secret")
	errWASMConfigMap                 = errors.New("WASM ConfigMap")
	errWASMBinaryNotFoundInConfigMap = errors.New("WASM binary not found in ConfigMap")
)

// FederationStateProvider exposes the minimal federation state needed by the
// snapshot builder to populate FederationMetadata on ConfigSnapshots and to
// include remote endpoints from federated clusters.
type FederationStateProvider interface {
	GetFederationID() string
	GetLocalMemberName() string
	GetVectorClock() map[string]int64
	IsActive() bool
	GetRemoteEndpoints(namespace, serviceName string) []*pb.ServiceEndpoints
}

// Builder builds ConfigSnapshots from Kubernetes resources
type Builder struct {
	client             client.Client
	federationProvider FederationStateProvider
}

// NewBuilder creates a new snapshot builder
func NewBuilder(client client.Client) *Builder {
	return &Builder{
		client: client,
	}
}

// buildContext holds all pre-fetched Kubernetes resources for a single
// snapshot build pass. This eliminates duplicate List() calls (N+1 pattern)
// and allows sub-functions to look up resources from memory instead of
// making additional API calls.
type buildContext struct {
	gateways    []novaedgev1alpha1.ProxyGateway
	routes      []novaedgev1alpha1.ProxyRoute
	backends    []novaedgev1alpha1.ProxyBackend
	policies    []novaedgev1alpha1.ProxyPolicy
	services    []corev1.Service
	wanLinks    []novaedgev1alpha1.ProxyWANLink
	wanPolicies []novaedgev1alpha1.ProxyWANPolicy

	// Pre-loaded node map for O(1) topology lookups (fixes N+1 node fetches)
	nodes map[string]*corev1.Node

	// Pre-fetched Secrets and ConfigMaps keyed by "namespace/name" for O(1) lookup
	// (eliminates per-policy/per-route Get() calls)
	secrets    map[string]*corev1.Secret
	configMaps map[string]*corev1.ConfigMap
}

// prefetch loads all Kubernetes resources needed for a snapshot build pass
// into the buildContext with a minimal number of API calls.
func (b *Builder) prefetch(ctx context.Context) (*buildContext, error) {
	bc := &buildContext{
		nodes:      make(map[string]*corev1.Node),
		secrets:    make(map[string]*corev1.Secret),
		configMaps: make(map[string]*corev1.ConfigMap),
	}

	// --- CRD resources ---
	gatewayList := &novaedgev1alpha1.ProxyGatewayList{}
	if err := b.client.List(ctx, gatewayList); err != nil {
		return nil, fmt.Errorf("failed to list gateways: %w", err)
	}
	bc.gateways = gatewayList.Items

	routeList := &novaedgev1alpha1.ProxyRouteList{}
	if err := b.client.List(ctx, routeList); err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}
	bc.routes = routeList.Items

	backendList := &novaedgev1alpha1.ProxyBackendList{}
	if err := b.client.List(ctx, backendList); err != nil {
		return nil, fmt.Errorf("failed to list backends: %w", err)
	}
	bc.backends = backendList.Items

	policyList := &novaedgev1alpha1.ProxyPolicyList{}
	if err := b.client.List(ctx, policyList); err != nil {
		return nil, fmt.Errorf("failed to list policies: %w", err)
	}
	bc.policies = policyList.Items

	serviceList := &corev1.ServiceList{}
	if err := b.client.List(ctx, serviceList); err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}
	bc.services = serviceList.Items

	wanLinkList := &novaedgev1alpha1.ProxyWANLinkList{}
	if err := b.client.List(ctx, wanLinkList); err != nil {
		return nil, fmt.Errorf("failed to list WAN links: %w", err)
	}
	bc.wanLinks = wanLinkList.Items

	wanPolicyList := &novaedgev1alpha1.ProxyWANPolicyList{}
	if err := b.client.List(ctx, wanPolicyList); err != nil {
		return nil, fmt.Errorf("failed to list WAN policies: %w", err)
	}
	bc.wanPolicies = wanPolicyList.Items

	// --- Nodes: pre-load all into map for O(1) topology lookup ---
	nodeList := &corev1.NodeList{}
	if err := b.client.List(ctx, nodeList); err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	for i := range nodeList.Items {
		bc.nodes[nodeList.Items[i].Name] = &nodeList.Items[i]
	}

	// --- Secrets: batch-fetch all TLS and auth secrets ---
	secretList := &corev1.SecretList{}
	if err := b.client.List(ctx, secretList); err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}
	for i := range secretList.Items {
		key := secretList.Items[i].Namespace + "/" + secretList.Items[i].Name
		bc.secrets[key] = &secretList.Items[i]
	}

	// --- ConfigMaps: batch-fetch for WAF rules and WASM binaries ---
	configMapList := &corev1.ConfigMapList{}
	if err := b.client.List(ctx, configMapList); err != nil {
		return nil, fmt.Errorf("failed to list configmaps: %w", err)
	}
	for i := range configMapList.Items {
		key := configMapList.Items[i].Namespace + "/" + configMapList.Items[i].Name
		bc.configMaps[key] = &configMapList.Items[i]
	}

	return bc, nil
}

// getSecret returns a pre-fetched Secret from the build context.
func (bc *buildContext) getSecret(namespace, name string) (*corev1.Secret, bool) {
	s, ok := bc.secrets[namespace+"/"+name]
	return s, ok
}

// getConfigMap returns a pre-fetched ConfigMap from the build context.
func (bc *buildContext) getConfigMap(namespace, name string) (*corev1.ConfigMap, bool) {
	cm, ok := bc.configMaps[namespace+"/"+name]
	return cm, ok
}

// SetFederationProvider sets the federation state provider used to populate
// FederationMetadata on built snapshots. When nil, snapshots are built
// without federation metadata.
func (b *Builder) SetFederationProvider(provider FederationStateProvider) {
	b.federationProvider = provider
}

// BuildSnapshotWithExtensions builds a complete ConfigSnapshot with TLS extensions for a specific node
func (b *Builder) BuildSnapshotWithExtensions(ctx context.Context, nodeName string) (*pb.ConfigSnapshot, *agentconfig.SnapshotExtensions, error) {
	snapshot, err := b.BuildSnapshot(ctx, nodeName)
	if err != nil {
		return nil, nil, err
	}

	extensions, err := b.buildExtensions(ctx, snapshot)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to build snapshot extensions")
		// Non-fatal: return snapshot without extensions
		return snapshot, nil, nil
	}

	return snapshot, extensions, nil
}

// buildExtensions builds mTLS, PROXY protocol, and OCSP extensions from gateway/backend CRDs
func (b *Builder) buildExtensions(ctx context.Context, _ *pb.ConfigSnapshot) (*agentconfig.SnapshotExtensions, error) {
	ext := &agentconfig.SnapshotExtensions{
		ListenerExtensions: make(map[string]*pb.ListenerExtensions),
		ClusterExtensions:  make(map[string]*pb.ClusterExtensions),
	}

	// Build listener extensions from gateways
	gatewayList := &novaedgev1alpha1.ProxyGatewayList{}
	if err := b.client.List(ctx, gatewayList); err != nil {
		return nil, err
	}

	for _, gw := range gatewayList.Items {
		for _, listener := range gw.Spec.Listeners {
			listenerExt := &pb.ListenerExtensions{
				OCSPStapling: listener.OCSPStapling,
			}

			// Build mTLS config
			if listener.ClientAuth != nil && listener.ClientAuth.Mode != "" && listener.ClientAuth.Mode != novaedgev1alpha1.ClientAuthModeNone {
				clientAuth := &pb.ClientAuthConfig{
					Mode:               string(listener.ClientAuth.Mode),
					RequiredCnPatterns: listener.ClientAuth.RequiredCNPatterns,
					RequiredSans:       listener.ClientAuth.RequiredSANs,
				}

				// Load CA certificate from secret
				if listener.ClientAuth.CACertRef != nil {
					caCert, err := b.loadCACert(ctx, listener.ClientAuth.CACertRef.Name,
						listener.ClientAuth.CACertRef.Namespace, gw.Namespace)
					if err != nil {
						log.FromContext(ctx).Error(err, "Failed to load mTLS CA cert",
							"gateway", gw.Name, "listener", listener.Name)
					} else {
						clientAuth.CaCert = caCert
					}
				}

				listenerExt.ClientAuth = clientAuth
			}

			// Build PROXY protocol config
			if listener.ProxyProtocol != nil && listener.ProxyProtocol.Enabled {
				listenerExt.ProxyProtocol = &pb.ProxyProtocolConfig{
					Enabled:      true,
					Version:      listener.ProxyProtocol.Version,
					TrustedCidrs: listener.ProxyProtocol.TrustedCIDRs,
				}
			}

			key := fmt.Sprintf("%s/%s/%s", gw.Namespace, gw.Name, listener.Name)
			ext.ListenerExtensions[key] = listenerExt
		}
	}

	// Build cluster extensions from backends
	backendList := &novaedgev1alpha1.ProxyBackendList{}
	if err := b.client.List(ctx, backendList); err != nil {
		return nil, err
	}

	for _, backend := range backendList.Items {
		if backend.Spec.UpstreamProxyProtocol != nil && backend.Spec.UpstreamProxyProtocol.Enabled {
			clusterKey := fmt.Sprintf("%s/%s", backend.Namespace, backend.Name)
			ext.ClusterExtensions[clusterKey] = &pb.ClusterExtensions{
				UpstreamProxyProtocol: &pb.UpstreamProxyProtocol{
					Enabled: true,
					Version: backend.Spec.UpstreamProxyProtocol.Version,
				},
			}
		}
	}

	return ext, nil
}

// loadCACert loads a CA certificate from a Kubernetes Secret
func (b *Builder) loadCACert(ctx context.Context, secretName, secretNamespace, defaultNamespace string) ([]byte, error) {
	namespace := secretNamespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	secret := &corev1.Secret{}
	if err := b.client.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}, secret); err != nil {
		return nil, fmt.Errorf("failed to get CA cert secret %s/%s: %w", namespace, secretName, err)
	}

	// Try common CA cert keys
	for _, key := range []string{"ca.crt", "ca-bundle.crt", "tls.crt"} {
		if caCert, ok := secret.Data[key]; ok {
			return caCert, nil
		}
	}

	return nil, fmt.Errorf("%w: %s/%s (tried ca.crt, ca-bundle.crt, tls.crt)", errNoCACertificateFoundInSecret, namespace, secretName)
}

// BuildSnapshot builds a complete ConfigSnapshot for a specific node
func (b *Builder) BuildSnapshot(ctx context.Context, nodeName string) (*pb.ConfigSnapshot, error) {
	logger := log.FromContext(ctx).WithValues("node", nodeName)
	logger.Info("Building config snapshot")

	startTime := time.Now()
	snapshot := &pb.ConfigSnapshot{
		GenerationTime: time.Now().Unix(),
	}

	// Pre-fetch all Kubernetes resources in bulk to avoid N+1 API calls.
	// Every sub-function below reads from this cached buildContext instead
	// of making its own List()/Get() calls.
	bc, err := b.prefetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to prefetch resources: %w", err)
	}

	// Build gateways
	snapshot.Gateways = b.buildGateways(ctx, bc)

	// Build routes
	routes := b.buildRoutes(ctx, bc)
	snapshot.Routes = routes

	// Build backends/clusters
	snapshot.Clusters, snapshot.Endpoints = b.buildClusters(ctx, nil, bc)

	// Build policies
	policies := b.buildPolicies(ctx, bc)
	snapshot.Policies = policies

	// Build L4 listeners (TCP/UDP/TLS passthrough)
	l4Listeners := b.buildL4Listeners(ctx, snapshot.Gateways, snapshot.Endpoints, bc)
	snapshot.L4Listeners = l4Listeners

	// Build internal service routing tables for east-west mesh traffic
	snapshot.InternalServices = b.buildInternalServices(ctx, bc)

	// Build mesh authorization policies
	meshAuthzPolicies := b.buildMeshAuthorizationPolicies(ctx, bc)
	snapshot.MeshAuthzPolicies = meshAuthzPolicies

	// Build SD-WAN WAN links
	wanLinks := b.buildWANLinks(bc)
	snapshot.WanLinks = wanLinks

	// Build SD-WAN WAN policies
	wanPolicies := b.buildWANPolicies(bc)
	snapshot.WanPolicies = wanPolicies

	// Sort all repeated fields for deterministic proto marshaling.
	// proto.MarshalOptions{Deterministic: true} only sorts map keys,
	// NOT repeated fields (slices). Since Go map iteration populates
	// them in random order, we must sort explicitly to ensure stable
	// version hashes across builds. (#867)
	sortSnapshot(snapshot)

	// Generate version based on content hash
	snapshot.Version = b.generateVersion(snapshot)

	// Enhance snapshot with federation metadata when federation is active
	if b.federationProvider != nil && b.federationProvider.IsActive() {
		b.enhanceWithFederation(snapshot)
	}

	// Record metrics
	duration := time.Since(startTime).Seconds()
	sizeBytes := proto.Size(snapshot)
	resourceCounts := map[string]int{
		"gateways":            len(snapshot.Gateways),
		"routes":              len(snapshot.Routes),
		"clusters":            len(snapshot.Clusters),
		"policies":            len(snapshot.Policies),
		"l4_listeners":        len(snapshot.L4Listeners),
		"internal_services":   len(snapshot.InternalServices),
		"mesh_authz_policies": len(snapshot.MeshAuthzPolicies),
		"wan_links":           len(snapshot.WanLinks),
		"wan_policies":        len(snapshot.WanPolicies),
	}
	RecordSnapshotBuild(nodeName, duration, sizeBytes, resourceCounts)

	logger.Info("Config snapshot built successfully",
		"version", snapshot.Version,
		"gateways", len(snapshot.Gateways),
		"routes", len(snapshot.Routes),
		"clusters", len(snapshot.Clusters),
		"policies", len(snapshot.Policies),
		"l4_listeners", len(snapshot.L4Listeners),
		"internal_services", len(snapshot.InternalServices),
		"mesh_authz_policies", len(snapshot.MeshAuthzPolicies),
		"wan_links", len(snapshot.WanLinks),
		"wan_policies", len(snapshot.WanPolicies),
		"duration_ms", duration*1000,
		"size_bytes", sizeBytes)

	return snapshot, nil
}

// buildGateways builds gateway configurations
func (b *Builder) buildGateways(ctx context.Context, bc *buildContext) []*pb.Gateway {
	gateways := make([]*pb.Gateway, 0, len(bc.gateways))
	for _, gw := range bc.gateways {
		gateway := &pb.Gateway{
			Name:              gw.Name,
			Namespace:         gw.Namespace,
			VipRef:            gw.Spec.VIPRef,
			IngressClassName:  gw.Spec.IngressClassName,
			LoadBalancerClass: gw.Spec.LoadBalancerClass,
			Listeners:         make([]*pb.Listener, 0, len(gw.Spec.Listeners)),
		}

		for _, listener := range gw.Spec.Listeners {
			pbListener := &pb.Listener{
				Name:                listener.Name,
				Port:                listener.Port,
				Protocol:            convertProtocol(listener.Protocol),
				Hostnames:           listener.Hostnames,
				SslRedirect:         listener.SSLRedirect,
				AllowedSourceRanges: listener.AllowedSourceRanges,
			}

			// Load TLS configuration if present
			if listener.TLS != nil {
				tlsConfig, err := b.loadTLSConfig(ctx, listener.TLS, gw.Namespace, bc)
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
					pbTLSConfig, err := b.loadTLSConfig(ctx, &tlsConfig, gw.Namespace, bc)
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

		// Add compression configuration
		if gw.Spec.Compression != nil {
			gateway.Compression = convertCompressionConfig(gw.Spec.Compression)
		}

		// Convert error pages configuration
		if len(gw.Spec.CustomErrorPages) > 0 {
			gateway.ErrorPages = convertErrorPages(gw.Spec.CustomErrorPages)
		}

		// Convert redirect scheme configuration
		if gw.Spec.RedirectScheme != nil && gw.Spec.RedirectScheme.Enabled {
			gateway.RedirectScheme = convertRedirectScheme(gw.Spec.RedirectScheme)
		}

		// Convert ExtProc configuration
		if gw.Spec.ExtProc != nil {
			gateway.ExtProc = convertExtProc(gw.Spec.ExtProc)
		}
		gateways = append(gateways, gateway)
	}

	return gateways
}

// buildRoutes builds route configurations
func (b *Builder) buildRoutes(_ context.Context, bc *buildContext) []*pb.Route {
	routes := make([]*pb.Route, 0, len(bc.routes))
	for _, r := range bc.routes {
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

			// Add per-route limits
			if rule.Limits != nil {
				pbRule.Limits = convertRouteLimits(rule.Limits)
			}

			// Add per-route buffering
			if rule.Buffering != nil {
				pbRule.Buffering = convertBufferingConfig(rule.Buffering)
			}

			// Convert mirror configuration if present
			if rule.Mirror != nil {
				mirrorNs := getNamespace(rule.Mirror.BackendRef.Namespace, r.Namespace)
				pbRule.MirrorBackend = &pb.BackendRef{
					Name:      rule.Mirror.BackendRef.Name,
					Namespace: mirrorNs,
					Weight:    getWeight(rule.Mirror.BackendRef.Weight),
				}
				pbRule.MirrorPercent = rule.Mirror.Percentage
				if pbRule.MirrorPercent == 0 {
					pbRule.MirrorPercent = 100 // Default: mirror all requests
				}
			}

			// Convert retry configuration
			if rule.Retry != nil {
				pbRule.Retry = convertRetryConfig(rule.Retry)
			}

			// Convert fault injection configuration
			if rule.FaultInjection != nil {
				pbRule.FaultInjection = convertFaultInjectionConfig(rule.FaultInjection)
			}

			// Convert body transform configuration
			if rule.BodyTransform != nil {
				pbRule.BodyTransform = convertBodyTransformConfig(rule.BodyTransform)
			}

			route.Rules = append(route.Rules, pbRule)
		}

		// Convert access log configuration
		if r.Spec.AccessLog != nil && r.Spec.AccessLog.Enabled {
			route.AccessLog = convertRouteAccessLog(r.Spec.AccessLog)
		}

		// Convert middleware pipeline
		if r.Spec.Pipeline != nil {
			route.Pipeline = convertMiddlewarePipeline(r.Spec.Pipeline)
		}

		// Convert expression
		route.Expression = r.Spec.Expression

		routes = append(routes, route)
	}

	return routes
}

// convertBackendTLS builds the BackendTLS proto from a backend's TLS spec,
// loading the CA cert from the pre-fetched secret cache.
func convertBackendTLS(ctx context.Context, backend novaedgev1alpha1.ProxyBackend, bc *buildContext) *pb.BackendTLS {
	tls := &pb.BackendTLS{
		Enabled:            backend.Spec.TLS.Enabled,
		InsecureSkipVerify: backend.Spec.TLS.InsecureSkipVerify,
	}

	if backend.Spec.TLS.CACertSecretRef == nil || *backend.Spec.TLS.CACertSecretRef == "" {
		return tls
	}

	secretName := *backend.Spec.TLS.CACertSecretRef
	secret, ok := bc.getSecret(backend.Namespace, secretName)
	if !ok {
		log.FromContext(ctx).Error(nil, "CA cert secret not found in cache",
			"backend", backend.Name, "secret", secretName)
		return tls
	}

	// Try common CA cert keys in priority order
	for _, key := range []string{"ca.crt", "tls.crt", "ca-bundle.crt"} {
		if caCert, found := secret.Data[key]; found {
			tls.CaCert = caCert
			break
		}
	}
	return tls
}

// buildClusters builds backend cluster configurations and their endpoints.
func (b *Builder) buildClusters(ctx context.Context, _ map[string]struct{}, bc *buildContext) ([]*pb.Cluster, map[string]*pb.EndpointList) {
	clusters := make([]*pb.Cluster, 0, len(bc.backends))
	endpoints := make(map[string]*pb.EndpointList)

	for _, backend := range bc.backends {
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

		if backend.Spec.OutlierDetection != nil {
			cluster.OutlierDetection = convertOutlierDetection(backend.Spec.OutlierDetection)
		}
		if backend.Spec.SlowStart != nil {
			cluster.SlowStart = convertSlowStart(backend.Spec.SlowStart)
		}

		if backend.Spec.TLS != nil {
			cluster.Tls = convertBackendTLS(ctx, backend, bc)
		}

		// Session affinity configuration
		if backend.Spec.SessionAffinity != nil {
			cluster.SessionAffinity = convertSessionAffinity(backend.Spec.SessionAffinity)
		}

		// Connection pool configuration (#832)
		if backend.Spec.ConnectionPool != nil {
			cluster.ConnectionPool = &pb.ConnectionPool{
				MaxIdleConns:        getInt32(backend.Spec.ConnectionPool.MaxIdleConns),
				MaxIdleConnsPerHost: getInt32(backend.Spec.ConnectionPool.MaxIdleConnsPerHost),
				MaxConnsPerHost:     getInt32(backend.Spec.ConnectionPool.MaxConnsPerHost),
				IdleConnTimeoutMs:   durationToMillis(backend.Spec.ConnectionPool.IdleConnTimeout),
			}
		}

		// Upstream proxy protocol configuration (#841)
		if backend.Spec.UpstreamProxyProtocol != nil {
			cluster.UpstreamProxyProtocol = &pb.UpstreamProxyProtocol{
				Enabled: backend.Spec.UpstreamProxyProtocol.Enabled,
				Version: backend.Spec.UpstreamProxyProtocol.Version,
			}
		}

		// Backend protocol (#843)
		if backend.Spec.Protocol != "" {
			cluster.Protocol = backend.Spec.Protocol
		}

		clusters = append(clusters, cluster)

		// Resolve endpoints for this backend
		if backend.Spec.ServiceRef != nil {
			endpointList, err := b.resolveServiceEndpoints(ctx, backend.Spec.ServiceRef, backend.Namespace, bc)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to resolve endpoints", "backend", backend.Name)
				continue
			}

			// Merge remote endpoints from federated clusters when federation is active
			if b.federationProvider != nil && b.federationProvider.IsActive() {
				svcNamespace := getNamespace(backend.Spec.ServiceRef.Namespace, backend.Namespace)
				remoteServiceEndpoints := b.federationProvider.GetRemoteEndpoints(svcNamespace, backend.Spec.ServiceRef.Name)
				for _, remoteSvc := range remoteServiceEndpoints {
					for _, ep := range remoteSvc.GetEndpoints() {
						remoteEP := &pb.Endpoint{
							Address: ep.Address,
							Port:    ep.Port,
							Ready:   ep.Ready,
							Labels:  mergeRemoteEndpointLabels(ep.Labels, remoteSvc.ClusterName, remoteSvc.Region, remoteSvc.Zone),
						}
						endpointList.Endpoints = append(endpointList.Endpoints, remoteEP)
					}
				}
			}

			clusterKey := fmt.Sprintf("%s/%s", backend.Namespace, backend.Name)
			endpoints[clusterKey] = endpointList
		}
	}

	return clusters, endpoints
}

// buildPolicies builds policy configurations
func (b *Builder) buildPolicies(ctx context.Context, bc *buildContext) []*pb.Policy {
	policies := make([]*pb.Policy, 0, len(bc.policies))
	for _, p := range bc.policies {
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
				Issuer:            p.Spec.JWT.Issuer,
				Audience:          p.Spec.JWT.Audience,
				JwksUri:           p.Spec.JWT.JWKSUri,
				HeaderName:        p.Spec.JWT.HeaderName,
				HeaderPrefix:      p.Spec.JWT.HeaderPrefix,
				AllowedAlgorithms: p.Spec.JWT.AllowedAlgorithms,
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

		if p.Spec.DistributedRateLimit != nil {
			policy.DistributedRateLimit = convertDistributedRateLimitConfig(p.Spec.DistributedRateLimit)
		}

		if p.Spec.WAF != nil {
			policy.Waf = convertWAFConfig(p.Spec.WAF, bc, p.Namespace)
		}

		// Add WASM plugin configuration
		if p.Spec.WASMPlugin != nil {
			wasmPriority := p.Spec.WASMPlugin.Priority
			if wasmPriority > math.MaxInt32 {
				wasmPriority = math.MaxInt32
			} else if wasmPriority < math.MinInt32 {
				wasmPriority = math.MinInt32
			}
			wasmConfig := &pb.WASMPluginConfig{
				Source:   p.Spec.WASMPlugin.Source,
				Config:   p.Spec.WASMPlugin.Config,
				Phase:    p.Spec.WASMPlugin.Phase,
				Priority: int32(wasmPriority), //nolint:gosec // bounds-checked above
			}
			if p.Spec.WASMPlugin.ConfigRef != nil {
				wasmConfig.ConfigRef = p.Spec.WASMPlugin.ConfigRef.Name
			}
			// Load WASM binary from pre-fetched ConfigMap cache
			wasmBytes, loadErr := b.loadWASMBinary(p.Spec.WASMPlugin.Source, p.Namespace, bc)
			if loadErr != nil {
				log.FromContext(ctx).Error(loadErr, "Failed to load WASM binary",
					"policy", p.Name,
					"source", p.Spec.WASMPlugin.Source)
			} else {
				wasmConfig.WasmBytes = wasmBytes
			}
			policy.WasmPlugin = wasmConfig
		}

		// Build BasicAuth config
		if p.Spec.BasicAuth != nil {
			basicAuthConfig, err := b.buildBasicAuthConfig(&p, bc)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to build BasicAuth config",
					"policy", p.Name)
			} else {
				policy.BasicAuth = basicAuthConfig
			}
		}

		// Build ForwardAuth config
		if p.Spec.ForwardAuth != nil {
			policy.ForwardAuth = b.buildForwardAuthConfig(p.Spec.ForwardAuth)
		}

		// Build OIDC config
		if p.Spec.OIDC != nil {
			oidcConfig, err := b.buildOIDCConfig(&p, bc)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to build OIDC config",
					"policy", p.Name)
			} else {
				policy.Oidc = oidcConfig
			}
		}

		policies = append(policies, policy)
	}

	return policies
}

// meshAnnotation is the annotation key that opts a Service into mesh interception.
const meshAnnotation = "novaedge.io/mesh"

// meshMTLSAnnotation controls the mTLS mode for a mesh-enabled Service.
// Valid values: "permissive" (default), "strict", "disabled".
const meshMTLSAnnotation = "novaedge.io/mesh-mtls"

// buildInternalServices discovers Kubernetes Services annotated for mesh
// interception and builds routing entries with resolved endpoints.
func (b *Builder) buildInternalServices(ctx context.Context, bc *buildContext) []*pb.InternalService {
	logger := log.FromContext(ctx)

	services := make([]*pb.InternalService, 0, len(bc.services))
	for i := range bc.services {
		svc := &bc.services[i]

		// Only include Services annotated with novaedge.io/mesh: "enabled"
		if svc.Annotations[meshAnnotation] != "enabled" {
			continue
		}

		// Skip headless services (no ClusterIP) and ExternalName services
		if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == "None" {
			logger.V(1).Info("Skipping headless/ExternalName service for mesh",
				"service", svc.Name, "namespace", svc.Namespace)
			continue
		}

		// Build ServicePort list
		ports := make([]*pb.ServicePort, 0, len(svc.Spec.Ports))
		for _, sp := range svc.Spec.Ports {
			ports = append(ports, &pb.ServicePort{
				Name:       sp.Name,
				Port:       sp.Port,
				TargetPort: int32(sp.TargetPort.IntValue()), //nolint:gosec // port range is 0-65535
				Protocol:   string(sp.Protocol),
			})
		}

		// Resolve endpoints from EndpointSlices
		endpoints, err := b.resolveInternalServiceEndpoints(ctx, svc)
		if err != nil {
			logger.Error(err, "Failed to resolve endpoints for mesh service",
				"service", svc.Name, "namespace", svc.Namespace)
			continue
		}

		// Determine mTLS mode from annotation (default: permissive)
		mtlsMode := "permissive"
		if mode, ok := svc.Annotations[meshMTLSAnnotation]; ok {
			mtlsMode = mode
		}

		services = append(services, &pb.InternalService{
			Name:        svc.Name,
			Namespace:   svc.Namespace,
			ClusterIp:   svc.Spec.ClusterIP,
			Ports:       ports,
			Endpoints:   endpoints,
			LbPolicy:    pb.LoadBalancingPolicy_ROUND_ROBIN,
			MeshEnabled: true,
			MtlsMode:    mtlsMode,
		})

		logger.V(1).Info("Added internal service for mesh routing",
			"service", svc.Name, "namespace", svc.Namespace,
			"clusterIP", svc.Spec.ClusterIP, "endpoints", len(endpoints))
	}

	// Sort for deterministic snapshot generation
	sort.Slice(services, func(i, j int) bool {
		if services[i].Namespace != services[j].Namespace {
			return services[i].Namespace < services[j].Namespace
		}
		return services[i].Name < services[j].Name
	})

	return services
}

// resolveInternalServiceEndpoints resolves all endpoints for a Service
// from its EndpointSlices, across all ports.
func (b *Builder) resolveInternalServiceEndpoints(ctx context.Context, svc *corev1.Service) ([]*pb.Endpoint, error) {
	endpointSliceList := &discoveryv1.EndpointSliceList{}
	if err := b.client.List(ctx, endpointSliceList,
		client.InNamespace(svc.Namespace),
		client.MatchingLabels{
			"kubernetes.io/service-name": svc.Name,
		}); err != nil {
		return nil, fmt.Errorf("failed to list endpoint slices: %w", err)
	}

	var endpoints []*pb.Endpoint
	for _, es := range endpointSliceList.Items {
		for _, ep := range es.Endpoints {
			if len(ep.Addresses) == 0 {
				continue
			}

			ready := ep.Conditions.Ready == nil || *ep.Conditions.Ready

			// For each port in the EndpointSlice, create endpoints
			for _, p := range es.Ports {
				if p.Port == nil {
					continue
				}
				for _, addr := range ep.Addresses {
					endpoints = append(endpoints, &pb.Endpoint{
						Address: addr,
						Port:    *p.Port,
						Ready:   ready,
					})
				}
			}
		}
	}

	return endpoints, nil
}

// Topology label keys used for locality-aware routing.
const (
	topologyZoneLabel   = "topology.kubernetes.io/zone"
	topologyRegionLabel = "topology.kubernetes.io/region"
)

// matchEndpointSlicePort finds the matching port in the EndpointSlice ports.
// Priority: 1) match by port name, 2) match by targetPort number,
// 3) fall back to service port number for unnamed single-port services.
// Returns 0 if no match is found.
func matchEndpointSlicePort(esPorts []discoveryv1.EndpointPort, targetPortName string, targetPortNumber, servicePort int32) int32 {
	for _, p := range esPorts {
		if p.Port == nil {
			continue
		}
		if targetPortName != "" && p.Name != nil && *p.Name == targetPortName {
			return *p.Port
		}
		if targetPortNumber > 0 && *p.Port == targetPortNumber {
			return *p.Port
		}
		if targetPortName == "" && targetPortNumber == 0 && *p.Port == servicePort {
			return *p.Port
		}
	}
	// Final fallback: if exactly one port exists, use it.
	if len(esPorts) == 1 && esPorts[0].Port != nil {
		return *esPorts[0].Port
	}
	return 0
}

// resolveServiceEndpoints resolves endpoints from a ServiceReference
func (b *Builder) resolveServiceEndpoints(ctx context.Context, serviceRef *novaedgev1alpha1.ServiceReference, defaultNamespace string, bc *buildContext) (*pb.EndpointList, error) {
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
	var targetPortName string
	var targetPortNumber int32
	for _, sp := range svc.Spec.Ports {
		if sp.Port == serviceRef.Port {
			targetPortName = sp.Name
			if tpVal := sp.TargetPort.IntValue(); tpVal > 0 && tpVal <= math.MaxInt32 {
				targetPortNumber = int32(tpVal) //nolint:gosec // bounds-checked above
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

			port := matchEndpointSlicePort(es.Ports, targetPortName, targetPortNumber, serviceRef.Port)
			if port == 0 {
				continue
			}

			ready := ep.Conditions.Ready == nil || *ep.Conditions.Ready
			labels := b.resolveEndpointTopologyLabels(ctx, &ep, bc.nodes)

			for _, addr := range ep.Addresses {
				endpoints = append(endpoints, &pb.Endpoint{
					Address: addr,
					Port:    port,
					Ready:   ready,
					Labels:  labels,
				})
			}
		}
	}

	return &pb.EndpointList{Endpoints: endpoints}, nil
}

// resolveEndpointTopologyLabels extracts zone and region labels from an
// EndpointSlice endpoint. Zone is taken from the endpoint's Zone field or
// endpoint hints. Region is looked up from the pre-loaded Node map.
func (b *Builder) resolveEndpointTopologyLabels(_ context.Context, ep *discoveryv1.Endpoint, nodeMap map[string]*corev1.Node) map[string]string {
	labels := make(map[string]string)

	// Zone: prefer the endpoint's Zone field (set by Kubernetes from the
	// Node's topology.kubernetes.io/zone label), then fall back to hints.
	if ep.Zone != nil && *ep.Zone != "" {
		labels[topologyZoneLabel] = *ep.Zone
	} else if ep.Hints != nil {
		for _, hint := range ep.Hints.ForZones {
			if hint.Name != "" {
				labels[topologyZoneLabel] = hint.Name
				break
			}
		}
	}

	// Region: look up the Node from the pre-loaded map (no API call).
	if ep.NodeName != nil && *ep.NodeName != "" {
		nodeName := *ep.NodeName
		if node, ok := nodeMap[nodeName]; ok && node != nil {
			if region, exists := node.Labels[topologyRegionLabel]; exists {
				labels[topologyRegionLabel] = region
			}
			// If zone was not set from the endpoint, try the node label.
			if _, hasZone := labels[topologyZoneLabel]; !hasZone {
				if zone, exists := node.Labels[topologyZoneLabel]; exists {
					labels[topologyZoneLabel] = zone
				}
			}
		}
	}

	if len(labels) == 0 {
		return nil
	}
	return labels
}

// Federation label keys used to tag remote endpoints.
const (
	federationClusterLabel = "novaedge.io/cluster"
	federationRegionLabel  = "novaedge.io/region"
	federationZoneLabel    = "novaedge.io/zone"
	federationRemoteLabel  = "novaedge.io/remote"
)

// mergeRemoteEndpointLabels builds a label map for a remote endpoint by
// preserving any existing labels from the remote side and overlaying
// federation-specific cluster, region, zone, and remote marker labels.
func mergeRemoteEndpointLabels(existing map[string]string, clusterName, region, zone string) map[string]string {
	labels := make(map[string]string, len(existing)+4)
	for k, v := range existing {
		labels[k] = v
	}
	labels[federationRemoteLabel] = "true"
	if clusterName != "" {
		labels[federationClusterLabel] = clusterName
	}
	if region != "" {
		labels[federationRegionLabel] = region
	}
	if zone != "" {
		labels[federationZoneLabel] = zone
	}
	return labels
}

// loadTLSConfig loads TLS certificates from the pre-fetched Secret cache
func (b *Builder) loadTLSConfig(_ context.Context, tls *novaedgev1alpha1.TLSConfig, defaultNamespace string, bc *buildContext) (*pb.TLSConfig, error) {
	namespace := tls.SecretRef.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	secret, ok := bc.getSecret(namespace, tls.SecretRef.Name)
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s not found in cache", errTLSSecret, namespace, tls.SecretRef.Name)
	}

	cert, ok := secret.Data["tls.crt"]
	if !ok {
		return nil, errTLSCrtNotFoundInSecret
	}

	key, ok := secret.Data["tls.key"]
	if !ok {
		return nil, errTLSKeyNotFoundInSecret
	}

	return &pb.TLSConfig{
		Cert:         cert,
		Key:          key,
		MinVersion:   tls.MinVersion,
		CipherSuites: tls.CipherSuites,
	}, nil
}

// loadWASMBinary loads a WASM binary from the pre-fetched ConfigMap cache
func (b *Builder) loadWASMBinary(source, defaultNamespace string, bc *buildContext) ([]byte, error) {
	// Source is expected to be a ConfigMap name (namespace/name or just name)
	parts := strings.SplitN(source, "/", 2)
	namespace := defaultNamespace
	name := source
	if len(parts) == 2 {
		namespace = parts[0]
		name = parts[1]
	}

	configMap, ok := bc.getConfigMap(namespace, name)
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s not found in cache", errWASMConfigMap, namespace, name)
	}

	// Look for the WASM binary in BinaryData
	if wasmData, ok := configMap.BinaryData["plugin.wasm"]; ok {
		return wasmData, nil
	}

	// Fallback: check Data for base64-encoded WASM
	if wasmStr, ok := configMap.Data["plugin.wasm"]; ok {
		decoded, err := base64.StdEncoding.DecodeString(wasmStr)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 WASM binary: %w", err)
		}
		return decoded, nil
	}

	return nil, fmt.Errorf("%w: %s/%s (expected key: plugin.wasm)", errWASMBinaryNotFoundInConfigMap, namespace, name)
}

// sortSnapshot sorts all repeated (slice) fields in the snapshot to ensure
// deterministic proto marshaling. proto.MarshalOptions{Deterministic: true}
// only sorts map keys, not repeated fields. Since Go map iteration order is
// random, slices populated from maps have non-deterministic order, causing
// different content hashes for semantically identical snapshots. (#867)
func sortSnapshot(s *pb.ConfigSnapshot) {
	sort.Slice(s.Gateways, func(i, j int) bool { return s.Gateways[i].Name < s.Gateways[j].Name })
	sort.Slice(s.Routes, func(i, j int) bool { return s.Routes[i].Name < s.Routes[j].Name })
	sort.Slice(s.Clusters, func(i, j int) bool { return s.Clusters[i].Name < s.Clusters[j].Name })
	sort.Slice(s.VipAssignments, func(i, j int) bool { return s.VipAssignments[i].Address < s.VipAssignments[j].Address })
	sort.Slice(s.Policies, func(i, j int) bool { return s.Policies[i].Name < s.Policies[j].Name })
	sort.Slice(s.AvailableControllers, func(i, j int) bool {
		return s.AvailableControllers[i].Name < s.AvailableControllers[j].Name
	})
	sort.Slice(s.L4Listeners, func(i, j int) bool { return s.L4Listeners[i].Name < s.L4Listeners[j].Name })
	// InternalServices already sorted in buildInternalServices (line ~1162).
	sort.Slice(s.MeshAuthzPolicies, func(i, j int) bool { return s.MeshAuthzPolicies[i].Name < s.MeshAuthzPolicies[j].Name })
	sort.Slice(s.WanLinks, func(i, j int) bool { return s.WanLinks[i].Name < s.WanLinks[j].Name })
	sort.Slice(s.WanPolicies, func(i, j int) bool { return s.WanPolicies[i].Name < s.WanPolicies[j].Name })

	// Sort nested repeated fields: endpoints within each EndpointList.
	for _, epList := range s.Endpoints {
		sort.Slice(epList.Endpoints, func(i, j int) bool {
			return epList.Endpoints[i].Address < epList.Endpoints[j].Address
		})
	}
}

// generateVersion generates a version string by hashing the entire proto
// snapshot with deterministic marshaling. This ensures ANY field change
// (route rules, LB policies, weights, headers, etc.) produces a new version,
// not just changes to resource identifiers.
func (b *Builder) generateVersion(snapshot *pb.ConfigSnapshot) string {
	// Temporarily zero out self-referential fields to avoid including
	// the previous version or generation timestamp in the hash.
	savedVersion := snapshot.Version
	savedGenTime := snapshot.GenerationTime
	snapshot.Version = ""
	snapshot.GenerationTime = 0

	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(snapshot)

	// Restore fields immediately.
	snapshot.Version = savedVersion
	snapshot.GenerationTime = savedGenTime

	if err != nil {
		// Proto marshal should never fail for a well-formed snapshot,
		// but return a unique error version so the caller still detects
		// a change and pushes the snapshot.
		return fmt.Sprintf("err-%d", time.Now().UnixNano())
	}

	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}

// buildMeshAuthorizationPolicies converts ProxyPolicy resources of type
// MeshAuthorization into proto MeshAuthorizationPolicy messages.
func (b *Builder) buildMeshAuthorizationPolicies(_ context.Context, bc *buildContext) []*pb.MeshAuthorizationPolicy {
	policies := make([]*pb.MeshAuthorizationPolicy, 0, len(bc.policies))
	for _, p := range bc.policies {
		if p.Spec.Type != novaedgev1alpha1.PolicyTypeMeshAuthorization {
			continue
		}
		if p.Spec.MeshAuthorization == nil {
			continue
		}

		targetNS := p.Namespace
		if p.Spec.TargetRef.Namespace != nil {
			targetNS = *p.Spec.TargetRef.Namespace
		}

		policy := &pb.MeshAuthorizationPolicy{
			Name:            p.Name,
			TargetService:   p.Spec.TargetRef.Name,
			TargetNamespace: targetNS,
			Action:          p.Spec.MeshAuthorization.Action,
			Rules:           convertMeshAuthzRules(p.Spec.MeshAuthorization.Rules),
		}
		policies = append(policies, policy)
	}

	return policies
}

// convertMeshAuthzRules converts CRD MeshAuthorizationRule slices to proto.
func convertMeshAuthzRules(rules []novaedgev1alpha1.MeshAuthorizationRule) []*pb.MeshAuthorizationRule {
	result := make([]*pb.MeshAuthorizationRule, 0, len(rules))
	for _, r := range rules {
		rule := &pb.MeshAuthorizationRule{
			From: convertMeshSources(r.From),
			To:   convertMeshDestinations(r.To),
		}
		result = append(result, rule)
	}
	return result
}

// convertMeshSources converts CRD MeshSource slices to proto.
func convertMeshSources(sources []novaedgev1alpha1.MeshSource) []*pb.MeshSource {
	result := make([]*pb.MeshSource, 0, len(sources))
	for _, s := range sources {
		result = append(result, &pb.MeshSource{
			Namespaces:      s.Namespaces,
			ServiceAccounts: s.ServiceAccounts,
			SpiffeIds:       s.SpiffeIDs,
		})
	}
	return result
}

// convertMeshDestinations converts CRD MeshDestination slices to proto.
func convertMeshDestinations(dests []novaedgev1alpha1.MeshDestination) []*pb.MeshDestination {
	result := make([]*pb.MeshDestination, 0, len(dests))
	for _, d := range dests {
		result = append(result, &pb.MeshDestination{
			Methods: d.Methods,
			Paths:   d.Paths,
		})
	}
	return result
}

// enhanceWithFederation populates FederationMetadata on the snapshot using the
// configured FederationStateProvider. This stamps every outbound snapshot with
// the federation ID, origin controller name, and the current vector clock so
// that agents and peer controllers can detect staleness and conflicts.
func (b *Builder) enhanceWithFederation(snapshot *pb.ConfigSnapshot) {
	fp := b.federationProvider
	snapshot.FederationMetadata = &pb.FederationMetadata{
		FederationId:     fp.GetFederationID(),
		OriginController: fp.GetLocalMemberName(),
		VectorClock:      fp.GetVectorClock(),
	}
}

// buildWANLinks builds SD-WAN WAN link configurations from pre-fetched ProxyWANLink CRDs.
func (b *Builder) buildWANLinks(bc *buildContext) []*pb.WANLink {
	links := make([]*pb.WANLink, 0, len(bc.wanLinks))
	for i := range bc.wanLinks {
		link := &bc.wanLinks[i]
		pbLink := &pb.WANLink{
			Name:      link.Name,
			Namespace: link.Namespace,
			Site:      link.Spec.Site,
			Iface:     link.Spec.Interface,
			Provider:  link.Spec.Provider,
			Bandwidth: link.Spec.Bandwidth,
			Cost:      link.Spec.Cost,
			Role:      string(link.Spec.Role),
		}

		if link.Spec.SLA != nil {
			pbLink.Sla = &pb.WANLinkSLA{}
			if link.Spec.SLA.MaxLatency != nil {
				pbLink.Sla.MaxLatencyMs = link.Spec.SLA.MaxLatency.Milliseconds()
			}
			if link.Spec.SLA.MaxJitter != nil {
				pbLink.Sla.MaxJitterMs = link.Spec.SLA.MaxJitter.Milliseconds()
			}
			if link.Spec.SLA.MaxPacketLoss != nil {
				pbLink.Sla.MaxPacketLoss = *link.Spec.SLA.MaxPacketLoss
			}
		}

		if link.Spec.TunnelEndpoint != nil {
			pbLink.TunnelEndpoint = &pb.WANTunnelEndpoint{
				PublicIp: link.Spec.TunnelEndpoint.PublicIP,
				Port:     link.Spec.TunnelEndpoint.Port,
			}
		}

		links = append(links, pbLink)
	}

	return links
}

// buildWANPolicies builds SD-WAN WAN policy configurations from pre-fetched ProxyWANPolicy CRDs.
func (b *Builder) buildWANPolicies(bc *buildContext) []*pb.WANPolicy {
	policies := make([]*pb.WANPolicy, 0, len(bc.wanPolicies))
	for i := range bc.wanPolicies {
		p := &bc.wanPolicies[i]
		pbPolicy := &pb.WANPolicy{
			Name:      p.Name,
			Namespace: p.Namespace,
		}

		if len(p.Spec.Match.Hosts) > 0 || len(p.Spec.Match.Paths) > 0 || len(p.Spec.Match.Headers) > 0 {
			pbPolicy.Match = &pb.WANPolicyMatch{
				Hosts:   p.Spec.Match.Hosts,
				Paths:   p.Spec.Match.Paths,
				Headers: p.Spec.Match.Headers,
			}
		}

		pbPolicy.PathSelection = &pb.WANPathSelection{
			Strategy:  string(p.Spec.PathSelection.Strategy),
			Failover:  p.Spec.PathSelection.Failover,
			DscpClass: p.Spec.PathSelection.DSCPClass,
		}

		policies = append(policies, pbPolicy)
	}

	return policies
}
