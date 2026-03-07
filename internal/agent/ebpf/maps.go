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
	"encoding/binary"
	"fmt"
	"net"

	"github.com/cilium/ebpf"
)

// LPMTrieKey4 is the key format for a BPF_MAP_TYPE_LPM_TRIE with IPv4
// prefixes. The struct layout matches the kernel's bpf_lpm_trie_key.
type LPMTrieKey4 struct {
	Prefixlen uint32
	Addr      [4]byte
}

// LPMTrieKey6 is the key format for a BPF_MAP_TYPE_LPM_TRIE with IPv6
// prefixes.
type LPMTrieKey6 struct {
	Prefixlen uint32
	Addr      [16]byte
}

// ParseCIDRToLPMKey4 parses a CIDR string (e.g. "10.0.0.0/24") into an
// LPMTrieKey4 suitable for BPF map operations.
func ParseCIDRToLPMKey4(cidr string) (LPMTrieKey4, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return LPMTrieKey4{}, fmt.Errorf("parsing CIDR %q: %w", cidr, err)
	}
	ip4 := ipNet.IP.To4()
	if ip4 == nil {
		return LPMTrieKey4{}, fmt.Errorf("CIDR %q is not IPv4", cidr)
	}
	ones, _ := ipNet.Mask.Size()
	key := LPMTrieKey4{
		Prefixlen: uint32(ones),
	}
	copy(key.Addr[:], ip4)
	return key, nil
}

// ParseCIDRToLPMKey6 parses a CIDR string into an LPMTrieKey6 for IPv6
// BPF map operations.
func ParseCIDRToLPMKey6(cidr string) (LPMTrieKey6, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return LPMTrieKey6{}, fmt.Errorf("parsing CIDR %q: %w", cidr, err)
	}
	ip6 := ipNet.IP.To16()
	if ip6 == nil {
		return LPMTrieKey6{}, fmt.Errorf("CIDR %q cannot be converted to IPv6", cidr)
	}
	ones, _ := ipNet.Mask.Size()
	key := LPMTrieKey6{
		Prefixlen: uint32(ones),
	}
	copy(key.Addr[:], ip6)
	return key, nil
}

// UpdateLPMTrie inserts or updates an entry in a BPF LPM trie map. The
// cidr parameter is parsed to construct the trie key; value is written as-is.
func UpdateLPMTrie(m *ebpf.Map, cidr string, value []byte) error {
	key, err := ParseCIDRToLPMKey4(cidr)
	if err != nil {
		return err
	}
	if err := m.Update(key, value, ebpf.UpdateAny); err != nil {
		eBPFMapOpsTotal.WithLabelValues(m.String(), "update_lpm", "error").Inc()
		return fmt.Errorf("updating LPM trie for %s: %w", cidr, err)
	}
	eBPFMapOpsTotal.WithLabelValues(m.String(), "update_lpm", "ok").Inc()
	return nil
}

// DeleteLPMTrie removes an entry from a BPF LPM trie map.
func DeleteLPMTrie(m *ebpf.Map, cidr string) error {
	key, err := ParseCIDRToLPMKey4(cidr)
	if err != nil {
		return err
	}
	if err := m.Delete(key); err != nil {
		eBPFMapOpsTotal.WithLabelValues(m.String(), "delete_lpm", "error").Inc()
		return fmt.Errorf("deleting LPM trie entry %s: %w", cidr, err)
	}
	eBPFMapOpsTotal.WithLabelValues(m.String(), "delete_lpm", "ok").Inc()
	return nil
}

// SyncHashMap reconciles a BPF hash map to match the desired state. Keys
// present in desired but not in the map are added; keys in the map but not
// in desired are removed. Both key and value must be fixed-size types
// suitable for BPF map operations.
func SyncHashMap[K comparable, V any](m *ebpf.Map, desired map[K]V) error {
	// Collect existing keys for deletion pass.
	existing := make(map[K]struct{})
	var cursor K
	var val V
	iter := m.Iterate()
	for iter.Next(&cursor, &val) {
		keyCopy := cursor
		existing[keyCopy] = struct{}{}
	}
	// iter.Err() may return ErrIterationAborted for concurrent modification;
	// we treat this as best-effort.

	// Delete stale entries.
	for k := range existing {
		if _, want := desired[k]; !want {
			if err := m.Delete(k); err != nil {
				eBPFMapOpsTotal.WithLabelValues(m.String(), "delete", "error").Inc()
				return fmt.Errorf("deleting stale key from BPF map: %w", err)
			}
			eBPFMapOpsTotal.WithLabelValues(m.String(), "delete", "ok").Inc()
		}
	}

	// Upsert desired entries.
	for k, v := range desired {
		if err := m.Update(k, v, ebpf.UpdateAny); err != nil {
			eBPFMapOpsTotal.WithLabelValues(m.String(), "update", "error").Inc()
			return fmt.Errorf("updating BPF map: %w", err)
		}
		eBPFMapOpsTotal.WithLabelValues(m.String(), "update", "ok").Inc()
	}

	return nil
}

// IPPortKey is a generic BPF map key combining an IPv4 address and port.
// It matches the C struct layout used by mesh redirect programs.
type IPPortKey struct {
	Addr [4]byte
	Port uint16
	_    uint16 // padding for 4-byte alignment
}

// NewIPPortKey creates an IPPortKey from an IP string and port number.
func NewIPPortKey(ip string, port uint16) (IPPortKey, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return IPPortKey{}, fmt.Errorf("invalid IP address: %s", ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return IPPortKey{}, fmt.Errorf("not an IPv4 address: %s", ip)
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
	_     uint8 // padding
}

// NewIPPortProtoKey creates a key from IP, port, and protocol number.
func NewIPPortProtoKey(ip string, port uint16, proto uint8) (IPPortProtoKey, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return IPPortProtoKey{}, fmt.Errorf("invalid IP address: %s", ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return IPPortProtoKey{}, fmt.Errorf("not an IPv4 address: %s", ip)
	}
	key := IPPortProtoKey{
		Port:  port,
		Proto: proto,
	}
	copy(key.Addr[:], ip4)
	return key, nil
}

// IPv4ToBytes converts an IPv4 address string to a 4-byte array in network
// byte order.
func IPv4ToBytes(ip string) ([4]byte, error) {
	var addr [4]byte
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return addr, fmt.Errorf("invalid IP: %s", ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return addr, fmt.Errorf("not IPv4: %s", ip)
	}
	copy(addr[:], ip4)
	return addr, nil
}

// PortToNetworkOrder converts a uint16 port to network byte order (big-endian).
func PortToNetworkOrder(port uint16) uint16 {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], port)
	return binary.NativeEndian.Uint16(buf[:])
}
