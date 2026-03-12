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
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/azrtydxb/novaedge/api/v1alpha1"
	"github.com/azrtydxb/novaedge/internal/pkg/convert"
	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

// convertProtocol converts NovaEdge ProtocolType to protobuf Protocol
func convertProtocol(protocol novaedgev1alpha1.ProtocolType) pb.Protocol {
	switch protocol {
	case novaedgev1alpha1.ProtocolTypeHTTP:
		return pb.Protocol_HTTP
	case novaedgev1alpha1.ProtocolTypeHTTPS:
		return pb.Protocol_HTTPS
	case novaedgev1alpha1.ProtocolTypeHTTP3:
		return pb.Protocol_HTTP3
	case novaedgev1alpha1.ProtocolTypeTCP:
		return pb.Protocol_TCP
	case novaedgev1alpha1.ProtocolTypeTLS:
		return pb.Protocol_TLS
	case novaedgev1alpha1.ProtocolTypeUDP:
		return pb.Protocol_UDP
	default:
		return pb.Protocol_PROTOCOL_UNSPECIFIED
	}
}

// convertLBPolicy converts NovaEdge LoadBalancingPolicy to protobuf LoadBalancingPolicy
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
	case novaedgev1alpha1.LBPolicyLeastConn:
		return pb.LoadBalancingPolicy_LEAST_CONN
	case novaedgev1alpha1.LBPolicySourceHash:
		return pb.LoadBalancingPolicy_SOURCE_HASH
	case novaedgev1alpha1.LBPolicySticky:
		return pb.LoadBalancingPolicy_STICKY
	default:
		return pb.LoadBalancingPolicy_LB_POLICY_UNSPECIFIED
	}
}

// convertPolicyType converts NovaEdge PolicyType to protobuf PolicyType
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
	case novaedgev1alpha1.PolicyTypeSecurityHeaders:
		return pb.PolicyType_SECURITY_HEADERS
	case novaedgev1alpha1.PolicyTypeDistributedRateLimit:
		return pb.PolicyType_DISTRIBUTED_RATE_LIMIT
	case novaedgev1alpha1.PolicyTypeWAF:
		return pb.PolicyType_WAF
	case novaedgev1alpha1.PolicyTypeWASMPlugin:
		return pb.PolicyType_WASM_PLUGIN
	case novaedgev1alpha1.PolicyTypeBasicAuth:
		return pb.PolicyType_BASIC_AUTH
	case novaedgev1alpha1.PolicyTypeForwardAuth:
		return pb.PolicyType_FORWARD_AUTH
	case novaedgev1alpha1.PolicyTypeOIDC:
		return pb.PolicyType_OIDC
	case novaedgev1alpha1.PolicyTypeMeshAuthorization:
		return pb.PolicyType_MESH_AUTHORIZATION
	default:
		return pb.PolicyType_POLICY_TYPE_UNSPECIFIED
	}
}

// convertMatches converts NovaEdge HTTPRouteMatches to protobuf RouteMatches
func convertMatches(matches []novaedgev1alpha1.HTTPRouteMatch) []*pb.RouteMatch {
	result := make([]*pb.RouteMatch, 0, len(matches))
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

// convertFilters converts NovaEdge HTTPRouteFilters to protobuf RouteFilters
func convertFilters(filters []novaedgev1alpha1.HTTPRouteFilter) []*pb.RouteFilter {
	result := make([]*pb.RouteFilter, 0, len(filters))
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

		// Handle response header filters
		for _, h := range f.ResponseAdd {
			pbFilter.ResponseAddHeaders = append(pbFilter.ResponseAddHeaders, &pb.HTTPHeader{
				Name:  h.Name,
				Value: h.Value,
			})
		}
		pbFilter.ResponseRemoveHeaders = f.ResponseRemove
		for _, h := range f.ResponseSet {
			pbFilter.ResponseSetHeaders = append(pbFilter.ResponseSetHeaders, &pb.HTTPHeader{
				Name:  h.Name,
				Value: h.Value,
			})
		}

		result = append(result, pbFilter)
	}
	return result
}

