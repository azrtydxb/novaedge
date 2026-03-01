// Package dataplane provides a Go gRPC client for communicating with the
// Rust dataplane daemon over a Unix domain socket.
package dataplane

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/api/proto/dataplane"
	configpb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Translator converts Go agent ConfigSnapshot into dataplane gRPC calls.
// It wraps a Client and provides a high-level Sync operation that pushes
// the full configuration to the Rust dataplane via ApplyConfig RPC.
type Translator struct {
	client *Client
	logger *zap.Logger
	mu     sync.Mutex
}

// NewTranslator creates a new Translator that uses the given Client to
// communicate with the Rust dataplane daemon.
func NewTranslator(client *Client, logger *zap.Logger) *Translator {
	return &Translator{
		client: client,
		logger: logger,
	}
}

// Sync translates the ConfigSnapshot and pushes it to the dataplane.
// It is safe for concurrent use; only one Sync runs at a time.
func (t *Translator) Sync(ctx context.Context, snapshot *configpb.ConfigSnapshot) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	req := TranslateSnapshot(snapshot)

	t.logger.Info("Syncing config to dataplane",
		zap.String("version", req.GetVersion()),
		zap.Int("gateways", len(req.GetGateways())),
		zap.Int("routes", len(req.GetRoutes())),
		zap.Int("clusters", len(req.GetClusters())),
		zap.Int("vips", len(req.GetVips())),
		zap.Int("l4_listeners", len(req.GetL4Listeners())),
		zap.Int("policies", len(req.GetPolicies())),
		zap.Int("wan_links", len(req.GetWanLinks())),
		zap.Bool("mesh_enabled", req.GetMeshConfig().GetEnabled()),
	)

	resp, err := t.client.ApplyConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("dataplane sync: %w", err)
	}

	if resp.GetStatus() == pb.OperationStatus_ERROR {
		return fmt.Errorf("dataplane sync rejected: %s", resp.GetMessage())
	}

	t.logger.Info("Dataplane config synced",
		zap.String("applied_version", resp.GetAppliedVersion()),
		zap.String("status", resp.GetStatus().String()),
	)

	return nil
}

// TranslateSnapshot converts a ConfigSnapshot (from the control-plane config
// proto) into an ApplyConfigRequest suitable for pushing to the Rust dataplane.
func TranslateSnapshot(snapshot *configpb.ConfigSnapshot) *pb.ApplyConfigRequest {
	if snapshot == nil {
		return &pb.ApplyConfigRequest{}
	}

	req := &pb.ApplyConfigRequest{
		Version: snapshot.GetVersion(),
	}

	req.Gateways = translateGateways(snapshot.GetGateways())
	req.Routes = translateRoutes(snapshot.GetRoutes())
	req.Clusters = translateClusters(snapshot.GetClusters(), snapshot.GetEndpoints())
	req.Vips = translateVIPs(snapshot.GetVipAssignments())
	req.L4Listeners = translateL4Listeners(snapshot.GetL4Listeners())
	req.Policies = translatePolicies(snapshot.GetPolicies())
	req.WanLinks = translateWANLinks(snapshot.GetWanLinks())

	if snapshot.GetMeshTlsConfig() != nil || len(snapshot.GetInternalServices()) > 0 {
		req.MeshConfig = translateMeshConfig(snapshot)
	}

	return req
}

// ---------------------------------------------------------------------------
// Gateway translation
// ---------------------------------------------------------------------------

func translateGateways(gateways []*configpb.Gateway) []*pb.GatewayConfig {
	var result []*pb.GatewayConfig
	for _, gw := range gateways {
		for _, lis := range gw.GetListeners() {
			dpGw := &pb.GatewayConfig{
				Name:        gw.GetName() + "/" + lis.GetName(),
				BindAddress: "0.0.0.0",
				Port:        uint32(lis.GetPort()), //nolint:gosec // proto field conversion
				Protocol:    translateGatewayProtocol(lis.GetProtocol()),
				Hostnames:   lis.GetHostnames(),
			}

			if lis.GetMaxRequestBodyBytes() > 0 {
				dpGw.MaxRequestBodyBytes = uint64(lis.GetMaxRequestBodyBytes()) //nolint:gosec // proto field
			}

			if lis.GetTls() != nil {
				dpGw.TlsConfig = translateTLS(lis.GetTls())
			}

			if lis.GetProxyProtocol() != nil {
				dpGw.ProxyProtocol = &pb.ProxyProtocolConfig{
					Enabled: lis.GetProxyProtocol().GetEnabled(),
					Version: lis.GetProxyProtocol().GetVersion(),
				}
			}

			result = append(result, dpGw)
		}
	}
	return result
}

