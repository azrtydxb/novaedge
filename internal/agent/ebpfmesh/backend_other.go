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

package ebpfmesh

import (
	"errors"

	novaebpf "github.com/azrtydxb/novaedge/internal/agent/ebpf"
	"github.com/azrtydxb/novaedge/internal/agent/mesh"
	"go.uber.org/zap"
)

var (
	errEBPFMeshRedirectIsOnlySupportedOnLinux = errors.New("eBPF mesh redirect is only supported on Linux")
)

// Backend is a stub on non-Linux platforms.
type Backend struct{}

// NewBackend returns a stub backend on non-Linux platforms.
func NewBackend(_ *zap.Logger, _ *novaebpf.ProgramLoader) *Backend {
	return &Backend{}
}

// Name returns the backend identifier.
func (b *Backend) Name() string {
	return "ebpf-sk-lookup"
}

// Setup returns an error on non-Linux platforms.
func (b *Backend) Setup() error {
	return errEBPFMeshRedirectIsOnlySupportedOnLinux
}

// ApplyRules returns an error on non-Linux platforms.
func (b *Backend) ApplyRules(_ []mesh.InterceptTarget, _ int32) error {
	return errEBPFMeshRedirectIsOnlySupportedOnLinux
}

// Cleanup is a no-op on non-Linux platforms.
func (b *Backend) Cleanup() error {
	return nil
}
