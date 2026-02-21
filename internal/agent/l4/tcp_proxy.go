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

package l4

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// DefaultTCPConnectTimeout is the default timeout for connecting to backends
	DefaultTCPConnectTimeout = 5 * time.Second
	// DefaultTCPIdleTimeout is the default idle timeout for TCP connections
	DefaultTCPIdleTimeout = 5 * time.Minute
	// DefaultTCPBufferSize is the default buffer size for bidirectional copy
	DefaultTCPBufferSize = 32 * 1024
	// DefaultDrainTimeout is the default drain timeout for graceful shutdown
	DefaultDrainTimeout = 30 * time.Second
	// MaxTCPConnections is the maximum number of concurrent TCP connections allowed
	MaxTCPConnections int64 = 10000
)

// tcpBufferPool is a sync.Pool for TCP proxy buffers to reduce allocations.
// Buffers are sized at DefaultTCPBufferSize (32KB) for optimal network I/O.
var tcpBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, DefaultTCPBufferSize)
		return &buf
	},
}

// getTCPBuffer retrieves a buffer from the pool or creates a new one.
func getTCPBuffer() *[]byte {
	return tcpBufferPool.Get().(*[]byte)
}

// putTCPBuffer returns a buffer to the pool for reuse.
// The buffer is reset to full capacity before returning.
func putTCPBuffer(buf *[]byte) {
	if buf != nil {
		*buf = (*buf)[:cap(*buf)]
		tcpBufferPool.Put(buf)
	}
}

// TCPProxyConfig holds configuration for a TCP proxy instance
type TCPProxyConfig struct {
	// ListenerName identifies this listener for metrics and logging
	ListenerName string
	// ConnectTimeout is the timeout for connecting to a backend
	ConnectTimeout time.Duration
	// IdleTimeout is the idle timeout for connections
	IdleTimeout time.Duration
	// BufferSize is the size of the copy buffer
	BufferSize int
	// DrainTimeout is the timeout for draining existing connections on config change
	DrainTimeout time.Duration
	// Backends is the list of backend endpoints
	Backends []*pb.Endpoint
	// BackendName is the name of the backend cluster
	BackendName string
}

// TCPProxy handles TCP proxying between clients and backends
type TCPProxy struct {
	config   TCPProxyConfig
	logger   *zap.Logger
	mu       sync.RWMutex
	backends []*pb.Endpoint
	// roundRobinIdx for simple round-robin backend selection
	roundRobinIdx atomic.Uint64
	// activeConns tracks active connections for graceful shutdown
	activeConns atomic.Int64
	// draining signals that the proxy is draining connections
	draining atomic.Bool
}

// NewTCPProxy creates a new TCP proxy
func NewTCPProxy(cfg TCPProxyConfig, logger *zap.Logger) *TCPProxy {
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = DefaultTCPConnectTimeout
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = DefaultTCPIdleTimeout
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = DefaultTCPBufferSize
	}
	if cfg.DrainTimeout == 0 {
		cfg.DrainTimeout = DefaultDrainTimeout
	}

	return &TCPProxy{
		config:   cfg,
		logger:   logger.With(zap.String("listener", cfg.ListenerName)),
		backends: cfg.Backends,
	}
}

// HandleConnection handles a single TCP connection by proxying it to a backend
func (p *TCPProxy) HandleConnection(ctx context.Context, clientConn net.Conn) {
	if p.draining.Load() {
		_ = clientConn.Close()
		return
	}

	// Reject new connections if at the maximum connection limit
	if p.activeConns.Load() >= MaxTCPConnections {
		p.logger.Warn("TCP connection limit reached",
			zap.Int64("max", MaxTCPConnections),
			zap.String("client", clientConn.RemoteAddr().String()))
		L4ConnectionErrors.WithLabelValues("tcp", p.config.ListenerName, "connection_limit").Inc()
		_ = clientConn.Close()
		return
	}

	p.activeConns.Add(1)
	defer p.activeConns.Add(-1)

	startTime := time.Now()
	listenerName := p.config.ListenerName
	backendName := p.config.BackendName

	L4ActiveConnections.WithLabelValues("tcp", listenerName).Inc()
	defer L4ActiveConnections.WithLabelValues("tcp", listenerName).Dec()

	// Select a backend
	backend := p.pickBackend()
	if backend == nil {
		p.logger.Warn("No backends available",
			zap.String("client", clientConn.RemoteAddr().String()))
		L4ConnectionErrors.WithLabelValues("tcp", listenerName, "no_backend").Inc()
		_ = clientConn.Close()
		return
	}

	backendAddr := fmt.Sprintf("%s:%d", backend.Address, backend.Port)

	// Connect to backend
	dialer := &net.Dialer{Timeout: p.config.ConnectTimeout}
	backendConn, err := dialer.DialContext(ctx, "tcp", backendAddr)
	if err != nil {
		p.logger.Error("Failed to connect to backend",
			zap.String("backend", backendAddr),
			zap.Error(err))
		L4ConnectionErrors.WithLabelValues("tcp", listenerName, "connect_failed").Inc()
		_ = clientConn.Close()
		return
	}

	L4ConnectionsTotal.WithLabelValues("tcp", listenerName, backendName).Inc()

	p.logger.Debug("TCP connection established",
		zap.String("client", clientConn.RemoteAddr().String()),
		zap.String("backend", backendAddr))

	// Bidirectional copy
	bytesSent, bytesReceived := p.bidirectionalCopy(ctx, clientConn, backendConn)

	duration := time.Since(startTime).Seconds()
	L4ConnectionDuration.WithLabelValues("tcp", listenerName).Observe(duration)
	L4BytesSent.WithLabelValues("tcp", listenerName, backendName).Add(float64(bytesSent))
	L4BytesReceived.WithLabelValues("tcp", listenerName, backendName).Add(float64(bytesReceived))

	p.logger.Debug("TCP connection closed",
		zap.String("client", clientConn.RemoteAddr().String()),
		zap.String("backend", backendAddr),
		zap.Int64("bytes_sent", bytesSent),
		zap.Int64("bytes_received", bytesReceived),
		zap.Float64("duration_s", duration))
}