// convertPathMatchType converts NovaEdge PathMatchType to protobuf PathMatchType
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

// convertHeaderMatchType converts NovaEdge HeaderMatchType to protobuf HeaderMatchType
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

// convertFilterType converts NovaEdge HTTPRouteFilterType to protobuf RouteFilterType
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
	case novaedgev1alpha1.HTTPRouteFilterResponseAddHeader:
		return pb.RouteFilterType_RESPONSE_ADD_HEADER
	case novaedgev1alpha1.HTTPRouteFilterResponseRemoveHeader:
		return pb.RouteFilterType_RESPONSE_REMOVE_HEADER
	case novaedgev1alpha1.HTTPRouteFilterResponseSetHeader:
		return pb.RouteFilterType_RESPONSE_SET_HEADER
	case novaedgev1alpha1.HTTPRouteFilterRequestMirror:
		// Request mirroring is handled separately via route-level MirrorBackend.
		return pb.RouteFilterType_ROUTE_FILTER_TYPE_UNSPECIFIED
	default:
		return pb.RouteFilterType_ROUTE_FILTER_TYPE_UNSPECIFIED
	}
}

// convertCircuitBreaker converts NovaEdge CircuitBreaker to protobuf CircuitBreaker
func convertCircuitBreaker(cb *novaedgev1alpha1.CircuitBreaker) *pb.CircuitBreaker {
	return &pb.CircuitBreaker{
		MaxConnections:     getInt32(cb.MaxConnections),
		MaxPendingRequests: getInt32(cb.MaxPendingRequests),
		MaxRequests:        getInt32(cb.MaxRequests),
		MaxRetries:         getInt32(cb.MaxRetries),
	}
}

// convertHealthCheck converts NovaEdge HealthCheck to protobuf HealthCheck
func convertHealthCheck(hc *novaedgev1alpha1.HealthCheck) *pb.HealthCheck {
	return &pb.HealthCheck{
		IntervalMs:         durationToMillis(hc.Interval),
		TimeoutMs:          durationToMillis(hc.Timeout),
		HealthyThreshold:   getInt32(hc.HealthyThreshold),
		UnhealthyThreshold: getInt32(hc.UnhealthyThreshold),
		HttpPath:           getString(hc.HTTPPath),
	}
}

// durationToMillis converts metav1.Duration to milliseconds
func durationToMillis(d metav1.Duration) int64 {
	return d.Milliseconds()
}

// durationToSeconds converts metav1.Duration pointer to seconds
func durationToSeconds(d *metav1.Duration) int64 {
	if d == nil {
		return 0
	}
	return int64(d.Seconds())
}

// getNamespace returns the namespace or defaultNs if not set
func getNamespace(ns *string, defaultNs string) string {
	if ns != nil && *ns != "" {
		return *ns
	}
	return defaultNs
}

// getWeight returns the weight or default value of 1
func getWeight(w *int32) int32 {
	if w != nil {
		return *w
	}
	return 1
}

// getInt32 returns the int32 value or 0 if nil
func getInt32(v *int32) int32 {
	if v != nil {
		return *v
	}
	return 0
}

// getString returns the string value or empty string if nil
func getString(v *string) string {
	if v != nil {
		return *v
	}
	return ""
}

