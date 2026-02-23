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

// Package afxdp provides AF_XDP (XSK) zero-copy packet processing for
// high-throughput data plane acceleration. It works in conjunction with
// an XDP filter program that redirects matched flows to the AF_XDP socket.
package afxdp

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	novaebpf "github.com/piwi3910/novaedge/internal/agent/ebpf"
	"go.uber.org/zap"
)

const subsystem = "afxdp"

// PacketProcessor processes raw Ethernet frames received from the AF_XDP
// socket. Implementations perform the actual load-balancing or proxying
// logic on zero-copy packet data.
type PacketProcessor interface {
	// ProcessPacket processes a single raw Ethernet frame.
	// The data slice is borrowed from the AF_XDP UMEM and must not be
	// retained after the call returns.
	ProcessPacket(data []byte) (response []byte, err error)
}

// WorkerConfig configures an AF_XDP worker.
type WorkerConfig struct {
	// InterfaceName is the network interface to bind to.
	InterfaceName string
	// QueueID is the NIC RX queue to bind the AF_XDP socket to.
	QueueID int
	// FrameSize is the UMEM frame size in bytes (default: 4096).
	FrameSize int
	// NumFrames is the number of UMEM frames (default: 4096).
	NumFrames int
	// PollTimeout is the poll timeout for the AF_XDP socket (default: 100ms).
	PollTimeout time.Duration
}

// Worker manages an AF_XDP socket and its associated XDP filter program.
type Worker struct {
	logger    *zap.Logger
	loader    *novaebpf.ProgramLoader
	config    WorkerConfig
	processor PacketProcessor
	mu        sync.RWMutex
	xdpLink   link.Link
	prog      *ebpf.Program
	vipMap    *ebpf.Map
	statsMap  *ebpf.Map
	xskMap    *ebpf.Map
	xsk       *xskSocket
	running   bool
}

// NewWorker creates an AF_XDP worker for the given interface and queue.
func NewWorker(logger *zap.Logger, loader *novaebpf.ProgramLoader, cfg WorkerConfig, processor PacketProcessor) *Worker {
	if cfg.FrameSize == 0 {
		cfg.FrameSize = 4096
	}
	if cfg.NumFrames == 0 {
		cfg.NumFrames = 4096
	}
	if cfg.PollTimeout == 0 {
		cfg.PollTimeout = 100 * time.Millisecond
	}
	return &Worker{
		logger:    logger.With(zap.String("component", "afxdp-worker"), zap.Int("queue", cfg.QueueID)),
		loader:    loader,
		config:    cfg,
		processor: processor,
	}
}

// Start loads the XDP filter program, creates the AF_XDP socket, and begins
// the poll loop. It blocks until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return fmt.Errorf("AF_XDP worker already running")
	}

	start := time.Now()

	// Load BPF collection.
	spec, err := loadAfxdpRedirect()
	if err != nil {
		w.mu.Unlock()
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("loading AF_XDP BPF spec: %w", err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		w.mu.Unlock()
		novaebpf.RecordError(subsystem, "load")
		return fmt.Errorf("creating AF_XDP BPF collection: %w", err)
	}

	prog := coll.Programs["afxdp_redirect_prog"]
	if prog == nil {
		coll.Close()
		w.mu.Unlock()
		return fmt.Errorf("afxdp_redirect_prog not found in BPF collection")
	}

	vipMap := coll.Maps["afxdp_vips"]
	if vipMap == nil {
		coll.Close()
		w.mu.Unlock()
		return fmt.Errorf("afxdp_vips map not found")
	}

	statsMap := coll.Maps["afxdp_stats"]

	xskMap := coll.Maps["xsk_map"]
	if xskMap == nil {
		coll.Close()
		w.mu.Unlock()
		return fmt.Errorf("xsk_map not found in BPF collection")
	}

	// Attach XDP program to interface.
	iface, err := net.InterfaceByName(w.config.InterfaceName)
	if err != nil {
		coll.Close()
		w.mu.Unlock()
		novaebpf.RecordError(subsystem, "attach")
		return fmt.Errorf("looking up interface %s: %w", w.config.InterfaceName, err)
	}

	xdpLink, err := link.AttachXDP(link.XDPOptions{
		Program:   prog,
		Interface: iface.Index,
	})
	if err != nil {
		coll.Close()
		w.mu.Unlock()
		novaebpf.RecordError(subsystem, "attach")
		return fmt.Errorf("attaching AF_XDP filter to %s: %w", w.config.InterfaceName, err)
	}

	// Create AF_XDP socket with UMEM and ring buffers.
	xsk, err := newXSKSocket(xskConfig{
		Ifindex:   iface.Index,
		QueueID:   w.config.QueueID,
		FrameSize: w.config.FrameSize,
		NumFrames: w.config.NumFrames,
	})
	if err != nil {
		xdpLink.Close()
		coll.Close()
		w.mu.Unlock()
		novaebpf.RecordError(subsystem, "xsk")
		return fmt.Errorf("creating AF_XDP socket: %w", err)
	}

	// Register socket in BPF xsk_map so the XDP program can redirect to it.
	if err := xsk.registerInMap(xskMap); err != nil {
		xsk.close()
		xdpLink.Close()
		coll.Close()
		w.mu.Unlock()
		novaebpf.RecordError(subsystem, "xsk")
		return fmt.Errorf("registering XSK in BPF map: %w", err)
	}

	w.prog = prog
	w.vipMap = vipMap
	w.statsMap = statsMap
	w.xskMap = xskMap
	w.xdpLink = xdpLink
	w.xsk = xsk
	w.running = true

	novaebpf.RecordProgramLoaded(subsystem)
	novaebpf.ObserveAttachDuration(subsystem, time.Since(start).Seconds())
	w.logger.Info("AF_XDP worker started",
		zap.String("interface", w.config.InterfaceName),
		zap.Int("ifindex", iface.Index),
		zap.Int("queue", w.config.QueueID),
		zap.Int("frame_size", w.config.FrameSize),
		zap.Int("num_frames", w.config.NumFrames))

	w.mu.Unlock()

	return w.pollLoop(ctx)
}

