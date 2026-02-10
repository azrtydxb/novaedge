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
	"net"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestManager_ApplyConfig_StartTCPListener(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Find a free port
	port := findFreePort(t)

	configs := []L4ListenerConfig{
		{
			Name:        "tcp-test",
			Port:        port,
			Type:        ListenerTypeTCP,
			BackendName: "backend1",
			Backends: []*pb.Endpoint{
				{Address: "127.0.0.1", Port: 9999, Ready: true},
			},
		},
	}

	err := manager.ApplyConfig(ctx, configs)
	if err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	if manager.GetActiveListeners() != 1 {
		t.Errorf("Expected 1 active listener, got %d", manager.GetActiveListeners())
	}

	// Verify we can connect to the listener
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect to TCP listener: %v", err)
	}
	_ = conn.Close()

	// Shutdown
	err = manager.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	if manager.GetActiveListeners() != 0 {
		t.Errorf("Expected 0 active listeners after shutdown, got %d", manager.GetActiveListeners())
	}
}

func TestManager_ApplyConfig_StopRemovedListener(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	port := findFreePort(t)

	// Start with one listener
	configs := []L4ListenerConfig{
		{
			Name:        "tcp-to-remove",
			Port:        port,
			Type:        ListenerTypeTCP,
			BackendName: "backend1",
			Backends: []*pb.Endpoint{
				{Address: "127.0.0.1", Port: 9999, Ready: true},
			},
		},
	}

	err := manager.ApplyConfig(ctx, configs)
	if err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	if manager.GetActiveListeners() != 1 {
		t.Errorf("Expected 1 active listener, got %d", manager.GetActiveListeners())
	}

	// Apply empty config to remove the listener
	err = manager.ApplyConfig(ctx, nil)
	if err != nil {
		t.Fatalf("ApplyConfig (empty) failed: %v", err)
	}

	if manager.GetActiveListeners() != 0 {
		t.Errorf("Expected 0 active listeners, got %d", manager.GetActiveListeners())
	}

	// Verify the port is released (should fail to connect)
	time.Sleep(100 * time.Millisecond) // Give time for listener to close
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Error("Expected connection to fail after listener removal")
	}
}

func TestManager_ApplyConfig_UpdateBackends(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	port := findFreePort(t)

	configs := []L4ListenerConfig{
		{
			Name:        "tcp-update",
			Port:        port,
			Type:        ListenerTypeTCP,
			BackendName: "backend1",
			Backends: []*pb.Endpoint{
				{Address: "127.0.0.1", Port: 9998, Ready: true},
			},
		},
	}

	err := manager.ApplyConfig(ctx, configs)
	if err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	// Update backends (same listener name/port, different backends)
	configs[0].Backends = []*pb.Endpoint{
		{Address: "127.0.0.1", Port: 9997, Ready: true},
		{Address: "127.0.0.1", Port: 9996, Ready: true},
	}

	err = manager.ApplyConfig(ctx, configs)
	if err != nil {
		t.Fatalf("ApplyConfig (update) failed: %v", err)
	}

	// Still should have 1 listener
	if manager.GetActiveListeners() != 1 {
		t.Errorf("Expected 1 active listener, got %d", manager.GetActiveListeners())
	}

	_ = manager.Shutdown(ctx)
}

func TestManager_ApplyConfig_UDPListener(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	port := findFreePort(t)

	configs := []L4ListenerConfig{
		{
			Name:        "udp-test",
			Port:        port,
			Type:        ListenerTypeUDP,
			BackendName: "backend1",
			Backends: []*pb.Endpoint{
				{Address: "127.0.0.1", Port: 9999, Ready: true},
			},
		},
	}

	err := manager.ApplyConfig(ctx, configs)
	if err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	if manager.GetActiveListeners() != 1 {
		t.Errorf("Expected 1 active listener, got %d", manager.GetActiveListeners())
	}

	_ = manager.Shutdown(ctx)
}

func TestManager_ApplyConfig_TLSPassthrough(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	port := findFreePort(t)

	configs := []L4ListenerConfig{
		{
			Name: "tls-test",
			Port: port,
			Type: ListenerTypeTLSPassthrough,
			TLSPassthroughConfig: &TLSPassthroughConfig{
				Routes: map[string]*TLSRoute{
					"example.com": {
						Hostname:    "example.com",
						Backends:    []*pb.Endpoint{{Address: "10.0.0.1", Port: 443, Ready: true}},
						BackendName: "example-backend",
					},
				},
			},
		},
	}

	err := manager.ApplyConfig(ctx, configs)
	if err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	if manager.GetActiveListeners() != 1 {
		t.Errorf("Expected 1 active listener, got %d", manager.GetActiveListeners())
	}

	_ = manager.Shutdown(ctx)
}

func TestListenerKey(t *testing.T) {
	key := listenerKey("my-listener", 8080)
	if key != "my-listener:8080" {
		t.Errorf("Expected 'my-listener:8080', got %q", key)
	}
}

// findFreePort finds a free TCP port for testing
func findFreePort(t *testing.T) int32 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return int32(port)
}
