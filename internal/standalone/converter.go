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

package standalone

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Converter converts standalone config to protobuf ConfigSnapshot
type Converter struct{}

// NewConverter creates a new converter
func NewConverter() *Converter {
	return &Converter{}
}

// safeInt32 converts an int to int32, clamping to math.MaxInt32 on overflow.
func safeInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// ToSnapshot converts a standalone Config to a ConfigSnapshot
func (c *Converter) ToSnapshot(cfg *Config, nodeName string) (*pb.ConfigSnapshot, error) {
	snapshot := &pb.ConfigSnapshot{
		GenerationTime: time.Now().Unix(),
		Endpoints:      make(map[string]*pb.EndpointList),
	}

	// Convert gateways (from listeners)
	gateways := c.convertListeners(cfg.Listeners)
	// Add compression configuration from global settings
	if cfg.Global.Compression != nil && cfg.Global.Compression.Enabled && len(gateways) > 0 {
		gateways[0].Compression = c.convertCompression(cfg.Global.Compression)
	}
	snapshot.Gateways = gateways

	// Convert routes
	routes := c.convertRoutes(cfg.Routes)
	snapshot.Routes = routes

	// Convert backends to clusters and endpoints
	clusters, endpoints := c.convertBackends(cfg.Backends)
	snapshot.Clusters = clusters
	snapshot.Endpoints = endpoints

	// Convert VIPs
	vips, err := c.convertVIPs(cfg.VIPs)
	if err != nil {
		return nil, fmt.Errorf("failed to convert VIPs: %w", err)
	}
	snapshot.VipAssignments = vips

	// Apply error pages configuration to gateways
	if cfg.ErrorPages != nil && cfg.ErrorPages.Enabled && len(gateways) > 0 {
		gateways[0].ErrorPages = c.convertErrorPages(cfg.ErrorPages)
	}

	// Apply redirect scheme configuration to gateways
	if cfg.RedirectScheme != nil && cfg.RedirectScheme.Enabled && len(gateways) > 0 {
		gateways[0].RedirectScheme = c.convertRedirectSchemeConfig(cfg.RedirectScheme)
	}

	// Apply access log configuration to routes
	if cfg.Global.AccessLog.Enabled && len(routes) > 0 {
		accessLogConfig := c.convertAccessLogConfig(&cfg.Global.AccessLog)
		for _, route := range routes {
			route.AccessLog = accessLogConfig
		}
	}

	// Convert policies
	policies := c.convertPolicies(cfg.Policies)
	snapshot.Policies = policies

	// Convert L4 listeners
	l4Listeners := c.convertL4Listeners(cfg.L4Listeners, endpoints)
	snapshot.L4Listeners = l4Listeners

	// Generate version hash
	snapshot.Version = c.generateVersion(snapshot)

	return snapshot, nil
}

func (c *Converter) convertListeners(listeners []ListenerConfig) []*pb.Gateway {
	// Create a single gateway with all listeners
	gateway := &pb.Gateway{
		Name:      "standalone",
		Namespace: "default",
		Listeners: make([]*pb.Listener, 0, len(listeners)),
	}

	for _, l := range listeners {
		listener := &pb.Listener{
			Name:     l.Name,
			Port:     safeInt32(l.Port),
			Protocol: c.parseProtocol(l.Protocol),
		}

		if l.TLS != nil {
			// For standalone mode, TLS cert/key paths are loaded at runtime
			// Store them in the TLSCertificates map with a default hostname
			listener.TlsCertificates = make(map[string]*pb.TLSConfig)
			// Create a placeholder that the server will need to load
			// The actual loading happens at server startup time
			listener.Tls = &pb.TLSConfig{
				MinVersion: l.TLS.MinVersion,
			}
		}

		if len(l.Hostnames) > 0 {
			listener.Hostnames = l.Hostnames
		}

		if l.MaxRequestBodySize > 0 {
			listener.MaxRequestBodyBytes = l.MaxRequestBodySize
		}

		gateway.Listeners = append(gateway.Listeners, listener)
	}

	return []*pb.Gateway{gateway}
}

