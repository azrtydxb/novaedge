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
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
	"unsafe"

	"go.uber.org/zap"
)

var errNotTCPConnection = errors.New("not a TCP connection")

// OriginalDst extracts the original destination address from a TPROXY'd
// TCP connection using the SO_ORIGINAL_DST socket option.
func OriginalDst(conn net.Conn) (net.IP, int, error) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil, 0, errNotTCPConnection
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get raw conn: %w", err)
	}

	var origAddr syscall.RawSockaddrInet4
	var controlErr error

	err = rawConn.Control(func(fd uintptr) {
		// SO_ORIGINAL_DST = 80 on Linux
		const soOriginalDst = 80
		addrLen := uint32(unsafe.Sizeof(origAddr)) //nolint:gosec // safe sizeof
		_, _, errno := syscall.Syscall6(
			syscall.SYS_GETSOCKOPT,
			fd,
			syscall.SOL_IP,
			soOriginalDst,
			uintptr(unsafe.Pointer(&origAddr)), //nolint:gosec // required for syscall
			uintptr(unsafe.Pointer(&addrLen)),  //nolint:gosec // required for syscall
			0,
		)
		if errno != 0 {
			controlErr = fmt.Errorf("getsockopt SO_ORIGINAL_DST: %w", errno)
		}
	})
	if err != nil {
		return nil, 0, fmt.Errorf("raw conn control: %w", err)
	}
	if controlErr != nil {
		return nil, 0, controlErr
	}

	ip := net.IPv4(origAddr.Addr[0], origAddr.Addr[1], origAddr.Addr[2], origAddr.Addr[3])
	port := int(binary.BigEndian.Uint16((*[2]byte)(unsafe.Pointer(&origAddr.Port))[:])) //nolint:gosec // required for SO_ORIGINAL_DST port extraction

	return ip, port, nil
}

// Start begins listening for TPROXY'd connections. It blocks until the
// context is cancelled or the listener fails.
func (tl *TransparentListener) Start(ctx context.Context) error {
	var listener net.Listener

	if tl.listener != nil {
		// Use pre-created listener (e.g., when eBPF backend needs the FD).
		listener = tl.listener
	} else {
		// For TPROXY, we need IP_TRANSPARENT socket option.
		// Use ListenConfig to set the socket option before bind.
		lc := net.ListenConfig{
			Control: func(_, _ string, c syscall.RawConn) error {
				var controlErr error
				err := c.Control(func(fd uintptr) {
					// IP_TRANSPARENT allows binding to non-local addresses
					controlErr = syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1) //nolint:gosec // fd conversion required
				})
				if err != nil {
					return err
				}
				return controlErr
			},
		}

		addr := fmt.Sprintf("0.0.0.0:%d", tl.port)
		// TPROXY uses nf_tproxy_get_sock_v4() which searches the IPv4 socket
		// hash table. We must use "tcp4" (AF_INET) not "tcp" to ensure the
		// socket is registered in the IPv4 namespace; an AF_INET6 dual-stack
		// socket is invisible to the IPv4 TPROXY lookup and packets are dropped.
		var err error
		listener, err = lc.Listen(ctx, "tcp4", addr)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %w", addr, err)
		}
	}

	tl.logger.Info("Transparent listener started", zap.Int32("port", tl.port))

	go tl.acceptLoop(ctx, listener)

	// Wait for context cancellation
	<-ctx.Done()

	if closeErr := listener.Close(); closeErr != nil {
		tl.logger.Debug("Listener close error", zap.Error(closeErr))
	}

	return nil
}

// CreateListener creates the TCP listener with IP_TRANSPARENT socket option
// and returns it without starting the accept loop. Use this when you need
// the listener's file descriptor before calling Start().
func (tl *TransparentListener) CreateListener(ctx context.Context) (net.Listener, error) {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			var controlErr error
			err := c.Control(func(fd uintptr) {
				controlErr = syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1) //nolint:gosec // fd conversion required
			})
			if err != nil {
				return err
			}
			return controlErr
		},
	}

	addr := fmt.Sprintf("0.0.0.0:%d", tl.port)
	listener, err := lc.Listen(ctx, "tcp4", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	return listener, nil
}

// acceptLoop accepts connections and dispatches them to the handler.
func (tl *TransparentListener) acceptLoop(ctx context.Context, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				tl.logger.Error("Accept failed", zap.Error(err))
				continue
			}
		}

		origIP, origPort, err := OriginalDst(conn)
		if err != nil {
			// SO_ORIGINAL_DST failed — this happens with eBPF sk_lookup
			// interception where no NAT/conntrack is involved. In that
			// case, conn.LocalAddr() IS the original destination because
			// bpf_sk_assign preserves the packet's destination address.
			if tcpAddr, ok := conn.LocalAddr().(*net.TCPAddr); ok && !tcpAddr.IP.IsUnspecified() {
				origIP = tcpAddr.IP
				origPort = tcpAddr.Port
			} else {
				tl.logger.Error("Failed to get original destination",
					zap.String("remote", conn.RemoteAddr().String()),
					zap.Error(err))
				_ = conn.Close()
				continue
			}
		}

		tl.logger.Debug("Intercepted connection",
			zap.String("remote", conn.RemoteAddr().String()),
			zap.String("orig_dst", fmt.Sprintf("%s:%d", origIP, origPort)))

		go tl.handler(ctx, conn, origIP, origPort)
	}
}
