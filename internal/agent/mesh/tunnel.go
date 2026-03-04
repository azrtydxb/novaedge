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

package mesh

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

var (
	errWriterClosed = errors.New("writer closed")
	errCONNECTTo    = errors.New("CONNECT to")
)

const (
	// tunnelConnectTimeout is the timeout for dialing a backend pod.
	tunnelConnectTimeout = 5 * time.Second

	// headerSourceID is the header containing the source SPIFFE ID.
	headerSourceID = "X-NovaEdge-Source-ID"

	// headerDestService is the header containing the destination service name.
	headerDestService = "X-NovaEdge-Dest-Service"

	// tunnelPoolCleanupInterval is how often the tunnel pool checks for idle clients.
	tunnelPoolCleanupInterval = 5 * time.Minute

	// tunnelPoolClientTTL is the maximum idle time before a pooled client is evicted.
	tunnelPoolClientTTL = 10 * time.Minute

	// tunnelWriteBufSize is the buffer size for the buffered tunnel writer.
	tunnelWriteBufSize = 32 * 1024

	// tunnelFlushInterval is the periodic flush interval for buffered tunnel writes.
	tunnelFlushInterval = 1 * time.Millisecond
)

// TunnelServer handles incoming HTTP/2 CONNECT requests from peer agents.
// It listens for mTLS connections and proxies traffic to local backend pods
// on behalf of the requesting peer.
type TunnelServer struct {
	logger      *zap.Logger
	port        int32
	tlsConfig   *tls.Config
	authorizer  *Authorizer
	tlsProvider *TLSProvider
	server      *http.Server
}

// NewTunnelServer creates a new tunnel server that listens on the given port
// for HTTP/2 CONNECT requests from peer agents. The tlsConfig must be configured
// for mTLS (client certificate required). The authorizer may be nil to skip
// authorization checks. The tlsProvider is used for cross-cluster SPIFFE ID
// validation and may be nil when federation is not needed.
func NewTunnelServer(logger *zap.Logger, port int32, tlsConfig *tls.Config, authorizer *Authorizer, tlsProvider *TLSProvider) *TunnelServer {
	return &TunnelServer{
		logger:      logger.Named("tunnel-server"),
		port:        port,
		tlsConfig:   tlsConfig,
		authorizer:  authorizer,
		tlsProvider: tlsProvider,
	}
}

// Start begins listening for tunnel connections. It blocks until ctx is
// cancelled or an unrecoverable error occurs.
func (ts *TunnelServer) Start(ctx context.Context) error {
	// Use the TunnelServer directly as the handler instead of a ServeMux.
	// ServeMux can redirect CONNECT requests, which we do not want.
	ts.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", ts.port),
		Handler:           ts,
		TLSConfig:         ts.tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Configure HTTP/2 on the server.
	if err := http2.ConfigureServer(ts.server, &http2.Server{}); err != nil {
		return fmt.Errorf("failed to configure HTTP/2: %w", err)
	}

	// Create listener using context-aware ListenConfig.
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", ts.server.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", ts.server.Addr, err)
	}

	tlsLn := tls.NewListener(ln, ts.tlsConfig)

	ts.logger.Info("Tunnel server starting",
		zap.Int32("port", ts.port),
	)

	// Graceful shutdown on context cancellation.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := ts.server.Shutdown(shutdownCtx); shutdownErr != nil {
			ts.logger.Error("Tunnel server shutdown error", zap.Error(shutdownErr))
		}
	}()

	if err := ts.server.Serve(tlsLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("tunnel server stopped: %w", err)
	}

	return nil
}

// ServeHTTP implements the http.Handler interface, dispatching to handleConnect.
func (ts *TunnelServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ts.handleConnect(w, r)
}

