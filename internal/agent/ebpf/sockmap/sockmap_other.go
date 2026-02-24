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

package sockmap

import (
	"fmt"
	"net"

	"go.uber.org/zap"
)

// Manager is a stub on non-Linux platforms where eBPF SOCKMAP is not available.
type Manager struct{}

// NewSockMapManager returns an error on non-Linux platforms since eBPF
// SOCKMAP requires Linux kernel support.
func NewSockMapManager(_ *zap.Logger) (*Manager, error) {
	return nil, fmt.Errorf("eBPF SOCKMAP is only supported on Linux")
}

// AddSameNodeEndpoint returns an error on non-Linux platforms.
func (m *Manager) AddSameNodeEndpoint(_ net.IP, _ uint16) error {
	return fmt.Errorf("eBPF SOCKMAP is only supported on Linux")
}

// RemoveSameNodeEndpoint returns an error on non-Linux platforms.
func (m *Manager) RemoveSameNodeEndpoint(_ net.IP, _ uint16) error {
	return fmt.Errorf("eBPF SOCKMAP is only supported on Linux")
}

// SyncEndpoints returns an error on non-Linux platforms.
func (m *Manager) SyncEndpoints(_ map[EndpointKey]EndpointValue) error {
	return fmt.Errorf("eBPF SOCKMAP is only supported on Linux")
}

// GetStats returns an error on non-Linux platforms.
func (m *Manager) GetStats() (uint64, uint64, error) {
	return 0, 0, fmt.Errorf("eBPF SOCKMAP is only supported on Linux")
}

// Close is a no-op on non-Linux platforms.
func (m *Manager) Close() error {
	return nil
}
