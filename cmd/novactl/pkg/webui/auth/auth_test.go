package auth

import (
	"context"
	"fmt"
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

func TestSetMaxSessions(t *testing.T) {
	// Save original and restore after test.
	orig := maxSessions.Load()
	defer maxSessions.Store(orig)

	SetMaxSessions(50)
	if maxSessions.Load() != 50 {
		t.Fatalf("expected maxSessions=50, got %d", maxSessions.Load())
	}

	// Zero resets to default.
	SetMaxSessions(0)
	if maxSessions.Load() != int32(defaultMaxSessions) {
		t.Fatalf("expected maxSessions=%d after zero, got %d", defaultMaxSessions, maxSessions.Load())
	}

	// Negative resets to default.
	SetMaxSessions(-1)
	if maxSessions.Load() != int32(defaultMaxSessions) {
		t.Fatalf("expected maxSessions=%d after negative, got %d", defaultMaxSessions, maxSessions.Load())
	}
}

func TestEvictExpiredSessions(t *testing.T) {
	m, err := NewManager(Config{
		BasicUser:  "admin",
		BasicPass:  "secret",
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Insert sessions that are already expired.
	past := time.Now().Add(-time.Minute)
	for i := 0; i < 5; i++ {
		m.sessions.Store(fmt.Sprintf("expired-%d", i), past)
	}

	// Insert one valid session.
	future := time.Now().Add(time.Hour)
	m.sessions.Store("valid-token", future)

	m.evictExcessSessions()

	// Expired sessions must be gone.
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("expired-%d", i)
		if _, ok := m.sessions.Load(key); ok {
			t.Errorf("expected expired session %s to be evicted", key)
		}
	}

	// Valid session must remain.
	if _, ok := m.sessions.Load("valid-token"); !ok {
		t.Error("expected valid session to remain after eviction")
	}
}

func TestEvictExcessSessionsDeterministic(t *testing.T) {
	// Save original and restore after test.
	orig := maxSessions.Load()
	defer maxSessions.Store(orig)

	SetMaxSessions(3)

	m, err := NewManager(Config{
		BasicUser:  "admin",
		BasicPass:  "secret",
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Insert 5 sessions with staggered expiries. The 2 with the earliest
	// expiry should be evicted, leaving 3.
	now := time.Now()
	m.sessions.Store("tok-1", now.Add(1*time.Minute)) // earliest — evict
	m.sessions.Store("tok-2", now.Add(2*time.Minute)) // second earliest — evict
	m.sessions.Store("tok-3", now.Add(3*time.Minute)) // keep
	m.sessions.Store("tok-4", now.Add(4*time.Minute)) // keep
	m.sessions.Store("tok-5", now.Add(5*time.Minute)) // keep

	m.evictExcessSessions()

	// tok-1 and tok-2 should be evicted (soonest to expire).
	for _, key := range []string{"tok-1", "tok-2"} {
		if _, ok := m.sessions.Load(key); ok {
			t.Errorf("expected session %s to be evicted", key)
		}
	}

	// tok-3, tok-4, tok-5 should remain.
	for _, key := range []string{"tok-3", "tok-4", "tok-5"} {
		if _, ok := m.sessions.Load(key); !ok {
			t.Errorf("expected session %s to remain", key)
		}
	}
}

func TestEvictNonStringKey(t *testing.T) {
	m, err := NewManager(Config{
		BasicUser:  "admin",
		BasicPass:  "secret",
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Store a non-string key (should be cleaned up by eviction).
	m.sessions.Store(12345, time.Now().Add(time.Hour))
	// Also store a valid session.
	m.sessions.Store("valid", time.Now().Add(time.Hour))

	m.evictExcessSessions()

	// Non-string key must be deleted.
	if _, ok := m.sessions.Load(12345); ok {
		t.Error("expected non-string key to be deleted by eviction")
	}

	// Valid session must remain.
	if _, ok := m.sessions.Load("valid"); !ok {
		t.Error("expected valid session to remain")
	}
}
