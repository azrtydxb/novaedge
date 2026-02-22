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

// Package l4 provides L4 (TCP/UDP) load balancing, proxying, and TLS passthrough
// capabilities for the NovaEdge data plane agent.
package l4

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// ListenerType represents the type of L4 listener
type ListenerType string

const (
	// ListenerTypeTCP is a plain TCP listener
	ListenerTypeTCP ListenerType = "TCP"
	// ListenerTypeUDP is a plain UDP listener
	ListenerTypeUDP ListenerType = "UDP"
	// ListenerTypeTLSPassthrough is a TLS passthrough (non-terminating) listener
	ListenerTypeTLSPassthrough ListenerType = "TLS"
)

// ListenerConfig configures a single L4 listener.
type ListenerConfig struct {
	// Name identifies this listener
	Name string
	// Port is the port to listen on
	Port int32
	// Type is the listener type (TCP, UDP, TLS passthrough)
	Type ListenerType
	// BackendName is the name of the backend cluster
	BackendName string
	// Backends is the list of backend endpoints
	Backends []*pb.Endpoint
	// TCPConfig holds TCP-specific configuration
	TCPConfig *TCPProxyConfig
	// UDPConfig holds UDP-specific configuration
	UDPConfig *UDPProxyConfig
	// TLSConfig holds TLS passthrough-specific configuration
	TLSPassthroughConfig *TLSPassthroughConfig
}

// activeListener tracks a running L4 listener
type activeListener struct {
	config      ListenerConfig
	tcpListener net.Listener
	udpConn     *net.UDPConn
	tcpProxy    *TCPProxy
	udpProxy    *UDPProxy
	tlsProxy    *TLSPassthrough
	cancel      context.CancelFunc
}

// XDPFastPath is an optional interface for XDP-based L4 load balancing.
// When set on the Manager, eligible plain TCP/UDP listeners (not TLS
// passthrough) are offloaded to the XDP program instead of userspace proxy.
type XDPFastPath interface {
	// IsRunning returns whether the XDP fast path is active.
	IsRunning() bool
}

// Manager manages L4 TCP/UDP listeners
type Manager struct {
	logger    *zap.Logger
	mu        sync.Mutex
	listeners map[string]*activeListener // key: "name:port"
	// XDP is an optional XDP fast-path manager. When set and running,
	// eligible routes (plain TCP/UDP without TLS termination) are
	// offloaded to the kernel XDP program. The XDP manager's SyncBackends
	// is called by the agent main.go; this field is used only for
	// informational logging.
	XDP XDPFastPath
}

// NewManager creates a new L4 listener manager
func NewManager(logger *zap.Logger) *Manager {
	return &Manager{
		logger:    logger.With(zap.String("component", "l4-manager")),
		listeners: make(map[string]*activeListener),
	}
}

// ApplyConfig applies a new set of L4 listener configurations
// It starts new listeners, updates existing ones, and stops removed ones
func (m *Manager) ApplyConfig(ctx context.Context, configs []ListenerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Build map of desired listeners
	desired := make(map[string]ListenerConfig)
	for _, cfg := range configs {
		key := listenerKey(cfg.Name, cfg.Port)
		desired[key] = cfg
	}

	// Stop listeners that are no longer needed
	for key, listener := range m.listeners {
		if _, needed := desired[key]; !needed {
			m.logger.Info("Stopping L4 listener",
				zap.String("name", listener.config.Name),
				zap.Int32("port", listener.config.Port))
			m.stopListenerLocked(listener)
			delete(m.listeners, key)
		}
	}

	// Start or update listeners
	for key, cfg := range desired {
		existing, exists := m.listeners[key]
		if exists {
			// Update backends on existing listener
			m.updateListenerLocked(existing, cfg)
		} else {
			// Start new listener
			if err := m.startListenerLocked(ctx, cfg); err != nil {
				m.logger.Error("Failed to start L4 listener",
					zap.String("name", cfg.Name),
					zap.Int32("port", cfg.Port),
					zap.String("type", string(cfg.Type)),
					zap.Int("backend_count", len(cfg.Backends)),
					zap.Error(err))
				continue
			}
		}
	}

	xdpActive := m.XDP != nil && m.XDP.IsRunning()
	m.logger.Info("L4 configuration applied",
		zap.Int("active_listeners", len(m.listeners)),
		zap.Bool("xdp_fastpath", xdpActive))

	return nil
}

