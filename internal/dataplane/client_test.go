package dataplane

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"

	pb "github.com/azrtydxb/novaedge/api/proto/dataplane"
)

// fakeDataplaneServer implements the DataplaneControlServer interface with
// minimal stubs that return success for every RPC.
type fakeDataplaneServer struct {
	pb.UnimplementedDataplaneControlServer
}

func (f *fakeDataplaneServer) ApplyConfig(_ context.Context, req *pb.ApplyConfigRequest) (*pb.ApplyConfigResponse, error) {
	return &pb.ApplyConfigResponse{
		Status:         pb.OperationStatus_OK,
		Message:        "applied",
		AppliedVersion: req.GetVersion(),
	}, nil
}

func (f *fakeDataplaneServer) UpsertGateway(_ context.Context, _ *pb.UpsertGatewayRequest) (*pb.UpsertGatewayResponse, error) {
	return &pb.UpsertGatewayResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) DeleteGateway(_ context.Context, _ *pb.DeleteGatewayRequest) (*pb.DeleteGatewayResponse, error) {
	return &pb.DeleteGatewayResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) UpsertRoute(_ context.Context, _ *pb.UpsertRouteRequest) (*pb.UpsertRouteResponse, error) {
	return &pb.UpsertRouteResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) DeleteRoute(_ context.Context, _ *pb.DeleteRouteRequest) (*pb.DeleteRouteResponse, error) {
	return &pb.DeleteRouteResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) UpsertCluster(_ context.Context, _ *pb.UpsertClusterRequest) (*pb.UpsertClusterResponse, error) {
	return &pb.UpsertClusterResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) DeleteCluster(_ context.Context, _ *pb.DeleteClusterRequest) (*pb.DeleteClusterResponse, error) {
	return &pb.DeleteClusterResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) UpsertL4Listener(_ context.Context, _ *pb.UpsertL4ListenerRequest) (*pb.UpsertL4ListenerResponse, error) {
	return &pb.UpsertL4ListenerResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) DeleteL4Listener(_ context.Context, _ *pb.DeleteL4ListenerRequest) (*pb.DeleteL4ListenerResponse, error) {
	return &pb.DeleteL4ListenerResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) UpsertPolicy(_ context.Context, _ *pb.UpsertPolicyRequest) (*pb.UpsertPolicyResponse, error) {
	return &pb.UpsertPolicyResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) DeletePolicy(_ context.Context, _ *pb.DeletePolicyRequest) (*pb.DeletePolicyResponse, error) {
	return &pb.DeletePolicyResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) UpsertMeshConfig(_ context.Context, _ *pb.UpsertMeshConfigRequest) (*pb.UpsertMeshConfigResponse, error) {
	return &pb.UpsertMeshConfigResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) DeleteMeshConfig(_ context.Context, _ *pb.DeleteMeshConfigRequest) (*pb.DeleteMeshConfigResponse, error) {
	return &pb.DeleteMeshConfigResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) UpsertWANLink(_ context.Context, _ *pb.UpsertWANLinkRequest) (*pb.UpsertWANLinkResponse, error) {
	return &pb.UpsertWANLinkResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) DeleteWANLink(_ context.Context, _ *pb.DeleteWANLinkRequest) (*pb.DeleteWANLinkResponse, error) {
	return &pb.DeleteWANLinkResponse{Status: pb.OperationStatus_OK}, nil
}

func (f *fakeDataplaneServer) GetDataplaneStatus(_ context.Context, _ *pb.GetDataplaneStatusRequest) (*pb.DataplaneStatus, error) {
	return &pb.DataplaneStatus{
		Mode:              "proxy",
		ActiveConnections: 42,
		UptimeSeconds:     3600,
		ConfigVersion:     "v1",
	}, nil
}