func translateGatewayProtocol(proto configpb.Protocol) pb.GatewayProtocol {
	switch proto {
	case configpb.Protocol_HTTP:
		return pb.GatewayProtocol_GATEWAY_PROTOCOL_HTTP
	case configpb.Protocol_HTTPS:
		return pb.GatewayProtocol_GATEWAY_PROTOCOL_HTTPS
	case configpb.Protocol_HTTP3:
		return pb.GatewayProtocol_GATEWAY_PROTOCOL_HTTP3
	case configpb.Protocol_TCP:
		return pb.GatewayProtocol_GATEWAY_PROTOCOL_TCP
	case configpb.Protocol_UDP:
		return pb.GatewayProtocol_GATEWAY_PROTOCOL_UDP
	default:
		return pb.GatewayProtocol_GATEWAY_PROTOCOL_UNSPECIFIED
	}
}

func translateTLS(tls *configpb.TLSConfig) *pb.TLSConfig {
	if tls == nil {
		return nil
	}
	return &pb.TLSConfig{
		CertPem:    tls.GetCert(),
		KeyPem:     tls.GetKey(),
		CaPem:      tls.GetCaCert(),
		MinVersion: tls.GetMinVersion(),
	}
}

// ---------------------------------------------------------------------------
// Route translation
// ---------------------------------------------------------------------------

func translateRoutes(routes []*configpb.Route) []*pb.RouteConfig {
	var result []*pb.RouteConfig
	for _, rt := range routes {
		dpRoute := &pb.RouteConfig{
			Name:      rt.GetName(),
			Hostnames: rt.GetHostnames(),
		}

		// Translate rules into backend refs and path/header matches.
		for _, rule := range rt.GetRules() {
			// Translate matches into path match (take first match's path).
			for _, match := range rule.GetMatches() {
				if match.GetPath() != nil && dpRoute.PathMatch == nil {
					dpRoute.PathMatch = &pb.PathMatch{
						MatchType: translatePathMatchType(match.GetPath().GetType()),
						Value:     match.GetPath().GetValue(),
					}
				}
				for _, hdr := range match.GetHeaders() {
					dpRoute.HeaderMatches = append(dpRoute.HeaderMatches, &pb.HeaderMatch{
						Name:  hdr.GetName(),
						Value: hdr.GetValue(),
					})
				}
			}

			// Translate backend refs.
			for _, br := range rule.GetBackendRefs() {
				dpRoute.BackendRefs = append(dpRoute.BackendRefs, &pb.BackendRef{
					ClusterName: br.GetName(),
					Weight:      uint32(br.GetWeight()), //nolint:gosec // proto field
				})
			}

			// Translate retry config.
			if rule.GetRetry() != nil && dpRoute.Retry == nil {
				dpRoute.Retry = &pb.RetryPolicy{
					MaxRetries:      uint32(rule.GetRetry().GetMaxRetries()), //nolint:gosec // proto field
					PerTryTimeoutMs: uint64(rule.GetRetry().GetPerTryTimeoutMs()),
					RetryOn:         rule.GetRetry().GetRetryOn(),
					BackoffBaseMs:   uint64(rule.GetRetry().GetBackoffBaseMs()),
				}
			}
		}

		// Map middleware pipeline references.
		for _, mw := range rt.GetPipeline().GetMiddleware() {
			dpRoute.MiddlewareRefs = append(dpRoute.MiddlewareRefs, mw.GetName())
		}
		// Map timeout from first rule's limits.
		if len(rt.GetRules()) > 0 {
			if lim := rt.GetRules()[0].GetLimits(); lim != nil && lim.GetRequestTimeoutMs() > 0 {
				dpRoute.TimeoutMs = uint64(lim.GetRequestTimeoutMs())
			}
		}

		result = append(result, dpRoute)
	}
	return result
}

