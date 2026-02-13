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

package server

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"
)

// mockConn implements net.Conn for testing
type mockConn struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	addr     net.Addr
}

func newMockConn(data []byte, remoteAddr net.Addr) *mockConn {
	return &mockConn{
		readBuf:  bytes.NewBuffer(data),
		writeBuf: &bytes.Buffer{},
		addr:     remoteAddr,
	}
}

func (c *mockConn) Read(b []byte) (int, error)  { return c.readBuf.Read(b) }
func (c *mockConn) Write(b []byte) (int, error) { return c.writeBuf.Write(b) }
func (c *mockConn) Close() error                { return nil }
func (c *mockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}
func (c *mockConn) RemoteAddr() net.Addr               { return c.addr }
func (c *mockConn) SetDeadline(_ time.Time) error      { return nil }
func (c *mockConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *mockConn) SetWriteDeadline(_ time.Time) error { return nil }

func TestParseProxyProtocolV1_TCP4(t *testing.T) {
	header := "PROXY TCP4 192.168.1.100 10.0.0.1 12345 80\r\nGET / HTTP/1.1\r\n"
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("10.0.0.2"), Port: 9999}
	conn := newMockConn([]byte(header), remoteAddr)

	logger := zap.NewNop()
	ppConn := newProxyProtocolConn(conn, 0, logger)

	// RemoteAddr triggers parsing
	addr := ppConn.RemoteAddr()
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("Expected *net.TCPAddr, got %T", addr)
	}

	if tcpAddr.IP.String() != "192.168.1.100" {
		t.Errorf("Expected source IP 192.168.1.100, got %s", tcpAddr.IP.String())
	}
	if tcpAddr.Port != 12345 {
		t.Errorf("Expected source port 12345, got %d", tcpAddr.Port)
	}
}

func TestParseProxyProtocolV1_TCP6(t *testing.T) {
	header := "PROXY TCP6 2001:db8::1 2001:db8::2 54321 443\r\nGET / HTTP/1.1\r\n"
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 9999}
	conn := newMockConn([]byte(header), remoteAddr)

	logger := zap.NewNop()
	ppConn := newProxyProtocolConn(conn, 0, logger)

	addr := ppConn.RemoteAddr()
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("Expected *net.TCPAddr, got %T", addr)
	}

	if tcpAddr.IP.String() != "2001:db8::1" {
		t.Errorf("Expected source IP 2001:db8::1, got %s", tcpAddr.IP.String())
	}
	if tcpAddr.Port != 54321 {
		t.Errorf("Expected source port 54321, got %d", tcpAddr.Port)
	}
}

func TestParseProxyProtocolV2_TCP4(t *testing.T) {
	// Build a v2 header
	header := BuildProxyProtocolV2Header(
		&net.TCPAddr{IP: net.ParseIP("10.0.0.50"), Port: 45678},
		&net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 443},
	)
	if header == nil {
		t.Fatal("Failed to build v2 header")
	}

	// Append some request data
	data := make([]byte, 0, len(header)+len("GET / HTTP/1.1\r\n"))
	data = append(data, header...)
	data = append(data, []byte("GET / HTTP/1.1\r\n")...)

	remoteAddr := &net.TCPAddr{IP: net.ParseIP("10.0.0.2"), Port: 9999}
	conn := newMockConn(data, remoteAddr)

	logger := zap.NewNop()
	ppConn := newProxyProtocolConn(conn, 0, logger)

	addr := ppConn.RemoteAddr()
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("Expected *net.TCPAddr, got %T", addr)
	}

	if tcpAddr.IP.String() != "10.0.0.50" {
		t.Errorf("Expected source IP 10.0.0.50, got %s", tcpAddr.IP.String())
	}
	if tcpAddr.Port != 45678 {
		t.Errorf("Expected source port 45678, got %d", tcpAddr.Port)
	}
}

func TestParseProxyProtocolV2_TCP6(t *testing.T) {
	header := BuildProxyProtocolV2Header(
		&net.TCPAddr{IP: net.ParseIP("2001:db8::100"), Port: 55555},
		&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 443},
	)
	if header == nil {
		t.Fatal("Failed to build v2 header")
	}

	data := make([]byte, 0, len(header)+len("GET / HTTP/1.1\r\n"))
	data = append(data, header...)
	data = append(data, []byte("GET / HTTP/1.1\r\n")...)

	remoteAddr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 9999}
	conn := newMockConn(data, remoteAddr)

	logger := zap.NewNop()
	ppConn := newProxyProtocolConn(conn, 0, logger)

	addr := ppConn.RemoteAddr()
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("Expected *net.TCPAddr, got %T", addr)
	}

	if tcpAddr.IP.String() != "2001:db8::100" {
		t.Errorf("Expected source IP 2001:db8::100, got %s", tcpAddr.IP.String())
	}
	if tcpAddr.Port != 55555 {
		t.Errorf("Expected source port 55555, got %d", tcpAddr.Port)
	}
}

