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

package integration

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/piwi3910/novaedge/internal/agent/config"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// CreateMockBackend creates a simple HTTP backend that returns the given status and body
func (s *IntegrationTestSuite) CreateMockBackend(status int, body string) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	})

	srv := httptest.NewServer(handler)
	s.backendServers = append(s.backendServers, srv)
	return srv
}

// CreateMockBackendWithID creates a backend that returns an ID as response body
func (s *IntegrationTestSuite) CreateMockBackendWithID(status int, id string) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(id))
	})

	srv := httptest.NewServer(handler)
	s.backendServers = append(s.backendServers, srv)
	return srv
}

// CreateMockBackendWithLatency creates a backend that adds latency to responses
func (s *IntegrationTestSuite) CreateMockBackendWithLatency(status int, body string, latencyMS int) *httptest.Server {
	return s.CreateMockBackend(status, body)
}

// CreateMockBackendWithStatusCode creates a backend that returns a specific status code
func (s *IntegrationTestSuite) CreateMockBackendWithStatusCode(status int) *httptest.Server {
	return s.CreateMockBackend(status, fmt.Sprintf("Status: %d", status))
}

// CreateConfigSnapshot creates a basic configuration snapshot with a single backend
func (s *IntegrationTestSuite) CreateConfigSnapshot(
	routeName string,
	hostname string,
	backendURL string,
	numEndpoints int,
) *config.Snapshot {
	clusterName := "test-cluster"
	clusterNamespace := "default"
	clusterKey := fmt.Sprintf("%s/%s", clusterNamespace, clusterName)

	// Extract host and port from backend URL
	host := extractHost(backendURL)
	port := extractPort(backendURL)

	// Create endpoints
	endpoints := make([]*pb.Endpoint, numEndpoints)
	for i := 0; i < numEndpoints; i++ {
		endpoints[i] = &pb.Endpoint{
			Address: host,
			Port:    int32(port),
			Ready:   true,
		}
	}

	return &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Gateways: []*pb.Gateway{
				{
					Name:      "test-gateway",
					Namespace: clusterNamespace,
					VipRef:    "test-vip",
					Listeners: []*pb.Listener{
						{
							Name:     "http",
							Port:     8080,
							Protocol: pb.Protocol_HTTP,
						},
					},
				},
			},
			Routes: []*pb.Route{
				{
					Name:      routeName,
					Namespace: clusterNamespace,
					Hostnames: []string{hostname},
					Rules: []*pb.RouteRule{
						{
							BackendRefs: []*pb.BackendRef{
								{
									Name:      clusterName,
									Namespace: clusterNamespace,
									Weight:    100,
								},
							},
						},
					},
				},
			},
			Clusters: []*pb.Cluster{
				{
					Name:      clusterName,
					Namespace: clusterNamespace,
					LbPolicy:  pb.LoadBalancingPolicy_ROUND_ROBIN,
				},
			},
			Endpoints: map[string]*pb.EndpointList{
				clusterKey: {
					Endpoints: endpoints,
				},
			},
			VipAssignments: []*pb.VIPAssignment{
				{
					VipName:  "test-vip",
					IsActive: true,
					Ports:    []int32{8080},
				},
			},
		},
	}
}

// CreateConfigSnapshotWithMultipleBackends creates a configuration with multiple backend servers
func (s *IntegrationTestSuite) CreateConfigSnapshotWithMultipleBackends(
	routeName string,
	hostname string,
	backends []*httptest.Server,
	lbPolicy pb.LoadBalancingPolicy,
) *config.Snapshot {
	clusterName := "test-cluster"
	clusterNamespace := "default"
	clusterKey := fmt.Sprintf("%s/%s", clusterNamespace, clusterName)

	// Create endpoints from all backends
	endpoints := make([]*pb.Endpoint, len(backends))
	for i, backend := range backends {
		host := extractHost(backend.URL)
		port := extractPort(backend.URL)
		endpoints[i] = &pb.Endpoint{
			Address: host,
			Port:    int32(port),
			Ready:   true,
		}
	}

	return &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Gateways: []*pb.Gateway{
				{
					Name:      "test-gateway",
					Namespace: clusterNamespace,
					VipRef:    "test-vip",
					Listeners: []*pb.Listener{
						{
							Name:     "http",
							Port:     8080,
							Protocol: pb.Protocol_HTTP,
						},
					},
				},
			},
			Routes: []*pb.Route{
				{
					Name:      routeName,
					Namespace: clusterNamespace,
					Hostnames: []string{hostname},
					Rules: []*pb.RouteRule{
						{
							BackendRefs: []*pb.BackendRef{
								{
									Name:      clusterName,
									Namespace: clusterNamespace,
									Weight:    100,
								},
							},
						},
					},
				},
			},
			Clusters: []*pb.Cluster{
				{
					Name:      clusterName,
					Namespace: clusterNamespace,
					LbPolicy:  lbPolicy,
				},
			},
			Endpoints: map[string]*pb.EndpointList{
				clusterKey: {
					Endpoints: endpoints,
				},
			},
			VipAssignments: []*pb.VIPAssignment{
				{
					VipName:  "test-vip",
					IsActive: true,
					Ports:    []int32{8080},
				},
			},
		},
	}
}

