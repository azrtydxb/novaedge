// Package dataplane provides a Go gRPC client for communicating with the
// Rust dataplane daemon over a Unix domain socket.
package dataplane

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/azrtydxb/novaedge/api/proto/dataplane"
)

// defaultRPCTimeout bounds how long any single unary RPC will wait for the
// dataplane to respond. This prevents callers from blocking indefinitely when
// the Rust daemon is not running, while still allowing WaitForReady retries
// within the window. The value is set to 30s to accommodate the startup race
// where the Go agent receives config before the Rust dataplane socket exists
// (eBPF loading on ARM64 can take several seconds on first boot).
const defaultRPCTimeout = 30 * time.Second

// Client wraps the gRPC connection to the Rust dataplane daemon.
type Client struct {
	socketPath string
	conn       *grpc.ClientConn
	client     pb.DataplaneControlClient
	logger     *zap.Logger
}

// NewClient creates a new dataplane gRPC client connected to the given
// Unix domain socket path. The connection uses insecure credentials since
// the socket is local and protected by filesystem permissions.
func NewClient(socketPath string, logger *zap.Logger) (*Client, error) {
	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	if err != nil {
		return nil, fmt.Errorf("dataplane: dial %s: %w", socketPath, err)
	}

	return &Client{
		socketPath: socketPath,
		conn:       conn,
		client:     pb.NewDataplaneControlClient(conn),
		logger:     logger,
	}, nil
}

// Close tears down the gRPC connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Full config push
// ---------------------------------------------------------------------------