func (c *Converter) parseProtocol(protocol string) pb.Protocol {
	switch strings.ToUpper(protocol) {
	case "HTTP":
		return pb.Protocol_HTTP
	case "HTTPS":
		return pb.Protocol_HTTPS
	case "HTTP3":
		return pb.Protocol_HTTP3
	case "TCP":
		return pb.Protocol_TCP
	case "TLS":
		return pb.Protocol_TLS
	case "UDP":
		return pb.Protocol_UDP
	default:
		return pb.Protocol_PROTOCOL_UNSPECIFIED
	}
}

func (c *Converter) convertRoutes(routes []RouteConfig) []*pb.Route {
	result := make([]*pb.Route, 0, len(routes))

	for _, r := range routes {
		route := &pb.Route{
			Name:      r.Name,
			Namespace: "default",
		}

		// Set hostnames
		if len(r.Match.Hostnames) > 0 {
			route.Hostnames = r.Match.Hostnames
		}

		// Convert to RouteRule with matches
		rule := &pb.RouteRule{}

		// Build match conditions
		match := &pb.RouteMatch{}

		// Path match
		if r.Match.Path != nil {
			match.Path = &pb.PathMatch{
				Type:  c.parsePathMatchType(r.Match.Path.Type),
				Value: r.Match.Path.Value,
			}
		}

		// Header matches
		for _, h := range r.Match.Headers {
			match.Headers = append(match.Headers, &pb.HeaderMatch{
				Name:  h.Name,
				Value: h.Value,
				Type:  c.parseHeaderMatchType(h.Type),
			})
		}

		// Method match
		if r.Match.Method != "" {
			match.Method = r.Match.Method
		}

		// Add match to rule if it has any conditions
		if match.Path != nil || len(match.Headers) > 0 || match.Method != "" {
			rule.Matches = append(rule.Matches, match)
		}

		// Backend refs
		for _, br := range r.Backends {
			weight := safeInt32(br.Weight)
			if weight == 0 {
				weight = 1
			}
			rule.BackendRefs = append(rule.BackendRefs, &pb.BackendRef{
				Name:      br.Name,
				Namespace: "default",
				Weight:    weight,
			})
		}

		// Limits
		if r.Limits != nil {
			rule.Limits = c.convertRouteLimits(r.Limits)
		}

		// Buffering
		if r.Buffering != nil {
			rule.Buffering = c.convertRouteBuffering(r.Buffering)
		}

		// Filters
		for _, f := range r.Filters {
			filter := &pb.RouteFilter{
				Type: c.parseFilterType(f.Type),
			}

			switch f.Type {
			case "AddHeader":
				for name, value := range f.Add {
					filter.AddHeaders = append(filter.AddHeaders, &pb.HTTPHeader{
						Name:  name,
						Value: value,
					})
				}
			case "RemoveHeader":
				filter.RemoveHeaders = f.Remove
			case "URLRewrite":
				filter.RewritePath = f.RewritePath
			case "RequestRedirect":
				filter.RedirectUrl = f.RedirectURL
			}

			rule.Filters = append(rule.Filters, filter)
		}

		// Convert mirror configuration if present
		if r.Mirror != nil && r.Mirror.Backend != "" {
			percentage := safeInt32(r.Mirror.Percentage)
			if percentage == 0 {
				percentage = 100 // Default: mirror all requests
			}
			rule.MirrorBackend = &pb.BackendRef{
				Name:      r.Mirror.Backend,
				Namespace: "default",
				Weight:    1,
			}
			rule.MirrorPercent = percentage
		}

		// Convert retry configuration
		if r.Retry != nil {
			rule.Retry = c.convertRetryConfig(r.Retry)
		}

		route.Rules = append(route.Rules, rule)
		result = append(result, route)
	}

	return result
}

func (c *Converter) parsePathMatchType(matchType string) pb.PathMatchType {
	switch matchType {
	case "Exact":
		return pb.PathMatchType_EXACT
	case "PathPrefix":
		return pb.PathMatchType_PATH_PREFIX
	case "RegularExpression":
		return pb.PathMatchType_REGULAR_EXPRESSION
	default:
		return pb.PathMatchType_PATH_PREFIX // Default to prefix match
	}
}

