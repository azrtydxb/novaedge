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
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// proxyProtoV1Prefix is the PROXY protocol v1 header prefix
	proxyProtoV1Prefix = "PROXY "

	// proxyProtoV2Signature is the 12-byte PROXY protocol v2 signature
	proxyProtoV2SignatureLen = 12

	// proxyProtoV1MaxLineLen is the maximum length of a PROXY protocol v1 header line
	proxyProtoV1MaxLineLen = 108

	// proxyProtoReadTimeout is the timeout for reading the PROXY protocol header
	proxyProtoReadTimeout = 5 * time.Second

	// proxyProtoV2CommandLocal is the LOCAL command for v2
	proxyProtoV2CommandLocal = 0x20

	// proxyProtoV2CommandProxy is the PROXY command for v2
	proxyProtoV2CommandProxy = 0x21

	// proxyProtoV2FamilyTCPv4 is TCP over IPv4
	proxyProtoV2FamilyTCPv4 = 0x11

	// proxyProtoV2FamilyTCPv6 is TCP over IPv6
	proxyProtoV2FamilyTCPv6 = 0x21

	// proxyProtoV2HeaderMinLen is the minimum PROXY protocol v2 header length
	proxyProtoV2HeaderMinLen = 16
)

// proxyProtoV2Sig is the 12-byte signature for PROXY protocol v2
var proxyProtoV2Sig = []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

// ProxyProtocolListener wraps a net.Listener to transparently parse PROXY protocol headers
type ProxyProtocolListener struct {
	net.Listener
	logger       *zap.Logger
	trustedCIDRs []*net.IPNet
	version      int32 // 0 = both, 1 = v1 only, 2 = v2 only
}

// NewProxyProtocolListener creates a new PROXY protocol listener wrapper
func NewProxyProtocolListener(inner net.Listener, version int32, trustedCIDRs []string, logger *zap.Logger) (*ProxyProtocolListener, error) {
	parsed := make([]*net.IPNet, 0, len(trustedCIDRs))
	for _, cidr := range trustedCIDRs {
		// Handle plain IPs without CIDR notation
		if !strings.Contains(cidr, "/") {
			if strings.Contains(cidr, ":") {
				cidr += "/128"
			} else {
				cidr += "/32"
			}
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted CIDR %q: %w", cidr, err)
		}
		parsed = append(parsed, network)
	}

	return &ProxyProtocolListener{
		Listener:     inner,
		logger:       logger,
		trustedCIDRs: parsed,
		version:      version,
	}, nil
}

// Accept waits for and returns the next connection, parsing PROXY protocol headers
func (l *ProxyProtocolListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	// Check if the source is trusted
	if !l.isTrusted(conn.RemoteAddr()) {
		if ce := l.logger.Check(zap.DebugLevel, "Connection from untrusted source, skipping PROXY protocol"); ce != nil {
			ce.Write(zap.String("remote_addr", conn.RemoteAddr().String()))
		}
		return conn, nil
	}

	return newProxyProtocolConn(conn, l.version, l.logger), nil
}

// isTrusted checks if the connection source is in the trusted CIDR list
func (l *ProxyProtocolListener) isTrusted(addr net.Addr) bool {
	// If no trusted CIDRs are configured, trust all sources
	if len(l.trustedCIDRs) == 0 {
		return true
	}

	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return false
	}

	for _, cidr := range l.trustedCIDRs {
		if cidr.Contains(tcpAddr.IP) {
			return true
		}
	}

	return false
}

// proxyProtocolConn wraps a net.Conn to parse the PROXY protocol header on first read
type proxyProtocolConn struct {
	net.Conn
	logger  *zap.Logger
	version int32

	once        sync.Once
	realAddr    net.Addr
	parseErr    error
	extraData   []byte // buffered data after PROXY header
	extraOffset int
}

func newProxyProtocolConn(conn net.Conn, version int32, logger *zap.Logger) *proxyProtocolConn {
	return &proxyProtocolConn{
		Conn:    conn,
		logger:  logger,
		version: version,
	}
}

// RemoteAddr returns the real client address extracted from the PROXY protocol header,
// or the original connection address if no valid header was found.
func (c *proxyProtocolConn) RemoteAddr() net.Addr {
	c.once.Do(c.parseHeader)
	if c.realAddr != nil {
		return c.realAddr
	}
	return c.Conn.RemoteAddr()
}

// Read reads from the connection, parsing the PROXY header on the first call.
func (c *proxyProtocolConn) Read(b []byte) (int, error) {
	c.once.Do(c.parseHeader)
	if c.parseErr != nil {
		return 0, c.parseErr
	}

	// If we have buffered extra data from header parsing, return that first
	if c.extraOffset < len(c.extraData) {
		n := copy(b, c.extraData[c.extraOffset:])
		c.extraOffset += n
		return n, nil
	}

	return c.Conn.Read(b)
}