// startListenerLocked starts a new L4 listener (must be called with mu held)
func (m *Manager) startListenerLocked(parentCtx context.Context, cfg ListenerConfig) error {
	key := listenerKey(cfg.Name, cfg.Port)
	listenerCtx, cancel := context.WithCancel(parentCtx)

	active := &activeListener{
		config: cfg,
		cancel: cancel,
	}

	switch cfg.Type {
	case ListenerTypeTCP:
		if err := m.startTCPListener(listenerCtx, active); err != nil {
			cancel()
			return err
		}
	case ListenerTypeUDP:
		if err := m.startUDPListener(listenerCtx, active); err != nil {
			cancel()
			return err
		}
	case ListenerTypeTLSPassthrough:
		if err := m.startTLSPassthroughListener(listenerCtx, active); err != nil {
			cancel()
			return err
		}
	default:
		cancel()
		return fmt.Errorf("unknown listener type: %s", cfg.Type)
	}

	m.listeners[key] = active
	m.logger.Info("L4 listener started",
		zap.String("name", cfg.Name),
		zap.Int32("port", cfg.Port),
		zap.String("type", string(cfg.Type)))

	return nil
}

// startTCPListener starts a TCP listener
func (m *Manager) startTCPListener(ctx context.Context, active *activeListener) error {
	cfg := active.config
	addr := fmt.Sprintf(":%d", cfg.Port)

	lc := NewReusePortListenConfig()
	tcpListener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on TCP %s: %w", addr, err)
	}
	active.tcpListener = tcpListener

	proxyCfg := TCPProxyConfig{
		ListenerName: cfg.Name,
		Backends:     cfg.Backends,
		BackendName:  cfg.BackendName,
	}
	if cfg.TCPConfig != nil {
		proxyCfg.ConnectTimeout = cfg.TCPConfig.ConnectTimeout
		proxyCfg.IdleTimeout = cfg.TCPConfig.IdleTimeout
		proxyCfg.BufferSize = cfg.TCPConfig.BufferSize
		proxyCfg.DrainTimeout = cfg.TCPConfig.DrainTimeout
	}

	tcpProxy := NewTCPProxy(proxyCfg, m.logger)
	active.tcpProxy = tcpProxy

	go m.acceptTCPConnections(ctx, tcpListener, tcpProxy)

	return nil
}

// acceptTCPConnections accepts and handles TCP connections
func (m *Manager) acceptTCPConnections(ctx context.Context, listener net.Listener, proxy *TCPProxy) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				m.logger.Error("Failed to accept TCP connection", zap.Error(err))
				continue
			}
		}

		go proxy.HandleConnection(ctx, conn)
	}
}

// startUDPListener starts a UDP listener
func (m *Manager) startUDPListener(ctx context.Context, active *activeListener) error {
	cfg := active.config
	addr := fmt.Sprintf(":%d", cfg.Port)

	lc := NewReusePortListenConfig()
	pc, err := lc.ListenPacket(ctx, "udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP %s: %w", addr, err)
	}
	udpConn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return fmt.Errorf("expected *net.UDPConn, got %T", pc)
	}
	active.udpConn = udpConn

	proxyCfg := UDPProxyConfig{
		ListenerName: cfg.Name,
		Backends:     cfg.Backends,
		BackendName:  cfg.BackendName,
	}
	if cfg.UDPConfig != nil {
		proxyCfg.SessionTimeout = cfg.UDPConfig.SessionTimeout
		proxyCfg.BufferSize = cfg.UDPConfig.BufferSize
	}

	udpProxy := NewUDPProxy(proxyCfg, m.logger)
	active.udpProxy = udpProxy

	go m.readUDPPackets(ctx, udpConn, udpProxy)
	go m.cleanupUDPSessions(ctx, udpProxy)

	return nil
}

// readUDPPackets reads UDP packets and dispatches them to the proxy
func (m *Manager) readUDPPackets(ctx context.Context, conn *net.UDPConn, proxy *UDPProxy) {
	buf := make([]byte, proxy.config.BufferSize)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set read deadline to allow context cancellation
		if err := conn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
			m.logger.Error("Failed to set UDP read deadline", zap.Error(err))
			continue
		}

		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			var netErr net.Error
			if isNetError(err, &netErr) && netErr.Timeout() {
				continue
			}
			select {
			case <-ctx.Done():
				return
			default:
				m.logger.Error("Failed to read UDP packet", zap.Error(err))
				continue
			}
		}

		// Copy packet data so the buffer can be reused
		packetData := make([]byte, n)
		copy(packetData, buf[:n])

		go proxy.HandlePacket(ctx, conn, clientAddr, packetData)
	}
}

// cleanupUDPSessions periodically cleans up expired UDP sessions
func (m *Manager) cleanupUDPSessions(ctx context.Context, proxy *UDPProxy) {
	ticker := time.NewTicker(DefaultUDPSessionCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			proxy.CloseAllSessions()
			return
		case <-ticker.C:
			proxy.CleanupExpiredSessions()
		}
	}
}