func (c *Converter) parseHeaderMatchType(matchType string) pb.HeaderMatchType {
	switch matchType {
	case "Exact", "":
		return pb.HeaderMatchType_HEADER_EXACT
	case "RegularExpression":
		return pb.HeaderMatchType_HEADER_REGULAR_EXPRESSION
	default:
		return pb.HeaderMatchType_HEADER_EXACT
	}
}

func (c *Converter) parseFilterType(filterType string) pb.RouteFilterType {
	switch filterType {
	case "AddHeader":
		return pb.RouteFilterType_ADD_HEADER
	case "RemoveHeader":
		return pb.RouteFilterType_REMOVE_HEADER
	case "URLRewrite":
		return pb.RouteFilterType_URL_REWRITE
	case "RequestRedirect":
		return pb.RouteFilterType_REQUEST_REDIRECT
	default:
		return pb.RouteFilterType_ROUTE_FILTER_TYPE_UNSPECIFIED
	}
}

func (c *Converter) convertBackends(backends []BackendConfig) ([]*pb.Cluster, map[string]*pb.EndpointList) {
	clusters := make([]*pb.Cluster, 0, len(backends))
	endpoints := make(map[string]*pb.EndpointList)

	for _, b := range backends {
		cluster := &pb.Cluster{
			Name:      b.Name,
			Namespace: "default",
			LbPolicy:  c.parseLBPolicy(b.LBPolicy),
		}

		// Health check
		if b.HealthCheck != nil {
			cluster.HealthCheck = &pb.HealthCheck{
				HttpPath: b.HealthCheck.Path,
			}
			if d, err := time.ParseDuration(b.HealthCheck.Interval); err == nil {
				cluster.HealthCheck.IntervalMs = d.Milliseconds()
			}
			if d, err := time.ParseDuration(b.HealthCheck.Timeout); err == nil {
				cluster.HealthCheck.TimeoutMs = d.Milliseconds()
			}
			cluster.HealthCheck.HealthyThreshold = safeInt32(b.HealthCheck.HealthyThreshold)
			cluster.HealthCheck.UnhealthyThreshold = safeInt32(b.HealthCheck.UnhealthyThreshold)
		}

		// Circuit breaker
		if b.CircuitBreaker != nil {
			cluster.CircuitBreaker = &pb.CircuitBreaker{
				MaxConnections:     safeInt32(b.CircuitBreaker.MaxConnections),
				MaxPendingRequests: safeInt32(b.CircuitBreaker.MaxPendingRequests),
				MaxRequests:        safeInt32(b.CircuitBreaker.MaxRequests),
				MaxRetries:         safeInt32(b.CircuitBreaker.MaxRetries),
			}
		}

		// Connection pool
		if b.ConnectionPool != nil {
			cluster.ConnectionPool = &pb.ConnectionPool{
				MaxIdleConns:        safeInt32(b.ConnectionPool.MaxIdleConnections),
				MaxIdleConnsPerHost: safeInt32(b.ConnectionPool.MaxConnections),
			}
			if d, err := time.ParseDuration(b.ConnectionPool.IdleTimeout); err == nil {
				cluster.ConnectionPool.IdleConnTimeoutMs = d.Milliseconds()
			}
		}

		// TLS
		if b.TLS != nil && b.TLS.Enabled {
			cluster.Tls = &pb.BackendTLS{
				Enabled:            true,
				InsecureSkipVerify: b.TLS.InsecureSkipVerify,
			}
		}

		// Session affinity
		if b.SessionAffinity != nil {
			cluster.SessionAffinity = c.convertSessionAffinity(b.SessionAffinity)
		}

		clusters = append(clusters, cluster)

		// Endpoints - key is "namespace/name"
		clusterKey := fmt.Sprintf("default/%s", b.Name)
		endpointList := &pb.EndpointList{
			Endpoints: make([]*pb.Endpoint, 0, len(b.Endpoints)),
		}

		for _, e := range b.Endpoints {
			// Parse address:port
			address, port := parseAddressPort(e.Address)
			endpoint := &pb.Endpoint{
				Address: address,
				Port:    port,
				Ready:   true,
			}
			// Weight is stored in Labels for pb.Endpoint (no direct Weight field)
			if e.Weight > 0 {
				endpoint.Labels = map[string]string{
					"weight": fmt.Sprintf("%d", e.Weight),
				}
			}
			endpointList.Endpoints = append(endpointList.Endpoints, endpoint)
		}

		endpoints[clusterKey] = endpointList
	}

	return clusters, endpoints
}

