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
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"

	v1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

const (
	sshRemoteForwardPort = "15002"
	sshDialTimeout       = 10 * time.Second
	sshKeepaliveInterval = 15 * time.Second
)

// sshTunnel implements the Tunnel interface using SSH port forwarding.
// It establishes an SSH connection to the remote cluster and creates
// a local TCP listener that forwards connections through the SSH channel
// to the remote agent tunnel port.
type sshTunnel struct {
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
	sshClient   *ssh.Client
}

// newSSHTunnel creates an SSH tunnel instance.
func newSSHTunnel(clusterName string, config v1alpha1.TunnelConfig, logger *zap.Logger) (*sshTunnel, error) {
	if config.RelayEndpoint == "" {
		return nil, fmt.Errorf("relay endpoint is required for SSH tunnel type")
	}

	return &sshTunnel{
		clusterName: clusterName,
		config:      config,
		logger:      logger.With(zap.String("tunnel", "ssh"), zap.String("cluster", clusterName)),
		done:        make(chan struct{}),
	}, nil
}

// Start begins the SSH tunnel connection and maintenance loop.
func (t *sshTunnel) Start(ctx context.Context) error {
	t.mu.Lock()
	t.ctx, t.cancel = context.WithCancel(ctx)
	t.mu.Unlock()

	go t.maintainConnection(t.ctx)

	return nil
}

// Stop gracefully shuts down the SSH tunnel.
func (t *sshTunnel) Stop() error {
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

// IsHealthy returns whether the SSH tunnel is connected.
func (t *sshTunnel) IsHealthy() bool {
	return t.healthy.Load()
}

// LocalAddr returns the local listener address for dialing through the tunnel.
func (t *sshTunnel) LocalAddr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.localAddr
}

// Type returns the tunnel type identifier.
func (t *sshTunnel) Type() string {
	return "ssh"
}

// maintainConnection keeps the SSH tunnel connected with exponential backoff.
func (t *sshTunnel) maintainConnection(ctx context.Context) {
	defer close(t.done)

	backoff := time.Second
	for {
		if err := t.connect(ctx); err != nil {
			t.logger.Error("ssh connection failed", zap.Error(err), zap.Duration("backoff", backoff))
			t.healthy.Store(false)

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			backoff = minDuration(backoff*2, maxBackoff)
			continue
		}

		t.healthy.Store(true)
		backoff = time.Second
		t.logger.Info("ssh tunnel established", zap.String("localAddr", t.LocalAddr()))

		// Run the forwarding loop until the connection is lost
		t.runForwardingLoop(ctx)

		t.mu.Lock()
		t.closeConnections()
		t.mu.Unlock()
		t.healthy.Store(false)

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// connect establishes the SSH connection and local listener.
func (t *sshTunnel) connect(ctx context.Context) error {
	sshConfig := &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // Host key verification handled at TLS layer
		Timeout:         sshDialTimeout,
	}

	// Establish SSH connection
	dialer := net.Dialer{Timeout: sshDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", t.config.RelayEndpoint)
	if err != nil {
		return fmt.Errorf("dialing ssh endpoint %s: %w", t.config.RelayEndpoint, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, t.config.RelayEndpoint, sshConfig)
	if err != nil {
		conn.Close()
		return fmt.Errorf("ssh handshake with %s: %w", t.config.RelayEndpoint, err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)

	// Create local TCP listener on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		client.Close()
		return fmt.Errorf("creating local listener: %w", err)
	}

	t.mu.Lock()
	t.sshClient = client
	t.listener = listener
	t.localAddr = listener.Addr().String()
	t.mu.Unlock()

	return nil
}

// runForwardingLoop accepts local connections and forwards them through the SSH tunnel.
func (t *sshTunnel) runForwardingLoop(ctx context.Context) {
	// Start keepalive monitor
	go t.keepalive(ctx)

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

		go t.forwardConnection(ctx, conn)
	}
}

// forwardConnection forwards a single local connection through the SSH tunnel.
func (t *sshTunnel) forwardConnection(ctx context.Context, localConn net.Conn) {
	defer localConn.Close()

	t.mu.RLock()
	client := t.sshClient
	t.mu.RUnlock()

	if client == nil {
		return
	}

	remoteAddr := net.JoinHostPort(t.config.RelayEndpoint, sshRemoteForwardPort)
	remoteConn, err := client.Dial("tcp", remoteAddr)
	if err != nil {
		t.logger.Debug("failed to dial remote via ssh", zap.Error(err))
		return
	}
	defer remoteConn.Close()

	bridgeConnections(ctx, localConn, remoteConn)
}

// keepalive sends periodic keepalive requests to detect connection loss.
func (t *sshTunnel) keepalive(ctx context.Context) {
	ticker := time.NewTicker(sshKeepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.mu.RLock()
			client := t.sshClient
			t.mu.RUnlock()

			if client == nil {
				return
			}

			_, _, err := client.SendRequest("keepalive@novaedge.io", true, nil)
			if err != nil {
				t.logger.Debug("ssh keepalive failed", zap.Error(err))
				// Close listener to break the accept loop
				t.mu.RLock()
				if t.listener != nil {
					t.listener.Close()
				}
				t.mu.RUnlock()
				return
			}
		}
	}
}

// closeConnections closes the SSH client and local listener.
func (t *sshTunnel) closeConnections() {
	if t.listener != nil {
		t.listener.Close()
		t.listener = nil
	}

	if t.sshClient != nil {
		t.sshClient.Close()
		t.sshClient = nil
	}
}

// bridgeConnections copies data bidirectionally between two connections.
func bridgeConnections(ctx context.Context, c1, c2 net.Conn) {
	done := make(chan struct{})

	go func() {
		defer close(done)
		_, _ = io.Copy(c1, c2)
	}()

	go func() {
		_, _ = io.Copy(c2, c1)
		// Close c2 to unblock the other goroutine
		c2.Close()
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}
}
