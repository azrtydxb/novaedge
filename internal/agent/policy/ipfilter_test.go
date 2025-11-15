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
	"net/http"
	"net/http/httptest"
	"testing"
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
	SetGlobalTrustedProxies([]string{"10.0.0.0/8", "192.168.1.1"})
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
	SetGlobalTrustedProxies([]string{"10.0.0.0/8"})
	defer func() {
		trustedProxyCIDRs = nil
	}()

	t.Run("direct connection no trusted proxies", func(t *testing.T) {
		// Clear trusted proxies
		trustedProxyCIDRs = nil

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "1.2.3.4:12345"
		req.Header.Set("X-Forwarded-For", "5.6.7.8")

		ip := extractClientIP(req)
		if ip != "1.2.3.4" {
			t.Errorf("Expected 1.2.3.4, got %s", ip)
		}
	})

	t.Run("through trusted proxy with X-Forwarded-For", func(t *testing.T) {
		SetGlobalTrustedProxies([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.2")

		ip := extractClientIP(req)
		if ip != "1.2.3.4" {
			t.Errorf("Expected 1.2.3.4 (client IP), got %s", ip)
		}
	})

	t.Run("through untrusted proxy ignores X-Forwarded-For", func(t *testing.T) {
		SetGlobalTrustedProxies([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "8.8.8.8:12345"
		req.Header.Set("X-Forwarded-For", "1.2.3.4")

		ip := extractClientIP(req)
		if ip != "8.8.8.8" {
			t.Errorf("Expected 8.8.8.8 (untrusted proxy IP), got %s", ip)
		}
	})

	t.Run("X-Real-IP fallback", func(t *testing.T) {
		SetGlobalTrustedProxies([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Real-IP", "1.2.3.4")

		ip := extractClientIP(req)
		if ip != "1.2.3.4" {
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
		req.RemoteAddr = "10.1.2.3:12345"

		if !filter.Allow(req) {
			t.Error("Expected IP in allow list to be allowed")
		}
	})

	t.Run("allow list: IP not in list", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "8.8.8.8:12345"

		if filter.Allow(req) {
			t.Error("Expected IP not in allow list to be denied")
		}
	})

	t.Run("deny list: IP in list", func(t *testing.T) {
		filter, _ := NewIPDenyListFilter([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.1.2.3:12345"

		if filter.Allow(req) {
			t.Error("Expected IP in deny list to be blocked")
		}
	})

	t.Run("deny list: IP not in list", func(t *testing.T) {
		filter, _ := NewIPDenyListFilter([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "8.8.8.8:12345"

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
		w.Write([]byte("OK"))
	})

	t.Run("allow list: allowed IP", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})
		middleware := HandleIPFilter(filter)
		handler := middleware(nextHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.1.2.3:12345"
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
		req.RemoteAddr = "8.8.8.8:12345"
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
		req.RemoteAddr = "8.8.8.8:12345"
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
		req.RemoteAddr = "10.1.2.3:12345"
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
		req.Header.Set("X-Forwarded-For", "5.6.7.8")

		ip := filter.extractClientIP(req)
		if ip != "1.2.3.4" {
			t.Errorf("Expected 1.2.3.4, got %s", ip)
		}
	})

	t.Run("through trusted proxy with X-Forwarded-For", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})
		filter.SetTrustedProxies([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.2")

		ip := filter.extractClientIP(req)
		if ip != "1.2.3.4" {
			t.Errorf("Expected 1.2.3.4 (client IP), got %s", ip)
		}
	})

	t.Run("through untrusted proxy ignores X-Forwarded-For", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})
		filter.SetTrustedProxies([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "8.8.8.8:12345"
		req.Header.Set("X-Forwarded-For", "1.2.3.4")

		ip := filter.extractClientIP(req)
		if ip != "8.8.8.8" {
			t.Errorf("Expected 8.8.8.8 (untrusted proxy IP), got %s", ip)
		}
	})

	t.Run("X-Real-IP fallback", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})
		filter.SetTrustedProxies([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Real-IP", "1.2.3.4")

		ip := filter.extractClientIP(req)
		if ip != "1.2.3.4" {
			t.Errorf("Expected 1.2.3.4 (from X-Real-IP), got %s", ip)
		}
	})

	t.Run("X-Forwarded-For with all trusted proxies", func(t *testing.T) {
		filter, _ := NewIPAllowListFilter([]string{"10.0.0.0/8"})
		filter.SetTrustedProxies([]string{"10.0.0.0/8"})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "10.0.0.2, 10.0.0.3, 10.0.0.4")

		ip := filter.extractClientIP(req)
		// When all IPs in XFF are trusted proxies, should fall back to RemoteAddr
		if ip != "10.0.0.1" {
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
