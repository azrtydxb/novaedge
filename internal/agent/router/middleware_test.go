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
	"testing"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/config"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestCreatePolicyMiddleware_TargetRefKindFiltering(t *testing.T) {
	logger := zap.NewNop()
	r := NewRouter(logger)

	route := &pb.Route{
		Name:      "my-route",
		Namespace: "default",
	}

	snapshot := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Policies: []*pb.Policy{
				{
					Name:      "gateway-rate-limit",
					Namespace: "default",
					Type:      pb.PolicyType_RATE_LIMIT,
					TargetRef: &pb.TargetRef{
						Kind:      "ProxyGateway",
						Name:      "my-route",
						Namespace: "default",
					},
					RateLimit: &pb.RateLimitConfig{
						RequestsPerSecond: 100,
						Burst:             50,
					},
				},
				{
					Name:      "route-rate-limit",
					Namespace: "default",
					Type:      pb.PolicyType_RATE_LIMIT,
					TargetRef: &pb.TargetRef{
						Kind:      "ProxyRoute",
						Name:      "my-route",
						Namespace: "default",
					},
					RateLimit: &pb.RateLimitConfig{
						RequestsPerSecond: 50,
						Burst:             25,
					},
				},
			},
		},
	}

	middlewares := r.createPolicyMiddleware(context.Background(), route, snapshot)

	// Should only match the ProxyRoute policy, not the ProxyGateway one
	if len(middlewares) != 1 {
		t.Fatalf("Expected 1 middleware (only ProxyRoute target), got %d", len(middlewares))
	}

	if middlewares[0].name != "rate-limit-route-rate-limit" {
		t.Errorf("Expected middleware name 'rate-limit-route-rate-limit', got %q", middlewares[0].name)
	}
}

func TestCreatePolicyMiddleware_SecurityHeaders(t *testing.T) {
	logger := zap.NewNop()
	r := NewRouter(logger)

	route := &pb.Route{
		Name:      "api-route",
		Namespace: "default",
	}

	snapshot := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Policies: []*pb.Policy{
				{
					Name:      "sec-headers",
					Namespace: "default",
					Type:      pb.PolicyType_SECURITY_HEADERS,
					TargetRef: &pb.TargetRef{
						Kind:      "ProxyRoute",
						Name:      "api-route",
						Namespace: "default",
					},
					SecurityHeaders: &pb.SecurityHeadersConfig{
						Hsts: &pb.HSTSConfig{
							Enabled:       true,
							MaxAgeSeconds: 31536000,
						},
						XFrameOptions:       "DENY",
						XContentTypeOptions: true,
					},
				},
			},
		},
	}

	middlewares := r.createPolicyMiddleware(context.Background(), route, snapshot)

	if len(middlewares) != 1 {
		t.Fatalf("Expected 1 middleware, got %d", len(middlewares))
	}

	if middlewares[0].name != "security-headers-sec-headers" {
		t.Errorf("Expected middleware name 'security-headers-sec-headers', got %q", middlewares[0].name)
	}

	// Verify the middleware handler is not nil
	if middlewares[0].handler == nil {
		t.Error("Expected non-nil handler for security headers middleware")
	}
}

func TestCreatePolicyMiddleware_NilTargetRef(t *testing.T) {
	logger := zap.NewNop()
	r := NewRouter(logger)

	route := &pb.Route{
		Name:      "my-route",
		Namespace: "default",
	}

	snapshot := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Policies: []*pb.Policy{
				{
					Name:      "no-target",
					Namespace: "default",
					Type:      pb.PolicyType_RATE_LIMIT,
					TargetRef: nil,
					RateLimit: &pb.RateLimitConfig{
						RequestsPerSecond: 100,
					},
				},
			},
		},
	}

	middlewares := r.createPolicyMiddleware(context.Background(), route, snapshot)

	// Should skip policies with nil targetRef
	if len(middlewares) != 0 {
		t.Fatalf("Expected 0 middlewares for nil targetRef, got %d", len(middlewares))
	}
}

func TestCreatePolicyMiddleware_NameMismatch(t *testing.T) {
	logger := zap.NewNop()
	r := NewRouter(logger)

	route := &pb.Route{
		Name:      "my-route",
		Namespace: "default",
	}

	snapshot := &config.Snapshot{
		ConfigSnapshot: &pb.ConfigSnapshot{
			Policies: []*pb.Policy{
				{
					Name:      "other-policy",
					Namespace: "default",
					Type:      pb.PolicyType_CORS,
					TargetRef: &pb.TargetRef{
						Kind:      "ProxyRoute",
						Name:      "other-route",
						Namespace: "default",
					},
					Cors: &pb.CORSConfig{
						AllowOrigins: []string{"*"},
					},
				},
			},
		},
	}

	middlewares := r.createPolicyMiddleware(context.Background(), route, snapshot)

	if len(middlewares) != 0 {
		t.Fatalf("Expected 0 middlewares for name mismatch, got %d", len(middlewares))
	}
}