// startFakeDataplaneServer creates a gRPC server listening on a temporary
// Unix domain socket and returns the socket path and a cleanup function.
// It uses /tmp directly to avoid exceeding the macOS 104-char socket path limit.
func startFakeDataplaneServer(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("/tmp", "dp-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	sockPath := filepath.Join(tmpDir, "dp.sock")

	lc := net.ListenConfig{}
	lis, err := lc.Listen(context.Background(), "unix", sockPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to listen on unix socket: %v", err)
	}

	srv := grpc.NewServer()
	pb.RegisterDataplaneControlServer(srv, &fakeDataplaneServer{})

	go func() {
		if serveErr := srv.Serve(lis); serveErr != nil {
			// Server was stopped; expected during cleanup.
			return
		}
	}()

	cleanup := func() {
		srv.GracefulStop()
		_ = os.RemoveAll(tmpDir)
	}

	return sockPath, cleanup
}

func TestNewClient(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	if client.socketPath != sockPath {
		t.Errorf("expected socketPath %q, got %q", sockPath, client.socketPath)
	}
	if client.conn == nil {
		t.Error("expected conn to be non-nil")
	}
	if client.client == nil {
		t.Error("expected gRPC client to be non-nil")
	}
}

func TestNewClient_NoSocket(t *testing.T) {
	// grpc.NewClient with lazy connection should still succeed even if the
	// socket does not exist yet.
	logger := zaptest.NewLogger(t)
	client, err := NewClient("/tmp/nonexistent-dataplane.sock", logger)
	if err != nil {
		t.Fatalf("NewClient() should succeed with lazy connection, got error: %v", err)
	}
	defer func() { _ = client.Close() }()
}

func TestClient_ApplyConfig(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.ApplyConfig(ctx, &pb.ApplyConfigRequest{
		Version: "v42",
	})
	if err != nil {
		t.Fatalf("ApplyConfig() error: %v", err)
	}
	if resp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("expected status OK, got %v", resp.GetStatus())
	}
	if resp.GetAppliedVersion() != "v42" {
		t.Errorf("expected applied version v42, got %q", resp.GetAppliedVersion())
	}
}

func TestClient_UpsertDeleteGateway(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uResp, err := client.UpsertGateway(ctx, &pb.UpsertGatewayRequest{
		Gateway: &pb.GatewayConfig{Name: "gw-1", Port: 8080},
	})
	if err != nil {
		t.Fatalf("UpsertGateway() error: %v", err)
	}
	if uResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("UpsertGateway: expected OK, got %v", uResp.GetStatus())
	}

	dResp, err := client.DeleteGateway(ctx, &pb.DeleteGatewayRequest{Name: "gw-1"})
	if err != nil {
		t.Fatalf("DeleteGateway() error: %v", err)
	}
	if dResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("DeleteGateway: expected OK, got %v", dResp.GetStatus())
	}
}

func TestClient_UpsertDeleteRoute(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uResp, err := client.UpsertRoute(ctx, &pb.UpsertRouteRequest{
		Route: &pb.RouteConfig{Name: "rt-1"},
	})
	if err != nil {
		t.Fatalf("UpsertRoute() error: %v", err)
	}
	if uResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("UpsertRoute: expected OK, got %v", uResp.GetStatus())
	}

	dResp, err := client.DeleteRoute(ctx, &pb.DeleteRouteRequest{Name: "rt-1"})
	if err != nil {
		t.Fatalf("DeleteRoute() error: %v", err)
	}
	if dResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("DeleteRoute: expected OK, got %v", dResp.GetStatus())
	}
}

func TestClient_UpsertDeleteCluster(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uResp, err := client.UpsertCluster(ctx, &pb.UpsertClusterRequest{
		Cluster: &pb.ClusterConfig{Name: "cl-1"},
	})
	if err != nil {
		t.Fatalf("UpsertCluster() error: %v", err)
	}
	if uResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("UpsertCluster: expected OK, got %v", uResp.GetStatus())
	}

	dResp, err := client.DeleteCluster(ctx, &pb.DeleteClusterRequest{Name: "cl-1"})
	if err != nil {
		t.Fatalf("DeleteCluster() error: %v", err)
	}
	if dResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("DeleteCluster: expected OK, got %v", dResp.GetStatus())
	}
}

