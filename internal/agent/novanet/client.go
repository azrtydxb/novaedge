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

// Package novanet provides a gRPC client for NovaNet's EBPFServices API.
// NovaEdge delegates all kernel-level eBPF operations (SOCKMAP, mesh
// redirects, rate limiting, passive health monitoring) to the NovaNet
// agent running on the same node via a Unix domain socket.
package novanet

import (
	"context"
	"sync"

	pb "github.com/azrtydxb/novaedge/api/proto/ebpfservices"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DefaultSocketPath is the default Unix domain socket where the NovaNet
// agent exposes its EBPFServices gRPC API.
const DefaultSocketPath = "/var/run/novanet/ebpf-services.sock"

// Client wraps a gRPC connection to the NovaNet EBPFServices API.
// When the NovaNet agent is unavailable, the client operates in degraded
// mode — all methods return nil gracefully so that the mesh and agent
// continue to function without eBPF acceleration.
type Client struct {
	socketPath string
	logger     *zap.Logger
	mu         sync.RWMutex
	conn       *grpc.ClientConn
	client     pb.EBPFServicesClient
	connected  bool
}

// NewClient creates a new NovaNet client targeting the given socket path.
func NewClient(socketPath string, logger *zap.Logger) *Client {
	return &Client{socketPath: socketPath, logger: logger}
}

// Connect establishes the gRPC connection to the NovaNet agent. If the
// connection fails, the client enters degraded mode and logs a warning
// rather than returning an error — this allows NovaEdge to start even
// when NovaNet is not yet running.
func (c *Client) Connect(ctx context.Context) error {
	conn, err := grpc.NewClient("unix://"+c.socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		c.logger.Warn("NovaNet eBPF services unavailable, running in degraded mode", zap.Error(err))
		return nil
	}
	c.mu.Lock()
	c.conn = conn
	c.client = pb.NewEBPFServicesClient(conn)
	c.connected = true
	c.mu.Unlock()
	c.logger.Info("connected to NovaNet eBPF services", zap.String("socket", c.socketPath))
	return nil
}

// IsConnected reports whether the client has an active connection to NovaNet.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// EnableSockmap requests NovaNet to enable SOCKMAP acceleration for a pod.
func (c *Client) EnableSockmap(ctx context.Context, namespace, name string) error {
	if !c.IsConnected() {
		return nil
	}
	_, err := c.client.EnableSockmap(ctx, &pb.EnableSockmapRequest{PodNamespace: namespace, PodName: name})
	return err
}

// DisableSockmap requests NovaNet to disable SOCKMAP acceleration for a pod.
func (c *Client) DisableSockmap(ctx context.Context, namespace, name string) error {
	if !c.IsConnected() {
		return nil
	}
	_, err := c.client.DisableSockmap(ctx, &pb.DisableSockmapRequest{PodNamespace: namespace, PodName: name})
	return err
}

// AddMeshRedirect requests NovaNet to add an sk_lookup mesh redirect rule.
func (c *Client) AddMeshRedirect(ctx context.Context, ip string, port, redirectPort uint32) error {
	if !c.IsConnected() {
		return nil
	}
	_, err := c.client.AddMeshRedirect(ctx, &pb.AddMeshRedirectRequest{
		Ip:           ip,
		Port:         port,
		RedirectPort: redirectPort,
	})
	return err
}

// RemoveMeshRedirect requests NovaNet to remove an sk_lookup mesh redirect rule.
func (c *Client) RemoveMeshRedirect(ctx context.Context, ip string, port uint32) error {
	if !c.IsConnected() {
		return nil
	}
	_, err := c.client.RemoveMeshRedirect(ctx, &pb.RemoveMeshRedirectRequest{Ip: ip, Port: port})
	return err
}

// ConfigureRateLimit requests NovaNet to configure kernel-level rate limiting.
func (c *Client) ConfigureRateLimit(ctx context.Context, cidr string, rate, burst uint32) error {
	if !c.IsConnected() {
		return nil
	}
	_, err := c.client.ConfigureRateLimit(ctx, &pb.ConfigureRateLimitRequest{
		Cidr:  cidr,
		Rate:  rate,
		Burst: burst,
	})
	return err
}

// RemoveRateLimit requests NovaNet to remove a rate limit rule.
func (c *Client) RemoveRateLimit(ctx context.Context, cidr string) error {
	if !c.IsConnected() {
		return nil
	}
	_, err := c.client.RemoveRateLimit(ctx, &pb.RemoveRateLimitRequest{Cidr: cidr})
	return err
}

// GetRateLimitStats retrieves rate limiting statistics from NovaNet.
func (c *Client) GetRateLimitStats(ctx context.Context, cidr string) (allowed, denied uint64, err error) {
	if !c.IsConnected() {
		return 0, 0, nil
	}
	resp, err := c.client.GetRateLimitStats(ctx, &pb.GetRateLimitStatsRequest{Cidr: cidr})
	if err != nil {
		return 0, 0, err
	}
	return resp.Allowed, resp.Denied, nil
}

// GetBackendHealth retrieves passive health monitoring data for a backend.
func (c *Client) GetBackendHealth(ctx context.Context, ip string, port uint32) (*pb.BackendHealthInfo, error) {
	if !c.IsConnected() {
		return nil, nil
	}
	resp, err := c.client.GetBackendHealth(ctx, &pb.GetBackendHealthRequest{Ip: ip, Port: port})
	if err != nil {
		return nil, err
	}
	if len(resp.Backends) == 0 {
		return nil, nil
	}
	return resp.Backends[0], nil
}

// Close shuts down the gRPC connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
