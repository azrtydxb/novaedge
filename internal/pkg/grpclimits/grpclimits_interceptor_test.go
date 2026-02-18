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

package grpclimits

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const testRequest = "test-request"

// peerAddr implements net.Addr for testing
type peerAddr string

func (p peerAddr) Network() string {
	return "tcp"
}

func (p peerAddr) String() string {
	return string(p)
}

// mockServerStream implements grpc.ServerStream for testing
type mockServerStream struct {
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func (m *mockServerStream) SendMsg(interface{}) error {
	return nil
}

func (m *mockServerStream) RecvMsg(interface{}) error {
	return nil
}

func (m *mockServerStream) SetHeader(metadata.MD) error {
	return nil
}

func (m *mockServerStream) SendHeader(metadata.MD) error {
	return nil
}

func (m *mockServerStream) SetTrailer(metadata.MD) {
}

func TestUnaryServerLoggingInterceptor_Success(t *testing.T) {
	logger := zap.NewNop()
	interceptor := unaryServerLoggingInterceptor(logger)

	ctx := context.Background()
	req := testRequest
	info := &grpc.UnaryServerInfo{
		FullMethod: "/test/Method",
	}
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return "test-response", nil
	}

	resp, err := interceptor(ctx, req, info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "test-response", resp)
}

func TestUnaryServerLoggingInterceptor_WithPeer(t *testing.T) {
	logger := zap.NewNop()
	interceptor := unaryServerLoggingInterceptor(logger)

	// Create context with peer info
	p := &peer.Peer{
		Addr: peerAddr("192.168.1.1:12345"),
	}
	ctx := peer.NewContext(context.Background(), p)

	req := testRequest
	info := &grpc.UnaryServerInfo{
		FullMethod: "/test/Method",
	}
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return "test-response", nil
	}

	resp, err := interceptor(ctx, req, info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "test-response", resp)
}

func TestUnaryServerLoggingInterceptor_HandlerError(t *testing.T) {
	logger := zap.NewNop()
	interceptor := unaryServerLoggingInterceptor(logger)

	ctx := context.Background()
	req := testRequest
	info := &grpc.UnaryServerInfo{
		FullMethod: "/test/Method",
	}
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return nil, status.Error(codes.Internal, "test error")
	}

	resp, err := interceptor(ctx, req, info, handler)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestUnaryServerLoggingInterceptor_WithError(t *testing.T) {
	logger := zap.NewNop()
	interceptor := unaryServerLoggingInterceptor(logger)

	ctx := context.Background()
	req := testRequest
	info := &grpc.UnaryServerInfo{
		FullMethod: "/test/Method",
	}
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return nil, errors.New("handler error")
	}

	resp, err := interceptor(ctx, req, info, handler)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestStreamServerLoggingInterceptor_Success(t *testing.T) {
	logger := zap.NewNop()
	interceptor := streamServerLoggingInterceptor(logger)

	ctx := context.Background()
	ss := &mockServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test/StreamMethod",
	}
	handler := func(_ interface{}, _ grpc.ServerStream) error {
		return nil
	}

	err := interceptor("test-srv", ss, info, handler)
	assert.NoError(t, err)
}

func TestStreamServerLoggingInterceptor_WithPeer(t *testing.T) {
	logger := zap.NewNop()
	interceptor := streamServerLoggingInterceptor(logger)

	// Create context with peer info
	p := &peer.Peer{
		Addr: peerAddr("10.0.0.1:54321"),
	}
	ctx := peer.NewContext(context.Background(), p)

	ss := &mockServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test/StreamMethod",
	}
	handler := func(_ interface{}, _ grpc.ServerStream) error {
		return nil
	}

	err := interceptor("test-srv", ss, info, handler)
	assert.NoError(t, err)
}

func TestStreamServerLoggingInterceptor_HandlerError(t *testing.T) {
	logger := zap.NewNop()
	interceptor := streamServerLoggingInterceptor(logger)

	ctx := context.Background()
	ss := &mockServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test/StreamMethod",
	}
	handler := func(_ interface{}, _ grpc.ServerStream) error {
		return status.Error(codes.Unavailable, "service unavailable")
	}

	err := interceptor("test-srv", ss, info, handler)
	assert.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestStreamServerLoggingInterceptor_NonGRPCError(t *testing.T) {
	logger := zap.NewNop()
	interceptor := streamServerLoggingInterceptor(logger)

	ctx := context.Background()
	ss := &mockServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test/StreamMethod",
	}
	handler := func(_ interface{}, _ grpc.ServerStream) error {
		return errors.New("random error")
	}

	err := interceptor("test-srv", ss, info, handler)
	assert.Error(t, err)
}

func TestStreamServerLoggingInterceptor_WithError(t *testing.T) {
	logger := zap.NewNop()
	interceptor := streamServerLoggingInterceptor(logger)

	ctx := context.Background()
	ss := &mockServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test/StreamMethod",
	}
	handler := func(_ interface{}, _ grpc.ServerStream) error {
		return errors.New("stream error")
	}

	err := interceptor("test-srv", ss, info, handler)
	assert.Error(t, err)
}

func TestServerOptions_WithDebugLogger(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	opts := ServerOptions(logger)

	assert.NotEmpty(t, opts)
	// Should have at least 7 options
	assert.GreaterOrEqual(t, len(opts), 7)
}

func TestClientOptions_NotEmpty(t *testing.T) {
	opts := ClientOptions()

	assert.NotEmpty(t, opts)
	// Should have at least 2 options
	assert.GreaterOrEqual(t, len(opts), 2)
}
