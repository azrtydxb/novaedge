// Package dataplane provides a Go gRPC client for communicating with the
// Rust dataplane daemon over a Unix domain socket.
package dataplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"go.uber.org/zap"

	pb "github.com/azrtydxb/novaedge/api/proto/dataplane"
	configpb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

var errDataplaneSyncRejected = errors.New("dataplane sync rejected")

// safeUint64 converts an int64 to uint64, clamping negative values to 0.
func safeUint64(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}

// safeInt32ToUint32 converts an int32 to uint32, clamping negative values to 0.
func safeInt32ToUint32(v int32) uint32 {
	if v < 0 {
		return 0
	}
	return uint32(v)
}

// defaultBindAddress is the address used for gateway and L4 listener binds.
// Using "::" (IPv6 any) enables dual-stack support — on most systems this
// accepts both IPv4 and IPv6 connections (fixes #942).
const defaultBindAddress = "::"

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
		return fmt.Errorf("%w: %s", errDataplaneSyncRejected, resp.GetMessage())
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
				BindAddress: defaultBindAddress,
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
	case configpb.Protocol_TLS, configpb.Protocol_PROTOCOL_UNSPECIFIED:
		return pb.GatewayProtocol_GATEWAY_PROTOCOL_UNSPECIFIED
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

// translateRouteRule translates a single config RouteRule into one or more
// dataplane RouteConfig entries. Following the Envoy pattern, each match
// entry fans out into a separate RouteConfig so that all match conditions
// are evaluated by the dataplane (fixes #935).
func translateRouteRule(rt *configpb.Route, ruleIdx int, rule *configpb.RouteRule, middlewareRefs []string) []*pb.RouteConfig {
	matches := rule.GetMatches()
	if len(matches) == 0 {
		// No matches — produce a single route with no match predicates.
		matches = []*configpb.RouteMatch{nil}
	}

	results := make([]*pb.RouteConfig, 0, len(matches))
	for matchIdx, m := range matches {
		name := fmt.Sprintf("%s/rule-%d", rt.GetName(), ruleIdx)
		if len(matches) > 1 {
			name = fmt.Sprintf("%s/rule-%d/match-%d", rt.GetName(), ruleIdx, matchIdx)
		}

		dpRoute := &pb.RouteConfig{
			Name:           name,
			Hostnames:      rt.GetHostnames(),
			MiddlewareRefs: middlewareRefs,
		}

		// Apply match predicates (path, headers, method).
		if m != nil {
			if m.GetPath() != nil {
				dpRoute.PathMatch = &pb.PathMatch{
					MatchType: translatePathMatchType(m.GetPath().GetType()),
					Value:     m.GetPath().GetValue(),
				}
			}
			for _, hdr := range m.GetHeaders() {
				dpRoute.HeaderMatches = append(dpRoute.HeaderMatches, &pb.HeaderMatch{
					Name:  hdr.GetName(),
					Value: hdr.GetValue(),
				})
			}
			if method := m.GetMethod(); method != "" {
				dpRoute.Methods = append(dpRoute.Methods, method)
			}
		}

		for _, br := range rule.GetBackendRefs() {
			dpRoute.BackendRefs = append(dpRoute.BackendRefs, &pb.BackendRef{
				ClusterName: br.GetName(),
				Weight:      uint32(br.GetWeight()), //nolint:gosec // proto field
			})
		}

		if rule.GetRetry() != nil {
			dpRoute.Retry = &pb.RetryPolicy{
				MaxRetries:      uint32(rule.GetRetry().GetMaxRetries()), //nolint:gosec // proto field
				PerTryTimeoutMs: safeUint64(rule.GetRetry().GetPerTryTimeoutMs()),
				RetryOn:         rule.GetRetry().GetRetryOn(),
				BackoffBaseMs:   safeUint64(rule.GetRetry().GetBackoffBaseMs()),
			}
		}

		if lim := rule.GetLimits(); lim != nil && lim.GetRequestTimeoutMs() > 0 {
			dpRoute.TimeoutMs = safeUint64(lim.GetRequestTimeoutMs())
		}

		applyRouteFilters(dpRoute, rule.GetFilters())

		if rule.GetMirrorBackend() != nil && rule.GetMirrorBackend().GetName() != "" {
			dpRoute.MirrorCluster = rule.GetMirrorBackend().GetName()
			if rule.GetMirrorPercent() > 0 {
				dpRoute.MirrorPercent = uint32(rule.GetMirrorPercent()) //nolint:gosec // proto field
			} else {
				dpRoute.MirrorPercent = 100
			}
		}

		results = append(results, dpRoute)
	}

	return results
}

