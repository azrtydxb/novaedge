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

package ebpf

import (
	"fmt"

	"go.uber.org/zap"
)

const (
	// DefaultPinPath is the default bpffs mount path (unused on non-Linux).
	DefaultPinPath = "/sys/fs/bpf/novaedge"
)

// ProgramLoader is a stub on non-Linux platforms.
type ProgramLoader struct{}

// NewProgramLoader returns a stub loader on non-Linux platforms.
func NewProgramLoader(_ *zap.Logger, _ string) *ProgramLoader {
	return &ProgramLoader{}
}

// EnsurePinPath is a no-op on non-Linux platforms.
func (l *ProgramLoader) EnsurePinPath() error {
	return fmt.Errorf("eBPF is only supported on Linux")
}

// PinPath returns an empty string on non-Linux platforms.
func (l *ProgramLoader) PinPath(_ string) string {
	return ""
}

// Close is a no-op on non-Linux platforms.
func (l *ProgramLoader) Close() error {
	return nil
}

// CleanupPins is a no-op on non-Linux platforms.
func (l *ProgramLoader) CleanupPins() error {
	return nil
}