// pollLoop runs the XSK packet processing loop until ctx is cancelled.
func (w *Worker) pollLoop(ctx context.Context) error {
	timeoutMs := int(w.config.PollTimeout.Milliseconds())
	if timeoutMs <= 0 {
		timeoutMs = 100
	}

	for {
		select {
		case <-ctx.Done():
			return w.Stop()
		default:
		}

		// Wait for packets on the RX ring.
		n, err := w.xsk.poll(timeoutMs)
		if err != nil {
			w.logger.Warn("AF_XDP poll error", zap.Error(err))
			continue
		}
		if n == 0 {
			// Timeout, reclaim any completed TX frames.
			w.xsk.reclaimCompleted()
			continue
		}

		// Read received packets from the RX ring.
		rxDescs := w.xsk.receive()
		if len(rxDescs) == 0 {
			continue
		}

		txCount := 0
		for _, desc := range rxDescs {
			data := w.xsk.frameData(desc)

			resp, err := w.processor.ProcessPacket(data)
			if err != nil {
				w.logger.Debug("packet processing error", zap.Error(err))
				continue
			}

			// If the processor returned a response, transmit it.
			if resp != nil && len(resp) <= w.config.FrameSize {
				// Write response into the same UMEM frame.
				copy(w.xsk.umemArea[desc.Addr:], resp)
				txDesc := desc
				txDesc.Len = uint32(len(resp))
				if w.xsk.transmit(txDesc) {
					txCount++
				}
			}
		}

		// Return RX frames that weren't used for TX back to the fill ring.
		w.xsk.returnToFillRing(rxDescs)

		// Notify kernel about TX submissions.
		if txCount > 0 {
			if err := w.xsk.kick(); err != nil {
				w.logger.Warn("AF_XDP TX kick error", zap.Error(err))
			}
		}

		// Reclaim completed TX frames.
		w.xsk.reclaimCompleted()
	}
}

// Stop detaches the XDP program and releases resources.
func (w *Worker) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return nil
	}

	if w.xsk != nil {
		w.xsk.close()
		w.xsk = nil
	}
	if w.xdpLink != nil {
		if err := w.xdpLink.Close(); err != nil {
			w.logger.Warn("failed to detach AF_XDP filter", zap.Error(err))
		}
		w.xdpLink = nil
	}
	if w.prog != nil {
		w.prog.Close()
		w.prog = nil
		novaebpf.RecordProgramUnloaded(subsystem)
	}
	if w.vipMap != nil {
		w.vipMap.Close()
		w.vipMap = nil
	}
	if w.statsMap != nil {
		w.statsMap.Close()
		w.statsMap = nil
	}
	if w.xskMap != nil {
		w.xskMap.Close()
		w.xskMap = nil
	}

	w.running = false
	w.logger.Info("AF_XDP worker stopped")
	return nil
}

// UpdateVIPs synchronizes the afxdp_vips BPF map with the desired set of
// VIP keys to redirect to the AF_XDP socket.
type VIPKey struct {
	Addr  [4]byte
	Port  uint16
	Proto uint8
	Pad   uint8
}

// SyncVIPs replaces all entries in the afxdp_vips map.
func (w *Worker) SyncVIPs(vips []VIPKey) error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.vipMap == nil {
		return fmt.Errorf("AF_XDP worker not started")
	}

	// Delete all existing entries.
	var key VIPKey
	keysToDelete := make([]VIPKey, 0)
	iter := w.vipMap.Iterate()
	var val uint32
	for iter.Next(&key, &val) {
		keysToDelete = append(keysToDelete, key)
	}
	for _, k := range keysToDelete {
		_ = w.vipMap.Delete(k)
	}

	// Insert new entries.
	var placeholder uint32
	for _, vk := range vips {
		if err := w.vipMap.Update(vk, placeholder, ebpf.UpdateAny); err != nil {
			novaebpf.RecordMapOp("afxdp_vips", "update", "error")
			return fmt.Errorf("updating afxdp_vips: %w", err)
		}
		novaebpf.RecordMapOp("afxdp_vips", "update", "ok")
	}

	w.logger.Info("AF_XDP VIPs synced", zap.Int("count", len(vips)))
	return nil
}

// Stats returns per-CPU aggregated statistics from the AF_XDP filter program.
func (w *Worker) Stats() map[string]uint64 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make(map[string]uint64)
	if w.statsMap == nil {
		return result
	}

	statNames := []string{
		"xdp_pass", "xdp_redirect", "xdp_drop",
		"match", "miss",
	}

	for i, name := range statNames {
		key := uint32(i)
		var values []uint64
		if err := w.statsMap.Lookup(key, &values); err != nil {
			continue
		}
		var total uint64
		for _, v := range values {
			total += v
		}
		result[name] = total
	}

	return result
}

// IsRunning returns whether the AF_XDP worker is active.
func (w *Worker) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}
