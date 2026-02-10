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
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestUDPProxy_PickBackendBySourceIP(t *testing.T) {
	logger := zaptest.NewLogger(t)

	backends := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	proxy := NewUDPProxy(UDPProxyConfig{
		ListenerName: "test-udp",
		Backends:     backends,
		BackendName:  "test",
	}, logger)

	// Same source IP should consistently select the same backend
	sourceIP := "192.168.1.100"
	first := proxy.pickBackendBySourceIP(sourceIP)
	if first == nil {
		t.Fatal("pickBackendBySourceIP returned nil")
	}

	for i := 0; i < 10; i++ {
		ep := proxy.pickBackendBySourceIP(sourceIP)
		if ep == nil {
			t.Fatal("pickBackendBySourceIP returned nil")
		}
		if ep.Address != first.Address {
			t.Errorf("Session affinity broken: expected %s, got %s",
				first.Address, ep.Address)
		}
	}
}

func TestUDPProxy_PickBackendBySourceIP_DifferentSources(t *testing.T) {
	logger := zaptest.NewLogger(t)

	backends := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: true},
	}

	proxy := NewUDPProxy(UDPProxyConfig{
		ListenerName: "test-udp-diff",
		Backends:     backends,
		BackendName:  "test",
	}, logger)

	// Different source IPs should distribute across backends
	seen := make(map[string]bool)
	sources := []string{
		"192.168.1.1", "192.168.1.2", "192.168.1.3", "192.168.1.4",
		"192.168.2.1", "192.168.2.2", "192.168.3.1", "192.168.4.1",
		"10.1.1.1", "10.2.2.2", "10.3.3.3", "10.4.4.4",
	}
	for _, src := range sources {
		ep := proxy.pickBackendBySourceIP(src)
		if ep != nil {
			seen[ep.Address] = true
		}
	}

	// With enough sources and 4 backends, we should see at least 2 different backends
	if len(seen) < 2 {
		t.Errorf("Expected distribution across backends, only saw %d", len(seen))
	}
}

func TestUDPProxy_PickBackend_NoReady(t *testing.T) {
	logger := zaptest.NewLogger(t)

	backends := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: false},
	}

	proxy := NewUDPProxy(UDPProxyConfig{
		ListenerName: "test-udp-no-ready",
		Backends:     backends,
		BackendName:  "test",
	}, logger)

	ep := proxy.pickBackendBySourceIP("192.168.1.1")
	if ep != nil {
		t.Errorf("Expected nil for no ready backends, got %v", ep)
	}
}

func TestUDPProxy_UpdateBackends(t *testing.T) {
	logger := zaptest.NewLogger(t)

	proxy := NewUDPProxy(UDPProxyConfig{
		ListenerName: "test-udp-update",
		Backends:     []*pb.Endpoint{{Address: "10.0.0.1", Port: 8080, Ready: true}},
		BackendName:  "test",
	}, logger)

	ep := proxy.pickBackendBySourceIP("1.2.3.4")
	if ep == nil || ep.Address != "10.0.0.1" {
		t.Fatal("Expected initial backend")
	}

	proxy.UpdateBackends([]*pb.Endpoint{
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	})

	ep = proxy.pickBackendBySourceIP("1.2.3.4")
	if ep == nil || ep.Address != "10.0.0.2" {
		t.Fatal("Expected updated backend")
	}
}

func TestUDPProxy_DefaultConfig(t *testing.T) {
	logger := zap.NewNop()

	proxy := NewUDPProxy(UDPProxyConfig{
		ListenerName: "test-defaults",
	}, logger)

	if proxy.config.SessionTimeout != DefaultUDPSessionTimeout {
		t.Errorf("Expected default session timeout %v, got %v",
			DefaultUDPSessionTimeout, proxy.config.SessionTimeout)
	}
	if proxy.config.BufferSize != DefaultUDPBufferSize {
		t.Errorf("Expected default buffer size %d, got %d",
			DefaultUDPBufferSize, proxy.config.BufferSize)
	}
}

func TestUDPProxy_CleanupExpiredSessions(t *testing.T) {
	logger := zaptest.NewLogger(t)

	proxy := NewUDPProxy(UDPProxyConfig{
		ListenerName:   "test-cleanup",
		SessionTimeout: 50 * time.Millisecond,
		BackendName:    "test",
	}, logger)

	// Simulate expired session
	proxy.sessions.Store("expired-client:1234", &udpSession{
		lastSeen: time.Now().Add(-1 * time.Second),
	})

	proxy.sessions.Store("active-client:5678", &udpSession{
		lastSeen: time.Now(),
	})

	proxy.CleanupExpiredSessions()

	// Expired session should be removed
	if _, ok := proxy.sessions.Load("expired-client:1234"); ok {
		t.Error("Expired session should have been cleaned up")
	}

	// Active session should remain
	if _, ok := proxy.sessions.Load("active-client:5678"); !ok {
		t.Error("Active session should not have been cleaned up")
	}
}

func TestUDPProxy_CloseAllSessions(t *testing.T) {
	logger := zaptest.NewLogger(t)

	proxy := NewUDPProxy(UDPProxyConfig{
		ListenerName: "test-close-all",
		BackendName:  "test",
	}, logger)

	// Store some sessions
	proxy.sessions.Store("client1", &udpSession{lastSeen: time.Now()})
	proxy.sessions.Store("client2", &udpSession{lastSeen: time.Now()})

	proxy.CloseAllSessions()

	// All sessions should be removed
	count := 0
	proxy.sessions.Range(func(_, _ interface{}) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("Expected 0 sessions after CloseAllSessions, got %d", count)
	}
}
