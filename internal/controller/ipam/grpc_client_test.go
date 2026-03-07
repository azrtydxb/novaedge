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

package ipam

import (
	"testing"
)

func TestIPToCIDR(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"10.0.0.1", "10.0.0.1/32"},
		{"192.168.1.100", "192.168.1.100/32"},
		{"2001:db8::1", "2001:db8::1/128"},
		{"::1", "::1/128"},
		{"10.0.0.1/32", "10.0.0.1/32"},         // already CIDR
		{"2001:db8::1/128", "2001:db8::1/128"}, // already CIDR
		{"10.0.0.1/24", "10.0.0.1/24"},         // non-host CIDR preserved
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ipToCIDR(tt.input)
			if result != tt.expected {
				t.Errorf("ipToCIDR(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGRPCAllocator_ImplementsClient(t *testing.T) {
	// Compile-time check (via var _ above), but also verify at test time.
	var _ Client = (*GRPCAllocator)(nil)
}