func translatePathMatchType(t configpb.PathMatchType) pb.PathMatchType {
	switch t {
	case configpb.PathMatchType_EXACT:
		return pb.PathMatchType_PATH_MATCH_EXACT
	case configpb.PathMatchType_PATH_PREFIX:
		return pb.PathMatchType_PATH_MATCH_PREFIX
	case configpb.PathMatchType_REGULAR_EXPRESSION:
		return pb.PathMatchType_PATH_MATCH_REGEX
	default:
		return pb.PathMatchType_PATH_MATCH_TYPE_UNSPECIFIED
	}
}

// ---------------------------------------------------------------------------
// Cluster translation
// ---------------------------------------------------------------------------

func translateClusters(
	clusters []*configpb.Cluster,
	endpoints map[string]*configpb.EndpointList,
) []*pb.ClusterConfig {
	var result []*pb.ClusterConfig
	for _, cl := range clusters {
		dpCluster := &pb.ClusterConfig{
			Name:             cl.GetName(),
			LbAlgorithm:      translateLBAlgorithm(cl.GetLbPolicy()),
			ConnectTimeoutMs: uint64(cl.GetConnectTimeoutMs()),
		}

		// Map endpoints for this cluster.
		if epList, ok := endpoints[cl.GetName()]; ok && epList != nil {
			for _, ep := range epList.GetEndpoints() {
				dpCluster.Endpoints = append(dpCluster.Endpoints, &pb.Endpoint{
					Ip:      ep.GetAddress(),
					Port:    uint32(ep.GetPort()), //nolint:gosec // proto field
					Healthy: ep.GetReady(),
				})
			}
		}

		// Translate health check.
		if hc := cl.GetHealthCheck(); hc != nil {
			dpCluster.HealthCheck = &pb.HealthCheckConfig{
				IntervalMs:         uint64(hc.GetIntervalMs()),
				TimeoutMs:          uint64(hc.GetTimeoutMs()),
				HealthyThreshold:   uint32(hc.GetHealthyThreshold()),   //nolint:gosec // proto field
				UnhealthyThreshold: uint32(hc.GetUnhealthyThreshold()), //nolint:gosec // proto field
				HttpPath:           hc.GetHttpPath(),
			}
		}

		// Translate circuit breaker.
		if cb := cl.GetCircuitBreaker(); cb != nil {
			dpCluster.CircuitBreaker = &pb.CircuitBreakerConfig{
				MaxConnections:     uint32(cb.GetMaxConnections()),     //nolint:gosec // proto field
				MaxPendingRequests: uint32(cb.GetMaxPendingRequests()), //nolint:gosec // proto field
				MaxRequests:        uint32(cb.GetMaxRequests()),        //nolint:gosec // proto field
				MaxRetries:         uint32(cb.GetMaxRetries()),         //nolint:gosec // proto field
			}
		}

		// Translate connection pool.
		if pool := cl.GetConnectionPool(); pool != nil {
			dpCluster.ConnectionPool = &pb.ConnectionPoolConfig{
				MaxConnections:   uint32(pool.GetMaxConnsPerHost()),     //nolint:gosec // proto field
				MaxIdle:          uint32(pool.GetMaxIdleConnsPerHost()), //nolint:gosec // proto field
				IdleTimeoutMs:    uint64(pool.GetIdleConnTimeoutMs()),
				ConnectTimeoutMs: uint64(cl.GetConnectTimeoutMs()),
			}
		}

		// Translate backend TLS.
		if btls := cl.GetTls(); btls != nil && btls.GetEnabled() {
			dpCluster.BackendTls = &pb.TLSConfig{
				CaPem:              btls.GetCaCert(),
				InsecureSkipVerify: btls.GetInsecureSkipVerify(),
			}
		}

		result = append(result, dpCluster)
	}
	return result
}