// convertSessionAffinity converts NovaEdge SessionAffinityConfig to protobuf SessionAffinityConfig
func convertSessionAffinity(sa *novaedgev1alpha1.SessionAffinityConfig) *pb.SessionAffinityConfig {
	if sa == nil {
		return nil
	}

	affinityType := "cookie"
	switch sa.Type {
	case "Cookie":
		affinityType = "cookie"
	case "Header":
		affinityType = "header"
	case "SourceIP":
		affinityType = "source_ip"
	default:
		log.Log.Error(nil, "Unknown session affinity type, defaulting to cookie", "type", sa.Type)
	}

	cookieName := sa.CookieName
	if cookieName == "" {
		cookieName = "NOVAEDGE_AFFINITY"
	}

	cookiePath := sa.CookiePath
	if cookiePath == "" {
		cookiePath = "/"
	}

	return &pb.SessionAffinityConfig{
		Type:             affinityType,
		CookieName:       cookieName,
		CookieTtlSeconds: int64(sa.CookieTTL.Seconds()),
		CookiePath:       cookiePath,
		CookieSecure:     sa.Secure,
		CookieSameSite:   sa.SameSite,
		HeaderName:       sa.HeaderName,
	}
}

// convertRetryConfig converts NovaEdge RetryConfig to protobuf RetryConfig
func convertRetryConfig(retry *novaedgev1alpha1.RetryConfig) *pb.RetryConfig {
	if retry == nil {
		return nil
	}

	pbRetry := &pb.RetryConfig{
		MaxRetries:   retry.MaxRetries,
		RetryOn:      retry.RetryOn,
		RetryMethods: retry.RetryMethods,
	}

	if retry.PerTryTimeout != nil {
		pbRetry.PerTryTimeoutMs = retry.PerTryTimeout.Milliseconds()
	}

	if retry.RetryBudget != nil {
		pbRetry.RetryBudget = *retry.RetryBudget
	}

	if retry.BackoffBase != nil {
		pbRetry.BackoffBaseMs = retry.BackoffBase.Milliseconds()
	}

	return pbRetry
}

// convertCompressionConfig converts NovaEdge CompressionConfig to protobuf CompressionConfig
func convertCompressionConfig(config *novaedgev1alpha1.CompressionConfig) *pb.CompressionConfig {
	if config == nil {
		return nil
	}

	minSize, err := convert.ParseByteSize(config.MinSize)
	if err != nil {
		log.Log.Error(nil, "failed to parse compression min size, using 0",
			"value", config.MinSize, "error", err)
	}

	return &pb.CompressionConfig{
		Enabled:      config.Enabled,
		MinSize:      minSize,
		Level:        config.Level,
		Algorithms:   config.Algorithms,
		ExcludeTypes: config.ExcludeTypes,
	}
}

// convertRouteLimits converts NovaEdge RouteLimits to protobuf RouteLimitsConfig
func convertRouteLimits(limits *novaedgev1alpha1.RouteLimits) *pb.RouteLimitsConfig {
	if limits == nil {
		return nil
	}

	maxBody, err := convert.ParseByteSize(limits.MaxRequestBodySize)
	if err != nil {
		log.Log.Error(nil, "failed to parse max request body size, using 0",
			"value", limits.MaxRequestBodySize, "error", err)
	}
	reqTimeout, err := parseDurationMs(limits.RequestTimeout)
	if err != nil {
		log.Log.Error(nil, "failed to parse request timeout, using 0",
			"value", limits.RequestTimeout, "error", err)
	}
	idleTimeout, err := parseDurationMs(limits.IdleTimeout)
	if err != nil {
		log.Log.Error(nil, "failed to parse idle timeout, using 0",
			"value", limits.IdleTimeout, "error", err)
	}

	return &pb.RouteLimitsConfig{
		MaxRequestBodySize: maxBody,
		RequestTimeoutMs:   reqTimeout,
		IdleTimeoutMs:      idleTimeout,
	}
}

// convertBufferingConfig converts NovaEdge RouteBufferingConfig to protobuf BufferingConfig
func convertBufferingConfig(config *novaedgev1alpha1.RouteBufferingConfig) *pb.BufferingConfig {
	if config == nil {
		return nil
	}

	maxSize, err := convert.ParseByteSize(config.MaxSize)
	if err != nil {
		log.Log.Error(nil, "failed to parse max buffer size, using 0",
			"value", config.MaxSize, "error", err)
	}

	return &pb.BufferingConfig{
		RequestBuffering:  config.Request,
		ResponseBuffering: config.Response,
		MaxBufferSize:     maxSize,
	}
}