// parseAddressPort parses "host:port" format
func parseAddressPort(addr string) (string, int32) {
	parts := strings.Split(addr, ":")
	if len(parts) == 2 {
		var port int
		if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
			return parts[0], 0 // Return host with zero port on parse error
		}
		return parts[0], safeInt32(port)
	}
	return addr, 80 // Default to port 80
}

func (c *Converter) parseLBPolicy(policy string) pb.LoadBalancingPolicy {
	switch policy {
	case "RoundRobin":
		return pb.LoadBalancingPolicy_ROUND_ROBIN
	case "P2C":
		return pb.LoadBalancingPolicy_P2C
	case "EWMA":
		return pb.LoadBalancingPolicy_EWMA
	case "RingHash":
		return pb.LoadBalancingPolicy_RING_HASH
	case "Maglev":
		return pb.LoadBalancingPolicy_MAGLEV
	case "LeastConn", "LeastConnections":
		return pb.LoadBalancingPolicy_LEAST_CONN
	default:
		zap.L().Warn("Unknown LB policy, defaulting to RoundRobin", zap.String("policy", policy))
		return pb.LoadBalancingPolicy_ROUND_ROBIN
	}
}

func (c *Converter) convertVIPs(vips []VIPConfig) ([]*pb.VIPAssignment, error) {
	result := make([]*pb.VIPAssignment, 0, len(vips))

	for _, v := range vips {
		assignment := &pb.VIPAssignment{
			VipName:  v.Name,
			Address:  v.Address,
			Mode:     c.parseVIPMode(v.Mode),
			IsActive: true,
		}

		// BGP config
		if v.BGP != nil {
			assignment.BgpConfig = &pb.BGPConfig{
				LocalAs:  v.BGP.LocalAS,
				RouterId: v.BGP.RouterID,
				Peers: []*pb.BGPPeer{
					{
						Address: v.BGP.PeerIP,
						As:      v.BGP.PeerAS,
					},
				},
			}
		}

		// OSPF config
		if v.OSPF != nil {
			// Parse area as uint32 if possible
			var areaID uint32
			if _, err := fmt.Sscanf(v.OSPF.Area, "%d", &areaID); err != nil {
				return nil, fmt.Errorf("failed to parse OSPF area %q: %w", v.OSPF.Area, err)
			}
			assignment.OspfConfig = &pb.OSPFConfig{
				RouterId: v.OSPF.RouterID,
				AreaId:   areaID,
			}
		}

		result = append(result, assignment)
	}

	return result, nil
}

func (c *Converter) parseVIPMode(mode string) pb.VIPMode {
	switch mode {
	case "L2":
		return pb.VIPMode_L2_ARP
	case "BGP":
		return pb.VIPMode_BGP
	case "OSPF":
		return pb.VIPMode_OSPF
	default:
		return pb.VIPMode_L2_ARP
	}
}

