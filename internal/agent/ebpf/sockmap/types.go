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

package sockmap

import (
	"fmt"
	"net"
)

// SockKey is the BPF SOCKHASH key identifying a socket pair. The struct
// layout matches the C struct sock_key used by the BPF sock_ops and
// sk_msg programs.
type SockKey struct {
	SrcIP   [4]byte
	DstIP   [4]byte
	SrcPort uint32
	DstPort uint32
	Family  uint32
}

// EndpointKey identifies a same-node endpoint eligible for SOCKMAP bypass.
// It is the key for the BPF endpoint_map hash map.
type EndpointKey struct {
	Addr [4]byte
	Port uint16
	Pad  uint16 // padding for 4-byte alignment
}

// EndpointValue is the value stored in the endpoint_map. A non-zero
// Eligible field indicates that the endpoint is local to this node
// and eligible for SOCKMAP-based socket redirection.
type EndpointValue struct {
	Eligible uint32
}

// NewEndpointKey creates an EndpointKey from an IP string and port number.
func NewEndpointKey(ip string, port uint16) (EndpointKey, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return EndpointKey{}, fmt.Errorf("invalid IP address: %s", ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return EndpointKey{}, fmt.Errorf("not an IPv4 address: %s", ip)
	}
	key := EndpointKey{
		Port: port,
	}
	copy(key.Addr[:], ip4)
	return key, nil
}

// StatsKey is the key for the BPF stats_map per-CPU array.
const (
	// StatsKeyRedirected is the array index for the "redirected" counter.
	StatsKeyRedirected uint32 = 0
	// StatsKeyFallback is the array index for the "fallback" counter.
	StatsKeyFallback uint32 = 1
)
