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
	"io"
	"net"
	"testing"
)

// pipeConn creates a connected pair of net.Conn using net.Pipe.
func pipeConn(data []byte) net.Conn {
	server, client := net.Pipe()
	go func() {
		_, _ = client.Write(data)
		_ = client.Close()
	}()
	return server
}

func TestDetectProtocolHTTP1GET(t *testing.T) {
	conn := pipeConn([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
	proto, pc := DetectProtocol(conn)
	if proto != ProtocolHTTP1 {
		t.Errorf("Expected HTTP1, got %s", proto)
	}

	// Verify peeked data is still readable
	buf := make([]byte, 3)
	n, err := pc.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(buf[:n]) != "GET" {
		t.Errorf("Expected peeked data to start with 'GET', got %q", string(buf[:n]))
	}
	_ = pc.Close()
}

func TestDetectProtocolHTTP1POST(t *testing.T) {
	conn := pipeConn([]byte("POST /api HTTP/1.1\r\nContent-Type: application/json\r\n\r\n{}"))
	proto, pc := DetectProtocol(conn)
	if proto != ProtocolHTTP1 {
		t.Errorf("Expected HTTP1, got %s", proto)
	}
	_ = pc.Close()
}

func TestDetectProtocolHTTP2(t *testing.T) {
	preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	conn := pipeConn(preface)
	proto, pc := DetectProtocol(conn)
	if proto != ProtocolHTTP2 {
		t.Errorf("Expected HTTP2, got %s", proto)
	}
	_ = pc.Close()
}

func TestDetectProtocolTLS(t *testing.T) {
	// TLS Client Hello starts with 0x16 0x03 (version)
	tlsHello := []byte{0x16, 0x03, 0x01, 0x00, 0x50, 0x01, 0x00, 0x00,
		0x4c, 0x03, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00}
	conn := pipeConn(tlsHello)
	proto, pc := DetectProtocol(conn)
	if proto != ProtocolTLS {
		t.Errorf("Expected TLS, got %s", proto)
	}

	// Verify data is still readable through PeekConn
	buf := make([]byte, 2)
	_, err := pc.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if len(buf) >= 2 && (buf[0] != 0x16 || buf[1] != 0x03) { //nolint:gosec // bounds checked
		t.Errorf("Expected TLS header bytes, got %x %x", buf[0], buf[1])
	}
	_ = pc.Close()
}

func TestDetectProtocolOpaque(t *testing.T) {
	// Binary data that doesn't match any protocol
	conn := pipeConn([]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f})
	proto, pc := DetectProtocol(conn)
	if proto != ProtocolOpaque {
		t.Errorf("Expected Opaque, got %s", proto)
	}
	_ = pc.Close()
}

func TestDetectProtocolEmptyConn(t *testing.T) {
	conn := pipeConn(nil)
	proto, pc := DetectProtocol(conn)
	if proto != ProtocolOpaque {
		t.Errorf("Expected Opaque for empty connection, got %s", proto)
	}
	_ = pc.Close()
}

func TestPeekConnPreservesFullData(t *testing.T) {
	data := []byte("GET /test HTTP/1.1\r\nHost: example.com\r\n\r\n")
	conn := pipeConn(data)

	proto, pc := DetectProtocol(conn)
	if proto != ProtocolHTTP1 {
		t.Fatalf("Expected HTTP1, got %s", proto)
	}

	// Read all data through PeekConn
	all, err := io.ReadAll(pc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if string(all) != string(data) {
		t.Errorf("Expected full data to be preserved.\nGot:  %q\nWant: %q", string(all), string(data))
	}
	_ = pc.Close()
}
