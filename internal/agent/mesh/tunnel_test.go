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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/http2"

	pb "github.com/azrtydxb/novaedge/internal/proto/gen"
)

// safeIntToInt32 converts int to int32 with bounds checking.
func safeIntToInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// tunnelTestPKI holds a self-signed CA and can issue client/server certificates.
type tunnelTestPKI struct {
	caCert    *x509.Certificate
	caKey     *ecdsa.PrivateKey
	caCertPEM []byte
	caPool    *x509.CertPool
}

// newTunnelTestPKI creates a self-signed CA for testing.
func newTunnelTestPKI(t *testing.T) *tunnelTestPKI {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Mesh CA"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create CA cert: %v", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("failed to parse CA cert: %v", err)
	}

	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})

	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	return &tunnelTestPKI{
		caCert:    caCert,
		caKey:     caKey,
		caCertPEM: caCertPEM,
		caPool:    caPool,
	}
}

// issueCert creates a certificate signed by the test CA with both server
// and client auth extended key usage. If spiffeID is non-empty, it is
// added as a URI SAN.
func (pki *tunnelTestPKI) issueCert(t *testing.T, cn string, spiffeID string) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	if spiffeID != "" {
		u, parseErr := url.Parse(spiffeID)
		if parseErr != nil {
			t.Fatalf("failed to parse SPIFFE ID URL: %v", parseErr)
		}
		template.URIs = []*url.URL{u}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, pki.caCert, &key.PublicKey, pki.caKey)
	if err != nil {
		t.Fatalf("failed to create cert: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal key: %v", err)
	}

	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatalf("failed to create TLS cert: %v", err)
	}

	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse leaf cert: %v", err)
	}
	cert.Leaf = leaf

	return cert
}

// startEchoServer starts a TCP server that echoes back anything it receives.
// Returns the listener address and a cleanup function.
func startEchoServer(t *testing.T) string {
	t.Helper()

	lc := net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start echo server: %v", err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	return ln.Addr().String()
}

// startTunnelServer starts a TunnelServer on a random port with the given
// TLS config and authorizer. Returns the listener address and cancellation.
func startTunnelServer(t *testing.T, serverTLS *tls.Config, authorizer *Authorizer) (string, context.CancelFunc) {
	t.Helper()

	logger := zap.NewNop()

	// Find a free port.
	lc := net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	_, portStr, _ := net.SplitHostPort(addr)
	var port int
	if _, scanErr := fmt.Sscanf(portStr, "%d", &port); scanErr != nil {
		t.Fatalf("failed to parse port: %v", scanErr)
	}

	if port < 0 || port > 65535 {
		t.Fatalf("port %d out of range", port)
	}

	ts := NewTunnelServer(logger, safeIntToInt32(port), serverTLS, authorizer, nil)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- ts.Start(ctx)
	}()

	// Wait for the server to be ready.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		dialer := &net.Dialer{Timeout: 100 * time.Millisecond}
		conn, dialErr := dialer.DialContext(context.Background(), "tcp", addr)
		if dialErr == nil {
			_ = conn.Close()
			return addr, cancel
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	t.Fatalf("tunnel server did not start in time")
	return "", nil
}

