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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testClientIP           = "1.2.3.4"
	testTrustedProxyAddr   = "10.0.0.1:12345"
	testTrustedProxyIP     = "10.0.0.1"
	testSecondTrustedProxy = "10.0.0.2"
	testUntrustedAddr      = "8.8.8.8:12345"
	testInternalAddr       = "10.1.2.3:12345"
	testAltClientIP        = "5.6.7.8"
)

func TestSetGlobalTrustedProxies(t *testing.T) {
	// Clean up after tests
	defer func() {
		trustedProxyCIDRs = nil
	}()

	t.Run("valid CIDR", func(t *testing.T) {
		err := SetGlobalTrustedProxies([]string{"10.0.0.0/8", "172.16.0.0/12"})
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if len(trustedProxyCIDRs) != 2 {
			t.Errorf("Expected 2 trusted proxy CIDRs, got %d", len(trustedProxyCIDRs))
		}
	})

	t.Run("single IP", func(t *testing.T) {
		err := SetGlobalTrustedProxies([]string{"192.168.1.1"})
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if len(trustedProxyCIDRs) != 1 {
			t.Errorf("Expected 1 trusted proxy CIDR, got %d", len(trustedProxyCIDRs))
		}
	})

	t.Run("IPv6 address", func(t *testing.T) {
		err := SetGlobalTrustedProxies([]string{"2001:db8::1"})
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if len(trustedProxyCIDRs) != 1 {
			t.Errorf("Expected 1 trusted proxy CIDR, got %d", len(trustedProxyCIDRs))
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		err := SetGlobalTrustedProxies([]string{"not-an-ip"})
		if err == nil {
			t.Error("Expected error for invalid input")
		}
	})
}

func TestIsGlobalTrustedProxy(t *testing.T) {
	// Setup
	if err := SetGlobalTrustedProxies([]string{"10.0.0.0/8", "192.168.1.1"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		trustedProxyCIDRs = nil
	}()

	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{
			name:     "IP in CIDR range",
			ip:       "10.1.2.3",
			expected: true,
		},
		{
			name:     "exact IP match",
			ip:       "192.168.1.1",
			expected: true,
		},
		{
			name:     "IP not in range",
			ip:       "8.8.8.8",
			expected: false,
		},
		{
			name:     "invalid IP",
			ip:       "not-an-ip",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isGlobalTrustedProxy(tt.ip)
			if result != tt.expected {
				t.Errorf("Expected %v for IP %s, got %v", tt.expected, tt.ip, result)
			}
		})
	}
}

func TestExtractClientIPPackageLevel(t *testing.T) {
	// Setup
	if err := SetGlobalTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		trustedProxyCIDRs = nil
	}()

	t.Run("direct connection no trusted proxies", func(t *testing.T) {
		// Clear trusted proxies
		trustedProxyCIDRs = nil

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "1.2.3.4:12345"
		req.Header.Set("X-Forwarded-For", testAltClientIP)

		ip := extractClientIP(req)
		if ip != testClientIP {
			t.Errorf("Expected 1.2.3.4, got %s", ip)
		}
	})

	t.Run("through trusted proxy with X-Forwarded-For", func(t *testing.T) {
		if err := SetGlobalTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.2")

		ip := extractClientIP(req)
		if ip != testClientIP {
			t.Errorf("Expected 1.2.3.4 (client IP), got %s", ip)
		}
	})

	t.Run("through untrusted proxy ignores X-Forwarded-For", func(t *testing.T) {
		if err := SetGlobalTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testUntrustedAddr
		req.Header.Set("X-Forwarded-For", testClientIP)

		ip := extractClientIP(req)
		if ip != "8.8.8.8" {
			t.Errorf("Expected 8.8.8.8 (untrusted proxy IP), got %s", ip)
		}
	})

	t.Run("X-Real-IP fallback", func(t *testing.T) {
		if err := SetGlobalTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Real-IP", testClientIP)

		ip := extractClientIP(req)
		if ip != testClientIP {
			t.Errorf("Expected 1.2.3.4 (from X-Real-IP), got %s", ip)
		}
	})
}

