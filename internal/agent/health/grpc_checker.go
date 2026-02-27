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
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

var (
	errGrpcHealthCheckService = errors.New("grpc health check: service")
)

// GRPCHealthChecker performs health checks using the standard gRPC health
// checking protocol (grpc.health.v1.Health/Check).
type GRPCHealthChecker struct {
	// ServiceName is the gRPC service name to check. An empty string checks
	// the overall server health status.
	ServiceName string

	// Interval between consecutive health checks.
	Interval time.Duration

	// Timeout for each individual health check RPC.
	Timeout time.Duration

	// UnhealthyThreshold is the number of consecutive failures required
	// before marking the endpoint as unhealthy.
	UnhealthyThreshold int

	// HealthyThreshold is the number of consecutive successes required
	// before marking the endpoint as healthy.
	HealthyThreshold int
}

// Check dials the given address using gRPC, sends a Health/Check RPC for
// the configured service name, and returns whether the endpoint is healthy.
// The endpoint is considered healthy only when the response status is SERVING.
// All other statuses (NOT_SERVING, UNKNOWN, SERVICE_UNKNOWN), connection
// failures, and timeouts are treated as unhealthy.
func (g *GRPCHealthChecker) Check(ctx context.Context, address string) (bool, error) {
	timeout := g.Timeout
	if timeout == 0 {
		timeout = DefaultHealthCheckTimeout
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, timeout)
	defer dialCancel()

	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return false, fmt.Errorf("grpc health check: failed to create client for %s: %w", address, err)
	}
	defer func() { _ = conn.Close() }()

	client := healthpb.NewHealthClient(conn)

	resp, err := client.Check(dialCtx, &healthpb.HealthCheckRequest{
		Service: g.ServiceName,
	})
	if err != nil {
		return false, fmt.Errorf("grpc health check: RPC failed for %s: %w", address, err)
	}

	if resp.GetStatus() == healthpb.HealthCheckResponse_SERVING {
		return true, nil
	}

	return false, fmt.Errorf("%w %q on %s reported status %s",
		errGrpcHealthCheckService, g.ServiceName, address, resp.GetStatus().String())
}
