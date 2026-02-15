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
	"io"
	"net"
	"strings"
)

// Protocol identifies the detected application protocol.
type Protocol string

const (
	// ProtocolHTTP1 is HTTP/1.0 or HTTP/1.1.
	ProtocolHTTP1 Protocol = "http1"

	// ProtocolHTTP2 is HTTP/2 (h2 prior knowledge / connection preface).
	ProtocolHTTP2 Protocol = "http2"

	// ProtocolTLS is TLS (Client Hello).
	ProtocolTLS Protocol = "tls"

	// ProtocolOpaque is an unrecognised TCP stream — treated as opaque L4.
	ProtocolOpaque Protocol = "opaque"
)

// http2Preface is the HTTP/2 connection preface sent by clients.
// "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
var http2Preface = []byte("PRI * HTTP/2")

// peekSize is how many bytes we read ahead for protocol detection.
const peekSize = 16

// PeekConn wraps a net.Conn with a buffered reader so that peeked bytes
// are not lost. Subsequent reads on PeekConn return the peeked data first.
type PeekConn struct {
	net.Conn
	reader *bufio.Reader
}

// Read reads from the buffered reader (peeked data first, then the connection).
func (pc *PeekConn) Read(b []byte) (int, error) {
	return pc.reader.Read(b)
}

// DetectProtocol peeks at the first bytes of the connection to determine
// the application-level protocol. It returns the detected protocol and a
// PeekConn that replays the peeked bytes transparently.
func DetectProtocol(conn net.Conn) (Protocol, *PeekConn) {
	br := bufio.NewReaderSize(conn, peekSize*2)
	peeked, err := br.Peek(peekSize)

	pc := &PeekConn{Conn: conn, reader: br}

	if err != nil && (err != io.EOF || len(peeked) == 0) {
		return ProtocolOpaque, pc
	}

	if len(peeked) == 0 {
		return ProtocolOpaque, pc
	}

	// Check for HTTP/2 connection preface: "PRI * HTTP/2"
	if len(peeked) >= len(http2Preface) && string(peeked[:len(http2Preface)]) == string(http2Preface) {
		return ProtocolHTTP2, pc
	}

	// Check for TLS Client Hello: starts with 0x16 (handshake) then 0x03 (TLS version)
	if peeked[0] == 0x16 && len(peeked) > 1 && peeked[1] == 0x03 {
		return ProtocolTLS, pc
	}

	// Check for HTTP/1.x methods
	s := string(peeked)
	for _, method := range []string{"GET ", "POST ", "PUT ", "DELETE ", "PATCH ", "HEAD ", "OPTIONS ", "CONNECT "} {
		if strings.HasPrefix(s, method) {
			return ProtocolHTTP1, pc
		}
	}

	return ProtocolOpaque, pc
}