func TestProxyProtocolListener_UntrustedSource(t *testing.T) {
	// Create a pipe for testing
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	logger := zap.NewNop()

	// Create a mock listener
	ml := &mockListener{
		conns: make(chan net.Conn, 1),
	}
	ml.conns <- serverConn

	// Create PROXY protocol listener with trusted CIDRs that don't match
	ppListener, err := NewProxyProtocolListener(ml, 0, []string{"192.168.0.0/16"}, logger)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	// Accept exercises the untrusted-source code path.
	// net.Pipe addresses are not TCP addresses, so isTrusted returns false
	// and the original (non-proxied) address is returned.
	_, err = ppListener.Accept()
	if err != nil {
		t.Fatalf("Accept failed: %v", err)
	}
}

func TestBuildProxyProtocolV1Header(t *testing.T) {
	header := BuildProxyProtocolV1Header(
		&net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345},
		&net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 80},
	)

	expected := "PROXY TCP4 192.168.1.1 10.0.0.1 12345 80\r\n"
	if string(header) != expected {
		t.Errorf("Expected %q, got %q", expected, string(header))
	}
}

func TestBuildProxyProtocolV2Header_Roundtrip(t *testing.T) {
	srcAddr := &net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 33333}
	dstAddr := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 443}

	header := BuildProxyProtocolV2Header(srcAddr, dstAddr)
	if header == nil {
		t.Fatal("Failed to build v2 header")
	}

	// Verify signature
	if !bytes.Equal(header[:proxyProtoV2SignatureLen], proxyProtoV2Sig) {
		t.Error("V2 header does not start with correct signature")
	}

	// Parse it back
	data := make([]byte, 0, len(header)+len("test data"))
	data = append(data, header...)
	data = append(data, []byte("test data")...)
	conn := newMockConn(data, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
	ppConn := newProxyProtocolConn(conn, 2, zap.NewNop())

	addr := ppConn.RemoteAddr()
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("Expected *net.TCPAddr, got %T", addr)
	}

	if tcpAddr.IP.String() != "10.1.2.3" {
		t.Errorf("Expected IP 10.1.2.3, got %s", tcpAddr.IP)
	}
	if tcpAddr.Port != 33333 {
		t.Errorf("Expected port 33333, got %d", tcpAddr.Port)
	}

	// Read remaining data
	remaining := make([]byte, 100)
	n, err := ppConn.Read(remaining)
	if err != nil && err != io.EOF {
		t.Errorf("Unexpected read error: %v", err)
	}
	if string(remaining[:n]) != "test data" {
		t.Errorf("Expected remaining data 'test data', got %q", string(remaining[:n]))
	}
}

func TestNoProxyProtocolHeader(t *testing.T) {
	// Regular HTTP request without PROXY header
	data := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 5555}
	conn := newMockConn(data, remoteAddr)

	logger := zap.NewNop()
	ppConn := newProxyProtocolConn(conn, 0, logger)

	// Should return original remote addr since no PROXY header
	addr := ppConn.RemoteAddr()
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("Expected *net.TCPAddr, got %T", addr)
	}

	if tcpAddr.IP.String() != "192.168.1.1" {
		t.Errorf("Expected original IP 192.168.1.1, got %s", tcpAddr.IP.String())
	}

	// Read data should still work
	buf := make([]byte, len(data))
	total := 0
	for total < len(data) {
		n, err := ppConn.Read(buf[total:])
		if err != nil {
			if err == io.EOF && total > 0 {
				break
			}
			t.Fatalf("Read error: %v", err)
		}
		total += n
	}
}

// mockListener for testing
type mockListener struct {
	conns chan net.Conn
}

func (l *mockListener) Accept() (net.Conn, error) {
	conn, ok := <-l.conns
	if !ok {
		return nil, fmt.Errorf("listener closed")
	}
	return conn, nil
}

func (l *mockListener) Close() error {
	close(l.conns)
	return nil
}

func (l *mockListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 8080}
}