// CreateConfigSnapshotWithFilters creates a configuration with route filters
func (s *IntegrationTestSuite) CreateConfigSnapshotWithFilters(
	routeName string,
	hostname string,
	backendURL string,
	filters []*pb.RouteFilter,
) *config.Snapshot {
	clusterName := "test-cluster"
	clusterNamespace := "default"
	clusterKey := fmt.Sprintf("%s/%s", clusterNamespace, clusterName)

	host := extractHost(backendURL)
	port := extractPort(backendURL)

	return &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Gateways: []*pb.Gateway{
				{
					Name:      "test-gateway",
					Namespace: clusterNamespace,
					VipRef:    "test-vip",
					Listeners: []*pb.Listener{
						{
							Name:     "http",
							Port:     8080,
							Protocol: pb.Protocol_HTTP,
						},
					},
				},
			},
			Routes: []*pb.Route{
				{
					Name:      routeName,
					Namespace: clusterNamespace,
					Hostnames: []string{hostname},
					Rules: []*pb.RouteRule{
						{
							Filters: filters,
							BackendRefs: []*pb.BackendRef{
								{
									Name:      clusterName,
									Namespace: clusterNamespace,
									Weight:    100,
								},
							},
						},
					},
				},
			},
			Clusters: []*pb.Cluster{
				{
					Name:      clusterName,
					Namespace: clusterNamespace,
					LbPolicy:  pb.LoadBalancingPolicy_ROUND_ROBIN,
				},
			},
			Endpoints: map[string]*pb.EndpointList{
				clusterKey: {
					Endpoints: []*pb.Endpoint{
						{
							Address: host,
							Port:    int32(port),
							Ready:   true,
						},
					},
				},
			},
			VipAssignments: []*pb.VIPAssignment{
				{
					VipName:  "test-vip",
					IsActive: true,
					Ports:    []int32{8080},
				},
			},
		},
	}
}

// CreateConfigSnapshotWithPolicies creates a configuration with policies
func (s *IntegrationTestSuite) CreateConfigSnapshotWithPolicies(
	routeName string,
	hostname string,
	backendURL string,
	policies []*pb.Policy,
) *config.Snapshot {
	clusterName := "test-cluster"
	clusterNamespace := "default"
	clusterKey := fmt.Sprintf("%s/%s", clusterNamespace, clusterName)

	host := extractHost(backendURL)
	port := extractPort(backendURL)

	return &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Gateways: []*pb.Gateway{
				{
					Name:      "test-gateway",
					Namespace: clusterNamespace,
					VipRef:    "test-vip",
					Listeners: []*pb.Listener{
						{
							Name:     "http",
							Port:     8080,
							Protocol: pb.Protocol_HTTP,
						},
					},
				},
			},
			Routes: []*pb.Route{
				{
					Name:      routeName,
					Namespace: clusterNamespace,
					Hostnames: []string{hostname},
					Rules: []*pb.RouteRule{
						{
							BackendRefs: []*pb.BackendRef{
								{
									Name:      clusterName,
									Namespace: clusterNamespace,
									Weight:    100,
								},
							},
						},
					},
				},
			},
			Clusters: []*pb.Cluster{
				{
					Name:      clusterName,
					Namespace: clusterNamespace,
					LbPolicy:  pb.LoadBalancingPolicy_ROUND_ROBIN,
				},
			},
			Endpoints: map[string]*pb.EndpointList{
				clusterKey: {
					Endpoints: []*pb.Endpoint{
						{
							Address: host,
							Port:    int32(port),
							Ready:   true,
						},
					},
				},
			},
			Policies: policies,
			VipAssignments: []*pb.VIPAssignment{
				{
					VipName:  "test-vip",
					IsActive: true,
					Ports:    []int32{8080},
				},
			},
		},
	}
}

// CreateTLSConfigSnapshot creates a configuration with TLS enabled
func (s *IntegrationTestSuite) CreateTLSConfigSnapshot(
	routeName string,
	hostname string,
	backendURL string,
	tlsCert []byte,
	tlsKey []byte,
) *config.Snapshot {
	clusterName := "test-cluster"
	clusterNamespace := "default"
	clusterKey := fmt.Sprintf("%s/%s", clusterNamespace, clusterName)

	host := extractHost(backendURL)
	port := extractPort(backendURL)

	return &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Gateways: []*pb.Gateway{
				{
					Name:      "test-gateway",
					Namespace: clusterNamespace,
					VipRef:    "test-vip",
					Listeners: []*pb.Listener{
						{
							Name:     "https",
							Port:     8443,
							Protocol: pb.Protocol_HTTPS,
							Tls: &pb.TLSConfig{
								Cert: tlsCert,
								Key:  tlsKey,
							},
						},
					},
				},
			},
			Routes: []*pb.Route{
				{
					Name:      routeName,
					Namespace: clusterNamespace,
					Hostnames: []string{hostname},
					Rules: []*pb.RouteRule{
						{
							BackendRefs: []*pb.BackendRef{
								{
									Name:      clusterName,
									Namespace: clusterNamespace,
									Weight:    100,
								},
							},
						},
					},
				},
			},
			Clusters: []*pb.Cluster{
				{
					Name:      clusterName,
					Namespace: clusterNamespace,
					LbPolicy:  pb.LoadBalancingPolicy_ROUND_ROBIN,
				},
			},
			Endpoints: map[string]*pb.EndpointList{
				clusterKey: {
					Endpoints: []*pb.Endpoint{
						{
							Address: host,
							Port:    int32(port),
							Ready:   true,
						},
					},
				},
			},
			VipAssignments: []*pb.VIPAssignment{
				{
					VipName:  "test-vip",
					IsActive: true,
					Ports:    []int32{8443},
				},
			},
		},
	}
}

