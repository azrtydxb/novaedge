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
	"hash/fnv"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// DefaultUDPSessionTimeout is the default timeout for UDP sessions
	DefaultUDPSessionTimeout = 30 * time.Second
	// DefaultUDPBufferSize is the default buffer size for UDP packets
	DefaultUDPBufferSize = 65535
	// DefaultUDPSessionCleanupInterval is the interval for cleaning up expired UDP sessions
	DefaultUDPSessionCleanupInterval = 10 * time.Second
)

// UDPProxyConfig holds configuration for a UDP proxy instance
type UDPProxyConfig struct {
	// ListenerName identifies this listener for metrics and logging
	ListenerName string
	// SessionTimeout is the idle timeout for UDP sessions
	SessionTimeout time.Duration
	// BufferSize is the maximum UDP packet size
	BufferSize int
	// Backends is the list of backend endpoints
	Backends []*pb.Endpoint
	// BackendName is the name of the backend cluster
	BackendName string
}

// udpSession represents a single UDP session keyed by source address
type udpSession struct {
	backendConn *net.UDPConn
	backend     *pb.Endpoint
	lastSeen    time.Time
	mu          sync.Mutex
}

// UDPProxy handles UDP proxying with session affinity based on source IP hash
type UDPProxy struct {
	config   UDPProxyConfig
	logger   *zap.Logger
	mu       sync.RWMutex
	backends []*pb.Endpoint
	sessions sync.Map // string (source addr) -> *udpSession
}

// NewUDPProxy creates a new UDP proxy
func NewUDPProxy(cfg UDPProxyConfig, logger *zap.Logger) *UDPProxy {
	if cfg.SessionTimeout == 0 {
		cfg.SessionTimeout = DefaultUDPSessionTimeout
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = DefaultUDPBufferSize
	}

	return &UDPProxy{
		config:   cfg,
		logger:   logger.With(zap.String("listener", cfg.ListenerName)),
		backends: cfg.Backends,
	}
}

// HandlePacket handles a single UDP packet by routing it to the appropriate backend
func (p *UDPProxy) HandlePacket(ctx context.Context, listenerConn *net.UDPConn, clientAddr *net.UDPAddr, data []byte) {
	listenerName := p.config.ListenerName
	backendName := p.config.BackendName
	sourceKey := clientAddr.String()

	// Look up existing session
	existing, loaded := p.sessions.Load(sourceKey)
	if loaded {
		session, ok := existing.(*udpSession)
		if !ok {
			p.logger.Error("Invalid session type in sessions map", zap.String("source", sourceKey))
			L4ConnectionErrors.WithLabelValues("udp", listenerName, "session_type_error").Inc()
			return
		}
		session.mu.Lock()
		session.lastSeen = time.Now()
		session.mu.Unlock()
		p.forwardUDPPacket(ctx, listenerConn, clientAddr, session, data)
		return
	}

	// Create new session
	backend := p.pickBackendBySourceIP(clientAddr.IP.String())
	if backend == nil {
		p.logger.Warn("No backends available for UDP",
			zap.String("source", sourceKey))
		L4ConnectionErrors.WithLabelValues("udp", listenerName, "no_backend").Inc()
		return
	}

	backendAddr := fmt.Sprintf("%s:%d", backend.Address, backend.Port)
	remoteAddr, err := net.ResolveUDPAddr("udp", backendAddr)
	if err != nil {
		p.logger.Error("Failed to resolve backend address",
			zap.String("backend", backendAddr),
			zap.Error(err))
		L4ConnectionErrors.WithLabelValues("udp", listenerName, "resolve_failed").Inc()
		return
	}

	backendConn, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		p.logger.Error("Failed to connect to UDP backend",
			zap.String("backend", backendAddr),
			zap.Error(err))
		L4ConnectionErrors.WithLabelValues("udp", listenerName, "connect_failed").Inc()
		return
	}

	session := &udpSession{
		backendConn: backendConn,
		backend:     backend,
		lastSeen:    time.Now(),
	}

	// Store session (check for race with another goroutine)
	actual, loaded := p.sessions.LoadOrStore(sourceKey, session)
	if loaded {
		// Another goroutine created the session first; close our connection
		_ = backendConn.Close()
		existingSession, ok := actual.(*udpSession)
		if !ok {
			p.logger.Error("Invalid session type in sessions map", zap.String("source", sourceKey))
			L4ConnectionErrors.WithLabelValues("udp", listenerName, "session_type_error").Inc()
			return
		}
		existingSession.mu.Lock()
		existingSession.lastSeen = time.Now()
		existingSession.mu.Unlock()
		p.forwardUDPPacket(ctx, listenerConn, clientAddr, existingSession, data)
		return
	}

	L4UDPSessionsTotal.WithLabelValues(listenerName, backendName).Inc()
	L4UDPActiveSessions.WithLabelValues(listenerName).Inc()

	p.logger.Debug("New UDP session",
		zap.String("source", sourceKey),
		zap.String("backend", backendAddr))

	// Start response reader goroutine
	go p.readBackendResponses(ctx, listenerConn, clientAddr, session, sourceKey)

	// Forward the initial packet
	p.forwardUDPPacket(ctx, listenerConn, clientAddr, session, data)
}

