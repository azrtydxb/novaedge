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

package vip

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"

	"go.uber.org/zap"
)

var (
	errFailedToGetLocalUDPAddress = errors.New("failed to get local UDP address")
	errBFDTransportNotStarted     = errors.New("BFD transport not started")
)

// bfdTransport handles BFD UDP packet I/O per RFC 5881.
//
// It listens for incoming BFD control packets on the configured port and
// provides a Send method for transmitting packets to peers. Received
// packets are dispatched to the BFDManager for state machine processing.
type bfdTransport struct {
	logger     *zap.Logger
	conn       *net.UDPConn
	manager    *BFDManager
	listenPort int
	localPort  int // actual bound port (may differ from listenPort when using port 0)

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

// newBFDTransport creates a new BFD UDP transport.
// Use listenPort=0 to let the OS assign an ephemeral port (useful for testing).
func newBFDTransport(logger *zap.Logger, manager *BFDManager, listenPort int) *bfdTransport {
	return &bfdTransport{
		logger:     logger.Named("bfd-transport"),
		manager:    manager,
		listenPort: listenPort,
	}
}

// Start opens the UDP socket and begins the receive loop.
func (t *bfdTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	listenAddr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: t.listenPort,
	}

	conn, err := net.ListenUDP("udp4", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", t.listenPort, err)
	}

	// RFC 5881 Section 4: BFD Control packets MUST be transmitted with a
	// TTL/Hop Limit value of 255. Single-hop receivers MUST verify TTL >= 254.
	rawConn, err := conn.SyscallConn()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to get raw connection for TTL: %w", err)
	}
	var setsockoptErr error
	if controlErr := rawConn.Control(func(fd uintptr) {
		setsockoptErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TTL, 255) //nolint:gosec // G115: fd conversion is safe on supported 64-bit platforms
	}); controlErr != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to set BFD socket options: %w", controlErr)
	}
	if setsockoptErr != nil {
		t.logger.Warn("Failed to set BFD TTL socket option, BFD may not work with strict peers",
			zap.Error(setsockoptErr),
		)
	}

	// Record the actual bound port (important when listenPort is 0)
	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		_ = conn.Close()
		return errFailedToGetLocalUDPAddress
	}
	t.localPort = localAddr.Port

	t.conn = conn
	t.ctx, t.cancel = context.WithCancel(ctx)

	t.logger.Info("BFD transport started",
		zap.Int("listen_port", t.localPort),
	)

	go t.receiveLoop()

	return nil
}

// Stop closes the UDP socket and stops the receive loop.
func (t *bfdTransport) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cancel != nil {
		t.cancel()
	}

	if t.conn != nil {
		_ = t.conn.Close()
		t.conn = nil
	}

	t.logger.Info("BFD transport stopped")
}

// LocalPort returns the actual port the transport is listening on.
// This is useful when listenPort was 0 (OS-assigned).
func (t *bfdTransport) LocalPort() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.localPort
}

// Send transmits a BFD control packet to a peer.
// The destination port is the peer's BFD control port.
func (t *bfdTransport) Send(peerAddr net.IP, peerPort int, pkt *bfdControlPacket) error {
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()

	if conn == nil {
		return errBFDTransportNotStarted
	}

	data, err := encodeBFDPacket(pkt)
	if err != nil {
		return fmt.Errorf("failed to encode BFD packet: %w", err)
	}

	dst := &net.UDPAddr{
		IP:   peerAddr,
		Port: peerPort,
	}

	_, err = conn.WriteToUDP(data, dst)
	if err != nil {
		return fmt.Errorf("failed to send BFD packet to %s:%d: %w", peerAddr, peerPort, err)
	}

	return nil
}

// receiveLoop reads BFD control packets from the UDP socket and dispatches
// them to the BFDManager for processing.
func (t *bfdTransport) receiveLoop() {
	buf := make([]byte, 256) // BFD control packets are 24 bytes minimum

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		n, remoteAddr, err := t.conn.ReadFromUDP(buf)
		if err != nil {
			// Check if the context was cancelled (normal shutdown)
			select {
			case <-t.ctx.Done():
				return
			default:
			}
			t.logger.Warn("Error reading BFD packet",
				zap.Error(err),
			)
			continue
		}

		if n < bfdPacketLength {
			t.logger.Debug("Ignoring short BFD packet",
				zap.Int("bytes", n),
				zap.String("from", remoteAddr.String()),
			)
			continue
		}

		pkt, err := decodeBFDPacket(buf[:n])
		if err != nil {
			t.logger.Debug("Failed to decode BFD packet",
				zap.Error(err),
				zap.String("from", remoteAddr.String()),
			)
			continue
		}

		if t.manager != nil {
			t.manager.ProcessPacket(remoteAddr.IP, pkt.State, pkt.MyDiscriminator)
		}
	}
}
