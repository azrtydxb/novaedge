package config

import (
	"errors"
	"testing"

	pkgerrors "github.com/piwi3910/novaedge/internal/pkg/errors"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewValidator(t *testing.T) {
	v := NewValidator()
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
}

// --- ValidateSnapshot tests ---

func TestValidateSnapshot_NilSnapshot(t *testing.T) {
	v := NewValidator()

	err := v.ValidateSnapshot(nil)
	if err == nil {
		t.Fatal("expected error for nil snapshot")
	}

	var validationErr *pkgerrors.ValidationError
	if !errors.As(err, &validationErr) {
		t.Errorf("expected ValidationError, got %T", err)
	}
}

func TestValidateSnapshot_NilConfigSnapshot(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{ConfigSnapshot: nil}
	err := v.ValidateSnapshot(snapshot)
	if err == nil {
		t.Fatal("expected error for nil ConfigSnapshot")
	}

	var validationErr *pkgerrors.ValidationError
	if !errors.As(err, &validationErr) {
		t.Errorf("expected ValidationError, got %T", err)
	}
}

func TestValidateSnapshot_EmptyVersion(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "",
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err == nil {
		t.Fatal("expected error for empty version")
	}

	var validationErr *pkgerrors.ValidationError
	if !errors.As(err, &validationErr) {
		t.Errorf("expected ValidationError, got %T", err)
	}
}

func TestValidateSnapshot_ValidMinimalSnapshot(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateSnapshot_ValidSnapshotWithData(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version:        "v2",
			GenerationTime: 1234567890,
			Gateways:       []*pb.Gateway{},
			Routes:         []*pb.Route{},
			Clusters:       []*pb.Cluster{},
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err != nil {
		t.Fatalf("expected no error for valid snapshot with data, got %v", err)
	}
}

func TestValidateSnapshot_InvalidGateway(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Gateways: []*pb.Gateway{
				{Name: "", Namespace: "default"},
			},
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err == nil {
		t.Fatal("expected error for invalid gateway")
	}
}

func TestValidateSnapshot_InvalidRoute(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Routes: []*pb.Route{
				{Name: "", Namespace: "default"},
			},
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err == nil {
		t.Fatal("expected error for invalid route")
	}
}

func TestValidateSnapshot_InvalidCluster(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Clusters: []*pb.Cluster{
				{Name: "", Namespace: "default"},
			},
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err == nil {
		t.Fatal("expected error for invalid cluster")
	}
}

func TestValidateSnapshot_InvalidEndpoints(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Endpoints: map[string]*pb.EndpointList{
				"cluster-a": {
					Endpoints: []*pb.Endpoint{
						{Address: "", Port: 80},
					},
				},
			},
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err == nil {
		t.Fatal("expected error for invalid endpoint")
	}
}

func TestValidateSnapshot_InvalidVIP(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			VipAssignments: []*pb.VIPAssignment{
				{VipName: "", Address: "10.0.0.1"},
			},
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err == nil {
		t.Fatal("expected error for invalid VIP assignment")
	}
}

func TestValidateSnapshot_FullValidSnapshot(t *testing.T) {
	v := NewValidator()

	snapshot := &Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v3",
			Gateways: []*pb.Gateway{
				{
					Name:      "my-gw",
					Namespace: "default",
					Listeners: []*pb.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: pb.Protocol_HTTP,
						},
					},
				},
			},
			Routes: []*pb.Route{
				{
					Name:      "my-route",
					Namespace: "default",
					Rules: []*pb.RouteRule{
						{
							Matches: []*pb.RouteMatch{
								{
									Path: &pb.PathMatch{
										Type:  pb.PathMatchType_PATH_PREFIX,
										Value: "/api",
									},
								},
							},
							BackendRefs: []*pb.BackendRef{
								{Name: "backend-svc", Weight: 1},
							},
						},
					},
				},
			},
			Clusters: []*pb.Cluster{
				{
					Name:             "backend-svc",
					Namespace:        "default",
					ConnectTimeoutMs: 5000,
				},
			},
			Endpoints: map[string]*pb.EndpointList{
				"default/backend-svc": {
					Endpoints: []*pb.Endpoint{
						{Address: "10.0.0.1", Port: 8080, Ready: true},
					},
				},
			},
			VipAssignments: []*pb.VIPAssignment{
				{
					VipName: "my-vip",
					Address: "192.168.1.100",
					Mode:    pb.VIPMode_L2_ARP,
				},
			},
		},
	}
	err := v.ValidateSnapshot(snapshot)
	if err != nil {
		t.Fatalf("expected no error for valid full snapshot, got %v", err)
	}
}