func (c *proxyProtocolConn) parseHeader() {
	// Set a read deadline for the PROXY header
	if err := c.SetReadDeadline(time.Now().Add(proxyProtoReadTimeout)); err != nil {
		if ce := c.logger.Check(zap.DebugLevel, "Failed to set PROXY protocol read deadline"); ce != nil {
			ce.Write(zap.Error(err))
		}
	}
	defer func() {
		// Reset the deadline
		if err := c.SetReadDeadline(time.Time{}); err != nil {
			if ce := c.logger.Check(zap.DebugLevel, "Failed to reset read deadline"); ce != nil {
				ce.Write(zap.Error(err))
			}
		}
	}()

	// Read enough bytes to determine the protocol version
	buf := make([]byte, proxyProtoV2HeaderMinLen)
	n, err := io.ReadFull(c.Conn, buf)
	if err != nil {
		// Not enough data for a PROXY header - treat as normal connection
		if n > 0 {
			c.extraData = buf[:n]
		}
		return
	}

	// Check for v2 signature
	if bytes.Equal(buf[:proxyProtoV2SignatureLen], proxyProtoV2Sig) {
		if c.version == 1 {
			// v2 not allowed, treat as normal data
			c.extraData = buf
			return
		}
		c.parseV2Header(buf)
		return
	}

	// Check for v1 prefix
	if string(buf[:len(proxyProtoV1Prefix)]) == proxyProtoV1Prefix {
		if c.version == 2 {
			// v1 not allowed, treat as normal data
			c.extraData = buf
			return
		}
		c.parseV1Header(buf)
		return
	}

	// Not a PROXY protocol header - buffer the data
	c.extraData = buf
}

func (c *proxyProtocolConn) parseV1Header(initialBuf []byte) {
	// Read the rest of the v1 header line (up to CRLF)
	reader := bufio.NewReaderSize(
		io.MultiReader(bytes.NewReader(initialBuf), c.Conn),
		proxyProtoV1MaxLineLen,
	)

	line, err := reader.ReadString('\n')
	if err != nil {
		c.logger.Warn("Failed to read PROXY v1 header line", zap.Error(err))
		c.extraData = initialBuf
		return
	}

	// Remove trailing CRLF
	line = strings.TrimRight(line, "\r\n")

	// Parse: PROXY TCP4/TCP6/UNKNOWN srcIP dstIP srcPort dstPort
	parts := strings.Split(line, " ")
	if len(parts) < 2 {
		c.logger.Warn("Invalid PROXY v1 header: insufficient fields")
		c.extraData = initialBuf
		return
	}

	// Handle UNKNOWN protocol
	if parts[1] == "UNKNOWN" {
		if ce := c.logger.Check(zap.DebugLevel, "PROXY v1 UNKNOWN protocol, using connection address"); ce != nil {
			ce.Write()
		}
		// Buffer any extra data after the header
		c.bufferReaderRemaining(reader)
		return
	}

	if len(parts) != 6 {
		c.logger.Warn("Invalid PROXY v1 header: expected 6 fields",
			zap.Int("got", len(parts)))
		c.extraData = initialBuf
		return
	}

	srcIP := net.ParseIP(parts[2])
	if srcIP == nil {
		c.logger.Warn("Invalid PROXY v1 source IP", zap.String("ip", parts[2]))
		c.extraData = initialBuf
		return
	}

	var srcPort int
	if _, err := fmt.Sscanf(parts[4], "%d", &srcPort); err != nil {
		c.logger.Warn("Invalid PROXY v1 source port", zap.String("port", parts[4]))
		c.extraData = initialBuf
		return
	}

	c.realAddr = &net.TCPAddr{
		IP:   srcIP,
		Port: srcPort,
	}

	if ce := c.logger.Check(zap.DebugLevel, "PROXY v1 header parsed"); ce != nil {
		ce.Write(
			zap.String("src_addr", c.realAddr.String()),
			zap.String("protocol", parts[1]),
		)
	}

	// Buffer any remaining data in the reader
	c.bufferReaderRemaining(reader)
}

func (c *proxyProtocolConn) bufferReaderRemaining(reader *bufio.Reader) {
	remaining := reader.Buffered()
	if remaining > 0 {
		extra := make([]byte, remaining)
		n, _ := reader.Read(extra)
		if n > 0 {
			c.extraData = extra[:n]
		}
	}
}

