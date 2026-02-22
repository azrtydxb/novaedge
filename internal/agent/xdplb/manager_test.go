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

package xdplb

import (
	"net"
	"testing"

	novaebpf "github.com/piwi3910/novaedge/internal/agent/ebpf"
	"go.uber.org/zap/zaptest"
)

func TestNewManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	m := NewManager(logger, loader, "eth0")
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestManagerNotRunningInitially(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	m := NewManager(logger, loader, "eth0")
	if m.IsRunning() {
		t.Error("expected IsRunning() == false on fresh manager")
	}
}

func TestManagerStopIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	m := NewManager(logger, loader, "eth0")
	if err := m.Stop(); err != nil {
		t.Errorf("Stop() on fresh manager returned error: %v", err)
	}
	if err := m.Stop(); err != nil {
		t.Errorf("second Stop() returned error: %v", err)
	}
}

func TestManagerStatsEmpty(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	m := NewManager(logger, loader, "eth0")
	stats := m.Stats()
	if stats == nil {
		t.Fatal("Stats() returned nil")
	}
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %v", stats)
	}
}

func TestManagerSyncBackendsWithoutStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	m := NewManager(logger, loader, "eth0")
	routes := []L4Route{
		{
			VIP:      "10.96.0.100",
			Port:     80,
			Protocol: 6,
			Backends: []Backend{
				{Addr: "10.0.0.1", Port: 8080, MAC: net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}},
			},
		},
	}
	err := m.SyncBackends(routes)
	if err == nil {
		t.Error("expected error calling SyncBackends without Start")
	}
}

func TestL4RouteType(t *testing.T) {
	route := L4Route{
		VIP:      "10.96.0.1",
		Port:     443,
		Protocol: 6,
		Backends: []Backend{
			{Addr: "10.0.1.1", Port: 8443},
			{Addr: "10.0.1.2", Port: 8443},
		},
	}
	if route.VIP != "10.96.0.1" {
		t.Error("unexpected VIP")
	}
	if len(route.Backends) != 2 {
		t.Error("expected 2 backends")
	}
}
