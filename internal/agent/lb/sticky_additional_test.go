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

func TestDefaultStickyConfig(t *testing.T) {
	config := DefaultStickyConfig()

	if config.CookieName != "NOVAEDGE_AFFINITY" {
		t.Errorf("CookieName = %q, want %q", config.CookieName, "NOVAEDGE_AFFINITY")
	}
	if config.TTL != 0 {
		t.Errorf("TTL = %v, want 0", config.TTL)
	}
	if config.Path != "/" {
		t.Errorf("Path = %q, want %q", config.Path, "/")
	}
	if config.Secure {
		t.Error("Secure should be false")
	}
	if config.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want %v", config.SameSite, http.SameSiteLaxMode)
	}
}

func TestNewStickyWrapper(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()

	sw := NewStickyWrapper(inner, config, endpoints)
	if sw == nil {
		t.Fatal("NewStickyWrapper() returned nil")
	}
}

func TestStickyWrapper_Select(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	// Select should delegate to inner LB
	ep := sw.Select()
	if ep == nil {
		t.Error("Select() returned nil")
	}
}

func TestStickyWrapper_UpdateEndpoints(t *testing.T) {
	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(initialEndpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, initialEndpoints)

	newEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	sw.UpdateEndpoints(newEndpoints)

	// Verify endpoints were updated
	if len(sw.endpoints) != 2 {
		t.Errorf("endpoints length = %d, want 2", len(sw.endpoints))
	}
}

func TestStickyWrapper_SelectWithAffinity_NoCookie(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	// Request without cookie
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, rec)
	if ep == nil {
		t.Fatal("SelectWithAffinity() returned nil")
	}

	// Should set a cookie
	resp := rec.Result()
	defer resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Error("Expected cookie to be set")
	}

	// Verify cookie name
	found := false
	for _, c := range cookies {
		if c.Name == config.CookieName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected cookie %q to be set", config.CookieName)
	}
}

func TestStickyWrapper_SelectWithAffinity_WithValidCookie(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	// Request with valid affinity cookie
	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  config.CookieName,
		Value: "10.0.0.1:8080",
	})
	rec := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, rec)
	if ep == nil {
		t.Fatal("SelectWithAffinity() returned nil")
	}

	// Should return the endpoint from the cookie
	if ep.Address != "10.0.0.1" {
		t.Errorf("Address = %q, want %q", ep.Address, "10.0.0.1")
	}

	// Should not set a new cookie since affinity was used
	resp := rec.Result()
	defer resp.Body.Close()
	cookies := resp.Cookies()
	for _, c := range cookies {
		if c.Name == config.CookieName {
			t.Error("Should not set new cookie when affinity is used")
		}
	}
}

func TestStickyWrapper_SelectWithAffinity_WithInvalidCookie(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	// Request with invalid affinity cookie (endpoint doesn't exist)
	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  config.CookieName,
		Value: "10.0.0.99:8080", // non-existent
	})
	rec := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, rec)
	if ep == nil {
		t.Fatal("SelectWithAffinity() returned nil")
	}

	// Should fall back to inner LB and set new cookie
	resp := rec.Result()
	defer resp.Body.Close()
	cookies := resp.Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == config.CookieName {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected new cookie to be set when affinity fails")
	}
}

func TestStickyWrapper_SelectWithAffinity_EmptyCookie(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	// Request with empty cookie value
	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  config.CookieName,
		Value: "",
	})
	rec := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, rec)
	if ep == nil {
		t.Fatal("SelectWithAffinity() returned nil")
	}
}

func TestStickyWrapper_SelectWithAffinity_UnreadyEndpoint(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: false}, // unready
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	// Request with cookie pointing to unready endpoint
	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  config.CookieName,
		Value: "10.0.0.1:8080",
	})
	rec := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, rec)
	if ep == nil {
		t.Fatal("SelectWithAffinity() returned nil")
	}

	// Should fall back to ready endpoint
	if ep.Address == "10.0.0.1" {
		t.Error("Should not return unready endpoint")
	}
}

func TestStickyWrapper_SelectWithAffinity_NoEndpoints(t *testing.T) {
	endpoints := []*pb.Endpoint{}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, rec)
	if ep != nil {
		t.Errorf("SelectWithAffinity() with no endpoints returned %v, want nil", ep)
	}
}

func TestStickyWrapper_GetInner(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	result := sw.GetInner()
	if result != inner {
		t.Error("GetInner() should return the inner load balancer")
	}
}

func TestStickyWrapper_CustomConfig(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := StickyConfig{
		CookieName: "CUSTOM_AFFINITY",
		TTL:        time.Hour,
		Path:       "/api",
		Secure:     true,
		SameSite:   http.SameSiteStrictMode,
	}
	sw := NewStickyWrapper(inner, config, endpoints)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	ep := sw.SelectWithAffinity(req, rec)
	if ep == nil {
		t.Fatal("SelectWithAffinity() returned nil")
	}

	// Verify custom cookie settings
	resp := rec.Result()
	defer resp.Body.Close()
	cookies := resp.Cookies()
	var affinityCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "CUSTOM_AFFINITY" {
			affinityCookie = c
			break
		}
	}

	if affinityCookie == nil {
		t.Fatal("Custom cookie not set")
	}

	if affinityCookie.Path != "/api" {
		t.Errorf("Cookie Path = %q, want %q", affinityCookie.Path, "/api")
	}
	if !affinityCookie.Secure {
		t.Error("Cookie should be Secure")
	}
	if affinityCookie.MaxAge != 3600 {
		t.Errorf("Cookie MaxAge = %d, want 3600", affinityCookie.MaxAge)
	}
}

func TestStickyWrapper_ConcurrentAccess(_ *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := DefaultStickyConfig()
	sw := NewStickyWrapper(inner, config, endpoints)

	done := make(chan bool)

	// Concurrent SelectWithAffinity
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				req := httptest.NewRequest("GET", "/test", nil)
				rec := httptest.NewRecorder()
				_ = sw.SelectWithAffinity(req, rec)
			}
			done <- true
		}()
	}

	// Concurrent UpdateEndpoints
	go func() {
		for i := 0; i < 10; i++ {
			newEndpoints := []*pb.Endpoint{
				{Address: "10.0.0.1", Port: 8080, Ready: true},
			}
			sw.UpdateEndpoints(newEndpoints)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 11; i++ {
		<-done
	}
}

func TestStickyWrapper_SessionCookie(t *testing.T) {
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
	}

	inner := NewRoundRobin(endpoints)
	config := StickyConfig{
		CookieName: "SESSION_AFFINITY",
		TTL:        0, // session cookie
		Path:       "/",
	}
	sw := NewStickyWrapper(inner, config, endpoints)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	_ = sw.SelectWithAffinity(req, rec)

	// Session cookie should have MaxAge = 0
	resp := rec.Result()
	defer resp.Body.Close()
	cookies := resp.Cookies()
	for _, c := range cookies {
		if c.Name == "SESSION_AFFINITY" {
			if c.MaxAge != 0 {
				t.Errorf("Session cookie MaxAge = %d, want 0", c.MaxAge)
			}
			break
		}
	}
}
