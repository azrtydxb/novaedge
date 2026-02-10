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
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	// SSEContentType is the MIME type for Server-Sent Events
	SSEContentType = "text/event-stream"

	// SSEDefaultIdleTimeout is the default idle timeout for SSE connections (5 minutes)
	SSEDefaultIdleTimeout = 5 * time.Minute

	// SSEDefaultHeartbeatInterval is the default interval for keepalive comments (30 seconds)
	SSEDefaultHeartbeatInterval = 30 * time.Second

	// sseKeepaliveComment is the comment sent as heartbeat to keep SSE connections alive
	sseKeepaliveComment = ":keepalive\n\n"
)

// SSEConfig holds runtime SSE configuration
type SSEConfig struct {
	IdleTimeout       time.Duration
	HeartbeatInterval time.Duration
	MaxConnections    int32
}

// DefaultSSEConfig returns the default SSE configuration
func DefaultSSEConfig() *SSEConfig {
	return &SSEConfig{
		IdleTimeout:       SSEDefaultIdleTimeout,
		HeartbeatInterval: SSEDefaultHeartbeatInterval,
		MaxConnections:    0, // unlimited
	}
}

// SSEProxy handles Server-Sent Events proxying with heartbeat injection and connection tracking
type SSEProxy struct {
	logger            *zap.Logger
	config            *SSEConfig
	activeConnections atomic.Int64
	mu                sync.RWMutex
	draining          atomic.Bool
}

// NewSSEProxy creates a new SSE proxy with the given configuration
func NewSSEProxy(logger *zap.Logger, config *SSEConfig) *SSEProxy {
	if config == nil {
		config = DefaultSSEConfig()
	}
	return &SSEProxy{
		logger: logger.With(zap.String("component", "sse-proxy")),
		config: config,
	}
}

// IsSSERequest checks if an HTTP request is a Server-Sent Events request
func IsSSERequest(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return accept == SSEContentType
}

// ActiveConnections returns the current number of active SSE connections
func (p *SSEProxy) ActiveConnections() int64 {
	return p.activeConnections.Load()
}

// SetDraining marks the proxy as draining, preventing new SSE connections
func (p *SSEProxy) SetDraining(draining bool) {
	p.draining.Store(draining)
}

// IsDraining returns whether the proxy is in draining mode
func (p *SSEProxy) IsDraining() bool {
	return p.draining.Load()
}

// UpdateConfig updates the SSE proxy configuration at runtime
func (p *SSEProxy) UpdateConfig(config *SSEConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if config != nil {
		p.config = config
	}
}

// getConfig returns the current SSE configuration (thread-safe)
func (p *SSEProxy) getConfig() *SSEConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

// ProxySSE handles proxying an SSE connection from the client to a backend
func (p *SSEProxy) ProxySSE(w http.ResponseWriter, r *http.Request, backendURL string) error {
	cfg := p.getConfig()

	// Check if draining
	if p.draining.Load() {
		http.Error(w, "Server is draining, not accepting new SSE connections", http.StatusServiceUnavailable)
		return fmt.Errorf("SSE proxy is draining")
	}

	// Check max connections
	if cfg.MaxConnections > 0 && p.activeConnections.Load() >= int64(cfg.MaxConnections) {
		http.Error(w, "Maximum SSE connections reached", http.StatusServiceUnavailable)
		return fmt.Errorf("maximum SSE connections reached: %d", cfg.MaxConnections)
	}

	// Check that the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return fmt.Errorf("response writer does not support flushing")
	}

	// Track connection
	p.activeConnections.Add(1)
	defer p.activeConnections.Add(-1)

	p.logger.Info("SSE connection established",
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("backend", backendURL),
		zap.Int64("active_connections", p.activeConnections.Load()),
	)

	// Create context with SSE-specific timeout (longer than normal requests)
	ctx, cancel := context.WithTimeout(r.Context(), cfg.IdleTimeout)
	defer cancel()

	// Connect to backend
	backendReq, err := http.NewRequestWithContext(ctx, r.Method, backendURL+r.URL.RequestURI(), r.Body)
	if err != nil {
		return fmt.Errorf("failed to create backend request: %w", err)
	}

	// Copy headers from client request
	copySSEHeaders(r.Header, backendReq.Header)
	backendReq.Header.Set("Accept", SSEContentType)

	client := &http.Client{
		Timeout: 0, // No timeout for SSE streaming
	}
	backendResp, err := client.Do(backendReq)
	if err != nil {
		p.logger.Error("Failed to connect to SSE backend",
			zap.String("backend", backendURL),
			zap.Error(err),
		)
		http.Error(w, "Backend connection failed", http.StatusBadGateway)
		return fmt.Errorf("failed to connect to backend: %w", err)
	}
	defer func() { _ = backendResp.Body.Close() }()

	// Set SSE-specific response headers
	w.Header().Set("Content-Type", SSEContentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.WriteHeader(backendResp.StatusCode)
	flusher.Flush()

	// Start heartbeat goroutine
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()

	go p.sendHeartbeats(heartbeatCtx, w, flusher, cfg.HeartbeatInterval)

	// Stream data from backend to client
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("SSE connection closed (context done)",
				zap.String("remote_addr", r.RemoteAddr),
			)
			return nil
		default:
			n, readErr := backendResp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := w.Write(buf[:n]); writeErr != nil {
					p.logger.Debug("SSE client disconnected",
						zap.String("remote_addr", r.RemoteAddr),
						zap.Error(writeErr),
					)
					return nil
				}
				flusher.Flush()
			}
			if readErr != nil {
				if readErr == io.EOF {
					p.logger.Info("SSE backend closed connection",
						zap.String("backend", backendURL),
					)
					return nil
				}
				p.logger.Error("Error reading from SSE backend",
					zap.Error(readErr),
				)
				return readErr
			}
		}
	}
}

// sendHeartbeats periodically sends keepalive comments to maintain the SSE connection
func (p *SSEProxy) sendHeartbeats(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, sseKeepaliveComment); err != nil {
				p.logger.Debug("Failed to send SSE heartbeat (client likely disconnected)")
				return
			}
			flusher.Flush()
		}
	}
}

// copySSEHeaders copies relevant headers for SSE backend requests
func copySSEHeaders(src, dst http.Header) {
	headersToCopy := []string{
		"Authorization",
		"Cookie",
		"User-Agent",
		"X-Forwarded-For",
		"X-Real-IP",
		"X-Request-ID",
	}

	for _, header := range headersToCopy {
		if value := src.Get(header); value != "" {
			dst.Set(header, value)
		}
	}

	// Copy custom X- headers
	for key := range src {
		if len(key) > 2 && key[:2] == "X-" {
			if _, exists := dst[key]; !exists {
				dst.Set(key, src.Get(key))
			}
		}
	}
}
