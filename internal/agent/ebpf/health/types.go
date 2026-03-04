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

// Package health provides eBPF-based backend health monitoring via BPF map counters.
package health

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

var (
	errInvalidIPAddress = errors.New("invalid IP address")
	errNotAnIPv4Address = errors.New("not an IPv4 address")
)

// BackendKey identifies a backend by its IP address and port. The
// struct layout is 8 bytes total (4-byte IP + 2-byte port + 2-byte
// padding) to match the BPF map key format.
type BackendKey struct {
	Addr [4]byte
	Port uint16
	_    uint16 // padding for 4-byte alignment
}

// NewBackendKey creates a BackendKey from an IP string and port.
func NewBackendKey(ip string, port uint16) (BackendKey, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return BackendKey{}, fmt.Errorf("%w: %s", errInvalidIPAddress, ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return BackendKey{}, fmt.Errorf("%w: %s", errNotAnIPv4Address, ip)
	}
	key := BackendKey{
		Port: port,
	}
	copy(key.Addr[:], ip4)
	return key, nil
}

// String returns a human-readable representation of the backend key.
func (k BackendKey) String() string {
	ip := net.IPv4(k.Addr[0], k.Addr[1], k.Addr[2], k.Addr[3])
	return net.JoinHostPort(ip.String(), fmt.Sprint(k.Port))
}

// BackendHealth is the per-CPU BPF map value that tracks connection
// health counters for a single backend on a single CPU core.
type BackendHealth struct {
	TotalConns    uint64 // total connections observed
	FailedConns   uint64 // TCP RST, connection refused
	TimeoutConns  uint64 // SYN timeout
	SuccessConns  uint64 // successful connections
	LastSuccessNS uint64 // timestamp of last success (ktime_ns)
	LastFailureNS uint64 // timestamp of last failure (ktime_ns)
	TotalRTTNS    uint64 // sum of SYN->SYN-ACK RTT in nanoseconds
}

// AggregatedHealth contains node-wide health data for a single backend,
// computed by summing per-CPU counters and calculating derived metrics.
type AggregatedHealth struct {
	// Counters (summed across all CPUs).
	TotalConns   uint64
	FailedConns  uint64
	TimeoutConns uint64
	SuccessConns uint64

	// Timestamps (max across CPUs).
	LastSuccessNS uint64
	LastFailureNS uint64

	// Derived metrics.
	FailureRate float64 // (failed + timeout) / total
	AvgRTTNS    uint64  // totalRTT / successConns

	// Delta counters (change since last poll).
	DeltaTotal   uint64
	DeltaFailed  uint64
	DeltaTimeout uint64
	DeltaSuccess uint64
}

// htons converts a uint16 from host to network byte order.
func htons(v uint16) uint16 {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	return binary.NativeEndian.Uint16(buf[:])
}
