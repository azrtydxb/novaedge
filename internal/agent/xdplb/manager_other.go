//go:build !linux

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
	"errors"
	"net"

	novaebpf "github.com/azrtydxb/novaedge/internal/agent/ebpf"
	"github.com/azrtydxb/novaedge/internal/agent/ebpf/conntrack"
	"github.com/azrtydxb/novaedge/internal/agent/ebpf/maglev"
	"go.uber.org/zap"
)

var (
	errXDPLBIsOnlySupportedOnLinux = errors.New("XDP LB is only supported on Linux")
)

// L4Route describes a VIP-to-backends mapping for XDP fast-path LB.
type L4Route struct {
	VIP      string
	Port     uint16
	Protocol uint8
	Backends []Backend
}

// Backend is a single upstream endpoint.
type Backend struct {
	Addr string
	Port uint16
	MAC  net.HardwareAddr
}

// Manager is a stub on non-Linux platforms.
type Manager struct{}

// NewManager returns a stub manager on non-Linux platforms.
func NewManager(_ *zap.Logger, _ *novaebpf.ProgramLoader, _ string) *Manager {
	return &Manager{}
}

// Start returns an error on non-Linux platforms.
func (m *Manager) Start() error {
	return errXDPLBIsOnlySupportedOnLinux
}

// Stop is a no-op on non-Linux platforms.
func (m *Manager) Stop() error { return nil }

// SyncBackends returns an error on non-Linux platforms.
func (m *Manager) SyncBackends(_ []L4Route) error {
	return errXDPLBIsOnlySupportedOnLinux
}

// Stats returns an empty map on non-Linux platforms.
func (m *Manager) Stats() map[string]uint64 {
	return map[string]uint64{}
}

// IsRunning returns false on non-Linux platforms.
func (m *Manager) IsRunning() bool { return false }

// SetMaglev is a no-op on non-Linux platforms.
func (m *Manager) SetMaglev(_ *maglev.Manager) {}

// SetConntrack is a no-op on non-Linux platforms.
func (m *Manager) SetConntrack(_ *conntrack.Conntrack) {}
