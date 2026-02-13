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
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// DefaultRedisConnectTimeout is the default timeout for connecting to Redis backends
	DefaultRedisConnectTimeout = 5 * time.Second
	// DefaultRedisIdleTimeout is the default idle timeout for Redis connections
	DefaultRedisIdleTimeout = 5 * time.Minute
	// DefaultRedisPoolSize is the default connection pool size per backend
	DefaultRedisPoolSize = 10
	// DefaultRedisReadTimeout is the default read timeout for Redis commands
	DefaultRedisReadTimeout = 30 * time.Second
	// DefaultRedisWriteTimeout is the default write timeout for Redis commands
	DefaultRedisWriteTimeout = 30 * time.Second
	// DefaultRedisDrainTimeout is the default drain timeout for graceful shutdown
	DefaultRedisDrainTimeout = 30 * time.Second
)

// RedisProxyConfig holds configuration for a Redis proxy instance
type RedisProxyConfig struct {
	// ListenerName identifies this listener for metrics and logging
	ListenerName string
	// ConnectTimeout is the timeout for connecting to a backend
	ConnectTimeout time.Duration
	// IdleTimeout is the idle timeout for pooled connections
	IdleTimeout time.Duration
	// ReadTimeout is the read timeout for client commands
	ReadTimeout time.Duration
	// WriteTimeout is the write timeout for sending responses
	WriteTimeout time.Duration
	// DrainTimeout is the timeout for draining connections on shutdown
	DrainTimeout time.Duration
	// PoolSize is the maximum number of pooled connections per backend
	PoolSize int
	// Backends is the list of backend Redis endpoints
	Backends []*pb.Endpoint
	// BackendName is the name of the backend cluster
	BackendName string
	// HealthCheckConfig optional health check configuration
	HealthCheckConfig *RedisHealthCheckerConfig
}

// redisBackendConn represents a pooled connection to a Redis backend
type redisBackendConn struct {
	conn     net.Conn
	addr     string
	lastUsed time.Time
}

// redisConnPool manages a pool of connections to a single Redis backend
type redisConnPool struct {
	addr     string
	mu       sync.Mutex
	conns    []*redisBackendConn
	maxSize  int
	timeout  time.Duration
	idleTime time.Duration
}

// RedisProxy handles protocol-aware Redis proxying between clients and backends
type RedisProxy struct {
	config   RedisProxyConfig
	logger   *zap.Logger
	mu       sync.RWMutex
	backends []*pb.Endpoint
	pools    map[string]*redisConnPool // key: "host:port"
	// roundRobinIdx for backend selection
	roundRobinIdx atomic.Uint64
	// activeConns tracks active client connections
	activeConns atomic.Int64
	// draining signals that the proxy is draining connections
	draining atomic.Bool
	// healthChecker performs Redis-specific health checks
	healthChecker *RedisHealthChecker
}

// NewRedisProxy creates a new Redis protocol-aware proxy
func NewRedisProxy(cfg RedisProxyConfig, logger *zap.Logger) *RedisProxy {
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = DefaultRedisConnectTimeout
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = DefaultRedisIdleTimeout
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = DefaultRedisReadTimeout
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = DefaultRedisWriteTimeout
	}
	if cfg.DrainTimeout == 0 {
		cfg.DrainTimeout = DefaultRedisDrainTimeout
	}
	if cfg.PoolSize == 0 {
		cfg.PoolSize = DefaultRedisPoolSize
	}

	proxy := &RedisProxy{
		config:   cfg,
		logger:   logger.With(zap.String("listener", cfg.ListenerName), zap.String("protocol", "redis")),
		backends: cfg.Backends,
		pools:    make(map[string]*redisConnPool),
	}

	// Initialize connection pools for each backend
	proxy.initPools()

	return proxy
}

// StartHealthChecker starts the Redis-specific health checker
func (p *RedisProxy) StartHealthChecker(ctx context.Context) {
	cfg := RedisHealthCheckerConfig{}
	if p.config.HealthCheckConfig != nil {
		cfg = *p.config.HealthCheckConfig
	}

	p.healthChecker = NewRedisHealthChecker(cfg, p.logger)
	p.healthChecker.UpdateBackends(p.backends)
	p.healthChecker.Start(ctx)
}

// StopHealthChecker stops the health checker
func (p *RedisProxy) StopHealthChecker() {
	if p.healthChecker != nil {
		p.healthChecker.Stop()
	}
}

// initPools initializes connection pools for all backends
func (p *RedisProxy) initPools() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, backend := range p.backends {
		addr := fmt.Sprintf("%s:%d", backend.Address, backend.Port)
		if _, exists := p.pools[addr]; !exists {
			p.pools[addr] = &redisConnPool{
				addr:     addr,
				conns:    make([]*redisBackendConn, 0, p.config.PoolSize),
				maxSize:  p.config.PoolSize,
				timeout:  p.config.ConnectTimeout,
				idleTime: p.config.IdleTimeout,
			}
		}
	}
}