func TestNewIPAllowListFilter(t *testing.T) {
	t.Run("valid CIDR", func(t *testing.T) {
		filter, err := NewIPAllowListFilter([]string{"10.0.0.0/8", "192.168.0.0/16"})
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if filter == nil {
			t.Fatal("Expected filter, got nil")
		}

		if filter.mode != "allow" {
			t.Errorf("Expected mode 'allow', got %s", filter.mode)
		}

		if len(filter.allowList) != 2 {
			t.Errorf("Expected 2 allow list entries, got %d", len(filter.allowList))
		}
	})

	t.Run("single IP", func(t *testing.T) {
		filter, err := NewIPAllowListFilter([]string{"192.168.1.1"})
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if len(filter.allowList) != 1 {
			t.Errorf("Expected 1 allow list entry, got %d", len(filter.allowList))
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		_, err := NewIPAllowListFilter([]string{"not-an-ip"})
		if err == nil {
			t.Error("Expected error for invalid input")
		}
	})
}

func TestNewIPDenyListFilter(t *testing.T) {
	t.Run("valid CIDR", func(t *testing.T) {
		filter, err := NewIPDenyListFilter([]string{"10.0.0.0/8"})
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if filter == nil {
			t.Fatal("Expected filter, got nil")
		}

		if filter.mode != "deny" {
			t.Errorf("Expected mode 'deny', got %s", filter.mode)
		}

		if len(filter.denyList) != 1 {
			t.Errorf("Expected 1 deny list entry, got %d", len(filter.denyList))
		}
	})

	t.Run("single IP", func(t *testing.T) {
		filter, err := NewIPDenyListFilter([]string{"192.168.1.1"})
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if len(filter.denyList) != 1 {
			t.Errorf("Expected 1 deny list entry, got %d", len(filter.denyList))
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		_, err := NewIPDenyListFilter([]string{"not-an-ip"})
		if err == nil {
			t.Error("Expected error for invalid input")
		}
	})
}

func TestIPFilterAllow(t *testing.T) {
	t.Run("allow list: IP in list", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8", "192.168.1.1"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testInternalAddr

		if !filter.Allow(req) {
			t.Error("Expected IP in allow list to be allowed")
		}
	})

	t.Run("allow list: IP not in list", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testUntrustedAddr

		if filter.Allow(req) {
			t.Error("Expected IP not in allow list to be denied")
		}
	})

	t.Run("deny list: IP in list", func(t *testing.T) {
		filter, _ := NewIPDenyListFilter([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testInternalAddr

		if filter.Allow(req) {
			t.Error("Expected IP in deny list to be blocked")
		}
	})

	t.Run("deny list: IP not in list", func(t *testing.T) {
		filter, _ := NewIPDenyListFilter([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testUntrustedAddr

		if !filter.Allow(req) {
			t.Error("Expected IP not in deny list to be allowed")
		}
	})

	t.Run("invalid IP returns false", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "invalid-ip"

		if filter.Allow(req) {
			t.Error("Expected invalid IP to be denied")
		}
	})
}

func TestHandleIPFilter(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	t.Run("allow list: allowed IP", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})
		middleware := HandleIPFilter(filter)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testInternalAddr
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("allow list: blocked IP", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})
		middleware := HandleIPFilter(filter)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testUntrustedAddr
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
		}
	})

	t.Run("deny list: allowed IP", func(t *testing.T) {
		filter, _ := NewIPDenyListFilter([]string{"10.0.0.0/8"})
		middleware := HandleIPFilter(filter)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testUntrustedAddr
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("deny list: blocked IP", func(t *testing.T) {
		filter, _ := NewIPDenyListFilter([]string{"10.0.0.0/8"})
		middleware := HandleIPFilter(filter)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testInternalAddr
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
		}
	})
}

