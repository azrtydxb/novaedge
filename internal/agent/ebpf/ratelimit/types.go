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

package ratelimit

// RateLimitKey is the BPF map key for per-IP rate limiting.
// It uses a 16-byte address field to support both IPv4 (mapped to
// ::ffff:x.x.x.x) and IPv6 addresses.
type RateLimitKey struct {
	IP [16]byte // IPv4-mapped or native IPv6
}

// RateLimitValue is the per-CPU BPF map value that tracks token bucket
// state for a single source IP on a single CPU core.
type RateLimitValue struct {
	Tokens       uint64 // current token count
	LastRefillNS uint64 // last refill timestamp in nanoseconds (ktime)
}

// RateLimitConfig is the BPF configuration map value (single entry at
// index 0). It controls the token bucket parameters used by the BPF
// program.
type RateLimitConfig struct {
	Rate     uint64 // tokens per second
	Burst    uint64 // maximum bucket size (token capacity)
	WindowNS uint64 // refill window in nanoseconds
}

// RateLimitStats holds aggregated allow/deny counters read from the
// BPF stats map.
type RateLimitStats struct {
	Allowed uint64
	Denied  uint64
}
