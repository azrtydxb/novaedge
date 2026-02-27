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

package maglev

import (
	"errors"

	"go.uber.org/zap"
)

var (
	errEBPFMaglevIsOnlySupportedOnLinux = errors.New("eBPF Maglev is only supported on Linux")
)

// Manager is a stub on non-Linux platforms.
type Manager struct{}

// NewManager returns a stub manager on non-Linux platforms.
func NewManager(_ *zap.Logger, _ uint32) *Manager {
	return &Manager{}
}

// Init returns an error on non-Linux platforms.
func (m *Manager) Init() error {
	return errEBPFMaglevIsOnlySupportedOnLinux
}

// UpdateTable returns an error on non-Linux platforms.
func (m *Manager) UpdateTable(_ []Backend) error {
	return errEBPFMaglevIsOnlySupportedOnLinux
}

// Stats returns an empty map on non-Linux platforms.
func (m *Manager) Stats() map[string]uint64 {
	return map[string]uint64{}
}

// Close is a no-op on non-Linux platforms.
func (m *Manager) Close() error { return nil }