// forwardUDPPacket forwards a UDP packet to the backend
func (p *UDPProxy) forwardUDPPacket(_ context.Context, _ *net.UDPConn, _ *net.UDPAddr, session *udpSession, data []byte) {
	listenerName := p.config.ListenerName
	backendName := p.config.BackendName

	session.mu.Lock()
	defer session.mu.Unlock()

	_, err := session.backendConn.Write(data)
	if err != nil {
		p.logger.Error("Failed to forward UDP packet",
			zap.Error(err))
		L4ConnectionErrors.WithLabelValues("udp", listenerName, "forward_failed").Inc()
		return
	}

	L4BytesReceived.WithLabelValues("udp", listenerName, backendName).Add(float64(len(data)))
}

// readBackendResponses reads responses from the backend and sends them back to the client
func (p *UDPProxy) readBackendResponses(ctx context.Context, listenerConn *net.UDPConn, clientAddr *net.UDPAddr, session *udpSession, sourceKey string) {
	listenerName := p.config.ListenerName
	backendName := p.config.BackendName
	buf := make([]byte, p.config.BufferSize)

	defer func() {
		_ = session.backendConn.Close()
		p.sessions.Delete(sourceKey)
		L4UDPActiveSessions.WithLabelValues(listenerName).Dec()
		p.logger.Debug("UDP session ended", zap.String("source", sourceKey))
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set read deadline for session timeout
		if err := session.backendConn.SetReadDeadline(time.Now().Add(p.config.SessionTimeout)); err != nil {
			return
		}

		n, err := session.backendConn.Read(buf)
		if err != nil {
			// Timeout means session expired
			var netErr net.Error
			if ok := isNetError(err, &netErr); ok && netErr.Timeout() {
				return
			}
			return
		}

		// Send response back to client
		_, err = listenerConn.WriteToUDP(buf[:n], clientAddr)
		if err != nil {
			p.logger.Error("Failed to send UDP response to client",
				zap.String("client", clientAddr.String()),
				zap.Error(err))
			L4ConnectionErrors.WithLabelValues("udp", listenerName, "response_failed").Inc()
			return
		}

		L4BytesSent.WithLabelValues("udp", listenerName, backendName).Add(float64(n))

		session.mu.Lock()
		session.lastSeen = time.Now()
		session.mu.Unlock()
	}
}

// isNetError checks if the error is a net.Error and extracts it
func isNetError(err error, target *net.Error) bool {
	netErr, ok := err.(net.Error) //nolint:errorlint // net.Error is an interface, type assertion is appropriate
	if ok {
		*target = netErr
	}
	return ok
}

// pickBackendBySourceIP selects a backend using source IP hash for session affinity
func (p *UDPProxy) pickBackendBySourceIP(sourceIP string) *pb.Endpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()

	backends := p.getReadyBackends()
	if len(backends) == 0 {
		return nil
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(sourceIP))
	idx := h.Sum32() % uint32(len(backends))
	return backends[idx]
}

// getReadyBackends returns only ready backends
func (p *UDPProxy) getReadyBackends() []*pb.Endpoint {
	ready := make([]*pb.Endpoint, 0, len(p.backends))
	for _, b := range p.backends {
		if b.Ready {
			ready = append(ready, b)
		}
	}
	return ready
}

// UpdateBackends updates the backend endpoint list
func (p *UDPProxy) UpdateBackends(backends []*pb.Endpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backends = backends
}

// CleanupExpiredSessions removes expired UDP sessions
func (p *UDPProxy) CleanupExpiredSessions() {
	now := time.Now()
	p.sessions.Range(func(key, value interface{}) bool {
		session, ok := value.(*udpSession)
		if !ok {
			p.sessions.Delete(key)
			return true
		}
		session.mu.Lock()
		expired := now.Sub(session.lastSeen) > p.config.SessionTimeout
		session.mu.Unlock()

		if expired {
			if session.backendConn != nil {
				_ = session.backendConn.Close()
			}
			p.sessions.Delete(key)
			L4UDPActiveSessions.WithLabelValues(p.config.ListenerName).Dec()
			p.logger.Debug("Cleaned up expired UDP session",
				zap.String("source", key.(string))) //nolint:forcetypeassert // key is always string from sessions map
		}
		return true
	})
}

// CloseAllSessions closes all active UDP sessions
func (p *UDPProxy) CloseAllSessions() {
	p.sessions.Range(func(key, value interface{}) bool {
		session, ok := value.(*udpSession)
		if ok && session.backendConn != nil {
			_ = session.backendConn.Close()
		}
		p.sessions.Delete(key)
		return true
	})
}
