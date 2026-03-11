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

// Package grpclimits provides shared gRPC configuration for message size
// limits and interceptors used by the controller-agent communication channel.
package grpclimits

import (
	"context"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	// DefaultMaxRecvMsgSize is the maximum inbound message size (16 MiB).
	// ConfigSnapshots can be large for clusters with many routes/endpoints.
	DefaultMaxRecvMsgSize = 16 * 1024 * 1024

	// DefaultMaxSendMsgSize is the maximum outbound message size (16 MiB).
	DefaultMaxSendMsgSize = 16 * 1024 * 1024

	// DefaultMaxConcurrentStreams limits concurrent gRPC streams per connection.
	DefaultMaxConcurrentStreams = 100
)

// ServerOptions returns gRPC server options with message size limits,
// keepalive enforcement, and logging interceptors.
func ServerOptions(logger *zap.Logger) []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.MaxRecvMsgSize(DefaultMaxRecvMsgSize),
		grpc.MaxSendMsgSize(DefaultMaxSendMsgSize),
		grpc.MaxConcurrentStreams(DefaultMaxConcurrentStreams),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 5 * time.Minute,
			Time:              1 * time.Minute,
			Timeout:           20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.ChainUnaryInterceptor(
			unaryServerLoggingInterceptor(logger),
		),
		grpc.ChainStreamInterceptor(
			streamServerLoggingInterceptor(logger),
		),
	}
}

// ClientOptions returns gRPC dial options with message size limits and
// keepalive parameters.
func ClientOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(DefaultMaxRecvMsgSize),
			grpc.MaxCallSendMsgSize(DefaultMaxSendMsgSize),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                1 * time.Minute,
			Timeout:             20 * time.Second,
			PermitWithoutStream: true,
		}),
	}
}

// unaryServerLoggingInterceptor logs unary RPC calls with peer info.
func unaryServerLoggingInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		peerAddr := "unknown"
		if p, ok := peer.FromContext(ctx); ok {
			peerAddr = p.Addr.String()
		}

		if ce := logger.Check(zap.DebugLevel, "gRPC unary call"); ce != nil {
			ce.Write(
				zap.String("method", info.FullMethod),
				zap.String("peer", peerAddr),
			)
		}

		resp, err := handler(ctx, req)
		if err != nil {
			st, _ := status.FromError(err)
			if st.Code() != codes.OK {
				logger.Warn("gRPC unary call failed",
					zap.String("method", info.FullMethod),
					zap.String("peer", peerAddr),
					zap.String("code", st.Code().String()),
					zap.Error(err),
				)
			}
		}
		return resp, err
	}
}

// streamServerLoggingInterceptor logs streaming RPC calls with peer info.
func streamServerLoggingInterceptor(logger *zap.Logger) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		peerAddr := "unknown"
		if p, ok := peer.FromContext(ss.Context()); ok {
			peerAddr = p.Addr.String()
		}

		if ce := logger.Check(zap.DebugLevel, "gRPC stream started"); ce != nil {
			ce.Write(
				zap.String("method", info.FullMethod),
				zap.String("peer", peerAddr),
			)
		}

		err := handler(srv, ss)
		if err != nil {
			st, _ := status.FromError(err)
			if st.Code() != codes.OK {
				logger.Warn("gRPC stream failed",
					zap.String("method", info.FullMethod),
					zap.String("peer", peerAddr),
					zap.String("code", st.Code().String()),
					zap.Error(err),
				)
			}
		}
		return err
	}
}
