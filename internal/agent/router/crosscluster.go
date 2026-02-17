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

package router

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/net/http2"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// labelRemote marks an endpoint as belonging to a remote cluster.
	labelRemote = "novaedge.io/remote"

	// labelCluster identifies which remote cluster the endpoint belongs to.
	labelCluster = "novaedge.io/cluster"

	// crossClusterDialTimeout is the timeout for establishing a tunnel to a remote gateway.
	crossClusterDialTimeout = 10 * time.Second

	// headerXForwardedCluster carries the originating cluster name through the tunnel.
	headerXForwardedCluster = "X-NovaEdge-Source-Cluster"
)

// GatewayAgent represents a gateway agent in a remote cluster that can accept
// tunneled traffic on behalf of that cluster's backends.
type GatewayAgent struct {
	// Address is the gateway agent's tunnel endpoint (host:port).
	Address string
	// Cluster is the name of the remote cluster this gateway belongs to.
	Cluster string
	// Region is the cloud region of the gateway for locality-aware selection.
	Region string
	// Zone is the availability zone of the gateway for locality-aware selection.
	Zone string
	// Healthy indicates whether this gateway is currently passing health checks.
	Healthy bool
}

// CrossClusterTunnelRegistry maintains a mapping of remote cluster names to
// their available gateway agents. It is used by the router to determine when
// an endpoint requires cross-cluster tunneling and to select the best gateway
// for forwarding.
type CrossClusterTunnelRegistry struct {
	mu       sync.RWMutex
	gateways map[string][]GatewayAgent // clusterName -> gateway agents
}

// NewCrossClusterTunnelRegistry creates an empty tunnel registry.
func NewCrossClusterTunnelRegistry() *CrossClusterTunnelRegistry {
	return &CrossClusterTunnelRegistry{
		gateways: make(map[string][]GatewayAgent),
	}
}

// UpdateGateways replaces the set of gateway agents for a given cluster.
// Pass an empty slice to mark a cluster as having no available gateways.
func (r *CrossClusterTunnelRegistry) UpdateGateways(clusterName string, gateways []GatewayAgent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Make a defensive copy so callers cannot mutate the stored slice.
	copied := make([]GatewayAgent, len(gateways))
	copy(copied, gateways)
	r.gateways[clusterName] = copied
}

// RemoveCluster removes all gateways for the specified cluster.
func (r *CrossClusterTunnelRegistry) RemoveCluster(clusterName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.gateways, clusterName)
}

// GetGateway returns the best healthy gateway for the given cluster. It iterates
// through the registered gateways and returns the first healthy one. Returns an
// error if no healthy gateway is available.
func (r *CrossClusterTunnelRegistry) GetGateway(clusterName string) (*GatewayAgent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	gateways, ok := r.gateways[clusterName]
	if !ok {
		return nil, fmt.Errorf("no gateways registered for cluster %q", clusterName)
	}

	for i := range gateways {
		if gateways[i].Healthy {
			return &gateways[i], nil
		}
	}

	return nil, fmt.Errorf("no healthy gateway available for cluster %q", clusterName)
}

// IsRemoteEndpoint checks whether the given endpoint has the novaedge.io/remote=true label,
// indicating it belongs to a remote cluster and requires cross-cluster tunneling.
func (r *CrossClusterTunnelRegistry) IsRemoteEndpoint(ep *pb.Endpoint) bool {
	if ep == nil {
		return false
	}
	labels := ep.GetLabels()
	if labels == nil {
		return false
	}
	return labels[labelRemote] == "true"
}

// GetClusterForEndpoint returns the cluster name from the endpoint's labels.
// Returns an empty string if the label is not set.
func (r *CrossClusterTunnelRegistry) GetClusterForEndpoint(ep *pb.Endpoint) string {
	if ep == nil {
		return ""
	}
	labels := ep.GetLabels()
	if labels == nil {
		return ""
	}
	return labels[labelCluster]
}

