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

// Package tunnel provides optional network tunnel implementations for cross-cluster NAT/firewall traversal.
package tunnel

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Tunnel is the interface that all network tunnel implementations must satisfy.
// Each tunnel provides L3/L4 connectivity to a remote cluster for NAT/firewall
// traversal, sitting underneath the mTLS HTTP/2 CONNECT tunnel.
type Tunnel interface {
	// Start initiates the tunnel connection and begins the maintenance loop.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the tunnel and releases resources.
	Stop() error

	// IsHealthy returns true if the tunnel is currently connected and operational.
	IsHealthy() bool

	// LocalAddr returns the local endpoint address to dial through this tunnel.
	// Callers use this address to reach the remote cluster via the tunnel.
	LocalAddr() string

	// OverlayAddr returns the overlay network address assigned to this tunnel,
	// or an empty string if the tunnel does not participate in overlay routing.
	OverlayAddr() string

	// Type returns the tunnel type identifier ("wireguard", "ssh", or "websocket").
	Type() string
}

// maintainTunnelConnection is a shared reconnection loop used by SSH and WebSocket tunnels.
// It handles exponential backoff, health tracking, and the connect/forward/cleanup lifecycle.
func maintainTunnelConnection(
	ctx context.Context,
	done chan struct{},
	healthy *atomic.Bool,
	mu *sync.RWMutex,
	logger *zap.Logger,
	tunnelType string,
	localAddr func() string,
	connect func(context.Context) error,
	runForwardingLoop func(context.Context),
	closeConnections func(),
) {
	defer close(done)

	backoff := time.Second
	for {
		if err := connect(ctx); err != nil {
			logger.Error(tunnelType+" connection failed", zap.Error(err), zap.Duration("backoff", backoff))
			healthy.Store(false)

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			backoff = minDuration(backoff*2, maxBackoff)
			continue
		}

		healthy.Store(true)
		backoff = time.Second
		logger.Info(tunnelType+" tunnel established", zap.String("localAddr", localAddr()))

		runForwardingLoop(ctx)

		mu.Lock()
		closeConnections()
		mu.Unlock()
		healthy.Store(false)

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}