func TestTunnelServerHandleConnect(t *testing.T) {
	tc := newTunnelTestPKI(t)

	serverCert := tc.issueCert(t, "tunnel-server", "spiffe://cluster.local/ns/system/sa/novaedge-agent")
	clientCert := tc.issueCert(t, "tunnel-client", "spiffe://cluster.local/ns/app/sa/frontend")

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	echoAddr := startEchoServer(t)

	tunnelAddr, cancel := startTunnelServer(t, serverTLS, nil)
	defer cancel()

	// Use an io.Pipe for the request body so we can write to the
	// CONNECT stream after the handshake completes.
	pr, pw := io.Pipe()

	h2Transport := &http2.Transport{
		TLSClientConfig: clientTLS,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodConnect, "https://"+echoAddr, pr)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set(headerDestService, "echo.default")

	// Override dial to connect to the tunnel server, not the echo backend.
	h2Transport.DialTLSContext = func(dialCtx context.Context, network, _ string, cfg *tls.Config) (net.Conn, error) {
		d := &tls.Dialer{
			NetDialer: &net.Dialer{Timeout: 5 * time.Second},
			Config:    cfg,
		}
		return d.DialContext(dialCtx, network, tunnelAddr)
	}

	resp, err := h2Transport.RoundTrip(req)
	if err != nil {
		_ = pw.Close()
		t.Fatalf("CONNECT request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		_ = pw.Close()
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	// Write through the tunnel via the pipe writer and read from the response body.
	testData := []byte("hello mesh tunnel")
	_, err = pw.Write(testData)
	if err != nil {
		t.Fatalf("failed to write to tunnel: %v", err)
	}

	buf := make([]byte, len(testData))
	_, err = io.ReadFull(resp.Body, buf)
	if err != nil {
		t.Fatalf("failed to read from tunnel: %v", err)
	}

	if string(buf) != string(testData) {
		t.Errorf("expected %q, got %q", testData, buf)
	}

	_ = pw.Close()
	_ = resp.Body.Close()
}

func TestTunnelServerRejectsNonConnect(t *testing.T) {
	tc := newTunnelTestPKI(t)

	serverCert := tc.issueCert(t, "tunnel-server", "")
	clientCert := tc.issueCert(t, "tunnel-client", "")

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	tunnelAddr, cancel := startTunnelServer(t, serverTLS, nil)
	defer cancel()

	client := &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: clientTLS,
		},
	}

	getURL := "https://" + tunnelAddr + "/"
	if _, parseErr := url.ParseRequestURI(getURL); parseErr != nil {
		t.Fatalf("invalid URL: %v", parseErr)
	}
	getReq, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, getURL, nil)
	if reqErr != nil {
		t.Fatalf("failed to create GET request: %v", reqErr)
	}
	resp, err := client.Do(getReq) //nolint:gosec // G704: test server URL validated via url.ParseRequestURI above
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", resp.StatusCode)
	}
}