func translateLBAlgorithm(policy configpb.LoadBalancingPolicy) pb.LBAlgorithm {
	switch policy {
	case configpb.LoadBalancingPolicy_ROUND_ROBIN:
		return pb.LBAlgorithm_LB_ALGORITHM_ROUND_ROBIN
	case configpb.LoadBalancingPolicy_LEAST_CONN:
		return pb.LBAlgorithm_LB_ALGORITHM_LEAST_CONN
	case configpb.LoadBalancingPolicy_P2C:
		return pb.LBAlgorithm_LB_ALGORITHM_P2C
	case configpb.LoadBalancingPolicy_EWMA:
		return pb.LBAlgorithm_LB_ALGORITHM_EWMA
	case configpb.LoadBalancingPolicy_RING_HASH:
		return pb.LBAlgorithm_LB_ALGORITHM_RING_HASH
	case configpb.LoadBalancingPolicy_MAGLEV:
		return pb.LBAlgorithm_LB_ALGORITHM_MAGLEV
	default:
		return pb.LBAlgorithm_LB_ALGORITHM_ROUND_ROBIN
	}
}

// ---------------------------------------------------------------------------
// VIP translation
// ---------------------------------------------------------------------------

func translateVIPs(vips []*configpb.VIPAssignment) []*pb.VIPConfig {
	var result []*pb.VIPConfig
	for _, v := range vips {
		dpVIP := &pb.VIPConfig{
			Name:    v.GetVipName(),
			Address: v.GetAddress(),
			Mode:    translateVIPMode(v.GetMode()),
		}

		if bgp := v.GetBgpConfig(); bgp != nil {
			peerAddresses := make([]string, 0, len(bgp.GetPeers()))
			peerASNumbers := make([]uint32, 0, len(bgp.GetPeers()))
			peerPorts := make([]uint32, 0, len(bgp.GetPeers()))
			for _, peer := range bgp.GetPeers() {
				peerAddresses = append(peerAddresses, peer.GetAddress())
				peerASNumbers = append(peerASNumbers, peer.GetAs())
				peerPorts = append(peerPorts, peer.GetPort())
			}
			dpVIP.BgpConfig = &pb.BGPConfig{
				LocalAsn:        bgp.GetLocalAs(),
				RouterId:        bgp.GetRouterId(),
				PeerAddresses:   peerAddresses,
				PeerAsNumbers:   peerASNumbers,
				PeerPorts:       peerPorts,
				Communities:     bgp.GetCommunities(),
				LocalPreference: bgp.GetLocalPreference(),
			}
		}

		if ospf := v.GetOspfConfig(); ospf != nil {
			dpVIP.OspfAreaId = ospf.GetAreaId()
		}
		if bfd := v.GetBfdConfig(); bfd != nil {
			dpVIP.BfdEnabled = bfd.GetEnabled()
			dpVIP.BfdMultiplier = uint32(bfd.GetDetectMultiplier()) //nolint:gosec // proto field
			// BFD intervals in config are strings like "300ms"; parse to milliseconds.
			if d, err := time.ParseDuration(bfd.GetDesiredMinTxInterval()); err == nil {
				dpVIP.BfdIntervalMs = uint64(d.Milliseconds()) //nolint:gosec // duration to ms
			}
		}

		result = append(result, dpVIP)
	}
	return result
}

func translateVIPMode(mode configpb.VIPMode) pb.VIPMode {
	switch mode {
	case configpb.VIPMode_L2_ARP:
		return pb.VIPMode_VIP_MODE_L2
	case configpb.VIPMode_BGP:
		return pb.VIPMode_VIP_MODE_BGP
	case configpb.VIPMode_OSPF:
		return pb.VIPMode_VIP_MODE_OSPF
	default:
		return pb.VIPMode_VIP_MODE_UNSPECIFIED
	}
}

// ---------------------------------------------------------------------------
// L4 Listener translation
// ---------------------------------------------------------------------------

