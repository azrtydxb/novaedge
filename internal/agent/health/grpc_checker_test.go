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

package health

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// fakeHealthServer implements the gRPC Health service for testing.
type fakeHealthServer struct {
	healthpb.UnimplementedHealthServer
	status    healthpb.HealthCheckResponse_ServingStatus
	returnErr error
}

func (f *fakeHealthServer) Check(
	_ context.Context,
	_ *healthpb.HealthCheckRequest,
) (*healthpb.HealthCheckResponse, error) {
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return &healthpb.HealthCheckResponse{
		Status: f.status,
	}, nil
}

// startFakeHealthServer starts a gRPC server with the given fake health
// service and returns its address and a cleanup function.
func startFakeHealthServer(t *testing.T, srv *fakeHealthServer) (string, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	healthpb.RegisterHealthServer(grpcServer, srv)

	go func() {
		if serveErr := grpcServer.Serve(lis); serveErr != nil {
			// Server was stopped; this is expected during cleanup.
			return
		}
	}()

	cleanup := func() {
		grpcServer.GracefulStop()
	}

	return lis.Addr().String(), cleanup
}

func TestGRPCHealthChecker_Serving(t *testing.T) {
	srv := &fakeHealthServer{
		status: healthpb.HealthCheckResponse_SERVING,
	}
	addr, cleanup := startFakeHealthServer(t, srv)
	defer cleanup()

	checker := &GRPCHealthChecker{
		ServiceName: "",
		Timeout:     2 * time.Second,
	}

	healthy, err := checker.Check(context.Background(), addr)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !healthy {
		t.Error("expected healthy=true for SERVING status")
	}
}

func TestGRPCHealthChecker_NotServing(t *testing.T) {
	srv := &fakeHealthServer{
		status: healthpb.HealthCheckResponse_NOT_SERVING,
	}
	addr, cleanup := startFakeHealthServer(t, srv)
	defer cleanup()

	checker := &GRPCHealthChecker{
		ServiceName: "myservice",
		Timeout:     2 * time.Second,
	}

	healthy, err := checker.Check(context.Background(), addr)
	if err == nil {
		t.Fatal("expected error for NOT_SERVING status")
	}
	if healthy {
		t.Error("expected healthy=false for NOT_SERVING status")
	}
}

func TestGRPCHealthChecker_ConnectionFailure(t *testing.T) {
	// Use an address where nothing is listening
	checker := &GRPCHealthChecker{
		ServiceName: "",
		Timeout:     1 * time.Second,
	}

	healthy, err := checker.Check(context.Background(), "127.0.0.1:19876")
	if err == nil {
		t.Fatal("expected error for connection failure")
	}
	if healthy {
		t.Error("expected healthy=false for connection failure")
	}
}

func TestGRPCHealthChecker_Timeout(t *testing.T) {
	// Create a server that returns NOT_FOUND to simulate an unregistered service,
	// but we test timeout by using an already-cancelled context.
	srv := &fakeHealthServer{
		status: healthpb.HealthCheckResponse_SERVING,
	}
	addr, cleanup := startFakeHealthServer(t, srv)
	defer cleanup()

	checker := &GRPCHealthChecker{
		ServiceName: "",
		Timeout:     2 * time.Second,
	}

	// Use a pre-cancelled context to simulate a timeout scenario
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	healthy, err := checker.Check(ctx, addr)
	if err == nil {
		t.Fatal("expected error for timed-out/cancelled context")
	}
	if healthy {
		t.Error("expected healthy=false for timeout")
	}
}

func TestGRPCHealthChecker_UnknownStatus(t *testing.T) {
	srv := &fakeHealthServer{
		status: healthpb.HealthCheckResponse_UNKNOWN,
	}
	addr, cleanup := startFakeHealthServer(t, srv)
	defer cleanup()

	checker := &GRPCHealthChecker{
		ServiceName: "",
		Timeout:     2 * time.Second,
	}

	healthy, err := checker.Check(context.Background(), addr)
	if err == nil {
		t.Fatal("expected error for UNKNOWN status")
	}
	if healthy {
		t.Error("expected healthy=false for UNKNOWN status")
	}
}

func TestGRPCHealthChecker_RPCError(t *testing.T) {
	srv := &fakeHealthServer{
		returnErr: status.Error(codes.Unavailable, "service unavailable"),
	}
	addr, cleanup := startFakeHealthServer(t, srv)
	defer cleanup()

	checker := &GRPCHealthChecker{
		ServiceName: "broken",
		Timeout:     2 * time.Second,
	}

	healthy, err := checker.Check(context.Background(), addr)
	if err == nil {
		t.Fatal("expected error for RPC failure")
	}
	if healthy {
		t.Error("expected healthy=false for RPC error")
	}
}

func TestGRPCHealthChecker_DefaultTimeout(t *testing.T) {
	srv := &fakeHealthServer{
		status: healthpb.HealthCheckResponse_SERVING,
	}
	addr, cleanup := startFakeHealthServer(t, srv)
	defer cleanup()

	// Checker with zero timeout should use the default
	checker := &GRPCHealthChecker{
		ServiceName: "",
		Timeout:     0,
	}

	healthy, err := checker.Check(context.Background(), addr)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !healthy {
		t.Error("expected healthy=true for SERVING status with default timeout")
	}
}
