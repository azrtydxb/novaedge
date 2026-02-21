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
	// DefaultSNIReadTimeout is the timeout for reading the TLS ClientHello
	DefaultSNIReadTimeout = 5 * time.Second
	// MaxTLSRecordSize is the maximum size of a TLS record header + payload we buffer
	MaxTLSRecordSize = 16384 + 5
	// TLSRecordHeaderLen is the length of a TLS record header
	TLSRecordHeaderLen = 5
	// TLSHandshakeType is the TLS content type for handshakes
	TLSHandshakeType = 0x16
	// TLSClientHelloType is the handshake type for ClientHello
	TLSClientHelloType = 0x01
)

// TLSPassthroughConfig holds configuration for TLS passthrough
type TLSPassthroughConfig struct {
	// ListenerName identifies this listener for metrics and logging
	ListenerName string
	// SNIReadTimeout is the timeout for reading the TLS ClientHello to extract SNI
	SNIReadTimeout time.Duration
	// ConnectTimeout is the timeout for connecting to backends
	ConnectTimeout time.Duration
	// IdleTimeout is the idle timeout for connections
	IdleTimeout time.Duration
	// BufferSize is the buffer size for bidirectional copy
	BufferSize int
	// DrainTimeout is the timeout for draining connections during config change
	DrainTimeout time.Duration
	// Routes maps SNI hostnames to backend endpoints
	Routes map[string]*TLSRoute
	// DefaultBackend is used when no SNI match is found (optional)
	DefaultBackend *TLSRoute
}

// TLSRoute maps an SNI hostname to backend endpoints
type TLSRoute struct {
	// Hostname is the SNI hostname for this route
	Hostname string
	// Backends is the list of backend endpoints
	Backends []*pb.Endpoint
	// BackendName is the name of the backend cluster
	BackendName string
}

// TLSPassthrough handles TLS passthrough (non-terminating) proxying with SNI-based routing
type TLSPassthrough struct {
	config   TLSPassthroughConfig
	logger   *zap.Logger
	mu       sync.RWMutex
	routes   map[string]*TLSRoute
	defRoute *TLSRoute
	// roundRobinIdx for simple round-robin backend selection per route
	roundRobinCounters sync.Map // string (hostname) -> *atomic.Uint64
	// activeConns tracks active connections for graceful shutdown
	activeConns atomic.Int64
	// draining signals that the proxy is draining connections
	draining atomic.Bool
}

// NewTLSPassthrough creates a new TLS passthrough proxy
func NewTLSPassthrough(cfg TLSPassthroughConfig, logger *zap.Logger) *TLSPassthrough {
	if cfg.SNIReadTimeout == 0 {
		cfg.SNIReadTimeout = DefaultSNIReadTimeout
	}
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

	return &TLSPassthrough{
		config:   cfg,
		logger:   logger.With(zap.String("listener", cfg.ListenerName)),
		routes:   cfg.Routes,
		defRoute: cfg.DefaultBackend,
	}
}