// handleConnect processes an HTTP CONNECT request from a peer agent.
// It extracts the destination from the request, optionally checks
// authorization, dials the backend pod, and starts bidirectional copy.
//
// For HTTP/2, CONNECT requests are handled as streams rather than hijacked
// connections. The request body provides the read side (client -> server)
// and the ResponseWriter provides the write side (server -> client).
func (ts *TunnelServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	// Only accept CONNECT method.
	if r.Method != http.MethodConnect {
		http.Error(w, "Method not allowed, use CONNECT", http.StatusMethodNotAllowed)
		return
	}

	// For HTTP/2 CONNECT, the target is in r.Host (the :authority pseudo-header).
	backendAddr := r.Host

	// Extract peer identity from mTLS certificate.
	sourceID := PeerSPIFFEID(r.TLS)
	destService := r.Header.Get(headerDestService)

	ts.logger.Debug("CONNECT request received",
		zap.String("source_id", sourceID),
		zap.String("dest_service", destService),
		zap.String("backend", backendAddr),
	)

	// Validate the peer's SPIFFE ID when a TLS provider is available.
	// This enforces that cross-cluster identities are only accepted when
	// federation is active and the originating cluster is in the allow list.
	if ts.tlsProvider != nil && sourceID != "" {
		if !ts.tlsProvider.ValidatePeerSPIFFEID(sourceID) {
			ts.logger.Warn("CONNECT request denied: invalid SPIFFE identity",
				zap.String("source_id", sourceID),
				zap.String("dest_service", destService),
				zap.String("backend", backendAddr),
			)
			http.Error(w, "Forbidden: invalid peer identity", http.StatusForbidden)
			return
		}
	}

	// Check authorization if an authorizer is configured.
	if ts.authorizer != nil {
		source := ParseSPIFFEID(sourceID)
		if !ts.authorizer.Authorize(source, destService, r.Method, r.URL.Path) {
			ts.logger.Warn("CONNECT request denied by policy",
				zap.String("source_id", sourceID),
				zap.String("dest_service", destService),
				zap.String("backend", backendAddr),
			)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	// Dial the backend pod.
	dialer := &net.Dialer{Timeout: tunnelConnectTimeout}
	backendConn, err := dialer.DialContext(r.Context(), "tcp", backendAddr)
	if err != nil {
		ts.logger.Error("Failed to dial backend",
			zap.String("backend", backendAddr),
			zap.Error(err),
		)
		http.Error(w, fmt.Sprintf("Failed to connect to backend: %v", err), http.StatusBadGateway)
		return
	}
	defer func() { _ = backendConn.Close() }()

	// Send 200 OK to signal the tunnel is established.
	w.WriteHeader(http.StatusOK)

	// Flush the 200 status to the client immediately.
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Bidirectional copy between the HTTP/2 stream and the backend connection.
	// The request body is the read side (client -> proxy -> backend).
	// The ResponseWriter is the write side (backend -> proxy -> client).
	//
	// We wrap the ResponseWriter in a bufferedFlushWriter that batches
	// writes into a 32KB buffer with periodic 1ms flush to reduce
	// per-write Flush() system call overhead.
	done := make(chan struct{})
	go func() {
		defer func() { done <- struct{}{} }()
		if _, err := io.Copy(backendConn, r.Body); err != nil {
			ts.logger.Debug("io.Copy client->backend finished with error", zap.Error(err))
		}
	}()

	bfw := newBufferedFlushWriter(w)
	defer func() { _ = bfw.Close() }()

	// Use a wrapper that calls BufferedWrite so io.Copy goes through the buffer.
	bfwWriter := &bufferedWriteAdapter{bfw: bfw}
	if _, err := io.Copy(bfwWriter, backendConn); err != nil {
		ts.logger.Debug("io.Copy backend->client finished with error", zap.Error(err))
	}
	<-done
}

// bufferedFlushWriter wraps an http.ResponseWriter with a bufio.Writer to
// batch small writes and reduce per-write Flush() overhead. A periodic timer
// ensures data is sent promptly even when the buffer is not full.
type bufferedFlushWriter struct {
	w      http.ResponseWriter
	buf    *bufio.Writer
	mu     sync.Mutex
	timer  *time.Timer
	closed bool
}

// newBufferedFlushWriter creates a buffered writer that accumulates writes
// into a 32KB buffer and flushes periodically (every 1ms) or when full.
func newBufferedFlushWriter(w http.ResponseWriter) *bufferedFlushWriter {
	bfw := &bufferedFlushWriter{w: w}
	bfw.buf = bufio.NewWriterSize(bfw, tunnelWriteBufSize)
	return bfw
}

// Write implements io.Writer. Called by bufio.Writer when flushing the buffer
// to the underlying http.ResponseWriter.
func (bfw *bufferedFlushWriter) Write(p []byte) (int, error) {
	n, err := bfw.w.Write(p)
	if flusher, ok := bfw.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}

// BufferedWrite writes data into the buffer and starts a periodic flush timer
// if one is not already running.
func (bfw *bufferedFlushWriter) BufferedWrite(p []byte) (int, error) {
	bfw.mu.Lock()
	defer bfw.mu.Unlock()

	if bfw.closed {
		return 0, errWriterClosed
	}

	n, err := bfw.buf.Write(p)

	// Start a flush timer if not already running.
	if bfw.timer == nil {
		bfw.timer = time.AfterFunc(tunnelFlushInterval, func() {
			bfw.mu.Lock()
			defer bfw.mu.Unlock()
			if !bfw.closed {
				_ = bfw.buf.Flush()
			}
			bfw.timer = nil
		})
	}

	return n, err
}

// Close flushes any remaining buffered data and stops the flush timer.
func (bfw *bufferedFlushWriter) Close() error {
	bfw.mu.Lock()
	defer bfw.mu.Unlock()

	if bfw.closed {
		return nil
	}
	bfw.closed = true

	if bfw.timer != nil {
		bfw.timer.Stop()
		bfw.timer = nil
	}

	return bfw.buf.Flush()
}

// bufferedWriteAdapter adapts a bufferedFlushWriter to an io.Writer for use
// with io.Copy, routing writes through the buffered path.
type bufferedWriteAdapter struct {
	bfw *bufferedFlushWriter
}

func (a *bufferedWriteAdapter) Write(p []byte) (int, error) {
	return a.bfw.BufferedWrite(p)
}

// pooledClient wraps an HTTP client with a last-used timestamp for idle eviction.
type pooledClient struct {
	client   *http.Client
	lastUsed time.Time
}

// TunnelPool manages persistent HTTP/2 mTLS connections to peer agents.
// It maintains a pool of HTTP clients keyed by node address, reusing
// connections for multiple tunnel requests to the same peer. Idle clients
// are periodically evicted to prevent unbounded memory growth.
type TunnelPool struct {
	mu        sync.RWMutex
	logger    *zap.Logger
	tlsConfig *tls.Config
	clients   map[string]*pooledClient
	stopOnce  sync.Once
	stopCh    chan struct{}
}

// NewTunnelPool creates a new tunnel client pool for dialing peer agents.
// It starts a background goroutine that periodically evicts idle clients.
func NewTunnelPool(logger *zap.Logger, tlsConfig *tls.Config) *TunnelPool {
	tp := &TunnelPool{
		logger:    logger.Named("tunnel-pool"),
		tlsConfig: tlsConfig,
		clients:   make(map[string]*pooledClient),
		stopCh:    make(chan struct{}),
	}
	go tp.cleanupLoop()
	return tp
}

// cleanupLoop periodically evicts idle clients from the pool.
func (tp *TunnelPool) cleanupLoop() {
	ticker := time.NewTicker(tunnelPoolCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tp.stopCh:
			return
		case <-ticker.C:
			tp.evictIdle()
		}
	}
}

// evictIdle removes clients that have not been used within the TTL.
func (tp *TunnelPool) evictIdle() {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	cutoff := time.Now().Add(-tunnelPoolClientTTL)
	for addr, pc := range tp.clients {
		if pc.lastUsed.Before(cutoff) {
			pc.client.CloseIdleConnections()
			delete(tp.clients, addr)
			tp.logger.Debug("Evicted idle tunnel client",
				zap.String("node_addr", addr),
				zap.Duration("idle_time", time.Since(pc.lastUsed)),
			)
		}
	}
}

// DialVia opens an HTTP/2 CONNECT tunnel through a peer agent to reach a
// backend pod. nodeAddr is the peer agent's tunnel address (e.g.,
// "192.168.100.21:15002"). backendAddr is the final destination (e.g.,
// "10.42.3.15:80"). sourceID is the source SPIFFE ID. destService is
// "name.namespace" for authorization.
//
// The returned net.Conn wraps the HTTP/2 stream: writes go to the request
// body (via an io.Pipe), reads come from the response body.
func (tp *TunnelPool) DialVia(ctx context.Context, nodeAddr, backendAddr, sourceID, destService string) (net.Conn, error) {
	// Create an io.Pipe for the request body. This allows us to write
	// to the CONNECT stream after the handshake is complete.
	pr, pw := io.Pipe()

	// Build the CONNECT request. For HTTP/2, the CONNECT method uses
	// the :authority pseudo-header as the target, which maps to req.Host.
	reqURL := fmt.Sprintf("https://%s", backendAddr)
	if _, parseErr := url.ParseRequestURI(reqURL); parseErr != nil {
		_ = pw.Close()
		_ = pr.Close()
		return nil, fmt.Errorf("invalid backend address %q: %w", backendAddr, parseErr)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodConnect, reqURL, pr)
	if err != nil {
		_ = pw.Close()
		_ = pr.Close()
		return nil, fmt.Errorf("failed to create CONNECT request: %w", err)
	}

	req.Header.Set(headerSourceID, sourceID)
	req.Header.Set(headerDestService, destService)

	// Create an HTTP/2 transport that dials the tunnel server (nodeAddr)
	// regardless of the CONNECT target address.
	transport := &http2.Transport{
		TLSClientConfig: tp.tlsConfig,
		DialTLSContext: func(dialCtx context.Context, network, _ string, cfg *tls.Config) (net.Conn, error) {
			dialer := &tls.Dialer{
				NetDialer: &net.Dialer{Timeout: tunnelConnectTimeout},
				Config:    cfg,
			}
			return dialer.DialContext(dialCtx, network, nodeAddr)
		},
	}

	connectClient := &http.Client{Transport: transport}

	resp, err := connectClient.Do(req)
	if err != nil {
		_ = pw.Close()
		return nil, fmt.Errorf("CONNECT to %s via %s failed: %w", backendAddr, nodeAddr, err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		_ = pw.Close()
		return nil, fmt.Errorf("%w: %s via %s returned status %d", errCONNECTTo, backendAddr, nodeAddr, resp.StatusCode)
	}

	localAddr := parseTCPAddr(nodeAddr)
	remoteAddr := parseTCPAddr(backendAddr)

	return &streamConn{
		reader:     resp.Body,
		writer:     pw,
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
	}, nil
}

// Close stops the cleanup goroutine and closes all persistent connections in the pool.
func (tp *TunnelPool) Close() {
	tp.stopOnce.Do(func() {
		close(tp.stopCh)
	})

	tp.mu.Lock()
	defer tp.mu.Unlock()

	for addr, pc := range tp.clients {
		pc.client.CloseIdleConnections()
		delete(tp.clients, addr)
	}

	tp.logger.Info("Tunnel pool closed")
}

// getOrCreateClient returns an existing HTTP/2 client for the node address
// or creates a new one. It updates the lastUsed timestamp on every access.
func (tp *TunnelPool) getOrCreateClient(nodeAddr string) *http.Client {
	tp.mu.RLock()
	pc, ok := tp.clients[nodeAddr]
	tp.mu.RUnlock()
	if ok {
		// Update lastUsed under write lock.
		tp.mu.Lock()
		pc.lastUsed = time.Now()
		tp.mu.Unlock()
		return pc.client
	}

	tp.mu.Lock()
	defer tp.mu.Unlock()

	// Double-check after acquiring write lock.
	if pc, ok = tp.clients[nodeAddr]; ok {
		pc.lastUsed = time.Now()
		return pc.client
	}

	client := &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: tp.tlsConfig,
		},
	}
	tp.clients[nodeAddr] = &pooledClient{
		client:   client,
		lastUsed: time.Now(),
	}

	tp.logger.Debug("Created HTTP/2 client for peer",
		zap.String("node_addr", nodeAddr),
	)

	return client
}

