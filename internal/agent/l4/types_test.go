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

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestListenerType_Constants(t *testing.T) {
	if ListenerTypeTCP != "TCP" {
		t.Errorf("ListenerTypeTCP = %q, want %q", ListenerTypeTCP, "TCP")
	}
	if ListenerTypeUDP != "UDP" {
		t.Errorf("ListenerTypeUDP = %q, want %q", ListenerTypeUDP, "UDP")
	}
	if ListenerTypeTLSPassthrough != "TLS" {
		t.Errorf("ListenerTypeTLSPassthrough = %q, want %q", ListenerTypeTLSPassthrough, "TLS")
	}
}

func TestListenerConfig_Fields(t *testing.T) {
	cfg := ListenerConfig{
		Name:       "test-listener",
		Port:       8080,
		Type:       ListenerTypeTCP,
		BackendName: "test-backend",
		Backends: []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080},
			{Address: "10.0.0.2", Port: 8080},
		},
		TCPConfig: &TCPProxyConfig{
			IdleTimeout: 300,
		},
	}

	if cfg.Name != "test-listener" {
		t.Errorf("Name = %q, want %q", cfg.Name, "test-listener")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.Type != ListenerTypeTCP {
		t.Errorf("Type = %q, want %q", cfg.Type, ListenerTypeTCP)
	}
	if cfg.BackendName != "test-backend" {
		t.Errorf("BackendName = %q, want %q", cfg.BackendName, "test-backend")
	}
	if len(cfg.Backends) != 2 {
		t.Errorf("Backends length = %d, want 2", len(cfg.Backends))
	}
	if cfg.TCPConfig == nil {
		t.Error("TCPConfig should not be nil")
	}
}

func TestNewManager(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(logger)

	if manager == nil {
		t.Fatal("NewManager returned nil")
	}
	if manager.logger == nil {
		t.Error("manager logger should not be nil")
	}
	if manager.listeners == nil {
		t.Error("manager listeners map should be initialized")
	}
}

func TestManager_InitialState(t *testing.T) {
	manager := NewManager(zap.NewNop())

	// Initially no listeners
	if len(manager.listeners) != 0 {
		t.Errorf("initial listeners count = %d, want 0", len(manager.listeners))
	}
}

func TestTCPProxyConfig_Defaults(t *testing.T) {
	cfg := &TCPProxyConfig{
		IdleTimeout: 3600,
	}

	if cfg.IdleTimeout != 3600 {
		t.Errorf("IdleTimeout = %d, want 3600", cfg.IdleTimeout)
	}
}

func TestUDPProxyConfig_Defaults(t *testing.T) {
	cfg := &UDPProxyConfig{
		SessionTimeout: 60,
	}

	if cfg.SessionTimeout != 60 {
		t.Errorf("SessionTimeout = %d, want 60", cfg.SessionTimeout)
	}
}

func TestTLSPassthroughConfig_Fields(t *testing.T) {
	cfg := &TLSPassthroughConfig{
		ListenerName:   "test-tls",
		SNIReadTimeout: 5 * time.Second,
		ConnectTimeout: 10 * time.Second,
		IdleTimeout:    300 * time.Second,
		BufferSize:     4096,
	}

	if cfg.ListenerName != "test-tls" {
		t.Errorf("ListenerName = %q, want %q", cfg.ListenerName, "test-tls")
	}
	if cfg.BufferSize != 4096 {
		t.Errorf("BufferSize = %d, want 4096", cfg.BufferSize)
	}
}
