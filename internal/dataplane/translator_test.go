package dataplane

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	pb "github.com/azrtydxb/novaedge/api/proto/dataplane"
	configpb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

// ---------------------------------------------------------------------------
// TranslateSnapshot unit tests
// ---------------------------------------------------------------------------

func TestTranslateSnapshot_Nil(t *testing.T) {
	req := TranslateSnapshot(nil)
	if req == nil {
		t.Fatal("TranslateSnapshot(nil) returned nil, want non-nil")
	}
	if req.GetVersion() != "" {
		t.Errorf("expected empty version, got %q", req.GetVersion())
	}
}

func TestTranslateSnapshot_EmptySnapshot(t *testing.T) {
	req := TranslateSnapshot(&configpb.ConfigSnapshot{})
	if req == nil {
		t.Fatal("TranslateSnapshot returned nil")
	}
	if len(req.GetGateways()) != 0 {
		t.Errorf("expected 0 gateways, got %d", len(req.GetGateways()))
	}
	if len(req.GetRoutes()) != 0 {
		t.Errorf("expected 0 routes, got %d", len(req.GetRoutes()))
	}
}

func TestTranslateSnapshot_Version(t *testing.T) {
	snap := &configpb.ConfigSnapshot{Version: "v42"}
	req := TranslateSnapshot(snap)
	if req.GetVersion() != "v42" {
		t.Errorf("version = %q, want %q", req.GetVersion(), "v42")
	}
}