func (c *proxyProtocolConn) parseV2Header(headerBuf []byte) {
	// headerBuf is 16 bytes: 12 signature + 1 version/command + 1 family + 2 length
	verCmd := headerBuf[12]
	family := headerBuf[13]
	addrLen := binary.BigEndian.Uint16(headerBuf[14:16])

	// Check command
	switch verCmd {
	case proxyProtoV2CommandLocal:
		// LOCAL command - no address info
		if ce := c.logger.Check(zap.DebugLevel, "PROXY v2 LOCAL command, using connection address"); ce != nil {
			ce.Write()
		}
		// Read and discard the remaining address bytes
		if addrLen > 0 {
			discard := make([]byte, addrLen)
			if _, err := io.ReadFull(c.Conn, discard); err != nil {
				c.logger.Warn("Failed to discard PROXY v2 address data", zap.Error(err))
			}
		}
		return
	case proxyProtoV2CommandProxy:
		// PROXY command - parse address
	default:
		c.logger.Warn("Unknown PROXY v2 command", zap.Uint8("command", verCmd))
		c.extraData = headerBuf
		return
	}

	// Read address data
	if addrLen == 0 {
		return
	}
	addrData := make([]byte, addrLen)
	if _, err := io.ReadFull(c.Conn, addrData); err != nil {
		c.logger.Warn("Failed to read PROXY v2 address data", zap.Error(err))
		return
	}

	// Parse address based on family
	switch family {
	case proxyProtoV2FamilyTCPv4:
		if len(addrData) < 12 {
			c.logger.Warn("PROXY v2 IPv4 address too short")
			return
		}
		srcIP := net.IP(addrData[0:4])
		srcPort := binary.BigEndian.Uint16(addrData[8:10])
		c.realAddr = &net.TCPAddr{
			IP:   srcIP,
			Port: int(srcPort),
		}
	case proxyProtoV2FamilyTCPv6:
		if len(addrData) < 36 {
			c.logger.Warn("PROXY v2 IPv6 address too short")
			return
		}
		srcIP := net.IP(addrData[0:16])
		srcPort := binary.BigEndian.Uint16(addrData[32:34])
		c.realAddr = &net.TCPAddr{
			IP:   srcIP,
			Port: int(srcPort),
		}
	default:
		c.logger.Warn("Unknown PROXY v2 address family", zap.Uint8("family", family))
		return
	}

	if ce := c.logger.Check(zap.DebugLevel, "PROXY v2 header parsed"); ce != nil {
		ce.Write(zap.String("src_addr", c.realAddr.String()))
	}
}

// BuildProxyProtocolV1Header generates a PROXY protocol v1 header for forwarding to backends
func BuildProxyProtocolV1Header(clientAddr, localAddr net.Addr) []byte {
	clientTCP, ok := clientAddr.(*net.TCPAddr)
	if !ok {
		return nil
	}
	localTCP, ok := localAddr.(*net.TCPAddr)
	if !ok {
		return nil
	}

	family := "TCP4"
	if clientTCP.IP.To4() == nil {
		family = "TCP6"
	}

	header := fmt.Sprintf("PROXY %s %s %s %d %d\r\n",
		family,
		clientTCP.IP.String(),
		localTCP.IP.String(),
		clientTCP.Port,
		localTCP.Port,
	)

	return []byte(header)
}

// BuildProxyProtocolV2Header generates a PROXY protocol v2 header for forwarding to backends
func BuildProxyProtocolV2Header(clientAddr, localAddr net.Addr) []byte {
	clientTCP, ok := clientAddr.(*net.TCPAddr)
	if !ok {
		return nil
	}
	localTCP, ok := localAddr.(*net.TCPAddr)
	if !ok {
		return nil
	}

	var family byte
	var addrData []byte

	if clientTCP.IP.To4() != nil {
		// IPv4
		family = proxyProtoV2FamilyTCPv4
		srcIP := clientTCP.IP.To4()
		dstIP := localTCP.IP.To4()
		if dstIP == nil {
			dstIP = net.IPv4zero.To4()
		}
		addrData = make([]byte, 12)
		copy(addrData[0:4], srcIP)
		copy(addrData[4:8], dstIP)
		binary.BigEndian.PutUint16(addrData[8:10], uint16(clientTCP.Port))
		binary.BigEndian.PutUint16(addrData[10:12], uint16(localTCP.Port))
	} else {
		// IPv6
		family = proxyProtoV2FamilyTCPv6
		srcIP := clientTCP.IP.To16()
		dstIP := localTCP.IP.To16()
		if dstIP == nil {
			dstIP = net.IPv6zero
		}
		addrData = make([]byte, 36)
		copy(addrData[0:16], srcIP)
		copy(addrData[16:32], dstIP)
		binary.BigEndian.PutUint16(addrData[32:34], uint16(clientTCP.Port))
		binary.BigEndian.PutUint16(addrData[34:36], uint16(localTCP.Port))
	}

	// Build header: signature + version/command + family + length + address
	header := make([]byte, 0, proxyProtoV2HeaderMinLen+len(addrData))
	header = append(header, proxyProtoV2Sig...)
	header = append(header, proxyProtoV2CommandProxy) // version 2, PROXY command
	header = append(header, family)
	lenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBytes, uint16(len(addrData)))
	header = append(header, lenBytes...)
	header = append(header, addrData...)

	return header
}