func translateL4Listeners(listeners []*configpb.L4Listener) []*pb.L4ListenerConfig {
	var result []*pb.L4ListenerConfig
	for _, l4 := range listeners {
		dpL4 := &pb.L4ListenerConfig{
			Name:        l4.GetName(),
			BindAddress: "0.0.0.0",
			Port:        uint32(l4.GetPort()), //nolint:gosec // proto field
			Protocol:    translateL4Protocol(l4.GetProtocol()),
		}

		// Map backend ref from the listener's backend name.
		if l4.GetBackendName() != "" {
			dpL4.BackendRefs = append(dpL4.BackendRefs, &pb.BackendRef{
				ClusterName: l4.GetBackendName(),
			})
		}

		if l4.GetTcpConfig() != nil {
			dpL4.IdleTimeoutMs = uint64(l4.GetTcpConfig().GetIdleTimeoutMs())
			dpL4.ConnectTimeoutMs = uint64(l4.GetTcpConfig().GetConnectTimeoutMs())
		}
		if l4.GetUdpConfig() != nil {
			dpL4.IdleTimeoutMs = uint64(l4.GetUdpConfig().GetSessionTimeoutMs())
		}

		result = append(result, dpL4)
	}
	return result
}

func translateL4Protocol(proto configpb.Protocol) pb.L4Protocol {
	switch proto {
	case configpb.Protocol_TCP:
		return pb.L4Protocol_L4_PROTOCOL_TCP
	case configpb.Protocol_UDP:
		return pb.L4Protocol_L4_PROTOCOL_UDP
	case configpb.Protocol_TLS:
		return pb.L4Protocol_L4_PROTOCOL_TLS_PASSTHROUGH
	default:
		return pb.L4Protocol_L4_PROTOCOL_UNSPECIFIED
	}
}

// ---------------------------------------------------------------------------
// Policy translation
// ---------------------------------------------------------------------------

