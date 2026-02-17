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
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

const (
	// tunnelConnectTimeout is the timeout for dialing a backend pod.
	tunnelConnectTimeout = 5 * time.Second

	// headerSourceID is the header containing the source SPIFFE ID.
	headerSourceID = "X-NovaEdge-Source-ID"

	// headerDestService is the header containing the destination service name.
	headerDestService = "X-NovaEdge-Dest-Service"
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
	// We wrap the ResponseWriter in a flushWriter so that each write is
	// flushed immediately to the HTTP/2 stream, preventing buffering delays.
	done := make(chan struct{})
	go func() {
		defer func() { done <- struct{}{} }()
		if _, err := io.Copy(backendConn, r.Body); err != nil {
			ts.logger.Debug("io.Copy client->backend finished with error", zap.Error(err))
		}
	}()

	fw := &flushWriter{w: w}
	if _, err := io.Copy(fw, backendConn); err != nil {
		ts.logger.Debug("io.Copy backend->client finished with error", zap.Error(err))
	}
	<-done
}

// flushWriter wraps an http.ResponseWriter and flushes after every write.
// This ensures data is sent immediately over the HTTP/2 stream.
type flushWriter struct {
	w http.ResponseWriter
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if flusher, ok := fw.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}

// TunnelPool manages persistent HTTP/2 mTLS connections to peer agents.
// It maintains a pool of HTTP clients keyed by node address, reusing
// connections for multiple tunnel requests to the same peer.
type TunnelPool struct {
	mu        sync.RWMutex
	logger    *zap.Logger
	tlsConfig *tls.Config
	clients   map[string]*http.Client
}

// NewTunnelPool creates a new tunnel client pool for dialing peer agents.
func NewTunnelPool(logger *zap.Logger, tlsConfig *tls.Config) *TunnelPool {
	return &TunnelPool{
		logger:    logger.Named("tunnel-pool"),
		tlsConfig: tlsConfig,
		clients:   make(map[string]*http.Client),
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
		return nil, fmt.Errorf("CONNECT to %s via %s returned status %d", backendAddr, nodeAddr, resp.StatusCode)
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

// Close closes all persistent connections in the pool.
func (tp *TunnelPool) Close() {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	for addr, client := range tp.clients {
		client.CloseIdleConnections()
		delete(tp.clients, addr)
	}

	tp.logger.Info("Tunnel pool closed")
}

// getOrCreateClient returns an existing HTTP/2 client for the node address
// or creates a new one.
func (tp *TunnelPool) getOrCreateClient(nodeAddr string) *http.Client {
	tp.mu.RLock()
	client, ok := tp.clients[nodeAddr]
	tp.mu.RUnlock()
	if ok {
		return client
	}

	tp.mu.Lock()
	defer tp.mu.Unlock()

	// Double-check after acquiring write lock.
	if client, ok = tp.clients[nodeAddr]; ok {
		return client
	}

	client = &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: tp.tlsConfig,
		},
	}
	tp.clients[nodeAddr] = client

	tp.logger.Debug("Created HTTP/2 client for peer",
		zap.String("node_addr", nodeAddr),
	)

	return client
}

// streamConn wraps an HTTP/2 CONNECT stream as a net.Conn.
// Reads come from the response body and writes go to the request body
// pipe writer. This allows the tunnel stream to be used as a regular
// network connection by upstream code.
type streamConn struct {
	reader     io.ReadCloser  // response body (read from peer)
	writer     io.WriteCloser // pipe writer (write to peer via request body)
	localAddr  net.Addr
	remoteAddr net.Addr
}

// Read reads data from the tunnel stream (response body from server).
func (sc *streamConn) Read(p []byte) (int, error) {
	return sc.reader.Read(p)
}

// Write writes data to the tunnel stream (request body to server).
func (sc *streamConn) Write(p []byte) (int, error) {
	return sc.writer.Write(p)
}

// Close closes both the reader and writer sides of the tunnel stream.
func (sc *streamConn) Close() error {
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

// SetDeadline is a no-op for HTTP/2 streams.
func (sc *streamConn) SetDeadline(_ time.Time) error {
	return nil
}

// SetReadDeadline is a no-op for HTTP/2 streams.
func (sc *streamConn) SetReadDeadline(_ time.Time) error {
	return nil
}

// SetWriteDeadline is a no-op for HTTP/2 streams.
func (sc *streamConn) SetWriteDeadline(_ time.Time) error {
	return nil
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