// HandleConnection handles a single Redis client connection
func (p *RedisProxy) HandleConnection(ctx context.Context, clientConn net.Conn) {
	if p.draining.Load() {
		_ = clientConn.Close()
		return
	}

	p.activeConns.Add(1)
	defer p.activeConns.Add(-1)

	listenerName := p.config.ListenerName

	RedisConnectionsActive.WithLabelValues(listenerName).Inc()
	defer RedisConnectionsActive.WithLabelValues(listenerName).Dec()

	L4ActiveConnections.WithLabelValues("redis", listenerName).Inc()
	defer L4ActiveConnections.WithLabelValues("redis", listenerName).Dec()

	defer func() { _ = clientConn.Close() }()

	// Select a backend
	backend := p.pickBackend()
	if backend == nil {
		p.logger.Warn("No Redis backends available",
			zap.String("client", clientConn.RemoteAddr().String()))
		L4ConnectionErrors.WithLabelValues("redis", listenerName, "no_backend").Inc()
		// Send error response to client
		p.sendErrorToClient(clientConn, "ERR no backend available")
		return
	}

	backendAddr := fmt.Sprintf("%s:%d", backend.Address, backend.Port)

	L4ConnectionsTotal.WithLabelValues("redis", listenerName, p.config.BackendName).Inc()

	p.logger.Debug("Redis client connected",
		zap.String("client", clientConn.RemoteAddr().String()),
		zap.String("backend", backendAddr))

	// Get or create backend connection
	backendConn, err := p.getBackendConn(ctx, backendAddr)
	if err != nil {
		p.logger.Error("Failed to connect to Redis backend",
			zap.String("backend", backendAddr),
			zap.Error(err))
		L4ConnectionErrors.WithLabelValues("redis", listenerName, "connect_failed").Inc()
		p.sendErrorToClient(clientConn, "ERR backend connection failed")
		return
	}
	defer func() { _ = backendConn.Close() }()

	// Proxy commands between client and backend
	p.proxyCommands(ctx, clientConn, backendConn, backendAddr)
}

// proxyCommands reads commands from the client, forwards to backend, and returns responses
func (p *RedisProxy) proxyCommands(ctx context.Context, clientConn, backendConn net.Conn, backendAddr string) {
	listenerName := p.config.ListenerName
	backendName := p.config.BackendName
	reader := NewRESPReader(clientConn)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set read deadline on client connection
		if err := clientConn.SetReadDeadline(time.Now().Add(p.config.ReadTimeout)); err != nil {
			return
		}

		// Read a command from the client
		cmdParts, rawCmd, err := reader.ReadCommand()
		if err != nil {
			// Check if it is a normal connection close
			if isConnectionClosed(err) {
				p.logger.Debug("Redis client disconnected",
					zap.String("client", clientConn.RemoteAddr().String()))
				return
			}
			// Check for timeout (idle client)
			var netErr net.Error
			if isNetError(err, &netErr) && netErr.Timeout() {
				p.logger.Debug("Redis client idle timeout",
					zap.String("client", clientConn.RemoteAddr().String()))
				return
			}
			p.logger.Debug("Redis read error",
				zap.String("client", clientConn.RemoteAddr().String()),
				zap.Error(err))
			return
		}

		if len(cmdParts) == 0 {
			continue
		}

		cmdName := strings.ToUpper(cmdParts[0])
		startTime := time.Now()

		// Track command metrics
		RedisCommandsTotal.WithLabelValues(listenerName, cmdName).Inc()

		// Forward the raw command to the backend
		if err := backendConn.SetWriteDeadline(time.Now().Add(p.config.WriteTimeout)); err != nil {
			RedisCommandsTotal.WithLabelValues(listenerName, cmdName+"_error").Inc()
			return
		}

		if _, err := backendConn.Write(rawCmd); err != nil {
			p.logger.Error("Failed to forward command to Redis backend",
				zap.String("command", cmdName),
				zap.String("backend", backendAddr),
				zap.Error(err))
			L4ConnectionErrors.WithLabelValues("redis", listenerName, "forward_failed").Inc()
			p.sendErrorToClient(clientConn, "ERR backend write failed")
			return
		}

		L4BytesReceived.WithLabelValues("redis", listenerName, backendName).Add(float64(len(rawCmd)))

		// Read the response from backend
		if err := backendConn.SetReadDeadline(time.Now().Add(p.config.ReadTimeout)); err != nil {
			return
		}

		backendReader := NewRESPReader(backendConn)
		respVal, err := backendReader.ReadValue()
		if err != nil {
			p.logger.Error("Failed to read response from Redis backend",
				zap.String("command", cmdName),
				zap.String("backend", backendAddr),
				zap.Error(err))
			L4ConnectionErrors.WithLabelValues("redis", listenerName, "backend_read_failed").Inc()
			p.sendErrorToClient(clientConn, "ERR backend read failed")
			return
		}

		// Forward the raw response to client
		if err := clientConn.SetWriteDeadline(time.Now().Add(p.config.WriteTimeout)); err != nil {
			return
		}

		if _, err := clientConn.Write(respVal.RawData); err != nil {
			p.logger.Debug("Failed to write response to Redis client",
				zap.String("client", clientConn.RemoteAddr().String()),
				zap.Error(err))
			return
		}

		L4BytesSent.WithLabelValues("redis", listenerName, backendName).Add(float64(len(respVal.RawData)))

		duration := time.Since(startTime).Seconds()
		RedisCommandDuration.WithLabelValues(listenerName, cmdName).Observe(duration)

		// Handle QUIT command
		if cmdName == "QUIT" {
			return
		}
	}
}

