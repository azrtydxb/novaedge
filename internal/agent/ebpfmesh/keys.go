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

package ebpfmesh

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

var (
	errInvalidIP = errors.New("invalid IP")
	errNotIPv4   = errors.New("not IPv4")
)

// meshSvcKey matches the C struct mesh_svc_key layout.
type meshSvcKey struct {
	Addr [4]byte
	Port uint16
	Pad  uint16
}

// meshSvcValue matches the C struct mesh_svc_value layout.
type meshSvcValue struct {
	RedirectPort uint32
}

// makeServiceKey constructs a BPF map key from an IP string and port.
func makeServiceKey(ip string, port int32) (meshSvcKey, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return meshSvcKey{}, fmt.Errorf("%w: %s", errInvalidIP, ip)
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return meshSvcKey{}, fmt.Errorf("%w: %s", errNotIPv4, ip)
	}
	key := meshSvcKey{
		Port: htons(uint16(port)),
	}
	copy(key.Addr[:], ip4)
	return key, nil
}

// htons converts a uint16 from host to network byte order.
func htons(v uint16) uint16 {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	return binary.NativeEndian.Uint16(buf[:])
}
