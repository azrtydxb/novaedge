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
	"runtime"
	"testing"
)

func TestTrySplice_NonTCPConns_ReturnsFallback(t *testing.T) {
	// net.Pipe returns in-memory connections that are not *net.TCPConn,
	// so trySplice should return (0, false, nil) indicating fallback.
	src, dst := net.Pipe()
	defer func() {
		_ = src.Close()
		_ = dst.Close()
	}()

	n, used, err := trySplice(dst, src)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if used {
		t.Fatal("expected used=false for non-TCP connections")
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes transferred, got %d", n)
	}
}

func TestTrySplice_TCPConns_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("splice is only supported on Linux")
	}

	// Create a TCP listener and two TCP connections via loopback.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer func() {
		_ = ln.Close()
	}()

	// Accept connection in a goroutine.
	acceptCh := make(chan net.Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			errCh <- acceptErr
			return
		}
		acceptCh <- conn
	}()

	srcConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer func() {
		_ = srcConn.Close()
	}()

	var serverConn net.Conn
	select {
	case serverConn = <-acceptCh:
	case acceptErr := <-errCh:
		t.Fatalf("accept failed: %v", acceptErr)
	}
	defer func() {
		_ = serverConn.Close()
	}()

	// Create a second TCP connection pair for the destination.
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create second listener: %v", err)
	}
	defer func() {
		_ = ln2.Close()
	}()

	acceptCh2 := make(chan net.Conn, 1)
	errCh2 := make(chan error, 1)
	go func() {
		conn, acceptErr := ln2.Accept()
		if acceptErr != nil {
			errCh2 <- acceptErr
			return
		}
		acceptCh2 <- conn
	}()

	dstConn, err := net.Dial("tcp", ln2.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial second listener: %v", err)
	}
	defer func() {
		_ = dstConn.Close()
	}()

	var serverConn2 net.Conn
	select {
	case serverConn2 = <-acceptCh2:
	case acceptErr := <-errCh2:
		t.Fatalf("accept failed: %v", acceptErr)
	}
	defer func() {
		_ = serverConn2.Close()
	}()

	// Write data to the source side, close to signal EOF, then splice.
	testData := []byte("hello splice")
	if _, writeErr := srcConn.Write(testData); writeErr != nil {
		t.Fatalf("failed to write test data: %v", writeErr)
	}
	// Close the write side so splice sees EOF.
	if tcpConn, ok := srcConn.(*net.TCPConn); ok {
		if closeErr := tcpConn.CloseWrite(); closeErr != nil {
			t.Fatalf("failed to close write: %v", closeErr)
		}
	}

	// trySplice: serverConn (src side from server's perspective) -> dstConn
	n, used, spliceErr := trySplice(dstConn, serverConn)
	if spliceErr != nil {
		t.Fatalf("trySplice returned error: %v", spliceErr)
	}
	if !used {
		t.Fatal("expected splice to be used on Linux with TCP connections")
	}
	if n != int64(len(testData)) {
		t.Fatalf("expected %d bytes spliced, got %d", len(testData), n)
	}

	// Read the spliced data from the destination side.
	buf := make([]byte, 64)
	nRead, readErr := serverConn2.Read(buf)
	if readErr != nil {
		t.Fatalf("failed to read spliced data: %v", readErr)
	}
	if string(buf[:nRead]) != string(testData) {
		t.Fatalf("expected %q, got %q", testData, buf[:nRead])
	}
}

func TestTrySplice_NilConns_NonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("this test verifies non-Linux fallback behavior")
	}

	// On non-Linux, trySplice always returns (0, false, nil).
	src, dst := net.Pipe()
	defer func() {
		_ = src.Close()
		_ = dst.Close()
	}()

	n, used, err := trySplice(dst, src)
	if err != nil {
		t.Fatalf("expected no error on non-Linux, got: %v", err)
	}
	if used {
		t.Fatal("expected used=false on non-Linux")
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes on non-Linux, got %d", n)
	}
}