// bidirectionalCopy performs bidirectional data copy between client and backend.
// On Linux, it attempts to use splice() for zero-copy kernel-level transfer.
// Falls back to userspace copy if splice is unavailable.
// Returns (bytes sent to client, bytes received from client).
func (p *TCPProxy) bidirectionalCopy(ctx context.Context, clientConn, backendConn net.Conn) (int64, int64) {
	defer func() {
		_ = clientConn.Close()
		_ = backendConn.Close()
	}()

	// Create a child context for cancellation
	copyCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var bytesSent, bytesReceived atomic.Int64

	// Copy backend -> client (sent)
	go func() {
		defer cancel()
		n := p.copyOneDirection(copyCtx, clientConn, backendConn)
		bytesSent.Store(n)
	}()

	// Copy client -> backend (received)
	n := p.copyOneDirection(copyCtx, backendConn, clientConn)
	bytesReceived.Store(n)
	cancel()

	return bytesSent.Load(), bytesReceived.Load()
}

// copyOneDirection copies data from src to dst, trying splice first then falling back to userspace copy.
func (p *TCPProxy) copyOneDirection(ctx context.Context, dst, src net.Conn) int64 {
	// Try splice (zero-copy) first on Linux
	n, used, err := trySplice(dst, src)
	if used {
		if err != nil {
			p.logger.Debug("splice ended with error", zap.Error(err))
		}
		return n
	}

	// Fallback to userspace copy
	buf := getTCPBuffer()
	defer putTCPBuffer(buf)
	n, _ = p.copyWithIdleTimeout(ctx, dst, src, *buf, p.config.IdleTimeout)
	return n
}

// copyWithIdleTimeout copies data from src to dst with an idle timeout
func (p *TCPProxy) copyWithIdleTimeout(ctx context.Context, dst, src net.Conn, buf []byte, idleTimeout time.Duration) (int64, error) {
	var total int64
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		// Set read deadline for idle timeout
		if err := src.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return total, fmt.Errorf("set read deadline: %w", err)
		}

		nr, readErr := src.Read(buf)
		if nr > 0 {
			// Reset write deadline
			if err := dst.SetWriteDeadline(time.Now().Add(idleTimeout)); err != nil {
				return total, fmt.Errorf("set write deadline: %w", err)
			}

			nw, writeErr := dst.Write(buf[:nr])
			total += int64(nw)
			if writeErr != nil {
				return total, writeErr
			}
			if nw != nr {
				return total, io.ErrShortWrite
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			// Check for timeout (idle timeout reached)
			var netErr net.Error
			if errors.As(readErr, &netErr) && netErr.Timeout() {
				return total, nil
			}
			return total, readErr
		}
	}
}

// pickBackend selects a backend endpoint using round-robin
func (p *TCPProxy) pickBackend() *pb.Endpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()

	backends := p.getReadyBackends()
	if len(backends) == 0 {
		return nil
	}

	idx := p.roundRobinIdx.Add(1) - 1
	return backends[idx%uint64(len(backends))]
}

// getReadyBackends returns only ready backends
func (p *TCPProxy) getReadyBackends() []*pb.Endpoint {
	ready := make([]*pb.Endpoint, 0, len(p.backends))
	for _, b := range p.backends {
		if b.Ready {
			ready = append(ready, b)
		}
	}
	return ready
}

// UpdateBackends updates the backend endpoint list
func (p *TCPProxy) UpdateBackends(backends []*pb.Endpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backends = backends
}

// Drain initiates graceful draining of existing connections
func (p *TCPProxy) Drain(timeout time.Duration) {
	p.draining.Store(true)

	deadline := time.After(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if p.activeConns.Load() <= 0 {
			p.logger.Info("All TCP connections drained")
			return
		}
		select {
		case <-deadline:
			p.logger.Warn("TCP drain timeout reached, some connections may be interrupted",
				zap.Duration("timeout", timeout))
			return
		case <-ticker.C:
		}
	}
}

// IsDraining returns true if the proxy is draining connections
func (p *TCPProxy) IsDraining() bool {
	return p.draining.Load()
}
