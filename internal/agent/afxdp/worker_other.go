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

package afxdp

import (
	"context"
	"fmt"
	"time"

	novaebpf "github.com/piwi3910/novaedge/internal/agent/ebpf"
	"go.uber.org/zap"
)

// PacketProcessor processes raw Ethernet frames.
type PacketProcessor interface {
	ProcessPacket(data []byte) (response []byte, err error)
}

// WorkerConfig configures an AF_XDP worker.
type WorkerConfig struct {
	InterfaceName string
	QueueID       int
	FrameSize     int
	NumFrames     int
	PollTimeout   time.Duration
}

// VIPKey identifies a VIP flow to redirect.
type VIPKey struct {
	Addr  [4]byte
	Port  uint16
	Proto uint8
	Pad   uint8
}

// Worker is a stub on non-Linux platforms.
type Worker struct{}

// NewWorker returns a stub worker on non-Linux platforms.
func NewWorker(_ *zap.Logger, _ *novaebpf.ProgramLoader, _ WorkerConfig, _ PacketProcessor) *Worker {
	return &Worker{}
}

// Start returns an error on non-Linux platforms.
func (w *Worker) Start(_ context.Context) error {
	return fmt.Errorf("AF_XDP is only supported on Linux")
}

// Stop is a no-op on non-Linux platforms.
func (w *Worker) Stop() error { return nil }

// SyncVIPs returns an error on non-Linux platforms.
func (w *Worker) SyncVIPs(_ []VIPKey) error {
	return fmt.Errorf("AF_XDP is only supported on Linux")
}

// Stats returns an empty map on non-Linux platforms.
func (w *Worker) Stats() map[string]uint64 {
	return map[string]uint64{}
}

// IsRunning returns false on non-Linux platforms.
func (w *Worker) IsRunning() bool { return false }
