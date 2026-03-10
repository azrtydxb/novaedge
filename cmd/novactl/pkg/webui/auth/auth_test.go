package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginSuccess(t *testing.T) {
	m, err := NewManager(Config{
		BasicUser:  "admin",
		BasicPass:  "secret",
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	token, err := m.Login("admin", "secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestLoginFailure(t *testing.T) {
	m, err := NewManager(Config{
		BasicUser:  "admin",
		BasicPass:  "secret",
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	_, err = m.Login("admin", "wrong")
	if err == nil {
		t.Fatal("expected error for invalid credentials")
	}

	_, err = m.Login("wrong", "secret")
	if err == nil {
		t.Fatal("expected error for invalid username")
	}
}

func TestTokenValidation(t *testing.T) {
	m, err := NewManager(Config{
		BasicUser:  "admin",
		BasicPass:  "secret",
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	token, err := m.Login("admin", "secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if err := m.ValidateToken(token); err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}

	// Invalid token string
	if err := m.ValidateToken("garbage"); err == nil {
		t.Fatal("expected error for garbage token")
	}
}

func TestLogout(t *testing.T) {
	m, err := NewManager(Config{
		BasicUser:  "admin",
		BasicPass:  "secret",
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	token, err := m.Login("admin", "secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	m.Logout(token)

	if err := m.ValidateToken(token); err == nil {
		t.Fatal("expected error after logout")
	}
}

func TestDisabledAuth(t *testing.T) {
	m, err := NewManager(Config{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if m.Enabled() {
		t.Fatal("expected auth to be disabled with empty config")
	}

	_, err = m.Login("any", "any")
	if err == nil {
		t.Fatal("expected error when auth is disabled")
	}
}

func TestMiddlewareAllowsWhenDisabled(t *testing.T) {
	m, err := NewManager(Config{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/gateways", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when auth disabled, got %d", rec.Code)
	}
}

func TestMiddlewareBlocksUnauthed(t *testing.T) {
	m, err := NewManager(Config{
		BasicUser:  "admin",
		BasicPass:  "secret",
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/gateways", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", rec.Code)
	}
}

func TestMiddlewareAllowsAuthPaths(t *testing.T) {
	m, err := NewManager(Config{
		BasicUser:  "admin",
		BasicPass:  "secret",
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	paths := []string{
		"/api/v1/auth/login",
		"/api/v1/auth/logout",
		"/api/v1/auth/session",
		"/api/v1/mode",
		"/api/v1/health",
		"/",
		"/index.html",
		"/assets/app.js",
	}

	for _, path := range paths {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for auth path %s, got %d", path, rec.Code)
		}
	}
}

func TestMiddlewareAllowsValidSession(t *testing.T) {
	m, err := NewManager(Config{
		BasicUser:  "admin",
		BasicPass:  "secret",
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	token, err := m.Login("admin", "secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/gateways", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated request, got %d", rec.Code)
	}
}
