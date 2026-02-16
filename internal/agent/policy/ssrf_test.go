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

package policy

import (
	"net"
	"testing"
	"time"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		private bool
	}{
		// RFC 1918 – 10.0.0.0/8
		{"10.0.0.0 start of range", "10.0.0.0", true},
		{"10.0.0.1", "10.0.0.1", true},
		{"10.255.255.255 end of range", "10.255.255.255", true},

		// RFC 1918 – 172.16.0.0/12
		{"172.16.0.1", "172.16.0.1", true},
		{"172.31.255.255 end of range", "172.31.255.255", true},
		{"172.15.255.255 just below range", "172.15.255.255", false},
		{"172.32.0.0 just above range", "172.32.0.0", false},

		// RFC 1918 – 192.168.0.0/16
		{"192.168.0.1", "192.168.0.1", true},
		{"192.168.255.255 end of range", "192.168.255.255", true},
		{"192.167.0.1 outside range", "192.167.0.1", false},

		// Loopback – 127.0.0.0/8
		{"127.0.0.1 loopback", "127.0.0.1", true},
		{"127.255.255.255 end of loopback", "127.255.255.255", true},

		// Link-local – 169.254.0.0/16
		{"169.254.0.1 link-local", "169.254.0.1", true},
		{"169.254.169.254 metadata endpoint", "169.254.169.254", true},

		// IPv6 loopback
		{"::1 IPv6 loopback", "::1", true},

		// IPv6 unique local (fc00::/7)
		{"fd00::1 unique local", "fd00::1", true},
		{"fc00::1 unique local", "fc00::1", true},

		// IPv6 link-local (fe80::/10)
		{"fe80::1 IPv6 link-local", "fe80::1", true},

		// Public IPs – should NOT be private
		{"8.8.8.8 Google DNS", "8.8.8.8", false},
		{"1.1.1.1 Cloudflare DNS", "1.1.1.1", false},
		{"93.184.216.34 example.com", "93.184.216.34", false},
		{"203.0.113.1 TEST-NET-3", "203.0.113.1", false},
		{"2001:4860:4860::8888 Google IPv6", "2001:4860:4860::8888", false},

		// Edge cases
		{"0.0.0.0 unspecified", "0.0.0.0", false},
		{"255.255.255.255 broadcast", "255.255.255.255", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.private {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}

func TestPrivateNetworksInitialized(t *testing.T) {
	// Verify that the init() function populated the private networks list
	if len(privateNetworks) == 0 {
		t.Fatal("privateNetworks slice is empty; init() may not have run")
	}

	// We expect exactly 8 CIDR blocks based on the source
	expected := 8
	if len(privateNetworks) != expected {
		t.Errorf("expected %d private network blocks, got %d", expected, len(privateNetworks))
	}
}

func TestNewSSRFProtectedClient(t *testing.T) {
	timeout := 5 * time.Second
	client := NewSSRFProtectedClient(timeout)

	if client == nil {
		t.Fatal("NewSSRFProtectedClient returned nil")
	}
	if client.Timeout != timeout {
		t.Errorf("client timeout = %v, want %v", client.Timeout, timeout)
	}
	if client.Transport == nil {
		t.Error("client transport is nil")
	}
}

func TestNewSSRFProtectedTransport(t *testing.T) {
	transport := NewSSRFProtectedTransport()

	if transport == nil {
		t.Fatal("NewSSRFProtectedTransport returned nil")
	}
	if transport.TLSHandshakeTimeout != 10*time.Second {
		t.Errorf("TLSHandshakeTimeout = %v, want 10s", transport.TLSHandshakeTimeout)
	}
	if transport.DialContext == nil {
		t.Error("DialContext function is nil")
	}
}