// startTLSPassthroughListener starts a TLS passthrough listener
func (m *Manager) startTLSPassthroughListener(ctx context.Context, active *activeListener) error {
	cfg := active.config
	addr := fmt.Sprintf(":%d", cfg.Port)

	lc := NewReusePortListenConfig()
	tcpListener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on TCP %s for TLS passthrough: %w", addr, err)
	}
	active.tcpListener = tcpListener

	passthroughCfg := TLSPassthroughConfig{
		ListenerName: cfg.Name,
	}
	if cfg.TLSPassthroughConfig != nil {
		passthroughCfg.SNIReadTimeout = cfg.TLSPassthroughConfig.SNIReadTimeout
		passthroughCfg.ConnectTimeout = cfg.TLSPassthroughConfig.ConnectTimeout
		passthroughCfg.IdleTimeout = cfg.TLSPassthroughConfig.IdleTimeout
		passthroughCfg.BufferSize = cfg.TLSPassthroughConfig.BufferSize
		passthroughCfg.DrainTimeout = cfg.TLSPassthroughConfig.DrainTimeout
		passthroughCfg.Routes = cfg.TLSPassthroughConfig.Routes
		passthroughCfg.DefaultBackend = cfg.TLSPassthroughConfig.DefaultBackend
	}

	tlsProxy := NewTLSPassthrough(passthroughCfg, m.logger)
	active.tlsProxy = tlsProxy

	go m.acceptTLSPassthroughConnections(ctx, tcpListener, tlsProxy)

	return nil
}

// acceptTLSPassthroughConnections accepts TCP connections for TLS passthrough
func (m *Manager) acceptTLSPassthroughConnections(ctx context.Context, listener net.Listener, proxy *TLSPassthrough) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				m.logger.Error("Failed to accept TLS passthrough connection", zap.Error(err))
				continue
			}
		}

		go proxy.HandleConnection(ctx, conn)
	}
}

// updateListenerLocked updates an existing listener's backends (must be called with mu held)
func (m *Manager) updateListenerLocked(active *activeListener, cfg ListenerConfig) {
	active.config = cfg

	switch cfg.Type {
	case ListenerTypeTCP:
		if active.tcpProxy != nil {
			active.tcpProxy.UpdateBackends(cfg.Backends)
		}
	case ListenerTypeUDP:
		if active.udpProxy != nil {
			active.udpProxy.UpdateBackends(cfg.Backends)
		}
	case ListenerTypeTLSPassthrough:
		if active.tlsProxy != nil && cfg.TLSPassthroughConfig != nil {
			active.tlsProxy.UpdateRoutes(cfg.TLSPassthroughConfig.Routes, cfg.TLSPassthroughConfig.DefaultBackend)
		}
	}

	m.logger.Info("L4 listener backends updated",
		zap.String("name", cfg.Name),
		zap.Int32("port", cfg.Port))
}

// stopListenerLocked stops an active listener (must be called with mu held)
func (m *Manager) stopListenerLocked(active *activeListener) {
	// Cancel the listener context
	active.cancel()

	// Close the listener socket
	if active.tcpListener != nil {
		_ = active.tcpListener.Close()
	}
	if active.udpConn != nil {
		_ = active.udpConn.Close()
	}

	// Drain existing connections
	drainTimeout := DefaultDrainTimeout
	switch {
	case active.tcpProxy != nil:
		if active.config.TCPConfig != nil && active.config.TCPConfig.DrainTimeout > 0 {
			drainTimeout = active.config.TCPConfig.DrainTimeout
		}
		active.tcpProxy.Drain(drainTimeout)
	case active.udpProxy != nil:
		active.udpProxy.CloseAllSessions()
	case active.tlsProxy != nil:
		if active.config.TLSPassthroughConfig != nil && active.config.TLSPassthroughConfig.DrainTimeout > 0 {
			drainTimeout = active.config.TLSPassthroughConfig.DrainTimeout
		}
		active.tlsProxy.Drain(drainTimeout)
	}
}

// Shutdown stops all listeners and drains all connections
func (m *Manager) Shutdown(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Shutting down L4 manager",
		zap.Int("listeners", len(m.listeners)))

	for key, listener := range m.listeners {
		m.stopListenerLocked(listener)
		delete(m.listeners, key)
	}

	m.logger.Info("L4 manager shutdown complete")
	return nil
}

// GetActiveListeners returns the number of active listeners
func (m *Manager) GetActiveListeners() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.listeners)
}

// listenerKey generates a unique key for a listener
func listenerKey(name string, port int32) string {
	return fmt.Sprintf("%s:%d", name, port)
}