// streamConn wraps an HTTP/2 CONNECT stream as a net.Conn.
// Reads come from the response body and writes go to the request body
// pipe writer. This allows the tunnel stream to be used as a regular
// network connection by upstream code.
//
// Since HTTP/2 streams do not natively support deadlines, we emulate them
// using time.AfterFunc timers that cancel the stream's context, causing
// in-progress reads and writes to fail.
type streamConn struct {
	reader     io.ReadCloser  // response body (read from peer)
	writer     io.WriteCloser // pipe writer (write to peer via request body)
	localAddr  net.Addr
	remoteAddr net.Addr

	mu            sync.Mutex
	closed        bool        // set when Close() or deadline fires
	readTimer     *time.Timer // pending read deadline timer
	writeTimer    *time.Timer // pending write deadline timer
	deadlineTimer *time.Timer // pending combined deadline timer
}

// Read reads data from the tunnel stream (response body from server).
func (sc *streamConn) Read(p []byte) (int, error) {
	return sc.reader.Read(p)
}

// Write writes data to the tunnel stream (request body to server).
func (sc *streamConn) Write(p []byte) (int, error) {
	return sc.writer.Write(p)
}

// Close closes both the reader and writer sides of the tunnel stream
// and cancels any pending deadline timers.
func (sc *streamConn) Close() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.stopTimerLocked(&sc.readTimer)
	sc.stopTimerLocked(&sc.writeTimer)
	sc.stopTimerLocked(&sc.deadlineTimer)
	return sc.closeLocked()
}

