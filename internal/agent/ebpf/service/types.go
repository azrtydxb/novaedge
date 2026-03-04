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

// Package service provides eBPF-based service map management for L4 load balancing.
package service

import (
	"errors"
	"fmt"
	"net"
)

var (
	errInvalidIPAddress = errors.New("invalid IP address")
	errNotAnIPv4Address = errors.New("not an IPv4 address")
)

// Key is the key for the BPF service lookup map. It identifies a
// Kubernetes Service by its ClusterIP, port, and protocol. The struct
// layout is carefully padded to match the C struct used in the BPF program.
type Key struct {
	IP    [4]byte
	Port  uint16
	Proto uint8
	Pad   uint8
}

// Value is the value stored in the BPF service map. It contains
// metadata about the service's backends and mesh configuration.
type Value struct {
	// BackendCount is the number of backends in the associated backend array.
	BackendCount uint32
	// Flags contains bitfield flags for service properties.
	// Bit 0: mesh-enabled
	// Bit 1: mTLS-required
	Flags uint32
}

// BackendInfo describes a single backend endpoint for a service. It is
// stored in a per-service BPF array map indexed by backend position.
type BackendInfo struct {
	IP        [4]byte
	Port      uint16
	Weight    uint16
	Healthy   uint8
	NodeLocal uint8
	Pad       [2]byte
}

const (
	// FlagMeshEnabled indicates the service is opted into the mesh.
	FlagMeshEnabled uint32 = 1 << 0

	// FlagMTLSRequired indicates that mTLS is required for this service.
	FlagMTLSRequired uint32 = 1 << 1
)

// NewKey creates a Key from an IP string, port, and protocol.
// The protocol value uses standard IANA numbers (6=TCP, 17=UDP).
func NewKey(ip string, port uint16, proto uint8) (Key, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return Key{}, fmt.Errorf("%w: %s", errInvalidIPAddress, ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return Key{}, fmt.Errorf("%w: %s", errNotAnIPv4Address, ip)
	}
	key := Key{
		Port:  port,
		Proto: proto,
	}
	copy(key.IP[:], ip4)
	return key, nil
}

// NewBackendInfo creates a BackendInfo from an IP string and port, with
// the given weight and health/locality flags.
func NewBackendInfo(ip string, port uint16, weight uint16, healthy bool, nodeLocal bool) (BackendInfo, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return BackendInfo{}, fmt.Errorf("%w: %s", errInvalidIPAddress, ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return BackendInfo{}, fmt.Errorf("%w: %s", errNotAnIPv4Address, ip)
	}

	info := BackendInfo{
		Port:   port,
		Weight: weight,
	}
	copy(info.IP[:], ip4)

	if healthy {
		info.Healthy = 1
	}
	if nodeLocal {
		info.NodeLocal = 1
	}
	return info, nil
}

// ProtoTCP is the IANA protocol number for TCP.
const ProtoTCP uint8 = 6

// ProtoUDP is the IANA protocol number for UDP.
const ProtoUDP uint8 = 17