func TestTranslateSnapshot_Gateways(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Gateways: []*configpb.Gateway{
			{
				Name: "web",
				Listeners: []*configpb.Listener{
					{
						Name:     "http",
						Port:     8080,
						Protocol: configpb.Protocol_HTTP,
						Hostnames: []string{
							"example.com",
						},
					},
					{
						Name:     "https",
						Port:     8443,
						Protocol: configpb.Protocol_HTTPS,
						Tls: &configpb.TLSConfig{
							Cert:       []byte("cert-pem"),
							Key:        []byte("key-pem"),
							CaCert:     []byte("ca-pem"),
							MinVersion: "1.3",
						},
					},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)

	if len(req.GetGateways()) != 2 {
		t.Fatalf("expected 2 gateways, got %d", len(req.GetGateways()))
	}

	httpGw := req.GetGateways()[0]
	if httpGw.GetName() != "web/http" {
		t.Errorf("gateway name = %q, want %q", httpGw.GetName(), "web/http")
	}
	if httpGw.GetPort() != 8080 {
		t.Errorf("gateway port = %d, want %d", httpGw.GetPort(), 8080)
	}
	if httpGw.GetProtocol() != pb.GatewayProtocol_GATEWAY_PROTOCOL_HTTP {
		t.Errorf("gateway protocol = %v, want HTTP", httpGw.GetProtocol())
	}
	if len(httpGw.GetHostnames()) != 1 || httpGw.GetHostnames()[0] != "example.com" {
		t.Errorf("gateway hostnames = %v, want [example.com]", httpGw.GetHostnames())
	}
	if httpGw.GetTlsConfig() != nil {
		t.Error("expected no TLS config on HTTP gateway")
	}

	httpsGw := req.GetGateways()[1]
	if httpsGw.GetName() != "web/https" {
		t.Errorf("gateway name = %q, want %q", httpsGw.GetName(), "web/https")
	}
	if httpsGw.GetProtocol() != pb.GatewayProtocol_GATEWAY_PROTOCOL_HTTPS {
		t.Errorf("gateway protocol = %v, want HTTPS", httpsGw.GetProtocol())
	}
	if httpsGw.GetTlsConfig() == nil {
		t.Fatal("expected TLS config on HTTPS gateway")
	}
	if string(httpsGw.GetTlsConfig().GetCertPem()) != "cert-pem" {
		t.Errorf("TLS cert = %q, want %q", string(httpsGw.GetTlsConfig().GetCertPem()), "cert-pem")
	}
	if httpsGw.GetTlsConfig().GetMinVersion() != "1.3" {
		t.Errorf("TLS min version = %q, want %q", httpsGw.GetTlsConfig().GetMinVersion(), "1.3")
	}
}

func TestTranslateSnapshot_GatewayProtocols(t *testing.T) {
	tests := []struct {
		input    configpb.Protocol
		expected pb.GatewayProtocol
	}{
		{configpb.Protocol_HTTP, pb.GatewayProtocol_GATEWAY_PROTOCOL_HTTP},
		{configpb.Protocol_HTTPS, pb.GatewayProtocol_GATEWAY_PROTOCOL_HTTPS},
		{configpb.Protocol_HTTP3, pb.GatewayProtocol_GATEWAY_PROTOCOL_HTTP3},
		{configpb.Protocol_TCP, pb.GatewayProtocol_GATEWAY_PROTOCOL_TCP},
		{configpb.Protocol_UDP, pb.GatewayProtocol_GATEWAY_PROTOCOL_UDP},
	}

	for _, tc := range tests {
		t.Run(tc.input.String(), func(t *testing.T) {
			got := translateGatewayProtocol(tc.input)
			if got != tc.expected {
				t.Errorf("translateGatewayProtocol(%v) = %v, want %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestTranslateSnapshot_Routes(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Routes: []*configpb.Route{
			{
				Name:      "api-route",
				Hostnames: []string{"api.example.com"},
				Rules: []*configpb.RouteRule{
					{
						Matches: []*configpb.RouteMatch{
							{
								Path: &configpb.PathMatch{
									Type:  configpb.PathMatchType_PATH_PREFIX,
									Value: "/api/v1",
								},
								Headers: []*configpb.HeaderMatch{
									{Name: "X-Env", Value: "prod"},
								},
							},
						},
						BackendRefs: []*configpb.BackendRef{
							{Name: "api-svc", Weight: 80},
							{Name: "api-canary", Weight: 20},
						},
						Retry: &configpb.RetryConfig{
							MaxRetries:      3,
							PerTryTimeoutMs: 500,
							RetryOn:         []string{"5xx"},
							BackoffBaseMs:   100,
						},
					},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)

	if len(req.GetRoutes()) != 1 {
		t.Fatalf("expected 1 route, got %d", len(req.GetRoutes()))
	}

	rt := req.GetRoutes()[0]
	if rt.GetName() != "api-route/rule-0" {
		t.Errorf("route name = %q, want %q", rt.GetName(), "api-route/rule-0")
	}
	if len(rt.GetHostnames()) != 1 || rt.GetHostnames()[0] != "api.example.com" {
		t.Errorf("route hostnames = %v, want [api.example.com]", rt.GetHostnames())
	}
	if rt.GetPathMatch() == nil {
		t.Fatal("expected path match")
	}
	if rt.GetPathMatch().GetMatchType() != pb.PathMatchType_PATH_MATCH_PREFIX {
		t.Errorf("path match type = %v, want PREFIX", rt.GetPathMatch().GetMatchType())
	}
	if rt.GetPathMatch().GetValue() != "/api/v1" {
		t.Errorf("path match value = %q, want %q", rt.GetPathMatch().GetValue(), "/api/v1")
	}
	if len(rt.GetHeaderMatches()) != 1 {
		t.Fatalf("expected 1 header match, got %d", len(rt.GetHeaderMatches()))
	}
	if rt.GetHeaderMatches()[0].GetName() != "X-Env" || rt.GetHeaderMatches()[0].GetValue() != "prod" {
		t.Errorf("header match = %v, want X-Env:prod", rt.GetHeaderMatches()[0])
	}

	if len(rt.GetBackendRefs()) != 2 {
		t.Fatalf("expected 2 backend refs, got %d", len(rt.GetBackendRefs()))
	}
	if rt.GetBackendRefs()[0].GetClusterName() != "api-svc" || rt.GetBackendRefs()[0].GetWeight() != 80 {
		t.Errorf("backend ref 0 = %v, want api-svc:80", rt.GetBackendRefs()[0])
	}
	if rt.GetBackendRefs()[1].GetClusterName() != "api-canary" || rt.GetBackendRefs()[1].GetWeight() != 20 {
		t.Errorf("backend ref 1 = %v, want api-canary:20", rt.GetBackendRefs()[1])
	}

	if rt.GetRetry() == nil {
		t.Fatal("expected retry policy")
	}
	if rt.GetRetry().GetMaxRetries() != 3 {
		t.Errorf("retry max_retries = %d, want %d", rt.GetRetry().GetMaxRetries(), 3)
	}
	if len(rt.GetRetry().GetRetryOn()) != 1 || rt.GetRetry().GetRetryOn()[0] != "5xx" {
		t.Errorf("retry retry_on = %v, want [5xx]", rt.GetRetry().GetRetryOn())
	}
}

func TestTranslateSnapshot_PathMatchTypes(t *testing.T) {
	tests := []struct {
		input    configpb.PathMatchType
		expected pb.PathMatchType
	}{
		{configpb.PathMatchType_EXACT, pb.PathMatchType_PATH_MATCH_EXACT},
		{configpb.PathMatchType_PATH_PREFIX, pb.PathMatchType_PATH_MATCH_PREFIX},
		{configpb.PathMatchType_REGULAR_EXPRESSION, pb.PathMatchType_PATH_MATCH_REGEX},
	}

	for _, tc := range tests {
		t.Run(tc.input.String(), func(t *testing.T) {
			got := translatePathMatchType(tc.input)
			if got != tc.expected {
				t.Errorf("translatePathMatchType(%v) = %v, want %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestTranslateSnapshot_Clusters(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Clusters: []*configpb.Cluster{
			{
				Name:             "api-svc",
				Namespace:        "default",
				LbPolicy:         configpb.LoadBalancingPolicy_LEAST_CONN,
				ConnectTimeoutMs: 5000,
				HealthCheck: &configpb.HealthCheck{
					IntervalMs:         30000,
					TimeoutMs:          5000,
					HealthyThreshold:   3,
					UnhealthyThreshold: 2,
					HttpPath:           "/healthz",
				},
				CircuitBreaker: &configpb.CircuitBreaker{
					MaxConnections:     100,
					MaxPendingRequests: 50,
					MaxRequests:        200,
					MaxRetries:         3,
				},
				ConnectionPool: &configpb.ConnectionPool{
					MaxConnsPerHost:     10,
					MaxIdleConnsPerHost: 5,
					IdleConnTimeoutMs:   60000,
				},
			},
		},
		Endpoints: map[string]*configpb.EndpointList{
			"default/api-svc": {
				Endpoints: []*configpb.Endpoint{
					{Address: "10.0.0.1", Port: 8080, Ready: true},
					{Address: "10.0.0.2", Port: 8080, Ready: false},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)

	if len(req.GetClusters()) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(req.GetClusters()))
	}

	cl := req.GetClusters()[0]
	if cl.GetName() != "api-svc" {
		t.Errorf("cluster name = %q, want %q", cl.GetName(), "api-svc")
	}
	if cl.GetLbAlgorithm() != pb.LBAlgorithm_LB_ALGORITHM_LEAST_CONN {
		t.Errorf("LB algorithm = %v, want LEAST_CONN", cl.GetLbAlgorithm())
	}
	if cl.GetConnectTimeoutMs() != 5000 {
		t.Errorf("connect timeout = %d, want %d", cl.GetConnectTimeoutMs(), 5000)
	}

	// Endpoints
	if len(cl.GetEndpoints()) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(cl.GetEndpoints()))
	}
	ep0 := cl.GetEndpoints()[0]
	if ep0.GetIp() != "10.0.0.1" || ep0.GetPort() != 8080 || !ep0.GetHealthy() {
		t.Errorf("endpoint 0 = %v, want 10.0.0.1:8080 healthy", ep0)
	}
	ep1 := cl.GetEndpoints()[1]
	if ep1.GetIp() != "10.0.0.2" || ep1.GetHealthy() {
		t.Errorf("endpoint 1 = %v, want 10.0.0.2:8080 unhealthy", ep1)
	}

	// Health check
	if cl.GetHealthCheck() == nil {
		t.Fatal("expected health check")
	}
	if cl.GetHealthCheck().GetIntervalMs() != 30000 {
		t.Errorf("health check interval = %d, want %d", cl.GetHealthCheck().GetIntervalMs(), 30000)
	}
	if cl.GetHealthCheck().GetHttpPath() != "/healthz" {
		t.Errorf("health check path = %q, want %q", cl.GetHealthCheck().GetHttpPath(), "/healthz")
	}

	// Circuit breaker
	if cl.GetCircuitBreaker() == nil {
		t.Fatal("expected circuit breaker")
	}
	if cl.GetCircuitBreaker().GetMaxConnections() != 100 {
		t.Errorf("CB max connections = %d, want %d", cl.GetCircuitBreaker().GetMaxConnections(), 100)
	}

	// Connection pool
	if cl.GetConnectionPool() == nil {
		t.Fatal("expected connection pool")
	}
	if cl.GetConnectionPool().GetMaxConnections() != 10 {
		t.Errorf("pool max connections = %d, want %d", cl.GetConnectionPool().GetMaxConnections(), 10)
	}
	if cl.GetConnectionPool().GetMaxIdle() != 5 {
		t.Errorf("pool max idle = %d, want %d", cl.GetConnectionPool().GetMaxIdle(), 5)
	}
}

func TestTranslateSnapshot_LBAlgorithms(t *testing.T) {
	tests := []struct {
		input    configpb.LoadBalancingPolicy
		expected pb.LBAlgorithm
	}{
		{configpb.LoadBalancingPolicy_ROUND_ROBIN, pb.LBAlgorithm_LB_ALGORITHM_ROUND_ROBIN},
		{configpb.LoadBalancingPolicy_LEAST_CONN, pb.LBAlgorithm_LB_ALGORITHM_LEAST_CONN},
		{configpb.LoadBalancingPolicy_P2C, pb.LBAlgorithm_LB_ALGORITHM_P2C},
		{configpb.LoadBalancingPolicy_EWMA, pb.LBAlgorithm_LB_ALGORITHM_EWMA},
		{configpb.LoadBalancingPolicy_RING_HASH, pb.LBAlgorithm_LB_ALGORITHM_RING_HASH},
		{configpb.LoadBalancingPolicy_MAGLEV, pb.LBAlgorithm_LB_ALGORITHM_MAGLEV},
	}

	for _, tc := range tests {
		t.Run(tc.input.String(), func(t *testing.T) {
			got := translateLBAlgorithm(tc.input)
			if got != tc.expected {
				t.Errorf("translateLBAlgorithm(%v) = %v, want %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestTranslateSnapshot_VIPs(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		VipAssignments: []*configpb.VIPAssignment{
			{
				VipName: "ingress-vip",
				Address: "192.168.1.100/32",
				Mode:    configpb.VIPMode_L2_ARP,
			},
			{
				VipName: "bgp-vip",
				Address: "10.0.100.1/32",
				Mode:    configpb.VIPMode_BGP,
				BgpConfig: &configpb.BGPConfig{
					LocalAs:         65000,
					RouterId:        "10.0.0.1",
					Communities:     []string{"65000:100"},
					LocalPreference: 200,
					Peers: []*configpb.BGPPeer{
						{Address: "10.0.0.254"},
						{Address: "10.0.0.253"},
					},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)

	if len(req.GetVips()) != 2 {
		t.Fatalf("expected 2 VIPs, got %d", len(req.GetVips()))
	}

	l2Vip := req.GetVips()[0]
	if l2Vip.GetName() != "ingress-vip" {
		t.Errorf("VIP name = %q, want %q", l2Vip.GetName(), "ingress-vip")
	}
	if l2Vip.GetMode() != pb.VIPMode_VIP_MODE_L2 {
		t.Errorf("VIP mode = %v, want L2", l2Vip.GetMode())
	}
	if l2Vip.GetBgpConfig() != nil {
		t.Error("L2 VIP should not have BGP config")
	}

	bgpVip := req.GetVips()[1]
	if bgpVip.GetName() != "bgp-vip" {
		t.Errorf("VIP name = %q, want %q", bgpVip.GetName(), "bgp-vip")
	}
	if bgpVip.GetMode() != pb.VIPMode_VIP_MODE_BGP {
		t.Errorf("VIP mode = %v, want BGP", bgpVip.GetMode())
	}
	if bgpVip.GetBgpConfig() == nil {
		t.Fatal("expected BGP config")
	}
	if bgpVip.GetBgpConfig().GetLocalAsn() != 65000 {
		t.Errorf("BGP local ASN = %d, want %d", bgpVip.GetBgpConfig().GetLocalAsn(), 65000)
	}
	if len(bgpVip.GetBgpConfig().GetPeerAddresses()) != 2 {
		t.Fatalf("expected 2 peer addresses, got %d", len(bgpVip.GetBgpConfig().GetPeerAddresses()))
	}
	if bgpVip.GetBgpConfig().GetPeerAddresses()[0] != "10.0.0.254" {
		t.Errorf("peer 0 = %q, want %q", bgpVip.GetBgpConfig().GetPeerAddresses()[0], "10.0.0.254")
	}
}

func TestTranslateSnapshot_VIPModes(t *testing.T) {
	tests := []struct {
		input    configpb.VIPMode
		expected pb.VIPMode
	}{
		{configpb.VIPMode_L2_ARP, pb.VIPMode_VIP_MODE_L2},
		{configpb.VIPMode_BGP, pb.VIPMode_VIP_MODE_BGP},
		{configpb.VIPMode_OSPF, pb.VIPMode_VIP_MODE_OSPF},
	}

	for _, tc := range tests {
		t.Run(tc.input.String(), func(t *testing.T) {
			got := translateVIPMode(tc.input)
			if got != tc.expected {
				t.Errorf("translateVIPMode(%v) = %v, want %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestTranslateSnapshot_L4Listeners(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		L4Listeners: []*configpb.L4Listener{
			{
				Name:        "mysql",
				Port:        3306,
				Protocol:    configpb.Protocol_TCP,
				BackendName: "mysql-backend",
				TcpConfig: &configpb.L4TCPConfig{
					IdleTimeoutMs:    300000,
					ConnectTimeoutMs: 5000,
				},
			},
			{
				Name:     "dns",
				Port:     53,
				Protocol: configpb.Protocol_UDP,
				UdpConfig: &configpb.L4UDPConfig{
					SessionTimeoutMs: 30000,
				},
			},
		},
	}

	req := TranslateSnapshot(snap)

	if len(req.GetL4Listeners()) != 2 {
		t.Fatalf("expected 2 L4 listeners, got %d", len(req.GetL4Listeners()))
	}

	tcp := req.GetL4Listeners()[0]
	if tcp.GetName() != "mysql" {
		t.Errorf("L4 name = %q, want %q", tcp.GetName(), "mysql")
	}
	if tcp.GetPort() != 3306 {
		t.Errorf("L4 port = %d, want %d", tcp.GetPort(), 3306)
	}
	if tcp.GetProtocol() != pb.L4Protocol_L4_PROTOCOL_TCP {
		t.Errorf("L4 protocol = %v, want TCP", tcp.GetProtocol())
	}
	if len(tcp.GetBackendRefs()) != 1 || tcp.GetBackendRefs()[0].GetClusterName() != "mysql-backend" {
		t.Errorf("L4 backend refs = %v, want [mysql-backend]", tcp.GetBackendRefs())
	}
	if tcp.GetIdleTimeoutMs() != 300000 {
		t.Errorf("L4 idle timeout = %d, want %d", tcp.GetIdleTimeoutMs(), 300000)
	}

	udp := req.GetL4Listeners()[1]
	if udp.GetProtocol() != pb.L4Protocol_L4_PROTOCOL_UDP {
		t.Errorf("L4 protocol = %v, want UDP", udp.GetProtocol())
	}
	if udp.GetIdleTimeoutMs() != 30000 {
		t.Errorf("L4 idle timeout = %d, want %d", udp.GetIdleTimeoutMs(), 30000)
	}
}

func TestTranslateSnapshot_L4Protocols(t *testing.T) {
	tests := []struct {
		input    configpb.Protocol
		expected pb.L4Protocol
	}{
		{configpb.Protocol_TCP, pb.L4Protocol_L4_PROTOCOL_TCP},
		{configpb.Protocol_UDP, pb.L4Protocol_L4_PROTOCOL_UDP},
		{configpb.Protocol_TLS, pb.L4Protocol_L4_PROTOCOL_TLS_PASSTHROUGH},
	}

	for _, tc := range tests {
		t.Run(tc.input.String(), func(t *testing.T) {
			got := translateL4Protocol(tc.input)
			if got != tc.expected {
				t.Errorf("translateL4Protocol(%v) = %v, want %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestTranslateSnapshot_Policies_RateLimit(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Policies: []*configpb.Policy{
			{
				Name: "rl-policy",
				Type: configpb.PolicyType_RATE_LIMIT,
				RateLimit: &configpb.RateLimitConfig{
					RequestsPerSecond: 100,
					Burst:             10,
					Key:               "client_ip",
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	if len(req.GetPolicies()) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(req.GetPolicies()))
	}

	pol := req.GetPolicies()[0]
	if pol.GetName() != "rl-policy" {
		t.Errorf("policy name = %q, want %q", pol.GetName(), "rl-policy")
	}
	if pol.GetPolicyType() != pb.PolicyType_POLICY_TYPE_RATE_LIMIT {
		t.Errorf("policy type = %v, want RATE_LIMIT", pol.GetPolicyType())
	}

	rl := pol.GetRateLimit()
	if rl == nil {
		t.Fatal("expected rate limit config")
	}
	if rl.GetRequestsPerSecond() != 100 {
		t.Errorf("RPS = %d, want %d", rl.GetRequestsPerSecond(), 100)
	}
	if rl.GetBurst() != 10 {
		t.Errorf("burst = %d, want %d", rl.GetBurst(), 10)
	}
	if rl.GetKey() != "client_ip" {
		t.Errorf("key = %q, want %q", rl.GetKey(), "client_ip")
	}
}

func TestTranslateSnapshot_Policies_JWT(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Policies: []*configpb.Policy{
			{
				Name: "jwt-policy",
				Type: configpb.PolicyType_JWT,
				Jwt: &configpb.JWTConfig{
					Issuer:            "https://auth.example.com",
					Audience:          []string{"api"},
					JwksUri:           "https://auth.example.com/.well-known/jwks.json",
					HeaderName:        "Authorization",
					HeaderPrefix:      "Bearer ",
					AllowedAlgorithms: []string{"RS256"},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	pol := req.GetPolicies()[0]
	jwt := pol.GetJwt()
	if jwt == nil {
		t.Fatal("expected JWT config")
	}
	if jwt.GetIssuer() != "https://auth.example.com" {
		t.Errorf("issuer = %q, want %q", jwt.GetIssuer(), "https://auth.example.com")
	}
	if len(jwt.GetAudiences()) != 1 || jwt.GetAudiences()[0] != "api" {
		t.Errorf("audiences = %v, want [api]", jwt.GetAudiences())
	}
	if jwt.GetJwksUri() != "https://auth.example.com/.well-known/jwks.json" {
		t.Errorf("jwks_uri = %q", jwt.GetJwksUri())
	}
}

func TestTranslateSnapshot_Policies_WAF(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Policies: []*configpb.Policy{
			{
				Name: "waf-policy",
				Type: configpb.PolicyType_WAF,
				Waf: &configpb.WAFConfig{
					Enabled:          true,
					Mode:             "block",
					ParanoiaLevel:    2,
					AnomalyThreshold: 5,
					RuleExclusions:   []string{"942100"},
					MaxBodySize:      1048576,
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	pol := req.GetPolicies()[0]
	waf := pol.GetWaf()
	if waf == nil {
		t.Fatal("expected WAF config")
	}
	if !waf.GetEnabled() {
		t.Error("WAF should be enabled")
	}
	if waf.GetMode() != "block" {
		t.Errorf("WAF mode = %q, want %q", waf.GetMode(), "block")
	}
	if waf.GetParanoiaLevel() != 2 {
		t.Errorf("paranoia = %d, want %d", waf.GetParanoiaLevel(), 2)
	}
	if waf.GetMaxBodySize() != 1048576 {
		t.Errorf("max body = %d, want %d", waf.GetMaxBodySize(), 1048576)
	}
}

func TestTranslateSnapshot_Policies_CORS(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Policies: []*configpb.Policy{
			{
				Name: "cors-policy",
				Type: configpb.PolicyType_CORS,
				Cors: &configpb.CORSConfig{
					AllowOrigins:     []string{"https://example.com"},
					AllowMethods:     []string{"GET", "POST"},
					AllowHeaders:     []string{"Content-Type"},
					ExposeHeaders:    []string{"X-Request-Id"},
					AllowCredentials: true,
					MaxAgeSeconds:    3600,
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	pol := req.GetPolicies()[0]
	cors := pol.GetCors()
	if cors == nil {
		t.Fatal("expected CORS config")
	}
	if len(cors.GetAllowOrigins()) != 1 || cors.GetAllowOrigins()[0] != "https://example.com" {
		t.Errorf("allow origins = %v", cors.GetAllowOrigins())
	}
	if !cors.GetAllowCredentials() {
		t.Error("allow credentials should be true")
	}
	if cors.GetMaxAgeSeconds() != 3600 {
		t.Errorf("max age = %d, want %d", cors.GetMaxAgeSeconds(), 3600)
	}
}

func TestTranslateSnapshot_Policies_IPFilter(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Policies: []*configpb.Policy{
			{
				Name: "ip-allow",
				Type: configpb.PolicyType_IP_ALLOW_LIST,
				IpList: &configpb.IPListConfig{
					Cidrs:        []string{"10.0.0.0/8", "192.168.0.0/16"},
					SourceHeader: "X-Forwarded-For",
				},
			},
			{
				Name: "ip-deny",
				Type: configpb.PolicyType_IP_DENY_LIST,
				IpList: &configpb.IPListConfig{
					Cidrs: []string{"172.16.0.0/12"},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	if len(req.GetPolicies()) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(req.GetPolicies()))
	}

	allowPol := req.GetPolicies()[0]
	if allowPol.GetIpFilter().GetAction() != "allow" {
		t.Errorf("IP filter action = %q, want %q", allowPol.GetIpFilter().GetAction(), "allow")
	}
	if len(allowPol.GetIpFilter().GetCidrs()) != 2 {
		t.Errorf("expected 2 CIDRs, got %d", len(allowPol.GetIpFilter().GetCidrs()))
	}
	if allowPol.GetIpFilter().GetSourceHeader() != "X-Forwarded-For" {
		t.Errorf("source header = %q, want %q", allowPol.GetIpFilter().GetSourceHeader(), "X-Forwarded-For")
	}

	denyPol := req.GetPolicies()[1]
	if denyPol.GetIpFilter().GetAction() != "deny" {
		t.Errorf("IP filter action = %q, want %q", denyPol.GetIpFilter().GetAction(), "deny")
	}
}

func TestTranslateSnapshot_Policies_BasicAuth(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Policies: []*configpb.Policy{
			{
				Name: "basic-auth",
				Type: configpb.PolicyType_BASIC_AUTH,
				BasicAuth: &configpb.BasicAuthConfig{
					Realm: "Restricted Area",
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	pol := req.GetPolicies()[0]
	ba := pol.GetBasicAuth()
	if ba == nil {
		t.Fatal("expected basic auth config")
	}
	if ba.GetRealm() != "Restricted Area" {
		t.Errorf("realm = %q, want %q", ba.GetRealm(), "Restricted Area")
	}
}

func TestTranslateSnapshot_Policies_ForwardAuth(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Policies: []*configpb.Policy{
			{
				Name: "fwd-auth",
				Type: configpb.PolicyType_FORWARD_AUTH,
				ForwardAuth: &configpb.ForwardAuthConfig{
					Address: "http://auth-svc.default.svc.cluster.local:8080/verify",
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	pol := req.GetPolicies()[0]
	fa := pol.GetForwardAuth()
	if fa == nil {
		t.Fatal("expected forward auth config")
	}
	if fa.GetAddress() != "http://auth-svc.default.svc.cluster.local:8080/verify" {
		t.Errorf("address = %q", fa.GetAddress())
	}
}

func TestTranslateSnapshot_PolicyTypes(t *testing.T) {
	tests := []struct {
		input    configpb.PolicyType
		expected pb.PolicyType
	}{
		{configpb.PolicyType_RATE_LIMIT, pb.PolicyType_POLICY_TYPE_RATE_LIMIT},
		{configpb.PolicyType_JWT, pb.PolicyType_POLICY_TYPE_JWT},
		{configpb.PolicyType_BASIC_AUTH, pb.PolicyType_POLICY_TYPE_BASIC_AUTH},
		{configpb.PolicyType_FORWARD_AUTH, pb.PolicyType_POLICY_TYPE_FORWARD_AUTH},
		{configpb.PolicyType_WAF, pb.PolicyType_POLICY_TYPE_WAF},
		{configpb.PolicyType_CORS, pb.PolicyType_POLICY_TYPE_CORS},
		{configpb.PolicyType_IP_ALLOW_LIST, pb.PolicyType_POLICY_TYPE_IP_FILTER},
		{configpb.PolicyType_IP_DENY_LIST, pb.PolicyType_POLICY_TYPE_IP_FILTER},
		{configpb.PolicyType_OIDC, pb.PolicyType_POLICY_TYPE_OAUTH2},
	}

	for _, tc := range tests {
		t.Run(tc.input.String(), func(t *testing.T) {
			got := translatePolicyType(tc.input)
			if got != tc.expected {
				t.Errorf("translatePolicyType(%v) = %v, want %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestTranslateSnapshot_WANLinks(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		WanLinks: []*configpb.WANLink{
			{
				Name:     "primary",
				Iface:    "eth0",
				Provider: "ISP-A",
				Cost:     10,
				Sla: &configpb.WANLinkSLA{
					MaxLatencyMs:  50,
					MaxJitterMs:   10,
					MaxPacketLoss: 0.01,
				},
			},
		},
	}

	req := TranslateSnapshot(snap)

	if len(req.GetWanLinks()) != 1 {
		t.Fatalf("expected 1 WAN link, got %d", len(req.GetWanLinks()))
	}

	wl := req.GetWanLinks()[0]
	if wl.GetName() != "primary" {
		t.Errorf("WAN link name = %q, want %q", wl.GetName(), "primary")
	}
	if wl.GetInterface() != "eth0" {
		t.Errorf("interface = %q, want %q", wl.GetInterface(), "eth0")
	}
	if wl.GetProvider() != "ISP-A" {
		t.Errorf("provider = %q, want %q", wl.GetProvider(), "ISP-A")
	}
	if wl.GetPriority() != 10 {
		t.Errorf("priority = %d, want %d", wl.GetPriority(), 10)
	}
	if wl.GetSlaTarget() == nil {
		t.Fatal("expected SLA target")
	}
	if wl.GetSlaTarget().GetMaxLatencyMs() != 50 {
		t.Errorf("SLA max latency = %d, want %d", wl.GetSlaTarget().GetMaxLatencyMs(), 50)
	}
}

func TestTranslateSnapshot_MeshConfig(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		MeshTlsConfig: &configpb.MeshTLSConfig{
			SpiffeId:      "spiffe://cluster.local/agent/node-1",
			TrustDomain:   "cluster.local",
			CaCertificate: []byte("ca-cert-pem"),
			Certificate:   []byte("cert-pem"),
			PrivateKey:    []byte("key-pem"),
		},
		InternalServices: []*configpb.InternalService{
			{
				Name:        "my-svc",
				MeshEnabled: true,
				Ports: []*configpb.ServicePort{
					{Name: "http", Port: 8080},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)

	if req.GetMeshConfig() == nil {
		t.Fatal("expected mesh config")
	}
	mc := req.GetMeshConfig()
	if !mc.GetEnabled() {
		t.Error("mesh should be enabled when internal services exist")
	}
	if mc.GetSpiffeId() != "spiffe://cluster.local/agent/node-1" {
		t.Errorf("SPIFFE ID = %q", mc.GetSpiffeId())
	}
	if mc.GetTrustDomain() != "cluster.local" {
		t.Errorf("trust domain = %q, want %q", mc.GetTrustDomain(), "cluster.local")
	}
	if string(mc.GetCaCertPem()) != "ca-cert-pem" {
		t.Errorf("CA cert = %q", mc.GetCaCertPem())
	}
}

func TestTranslateSnapshot_MeshConfigOmittedWhenEmpty(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Version: "v1",
	}
	req := TranslateSnapshot(snap)
	if req.GetMeshConfig() != nil {
		t.Error("mesh config should be nil when no mesh TLS or internal services")
	}
}

func TestTranslateSnapshot_GatewayProxyProtocol(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Gateways: []*configpb.Gateway{
			{
				Name: "proxy",
				Listeners: []*configpb.Listener{
					{
						Name:     "with-pp",
						Port:     8080,
						Protocol: configpb.Protocol_HTTP,
						ProxyProtocol: &configpb.ProxyProtocolConfig{
							Enabled: true,
							Version: 2,
						},
					},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	gw := req.GetGateways()[0]
	if gw.GetProxyProtocol() == nil {
		t.Fatal("expected proxy protocol config")
	}
	if !gw.GetProxyProtocol().GetEnabled() {
		t.Error("proxy protocol should be enabled")
	}
	if gw.GetProxyProtocol().GetVersion() != 2 {
		t.Errorf("proxy protocol version = %d, want %d", gw.GetProxyProtocol().GetVersion(), 2)
	}
}

// ---------------------------------------------------------------------------
// Translator.Sync integration test (with fake gRPC server)
// ---------------------------------------------------------------------------

func TestTranslator_Sync(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	translator := NewTranslator(client, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snap := &configpb.ConfigSnapshot{
		Version: "v99",
		Gateways: []*configpb.Gateway{
			{
				Name: "test-gw",
				Listeners: []*configpb.Listener{
					{
						Name:     "http",
						Port:     80,
						Protocol: configpb.Protocol_HTTP,
					},
				},
			},
		},
		Routes: []*configpb.Route{
			{
				Name:      "test-route",
				Hostnames: []string{"test.example.com"},
			},
		},
	}

	if err := translator.Sync(ctx, snap); err != nil {
		t.Fatalf("Sync() error: %v", err)
	}
}

func TestTranslator_Sync_NilSnapshot(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	translator := NewTranslator(client, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := translator.Sync(ctx, nil); err != nil {
		t.Fatalf("Sync(nil) error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Full snapshot round-trip test
// ---------------------------------------------------------------------------

func TestTranslateSnapshot_FullRoundTrip(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Version: "v123",
		Gateways: []*configpb.Gateway{
			{
				Name: "main",
				Listeners: []*configpb.Listener{
					{Name: "http", Port: 80, Protocol: configpb.Protocol_HTTP},
					{Name: "https", Port: 443, Protocol: configpb.Protocol_HTTPS,
						Tls: &configpb.TLSConfig{Cert: []byte("c"), Key: []byte("k")}},
				},
			},
		},
		Routes: []*configpb.Route{
			{
				Name:      "web",
				Hostnames: []string{"web.example.com"},
				Rules: []*configpb.RouteRule{
					{
						BackendRefs: []*configpb.BackendRef{
							{Name: "web-svc", Weight: 100},
						},
					},
				},
			},
		},
		Clusters: []*configpb.Cluster{
			{Name: "web-svc", Namespace: "default", LbPolicy: configpb.LoadBalancingPolicy_ROUND_ROBIN},
		},
		Endpoints: map[string]*configpb.EndpointList{
			"default/web-svc": {
				Endpoints: []*configpb.Endpoint{
					{Address: "10.0.0.1", Port: 8080, Ready: true},
				},
			},
		},
		VipAssignments: []*configpb.VIPAssignment{
			{VipName: "vip-1", Address: "192.168.1.1/32", Mode: configpb.VIPMode_L2_ARP},
		},
		L4Listeners: []*configpb.L4Listener{
			{Name: "db", Port: 5432, Protocol: configpb.Protocol_TCP, BackendName: "postgres"},
		},
		Policies: []*configpb.Policy{
			{Name: "rl", Type: configpb.PolicyType_RATE_LIMIT,
				RateLimit: &configpb.RateLimitConfig{RequestsPerSecond: 50}},
		},
		WanLinks: []*configpb.WANLink{
			{Name: "wan-1", Iface: "eth0"},
		},
		MeshTlsConfig: &configpb.MeshTLSConfig{
			SpiffeId:    "spiffe://example.com/node",
			TrustDomain: "example.com",
		},
		InternalServices: []*configpb.InternalService{
			{Name: "internal-svc"},
		},
	}

	req := TranslateSnapshot(snap)

	// Verify all sections are populated.
	if req.GetVersion() != "v123" {
		t.Errorf("version = %q, want %q", req.GetVersion(), "v123")
	}
	if len(req.GetGateways()) != 2 {
		t.Errorf("gateways = %d, want %d", len(req.GetGateways()), 2)
	}
	if len(req.GetRoutes()) != 1 {
		t.Errorf("routes = %d, want %d", len(req.GetRoutes()), 1)
	}
	if len(req.GetClusters()) != 1 {
		t.Errorf("clusters = %d, want %d", len(req.GetClusters()), 1)
	}
	if len(req.GetVips()) != 1 {
		t.Errorf("vips = %d, want %d", len(req.GetVips()), 1)
	}
	if len(req.GetL4Listeners()) != 1 {
		t.Errorf("l4_listeners = %d, want %d", len(req.GetL4Listeners()), 1)
	}
	if len(req.GetPolicies()) != 1 {
		t.Errorf("policies = %d, want %d", len(req.GetPolicies()), 1)
	}
	if len(req.GetWanLinks()) != 1 {
		t.Errorf("wan_links = %d, want %d", len(req.GetWanLinks()), 1)
	}
	if req.GetMeshConfig() == nil {
		t.Error("expected mesh config")
	}
}

// ---------------------------------------------------------------------------
// Tests for newly mapped fields (Batch 2)
// ---------------------------------------------------------------------------

func TestTranslateSnapshot_Routes_MiddlewareAndTimeout(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Routes: []*configpb.Route{
			{
				Name:      "mw-route",
				Hostnames: []string{"mw.example.com"},
				Pipeline: &configpb.MiddlewarePipeline{
					Middleware: []*configpb.MiddlewareRef{
						{Name: "rate-limit"},
						{Name: "jwt-auth"},
					},
				},
				Rules: []*configpb.RouteRule{
					{
						Limits: &configpb.RouteLimitsConfig{
							RequestTimeoutMs: 5000,
						},
						BackendRefs: []*configpb.BackendRef{
							{Name: "svc", Weight: 100},
						},
					},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	rt := req.GetRoutes()[0]

	if len(rt.GetMiddlewareRefs()) != 2 {
		t.Fatalf("expected 2 middleware refs, got %d", len(rt.GetMiddlewareRefs()))
	}
	if rt.GetMiddlewareRefs()[0] != "rate-limit" || rt.GetMiddlewareRefs()[1] != "jwt-auth" {
		t.Errorf("middleware refs = %v, want [rate-limit, jwt-auth]", rt.GetMiddlewareRefs())
	}
	if rt.GetTimeoutMs() != 5000 {
		t.Errorf("timeout_ms = %d, want %d", rt.GetTimeoutMs(), 5000)
	}
}

func TestTranslateSnapshot_Clusters_BackendTLS(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Clusters: []*configpb.Cluster{
			{
				Name: "tls-backend",
				Tls: &configpb.BackendTLS{
					Enabled:            true,
					InsecureSkipVerify: true,
					CaCert:             []byte("ca-pem"),
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	cl := req.GetClusters()[0]

	if cl.GetBackendTls() == nil {
		t.Fatal("expected backend TLS config")
	}
	if !cl.GetBackendTls().GetInsecureSkipVerify() {
		t.Error("expected insecure_skip_verify = true")
	}
	if string(cl.GetBackendTls().GetCaPem()) != "ca-pem" {
		t.Errorf("ca_pem = %q, want %q", string(cl.GetBackendTls().GetCaPem()), "ca-pem")
	}
}

func TestTranslateSnapshot_VIPs_OSPFAndBFD(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		VipAssignments: []*configpb.VIPAssignment{
			{
				VipName: "ospf-vip",
				Address: "10.0.0.1/32",
				Mode:    configpb.VIPMode_OSPF,
				OspfConfig: &configpb.OSPFConfig{
					AreaId: 100,
				},
				BfdConfig: &configpb.BFDConfig{
					Enabled:              true,
					DetectMultiplier:     3,
					DesiredMinTxInterval: "300ms",
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	vip := req.GetVips()[0]

	if vip.GetOspfAreaId() != 100 {
		t.Errorf("ospf_area_id = %d, want %d", vip.GetOspfAreaId(), 100)
	}
	if !vip.GetBfdEnabled() {
		t.Error("expected bfd_enabled = true")
	}
	if vip.GetBfdMultiplier() != 3 {
		t.Errorf("bfd_multiplier = %d, want %d", vip.GetBfdMultiplier(), 3)
	}
	if vip.GetBfdIntervalMs() != 300 {
		t.Errorf("bfd_interval_ms = %d, want %d", vip.GetBfdIntervalMs(), 300)
	}
}

func TestTranslateSnapshot_VIPs_BGPPeerDetails(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		VipAssignments: []*configpb.VIPAssignment{
			{
				VipName: "bgp-detailed",
				Address: "10.0.0.2/32",
				Mode:    configpb.VIPMode_BGP,
				BgpConfig: &configpb.BGPConfig{
					LocalAs:  65001,
					RouterId: "10.0.0.1",
					Peers: []*configpb.BGPPeer{
						{Address: "10.0.0.254", As: 65002, Port: 179},
						{Address: "10.0.0.253", As: 65003, Port: 1179},
					},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	bgp := req.GetVips()[0].GetBgpConfig()

	if len(bgp.GetPeerAsNumbers()) != 2 {
		t.Fatalf("expected 2 peer AS numbers, got %d", len(bgp.GetPeerAsNumbers()))
	}
	if bgp.GetPeerAsNumbers()[0] != 65002 || bgp.GetPeerAsNumbers()[1] != 65003 {
		t.Errorf("peer AS numbers = %v, want [65002, 65003]", bgp.GetPeerAsNumbers())
	}
	if bgp.GetPeerPorts()[0] != 179 || bgp.GetPeerPorts()[1] != 1179 {
		t.Errorf("peer ports = %v, want [179, 1179]", bgp.GetPeerPorts())
	}
}

func TestTranslateSnapshot_Policies_BasicAuth_FullFields(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Policies: []*configpb.Policy{
			{
				Name: "full-basic-auth",
				Type: configpb.PolicyType_BASIC_AUTH,
				BasicAuth: &configpb.BasicAuthConfig{ //nolint:gosec // G101: test fixture data, not real credentials
					Realm:     "Admin",
					Htpasswd:  "admin:$apr1$hash",
					StripAuth: true,
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	ba := req.GetPolicies()[0].GetBasicAuth()

	if ba.GetHtpasswd() != "admin:$apr1$hash" {
		t.Errorf("htpasswd = %q, want %q", ba.GetHtpasswd(), "admin:$apr1$hash")
	}
	if !ba.GetStripAuthorization() {
		t.Error("expected strip_authorization = true")
	}
}

func TestTranslateSnapshot_Policies_ForwardAuth_FullFields(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Policies: []*configpb.Policy{
			{
				Name: "full-fwd-auth",
				Type: configpb.PolicyType_FORWARD_AUTH,
				ForwardAuth: &configpb.ForwardAuthConfig{
					Address:         "http://auth:8080/verify",
					AuthHeaders:     []string{"Authorization", "X-Custom"},
					ResponseHeaders: []string{"X-User-Id"},
					TimeoutMs:       3000,
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	fa := req.GetPolicies()[0].GetForwardAuth()

	if len(fa.GetAuthRequestHeaders()) != 2 {
		t.Fatalf("expected 2 auth request headers, got %d", len(fa.GetAuthRequestHeaders()))
	}
	if fa.GetAuthRequestHeaders()[0] != "Authorization" {
		t.Errorf("auth request header[0] = %q", fa.GetAuthRequestHeaders()[0])
	}
	if len(fa.GetAuthResponseHeaders()) != 1 || fa.GetAuthResponseHeaders()[0] != "X-User-Id" {
		t.Errorf("auth response headers = %v, want [X-User-Id]", fa.GetAuthResponseHeaders())
	}
	if fa.GetTimeoutMs() != 3000 {
		t.Errorf("timeout_ms = %d, want %d", fa.GetTimeoutMs(), 3000)
	}
}

func TestTranslateSnapshot_Policies_SecurityHeaders(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Policies: []*configpb.Policy{
			{
				Name: "sec-headers",
				Type: configpb.PolicyType_SECURITY_HEADERS,
				SecurityHeaders: &configpb.SecurityHeadersConfig{
					ContentSecurityPolicy: "default-src 'self'",
					XFrameOptions:         "DENY",
					XContentTypeOptions:   true,
					ReferrerPolicy:        "no-referrer",
					PermissionsPolicy:     "geolocation=()",
					Hsts: &configpb.HSTSConfig{
						Enabled:           true,
						MaxAgeSeconds:     31536000,
						IncludeSubdomains: true,
						Preload:           true,
					},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	pol := req.GetPolicies()[0]

	if pol.GetPolicyType() != pb.PolicyType_POLICY_TYPE_SECURITY_HEADERS {
		t.Errorf("policy type = %v, want SECURITY_HEADERS", pol.GetPolicyType())
	}

	sh := pol.GetSecurityHeaders()
	if sh == nil {
		t.Fatal("expected security headers config")
	}
	if sh.GetContentSecurityPolicy() != "default-src 'self'" {
		t.Errorf("CSP = %q", sh.GetContentSecurityPolicy())
	}
	if sh.GetXFrameOptions() != "DENY" {
		t.Errorf("X-Frame-Options = %q", sh.GetXFrameOptions())
	}
	if !sh.GetXContentTypeOptions() {
		t.Error("expected x_content_type_options = true")
	}
	if sh.GetStrictTransportSecurity() != "max-age=31536000; includeSubDomains; preload" {
		t.Errorf("STS = %q", sh.GetStrictTransportSecurity())
	}
	if sh.GetReferrerPolicy() != "no-referrer" {
		t.Errorf("referrer policy = %q", sh.GetReferrerPolicy())
	}
}

func TestTranslateSnapshot_WANLinks_Gateway(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		WanLinks: []*configpb.WANLink{
			{
				Name:     "wan-with-gw",
				Iface:    "eth0",
				Provider: "ISP-B",
				Cost:     5,
				TunnelEndpoint: &configpb.WANTunnelEndpoint{
					PublicIp: "203.0.113.1",
					Port:     51820,
				},
			},
		},
	}

	req := TranslateSnapshot(snap)
	wl := req.GetWanLinks()[0]

	if wl.GetGateway() != "203.0.113.1" {
		t.Errorf("gateway = %q, want %q", wl.GetGateway(), "203.0.113.1")
	}
}

func TestTranslateSnapshot_Routes_RewriteAndHeaders(t *testing.T) {
	snap := &configpb.ConfigSnapshot{
		Routes: []*configpb.Route{
			{
				Name: "route-with-filters",
				Rules: []*configpb.RouteRule{
					{
						Filters: []*configpb.RouteFilter{
							{
								Type:        configpb.RouteFilterType_URL_REWRITE,
								RewritePath: "/api/v2",
							},
							{
								Type: configpb.RouteFilterType_ADD_HEADER,
								AddHeaders: []*configpb.HTTPHeader{
									{Name: "X-Custom", Value: "hello"},
									{Name: "X-Source", Value: "novaedge"},
								},
							},
						},
						Matches: []*configpb.RouteMatch{
							{Method: "GET"},
							{Method: "POST"},
						},
					},
					{
						Matches: []*configpb.RouteMatch{
							{Method: "POST"},
							{Method: "DELETE"},
						},
					},
				},
			},
		},
	}

	req := TranslateSnapshot(snap)

	// Each rule produces a separate RouteConfig.
	if len(req.GetRoutes()) != 2 {
		t.Fatalf("expected 2 routes (one per rule), got %d", len(req.GetRoutes()))
	}

	// Rule 0: has filters and method from first match (GET).
	rt0 := req.GetRoutes()[0]
	if rt0.GetRewritePath() != "/api/v2" {
		t.Errorf("rule-0 rewrite_path = %q, want %q", rt0.GetRewritePath(), "/api/v2")
	}
	wantHeaders := map[string]string{"X-Custom": "hello", "X-Source": "novaedge"}
	if len(rt0.GetAddHeaders()) != len(wantHeaders) {
		t.Fatalf("rule-0 add_headers len = %d, want %d", len(rt0.GetAddHeaders()), len(wantHeaders))
	}
	for k, v := range wantHeaders {
		if got := rt0.GetAddHeaders()[k]; got != v {
			t.Errorf("rule-0 add_headers[%q] = %q, want %q", k, got, v)
		}
	}
	if len(rt0.GetMethods()) != 1 || rt0.GetMethods()[0] != "GET" {
		t.Errorf("rule-0 methods = %v, want [GET]", rt0.GetMethods())
	}

	// Rule 1: no filters, method from first match (POST).
	rt1 := req.GetRoutes()[1]
	if rt1.GetRewritePath() != "" {
		t.Errorf("rule-1 rewrite_path = %q, want empty", rt1.GetRewritePath())
	}
	if len(rt1.GetMethods()) != 1 || rt1.GetMethods()[0] != "POST" {
		t.Errorf("rule-1 methods = %v, want [POST]", rt1.GetMethods())
	}
}

func TestTranslateSnapshot_PolicyTypes_SecurityHeaders(t *testing.T) {
	got := translatePolicyType(configpb.PolicyType_SECURITY_HEADERS)
	if got != pb.PolicyType_POLICY_TYPE_SECURITY_HEADERS {
		t.Errorf("translatePolicyType(SECURITY_HEADERS) = %v, want POLICY_TYPE_SECURITY_HEADERS", got)
	}
}
