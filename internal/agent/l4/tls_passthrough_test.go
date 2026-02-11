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

package l4

import (
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestExtractSNI_ParseClientHelloSNI(t *testing.T) {
	// This is a minimal TLS ClientHello with SNI extension for "example.com"
	// Constructed manually for testing the SNI parser
	clientHello := buildTestClientHello("example.com")

	sni, err := parseClientHelloSNI(clientHello)
	if err != nil {
		t.Fatalf("Failed to parse ClientHello SNI: %v", err)
	}

	if sni != "example.com" {
		t.Errorf("Expected SNI 'example.com', got %q", sni)
	}
}

func TestExtractSNI_ParseSNIExtension(t *testing.T) {
	hostname := "test.example.com"
	// Build SNI extension data
	// Server Name List: type(1) + length(2) + hostname
	sniData := []byte{
		0x00, byte(len(hostname) + 3), // list length
		0x00,                      // host name type
		0x00, byte(len(hostname)), // hostname length
	}
	sniData = append(sniData, []byte(hostname)...)

	result, err := parseSNIExtension(sniData)
	if err != nil {
		t.Fatalf("Failed to parse SNI extension: %v", err)
	}

	if result != hostname {
		t.Errorf("Expected %q, got %q", hostname, result)
	}
}

func TestExtractSNI_ParseSNIExtension_Empty(t *testing.T) {
	_, err := parseSNIExtension([]byte{})
	if err == nil {
		t.Error("Expected error for empty SNI extension")
	}
}

func TestTLSPassthrough_LookupRoute(t *testing.T) {
	logger := zaptest.NewLogger(t)

	routes := map[string]*TLSRoute{
		"example.com": {
			Hostname:    "example.com",
			Backends:    []*pb.Endpoint{{Address: "10.0.0.1", Port: 443, Ready: true}},
			BackendName: "example-backend",
		},
		"*.wildcard.com": {
			Hostname:    "*.wildcard.com",
			Backends:    []*pb.Endpoint{{Address: "10.0.0.2", Port: 443, Ready: true}},
			BackendName: "wildcard-backend",
		},
	}

	defaultRoute := &TLSRoute{
		Hostname:    "*",
		Backends:    []*pb.Endpoint{{Address: "10.0.0.3", Port: 443, Ready: true}},
		BackendName: "default-backend",
	}

	proxy := NewTLSPassthrough(TLSPassthroughConfig{
		ListenerName:   "test-tls",
		Routes:         routes,
		DefaultBackend: defaultRoute,
	}, logger)

	tests := []struct {
		name          string
		sni           string
		expectBackend string
	}{
		{
			name:          "exact match",
			sni:           "example.com",
			expectBackend: "example-backend",
		},
		{
			name:          "wildcard match",
			sni:           "sub.wildcard.com",
			expectBackend: "wildcard-backend",
		},
		{
			name:          "default route",
			sni:           "unknown.com",
			expectBackend: "default-backend",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			route := proxy.lookupRoute(tc.sni)
			if route == nil {
				t.Fatal("Expected route, got nil")
			}
			if route.BackendName != tc.expectBackend {
				t.Errorf("Expected backend %s, got %s", tc.expectBackend, route.BackendName)
			}
		})
	}
}

func TestTLSPassthrough_LookupRoute_NoDefault(t *testing.T) {
	logger := zaptest.NewLogger(t)

	routes := map[string]*TLSRoute{
		"example.com": {
			Hostname:    "example.com",
			Backends:    []*pb.Endpoint{{Address: "10.0.0.1", Port: 443, Ready: true}},
			BackendName: "example-backend",
		},
	}

	proxy := NewTLSPassthrough(TLSPassthroughConfig{
		ListenerName: "test-tls-no-default",
		Routes:       routes,
	}, logger)

	route := proxy.lookupRoute("unknown.com")
	if route != nil {
		t.Error("Expected nil route for unknown hostname without default")
	}
}

func TestTLSPassthrough_PickBackend(t *testing.T) {
	logger := zaptest.NewLogger(t)

	route := &TLSRoute{
		Hostname: "example.com",
		Backends: []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 443, Ready: true},
			{Address: "10.0.0.2", Port: 443, Ready: true},
		},
		BackendName: "test-backend",
	}

	proxy := NewTLSPassthrough(TLSPassthroughConfig{
		ListenerName: "test-pick",
	}, logger)

	// Pick multiple backends to verify round-robin
	seen := make(map[string]int)
	for i := 0; i < 10; i++ {
		ep := proxy.pickBackend(route)
		if ep == nil {
			t.Fatal("pickBackend returned nil")
		}
		seen[ep.Address]++
	}

	if len(seen) < 2 {
		t.Error("Expected round-robin to distribute across backends")
	}
}