// HandleConnection handles a TLS connection by extracting SNI and routing without decryption
func (p *TLSPassthrough) HandleConnection(ctx context.Context, clientConn net.Conn) {
	if p.draining.Load() {
		_ = clientConn.Close()
		return
	}

	p.activeConns.Add(1)
	defer p.activeConns.Add(-1)

	startTime := time.Now()
	listenerName := p.config.ListenerName

	L4ActiveConnections.WithLabelValues("tls", listenerName).Inc()
	defer L4ActiveConnections.WithLabelValues("tls", listenerName).Dec()

	// Set deadline for SNI extraction
	if err := clientConn.SetReadDeadline(time.Now().Add(p.config.SNIReadTimeout)); err != nil {
		p.logger.Error("Failed to set read deadline", zap.Error(err))
		L4ConnectionErrors.WithLabelValues("tls", listenerName, "deadline_error").Inc()
		_ = clientConn.Close()
		return
	}

	// Read TLS ClientHello to extract SNI
	sni, bufferedData, err := extractSNI(clientConn)
	if err != nil {
		p.logger.Warn("Failed to extract SNI",
			zap.String("client", clientConn.RemoteAddr().String()),
			zap.Error(err))
		L4SNIRoutingErrors.WithLabelValues(listenerName, "sni_extraction_failed").Inc()
		_ = clientConn.Close()
		return
	}

	// Clear read deadline
	if err := clientConn.SetReadDeadline(time.Time{}); err != nil {
		p.logger.Error("Failed to clear read deadline", zap.Error(err))
		_ = clientConn.Close()
		return
	}

	// Look up route for this SNI
	route := p.lookupRoute(sni)
	if route == nil {
		p.logger.Warn("No route found for SNI",
			zap.String("sni", sni),
			zap.String("client", clientConn.RemoteAddr().String()))
		L4SNIRoutingErrors.WithLabelValues(listenerName, "no_route").Inc()
		_ = clientConn.Close()
		return
	}

	L4TLSPassthroughTotal.WithLabelValues(listenerName, sni).Inc()

	// Select a backend
	backend := p.pickBackend(route)
	if backend == nil {
		p.logger.Warn("No backends available for SNI",
			zap.String("sni", sni))
		L4ConnectionErrors.WithLabelValues("tls", listenerName, "no_backend").Inc()
		_ = clientConn.Close()
		return
	}

	backendAddr := fmt.Sprintf("%s:%d", backend.Address, backend.Port)

	// Connect to backend
	dialer := &net.Dialer{Timeout: p.config.ConnectTimeout}
	backendConn, err := dialer.DialContext(ctx, "tcp", backendAddr)
	if err != nil {
		p.logger.Error("Failed to connect to TLS backend",
			zap.String("sni", sni),
			zap.String("backend", backendAddr),
			zap.Error(err))
		L4ConnectionErrors.WithLabelValues("tls", listenerName, "connect_failed").Inc()
		_ = clientConn.Close()
		return
	}

	L4ConnectionsTotal.WithLabelValues("tls", listenerName, route.BackendName).Inc()

	p.logger.Debug("TLS passthrough connection established",
		zap.String("sni", sni),
		zap.String("client", clientConn.RemoteAddr().String()),
		zap.String("backend", backendAddr))

	// Forward the buffered ClientHello data first
	if _, err := backendConn.Write(bufferedData); err != nil {
		p.logger.Error("Failed to forward ClientHello to backend",
			zap.Error(err))
		_ = clientConn.Close()
		_ = backendConn.Close()
		return
	}

	// Bidirectional copy for the rest of the connection
	bytesSent, bytesReceived := p.bidirectionalCopy(ctx, clientConn, backendConn)

	duration := time.Since(startTime).Seconds()
	L4ConnectionDuration.WithLabelValues("tls", listenerName).Observe(duration)
	L4BytesSent.WithLabelValues("tls", listenerName, route.BackendName).Add(float64(bytesSent))
	L4BytesReceived.WithLabelValues("tls", listenerName, route.BackendName).Add(float64(bytesReceived))

	p.logger.Debug("TLS passthrough connection closed",
		zap.String("sni", sni),
		zap.String("client", clientConn.RemoteAddr().String()),
		zap.String("backend", backendAddr),
		zap.Float64("duration_s", duration))
}

// lookupRoute finds a TLS route for the given SNI hostname
func (p *TLSPassthrough) lookupRoute(sni string) *TLSRoute {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Exact match first
	if route, ok := p.routes[sni]; ok {
		return route
	}

	// Wildcard match: try *.domain.com for sub.domain.com
	for i := 0; i < len(sni); i++ {
		if sni[i] == '.' {
			wildcard := "*" + sni[i:]
			if route, ok := p.routes[wildcard]; ok {
				return route
			}
		}
	}

	// Default backend
	return p.defRoute
}

