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
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
)

var (
	errCIDR             = errors.New("CIDR")
	errInvalidIPAddress = errors.New("invalid IP address")
	errNotAnIPv4Address = errors.New("not an IPv4 address")
	errInvalidIP        = errors.New("invalid IP")
	errNotIPv4          = errors.New("not IPv4")
)

// safeIntToUint32 converts int to uint32 with bounds checking.
func safeIntToUint32(v int) uint32 {
	if v < 0 {
		return 0
	}
	if v > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v) //nolint:gosec // G115: bounds checked above
}

// LPMTrieKey4 is the key format for a BPF_MAP_TYPE_LPM_TRIE with IPv4 prefixes.
type LPMTrieKey4 struct {
	Prefixlen uint32
	Addr      [4]byte
}

// LPMTrieKey6 is the key format for a BPF_MAP_TYPE_LPM_TRIE with IPv6 prefixes.
type LPMTrieKey6 struct {
	Prefixlen uint32
	Addr      [16]byte
}

// ParseCIDRToLPMKey4 parses a CIDR string into an LPMTrieKey4.
func ParseCIDRToLPMKey4(cidr string) (LPMTrieKey4, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return LPMTrieKey4{}, fmt.Errorf("parsing CIDR %q: %w", cidr, err)
	}
	ip4 := ipNet.IP.To4()
	if ip4 == nil {
		return LPMTrieKey4{}, fmt.Errorf("%w: %q is not IPv4", errCIDR, cidr)
	}
	ones, _ := ipNet.Mask.Size()
	key := LPMTrieKey4{
		Prefixlen: safeIntToUint32(ones),
	}
	copy(key.Addr[:], ip4)
	return key, nil
}

// ParseCIDRToLPMKey6 parses a CIDR string into an LPMTrieKey6.
func ParseCIDRToLPMKey6(cidr string) (LPMTrieKey6, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return LPMTrieKey6{}, fmt.Errorf("parsing CIDR %q: %w", cidr, err)
	}
	ip6 := ipNet.IP.To16()
	if ip6 == nil {
		return LPMTrieKey6{}, fmt.Errorf("%w: %q cannot be converted to IPv6", errCIDR, cidr)
	}
	ones, _ := ipNet.Mask.Size()
	key := LPMTrieKey6{
		Prefixlen: safeIntToUint32(ones),
	}
	copy(key.Addr[:], ip6)
	return key, nil
}

// IPPortKey is a generic BPF map key combining an IPv4 address and port.
type IPPortKey struct {
	Addr [4]byte
	Port uint16
	_    uint16
}

// NewIPPortKey creates an IPPortKey from an IP string and port number.
func NewIPPortKey(ip string, port uint16) (IPPortKey, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return IPPortKey{}, fmt.Errorf("%w: %s", errInvalidIPAddress, ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return IPPortKey{}, fmt.Errorf("%w: %s", errNotAnIPv4Address, ip)
	}
	key := IPPortKey{
		Port: port,
	}
	copy(key.Addr[:], ip4)
	return key, nil
}

// IPPortProtoKey extends IPPortKey with a protocol field for L4 lookups.
type IPPortProtoKey struct {
	Addr  [4]byte
	Port  uint16
	Proto uint8
	_     uint8
}

// NewIPPortProtoKey creates a key from IP, port, and protocol number.
func NewIPPortProtoKey(ip string, port uint16, proto uint8) (IPPortProtoKey, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return IPPortProtoKey{}, fmt.Errorf("%w: %s", errInvalidIPAddress, ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return IPPortProtoKey{}, fmt.Errorf("%w: %s", errNotAnIPv4Address, ip)
	}
	key := IPPortProtoKey{
		Port:  port,
		Proto: proto,
	}
	copy(key.Addr[:], ip4)
	return key, nil
}

// IPv4ToBytes converts an IPv4 address string to a 4-byte array.
func IPv4ToBytes(ip string) ([4]byte, error) {
	var addr [4]byte
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return addr, fmt.Errorf("%w: %s", errInvalidIP, ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return addr, fmt.Errorf("%w: %s", errNotIPv4, ip)
	}
	copy(addr[:], ip4)
	return addr, nil
}

// PortToNetworkOrder converts a uint16 port to network byte order.
func PortToNetworkOrder(port uint16) uint16 {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], port)
	return binary.NativeEndian.Uint16(buf[:])
}