func TestTLSPassthrough_PickBackend_NoReady(t *testing.T) {
	logger := zaptest.NewLogger(t)

	route := &TLSRoute{
		Hostname: "example.com",
		Backends: []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 443, Ready: false},
		},
		BackendName: "test-backend",
	}

	proxy := NewTLSPassthrough(TLSPassthroughConfig{
		ListenerName: "test-no-ready",
	}, logger)

	ep := proxy.pickBackend(route)
	if ep != nil {
		t.Errorf("Expected nil for no ready backends, got %v", ep)
	}
}

func TestTLSPassthrough_UpdateRoutes(t *testing.T) {
	logger := zaptest.NewLogger(t)

	proxy := NewTLSPassthrough(TLSPassthroughConfig{
		ListenerName: "test-update",
		Routes: map[string]*TLSRoute{
			"old.com": {
				Hostname:    "old.com",
				Backends:    []*pb.Endpoint{{Address: "10.0.0.1", Port: 443, Ready: true}},
				BackendName: "old-backend",
			},
		},
	}, logger)

	route := proxy.lookupRoute("old.com")
	if route == nil || route.BackendName != "old-backend" {
		t.Fatal("Expected old route")
	}

	// Update routes
	newRoutes := map[string]*TLSRoute{
		"new.com": {
			Hostname:    "new.com",
			Backends:    []*pb.Endpoint{{Address: "10.0.0.2", Port: 443, Ready: true}},
			BackendName: "new-backend",
		},
	}
	proxy.UpdateRoutes(newRoutes, nil)

	route = proxy.lookupRoute("old.com")
	if route != nil {
		t.Error("Old route should be gone after update")
	}

	route = proxy.lookupRoute("new.com")
	if route == nil || route.BackendName != "new-backend" {
		t.Fatal("Expected new route after update")
	}
}

func TestTLSPassthrough_Drain(t *testing.T) {
	logger := zaptest.NewLogger(t)

	proxy := NewTLSPassthrough(TLSPassthroughConfig{
		ListenerName: "test-drain",
	}, logger)

	if proxy.IsDraining() {
		t.Error("Should not be draining initially")
	}

	proxy.Drain(100 * time.Millisecond)

	if !proxy.IsDraining() {
		t.Error("Should be draining after Drain()")
	}
}

func TestTLSPassthrough_DefaultConfig(t *testing.T) {
	logger := zap.NewNop()

	proxy := NewTLSPassthrough(TLSPassthroughConfig{
		ListenerName: "test-defaults",
	}, logger)

	if proxy.config.SNIReadTimeout != DefaultSNIReadTimeout {
		t.Errorf("Expected default SNI read timeout %v, got %v",
			DefaultSNIReadTimeout, proxy.config.SNIReadTimeout)
	}
	if proxy.config.ConnectTimeout != DefaultTCPConnectTimeout {
		t.Errorf("Expected default connect timeout %v, got %v",
			DefaultTCPConnectTimeout, proxy.config.ConnectTimeout)
	}
}

func TestGetReadyEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	ready := getReadyEndpoints(endpoints)
	if len(ready) != 2 {
		t.Errorf("Expected 2 ready endpoints, got %d", len(ready))
	}

	for _, ep := range ready {
		if !ep.Ready {
			t.Errorf("Non-ready endpoint in ready list: %s", ep.Address)
		}
	}
}

// buildTestClientHello constructs a minimal TLS ClientHello handshake message with SNI
func buildTestClientHello(hostname string) []byte {
	// SNI extension data
	sniExtData := []byte{
		0x00, byte(len(hostname) + 3), // list length
		0x00,                      // host name type
		0x00, byte(len(hostname)), // hostname length
	}
	sniExtData = append(sniExtData, []byte(hostname)...)

	// Extensions: SNI extension (type 0x0000)
	extensions := []byte{
		0x00, 0x00, // Extension type: SNI
		0x00, byte(len(sniExtData)), // Extension length
	}
	extensions = append(extensions, sniExtData...)

	// Extensions length
	extensionsLen := len(extensions)

	// Build ClientHello body
	body := []byte{
		0x03, 0x03, // Client version: TLS 1.2
	}
	// 32 bytes random
	random := make([]byte, 32)
	body = append(body, random...)

	// Session ID (empty)
	body = append(body, 0x00) // session ID length = 0

	// Cipher suites (2 bytes length + 2 bytes for one suite)
	body = append(body, 0x00, 0x02, 0x00, 0x2f) // TLS_RSA_WITH_AES_128_CBC_SHA

	// Compression methods (1 byte length + 1 byte for null)
	body = append(body, 0x01, 0x00)

	// Extensions length (2 bytes)
	body = append(body, byte(extensionsLen>>8), byte(extensionsLen))
	body = append(body, extensions...)

	// Build handshake message
	handshakeLen := len(body)
	handshake := []byte{
		TLSClientHelloType,        // Handshake type: ClientHello
		0x00,                      // Length high byte
		byte(handshakeLen >> 8),   // Length mid byte
		byte(handshakeLen & 0xff), // Length low byte
	}
	handshake = append(handshake, body...)

	return handshake
}
