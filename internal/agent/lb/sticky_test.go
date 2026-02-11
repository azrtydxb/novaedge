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

package lb

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestStickyWrapperFirstRequestSetsCookie(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, w)
	if ep == nil {
		t.Fatal("SelectWithAffinity returned nil")
	}

	// Check that a cookie was set
	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	cookies := resp.Cookies()

	found := false
	for _, c := range cookies {
		if c.Name == config.CookieName {
			found = true
			expectedValue := endpointKey(ep)
			if c.Value != expectedValue {
				t.Errorf("Cookie value = %q, want %q", c.Value, expectedValue)
			}
			if !c.HttpOnly {
				t.Error("Cookie should be HttpOnly")
			}
			break
		}
	}
	if !found {
		t.Errorf("Affinity cookie %q was not set", config.CookieName)
	}
}

func TestStickyWrapperSubsequentRequestUsesExistingCookie(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	// Target endpoint: always 10.0.0.2
	targetKey := "10.0.0.2:8080"

	// Create request with existing affinity cookie
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  config.CookieName,
		Value: targetKey,
	})
	w := httptest.NewRecorder()

	// Should always return the endpoint from the cookie
	for i := 0; i < 50; i++ {
		ep := sw.SelectWithAffinity(req, w)
		if ep == nil {
			t.Fatal("SelectWithAffinity returned nil")
		}
		if ep.Address != "10.0.0.2" || ep.Port != 8080 {
			t.Errorf("Expected 10.0.0.2:8080, got %s:%d", ep.Address, ep.Port)
		}
	}
}

func TestStickyWrapperFallbackWhenEndpointGone(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	// Create request with cookie pointing to a non-existent endpoint
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  config.CookieName,
		Value: "10.0.0.99:8080",
	})
	w := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, w)
	if ep == nil {
		t.Fatal("SelectWithAffinity returned nil on fallback")
	}

	// Should fall back to one of the available endpoints
	if ep.Address != "10.0.0.1" && ep.Address != "10.0.0.2" {
		t.Errorf("Expected fallback to available endpoint, got %s", ep.Address)
	}

	// A new cookie should have been set
	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	cookies := resp.Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == config.CookieName {
			found = true
			break
		}
	}
	if !found {
		t.Error("New affinity cookie should have been set after fallback")
	}
}

func TestStickyWrapperFallbackWhenEndpointUnhealthy(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: false}, // Unhealthy
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	// Cookie points to unhealthy endpoint
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  config.CookieName,
		Value: "10.0.0.2:8080",
	})
	w := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, w)
	if ep == nil {
		t.Fatal("SelectWithAffinity returned nil")
	}

	// Should fall back to healthy endpoint
	if ep.Address != "10.0.0.1" {
		t.Errorf("Expected fallback to healthy endpoint 10.0.0.1, got %s", ep.Address)
	}
}

func TestStickyWrapperCookieTTL(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := StickyConfig{
		CookieName: "TEST_AFFINITY",
		TTL:        30 * time.Minute,
		Path:       "/api",
		Secure:     true,
		SameSite:   http.SameSiteStrictMode,
	}
	sw := NewStickyWrapper(inner, config, endpoints)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	sw.SelectWithAffinity(req, w)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	cookies := resp.Cookies()

	for _, c := range cookies {
		if c.Name == "TEST_AFFINITY" {
			if c.MaxAge != 1800 {
				t.Errorf("Cookie MaxAge = %d, want 1800", c.MaxAge)
			}
			if c.Path != "/api" {
				t.Errorf("Cookie Path = %q, want /api", c.Path)
			}
			if !c.Secure {
				t.Error("Cookie should be Secure")
			}
			if c.SameSite != http.SameSiteStrictMode {
				t.Errorf("Cookie SameSite = %d, want Strict", c.SameSite)
			}
			return
		}
	}
	t.Error("Cookie TEST_AFFINITY was not set")
}

func TestStickyWrapperUpdateEndpoints(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(initialEndpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, initialEndpoints)

	// Update to new endpoints
	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.3", Port: 8080, Ready: true},
		{Address: "10.0.0.4", Port: 8080, Ready: true},
	}
	sw.UpdateEndpoints(newEndpoints)

	// Cookie pointing to old endpoint should trigger fallback
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  config.CookieName,
		Value: "10.0.0.1:8080",
	})
	w := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, w)
	if ep == nil {
		t.Fatal("SelectWithAffinity returned nil")
	}

	// Should route to one of the new endpoints
	if ep.Address != "10.0.0.3" && ep.Address != "10.0.0.4" {
		t.Errorf("Expected one of new endpoints, got %s", ep.Address)
	}
}

func TestStickyWrapperNoCookieNilRequest(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	// Request with no cookies
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, w)
	if ep == nil {
		t.Fatal("SelectWithAffinity returned nil")
	}
	if ep.Address != "10.0.0.1" {
		t.Errorf("Expected 10.0.0.1, got %s", ep.Address)
	}
}

func TestStickyWrapperGetInner(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	if sw.GetInner() != inner {
		t.Error("GetInner should return the underlying load balancer")
	}
}

func TestParseSameSite(t *testing.T) {
	tests := []struct {
		input    string
		expected http.SameSite
	}{
		{"Strict", http.SameSiteStrictMode},
		{"Lax", http.SameSiteLaxMode},
		{"None", http.SameSiteNoneMode},
		{"", http.SameSiteLaxMode},
		{"Invalid", http.SameSiteLaxMode},
	}

	for _, tt := range tests {
		result := ParseSameSite(tt.input)
		if result != tt.expected {
			t.Errorf("ParseSameSite(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}