// crossClusterTransport creates an http.RoundTripper that dials through an mTLS
// HTTP/2 CONNECT tunnel to the specified gateway agent. The transport establishes
// a tunnel to the gateway, which then proxies the connection to the actual backend.
func crossClusterTransport(gatewayAddr string, tlsConfig *tls.Config) http.RoundTripper {
	return &http.Transport{
		DialContext: func(dialCtx context.Context, _, addr string) (net.Conn, error) {
			return dialViaTunnel(dialCtx, gatewayAddr, addr, tlsConfig)
		},
		// Reasonable defaults for the tunneled connection.
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   false,
		ForceAttemptHTTP2:   false,
	}
}

// dialViaTunnel establishes an HTTP/2 CONNECT tunnel through the gateway to reach
// the backend address. This is similar to mesh.TunnelPool.DialVia but designed for
// cross-cluster federation where the gateway is in a remote cluster.
func dialViaTunnel(ctx context.Context, gatewayAddr, backendAddr string, tlsConfig *tls.Config) (net.Conn, error) {
	pr, pw := io.Pipe()

	reqURL := fmt.Sprintf("https://%s", backendAddr)
	req, err := http.NewRequestWithContext(ctx, http.MethodConnect, reqURL, pr)
	if err != nil {
		_ = pw.Close()
		_ = pr.Close()
		return nil, fmt.Errorf("failed to create CONNECT request: %w", err)
	}

	transport := &http2.Transport{
		TLSClientConfig: tlsConfig,
		DialTLSContext: func(dialCtx context.Context, network, _ string, cfg *tls.Config) (net.Conn, error) {
			dialer := &tls.Dialer{
				NetDialer: &net.Dialer{Timeout: crossClusterDialTimeout},
				Config:    cfg,
			}
			return dialer.DialContext(dialCtx, network, gatewayAddr)
		},
	}

	client := &http.Client{Transport: transport}

	resp, err := client.Do(req)
	if err != nil {
		_ = pw.Close()
		return nil, fmt.Errorf("CONNECT to %s via gateway %s failed: %w", backendAddr, gatewayAddr, err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		_ = pw.Close()
		return nil, fmt.Errorf("CONNECT to %s via gateway %s returned status %d", backendAddr, gatewayAddr, resp.StatusCode)
	}

	return &tunnelStreamConn{
		reader:     resp.Body,
		writer:     pw,
		localAddr:  &net.TCPAddr{},
		remoteAddr: &net.TCPAddr{},
	}, nil
}

// tunnelStreamConn wraps an HTTP/2 CONNECT stream as a net.Conn for cross-cluster tunneling.
type tunnelStreamConn struct {
	reader     io.ReadCloser
	writer     io.WriteCloser
	localAddr  net.Addr
	remoteAddr net.Addr
}

func (c *tunnelStreamConn) Read(p []byte) (int, error)         { return c.reader.Read(p) }
func (c *tunnelStreamConn) Write(p []byte) (int, error)        { return c.writer.Write(p) }
func (c *tunnelStreamConn) LocalAddr() net.Addr                { return c.localAddr }
func (c *tunnelStreamConn) RemoteAddr() net.Addr               { return c.remoteAddr }
func (c *tunnelStreamConn) SetDeadline(_ time.Time) error      { return nil }
func (c *tunnelStreamConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *tunnelStreamConn) SetWriteDeadline(_ time.Time) error { return nil }

func (c *tunnelStreamConn) Close() error {
	rErr := c.reader.Close()
	wErr := c.writer.Close()
	if rErr != nil {
		return rErr
	}
	return wErr
}

// forwardViaTunnel routes a request to a remote cluster endpoint through an
// mTLS HTTP/2 CONNECT tunnel. It looks up the appropriate gateway agent,
// creates a reverse proxy with a custom transport that dials through the tunnel,
// and forwards the original request to the remote backend.
func (r *Router) forwardViaTunnel(_ context.Context, w http.ResponseWriter, req *http.Request, endpoint *pb.Endpoint, backendSpan trace.Span) {
	clusterName := r.tunnelRegistry.GetClusterForEndpoint(endpoint)
	if clusterName == "" {
		backendSpan.SetStatus(codes.Error, "Remote endpoint missing cluster label")
		r.logger.Error("Remote endpoint missing cluster label",
			zap.String("address", endpoint.Address),
			zap.Int32("port", endpoint.Port),
		)
		http.Error(w, "Backend configuration error", http.StatusInternalServerError)
		return
	}

	gateway, err := r.tunnelRegistry.GetGateway(clusterName)
	if err != nil {
		backendSpan.SetStatus(codes.Error, "No gateway for remote cluster")
		backendSpan.RecordError(err)
		r.logger.Error("No gateway available for remote cluster",
			zap.String("cluster", clusterName),
			zap.Error(err),
		)
		http.Error(w, "Remote cluster not available", http.StatusServiceUnavailable)
		return
	}

	backendAddr := formatEndpointKey(endpoint.Address, endpoint.Port)
	backendSpan.SetAttributes(
		attribute.String("novaedge.crosscluster.cluster", clusterName),
		attribute.String("novaedge.crosscluster.gateway", gateway.Address),
		attribute.String("novaedge.crosscluster.backend", backendAddr),
	)
	backendSpan.AddEvent("forwarding_via_tunnel", trace.WithAttributes(
		attribute.String("gateway", gateway.Address),
		attribute.String("cluster", clusterName),
	))

	r.logger.Debug("Forwarding request via cross-cluster tunnel",
		zap.String("cluster", clusterName),
		zap.String("gateway", gateway.Address),
		zap.String("backend", backendAddr),
	)

	backendStart := time.Now()

	// Build a reverse proxy that tunnels through the gateway agent.
	proxy := &httputil.ReverseProxy{
		Director: func(outReq *http.Request) {
			outReq.URL.Scheme = "http"
			outReq.URL.Host = backendAddr
			outReq.Host = req.Host

			// Carry the originating cluster name so the remote side can trace it.
			outReq.Header.Set(headerXForwardedCluster, clusterName)
		},
		Transport: crossClusterTransport(gateway.Address, r.tunnelTLSConfig),
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, proxyErr error) {
			backendDuration := time.Since(backendStart).Seconds()
			metrics.RecordBackendRequest(clusterName, backendAddr, "failure", backendDuration)

			backendSpan.RecordError(proxyErr)
			backendSpan.SetStatus(codes.Error, "Cross-cluster tunnel failed")
			backendSpan.SetAttributes(
				attribute.Float64("novaedge.backend.duration_seconds", backendDuration),
				attribute.String("novaedge.backend.status", "failure"),
			)

			if errors.Is(proxyErr, context.DeadlineExceeded) {
				r.logger.Error("Cross-cluster tunnel timeout",
					zap.String("cluster", clusterName),
					zap.String("gateway", gateway.Address),
					zap.Error(proxyErr),
				)
				http.Error(rw, "Gateway timeout", http.StatusGatewayTimeout)
				return
			}

			r.logger.Error("Cross-cluster tunnel error",
				zap.String("cluster", clusterName),
				zap.String("gateway", gateway.Address),
				zap.Error(proxyErr),
			)
			http.Error(rw, "Bad gateway", http.StatusBadGateway)
		},
		ModifyResponse: func(resp *http.Response) error {
			backendDuration := time.Since(backendStart).Seconds()
			metrics.RecordBackendRequest(clusterName, backendAddr, "success", backendDuration)

			backendSpan.SetStatus(codes.Ok, "")
			backendSpan.SetAttributes(
				attribute.Float64("novaedge.backend.duration_seconds", backendDuration),
				attribute.String("novaedge.backend.status", "success"),
				attribute.Int("http.response.status_code", resp.StatusCode),
			)

			return nil
		},
	}

	proxy.ServeHTTP(w, req)
}