// convertDistributedRateLimitConfig converts NovaEdge DistributedRateLimitConfig to protobuf
func convertDistributedRateLimitConfig(config *novaedgev1alpha1.DistributedRateLimitConfig) *pb.DistributedRateLimitConfig {
	if config == nil {
		return nil
	}

	pbConfig := &pb.DistributedRateLimitConfig{
		RequestsPerSecond: config.RequestsPerSecond,
		Burst:             getInt32(config.Burst),
		Algorithm:         config.Algorithm,
		Key:               config.Key,
	}

	pbConfig.Redis = &pb.RedisConfig{
		Address:     config.RedisRef.Address,
		Tls:         config.RedisRef.TLS,
		Database:    config.RedisRef.Database,
		ClusterMode: config.RedisRef.ClusterMode,
	}

	if config.RedisRef.Password != nil {
		pbConfig.Redis.PasswordSecretRef = config.RedisRef.Password.Name
	}

	return pbConfig
}

// convertWAFConfig converts NovaEdge WAFConfig to protobuf WAFConfig.
// When a buildContext and namespace are provided, it looks up referenced
// ConfigMap rules from the pre-fetched cache (no API call).
func convertWAFConfig(config *novaedgev1alpha1.WAFConfig, bc *buildContext, namespace string) *pb.WAFConfig {
	if config == nil {
		return nil
	}

	pbConfig := &pb.WAFConfig{
		Enabled:                config.Enabled,
		Mode:                   config.Mode,
		ParanoiaLevel:          config.ParanoiaLevel,
		AnomalyThreshold:       config.AnomalyThreshold,
		RuleExclusions:         config.RuleExclusions,
		CustomRules:            config.CustomRules,
		MaxBodySize:            config.MaxBodySize,
		ResponseBodyInspection: config.ResponseBodyInspection,
		MaxResponseBodySize:    config.MaxResponseBodySize,
	}

	if config.RulesConfigMap != nil {
		pbConfig.RulesConfigMapRef = config.RulesConfigMap.Name

		// Load ConfigMap rules from the pre-fetched cache
		if bc != nil && namespace != "" {
			if cm, ok := bc.getConfigMap(namespace, config.RulesConfigMap.Name); ok {
				for _, content := range cm.Data {
					rules := parseConfigMapRules(content)
					pbConfig.CustomRules = append(pbConfig.CustomRules, rules...)
				}
			} else {
				logger := log.Log.WithName("snapshot")
				logger.Error(nil, "WAF rules ConfigMap not found in cache",
					"configmap", config.RulesConfigMap.Name,
					"namespace", namespace,
				)
			}
		}
	}

	return pbConfig
}

// parseConfigMapRules parses multi-line SecLang rules from ConfigMap content
func parseConfigMapRules(content string) []string {
	lines := strings.Split(content, "\n")
	rules := make([]string, 0, len(lines))
	var current strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if current.Len() > 0 {
				rules = append(rules, current.String())
				current.Reset()
			}
			continue
		}
		if strings.HasSuffix(trimmed, "\\") {
			current.WriteString(strings.TrimSuffix(trimmed, "\\"))
			current.WriteString(" ")
			continue
		}
		current.WriteString(trimmed)
		rules = append(rules, current.String())
		current.Reset()
	}
	if current.Len() > 0 {
		rules = append(rules, current.String())
	}
	return rules
}

// parseDurationMs parses a duration string and returns milliseconds.
func parseDurationMs(s string) (int64, error) {
	if s == "" || s == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	return d.Milliseconds(), nil
}

