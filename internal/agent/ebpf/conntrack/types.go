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

package conntrack

// DefaultMaxEntries is the default maximum number of entries in the LRU
// conntrack table. The kernel LRU eviction ensures bounded memory usage.
const DefaultMaxEntries uint32 = 65536

// Connection states matching the C constants in bpf/conntrack.c.
const (
	StateSynSent     uint8 = 0
	StateEstablished uint8 = 1
	StateFinWait     uint8 = 2
	StateClosed      uint8 = 3
)

// CTKey is the 5-tuple identifying a connection flow.
// It matches the C struct ct_key layout (16 bytes, padded).
type CTKey struct {
	SrcIP   [4]byte // source IPv4 address (network byte order)
	DstIP   [4]byte // destination IPv4 address (network byte order)
	SrcPort uint16  // source port (network byte order)
	DstPort uint16  // destination port (network byte order)
	Proto   uint8   // IPPROTO_TCP (6) or IPPROTO_UDP (17)
	Pad     [3]byte // padding to 16 bytes
}

// CTEntry is the conntrack entry storing pinned backend and metadata.
// It matches the C struct ct_entry layout.
type CTEntry struct {
	BackendIP   [4]byte // resolved backend IPv4 (network byte order)
	BackendPort uint16  // resolved backend port (network byte order)
	State       uint8   // connection state (StateSynSent, etc.)
	Pad         uint8
	Timestamp   uint64 // last packet timestamp (ktime_ns)
	RxBytes     uint64 // bytes received from client
	TxBytes     uint64 // bytes sent to client
}