// pickBackend selects a backend endpoint using round-robin per route
func (p *TLSPassthrough) pickBackend(route *TLSRoute) *pb.Endpoint {
	backends := getReadyEndpoints(route.Backends)
	if len(backends) == 0 {
		return nil
	}

	counterVal, _ := p.roundRobinCounters.LoadOrStore(route.Hostname, &atomic.Uint64{})
	counter, ok := counterVal.(*atomic.Uint64)
	if !ok {
		// Should never happen, but handle gracefully
		return backends[0]
	}
	idx := counter.Add(1) - 1
	return backends[idx%uint64(len(backends))]
}

// bidirectionalCopy performs bidirectional data copy between client and backend.
// On Linux, it attempts to use splice() for zero-copy transfer.
// Falls back to userspace copy if splice is unavailable.
func (p *TLSPassthrough) bidirectionalCopy(ctx context.Context, clientConn, backendConn net.Conn) (int64, int64) {
	defer func() {
		_ = clientConn.Close()
		_ = backendConn.Close()
	}()

	copyCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var bytesSent, bytesReceived atomic.Int64

	// Copy backend -> client
	go func() {
		defer cancel()
		// Try splice (zero-copy) first on Linux
		n, used, _ := trySplice(clientConn, backendConn)
		if !used {
			buf := getTCPBuffer()
			defer putTCPBuffer(buf)
			n, _ = copyWithTimeout(copyCtx, clientConn, backendConn, *buf, p.config.IdleTimeout)
		}
		bytesSent.Store(n)
	}()

	// Copy client -> backend
	n, used, _ := trySplice(backendConn, clientConn)
	if !used {
		buf := getTCPBuffer()
		defer putTCPBuffer(buf)
		n, _ = copyWithTimeout(copyCtx, backendConn, clientConn, *buf, p.config.IdleTimeout)
	}
	bytesReceived.Store(n)
	cancel()

	return bytesSent.Load(), bytesReceived.Load()
}

// UpdateRoutes updates the SNI routing table
func (p *TLSPassthrough) UpdateRoutes(routes map[string]*TLSRoute, defaultBackend *TLSRoute) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.routes = routes
	p.defRoute = defaultBackend
}

// Drain initiates graceful draining of existing connections
func (p *TLSPassthrough) Drain(timeout time.Duration) {
	p.draining.Store(true)

	deadline := time.After(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if p.activeConns.Load() <= 0 {
			p.logger.Info("All TLS passthrough connections drained")
			return
		}
		select {
		case <-deadline:
			p.logger.Warn("TLS passthrough drain timeout reached",
				zap.Duration("timeout", timeout))
			return
		case <-ticker.C:
		}
	}
}

// IsDraining returns true if the proxy is draining connections
func (p *TLSPassthrough) IsDraining() bool {
	return p.draining.Load()
}

// extractSNI reads the TLS ClientHello message to extract the SNI hostname
// Returns the SNI hostname and all data read (which must be forwarded to the backend)
func extractSNI(conn net.Conn) (string, []byte, error) {
	// Read TLS record header (5 bytes)
	header := make([]byte, TLSRecordHeaderLen)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", nil, fmt.Errorf("failed to read TLS record header: %w", err)
	}

	// Verify this is a TLS handshake record
	if len(header) < TLSRecordHeaderLen {
		return "", header, fmt.Errorf("TLS record header too short: %d bytes", len(header))
	}
	if header[0] != TLSHandshakeType {
		return "", header, fmt.Errorf("not a TLS handshake record: content type %d", header[0])
	}

	// Get record length
	recordLen := int(header[3])<<8 | int(header[4])
	if recordLen > MaxTLSRecordSize {
		return "", header, fmt.Errorf("TLS record too large: %d bytes", recordLen)
	}

	// Read the full record
	record := make([]byte, recordLen)
	if _, err := io.ReadFull(conn, record); err != nil {
		return "", append(header, record...), fmt.Errorf("failed to read TLS record: %w", err)
	}

	bufferedData := make([]byte, 0, len(header)+len(record))
	bufferedData = append(bufferedData, header...)
	bufferedData = append(bufferedData, record...)

	// Parse ClientHello to find SNI
	sni, err := parseClientHelloSNI(record)
	if err != nil {
		return "", bufferedData, fmt.Errorf("failed to parse ClientHello: %w", err)
	}

	return sni, bufferedData, nil
}