// ApplyConfig pushes a full configuration snapshot to the dataplane.
func (c *Client) ApplyConfig(ctx context.Context, req *pb.ApplyConfigRequest) (*pb.ApplyConfigResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.ApplyConfig(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: ApplyConfig: %w", err)
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// Gateway / Listener management
// ---------------------------------------------------------------------------

// UpsertGateway creates or updates an HTTP/HTTPS/HTTP3/TCP/UDP listener.
func (c *Client) UpsertGateway(ctx context.Context, req *pb.UpsertGatewayRequest) (*pb.UpsertGatewayResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.UpsertGateway(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: UpsertGateway: %w", err)
	}
	return resp, nil
}

// DeleteGateway removes a listener by name.
func (c *Client) DeleteGateway(ctx context.Context, req *pb.DeleteGatewayRequest) (*pb.DeleteGatewayResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.DeleteGateway(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: DeleteGateway: %w", err)
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// L7 Route management
// ---------------------------------------------------------------------------

// UpsertRoute creates or updates an L7 routing rule.
func (c *Client) UpsertRoute(ctx context.Context, req *pb.UpsertRouteRequest) (*pb.UpsertRouteResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.UpsertRoute(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: UpsertRoute: %w", err)
	}
	return resp, nil
}

// DeleteRoute removes a route by name.
func (c *Client) DeleteRoute(ctx context.Context, req *pb.DeleteRouteRequest) (*pb.DeleteRouteResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.DeleteRoute(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: DeleteRoute: %w", err)
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// Backend cluster management
// ---------------------------------------------------------------------------

// UpsertCluster creates or updates a backend cluster.
func (c *Client) UpsertCluster(ctx context.Context, req *pb.UpsertClusterRequest) (*pb.UpsertClusterResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.UpsertCluster(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: UpsertCluster: %w", err)
	}
	return resp, nil
}

// DeleteCluster removes a backend cluster by name.
func (c *Client) DeleteCluster(ctx context.Context, req *pb.DeleteClusterRequest) (*pb.DeleteClusterResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.DeleteCluster(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: DeleteCluster: %w", err)
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// L4 Listener management
// ---------------------------------------------------------------------------

// UpsertL4Listener creates or updates a TCP/UDP/TLS passthrough listener.
func (c *Client) UpsertL4Listener(ctx context.Context, req *pb.UpsertL4ListenerRequest) (*pb.UpsertL4ListenerResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.UpsertL4Listener(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: UpsertL4Listener: %w", err)
	}
	return resp, nil
}

// DeleteL4Listener removes an L4 listener by name.
func (c *Client) DeleteL4Listener(ctx context.Context, req *pb.DeleteL4ListenerRequest) (*pb.DeleteL4ListenerResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.DeleteL4Listener(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: DeleteL4Listener: %w", err)
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// Policy management
// ---------------------------------------------------------------------------

// UpsertPolicy creates or updates a typed policy (rate-limit, auth, WAF, etc.).
func (c *Client) UpsertPolicy(ctx context.Context, req *pb.UpsertPolicyRequest) (*pb.UpsertPolicyResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.UpsertPolicy(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: UpsertPolicy: %w", err)
	}
	return resp, nil
}

// DeletePolicy removes a policy by name.
func (c *Client) DeletePolicy(ctx context.Context, req *pb.DeletePolicyRequest) (*pb.DeletePolicyResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.DeletePolicy(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: DeletePolicy: %w", err)
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// Mesh config
// ---------------------------------------------------------------------------

// UpsertMeshConfig creates or updates the service-mesh configuration.
func (c *Client) UpsertMeshConfig(ctx context.Context, req *pb.UpsertMeshConfigRequest) (*pb.UpsertMeshConfigResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.UpsertMeshConfig(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: UpsertMeshConfig: %w", err)
	}
	return resp, nil
}

// DeleteMeshConfig removes the mesh configuration.
func (c *Client) DeleteMeshConfig(ctx context.Context, req *pb.DeleteMeshConfigRequest) (*pb.DeleteMeshConfigResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.DeleteMeshConfig(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: DeleteMeshConfig: %w", err)
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// SD-WAN
// ---------------------------------------------------------------------------

// UpsertWANLink creates or updates a WAN link.
func (c *Client) UpsertWANLink(ctx context.Context, req *pb.UpsertWANLinkRequest) (*pb.UpsertWANLinkResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.UpsertWANLink(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: UpsertWANLink: %w", err)
	}
	return resp, nil
}

// DeleteWANLink removes a WAN link by name.
func (c *Client) DeleteWANLink(ctx context.Context, req *pb.DeleteWANLinkRequest) (*pb.DeleteWANLinkResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.DeleteWANLink(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: DeleteWANLink: %w", err)
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// Observability
// ---------------------------------------------------------------------------

// StreamFlows opens a server-streaming RPC that returns flow events captured
// from the eBPF ring buffer. The returned channel is closed when the stream
// ends or the context is cancelled.
func (c *Client) StreamFlows(ctx context.Context, req *pb.StreamFlowsRequest) (<-chan *pb.FlowEvent, error) {
	stream, err := c.client.StreamFlows(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: StreamFlows: %w", err)
	}

	ch := make(chan *pb.FlowEvent, 64)
	go func() {
		defer close(ch)
		for {
			event, recvErr := stream.Recv()
			if recvErr != nil {
				if !errors.Is(recvErr, io.EOF) {
					c.logger.Warn("StreamFlows recv error", zap.Error(recvErr))
				}
				return
			}
			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// StreamMetrics opens a server-streaming RPC that returns periodic metrics
// snapshots from the dataplane. The returned channel is closed when the stream
// ends or the context is cancelled.
func (c *Client) StreamMetrics(ctx context.Context, req *pb.StreamMetricsRequest) (<-chan *pb.MetricsSnapshot, error) {
	stream, err := c.client.StreamMetrics(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("dataplane: StreamMetrics: %w", err)
	}

	ch := make(chan *pb.MetricsSnapshot, 64)
	go func() {
		defer close(ch)
		for {
			snapshot, recvErr := stream.Recv()
			if recvErr != nil {
				if !errors.Is(recvErr, io.EOF) {
					c.logger.Warn("StreamMetrics recv error", zap.Error(recvErr))
				}
				return
			}
			select {
			case ch <- snapshot:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// GetDataplaneStatus returns the current dataplane status.
func (c *Client) GetDataplaneStatus(ctx context.Context) (*pb.DataplaneStatus, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultRPCTimeout)
	defer cancel()
	resp, err := c.client.GetDataplaneStatus(callCtx, &pb.GetDataplaneStatusRequest{})
	if err != nil {
		return nil, fmt.Errorf("dataplane: GetDataplaneStatus: %w", err)
	}
	return resp, nil
}