// closeLocked closes the underlying reader and writer to interrupt any blocked
// I/O. Must be called with sc.mu held. Safe to call multiple times.
func (sc *streamConn) closeLocked() error {
	if sc.closed {
		return nil
	}
	sc.closed = true
	rErr := sc.reader.Close()
	wErr := sc.writer.Close()
	if rErr != nil {
		return rErr
	}
	return wErr
}

// LocalAddr returns the local network address placeholder.
func (sc *streamConn) LocalAddr() net.Addr {
	return sc.localAddr
}

// RemoteAddr returns the remote network address placeholder.
func (sc *streamConn) RemoteAddr() net.Addr {
	return sc.remoteAddr
}

// SetDeadline sets both read and write deadlines. If the deadline is in the
// past or zero, any pending timer is cancelled. Otherwise, a timer is started
// that will cancel the stream when the deadline expires.
func (sc *streamConn) SetDeadline(t time.Time) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.setTimerLocked(&sc.deadlineTimer, t)
	return nil
}

// SetReadDeadline sets the read deadline for the stream.
func (sc *streamConn) SetReadDeadline(t time.Time) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.setTimerLocked(&sc.readTimer, t)
	return nil
}

// SetWriteDeadline sets the write deadline for the stream.
func (sc *streamConn) SetWriteDeadline(t time.Time) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.setTimerLocked(&sc.writeTimer, t)
	return nil
}