func translatePolicies(policies []*configpb.Policy) []*pb.PolicyConfig {
	var result []*pb.PolicyConfig
	for _, pol := range policies {
		dpPol := &pb.PolicyConfig{
			Name:       pol.GetName(),
			PolicyType: translatePolicyType(pol.GetType()),
		}

		// Translate policy-specific configuration.
		switch pol.GetType() {
		case configpb.PolicyType_RATE_LIMIT:
			if rl := pol.GetRateLimit(); rl != nil {
				dpPol.Config = &pb.PolicyConfig_RateLimit{
					RateLimit: &pb.RateLimitPolicyConfig{
						RequestsPerSecond: uint32(rl.GetRequestsPerSecond()), //nolint:gosec // proto field
						Burst:             uint32(rl.GetBurst()),             //nolint:gosec // proto field
						Key:               rl.GetKey(),
					},
				}
			}
		case configpb.PolicyType_JWT:
			if jwt := pol.GetJwt(); jwt != nil {
				dpPol.Config = &pb.PolicyConfig_Jwt{
					Jwt: &pb.JWTPolicyConfig{
						Issuer:            jwt.GetIssuer(),
						Audiences:         jwt.GetAudience(),
						JwksUri:           jwt.GetJwksUri(),
						HeaderName:        jwt.GetHeaderName(),
						HeaderPrefix:      jwt.GetHeaderPrefix(),
						AllowedAlgorithms: jwt.GetAllowedAlgorithms(),
					},
				}
			}
		case configpb.PolicyType_BASIC_AUTH:
			if ba := pol.GetBasicAuth(); ba != nil {
				dpPol.Config = &pb.PolicyConfig_BasicAuth{
					BasicAuth: &pb.BasicAuthPolicyConfig{
						Realm:              ba.GetRealm(),
						Htpasswd:           ba.GetHtpasswd(),
						StripAuthorization: ba.GetStripAuth(),
					},
				}
			}
		case configpb.PolicyType_FORWARD_AUTH:
			if fa := pol.GetForwardAuth(); fa != nil {
				dpPol.Config = &pb.PolicyConfig_ForwardAuth{
					ForwardAuth: &pb.ForwardAuthPolicyConfig{
						Address:             fa.GetAddress(),
						AuthRequestHeaders:  fa.GetAuthHeaders(),
						AuthResponseHeaders: fa.GetResponseHeaders(),
						TimeoutMs:           uint64(fa.GetTimeoutMs()),
					},
				}
			}
		case configpb.PolicyType_WAF:
			if waf := pol.GetWaf(); waf != nil {
				dpPol.Config = &pb.PolicyConfig_Waf{
					Waf: &pb.WAFPolicyConfig{
						Enabled:          waf.GetEnabled(),
						Mode:             waf.GetMode(),
						ParanoiaLevel:    uint32(waf.GetParanoiaLevel()),    //nolint:gosec // proto field
						AnomalyThreshold: uint32(waf.GetAnomalyThreshold()), //nolint:gosec // proto field
						RuleExclusions:   waf.GetRuleExclusions(),
						MaxBodySize:      uint64(waf.GetMaxBodySize()),
					},
				}
			}
		case configpb.PolicyType_CORS:
			if cors := pol.GetCors(); cors != nil {
				dpPol.Config = &pb.PolicyConfig_Cors{
					Cors: &pb.CORSPolicyConfig{
						AllowOrigins:     cors.GetAllowOrigins(),
						AllowMethods:     cors.GetAllowMethods(),
						AllowHeaders:     cors.GetAllowHeaders(),
						ExposeHeaders:    cors.GetExposeHeaders(),
						AllowCredentials: cors.GetAllowCredentials(),
						MaxAgeSeconds:    uint64(cors.GetMaxAgeSeconds()),
					},
				}
			}
		case configpb.PolicyType_IP_ALLOW_LIST, configpb.PolicyType_IP_DENY_LIST:
			if ipList := pol.GetIpList(); ipList != nil {
				action := "allow"
				if pol.GetType() == configpb.PolicyType_IP_DENY_LIST {
					action = "deny"
				}
				dpPol.Config = &pb.PolicyConfig_IpFilter{
					IpFilter: &pb.IPFilterPolicyConfig{
						Action:       action,
						Cidrs:        ipList.GetCidrs(),
						SourceHeader: ipList.GetSourceHeader(),
					},
				}
			}
		case configpb.PolicyType_OIDC:
			if oidc := pol.GetOidc(); oidc != nil {
				dpPol.Config = &pb.PolicyConfig_Oauth2{
					Oauth2: &pb.OAuth2PolicyConfig{
						IssuerUrl:     oidc.GetIssuerUrl(),
						ClientId:      oidc.GetClientId(),
						ClientSecret:  oidc.GetClientSecret(),
						RedirectUrl:   oidc.GetRedirectUrl(),
						Scopes:        oidc.GetScopes(),
						SessionSecret: oidc.GetSessionSecret(),
					},
				}
			}
		case configpb.PolicyType_SECURITY_HEADERS:
			if sh := pol.GetSecurityHeaders(); sh != nil {
				stsValue := ""
				if hsts := sh.GetHsts(); hsts != nil && hsts.GetEnabled() {
					stsValue = fmt.Sprintf("max-age=%d", hsts.GetMaxAgeSeconds())
					if hsts.GetIncludeSubdomains() {
						stsValue += "; includeSubDomains"
					}
					if hsts.GetPreload() {
						stsValue += "; preload"
					}
				}
				dpPol.Config = &pb.PolicyConfig_SecurityHeaders{
					SecurityHeaders: &pb.SecurityHeadersPolicyConfig{
						ContentSecurityPolicy:   sh.GetContentSecurityPolicy(),
						XFrameOptions:           sh.GetXFrameOptions(),
						XContentTypeOptions:     sh.GetXContentTypeOptions(),
						StrictTransportSecurity: stsValue,
						ReferrerPolicy:          sh.GetReferrerPolicy(),
						PermissionsPolicy:       sh.GetPermissionsPolicy(),
					},
				}
			}
		case configpb.PolicyType_WASM_PLUGIN,
			configpb.PolicyType_DISTRIBUTED_RATE_LIMIT,
			configpb.PolicyType_MESH_AUTHORIZATION:
			// These policy types have no dataplane equivalent yet; skip.
		}

		result = append(result, dpPol)
	}
	return result
}