// CreateConfigSnapshotWithHTTP2 creates a configuration with HTTP/2 enabled
func (s *IntegrationTestSuite) CreateConfigSnapshotWithHTTP2(
	routeName string,
	hostname string,
	backendURL string,
) *config.Snapshot {
	clusterName := "test-cluster"
	clusterNamespace := "default"
	clusterKey := fmt.Sprintf("%s/%s", clusterNamespace, clusterName)

	host := extractHost(backendURL)
	port := extractPort(backendURL)

	return &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Version: "v1",
			Gateways: []*pb.Gateway{
				{
					Name:      "test-gateway",
					Namespace: clusterNamespace,
					VipRef:    "test-vip",
					Listeners: []*pb.Listener{
						{
							Name:     "https",
							Port:     8443,
							Protocol: pb.Protocol_HTTPS,
						},
					},
				},
			},
			Routes: []*pb.Route{
				{
					Name:      routeName,
					Namespace: clusterNamespace,
					Hostnames: []string{hostname},
					Rules: []*pb.RouteRule{
						{
							BackendRefs: []*pb.BackendRef{
								{
									Name:      clusterName,
									Namespace: clusterNamespace,
									Weight:    100,
								},
							},
						},
					},
				},
			},
			Clusters: []*pb.Cluster{
				{
					Name:      clusterName,
					Namespace: clusterNamespace,
					LbPolicy:  pb.LoadBalancingPolicy_ROUND_ROBIN,
				},
			},
			Endpoints: map[string]*pb.EndpointList{
				clusterKey: {
					Endpoints: []*pb.Endpoint{
						{
							Address: host,
							Port:    int32(port),
							Ready:   true,
						},
					},
				},
			},
			VipAssignments: []*pb.VIPAssignment{
				{
					VipName:  "test-vip",
					IsActive: true,
					Ports:    []int32{8443},
				},
			},
		},
	}
}

// extractHost extracts hostname from a URL
func extractHost(urlStr string) string {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return "localhost"
	}

	// Split by : to remove port
	hostPart := parsed.Host
	if idx := strings.Index(hostPart, ":"); idx != -1 {
		hostPart = hostPart[:idx]
	}

	// If it's localhost, map to 127.0.0.1 for consistency
	if hostPart == "localhost" || hostPart == "127.0.0.1" {
		return "127.0.0.1"
	}

	return hostPart
}

// extractPort extracts port number from a URL
func extractPort(urlStr string) int {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return 8080
	}

	// Try to get port from host:port
	host := parsed.Host
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		portStr := host[idx+1:]
		// Try to parse as integer
		var port int
		fmt.Sscanf(portStr, "%d", &port)
		if port > 0 {
			return port
		}
	}

	// Default based on scheme
	switch parsed.Scheme {
	case "https":
		return 443
	default:
		return 80
	}
}

// CreateMockBackendWithCustomHandler creates a backend with a custom HTTP handler
func (s *IntegrationTestSuite) CreateMockBackendWithCustomHandler(handler http.HandlerFunc) *httptest.Server {
	srv := httptest.NewServer(handler)
	s.backendServers = append(s.backendServers, srv)
	return srv
}

// CreateMultipleMockBackends creates multiple mock backends with incrementing IDs
func (s *IntegrationTestSuite) CreateMultipleMockBackends(count int) []*httptest.Server {
	servers := make([]*httptest.Server, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("backend-%d", i)
		servers[i] = s.CreateMockBackendWithID(http.StatusOK, id)
	}
	return servers
}

// WaitForServerReady waits for an httptest.Server to be ready
func WaitForServerReady(srv *httptest.Server, maxAttempts int) bool {
	for i := 0; i < maxAttempts; i++ {
		resp, err := http.Get(srv.URL + "/health")
		if err == nil {
			resp.Body.Close()
			return true
		}
	}
	return false
}

// SimulateBackendFailure simulates a backend failure by closing its connection
// Note: This is a helper for testing resilience patterns
func SimulateBackendFailure(endpoint *pb.Endpoint) string {
	return fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)
}

// GetLocalIP returns a local IP address suitable for testing
func GetLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// HealthEndpoint represents a health check result
type HealthEndpoint struct {
	Address    string
	Port       int
	Healthy    bool
	LastChecked int64
}