func (c *Converter) convertPolicies(policies []PolicyConfig) []*pb.Policy {
	result := make([]*pb.Policy, 0, len(policies))

	for _, p := range policies {
		policy := &pb.Policy{
			Name:      p.Name,
			Namespace: "default",
		}

		switch p.Type {
		case "RateLimit":
			if p.RateLimit != nil {
				policy.RateLimit = &pb.RateLimitConfig{
					RequestsPerSecond: safeInt32(p.RateLimit.RequestsPerSecond),
					Burst:             safeInt32(p.RateLimit.BurstSize),
					Key:               p.RateLimit.Key,
				}
			}
		case "CORS":
			if p.CORS != nil {
				policy.Cors = &pb.CORSConfig{
					AllowOrigins:     p.CORS.AllowOrigins,
					AllowMethods:     p.CORS.AllowMethods,
					AllowHeaders:     p.CORS.AllowHeaders,
					ExposeHeaders:    p.CORS.ExposeHeaders,
					MaxAgeSeconds:    int64(p.CORS.MaxAge),
					AllowCredentials: p.CORS.AllowCredentials,
				}
			}
		case "IPFilter":
			if p.IPFilter != nil {
				// Combine allow and deny lists into CIDRs
				// For an IP allow list, use those CIDRs
				cidrs := p.IPFilter.AllowList
				if len(cidrs) == 0 {
					cidrs = p.IPFilter.DenyList
				}
				policy.IpList = &pb.IPListConfig{
					Cidrs: cidrs,
				}
			}
		case "JWT":
			if p.JWT != nil {
				policy.Jwt = &pb.JWTConfig{
					Issuer:   p.JWT.Issuer,
					Audience: p.JWT.Audience,
					JwksUri:  p.JWT.JWKSURI,
				}
			}
		case "DistributedRateLimit":
			if p.DistributedRateLimit != nil {
				policy.Type = pb.PolicyType_DISTRIBUTED_RATE_LIMIT
				policy.DistributedRateLimit = &pb.DistributedRateLimitConfig{
					RequestsPerSecond: safeInt32(p.DistributedRateLimit.RequestsPerSecond),
					Burst:             safeInt32(p.DistributedRateLimit.BurstSize),
					Algorithm:         p.DistributedRateLimit.Algorithm,
					Key:               p.DistributedRateLimit.Key,
					Redis: &pb.RedisConfig{
						Address:     p.DistributedRateLimit.Redis.Address,
						Tls:         p.DistributedRateLimit.Redis.TLS,
						Database:    safeInt32(p.DistributedRateLimit.Redis.Database),
						ClusterMode: p.DistributedRateLimit.Redis.ClusterMode,
					},
				}
			}
		case "WAF":
			if p.WAF != nil {
				policy.Type = pb.PolicyType_WAF
				policy.Waf = &pb.WAFConfig{
					Enabled:          p.WAF.Enabled,
					Mode:             p.WAF.Mode,
					ParanoiaLevel:    safeInt32(p.WAF.ParanoiaLevel),
					AnomalyThreshold: safeInt32(p.WAF.AnomalyThreshold),
					RuleExclusions:   p.WAF.RuleExclusions,
					CustomRules:      p.WAF.CustomRules,
				}
			}
		}

		result = append(result, policy)
	}

	return result
}

func (c *Converter) convertCompression(comp *StandaloneCompressionConfig) *pb.CompressionConfig {
	if comp == nil {
		return nil
	}
	var minSize int64
	if comp.MinSize != "" {
		n, err := strconv.ParseInt(comp.MinSize, 10, 64)
		if err != nil {
			zap.L().Warn("failed to parse compression min size, using 0",
				zap.String("value", comp.MinSize), zap.Error(err))
		} else {
			minSize = n
		}
	}
	return &pb.CompressionConfig{
		Enabled:      comp.Enabled,
		MinSize:      minSize,
		Level:        safeInt32(comp.Level),
		Algorithms:   comp.Algorithms,
		ExcludeTypes: comp.ExcludeTypes,
	}
}

func (c *Converter) convertRouteLimits(limits *StandaloneRouteLimits) *pb.RouteLimitsConfig {
	if limits == nil {
		return nil
	}
	result := &pb.RouteLimitsConfig{}
	if limits.MaxRequestBodySize != "" {
		n, err := standaloneParseByteSize(limits.MaxRequestBodySize)
		if err != nil {
			zap.L().Warn("failed to parse max request body size, using 0",
				zap.String("value", limits.MaxRequestBodySize), zap.Error(err))
		} else {
			result.MaxRequestBodySize = n
		}
	}
	if limits.RequestTimeout != "" {
		d, err := time.ParseDuration(limits.RequestTimeout)
		if err != nil {
			zap.L().Warn("failed to parse request timeout, using 0",
				zap.String("value", limits.RequestTimeout), zap.Error(err))
		} else {
			result.RequestTimeoutMs = d.Milliseconds()
		}
	}
	if limits.IdleTimeout != "" {
		d, err := time.ParseDuration(limits.IdleTimeout)
		if err != nil {
			zap.L().Warn("failed to parse idle timeout, using 0",
				zap.String("value", limits.IdleTimeout), zap.Error(err))
		} else {
			result.IdleTimeoutMs = d.Milliseconds()
		}
	}
	return result
}

