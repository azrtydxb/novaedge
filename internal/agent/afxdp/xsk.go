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

package afxdp

import (
	"fmt"
	"unsafe"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

// xskSocket manages an AF_XDP socket with UMEM and ring buffers.
type xskSocket struct {
	fd        int
	ifindex   int
	queueID   int
	frameSize int
	numFrames int

	// UMEM backing memory (mmap'd)
	umemArea []byte

	// Ring buffer memory regions (mmap'd)
	fillMap       []byte
	completionMap []byte
	rxMap         []byte
	txMap         []byte

	// Ring buffer pointers
	fillRing       ring
	completionRing ring
	rxRing         ring
	txRing         ring
}

// ring provides access to a shared producer/consumer ring buffer.
type ring struct {
	producer *uint32
	consumer *uint32
	descs    []unix.XDPDesc
	mask     uint32
}

// xskConfig contains XSK socket configuration.
type xskConfig struct {
	Ifindex   int
	QueueID   int
	FrameSize int
	NumFrames int
}

// newXSKSocket creates and configures an AF_XDP socket with UMEM.
func newXSKSocket(cfg xskConfig) (*xskSocket, error) {
	fd, err := unix.Socket(unix.AF_XDP, unix.SOCK_RAW, 0)
	if err != nil {
		return nil, fmt.Errorf("creating AF_XDP socket: %w", err)
	}

	s := &xskSocket{
		fd:        fd,
		ifindex:   cfg.Ifindex,
		queueID:   cfg.QueueID,
		frameSize: cfg.FrameSize,
		numFrames: cfg.NumFrames,
	}

	if err := s.setupUMEM(); err != nil {
		unix.Close(fd)
		return nil, err
	}

	if err := s.setupRings(); err != nil {
		s.close()
		return nil, err
	}

	if err := s.bind(); err != nil {
		s.close()
		return nil, err
	}

	// Pre-populate the fill ring so the kernel has buffers to write into.
	s.populateFillRing()

	return s, nil
}

// setupUMEM allocates and registers the UMEM (packet buffer memory).
func (s *xskSocket) setupUMEM() error {
	umemSize := s.frameSize * s.numFrames
	area, err := unix.Mmap(-1, 0, umemSize,
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_PRIVATE|unix.MAP_ANONYMOUS|unix.MAP_POPULATE)
	if err != nil {
		return fmt.Errorf("mmap UMEM area (%d bytes): %w", umemSize, err)
	}
	s.umemArea = area

	reg := unix.XDPUmemReg{
		Addr: uint64(uintptr(unsafe.Pointer(&area[0]))),
		Len:  uint64(umemSize),
		Size: uint32(s.frameSize),
	}
	_, _, errno := unix.Syscall6(
		unix.SYS_SETSOCKOPT,
		uintptr(s.fd),
		unix.SOL_XDP,
		unix.XDP_UMEM_REG,
		uintptr(unsafe.Pointer(&reg)),
		unsafe.Sizeof(reg),
		0,
	)
	if errno != 0 {
		return fmt.Errorf("setsockopt XDP_UMEM_REG: %w", errno)
	}

	// Set ring sizes (half for fill/completion, half for rx/tx).
	ringSize := uint32(s.numFrames)
	for _, opt := range []int{unix.XDP_UMEM_FILL_RING, unix.XDP_UMEM_COMPLETION_RING,
		unix.XDP_RX_RING, unix.XDP_TX_RING} {
		if err := unix.SetsockoptInt(s.fd, unix.SOL_XDP, opt, int(ringSize)); err != nil {
			return fmt.Errorf("setsockopt ring size (opt=%d): %w", opt, err)
		}
	}

	return nil
}

// setupRings mmaps the ring buffer regions and initializes ring pointers.
func (s *xskSocket) setupRings() error {
	// Get mmap offsets from the kernel.
	offsets, err := s.getMmapOffsets()
	if err != nil {
		return err
	}

	ringSize := uint32(s.numFrames)
	descSize := uint32(unsafe.Sizeof(unix.XDPDesc{}))

	// Fill ring
	fillSize := int(offsets.Fr.Desc + uint64(ringSize)*uint64(descSize))
	s.fillMap, err = unix.Mmap(s.fd, unix.XDP_UMEM_PGOFF_FILL_RING, fillSize,
		unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_POPULATE)
	if err != nil {
		return fmt.Errorf("mmap fill ring: %w", err)
	}
	s.fillRing = makeRing(s.fillMap, offsets.Fr, ringSize)

	// Completion ring
	compSize := int(offsets.Cr.Desc + uint64(ringSize)*uint64(descSize))
	s.completionMap, err = unix.Mmap(s.fd, unix.XDP_UMEM_PGOFF_COMPLETION_RING, compSize,
		unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_POPULATE)
	if err != nil {
		return fmt.Errorf("mmap completion ring: %w", err)
	}
	s.completionRing = makeRing(s.completionMap, offsets.Cr, ringSize)

	// RX ring
	rxSize := int(offsets.Rx.Desc + uint64(ringSize)*uint64(descSize))
	s.rxMap, err = unix.Mmap(s.fd, unix.XDP_PGOFF_RX_RING, rxSize,
		unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_POPULATE)
	if err != nil {
		return fmt.Errorf("mmap rx ring: %w", err)
	}
	s.rxRing = makeRing(s.rxMap, offsets.Rx, ringSize)

	// TX ring
	txSize := int(offsets.Tx.Desc + uint64(ringSize)*uint64(descSize))
	s.txMap, err = unix.Mmap(s.fd, unix.XDP_PGOFF_TX_RING, txSize,
		unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED|unix.MAP_POPULATE)
	if err != nil {
		return fmt.Errorf("mmap tx ring: %w", err)
	}
	s.txRing = makeRing(s.txMap, offsets.Tx, ringSize)

	return nil
}

// bind binds the AF_XDP socket to the interface and queue.
func (s *xskSocket) bind() error {
	sa := unix.SockaddrXDP{
		Flags:   0,
		Ifindex: uint32(s.ifindex),
		QueueID: uint32(s.queueID),
	}
	return unix.Bind(s.fd, &sa)
}

// registerInMap registers this socket's FD in the BPF xsk_map.
func (s *xskSocket) registerInMap(xskMap *ebpf.Map) error {
	key := uint32(s.queueID)
	val := uint32(s.fd)
	return xskMap.Update(key, val, ebpf.UpdateAny)
}

// populateFillRing fills the fill ring with all available frame addresses.
func (s *xskSocket) populateFillRing() {
	prod := *s.fillRing.producer
	for i := uint32(0); i < uint32(s.numFrames); i++ {
		idx := (prod + i) & s.fillRing.mask
		s.fillRing.descs[idx].Addr = uint64(i) * uint64(s.frameSize)
	}
	*s.fillRing.producer = prod + uint32(s.numFrames)
}

// poll waits for activity on the socket. Returns the number of events or 0 on timeout.
func (s *xskSocket) poll(timeoutMs int) (int, error) {
	fds := []unix.PollFd{{
		Fd:     int32(s.fd),
		Events: unix.POLLIN,
	}}
	n, err := unix.Poll(fds, timeoutMs)
	if err != nil && err != unix.EINTR {
		return 0, fmt.Errorf("poll AF_XDP socket: %w", err)
	}
	return n, nil
}

// receive returns packets from the RX ring. The returned descriptors reference
// UMEM frames that must be returned to the fill ring after processing.
func (s *xskSocket) receive() []unix.XDPDesc {
	cons := *s.rxRing.consumer
	prod := *s.rxRing.producer

	n := prod - cons
	if n == 0 {
		return nil
	}

	descs := make([]unix.XDPDesc, n)
	for i := uint32(0); i < n; i++ {
		idx := (cons + i) & s.rxRing.mask
		descs[i] = s.rxRing.descs[idx]
	}
	*s.rxRing.consumer = cons + n
	return descs
}

// transmit submits a packet for transmission via the TX ring.
func (s *xskSocket) transmit(desc unix.XDPDesc) bool {
	prod := *s.txRing.producer
	cons := *s.txRing.consumer

	// Check if TX ring is full
	if prod-cons >= uint32(s.numFrames) {
		return false
	}

	idx := prod & s.txRing.mask
	s.txRing.descs[idx] = desc
	*s.txRing.producer = prod + 1
	return true
}

// kick notifies the kernel about TX submissions via sendto.
func (s *xskSocket) kick() error {
	_, _, errno := unix.Syscall6(
		unix.SYS_SENDTO,
		uintptr(s.fd),
		0, 0, // no data, just a kick
		unix.MSG_DONTWAIT,
		0, 0,
	)
	if errno != 0 && errno != unix.EAGAIN && errno != unix.EBUSY && errno != unix.ENOBUFS {
		return fmt.Errorf("sendto kick: %w", errno)
	}
	return nil
}

// reclaimCompleted moves completed TX frames back to the fill ring.
func (s *xskSocket) reclaimCompleted() {
	cons := *s.completionRing.consumer
	prod := *s.completionRing.producer

	n := prod - cons
	if n == 0 {
		return
	}

	fillProd := *s.fillRing.producer
	for i := uint32(0); i < n; i++ {
		compIdx := (cons + i) & s.completionRing.mask
		addr := s.completionRing.descs[compIdx].Addr

		fillIdx := (fillProd + i) & s.fillRing.mask
		s.fillRing.descs[fillIdx].Addr = addr
	}
	*s.completionRing.consumer = cons + n
	*s.fillRing.producer = fillProd + n
}

// returnToFillRing returns frame addresses to the fill ring for reuse.
func (s *xskSocket) returnToFillRing(descs []unix.XDPDesc) {
	prod := *s.fillRing.producer
	for i, desc := range descs {
		idx := (prod + uint32(i)) & s.fillRing.mask
		s.fillRing.descs[idx].Addr = desc.Addr
	}
	*s.fillRing.producer = prod + uint32(len(descs))
}

// frameData returns the UMEM slice for a descriptor.
func (s *xskSocket) frameData(desc unix.XDPDesc) []byte {
	return s.umemArea[desc.Addr : desc.Addr+uint64(desc.Len)]
}

// close releases all resources.
func (s *xskSocket) close() {
	if s.fd >= 0 {
		unix.Close(s.fd)
		s.fd = -1
	}
	if s.umemArea != nil {
		unix.Munmap(s.umemArea)
		s.umemArea = nil
	}
	if s.fillMap != nil {
		unix.Munmap(s.fillMap)
		s.fillMap = nil
	}
	if s.completionMap != nil {
		unix.Munmap(s.completionMap)
		s.completionMap = nil
	}
	if s.rxMap != nil {
		unix.Munmap(s.rxMap)
		s.rxMap = nil
	}
	if s.txMap != nil {
		unix.Munmap(s.txMap)
		s.txMap = nil
	}
}

// getMmapOffsets retrieves the ring buffer mmap offsets from the kernel.
func (s *xskSocket) getMmapOffsets() (*unix.XDPMmapOffsets, error) {
	var offsets unix.XDPMmapOffsets
	optlen := uint32(unsafe.Sizeof(offsets))
	_, _, errno := unix.Syscall6(
		unix.SYS_GETSOCKOPT,
		uintptr(s.fd),
		unix.SOL_XDP,
		unix.XDP_MMAP_OFFSETS,
		uintptr(unsafe.Pointer(&offsets)),
		uintptr(unsafe.Pointer(&optlen)),
		0,
	)
	if errno != 0 {
		return nil, fmt.Errorf("getsockopt XDP_MMAP_OFFSETS: %w", errno)
	}
	return &offsets, nil
}

// makeRing initializes a ring from an mmap'd region and offsets.
func makeRing(area []byte, off unix.XDPRingOffset, size uint32) ring {
	return ring{
		producer: (*uint32)(unsafe.Pointer(&area[off.Producer])),
		consumer: (*uint32)(unsafe.Pointer(&area[off.Consumer])),
		descs:    unsafe.Slice((*unix.XDPDesc)(unsafe.Pointer(&area[off.Desc])), size),
		mask:     size - 1,
	}
}
