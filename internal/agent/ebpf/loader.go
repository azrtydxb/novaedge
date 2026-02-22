//go:build linux

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
	"os"
	"path/filepath"

	"github.com/cilium/ebpf"
	"go.uber.org/zap"
)

const (
	// DefaultPinPath is the default bpffs mount path where NovaEdge pins
	// its BPF objects for persistence across restarts.
	DefaultPinPath = "/sys/fs/bpf/novaedge"
)

// ProgramLoader manages the lifecycle of BPF programs and maps. It handles
// loading compiled BPF ELF objects, pinning them to bpffs for persistence,
// and cleaning up on shutdown.
type ProgramLoader struct {
	pinPath     string
	logger      *zap.Logger
	collections []*ebpf.Collection
}

// NewProgramLoader creates a loader that pins BPF objects under the given
// bpffs path. If pinPath is empty, DefaultPinPath is used.
func NewProgramLoader(logger *zap.Logger, pinPath string) *ProgramLoader {
	if pinPath == "" {
		pinPath = DefaultPinPath
	}
	return &ProgramLoader{
		pinPath: pinPath,
		logger:  logger.With(zap.String("component", "ebpf-loader")),
	}
}

// EnsurePinPath creates the bpffs pin directory if it does not exist.
func (l *ProgramLoader) EnsurePinPath() error {
	if err := os.MkdirAll(l.pinPath, 0700); err != nil {
		return fmt.Errorf("creating bpffs pin path %s: %w", l.pinPath, err)
	}
	return nil
}

// PinPath returns the resolved pin path for a given sub-directory name.
// Callers use this to construct per-subsystem pin paths (e.g. "mesh", "xdp").
func (l *ProgramLoader) PinPath(subsystem string) string {
	return filepath.Join(l.pinPath, subsystem)
}

// LoadCollection loads a BPF ELF collection from a CollectionSpec, optionally
// pinning maps to bpffs under the given subsystem directory. Pass an empty
// subsystem to skip pinning.
func (l *ProgramLoader) LoadCollection(spec *ebpf.CollectionSpec, subsystem string) (*ebpf.Collection, error) {
	opts := ebpf.CollectionOptions{}
	if subsystem != "" {
		pinDir := l.PinPath(subsystem)
		if err := os.MkdirAll(pinDir, 0700); err != nil {
			return nil, fmt.Errorf("creating pin dir %s: %w", pinDir, err)
		}
		opts.Maps.PinPath = pinDir
	}

	coll, err := ebpf.NewCollectionWithOptions(spec, opts)
	if err != nil {
		eBPFErrorsTotal.WithLabelValues(subsystem, "load").Inc()
		return nil, fmt.Errorf("loading BPF collection: %w", err)
	}

	l.collections = append(l.collections, coll)
	eBPFProgramsLoaded.WithLabelValues(subsystem).Inc()
	l.logger.Info("BPF collection loaded",
		zap.String("subsystem", subsystem),
		zap.Int("programs", len(coll.Programs)),
		zap.Int("maps", len(coll.Maps)),
	)

	return coll, nil
}

// Close releases all loaded BPF collections, detaching programs and closing
// map file descriptors. It should be called during agent shutdown.
func (l *ProgramLoader) Close() error {
	var firstErr error
	for _, coll := range l.collections {
		coll.Close()
	}
	l.collections = nil
	l.logger.Info("all BPF collections closed")
	return firstErr
}

// CleanupPins removes all pinned BPF objects under the loader's pin path.
// This is a destructive operation intended for clean uninstall.
func (l *ProgramLoader) CleanupPins() error {
	l.logger.Info("removing pinned BPF objects", zap.String("path", l.pinPath))
	return os.RemoveAll(l.pinPath)
}