func TestClient_UpsertDeleteL4Listener(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uResp, err := client.UpsertL4Listener(ctx, &pb.UpsertL4ListenerRequest{
		Listener: &pb.L4ListenerConfig{Name: "l4-1", Port: 9090},
	})
	if err != nil {
		t.Fatalf("UpsertL4Listener() error: %v", err)
	}
	if uResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("UpsertL4Listener: expected OK, got %v", uResp.GetStatus())
	}

	dResp, err := client.DeleteL4Listener(ctx, &pb.DeleteL4ListenerRequest{Name: "l4-1"})
	if err != nil {
		t.Fatalf("DeleteL4Listener() error: %v", err)
	}
	if dResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("DeleteL4Listener: expected OK, got %v", dResp.GetStatus())
	}
}

func TestClient_UpsertDeletePolicy(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uResp, err := client.UpsertPolicy(ctx, &pb.UpsertPolicyRequest{
		Policy: &pb.PolicyConfig{Name: "pol-1"},
	})
	if err != nil {
		t.Fatalf("UpsertPolicy() error: %v", err)
	}
	if uResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("UpsertPolicy: expected OK, got %v", uResp.GetStatus())
	}

	dResp, err := client.DeletePolicy(ctx, &pb.DeletePolicyRequest{Name: "pol-1"})
	if err != nil {
		t.Fatalf("DeletePolicy() error: %v", err)
	}
	if dResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("DeletePolicy: expected OK, got %v", dResp.GetStatus())
	}
}

func TestClient_UpsertDeleteMeshConfig(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uResp, err := client.UpsertMeshConfig(ctx, &pb.UpsertMeshConfigRequest{
		MeshConfig: &pb.MeshConfig{Enabled: true},
	})
	if err != nil {
		t.Fatalf("UpsertMeshConfig() error: %v", err)
	}
	if uResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("UpsertMeshConfig: expected OK, got %v", uResp.GetStatus())
	}

	dResp, err := client.DeleteMeshConfig(ctx, &pb.DeleteMeshConfigRequest{})
	if err != nil {
		t.Fatalf("DeleteMeshConfig() error: %v", err)
	}
	if dResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("DeleteMeshConfig: expected OK, got %v", dResp.GetStatus())
	}
}

func TestClient_UpsertDeleteWANLink(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uResp, err := client.UpsertWANLink(ctx, &pb.UpsertWANLinkRequest{
		WanLink: &pb.WANLinkConfig{Name: "wan-1", Interface: "eth0"},
	})
	if err != nil {
		t.Fatalf("UpsertWANLink() error: %v", err)
	}
	if uResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("UpsertWANLink: expected OK, got %v", uResp.GetStatus())
	}

	dResp, err := client.DeleteWANLink(ctx, &pb.DeleteWANLinkRequest{Name: "wan-1"})
	if err != nil {
		t.Fatalf("DeleteWANLink() error: %v", err)
	}
	if dResp.GetStatus() != pb.OperationStatus_OK {
		t.Errorf("DeleteWANLink: expected OK, got %v", dResp.GetStatus())
	}
}

func TestClient_GetDataplaneStatus(t *testing.T) {
	sockPath, cleanup := startFakeDataplaneServer(t)
	defer cleanup()

	logger := zaptest.NewLogger(t)
	client, err := NewClient(sockPath, logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := client.GetDataplaneStatus(ctx)
	if err != nil {
		t.Fatalf("GetDataplaneStatus() error: %v", err)
	}
	if status.GetMode() != "proxy" {
		t.Errorf("expected mode 'proxy', got %q", status.GetMode())
	}
	if status.GetActiveConnections() != 42 {
		t.Errorf("expected 42 active connections, got %d", status.GetActiveConnections())
	}
	if status.GetUptimeSeconds() != 3600 {
		t.Errorf("expected 3600 uptime seconds, got %d", status.GetUptimeSeconds())
	}
}

func TestClient_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client, err := NewClient("/tmp/nonexistent.sock", logger)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestClient_Close_Nil(t *testing.T) {
	client := &Client{logger: zap.NewNop()}
	if err := client.Close(); err != nil {
		t.Errorf("Close() on nil conn should return nil, got: %v", err)
	}
}