// parseInt64 parses a string as int64.
func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// convertErrorPages converts CRD CustomErrorPages to protobuf ErrorPageConfig
func convertErrorPages(pages []novaedgev1alpha1.CustomErrorPage) *pb.ErrorPageConfig {
	config := &pb.ErrorPageConfig{
		Enabled: true,
		Pages:   make(map[int32]string),
	}

	for _, page := range pages {
		body := page.Body
		if body == "" {
			// Use path reference as a placeholder; the agent resolves it
			body = page.Path
		}
		for _, code := range page.Codes {
			config.Pages[code] = body
		}
	}

	return config
}

// convertRedirectScheme converts CRD RedirectSchemeConfig to protobuf RedirectSchemeConfig
func convertRedirectScheme(rs *novaedgev1alpha1.RedirectSchemeConfig) *pb.RedirectSchemeConfig {
	if rs == nil {
		return nil
	}

	return &pb.RedirectSchemeConfig{
		Enabled:    rs.Enabled,
		Scheme:     rs.Scheme,
		Port:       rs.Port,
		StatusCode: rs.StatusCode,
		Exclusions: rs.Exclusions,
	}
}

// convertRouteAccessLog converts CRD RouteAccessLogConfig to protobuf AccessLogConfig
func convertRouteAccessLog(al *novaedgev1alpha1.RouteAccessLogConfig) *pb.AccessLogConfig {
	if al == nil {
		return nil
	}

	config := &pb.AccessLogConfig{
		Enabled:  al.Enabled,
		Format:   al.Format,
		Template: al.Template,
		Output:   al.Output,
		FilePath: al.FilePath,
		MaxSize:  al.MaxSize,
	}

	if al.MaxBackups != nil {
		config.MaxBackups = *al.MaxBackups
	}

	config.FilterStatusCodes = al.FilterStatusCodes

	if al.SampleRate != nil {
		config.SampleRate = float64(*al.SampleRate) / 100.0
	} else {
		config.SampleRate = 1.0
	}
	return config
}

// convertMiddlewarePipeline converts a MiddlewarePipelineConfig to protobuf
func convertMiddlewarePipeline(pipeline *novaedgev1alpha1.MiddlewarePipelineConfig) *pb.MiddlewarePipeline {
	if pipeline == nil {
		return nil
	}

	pbPipeline := &pb.MiddlewarePipeline{
		Middleware: make([]*pb.MiddlewareRef, 0, len(pipeline.Middleware)),
	}

	for _, mw := range pipeline.Middleware {
		mwPriority := mw.Priority
		if mwPriority > math.MaxInt32 {
			mwPriority = math.MaxInt32
		} else if mwPriority < math.MinInt32 {
			mwPriority = math.MinInt32
		}
		pbRef := &pb.MiddlewareRef{
			Type:     mw.Type,
			Name:     mw.Name,
			Priority: int32(mwPriority), //nolint:gosec // bounds-checked above
			Config:   mw.Config,
		}
		pbPipeline.Middleware = append(pbPipeline.Middleware, pbRef)
	}

	return pbPipeline
}

// convertOutlierDetection converts NovaEdge OutlierDetectionConfig to protobuf OutlierDetection
func convertOutlierDetection(od *novaedgev1alpha1.OutlierDetectionConfig) *pb.OutlierDetection {
	if od == nil {
		return nil
	}
	stdevFactor := 1.9
	if od.SuccessRateStdevFactor != "" {
		if v, err := strconv.ParseFloat(od.SuccessRateStdevFactor, 64); err == nil {
			stdevFactor = v
		}
	}
	return &pb.OutlierDetection{
		IntervalMs:               durationToMillis(od.Interval),
		Consecutive_5XxThreshold: getInt32(od.Consecutive5xxThreshold),
		BaseEjectionDurationMs:   durationToMillis(od.BaseEjectionDuration),
		MaxEjectionPercent:       getInt32(od.MaxEjectionPercent),
		SuccessRateMinHosts:      getInt32(od.SuccessRateMinHosts),
		SuccessRateMinRequests:   getInt32(od.SuccessRateMinRequests),
		SuccessRateStdevFactor:   stdevFactor,
	}
}