// sendErrorToClient sends a RESP error to the client connection
func (p *RedisProxy) sendErrorToClient(conn net.Conn, msg string) {
	errResp := EncodeError(msg)
	_ = conn.SetWriteDeadline(time.Now().Add(p.config.WriteTimeout))
	_, _ = conn.Write(errResp)
}

// getBackendConn gets a connection from the pool or creates a new one
func (p *RedisProxy) getBackendConn(ctx context.Context, addr string) (net.Conn, error) {
	p.mu.RLock()
	pool, exists := p.pools[addr]
	p.mu.RUnlock()

	if exists {
		if conn := pool.get(); conn != nil {
			return conn, nil
		}
	}

	// Create new connection
	dialer := &net.Dialer{Timeout: p.config.ConnectTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect to redis backend %s: %w", addr, err)
	}

	return conn, nil
}

// get retrieves a connection from the pool
func (pool *redisConnPool) get() net.Conn {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	for len(pool.conns) > 0 {
		// Pop from the end (LIFO for better locality)
		last := len(pool.conns) - 1
		bc := pool.conns[last]
		pool.conns = pool.conns[:last]

		// Check if connection is still fresh
		if time.Since(bc.lastUsed) < pool.idleTime {
			return bc.conn
		}
		// Connection too old, close it
		_ = bc.conn.Close()
	}

	return nil
}

// put returns a connection to the pool. Returns false if pool is full.
//
//nolint:unparam // addr varies in production use; test callers happen to use a single address
func (pool *redisConnPool) put(conn net.Conn, addr string) bool {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	if len(pool.conns) >= pool.maxSize {
		return false
	}

	pool.conns = append(pool.conns, &redisBackendConn{
		conn:     conn,
		addr:     addr,
		lastUsed: time.Now(),
	})
	return true
}

// close closes all connections in the pool
func (pool *redisConnPool) close() {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	for _, bc := range pool.conns {
		_ = bc.conn.Close()
	}
	pool.conns = nil
}

// pickBackend selects a backend endpoint using round-robin
func (p *RedisProxy) pickBackend() *pb.Endpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()

	backends := getReadyEndpoints(p.backends)
	if len(backends) == 0 {
		return nil
	}

	idx := p.roundRobinIdx.Add(1) - 1
	return backends[idx%uint64(len(backends))]
}

// UpdateBackends updates the backend endpoint list
func (p *RedisProxy) UpdateBackends(backends []*pb.Endpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backends = backends

	// Initialize pools for new backends
	for _, backend := range backends {
		addr := fmt.Sprintf("%s:%d", backend.Address, backend.Port)
		if _, exists := p.pools[addr]; !exists {
			p.pools[addr] = &redisConnPool{
				addr:     addr,
				conns:    make([]*redisBackendConn, 0, p.config.PoolSize),
				maxSize:  p.config.PoolSize,
				timeout:  p.config.ConnectTimeout,
				idleTime: p.config.IdleTimeout,
			}
		}
	}

	// Update health checker if active
	if p.healthChecker != nil {
		p.healthChecker.UpdateBackends(backends)
	}
}

// Drain initiates graceful draining of existing connections
func (p *RedisProxy) Drain(timeout time.Duration) {
	p.draining.Store(true)

	deadline := time.After(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

drainLoop:
	for {
		if p.activeConns.Load() <= 0 {
			p.logger.Info("All Redis connections drained")
			break drainLoop
		}
		select {
		case <-deadline:
			p.logger.Warn("Redis drain timeout reached, some connections may be interrupted",
				zap.Duration("timeout", timeout))
			break drainLoop
		case <-ticker.C:
			continue
		}
	}

	// Close all connection pools
	p.mu.RLock()
	for _, pool := range p.pools {
		pool.close()
	}
	p.mu.RUnlock()
}

// IsDraining returns true if the proxy is draining connections
func (p *RedisProxy) IsDraining() bool {
	return p.draining.Load()
}

// ActiveConnections returns the number of active client connections
func (p *RedisProxy) ActiveConnections() int64 {
	return p.activeConns.Load()
}

// isConnectionClosed checks if the error indicates a closed connection
func isConnectionClosed(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "closed") ||
		strings.Contains(errStr, "reset by peer")
}