func TestIPFilterExtractClientIP(t *testing.T) {
	t.Run("direct connection no trusted proxies", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "1.2.3.4:12345"
		req.Header.Set("X-Forwarded-For", testAltClientIP)

		ip := filter.extractClientIP(req)
		if ip != testClientIP {
			t.Errorf("Expected 1.2.3.4, got %s", ip)
		}
	})

	t.Run("through trusted proxy with X-Forwarded-For", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})
		if err := filter.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.2")

		ip := filter.extractClientIP(req)
		if ip != testClientIP {
			t.Errorf("Expected 1.2.3.4 (client IP), got %s", ip)
		}
	})

	t.Run("through untrusted proxy ignores X-Forwarded-For", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})
		if err := filter.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testUntrustedAddr
		req.Header.Set("X-Forwarded-For", testClientIP)

		ip := filter.extractClientIP(req)
		if ip != "8.8.8.8" {
			t.Errorf("Expected 8.8.8.8 (untrusted proxy IP), got %s", ip)
		}
	})

	t.Run("X-Real-IP fallback", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})
		if err := filter.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Real-IP", testClientIP)

		ip := filter.extractClientIP(req)
		if ip != testClientIP {
			t.Errorf("Expected 1.2.3.4 (from X-Real-IP), got %s", ip)
		}
	})

	t.Run("X-Forwarded-For with all trusted proxies", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})
		if err := filter.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", "10.0.0.2, 10.0.0.3, 10.0.0.4")

		ip := filter.extractClientIP(req)
		// When all IPs in XFF are trusted proxies, should fall back to RemoteAddr
		if ip != testTrustedProxyIP {
			t.Errorf("Expected 10.0.0.1 (RemoteAddr fallback), got %s", ip)
		}
	})
}

func TestSetTrustedProxies(t *testing.T) {
	t.Run("valid configuration", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})

		err := filter.SetTrustedProxies([]string{"192.168.0.0/16", "172.16.0.1"})
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if len(filter.trustedProxy) != 2 {
			t.Errorf("Expected 2 trusted proxies, got %d", len(filter.trustedProxy))
		}
	})

	t.Run("invalid IP", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})

		err := filter.SetTrustedProxies([]string{"not-an-ip"})
		if err == nil {
			t.Error("Expected error for invalid IP")
		}
	})
}

func TestIsBogonIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"loopback IPv4", "127.0.0.1", true},
		{"loopback IPv6", "::1", true},
		{"private 10.x", testTrustedProxyIP, true},
		{"private 172.16.x", "172.16.0.1", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"link-local unicast", "169.254.1.1", true},
		{"link-local multicast", "224.0.0.1", true},
		{"public IP", "8.8.8.8", false},
		{"public IP 2", testClientIP, false},
		{"public IPv6", "2001:db8::1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("Failed to parse test IP: %s", tt.ip)
			}
			result := isBogonIP(ip)
			if result != tt.expected {
				t.Errorf("isBogonIP(%s) = %v, want %v", tt.ip, result, tt.expected)
			}
		})
	}
}

func TestExtractClientIPBogonFiltering(t *testing.T) {
	// Setup global trusted proxies
	if err := SetGlobalTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		trustedProxyCIDRs = nil
	}()

	t.Run("bogon IP in XFF is skipped", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", "1.2.3.4, 192.168.1.100")

		ip := extractClientIP(req)
		if ip != testClientIP {
			t.Errorf("Expected 1.2.3.4 (skipping bogon 192.168.1.100), got %s", ip)
		}
	})

	t.Run("all bogon IPs in XFF falls back to RemoteAddr", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", "192.168.1.1, 172.16.0.1")

		ip := extractClientIP(req)
		if ip != testTrustedProxyIP {
			t.Errorf("Expected 10.0.0.1 (RemoteAddr fallback), got %s", ip)
		}
	})

	t.Run("loopback in XFF is skipped", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", "5.6.7.8, 127.0.0.1")

		ip := extractClientIP(req)
		if ip != testAltClientIP {
			t.Errorf("Expected 5.6.7.8 (skipping loopback), got %s", ip)
		}
	})
}