// --- ValidateGateway tests ---

func TestValidateGateway_Nil(t *testing.T) {
	v := NewValidator()
	err := v.ValidateGateway(nil, 0)
	if err == nil {
		t.Fatal("expected error for nil gateway")
	}
}

func TestValidateGateway_EmptyName(t *testing.T) {
	v := NewValidator()
	gw := &pb.Gateway{Name: "", Namespace: "default", Listeners: []*pb.Listener{{Name: "http", Port: 80, Protocol: pb.Protocol_HTTP}}}
	err := v.ValidateGateway(gw, 0)
	if err == nil {
		t.Fatal("expected error for empty gateway name")
	}
}

func TestValidateGateway_EmptyNamespace(t *testing.T) {
	v := NewValidator()
	gw := &pb.Gateway{Name: "gw", Namespace: "", Listeners: []*pb.Listener{{Name: "http", Port: 80, Protocol: pb.Protocol_HTTP}}}
	err := v.ValidateGateway(gw, 0)
	if err == nil {
		t.Fatal("expected error for empty gateway namespace")
	}
}

func TestValidateGateway_NoListeners(t *testing.T) {
	v := NewValidator()
	gw := &pb.Gateway{Name: "gw", Namespace: "default", Listeners: []*pb.Listener{}}
	err := v.ValidateGateway(gw, 0)
	if err == nil {
		t.Fatal("expected error for gateway with no listeners")
	}
}