// convertSlowStart converts NovaEdge SlowStartConfig to protobuf SlowStart
func convertSlowStart(ss *novaedgev1alpha1.SlowStartConfig) *pb.SlowStart {
	if ss == nil {
		return nil
	}
	aggression := 1.0
	if ss.Aggression != "" {
		if v, err := strconv.ParseFloat(ss.Aggression, 64); err == nil {
			aggression = v
		}
	}
	return &pb.SlowStart{
		WindowMs:   durationToMillis(ss.Window),
		Aggression: aggression,
	}
}

// convertFaultInjectionConfig converts CRD FaultInjectionConfig to protobuf FaultInjectionConfig
func convertFaultInjectionConfig(fi *novaedgev1alpha1.FaultInjectionConfig) *pb.FaultInjectionConfig {
	if fi == nil {
		return nil
	}

	pbFI := &pb.FaultInjectionConfig{
		HeaderActivation: fi.HeaderActivation,
	}

	if fi.DelayDuration != nil {
		pbFI.DelayDurationMs = fi.DelayDuration.Milliseconds()
	}
	if fi.DelayPercent != nil {
		pbFI.DelayPercent = *fi.DelayPercent
	}
	if fi.AbortStatusCode != nil {
		pbFI.AbortStatusCode = *fi.AbortStatusCode
	}
	if fi.AbortPercent != nil {
		pbFI.AbortPercent = *fi.AbortPercent
	}

	return pbFI
}

// convertBodyTransformConfig converts CRD BodyTransformConfig to protobuf BodyTransformConfig
func convertBodyTransformConfig(bt *novaedgev1alpha1.BodyTransformConfig) *pb.BodyTransformConfig {
	if bt == nil {
		return nil
	}

	pbBT := &pb.BodyTransformConfig{}

	if bt.MaxBodySize != nil {
		pbBT.MaxBodySize = *bt.MaxBodySize
	}

	pbBT.Request = convertTransformOperations(bt.Request)
	pbBT.Response = convertTransformOperations(bt.Response)

	return pbBT
}

// convertTransformOperations converts a slice of CRD TransformOperations to protobuf
func convertTransformOperations(ops []novaedgev1alpha1.TransformOperation) []*pb.TransformOperation {
	if len(ops) == 0 {
		return nil
	}

	result := make([]*pb.TransformOperation, 0, len(ops))
	for _, op := range ops {
		result = append(result, &pb.TransformOperation{
			Op:    op.Op,
			Path:  op.Path,
			Value: op.Value,
			From:  op.From,
		})
	}
	return result
}

// convertExtProc converts CRD ExtProcCRDConfig to protobuf ExtProcConfig
func convertExtProc(ep *novaedgev1alpha1.ExtProcCRDConfig) *pb.ExtProcConfig {
	if ep == nil {
		return nil
	}
	cfg := &pb.ExtProcConfig{
		Enabled: true,
		Address: ep.Address,
	}
	if ep.Timeout != nil {
		cfg.TimeoutMs = ep.Timeout.Milliseconds()
	}
	if ep.FailOpen != nil {
		cfg.FailOpen = *ep.FailOpen
	}
	if ep.ProcessRequestHeaders != nil {
		cfg.ProcessRequestHeaders = *ep.ProcessRequestHeaders
	}
	if ep.ProcessRequestBody != nil {
		cfg.ProcessRequestBody = *ep.ProcessRequestBody
	}
	if ep.ProcessResponseHeaders != nil {
		cfg.ProcessResponseHeaders = *ep.ProcessResponseHeaders
	}
	if ep.ProcessResponseBody != nil {
		cfg.ProcessResponseBody = *ep.ProcessResponseBody
	}
	return cfg
}
