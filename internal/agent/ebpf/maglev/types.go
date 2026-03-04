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

package maglev

// DefaultTableSize is the default Maglev lookup table size. This must be
// a prime number. 16381 provides a good balance between memory usage and
// distribution quality for typical deployment sizes.
const DefaultTableSize uint32 = 16381

// BackendKey is the BPF map key for the backend hash map.
// It matches the C struct backend_key layout.
type BackendKey struct {
	ID uint32
}

// BackendValue is the BPF map value for the backend hash map.
// It matches the C struct backend_value layout.
type BackendValue struct {
	IP   [4]byte // IPv4 address in network byte order
	Port uint16  // port in network byte order
	Pad  uint16
}

// Entry is a single slot in the Maglev lookup table.
// It matches the C struct maglev_entry layout.
type Entry struct {
	BackendID uint32
}

// Backend describes an upstream endpoint for the Maglev table.
// This is the Go-friendly representation used by callers.
type Backend struct {
	ID   uint32 // unique backend identifier
	Addr string // IPv4 address string (e.g. "10.0.0.1")
	Port uint16 // port in host byte order
}
