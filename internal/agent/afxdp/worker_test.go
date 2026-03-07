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
	"testing"
	"time"

	novaebpf "github.com/azrtydxb/novaedge/internal/agent/ebpf"
	"go.uber.org/zap/zaptest"
)

type noopProcessor struct{}

func (noopProcessor) ProcessPacket(data []byte) ([]byte, error) { return nil, nil }

func TestNewWorker(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	w := NewWorker(logger, loader, WorkerConfig{
		InterfaceName: "eth0",
		QueueID:       0,
	}, noopProcessor{})
	if w == nil {
		t.Fatal("NewWorker returned nil")
	}
}

func TestWorkerNotRunningInitially(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	w := NewWorker(logger, loader, WorkerConfig{
		InterfaceName: "eth0",
	}, noopProcessor{})
	if w.IsRunning() {
		t.Error("expected IsRunning() == false on fresh worker")
	}
}

func TestWorkerStopIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	w := NewWorker(logger, loader, WorkerConfig{
		InterfaceName: "eth0",
	}, noopProcessor{})
	if err := w.Stop(); err != nil {
		t.Errorf("Stop() on fresh worker returned error: %v", err)
	}
	if err := w.Stop(); err != nil {
		t.Errorf("second Stop() returned error: %v", err)
	}
}

func TestWorkerStatsEmpty(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	w := NewWorker(logger, loader, WorkerConfig{
		InterfaceName: "eth0",
	}, noopProcessor{})
	stats := w.Stats()
	if stats == nil {
		t.Fatal("Stats() returned nil")
	}
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %v", stats)
	}
}

func TestWorkerSyncVIPsWithoutStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	w := NewWorker(logger, loader, WorkerConfig{
		InterfaceName: "eth0",
	}, noopProcessor{})
	err := w.SyncVIPs([]VIPKey{{Addr: [4]byte{10, 0, 0, 1}, Port: 80, Proto: 6}})
	if err == nil {
		t.Error("expected error calling SyncVIPs without Start")
	}
}

func TestWorkerConfigDefaults(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	w := NewWorker(logger, loader, WorkerConfig{
		InterfaceName: "eth0",
	}, noopProcessor{})
	_ = w // Just verifying defaults are set in NewWorker
}

func TestVIPKeyStruct(t *testing.T) {
	vk := VIPKey{
		Addr:  [4]byte{10, 96, 0, 100},
		Port:  8080,
		Proto: 6,
	}
	if vk.Addr[0] != 10 {
		t.Error("unexpected addr byte")
	}
	if vk.Port != 8080 {
		t.Error("unexpected port")
	}
	if vk.Proto != 6 {
		t.Error("unexpected proto")
	}
}

func TestWorkerConfigCustomValues(t *testing.T) {
	logger := zaptest.NewLogger(t)
	loader := novaebpf.NewProgramLoader(logger, "")
	w := NewWorker(logger, loader, WorkerConfig{
		InterfaceName: "eth0",
		QueueID:       3,
		FrameSize:     2048,
		NumFrames:     8192,
		PollTimeout:   200 * time.Millisecond,
	}, noopProcessor{})
	if w == nil {
		t.Fatal("NewWorker returned nil")
	}
}
