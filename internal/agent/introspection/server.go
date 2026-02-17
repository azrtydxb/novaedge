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

// Package introspection provides a gRPC server for agent runtime state inspection.
package introspection

import (
	"context"
	"math"
	"net"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// StateProvider provides access to agent runtime state.
type StateProvider interface {
	GetCurrentSnapshot() *pb.ConfigSnapshot
}

// Server implements gRPC introspection RPCs for the agent.
type Server struct {
	pb.UnimplementedConfigServiceServer
	provider StateProvider
	logger   *zap.Logger
}

// NewServer creates a new introspection server.
func NewServer(provider StateProvider, logger *zap.Logger) *Server {
	return &Server{provider: provider, logger: logger}
}

// safeInt32 safely converts an int to int32, clamping to max int32 on overflow.
func safeInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v) //nolint:gosec // overflow handled above
}

// GetAgentConfig returns a summary of the current agent configuration.
func (s *Server) GetAgentConfig(_ context.Context, _ *pb.GetConfigRequest) (*pb.GetConfigResponse, error) {
	snap := s.provider.GetCurrentSnapshot()
	if snap == nil {
		return &pb.GetConfigResponse{}, nil
	}
	var endpointCount int
	for _, epList := range snap.Endpoints {
		endpointCount += len(epList.Endpoints)
	}
	return &pb.GetConfigResponse{
		Version:       snap.Version,
		GatewayCount:  safeInt32(len(snap.Gateways)),
		RouteCount:    safeInt32(len(snap.Routes)),
		ClusterCount:  safeInt32(len(snap.Clusters)),
		EndpointCount: safeInt32(endpointCount),
		VipCount:      safeInt32(len(snap.VipAssignments)),
		PolicyCount:   safeInt32(len(snap.Policies)),
	}, nil
}

// GetBackendHealth returns health information for all backend clusters.
func (s *Server) GetBackendHealth(_ context.Context, _ *pb.GetBackendHealthRequest) (*pb.GetBackendHealthResponse, error) {
	snap := s.provider.GetCurrentSnapshot()
	if snap == nil {
		return &pb.GetBackendHealthResponse{}, nil
	}
	backends := make([]*pb.BackendHealthInfo, 0, len(snap.Clusters))
	for _, cluster := range snap.Clusters {
		bh := &pb.BackendHealthInfo{
			ClusterName: cluster.Name,
			Namespace:   cluster.Namespace,
			LbPolicy:    cluster.LbPolicy.String(),
		}
		if epList, ok := snap.Endpoints[cluster.Name]; ok {
			for _, ep := range epList.Endpoints {
				bh.Endpoints = append(bh.Endpoints, &pb.EndpointHealthInfo{
					Address: ep.Address,
					Port:    ep.Port,
					Healthy: ep.Ready,
				})
			}
		}
		backends = append(backends, bh)
	}
	return &pb.GetBackendHealthResponse{Backends: backends}, nil
}

// GetVIPs returns VIP assignment information from the current snapshot.
func (s *Server) GetVIPs(_ context.Context, _ *pb.GetVIPsRequest) (*pb.GetVIPsResponse, error) {
	snap := s.provider.GetCurrentSnapshot()
	if snap == nil {
		return &pb.GetVIPsResponse{}, nil
	}
	vips := make([]*pb.VIPInfoResponse, 0, len(snap.VipAssignments))
	for _, v := range snap.VipAssignments {
		vips = append(vips, &pb.VIPInfoResponse{
			Name:     v.VipName,
			Address:  v.Address,
			Mode:     v.Mode.String(),
			IsActive: v.IsActive,
			Ports:    v.Ports,
		})
	}
	return &pb.GetVIPsResponse{Vips: vips}, nil
}

// Start starts the gRPC introspection server and blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context, addr string) error {
	lc := net.ListenConfig{}
	lis, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	pb.RegisterConfigServiceServer(grpcServer, s)
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()
	s.logger.Info("introspection gRPC server started", zap.String("addr", addr))
	return grpcServer.Serve(lis)
}