func TestValidateGateway_Valid(t *testing.T) {
	v := NewValidator()
	gw := &pb.Gateway{
		Name:      "gw",
		Namespace: "default",
		Listeners: []*pb.Listener{
			{Name: "http", Port: 80, Protocol: pb.Protocol_HTTP},
		},
	}
	err := v.ValidateGateway(gw, 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateGateway_InvalidListener(t *testing.T) {
	v := NewValidator()
	gw := &pb.Gateway{
		Name:      "gw",
		Namespace: "default",
		Listeners: []*pb.Listener{
			{Name: "http", Port: 0, Protocol: pb.Protocol_HTTP},
		},
	}
	err := v.ValidateGateway(gw, 0)
	if err == nil {
		t.Fatal("expected error for invalid listener port")
	}
}

// --- ValidateListener tests ---

func TestValidateListener_Nil(t *testing.T) {
	v := NewValidator()
	err := v.ValidateListener(nil, "gateways[0]", 0)
	if err == nil {
		t.Fatal("expected error for nil listener")
	}
}

func TestValidateListener_EmptyName(t *testing.T) {
	v := NewValidator()
	listener := &pb.Listener{Name: "", Port: 80, Protocol: pb.Protocol_HTTP}
	err := v.ValidateListener(listener, "gateways[0]", 0)
	if err == nil {
		t.Fatal("expected error for empty listener name")
	}
}

func TestValidateListener_InvalidPortZero(t *testing.T) {
	v := NewValidator()
	listener := &pb.Listener{Name: "http", Port: 0, Protocol: pb.Protocol_HTTP}
	err := v.ValidateListener(listener, "gateways[0]", 0)
	if err == nil {
		t.Fatal("expected error for port 0")
	}
}

func TestValidateListener_InvalidPortTooHigh(t *testing.T) {
	v := NewValidator()
	listener := &pb.Listener{Name: "http", Port: 65536, Protocol: pb.Protocol_HTTP}
	err := v.ValidateListener(listener, "gateways[0]", 0)
	if err == nil {
		t.Fatal("expected error for port > 65535")
	}
}

func TestValidateListener_InvalidPortNegative(t *testing.T) {
	v := NewValidator()
	listener := &pb.Listener{Name: "http", Port: -1, Protocol: pb.Protocol_HTTP}
	err := v.ValidateListener(listener, "gateways[0]", 0)
	if err == nil {
		t.Fatal("expected error for negative port")
	}
}

func TestValidateListener_UnspecifiedProtocol(t *testing.T) {
	v := NewValidator()
	listener := &pb.Listener{Name: "http", Port: 80, Protocol: pb.Protocol_PROTOCOL_UNSPECIFIED}
	err := v.ValidateListener(listener, "gateways[0]", 0)
	if err == nil {
		t.Fatal("expected error for unspecified protocol")
	}
}

func TestValidateListener_ValidPorts(t *testing.T) {
	v := NewValidator()

	tests := []int32{1, 80, 443, 8080, 65535}
	for _, port := range tests {
		listener := &pb.Listener{Name: "test", Port: port, Protocol: pb.Protocol_HTTP}
		err := v.ValidateListener(listener, "gateways[0]", 0)
		if err != nil {
			t.Errorf("expected no error for port %d, got %v", port, err)
		}
	}
}

func TestValidateListener_HTTPSWithoutTLS(t *testing.T) {
	v := NewValidator()
	listener := &pb.Listener{Name: "https", Port: 443, Protocol: pb.Protocol_HTTPS}
	err := v.ValidateListener(listener, "gateways[0]", 0)
	if err == nil {
		t.Fatal("expected error for HTTPS listener without TLS config")
	}
}

func TestValidateListener_HTTPSWithTLS(t *testing.T) {
	v := NewValidator()
	listener := &pb.Listener{
		Name:     "https",
		Port:     443,
		Protocol: pb.Protocol_HTTPS,
		Tls: &pb.TLSConfig{
			Cert: []byte("cert-data"),
			Key:  []byte("key-data"),
		},
	}
	err := v.ValidateListener(listener, "gateways[0]", 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateListener_HTTPSWithSNICerts(t *testing.T) {
	v := NewValidator()
	listener := &pb.Listener{
		Name:     "https",
		Port:     443,
		Protocol: pb.Protocol_HTTPS,
		TlsCertificates: map[string]*pb.TLSConfig{
			"example.com": {
				Cert: []byte("cert-data"),
				Key:  []byte("key-data"),
			},
		},
	}
	err := v.ValidateListener(listener, "gateways[0]", 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// --- ValidateTLSConfig tests ---

func TestValidateTLSConfig_Nil(t *testing.T) {
	v := NewValidator()
	err := v.ValidateTLSConfig(nil, "tls")
	if err != nil {
		t.Fatalf("expected no error for nil TLS config, got %v", err)
	}
}

func TestValidateTLSConfig_BothPresent(t *testing.T) {
	v := NewValidator()
	cfg := &pb.TLSConfig{Cert: []byte("cert"), Key: []byte("key")}
	err := v.ValidateTLSConfig(cfg, "tls")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateTLSConfig_BothAbsent(t *testing.T) {
	v := NewValidator()
	cfg := &pb.TLSConfig{}
	err := v.ValidateTLSConfig(cfg, "tls")
	if err != nil {
		t.Fatalf("expected no error for both absent, got %v", err)
	}
}

func TestValidateTLSConfig_CertWithoutKey(t *testing.T) {
	v := NewValidator()
	cfg := &pb.TLSConfig{Cert: []byte("cert")}
	err := v.ValidateTLSConfig(cfg, "tls")
	if err == nil {
		t.Fatal("expected error for cert without key")
	}
}

func TestValidateTLSConfig_KeyWithoutCert(t *testing.T) {
	v := NewValidator()
	cfg := &pb.TLSConfig{Key: []byte("key")}
	err := v.ValidateTLSConfig(cfg, "tls")
	if err == nil {
		t.Fatal("expected error for key without cert")
	}
}

// --- ValidateRoute tests ---

func TestValidateRoute_Nil(t *testing.T) {
	v := NewValidator()
	err := v.ValidateRoute(nil, 0)
	if err == nil {
		t.Fatal("expected error for nil route")
	}
}

func TestValidateRoute_EmptyName(t *testing.T) {
	v := NewValidator()
	route := &pb.Route{Name: "", Namespace: "default", Rules: []*pb.RouteRule{}}
	err := v.ValidateRoute(route, 0)
	if err == nil {
		t.Fatal("expected error for empty route name")
	}
}

func TestValidateRoute_EmptyNamespace(t *testing.T) {
	v := NewValidator()
	route := &pb.Route{Name: "route", Namespace: "", Rules: []*pb.RouteRule{}}
	err := v.ValidateRoute(route, 0)
	if err == nil {
		t.Fatal("expected error for empty route namespace")
	}
}

func TestValidateRoute_NoRules(t *testing.T) {
	v := NewValidator()
	route := &pb.Route{Name: "route", Namespace: "default", Rules: []*pb.RouteRule{}}
	err := v.ValidateRoute(route, 0)
	if err == nil {
		t.Fatal("expected error for route with no rules")
	}
}

func TestValidateRoute_Valid(t *testing.T) {
	v := NewValidator()
	route := &pb.Route{
		Name:      "my-route",
		Namespace: "default",
		Rules: []*pb.RouteRule{
			{
				BackendRefs: []*pb.BackendRef{
					{Name: "backend", Weight: 1},
				},
			},
		},
	}
	err := v.ValidateRoute(route, 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// --- ValidateRouteRule tests ---

func TestValidateRouteRule_Nil(t *testing.T) {
	v := NewValidator()
	err := v.ValidateRouteRule(nil, "routes[0]", 0)
	if err == nil {
		t.Fatal("expected error for nil route rule")
	}
}

func TestValidateRouteRule_NoBackendRefs(t *testing.T) {
	v := NewValidator()
	rule := &pb.RouteRule{BackendRefs: []*pb.BackendRef{}}
	err := v.ValidateRouteRule(rule, "routes[0]", 0)
	if err == nil {
		t.Fatal("expected error for route rule with no backend refs")
	}
}

func TestValidateRouteRule_ValidCatchAll(t *testing.T) {
	v := NewValidator()
	rule := &pb.RouteRule{
		BackendRefs: []*pb.BackendRef{
			{Name: "backend", Weight: 1},
		},
	}
	err := v.ValidateRouteRule(rule, "routes[0]", 0)
	if err != nil {
		t.Fatalf("expected no error for catch-all rule, got %v", err)
	}
}

func TestValidateRouteRule_ValidWithMatch(t *testing.T) {
	v := NewValidator()
	rule := &pb.RouteRule{
		Matches: []*pb.RouteMatch{
			{
				Path: &pb.PathMatch{
					Type:  pb.PathMatchType_PATH_PREFIX,
					Value: "/api",
				},
			},
		},
		BackendRefs: []*pb.BackendRef{
			{Name: "backend", Weight: 1},
		},
	}
	err := v.ValidateRouteRule(rule, "routes[0]", 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// --- ValidateRouteMatch tests ---

func TestValidateRouteMatch_Nil(t *testing.T) {
	v := NewValidator()
	err := v.ValidateRouteMatch(nil, "routes[0].rules[0]", 0)
	if err == nil {
		t.Fatal("expected error for nil route match")
	}
}

func TestValidateRouteMatch_EmptyPathValue(t *testing.T) {
	v := NewValidator()
	match := &pb.RouteMatch{
		Path: &pb.PathMatch{
			Type:  pb.PathMatchType_EXACT,
			Value: "",
		},
	}
	err := v.ValidateRouteMatch(match, "routes[0].rules[0]", 0)
	if err == nil {
		t.Fatal("expected error for empty path value")
	}
}

func TestValidateRouteMatch_UnspecifiedPathType(t *testing.T) {
	v := NewValidator()
	match := &pb.RouteMatch{
		Path: &pb.PathMatch{
			Type:  pb.PathMatchType_PATH_MATCH_TYPE_UNSPECIFIED,
			Value: "/api",
		},
	}
	err := v.ValidateRouteMatch(match, "routes[0].rules[0]", 0)
	if err == nil {
		t.Fatal("expected error for unspecified path type")
	}
}

func TestValidateRouteMatch_PathPrefixWithoutSlash(t *testing.T) {
	v := NewValidator()
	match := &pb.RouteMatch{
		Path: &pb.PathMatch{
			Type:  pb.PathMatchType_PATH_PREFIX,
			Value: "api",
		},
	}
	err := v.ValidateRouteMatch(match, "routes[0].rules[0]", 0)
	if err == nil {
		t.Fatal("expected error for path prefix not starting with /")
	}
}

func TestValidateRouteMatch_ExactPathWithoutSlash(t *testing.T) {
	v := NewValidator()
	match := &pb.RouteMatch{
		Path: &pb.PathMatch{
			Type:  pb.PathMatchType_EXACT,
			Value: "exact-path",
		},
	}
	err := v.ValidateRouteMatch(match, "routes[0].rules[0]", 0)
	if err == nil {
		t.Fatal("expected error for exact path not starting with /")
	}
}

func TestValidateRouteMatch_RegexPathNoSlashRequired(t *testing.T) {
	v := NewValidator()
	match := &pb.RouteMatch{
		Path: &pb.PathMatch{
			Type:  pb.PathMatchType_REGULAR_EXPRESSION,
			Value: ".*\\.json",
		},
	}
	err := v.ValidateRouteMatch(match, "routes[0].rules[0]", 0)
	if err != nil {
		t.Fatalf("expected no error for regex path without /, got %v", err)
	}
}

func TestValidateRouteMatch_InvalidMethod(t *testing.T) {
	v := NewValidator()
	match := &pb.RouteMatch{
		Method: "INVALID",
	}
	err := v.ValidateRouteMatch(match, "routes[0].rules[0]", 0)
	if err == nil {
		t.Fatal("expected error for invalid HTTP method")
	}
}

func TestValidateRouteMatch_ValidMethods(t *testing.T) {
	v := NewValidator()
	methods := []string{"GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH"}

	for _, method := range methods {
		match := &pb.RouteMatch{Method: method}
		err := v.ValidateRouteMatch(match, "routes[0].rules[0]", 0)
		if err != nil {
			t.Errorf("expected no error for method %s, got %v", method, err)
		}
	}
}

func TestValidateRouteMatch_EmptyIsValid(t *testing.T) {
	v := NewValidator()
	match := &pb.RouteMatch{}
	err := v.ValidateRouteMatch(match, "routes[0].rules[0]", 0)
	if err != nil {
		t.Fatalf("expected no error for empty match (catch-all), got %v", err)
	}
}

// --- ValidateBackendRef tests ---

func TestValidateBackendRef_Nil(t *testing.T) {
	v := NewValidator()
	err := v.ValidateBackendRef(nil, "routes[0].rules[0]", 0)
	if err == nil {
		t.Fatal("expected error for nil backend ref")
	}
}

func TestValidateBackendRef_EmptyName(t *testing.T) {
	v := NewValidator()
	ref := &pb.BackendRef{Name: "", Weight: 1}
	err := v.ValidateBackendRef(ref, "routes[0].rules[0]", 0)
	if err == nil {
		t.Fatal("expected error for empty backend ref name")
	}
}

func TestValidateBackendRef_NegativeWeight(t *testing.T) {
	v := NewValidator()
	ref := &pb.BackendRef{Name: "backend", Weight: -1}
	err := v.ValidateBackendRef(ref, "routes[0].rules[0]", 0)
	if err == nil {
		t.Fatal("expected error for negative weight")
	}
}

func TestValidateBackendRef_ZeroWeight(t *testing.T) {
	v := NewValidator()
	ref := &pb.BackendRef{Name: "backend", Weight: 0}
	err := v.ValidateBackendRef(ref, "routes[0].rules[0]", 0)
	if err != nil {
		t.Fatalf("expected no error for zero weight, got %v", err)
	}
}

func TestValidateBackendRef_Valid(t *testing.T) {
	v := NewValidator()
	ref := &pb.BackendRef{Name: "backend", Weight: 10}
	err := v.ValidateBackendRef(ref, "routes[0].rules[0]", 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// --- ValidateCluster tests ---

func TestValidateCluster_Nil(t *testing.T) {
	v := NewValidator()
	err := v.ValidateCluster(nil, 0)
	if err == nil {
		t.Fatal("expected error for nil cluster")
	}
}

func TestValidateCluster_EmptyName(t *testing.T) {
	v := NewValidator()
	cluster := &pb.Cluster{Name: "", Namespace: "default"}
	err := v.ValidateCluster(cluster, 0)
	if err == nil {
		t.Fatal("expected error for empty cluster name")
	}
}

func TestValidateCluster_EmptyNamespace(t *testing.T) {
	v := NewValidator()
	cluster := &pb.Cluster{Name: "cluster", Namespace: ""}
	err := v.ValidateCluster(cluster, 0)
	if err == nil {
		t.Fatal("expected error for empty cluster namespace")
	}
}

func TestValidateCluster_NegativeConnectTimeout(t *testing.T) {
	v := NewValidator()
	cluster := &pb.Cluster{Name: "cluster", Namespace: "default", ConnectTimeoutMs: -1}
	err := v.ValidateCluster(cluster, 0)
	if err == nil {
		t.Fatal("expected error for negative connect timeout")
	}
}

func TestValidateCluster_NegativeIdleTimeout(t *testing.T) {
	v := NewValidator()
	cluster := &pb.Cluster{Name: "cluster", Namespace: "default", IdleTimeoutMs: -1}
	err := v.ValidateCluster(cluster, 0)
	if err == nil {
		t.Fatal("expected error for negative idle timeout")
	}
}

func TestValidateCluster_Valid(t *testing.T) {
	v := NewValidator()
	cluster := &pb.Cluster{
		Name:             "cluster",
		Namespace:        "default",
		ConnectTimeoutMs: 5000,
		IdleTimeoutMs:    60000,
	}
	err := v.ValidateCluster(cluster, 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateCluster_ZeroTimeouts(t *testing.T) {
	v := NewValidator()
	cluster := &pb.Cluster{
		Name:             "cluster",
		Namespace:        "default",
		ConnectTimeoutMs: 0,
		IdleTimeoutMs:    0,
	}
	err := v.ValidateCluster(cluster, 0)
	if err != nil {
		t.Fatalf("expected no error for zero timeouts, got %v", err)
	}
}

// --- ValidateEndpointList tests ---

func TestValidateEndpointList_Nil(t *testing.T) {
	v := NewValidator()
	err := v.ValidateEndpointList("cluster-a", nil)
	if err == nil {
		t.Fatal("expected error for nil endpoint list")
	}
}

func TestValidateEndpointList_Empty(t *testing.T) {
	v := NewValidator()
	err := v.ValidateEndpointList("cluster-a", &pb.EndpointList{})
	if err != nil {
		t.Fatalf("expected no error for empty endpoint list, got %v", err)
	}
}

func TestValidateEndpointList_Valid(t *testing.T) {
	v := NewValidator()
	list := &pb.EndpointList{
		Endpoints: []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080},
			{Address: "10.0.0.2", Port: 8080},
		},
	}
	err := v.ValidateEndpointList("cluster-a", list)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateEndpointList_InvalidEndpoint(t *testing.T) {
	v := NewValidator()
	list := &pb.EndpointList{
		Endpoints: []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080},
			{Address: "10.0.0.2", Port: 0},
		},
	}
	err := v.ValidateEndpointList("cluster-a", list)
	if err == nil {
		t.Fatal("expected error for invalid endpoint port")
	}
}

// --- ValidateEndpoint tests ---

func TestValidateEndpoint_Nil(t *testing.T) {
	v := NewValidator()
	err := v.ValidateEndpoint(nil, "cluster-a", 0)
	if err == nil {
		t.Fatal("expected error for nil endpoint")
	}
}

func TestValidateEndpoint_EmptyAddress(t *testing.T) {
	v := NewValidator()
	ep := &pb.Endpoint{Address: "", Port: 80}
	err := v.ValidateEndpoint(ep, "cluster-a", 0)
	if err == nil {
		t.Fatal("expected error for empty endpoint address")
	}
}

func TestValidateEndpoint_InvalidAddress(t *testing.T) {
	v := NewValidator()
	ep := &pb.Endpoint{Address: "not an address!", Port: 80}
	err := v.ValidateEndpoint(ep, "cluster-a", 0)
	if err == nil {
		t.Fatal("expected error for invalid endpoint address")
	}
}

func TestValidateEndpoint_ValidIPv4(t *testing.T) {
	v := NewValidator()
	ep := &pb.Endpoint{Address: "10.0.0.1", Port: 80}
	err := v.ValidateEndpoint(ep, "cluster-a", 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateEndpoint_ValidIPv6(t *testing.T) {
	v := NewValidator()
	ep := &pb.Endpoint{Address: "::1", Port: 80}
	err := v.ValidateEndpoint(ep, "cluster-a", 0)
	if err != nil {
		t.Fatalf("expected no error for IPv6, got %v", err)
	}
}

func TestValidateEndpoint_ValidHostname(t *testing.T) {
	v := NewValidator()
	ep := &pb.Endpoint{Address: "backend.example.com", Port: 80}
	err := v.ValidateEndpoint(ep, "cluster-a", 0)
	if err != nil {
		t.Fatalf("expected no error for hostname, got %v", err)
	}
}

func TestValidateEndpoint_PortZero(t *testing.T) {
	v := NewValidator()
	ep := &pb.Endpoint{Address: "10.0.0.1", Port: 0}
	err := v.ValidateEndpoint(ep, "cluster-a", 0)
	if err == nil {
		t.Fatal("expected error for port 0")
	}
}

func TestValidateEndpoint_PortTooHigh(t *testing.T) {
	v := NewValidator()
	ep := &pb.Endpoint{Address: "10.0.0.1", Port: 65536}
	err := v.ValidateEndpoint(ep, "cluster-a", 0)
	if err == nil {
		t.Fatal("expected error for port > 65535")
	}
}

func TestValidateEndpoint_PortNegative(t *testing.T) {
	v := NewValidator()
	ep := &pb.Endpoint{Address: "10.0.0.1", Port: -1}
	err := v.ValidateEndpoint(ep, "cluster-a", 0)
	if err == nil {
		t.Fatal("expected error for negative port")
	}
}

func TestValidateEndpoint_ValidPortRange(t *testing.T) {
	v := NewValidator()
	ports := []int32{1, 80, 443, 8080, 65535}
	for _, port := range ports {
		ep := &pb.Endpoint{Address: "10.0.0.1", Port: port}
		err := v.ValidateEndpoint(ep, "cluster-a", 0)
		if err != nil {
			t.Errorf("expected no error for port %d, got %v", port, err)
		}
	}
}

// --- ValidateVIPAssignment tests ---

func TestValidateVIPAssignment_Nil(t *testing.T) {
	v := NewValidator()
	err := v.ValidateVIPAssignment(nil, 0)
	if err == nil {
		t.Fatal("expected error for nil VIP assignment")
	}
}

func TestValidateVIPAssignment_EmptyName(t *testing.T) {
	v := NewValidator()
	vipAssign := &pb.VIPAssignment{VipName: "", Address: "10.0.0.1", Mode: pb.VIPMode_L2_ARP}
	err := v.ValidateVIPAssignment(vipAssign, 0)
	if err == nil {
		t.Fatal("expected error for empty VIP name")
	}
}

func TestValidateVIPAssignment_EmptyAddress(t *testing.T) {
	v := NewValidator()
	vipAssign := &pb.VIPAssignment{VipName: "vip1", Address: "", Mode: pb.VIPMode_L2_ARP}
	err := v.ValidateVIPAssignment(vipAssign, 0)
	if err == nil {
		t.Fatal("expected error for empty VIP address")
	}
}

func TestValidateVIPAssignment_InvalidAddress(t *testing.T) {
	v := NewValidator()
	vipAssign := &pb.VIPAssignment{VipName: "vip1", Address: "not-an-ip", Mode: pb.VIPMode_L2_ARP}
	err := v.ValidateVIPAssignment(vipAssign, 0)
	if err == nil {
		t.Fatal("expected error for invalid VIP address")
	}
}

func TestValidateVIPAssignment_ValidIP(t *testing.T) {
	v := NewValidator()
	vipAssign := &pb.VIPAssignment{VipName: "vip1", Address: "192.168.1.100", Mode: pb.VIPMode_L2_ARP}
	err := v.ValidateVIPAssignment(vipAssign, 0)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateVIPAssignment_ValidCIDR(t *testing.T) {
	v := NewValidator()
	vipAssign := &pb.VIPAssignment{VipName: "vip1", Address: "192.168.1.100/32", Mode: pb.VIPMode_L2_ARP}
	err := v.ValidateVIPAssignment(vipAssign, 0)
	if err != nil {
		t.Fatalf("expected no error for CIDR address, got %v", err)
	}
}

func TestValidateVIPAssignment_InvalidCIDR(t *testing.T) {
	v := NewValidator()
	vipAssign := &pb.VIPAssignment{VipName: "vip1", Address: "192.168.1.100/99", Mode: pb.VIPMode_L2_ARP}
	err := v.ValidateVIPAssignment(vipAssign, 0)
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestValidateVIPAssignment_UnspecifiedMode(t *testing.T) {
	v := NewValidator()
	vipAssign := &pb.VIPAssignment{VipName: "vip1", Address: "192.168.1.100", Mode: pb.VIPMode_VIP_MODE_UNSPECIFIED}
	err := v.ValidateVIPAssignment(vipAssign, 0)
	if err == nil {
		t.Fatal("expected error for unspecified VIP mode")
	}
}

func TestValidateVIPAssignment_ValidModes(t *testing.T) {
	v := NewValidator()
	modes := []pb.VIPMode{pb.VIPMode_L2_ARP, pb.VIPMode_BGP, pb.VIPMode_OSPF}
	for _, mode := range modes {
		vipAssign := &pb.VIPAssignment{VipName: "vip1", Address: "192.168.1.100", Mode: mode}
		err := v.ValidateVIPAssignment(vipAssign, 0)
		if err != nil {
			t.Errorf("expected no error for mode %s, got %v", mode.String(), err)
		}
	}
}

func TestValidateVIPAssignment_InvalidPort(t *testing.T) {
	v := NewValidator()
	vipAssign := &pb.VIPAssignment{
		VipName: "vip1",
		Address: "192.168.1.100",
		Mode:    pb.VIPMode_L2_ARP,
		Ports:   []int32{80, 0, 443},
	}
	err := v.ValidateVIPAssignment(vipAssign, 0)
	if err == nil {
		t.Fatal("expected error for invalid VIP port")
	}
}

func TestValidateVIPAssignment_ValidPorts(t *testing.T) {
	v := NewValidator()
	vipAssign := &pb.VIPAssignment{
		VipName: "vip1",
		Address: "192.168.1.100",
		Mode:    pb.VIPMode_L2_ARP,
		Ports:   []int32{80, 443, 8080},
	}
	err := v.ValidateVIPAssignment(vipAssign, 0)
	if err != nil {
		t.Fatalf("expected no error for valid ports, got %v", err)
	}
}

func TestValidateVIPAssignment_PortTooHigh(t *testing.T) {
	v := NewValidator()
	vipAssign := &pb.VIPAssignment{
		VipName: "vip1",
		Address: "192.168.1.100",
		Mode:    pb.VIPMode_L2_ARP,
		Ports:   []int32{80, 65536},
	}
	err := v.ValidateVIPAssignment(vipAssign, 0)
	if err == nil {
		t.Fatal("expected error for VIP port > 65535")
	}
}

// --- isValidHostname tests ---

func TestIsValidHostname(t *testing.T) {
	tests := []struct {
		hostname string
		valid    bool
	}{
		{"example.com", true},
		{"sub.example.com", true},
		{"a.b.c.d.example.com", true},
		{"backend-svc", true},
		{"backend-svc.namespace.svc.cluster.local", true},
		{"", false},
		{"-invalid.com", false},
		{"invalid-.com", false},
		{"inv@lid.com", false},
		{"has space.com", false},
		{"has_underscore.com", false},
	}

	for _, tt := range tests {
		result := isValidHostname(tt.hostname)
		if result != tt.valid {
			t.Errorf("isValidHostname(%q) = %v, want %v", tt.hostname, result, tt.valid)
		}
	}
}

// --- Error type tests ---

func TestValidationErrors_AreValidationErrorType(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name string
		fn   func() error
	}{
		{"nil snapshot", func() error { return v.ValidateSnapshot(nil) }},
		{"nil gateway", func() error { return v.ValidateGateway(nil, 0) }},
		{"nil route", func() error { return v.ValidateRoute(nil, 0) }},
		{"nil cluster", func() error { return v.ValidateCluster(nil, 0) }},
		{"nil endpoint", func() error { return v.ValidateEndpoint(nil, "cluster", 0) }},
		{"nil VIP", func() error { return v.ValidateVIPAssignment(nil, 0) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err == nil {
				t.Fatal("expected error")
			}
			var validationErr *pkgerrors.ValidationError
			if !errors.As(err, &validationErr) {
				t.Errorf("expected ValidationError, got %T: %v", err, err)
			}
		})
	}
}
