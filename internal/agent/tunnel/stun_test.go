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

package tunnel

import (
	"net"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewSTUNDiscoverer_DefaultServers(t *testing.T) {
	d := NewSTUNDiscoverer(nil, nil)
	if len(d.servers) != len(defaultSTUNServers) {
		t.Fatalf("expected %d default servers, got %d", len(defaultSTUNServers), len(d.servers))
	}
	for i, s := range d.servers {
		if s != defaultSTUNServers[i] {
			t.Errorf("server[%d] = %q, want %q", i, s, defaultSTUNServers[i])
		}
	}
}

func TestNewSTUNDiscoverer_CustomServers(t *testing.T) {
	custom := []string{"stun.example.com:3478", "stun2.example.com:3478"}
	d := NewSTUNDiscoverer(custom, zap.NewNop())
	if len(d.servers) != 2 {
		t.Fatalf("expected 2 custom servers, got %d", len(d.servers))
	}
	for i, s := range custom {
		if d.servers[i] != s {
			t.Errorf("servers[%d] = %q, want %q", i, d.servers[i], s)
		}
	}
}

func TestNewSTUNDiscoverer_EmptyServers(t *testing.T) {
	d := NewSTUNDiscoverer([]string{}, nil)
	if len(d.servers) != len(defaultSTUNServers) {
		t.Fatalf("empty slice should use defaults, got %d servers", len(d.servers))
	}
}

func TestNewSTUNDiscoverer_NilLogger(t *testing.T) {
	d := NewSTUNDiscoverer(nil, nil)
	if d.logger == nil {
		t.Fatal("logger should not be nil when constructed with nil logger")
	}
}

func TestSTUNDiscoverer_ClearCache(t *testing.T) {
	d := NewSTUNDiscoverer(nil, zap.NewNop())

	// Manually set cache to simulate a previous discovery.
	d.mu.Lock()
	d.cachedAddr = &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 5678}
	d.cachedAt = time.Now()
	d.mu.Unlock()

	// Verify cache is set.
	d.mu.RLock()
	if d.cachedAddr == nil {
		t.Fatal("cache should be set before ClearCache")
	}
	d.mu.RUnlock()

	d.ClearCache()

	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.cachedAddr != nil {
		t.Error("cached address should be nil after ClearCache")
	}
	if !d.cachedAt.IsZero() {
		t.Error("cached time should be zero after ClearCache")
	}
}

func TestSTUNDiscoverer_CacheHit(t *testing.T) {
	d := NewSTUNDiscoverer([]string{"invalid:0"}, zap.NewNop())

	expected := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 9999}
	d.mu.Lock()
	d.cachedAddr = expected
	d.cachedAt = time.Now()
	d.mu.Unlock()

	// Discover should return cached value even though the server is invalid.
	addr, err := d.Discover()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !addr.IP.Equal(expected.IP) || addr.Port != expected.Port {
		t.Errorf("got %v, want %v", addr, expected)
	}
}

func TestSTUNDiscoverer_CacheExpiry(t *testing.T) {
	d := NewSTUNDiscoverer([]string{"invalid:0"}, zap.NewNop())

	// Set cache with an expired timestamp.
	d.mu.Lock()
	d.cachedAddr = &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 9999}
	d.cachedAt = time.Now().Add(-stunCacheTTL - time.Second)
	d.mu.Unlock()

	// Discover should fail because the cache is expired and the server is invalid.
	_, err := d.Discover()
	if err == nil {
		t.Fatal("expected error when cache expired and servers unreachable")
	}
}

func TestSTUNDiscoverer_CacheReturnsCopy(t *testing.T) {
	d := NewSTUNDiscoverer([]string{"invalid:0"}, zap.NewNop())

	original := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 9999}
	d.mu.Lock()
	d.cachedAddr = original
	d.cachedAt = time.Now()
	d.mu.Unlock()

	addr, err := d.Discover()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Modifying the returned address should not affect the cache.
	addr.Port = 1111
	d.mu.RLock()
	if d.cachedAddr.Port != 9999 {
		t.Error("modifying returned address should not affect cache")
	}
	d.mu.RUnlock()
}
