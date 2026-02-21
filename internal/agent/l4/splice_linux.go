//go:build linux

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
	"net"

	"golang.org/x/sys/unix"
)

const (
	// splicePipeSize is the pipe buffer size used for splice transfers (1 MB).
	splicePipeSize = 1 << 20
)

// trySplice attempts to use splice() for zero-copy data transfer between two TCP connections.
// Returns the number of bytes transferred, whether splice was used, and any error.
// If splice is not supported (e.g., non-TCP sockets), returns (0, false, nil).
func trySplice(dst, src net.Conn) (int64, bool, error) {
	srcTCP, srcOK := src.(*net.TCPConn)
	dstTCP, dstOK := dst.(*net.TCPConn)
	if !srcOK || !dstOK {
		return 0, false, nil
	}

	// Get raw file descriptors
	srcRaw, err := srcTCP.SyscallConn()
	if err != nil {
		return 0, false, nil
	}
	dstRaw, err := dstTCP.SyscallConn()
	if err != nil {
		return 0, false, nil
	}

	// Create pipe for splice
	var pipeFDs [2]int
	if err := unix.Pipe2(pipeFDs[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return 0, false, nil
	}
	defer func() {
		_ = unix.Close(pipeFDs[0])
		_ = unix.Close(pipeFDs[1])
	}()

	// Increase pipe buffer size for better throughput
	_, _ = unix.FcntlInt(uintptr(pipeFDs[0]), unix.F_SETPIPE_SZ, splicePipeSize)

	var total int64

	for {
		// Splice from src socket to pipe write end
		var nRead int64
		var readErr error

		readControlErr := srcRaw.Read(func(fd uintptr) bool {
			n, err := unix.Splice(int(fd), nil, pipeFDs[1], nil, splicePipeSize,
				unix.SPLICE_F_NONBLOCK|unix.SPLICE_F_MOVE)
			if err == unix.EAGAIN {
				return false // tell Go runtime to wait for readability
			}
			if err != nil {
				readErr = err
				return true
			}
			nRead = int64(n)
			return true
		})
		if readControlErr != nil {
			if total > 0 {
				return total, true, nil
			}
			return 0, false, nil
		}
		if readErr != nil {
			if total > 0 {
				return total, true, nil
			}
			return 0, false, nil
		}
		if nRead == 0 {
			// EOF
			return total, true, nil
		}

		// Splice from pipe read end to dst socket
		var written int64
		for written < nRead {
			var nWrite int64
			var writeErr error

			writeControlErr := dstRaw.Write(func(fd uintptr) bool {
				n, err := unix.Splice(pipeFDs[0], nil, int(fd), nil, int(nRead-written),
					unix.SPLICE_F_NONBLOCK|unix.SPLICE_F_MOVE)
				if err == unix.EAGAIN {
					return false
				}
				if err != nil {
					writeErr = err
					return true
				}
				nWrite = int64(n)
				return true
			})
			if writeControlErr != nil || writeErr != nil {
				return total + written, true, writeErr
			}
			written += nWrite
		}
		total += written
	}
}
