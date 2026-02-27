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

package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	v1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

var (
	errRelayEndpointIsRequiredForWebSocketTunnelType = errors.New("relay endpoint is required for WebSocket tunnel type")
)

const (
	wsDialTimeout  = 10 * time.Second
	wsPingInterval = 15 * time.Second
	wsPongTimeout  = 10 * time.Second
)

// webSocketTunnel implements the Tunnel interface using WebSocket connections.
// It dials a WebSocket connection to the remote gateway and creates a local
// TCP listener that bridges connections through the WebSocket as a
// bidirectional byte stream.
type webSocketTunnel struct {
	mu          sync.RWMutex
	clusterName string
	config      v1alpha1.TunnelConfig
	localAddr   string
	healthy     atomic.Bool
	logger      *zap.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	listener    net.Listener
	wsConn      *websocket.Conn
}

// newWebSocketTunnel creates a WebSocket tunnel instance.
func newWebSocketTunnel(clusterName string, config v1alpha1.TunnelConfig, logger *zap.Logger) (*webSocketTunnel, error) {
	if config.RelayEndpoint == "" {
		return nil, errRelayEndpointIsRequiredForWebSocketTunnelType
	}

	return &webSocketTunnel{
		clusterName: clusterName,
		config:      config,
		logger:      logger.With(zap.String("tunnel", "websocket"), zap.String("cluster", clusterName)),
		done:        make(chan struct{}),
	}, nil
}

// Start begins the WebSocket tunnel connection and maintenance loop.
func (t *webSocketTunnel) Start(ctx context.Context) error {
	t.mu.Lock()
	t.ctx, t.cancel = context.WithCancel(ctx)
	t.mu.Unlock()

	go t.maintainConnection(t.ctx)

	return nil
}

// Stop gracefully shuts down the WebSocket tunnel.
func (t *webSocketTunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cancel != nil {
		t.cancel()
	}

	<-t.done

	t.healthy.Store(false)
	t.closeConnections()

	return nil
}

// IsHealthy returns whether the WebSocket tunnel is connected.
func (t *webSocketTunnel) IsHealthy() bool {
	return t.healthy.Load()
}

// LocalAddr returns the local listener address for dialing through the tunnel.
func (t *webSocketTunnel) LocalAddr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.localAddr
}

// OverlayAddr returns an empty string as WebSocket tunnels do not participate in overlay routing.
func (t *webSocketTunnel) OverlayAddr() string {
	return ""
}

// Type returns the tunnel type identifier.
func (t *webSocketTunnel) Type() string {
	return "websocket"
}

// maintainConnection keeps the WebSocket tunnel connected with exponential backoff.
func (t *webSocketTunnel) maintainConnection(ctx context.Context) {
	maintainTunnelConnection(ctx, t.done, &t.healthy, &t.mu, t.logger, "websocket",
		t.LocalAddr, t.connect, t.runForwardingLoop, t.closeConnections)
}

// connect establishes the WebSocket connection and local listener.
func (t *webSocketTunnel) connect(ctx context.Context) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: wsDialTimeout,
	}

	wsURL := t.config.RelayEndpoint
	wsConn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dialing websocket %s: %w", wsURL, err)
	}

	// Create local TCP listener on a random port
	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		_ = wsConn.Close()
		return fmt.Errorf("creating local listener: %w", err)
	}

	t.mu.Lock()
	t.wsConn = wsConn
	t.listener = listener
	t.localAddr = listener.Addr().String()
	t.mu.Unlock()

	return nil
}

// runForwardingLoop accepts local connections and bridges them through the WebSocket.
func (t *webSocketTunnel) runForwardingLoop(ctx context.Context) {
	// Start ping/pong keepalive
	go t.pingLoop(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		t.mu.RLock()
		listener := t.listener
		t.mu.RUnlock()

		if listener == nil {
			return
		}

		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			t.logger.Debug("listener accept error", zap.Error(err))
			return
		}

		go t.bridgeToWebSocket(ctx, conn)
	}
}

// bridgeToWebSocket bridges a local TCP connection to the WebSocket.
// Each accepted connection gets its own WebSocket binary message stream.
func (t *webSocketTunnel) bridgeToWebSocket(ctx context.Context, localConn net.Conn) {
	defer func() { _ = localConn.Close() }()

	t.mu.RLock()
	ws := t.wsConn
	t.mu.RUnlock()

	if ws == nil {
		return
	}

	done := make(chan struct{})

	// Read from WebSocket, write to local connection
	go func() {
		defer close(done)
		for {
			_, reader, err := ws.NextReader()
			if err != nil {
				return
			}
			if _, err := io.Copy(localConn, reader); err != nil {
				return
			}
		}
	}()

	// Read from local connection, write to WebSocket
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := localConn.Read(buf)
			if n > 0 {
				t.mu.RLock()
				wsConn := t.wsConn
				t.mu.RUnlock()
				if wsConn == nil {
					return
				}
				if writeErr := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}
}

// pingLoop sends periodic WebSocket ping frames to detect connection loss.
func (t *webSocketTunnel) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.mu.RLock()
			ws := t.wsConn
			t.mu.RUnlock()

			if ws == nil {
				return
			}

			if err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(wsPongTimeout)); err != nil {
				t.logger.Debug("websocket ping failed", zap.Error(err))
				// Close listener to break the accept loop
				t.mu.RLock()
				if t.listener != nil {
					_ = t.listener.Close()
				}
				t.mu.RUnlock()
				return
			}
		}
	}
}

// closeConnections closes the WebSocket connection and local listener.
func (t *webSocketTunnel) closeConnections() {
	if t.listener != nil {
		_ = t.listener.Close()
		t.listener = nil
	}

	if t.wsConn != nil {
		_ = t.wsConn.Close()
		t.wsConn = nil
	}
}