func translatePolicyType(t configpb.PolicyType) pb.PolicyType {
	switch t {
	case configpb.PolicyType_RATE_LIMIT:
		return pb.PolicyType_POLICY_TYPE_RATE_LIMIT
	case configpb.PolicyType_JWT:
		return pb.PolicyType_POLICY_TYPE_JWT
	case configpb.PolicyType_BASIC_AUTH:
		return pb.PolicyType_POLICY_TYPE_BASIC_AUTH
	case configpb.PolicyType_FORWARD_AUTH:
		return pb.PolicyType_POLICY_TYPE_FORWARD_AUTH
	case configpb.PolicyType_WAF:
		return pb.PolicyType_POLICY_TYPE_WAF
	case configpb.PolicyType_CORS:
		return pb.PolicyType_POLICY_TYPE_CORS
	case configpb.PolicyType_IP_ALLOW_LIST, configpb.PolicyType_IP_DENY_LIST:
		return pb.PolicyType_POLICY_TYPE_IP_FILTER
	case configpb.PolicyType_OIDC:
		return pb.PolicyType_POLICY_TYPE_OAUTH2
	case configpb.PolicyType_SECURITY_HEADERS:
		return pb.PolicyType_POLICY_TYPE_SECURITY_HEADERS
	default:
		return pb.PolicyType_POLICY_TYPE_UNSPECIFIED
	}
}

// ---------------------------------------------------------------------------
// WAN Link translation
// ---------------------------------------------------------------------------

func translateWANLinks(wanLinks []*configpb.WANLink) []*pb.WANLinkConfig {
	var result []*pb.WANLinkConfig
	for _, wl := range wanLinks {
		dpWL := &pb.WANLinkConfig{
			Name:      wl.GetName(),
			Interface: wl.GetIface(),
			Provider:  wl.GetProvider(),
			Priority:  uint32(wl.GetCost()), //nolint:gosec // proto field
			Gateway:   wl.GetTunnelEndpoint().GetPublicIp(),
		}

		if sla := wl.GetSla(); sla != nil {
			dpWL.SlaTarget = &pb.SLATarget{
				MaxLatencyMs:     uint64(sla.GetMaxLatencyMs()),
				MaxJitterMs:      uint64(sla.GetMaxJitterMs()),
				MaxPacketLossPct: sla.GetMaxPacketLoss(),
			}
		}

		result = append(result, dpWL)
	}
	return result
}

// ---------------------------------------------------------------------------
// Mesh config translation
// ---------------------------------------------------------------------------

func translateMeshConfig(snapshot *configpb.ConfigSnapshot) *pb.MeshConfig {
	mc := &pb.MeshConfig{
		Enabled: len(snapshot.GetInternalServices()) > 0,
	}

	if tls := snapshot.GetMeshTlsConfig(); tls != nil {
		mc.SpiffeId = tls.GetSpiffeId()
		mc.TrustDomain = tls.GetTrustDomain()
		mc.CaCertPem = tls.GetCaCertificate()
		mc.CertPem = tls.GetCertificate()
		mc.KeyPem = tls.GetPrivateKey()
	}

	// Populate intercept ports from mesh-enabled internal services.
	// Derive Enabled from the presence of mesh-enabled services (not all services).
	portSet := make(map[uint32]bool)
	for _, svc := range snapshot.GetInternalServices() {
		if !svc.GetMeshEnabled() {
			continue
		}
		for _, port := range svc.GetPorts() {
			p := uint32(port.GetPort()) //nolint:gosec // port range
			if p > 0 && !portSet[p] {
				portSet[p] = true
				mc.InterceptPorts = append(mc.InterceptPorts, p)
			}
		}
	}
	mc.Enabled = len(mc.InterceptPorts) > 0
	// Sort for deterministic config output.
	sort.Slice(mc.InterceptPorts, func(i, j int) bool { return mc.InterceptPorts[i] < mc.InterceptPorts[j] })

	// Default mTLS mode: permissive when mesh is enabled but no explicit mode set.
	if mc.Enabled && mc.MtlsMode == "" {
		mc.MtlsMode = "permissive"
	}

	return mc
}