// parseClientHelloSNI parses a TLS ClientHello handshake message to extract the SNI extension
func parseClientHelloSNI(data []byte) (string, error) {
	if len(data) < 4 {
		return "", errors.New("handshake message too short")
	}

	// Check handshake type
	if data[0] != TLSClientHelloType {
		return "", fmt.Errorf("not a ClientHello: type %d", data[0])
	}

	// Skip handshake header (1 byte type + 3 bytes length)
	pos := 4

	// Skip client version (2 bytes) and random (32 bytes)
	if pos+34 > len(data) {
		return "", errors.New("ClientHello too short: missing version/random")
	}
	pos += 34

	// Skip session ID
	if pos+1 > len(data) {
		return "", errors.New("ClientHello too short: missing session ID length")
	}
	sessionIDLen := int(data[pos])
	pos++
	pos += sessionIDLen

	// Skip cipher suites
	if pos+2 > len(data) {
		return "", errors.New("ClientHello too short: missing cipher suites")
	}
	cipherSuitesLen := int(data[pos])<<8 | int(data[pos+1])
	pos += 2
	pos += cipherSuitesLen

	// Skip compression methods
	if pos+1 > len(data) {
		return "", errors.New("ClientHello too short: missing compression methods")
	}
	compressionLen := int(data[pos])
	pos++
	pos += compressionLen

	// Extensions
	if pos+2 > len(data) {
		return "", errors.New("no extensions present")
	}
	extensionsLen := int(data[pos])<<8 | int(data[pos+1])
	pos += 2

	end := pos + extensionsLen
	if end > len(data) {
		end = len(data)
	}

	// Iterate through extensions to find SNI (type 0x0000)
	for pos+4 <= end {
		extType := int(data[pos])<<8 | int(data[pos+1])
		extLen := int(data[pos+2])<<8 | int(data[pos+3])
		pos += 4

		if extType == 0 { // SNI extension
			return parseSNIExtension(data[pos : pos+extLen])
		}

		pos += extLen
	}

	return "", errors.New("SNI extension not found")
}

// parseSNIExtension parses the SNI extension data to extract the hostname
func parseSNIExtension(data []byte) (string, error) {
	if len(data) < 2 {
		return "", errors.New("SNI extension too short")
	}

	// Server name list length
	listLen := int(data[0])<<8 | int(data[1])
	if listLen+2 > len(data) {
		return "", errors.New("SNI list length exceeds data")
	}

	pos := 2
	end := pos + listLen
	for pos+3 <= end && pos+3 <= len(data) {
		nameType := data[pos] //nolint:gosec // pos+3 <= len(data) guarantees pos is in bounds
		nameLen := int(data[pos+1])<<8 | int(data[pos+2])
		pos += 3

		if nameType == 0 { // Host name type
			if pos+nameLen > end {
				return "", errors.New("SNI hostname length exceeds data")
			}
			return string(data[pos : pos+nameLen]), nil
		}

		pos += nameLen
	}

	return "", errors.New("no hostname found in SNI extension")
}

// getReadyEndpoints filters endpoints to only ready ones
func getReadyEndpoints(endpoints []*pb.Endpoint) []*pb.Endpoint {
	ready := make([]*pb.Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		if ep.Ready {
			ready = append(ready, ep)
		}
	}
	return ready
}

// copyWithTimeout copies data with idle timeout (shared utility)
func copyWithTimeout(ctx context.Context, dst, src net.Conn, buf []byte, idleTimeout time.Duration) (int64, error) {
	var total int64
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		if err := src.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return total, fmt.Errorf("set read deadline: %w", err)
		}

		nr, readErr := src.Read(buf)
		if nr > 0 {
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
			var netErr net.Error
			if errors.As(readErr, &netErr) && netErr.Timeout() {
				return total, nil
			}
			return total, readErr
		}
	}
}