// setTimerLocked sets or resets a deadline timer. Must be called with sc.mu held.
func (sc *streamConn) setTimerLocked(timer **time.Timer, t time.Time) {
	// Cancel any existing timer.
	sc.stopTimerLocked(timer)

	if t.IsZero() {
		// Zero time means no deadline.
		return
	}

	d := time.Until(t)
	if d <= 0 {
		// Deadline already passed — close I/O immediately.
		_ = sc.closeLocked()
		return
	}

	*timer = time.AfterFunc(d, func() {
		sc.mu.Lock()
		defer sc.mu.Unlock()
		_ = sc.closeLocked()
	})
}

// stopTimerLocked stops and nils a timer. Must be called with sc.mu held.
func (sc *streamConn) stopTimerLocked(timer **time.Timer) {
	if *timer != nil {
		(*timer).Stop()
		*timer = nil
	}
}

// parseTCPAddr attempts to parse an address string into a *net.TCPAddr.
// Returns a zero-value TCPAddr on failure.
func parseTCPAddr(addr string) *net.TCPAddr {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return &net.TCPAddr{}
	}

	ip := net.ParseIP(host)
	portNum := 0
	if _, scanErr := fmt.Sscanf(port, "%d", &portNum); scanErr != nil {
		return &net.TCPAddr{IP: ip}
	}

	return &net.TCPAddr{IP: ip, Port: portNum}
}