func TestTunnelServerAuthorizerDenies(t *testing.T) {
	tc := newTunnelTestPKI(t)

	serverCert := tc.issueCert(t, "tunnel-server", "")
	clientCert := tc.issueCert(t, "tunnel-client", "spiffe://cluster.local/ns/app/sa/frontend")

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	echoAddr := startEchoServer(t)

	// Create an authorizer that denies all traffic via a DENY policy.
	authorizer := NewAuthorizer(zap.NewNop())
	authorizer.UpdatePolicies([]*pb.MeshAuthorizationPolicy{
		{
			Name:            "deny-all",
			TargetService:   "echo",
			TargetNamespace: "default",
			Action:          "DENY",
			Rules: []*pb.MeshAuthorizationRule{
				{}, // empty from/to matches everything
			},
		},
	})

	tunnelAddr, cancel := startTunnelServer(t, serverTLS, authorizer)
	defer cancel()

	h2Transport := &http2.Transport{
		TLSClientConfig: clientTLS,
		DialTLSContext: func(dialCtx context.Context, network, _ string, cfg *tls.Config) (net.Conn, error) {
			d := &tls.Dialer{
				NetDialer: &net.Dialer{Timeout: 5 * time.Second},
				Config:    cfg,
			}
			return d.DialContext(dialCtx, network, tunnelAddr)
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodConnect, "https://"+echoAddr, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set(headerDestService, "echo.default")

	resp, err := h2Transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("CONNECT request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
	}
}

func TestTunnelPoolDialVia(t *testing.T) {
	tc := newTunnelTestPKI(t)

	serverCert := tc.issueCert(t, "tunnel-server", "spiffe://cluster.local/ns/system/sa/novaedge-agent")
	clientCert := tc.issueCert(t, "tunnel-client", "spiffe://cluster.local/ns/app/sa/frontend")

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	// Start an echo backend.
	echoAddr := startEchoServer(t)

	// Start tunnel server.
	tunnelAddr, cancel := startTunnelServer(t, serverTLS, nil)
	defer cancel()

	// Create tunnel pool and dial via the tunnel.
	pool := NewTunnelPool(zap.NewNop(), clientTLS)
	defer pool.Close()

	conn, err := pool.DialVia(
		context.Background(),
		tunnelAddr,
		echoAddr,
		"spiffe://cluster.local/ns/app/sa/frontend",
		"echo.default",
	)
	if err != nil {
		t.Fatalf("DialVia failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Write through the tunnel and verify echo.
	testData := []byte("pool dial via test")
	_, err = conn.Write(testData)
	if err != nil {
		t.Fatalf("failed to write to tunnel conn: %v", err)
	}

	buf := make([]byte, len(testData))
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("failed to read from tunnel conn: %v", err)
	}

	if string(buf) != string(testData) {
		t.Errorf("expected %q, got %q", testData, buf)
	}

	// Verify the conn implements net.Conn.
	if conn.LocalAddr() == nil {
		t.Error("expected non-nil LocalAddr")
	}
	if conn.RemoteAddr() == nil {
		t.Error("expected non-nil RemoteAddr")
	}
	if err := conn.SetDeadline(time.Now()); err != nil {
		t.Errorf("SetDeadline should return nil, got %v", err)
	}
}

func TestTunnelPoolReusesClient(t *testing.T) {
	pool := NewTunnelPool(zap.NewNop(), &tls.Config{MinVersion: tls.VersionTLS12})
	defer pool.Close()

	// Getting a client twice for the same address should return the same one.
	c1 := pool.getOrCreateClient("192.168.1.1:15002")
	c2 := pool.getOrCreateClient("192.168.1.1:15002")

	if c1 != c2 {
		t.Error("expected same client instance for same address")
	}

	// Different address should return different client.
	c3 := pool.getOrCreateClient("192.168.1.2:15002")
	if c1 == c3 {
		t.Error("expected different client for different address")
	}
}

func TestStreamConnInterface(t *testing.T) {
	// Verify streamConn implements net.Conn at compile time.
	var _ net.Conn = (*streamConn)(nil)

	// Create a pipe to provide reader and writer for the streamConn.
	pr, pw := io.Pipe()

	sc := &streamConn{
		reader:     pr,
		writer:     pw,
		localAddr:  &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
		remoteAddr: &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 80},
	}

	if sc.LocalAddr().String() != "127.0.0.1:1234" {
		t.Errorf("unexpected local addr: %s", sc.LocalAddr())
	}
	if sc.RemoteAddr().String() != "10.0.0.1:80" {
		t.Errorf("unexpected remote addr: %s", sc.RemoteAddr())
	}
	if err := sc.SetDeadline(time.Now()); err != nil {
		t.Errorf("SetDeadline error: %v", err)
	}
	if err := sc.SetReadDeadline(time.Now()); err != nil {
		t.Errorf("SetReadDeadline error: %v", err)
	}
	if err := sc.SetWriteDeadline(time.Now()); err != nil {
		t.Errorf("SetWriteDeadline error: %v", err)
	}

	// Close should close both reader and writer.
	if err := sc.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
}

func TestTunnelServerFederatedIdentityAllowed(t *testing.T) {
	tc := newTunnelTestPKI(t)

	serverCert := tc.issueCert(t, "tunnel-server", "spiffe://cluster.local/agent/server-node")
	// Client presents a federated SPIFFE ID from cluster-b.
	clientCert := tc.issueCert(t, "tunnel-client", "spiffe://my-federation/cluster/cluster-b/agent/node-2")

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	echoAddr := startEchoServer(t)

	// Create a TLS provider with federation config that allows cluster-b.
	fedCfg := &FederationConfig{
		FederationID:    "my-federation",
		ClusterName:     "cluster-a",
		AllowedClusters: []string{"cluster-b"},
	}
	tlsProvider := NewTLSProvider(zap.NewNop(), "cluster.local", fedCfg)

	tunnelAddr, cancel := startTunnelServerWithProvider(t, serverTLS, nil, tlsProvider)
	defer cancel()

	pr, pw := io.Pipe()
	h2Transport := &http2.Transport{
		TLSClientConfig: clientTLS,
		DialTLSContext: func(dialCtx context.Context, network, _ string, cfg *tls.Config) (net.Conn, error) {
			d := &tls.Dialer{
				NetDialer: &net.Dialer{Timeout: 5 * time.Second},
				Config:    cfg,
			}
			return d.DialContext(dialCtx, network, tunnelAddr)
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodConnect, "https://"+echoAddr, pr)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set(headerDestService, "echo.default")

	resp, err := h2Transport.RoundTrip(req)
	if err != nil {
		_ = pw.Close()
		t.Fatalf("CONNECT request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		_ = pw.Close()
		t.Fatalf("expected 200 OK for allowed federated identity, got %d", resp.StatusCode)
	}

	_ = pw.Close()
	_ = resp.Body.Close()
}

func TestTunnelServerFederatedIdentityDenied(t *testing.T) {
	tc := newTunnelTestPKI(t)

	serverCert := tc.issueCert(t, "tunnel-server", "spiffe://cluster.local/agent/server-node")
	// Client presents a federated SPIFFE ID from cluster-d (not allowed).
	clientCert := tc.issueCert(t, "tunnel-client", "spiffe://my-federation/cluster/cluster-d/agent/node-4")

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      tc.caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}

	echoAddr := startEchoServer(t)

	// Create a TLS provider with federation config that only allows cluster-b.
	fedCfg := &FederationConfig{
		FederationID:    "my-federation",
		ClusterName:     "cluster-a",
		AllowedClusters: []string{"cluster-b"},
	}
	tlsProvider := NewTLSProvider(zap.NewNop(), "cluster.local", fedCfg)

	tunnelAddr, cancel := startTunnelServerWithProvider(t, serverTLS, nil, tlsProvider)
	defer cancel()

	h2Transport := &http2.Transport{
		TLSClientConfig: clientTLS,
		DialTLSContext: func(dialCtx context.Context, network, _ string, cfg *tls.Config) (net.Conn, error) {
			d := &tls.Dialer{
				NetDialer: &net.Dialer{Timeout: 5 * time.Second},
				Config:    cfg,
			}
			return d.DialContext(dialCtx, network, tunnelAddr)
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodConnect, "https://"+echoAddr, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set(headerDestService, "echo.default")

	resp, err := h2Transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("CONNECT request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for disallowed federated identity, got %d", resp.StatusCode)
	}
}

// startTunnelServerWithProvider starts a TunnelServer with a specific TLSProvider
// for federation-aware identity validation.
func startTunnelServerWithProvider(t *testing.T, serverTLS *tls.Config, authorizer *Authorizer, tlsProvider *TLSProvider) (string, context.CancelFunc) {
	t.Helper()

	logger := zap.NewNop()

	lc := net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	_, portStr, _ := net.SplitHostPort(addr)
	var port int
	if _, scanErr := fmt.Sscanf(portStr, "%d", &port); scanErr != nil {
		t.Fatalf("failed to parse port: %v", scanErr)
	}

	if port < 0 || port > 65535 {
		t.Fatalf("port %d out of range", port)
	}

	ts := NewTunnelServer(logger, safeIntToInt32(port), serverTLS, authorizer, tlsProvider)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- ts.Start(ctx)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		dialer := &net.Dialer{Timeout: 100 * time.Millisecond}
		conn, dialErr := dialer.DialContext(context.Background(), "tcp", addr)
		if dialErr == nil {
			_ = conn.Close()
			return addr, cancel
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	t.Fatalf("tunnel server did not start in time")
	return "", nil
}

func TestParseTCPAddr(t *testing.T) {
	tests := []struct {
		input string
		ip    string
		port  int
	}{
		{"192.168.1.1:8080", "192.168.1.1", 8080},
		{"[::1]:443", "::1", 443},
		{"invalid", "", 0},
	}

	for _, tt := range tests {
		addr := parseTCPAddr(tt.input)
		if tt.ip != "" {
			if addr.IP.String() != tt.ip {
				t.Errorf("parseTCPAddr(%q) IP = %s, want %s", tt.input, addr.IP, tt.ip)
			}
		}
		if addr.Port != tt.port {
			t.Errorf("parseTCPAddr(%q) Port = %d, want %d", tt.input, addr.Port, tt.port)
		}
	}
}