func TestExtractClientIPValidation(t *testing.T) {
	// Setup global trusted proxies
	if err := SetGlobalTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		trustedProxyCIDRs = nil
	}()

	t.Run("unparseable IP in XFF is skipped", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", "1.2.3.4, not-an-ip")

		ip := extractClientIP(req)
		if ip != testClientIP {
			t.Errorf("Expected 1.2.3.4 (skipping invalid entry), got %s", ip)
		}
	})

	t.Run("all unparseable IPs in XFF falls back to RemoteAddr", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", "garbage, not-an-ip, %%%")

		ip := extractClientIP(req)
		if ip != testTrustedProxyIP {
			t.Errorf("Expected 10.0.0.1 (RemoteAddr fallback), got %s", ip)
		}
	})

	t.Run("unparseable X-Real-IP falls back to RemoteAddr", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Real-IP", "not-an-ip")

		ip := extractClientIP(req)
		if ip != testTrustedProxyIP {
			t.Errorf("Expected 10.0.0.1 (RemoteAddr fallback for invalid X-Real-IP), got %s", ip)
		}
	})
}

func TestExtractClientIPMaxDepth(t *testing.T) {
	// Setup global trusted proxies
	if err := SetGlobalTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		trustedProxyCIDRs = nil
	}()

	t.Run("XFF within depth limit is fully processed", func(t *testing.T) {
		// Build an XFF with exactly defaultMaxXFFDepth entries
		entries := make([]string, defaultMaxXFFDepth)
		for i := range entries {
			entries[i] = testSecondTrustedProxy // trusted proxy IPs
		}
		entries[0] = testAltClientIP // real client at leftmost position

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", strings.Join(entries, ", "))

		ip := extractClientIP(req)
		if ip != testAltClientIP {
			t.Errorf("Expected 5.6.7.8 (client IP within depth limit), got %s", ip)
		}
	})

	t.Run("XFF exceeding depth limit is truncated to rightmost entries", func(t *testing.T) {
		// Build an XFF with more entries than the limit
		entries := make([]string, defaultMaxXFFDepth+5)
		for i := range entries {
			entries[i] = testSecondTrustedProxy // trusted proxy IPs
		}
		// Place a public IP at position 0 (leftmost, will be truncated away)
		entries[0] = testAltClientIP
		// Place a public IP within the rightmost N entries
		entries[len(entries)-3] = "9.8.7.6"

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", strings.Join(entries, ", "))

		ip := extractClientIP(req)
		if ip != "9.8.7.6" {
			t.Errorf("Expected 9.8.7.6 (IP within truncated window), got %s", ip)
		}
	})

	t.Run("truncated XFF loses leftmost client IP", func(t *testing.T) {
		// Build an XFF that exceeds the limit where the only public IP is outside the window
		entries := make([]string, defaultMaxXFFDepth+5)
		entries[0] = testAltClientIP // real client - will be truncated
		for i := 1; i < len(entries); i++ {
			entries[i] = testSecondTrustedProxy // trusted proxy IPs
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", strings.Join(entries, ", "))

		ip := extractClientIP(req)
		// The real client IP was truncated, so it falls back to RemoteAddr
		if ip != testTrustedProxyIP {
			t.Errorf("Expected 10.0.0.1 (RemoteAddr fallback after truncation), got %s", ip)
		}
	})
}

func TestIPFilterExtractClientIPBogonFiltering(t *testing.T) {
	t.Run("bogon IP in XFF is skipped", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"0.0.0.0/0"})
		if err := filter.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", "1.2.3.4, 192.168.1.100")

		ip := filter.extractClientIP(req)
		if ip != testClientIP {
			t.Errorf("Expected 1.2.3.4 (skipping bogon), got %s", ip)
		}
	})

	t.Run("unparseable IP in XFF is skipped", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"0.0.0.0/0"})
		if err := filter.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Forwarded-For", "1.2.3.4, garbage-value")

		ip := filter.extractClientIP(req)
		if ip != testClientIP {
			t.Errorf("Expected 1.2.3.4 (skipping invalid), got %s", ip)
		}
	})

	t.Run("unparseable X-Real-IP falls back to RemoteAddr", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"0.0.0.0/0"})
		if err := filter.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testTrustedProxyAddr
		req.Header.Set("X-Real-IP", "not-valid")

		ip := filter.extractClientIP(req)
		if ip != testTrustedProxyIP {
			t.Errorf("Expected 10.0.0.1 (RemoteAddr fallback), got %s", ip)
		}
	})
}