// applyRouteFilters applies rewrite_path and add_headers filters to a RouteConfig.
func applyRouteFilters(dpRoute *pb.RouteConfig, filters []*configpb.RouteFilter) {
	for _, f := range filters {
		if f.GetType() == configpb.RouteFilterType_URL_REWRITE && f.GetRewritePath() != "" && dpRoute.RewritePath == "" {
			dpRoute.RewritePath = f.GetRewritePath()
		}
		if f.GetType() == configpb.RouteFilterType_ADD_HEADER && len(f.GetAddHeaders()) > 0 && len(dpRoute.AddHeaders) == 0 {
			dpRoute.AddHeaders = make(map[string]string)
			for _, hdr := range f.GetAddHeaders() {
				dpRoute.AddHeaders[hdr.GetName()] = hdr.GetValue()
			}
		}
	}
}

func translateRoutes(routes []*configpb.Route) []*pb.RouteConfig {
	var result []*pb.RouteConfig
	for _, rt := range routes {
		var middlewareRefs []string
		for _, mw := range rt.GetPipeline().GetMiddleware() {
			middlewareRefs = append(middlewareRefs, mw.GetName())
		}

		for ruleIdx, rule := range rt.GetRules() {
			result = append(result, translateRouteRule(rt, ruleIdx, rule, middlewareRefs)...)
		}
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
	case configpb.PathMatchType_PATH_MATCH_TYPE_UNSPECIFIED:
		return pb.PathMatchType_PATH_MATCH_TYPE_UNSPECIFIED
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
	result := make([]*pb.ClusterConfig, 0, len(clusters))
	for _, cl := range clusters {
		dpCluster := &pb.ClusterConfig{
			Name:             cl.GetName(),
			LbAlgorithm:      translateLBAlgorithm(cl.GetLbPolicy()),
			ConnectTimeoutMs: safeUint64(cl.GetConnectTimeoutMs()),
		}

		// Map endpoints for this cluster.
		// Endpoint map keys use "namespace/name" format (set by snapshot builder).
		clusterKey := cl.GetNamespace() + "/" + cl.GetName()
		if epList, ok := endpoints[clusterKey]; ok && epList != nil {
			for _, ep := range epList.GetEndpoints() {
				weight := uint32(ep.GetWeight()) //nolint:gosec // proto field
				if weight == 0 {
					weight = 1 // default weight
				}
				dpCluster.Endpoints = append(dpCluster.Endpoints, &pb.Endpoint{
					Ip:      ep.GetAddress(),
					Port:    uint32(ep.GetPort()), //nolint:gosec // proto field
					Weight:  weight,
					Healthy: ep.GetReady(),
				})
			}
		}

		// Translate health check.
		if hc := cl.GetHealthCheck(); hc != nil {
			dpCluster.HealthCheck = &pb.HealthCheckConfig{
				IntervalMs:         safeUint64(hc.GetIntervalMs()),
				TimeoutMs:          safeUint64(hc.GetTimeoutMs()),
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
				IdleTimeoutMs:    safeUint64(pool.GetIdleConnTimeoutMs()),
				ConnectTimeoutMs: safeUint64(cl.GetConnectTimeoutMs()),
			}
		}

		// Translate backend TLS.
		if btls := cl.GetTls(); btls != nil && btls.GetEnabled() {
			dpCluster.BackendTls = &pb.TLSConfig{
				CaPem:              btls.GetCaCert(),
				InsecureSkipVerify: btls.GetInsecureSkipVerify(),
			}
		}

		// Translate session affinity.
		if sa := cl.GetSessionAffinity(); sa != nil {
			dpCluster.SessionAffinity = &pb.SessionAffinityConfig{
				Type:             sa.GetType(),
				CookieName:       sa.GetCookieName(),
				CookieTtlSeconds: sa.GetCookieTtlSeconds(),
				CookiePath:       sa.GetCookiePath(),
				CookieSecure:     sa.GetCookieSecure(),
				CookieSameSite:   sa.GetCookieSameSite(),
				HeaderName:       sa.GetHeaderName(),
			}
		}

		// Translate outlier detection.
		if od := cl.GetOutlierDetection(); od != nil {
			dpCluster.OutlierDetection = &pb.OutlierDetectionConfig{
				IntervalMs:               safeUint64(od.GetIntervalMs()),
				Consecutive_5XxThreshold: uint32(od.GetConsecutive_5XxThreshold()), //nolint:gosec // proto field
				BaseEjectionDurationMs:   safeUint64(od.GetBaseEjectionDurationMs()),
				MaxEjectionPercent:       uint32(od.GetMaxEjectionPercent()),     //nolint:gosec // proto field
				SuccessRateMinHosts:      uint32(od.GetSuccessRateMinHosts()),    //nolint:gosec // proto field
				SuccessRateMinRequests:   uint32(od.GetSuccessRateMinRequests()), //nolint:gosec // proto field
				SuccessRateStdevFactor:   od.GetSuccessRateStdevFactor(),
			}
		}

		// Translate slow start.
		if ss := cl.GetSlowStart(); ss != nil {
			dpCluster.SlowStart = &pb.SlowStartConfig{
				WindowMs:   safeUint64(ss.GetWindowMs()),
				Aggression: ss.GetAggression(),
			}
		}

		// Translate upstream proxy protocol (#841).
		if upp := cl.GetUpstreamProxyProtocol(); upp != nil && upp.GetEnabled() {
			dpCluster.UpstreamProxyProtocol = &pb.UpstreamProxyProtocolConfig{
				Enabled: upp.GetEnabled(),
				Version: safeInt32ToUint32(upp.GetVersion()),
			}
		}

		// Translate backend protocol (#843).
		if cl.GetProtocol() != "" {
			dpCluster.Protocol = cl.GetProtocol()
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
	case configpb.LoadBalancingPolicy_SOURCE_HASH:
		return pb.LBAlgorithm_LB_ALGORITHM_SOURCE_HASH
	case configpb.LoadBalancingPolicy_STICKY:
		return pb.LBAlgorithm_LB_ALGORITHM_STICKY
	case configpb.LoadBalancingPolicy_LB_POLICY_UNSPECIFIED:
		return pb.LBAlgorithm_LB_ALGORITHM_ROUND_ROBIN
	default:
		return pb.LBAlgorithm_LB_ALGORITHM_ROUND_ROBIN
	}
}

// ---------------------------------------------------------------------------
// L4 Listener translation
// ---------------------------------------------------------------------------

func translateL4Listeners(listeners []*configpb.L4Listener) []*pb.L4ListenerConfig {
	result := make([]*pb.L4ListenerConfig, 0, len(listeners))
	for _, l4 := range listeners {
		dpL4 := &pb.L4ListenerConfig{
			Name:        l4.GetName(),
			BindAddress: defaultBindAddress,
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
			dpL4.IdleTimeoutMs = safeUint64(l4.GetTcpConfig().GetIdleTimeoutMs())
			dpL4.ConnectTimeoutMs = safeUint64(l4.GetTcpConfig().GetConnectTimeoutMs())
		}
		if l4.GetUdpConfig() != nil {
			dpL4.IdleTimeoutMs = safeUint64(l4.GetUdpConfig().GetSessionTimeoutMs())
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
	case configpb.Protocol_PROTOCOL_UNSPECIFIED, configpb.Protocol_HTTP, configpb.Protocol_HTTPS, configpb.Protocol_HTTP3:
		return pb.L4Protocol_L4_PROTOCOL_UNSPECIFIED
	default:
		return pb.L4Protocol_L4_PROTOCOL_UNSPECIFIED
	}
}

// ---------------------------------------------------------------------------
// Policy translation
// ---------------------------------------------------------------------------

// policyConfigTranslator translates the config-specific portion of a policy
// and sets the Config oneof field on the provided dpPol.
type policyConfigTranslator func(pol *configpb.Policy, dpPol *pb.PolicyConfig)

// policyConfigTranslators maps config policy types to their dataplane translators.
var policyConfigTranslators = map[configpb.PolicyType]policyConfigTranslator{
	configpb.PolicyType_RATE_LIMIT: func(pol *configpb.Policy, dpPol *pb.PolicyConfig) {
		if rl := pol.GetRateLimit(); rl != nil {
			dpPol.Config = &pb.PolicyConfig_RateLimit{
				RateLimit: &pb.RateLimitPolicyConfig{
					RequestsPerSecond: uint32(rl.GetRequestsPerSecond()), //nolint:gosec // proto field
					Burst:             uint32(rl.GetBurst()),             //nolint:gosec // proto field
					Key:               rl.GetKey(),
				},
			}
		}
	},
	configpb.PolicyType_JWT: func(pol *configpb.Policy, dpPol *pb.PolicyConfig) {
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
	},
	configpb.PolicyType_BASIC_AUTH: func(pol *configpb.Policy, dpPol *pb.PolicyConfig) {
		if ba := pol.GetBasicAuth(); ba != nil {
			dpPol.Config = &pb.PolicyConfig_BasicAuth{
				BasicAuth: &pb.BasicAuthPolicyConfig{
					Realm:              ba.GetRealm(),
					Htpasswd:           ba.GetHtpasswd(),
					StripAuthorization: ba.GetStripAuth(),
				},
			}
		}
	},
	configpb.PolicyType_FORWARD_AUTH: func(pol *configpb.Policy, dpPol *pb.PolicyConfig) {
		if fa := pol.GetForwardAuth(); fa != nil {
			faCfg := &pb.ForwardAuthPolicyConfig{
				Address:             fa.GetAddress(),
				AuthRequestHeaders:  fa.GetAuthHeaders(),
				AuthResponseHeaders: fa.GetResponseHeaders(),
			}
			if fa.GetTimeoutMs() > 0 {
				faCfg.TimeoutMs = safeUint64(fa.GetTimeoutMs())
			}
			dpPol.Config = &pb.PolicyConfig_ForwardAuth{ForwardAuth: faCfg}
		}
	},
	configpb.PolicyType_WAF: func(pol *configpb.Policy, dpPol *pb.PolicyConfig) {
		if waf := pol.GetWaf(); waf != nil {
			dpPol.Config = &pb.PolicyConfig_Waf{
				Waf: &pb.WAFPolicyConfig{
					Enabled:          waf.GetEnabled(),
					Mode:             waf.GetMode(),
					ParanoiaLevel:    uint32(waf.GetParanoiaLevel()),    //nolint:gosec // proto field
					AnomalyThreshold: uint32(waf.GetAnomalyThreshold()), //nolint:gosec // proto field
					RuleExclusions:   waf.GetRuleExclusions(),
					MaxBodySize:      safeUint64(waf.GetMaxBodySize()),
				},
			}
		}
	},
	configpb.PolicyType_CORS: func(pol *configpb.Policy, dpPol *pb.PolicyConfig) {
		if cors := pol.GetCors(); cors != nil {
			dpPol.Config = &pb.PolicyConfig_Cors{
				Cors: &pb.CORSPolicyConfig{
					AllowOrigins:     cors.GetAllowOrigins(),
					AllowMethods:     cors.GetAllowMethods(),
					AllowHeaders:     cors.GetAllowHeaders(),
					ExposeHeaders:    cors.GetExposeHeaders(),
					AllowCredentials: cors.GetAllowCredentials(),
					MaxAgeSeconds:    safeUint64(cors.GetMaxAgeSeconds()),
				},
			}
		}
	},
	configpb.PolicyType_OIDC: func(pol *configpb.Policy, dpPol *pb.PolicyConfig) {
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
	},
	configpb.PolicyType_SECURITY_HEADERS: func(pol *configpb.Policy, dpPol *pb.PolicyConfig) {
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
	},
}

// translateIPListPolicy translates IP allow/deny list policies onto dpPol.
func translateIPListPolicy(pol *configpb.Policy, dpPol *pb.PolicyConfig) {
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
}

func translatePolicies(policies []*configpb.Policy) []*pb.PolicyConfig {
	result := make([]*pb.PolicyConfig, 0, len(policies))
	for _, pol := range policies {
		dpPol := &pb.PolicyConfig{
			Name:       pol.GetName(),
			PolicyType: translatePolicyType(pol.GetType()),
		}

		pType := pol.GetType()
		switch pType {
		case configpb.PolicyType_IP_ALLOW_LIST, configpb.PolicyType_IP_DENY_LIST:
			translateIPListPolicy(pol, dpPol)
		case configpb.PolicyType_WASM_PLUGIN,
			configpb.PolicyType_DISTRIBUTED_RATE_LIMIT,
			configpb.PolicyType_MESH_AUTHORIZATION,
			configpb.PolicyType_POLICY_TYPE_UNSPECIFIED:
			// These policy types have no dataplane equivalent yet; skip.
		case configpb.PolicyType_RATE_LIMIT,
			configpb.PolicyType_JWT,
			configpb.PolicyType_CORS,
			configpb.PolicyType_SECURITY_HEADERS,
			configpb.PolicyType_WAF,
			configpb.PolicyType_BASIC_AUTH,
			configpb.PolicyType_FORWARD_AUTH,
			configpb.PolicyType_OIDC:
			if translator, ok := policyConfigTranslators[pType]; ok {
				translator(pol, dpPol)
			}
		default:
			if translator, ok := policyConfigTranslators[pType]; ok {
				translator(pol, dpPol)
			}
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
	case configpb.PolicyType_POLICY_TYPE_UNSPECIFIED,
		configpb.PolicyType_DISTRIBUTED_RATE_LIMIT,
		configpb.PolicyType_WASM_PLUGIN,
		configpb.PolicyType_MESH_AUTHORIZATION:
		return pb.PolicyType_POLICY_TYPE_UNSPECIFIED
	default:
		return pb.PolicyType_POLICY_TYPE_UNSPECIFIED
	}
}

// ---------------------------------------------------------------------------
// WAN Link translation
// ---------------------------------------------------------------------------

func translateWANLinks(wanLinks []*configpb.WANLink) []*pb.WANLinkConfig {
	result := make([]*pb.WANLinkConfig, 0, len(wanLinks))
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
				MaxLatencyMs:     safeUint64(sla.GetMaxLatencyMs()),
				MaxJitterMs:      safeUint64(sla.GetMaxJitterMs()),
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