func (c *Converter) convertRouteBuffering(buf *StandaloneBufferingConfig) *pb.BufferingConfig {
	if buf == nil {
		return nil
	}
	result := &pb.BufferingConfig{
		RequestBuffering:  buf.Request,
		ResponseBuffering: buf.Response,
	}
	if buf.MaxSize != "" {
		n, err := standaloneParseByteSize(buf.MaxSize)
		if err != nil {
			zap.L().Warn("failed to parse max buffer size, using 0",
				zap.String("value", buf.MaxSize), zap.Error(err))
		} else {
			result.MaxBufferSize = n
		}
	}
	return result
}

// standaloneParseByteSize parses human-readable byte size (e.g., "10Mi", "1024").
func standaloneParseByteSize(s string) (int64, error) {
	if s == "" || s == "0" {
		return 0, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	multipliers := map[string]int64{
		"Ki": 1 << 10, "Mi": 1 << 20, "Gi": 1 << 30,
		"KB": 1000, "MB": 1000 * 1000, "GB": 1000 * 1000 * 1000,
	}
	for suffix, mult := range multipliers {
		if strings.HasSuffix(s, suffix) {
			numStr := strings.TrimSuffix(s, suffix)
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid byte size: %s", s)
			}
			return n * mult, nil
		}
	}
	return 0, fmt.Errorf("invalid byte size: %s", s)
}

func (c *Converter) convertL4Listeners(configs []L4ListenerStandaloneConfig, endpoints map[string]*pb.EndpointList) []*pb.L4Listener {
	result := make([]*pb.L4Listener, 0, len(configs))

	for _, cfg := range configs {
		l4Listener := &pb.L4Listener{
			Name:     cfg.Name,
			Port:     safeInt32(cfg.Port),
			Protocol: c.parseProtocol(cfg.Protocol),
		}

		switch cfg.Protocol {
		case "TCP":
			l4Listener.BackendName = cfg.Backend
			clusterKey := fmt.Sprintf("default/%s", cfg.Backend)
			if epList, ok := endpoints[clusterKey]; ok {
				l4Listener.Backends = epList.Endpoints
			}
			if cfg.TCP != nil {
				l4Listener.TcpConfig = &pb.L4TCPConfig{
					BufferSize: safeInt32(cfg.TCP.BufferSize),
				}
				if d, err := time.ParseDuration(cfg.TCP.ConnectTimeout); err == nil {
					l4Listener.TcpConfig.ConnectTimeoutMs = d.Milliseconds()
				}
				if d, err := time.ParseDuration(cfg.TCP.IdleTimeout); err == nil {
					l4Listener.TcpConfig.IdleTimeoutMs = d.Milliseconds()
				}
				if d, err := time.ParseDuration(cfg.TCP.DrainTimeout); err == nil {
					l4Listener.TcpConfig.DrainTimeoutMs = d.Milliseconds()
				}
			}

		case "UDP":
			l4Listener.BackendName = cfg.Backend
			clusterKey := fmt.Sprintf("default/%s", cfg.Backend)
			if epList, ok := endpoints[clusterKey]; ok {
				l4Listener.Backends = epList.Endpoints
			}
			if cfg.UDP != nil {
				l4Listener.UdpConfig = &pb.L4UDPConfig{
					BufferSize: safeInt32(cfg.UDP.BufferSize),
				}
				if d, err := time.ParseDuration(cfg.UDP.SessionTimeout); err == nil {
					l4Listener.UdpConfig.SessionTimeoutMs = d.Milliseconds()
				}
			}

		case "TLS":
			var tlsRoutes []*pb.L4TLSRoute
			for _, route := range cfg.TLSRoutes {
				clusterKey := fmt.Sprintf("default/%s", route.Backend)
				var backends []*pb.Endpoint
				if epList, ok := endpoints[clusterKey]; ok {
					backends = epList.Endpoints
				}
				tlsRoutes = append(tlsRoutes, &pb.L4TLSRoute{
					Hostname:    route.Hostname,
					BackendName: route.Backend,
					Backends:    backends,
				})
			}
			l4Listener.TlsRoutes = tlsRoutes

			if cfg.DefaultTLSBackend != "" {
				clusterKey := fmt.Sprintf("default/%s", cfg.DefaultTLSBackend)
				var backends []*pb.Endpoint
				if epList, ok := endpoints[clusterKey]; ok {
					backends = epList.Endpoints
				}
				l4Listener.DefaultTlsBackend = &pb.L4TLSRoute{
					Hostname:    "*",
					BackendName: cfg.DefaultTLSBackend,
					Backends:    backends,
				}
			}
		}

		result = append(result, l4Listener)
	}

	return result
}
func (c *Converter) generateVersion(snapshot *pb.ConfigSnapshot) string {
	data, _ := proto.Marshal(snapshot)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:8])
}

