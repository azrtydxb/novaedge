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
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestTCPProxy_HandleConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a test backend server
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create backend listener: %v", err)
	}
	defer func() { _ = backendListener.Close() }()

	backendAddr := backendListener.Addr().(*net.TCPAddr)

	// Backend echoes back what it receives
	go func() {
		for {
			conn, acceptErr := backendListener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				buf := make([]byte, 1024)
				n, readErr := conn.Read(buf)
				if readErr != nil {
					return
				}
				_, _ = conn.Write(buf[:n])
			}()
		}
	}()

	backends := []*pb.Endpoint{
		{
			Address: backendAddr.IP.String(),
			Port:    int32(backendAddr.Port),
			Ready:   true,
		},
	}

	proxy := NewTCPProxy(TCPProxyConfig{
		ListenerName:   "test-tcp",
		ConnectTimeout: 2 * time.Second,
		IdleTimeout:    2 * time.Second,
		Backends:       backends,
		BackendName:    "test-backend",
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create client connection pair
	clientConn, serverConn := net.Pipe()

	// Send data from client side
	testData := []byte("hello TCP proxy")
	go func() {
		_, _ = clientConn.Write(testData)
		// Read response
		buf := make([]byte, 1024)
		n, readErr := clientConn.Read(buf)
		if readErr != nil {
			return
		}
		if string(buf[:n]) != string(testData) {
			t.Errorf("Expected %q, got %q", string(testData), string(buf[:n]))
		}
		_ = clientConn.Close()
	}()

	proxy.HandleConnection(ctx, serverConn)
}

func TestTCPProxy_PickBackend_RoundRobin(t *testing.T) {
	logger := zaptest.NewLogger(t)

	backends := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	proxy := NewTCPProxy(TCPProxyConfig{
		ListenerName: "test-rr",
		Backends:     backends,
		BackendName:  "test",
	}, logger)

	// Pick backends and verify round-robin
	seen := make(map[string]int)
	for i := 0; i < 9; i++ {
		ep := proxy.pickBackend()
		if ep == nil {
			t.Fatal("pickBackend returned nil")
		}
		seen[ep.Address]++
	}

	// Each backend should be picked 3 times
	for _, backend := range backends {
		if seen[backend.Address] != 3 {
			t.Errorf("Expected backend %s to be picked 3 times, got %d",
				backend.Address, seen[backend.Address])
		}
	}
}

func TestTCPProxy_PickBackend_NoReady(t *testing.T) {
	logger := zaptest.NewLogger(t)

	backends := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: false},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
	}

	proxy := NewTCPProxy(TCPProxyConfig{
		ListenerName: "test-no-ready",
		Backends:     backends,
		BackendName:  "test",
	}, logger)

	ep := proxy.pickBackend()
	if ep != nil {
		t.Errorf("Expected nil for no ready backends, got %v", ep)
	}
}

func TestTCPProxy_UpdateBackends(t *testing.T) {
	logger := zaptest.NewLogger(t)

	initialBackends := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	proxy := NewTCPProxy(TCPProxyConfig{
		ListenerName: "test-update",
		Backends:     initialBackends,
		BackendName:  "test",
	}, logger)

	ep := proxy.pickBackend()
	if ep == nil || ep.Address != "10.0.0.1" {
		t.Fatal("Expected initial backend")
	}

	// Update backends
	newBackends := []*pb.Endpoint{
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}
	proxy.UpdateBackends(newBackends)

	// Verify new backends are used
	ep = proxy.pickBackend()
	if ep == nil {
		t.Fatal("Expected backend after update")
	}
	if ep.Address != "10.0.0.2" && ep.Address != "10.0.0.3" {
		t.Errorf("Expected updated backend, got %s", ep.Address)
	}
}

func TestTCPProxy_Drain(t *testing.T) {
	logger := zaptest.NewLogger(t)

	proxy := NewTCPProxy(TCPProxyConfig{
		ListenerName: "test-drain",
		Backends:     []*pb.Endpoint{{Address: "10.0.0.1", Port: 8080, Ready: true}},
		BackendName:  "test",
	}, logger)

	if proxy.IsDraining() {
		t.Error("Proxy should not be draining initially")
	}

	proxy.Drain(100 * time.Millisecond)

	if !proxy.IsDraining() {
		t.Error("Proxy should be draining after Drain()")
	}
}

func TestTCPProxy_ConnectionForwarding(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start a TCP echo server as backend
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	defer func() { _ = backendListener.Close() }()

	backendAddr := backendListener.Addr().(*net.TCPAddr)

	go func() {
		for {
			conn, acceptErr := backendListener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn) // Echo
			}()
		}
	}()

	// Start a TCP proxy listener
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start proxy listener: %v", err)
	}
	defer func() { _ = proxyListener.Close() }()

	proxyAddr := proxyListener.Addr().(*net.TCPAddr)

	backends := []*pb.Endpoint{
		{
			Address: backendAddr.IP.String(),
			Port:    int32(backendAddr.Port),
			Ready:   true,
		},
	}

	proxy := NewTCPProxy(TCPProxyConfig{
		ListenerName:   "test-forward",
		ConnectTimeout: 2 * time.Second,
		IdleTimeout:    2 * time.Second,
		Backends:       backends,
		BackendName:    "test-backend",
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		for {
			conn, acceptErr := proxyListener.Accept()
			if acceptErr != nil {
				return
			}
			go proxy.HandleConnection(ctx, conn)
		}
	}()

	// Connect to proxy and send data
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyAddr.Port), 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect to proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	testData := "hello through proxy"
	_, err = conn.Write([]byte(testData))
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	buf := make([]byte, 1024)
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("Failed to set deadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != testData {
		t.Errorf("Expected %q, got %q", testData, string(buf[:n]))
	}
}

func TestTCPProxy_DefaultConfig(t *testing.T) {
	logger := zap.NewNop()

	proxy := NewTCPProxy(TCPProxyConfig{
		ListenerName: "test-defaults",
	}, logger)

	if proxy.config.ConnectTimeout != DefaultTCPConnectTimeout {
		t.Errorf("Expected default connect timeout %v, got %v",
			DefaultTCPConnectTimeout, proxy.config.ConnectTimeout)
	}
	if proxy.config.IdleTimeout != DefaultTCPIdleTimeout {
		t.Errorf("Expected default idle timeout %v, got %v",
			DefaultTCPIdleTimeout, proxy.config.IdleTimeout)
	}
	if proxy.config.BufferSize != DefaultTCPBufferSize {
		t.Errorf("Expected default buffer size %d, got %d",
			DefaultTCPBufferSize, proxy.config.BufferSize)
	}
	if proxy.config.DrainTimeout != DefaultDrainTimeout {
		t.Errorf("Expected default drain timeout %v, got %v",
			DefaultDrainTimeout, proxy.config.DrainTimeout)
	}
}
