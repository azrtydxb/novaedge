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

package router

import (
	"testing"
)

func TestFormatEndpointKey(t *testing.T) {
	tests := []struct {
		address  string
		port     int32
		expected string
	}{
		{"10.0.0.1", 8080, "10.0.0.1:8080"},
		{"192.168.1.1", 443, "192.168.1.1:443"},
		{"localhost", 3000, "localhost:3000"},
		{"::1", 8080, "::1:8080"},
		{"2001:db8::1", 9000, "2001:db8::1:9000"},
		{"", 80, ":80"},
		{"10.0.0.1", 0, "10.0.0.1:0"},
		{"10.0.0.1", 65535, "10.0.0.1:65535"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := formatEndpointKey(tt.address, tt.port)
			if result != tt.expected {
				t.Errorf("formatEndpointKey(%q, %d) = %q, want %q", tt.address, tt.port, result, tt.expected)
			}
		})
	}
}

func TestFormatEndpointKey_Concurrent(t *testing.T) {
	done := make(chan bool)

	for i := int32(0); i < 100; i++ {
		go func(id int32) {
			for j := int32(0); j < 100; j++ {
				address := "10.0.0.1"
				port := id*100 + j
				result := formatEndpointKey(address, port)
				if result == "" {
					t.Error("formatEndpointKey returned empty string")
				}
			}
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}