func (c *Converter) convertSessionAffinity(sa *SessionAffinityStandaloneConfig) *pb.SessionAffinityConfig {
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
		zap.L().Warn("Unknown session affinity type, defaulting to cookie", zap.String("type", sa.Type))
	}

	cookieName := sa.CookieName
	if cookieName == "" {
		cookieName = "NOVAEDGE_AFFINITY"
	}

	cookiePath := sa.CookiePath
	if cookiePath == "" {
		cookiePath = "/"
	}

	var ttlSeconds int64
	if sa.CookieTTL != "" {
		if d, err := time.ParseDuration(sa.CookieTTL); err == nil {
			ttlSeconds = int64(d.Seconds())
		} else {
			zap.L().Warn("Failed to parse session affinity cookie TTL duration", zap.String("ttl", sa.CookieTTL), zap.Error(err))
		}
	}

	return &pb.SessionAffinityConfig{
		Type:             affinityType,
		CookieName:       cookieName,
		CookieTtlSeconds: ttlSeconds,
		CookiePath:       cookiePath,
		CookieSecure:     sa.Secure,
		CookieSameSite:   sa.SameSite,
	}
}

func (c *Converter) convertErrorPages(ep *ErrorPagesConfig) *pb.ErrorPageConfig {
	if ep == nil || !ep.Enabled {
		return nil
	}

	config := &pb.ErrorPageConfig{
		Enabled:     true,
		Pages:       make(map[int32]string),
		DefaultPage: ep.DefaultPage,
	}

	for code, tmpl := range ep.Pages {
		config.Pages[safeInt32(code)] = tmpl
	}

	return config
}

func (c *Converter) convertRedirectSchemeConfig(rs *RedirectSchemeStandaloneConfig) *pb.RedirectSchemeConfig {
	if rs == nil || !rs.Enabled {
		return nil
	}

	return &pb.RedirectSchemeConfig{
		Enabled:    true,
		Scheme:     rs.Scheme,
		Port:       safeInt32(rs.Port),
		StatusCode: safeInt32(rs.StatusCode),
		Exclusions: rs.Exclusions,
	}
}

func (c *Converter) convertAccessLogConfig(al *AccessLogConfig) *pb.AccessLogConfig {
	if al == nil || !al.Enabled {
		return nil
	}

	config := &pb.AccessLogConfig{
		Enabled:  true,
		Format:   al.Format,
		Template: al.Template,
		Output:   al.Output,
		MaxSize:  al.MaxSize,
	}

	// Use Path as FilePath for backward compatibility
	if al.Path != "" && al.Path != "stdout" {
		config.FilePath = al.Path
	}

	config.MaxBackups = safeInt32(al.MaxBackups)

	for _, code := range al.FilterStatusCodes {
		config.FilterStatusCodes = append(config.FilterStatusCodes, safeInt32(code))
	}

	config.SampleRate = al.SampleRate
	if config.SampleRate <= 0 {
		config.SampleRate = 1.0
	}

	return config
}

func (c *Converter) convertRetryConfig(retry *RetryPolicyConfig) *pb.RetryConfig {
	if retry == nil {
		return nil
	}

	cfg := &pb.RetryConfig{
		MaxRetries:   safeInt32(retry.MaxRetries),
		RetryOn:      retry.RetryOn,
		RetryBudget:  retry.RetryBudget,
		RetryMethods: retry.RetryMethods,
	}

	if retry.PerTryTimeout != "" {
		if d, err := time.ParseDuration(retry.PerTryTimeout); err == nil {
			cfg.PerTryTimeoutMs = d.Milliseconds()
		}
	}

	if retry.BackoffBase != "" {
		if d, err := time.ParseDuration(retry.BackoffBase); err == nil {
			cfg.BackoffBaseMs = d.Milliseconds()
		}
	}

	return cfg
}
