// Package auth provides authentication and session management for the NovaEdge web UI.
package auth

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	errAuthenticationIsNotConfigured = errors.New("authentication is not configured")
	errInvalidCredentials            = errors.New("invalid credentials")
	errUnexpectedSigningMethod       = errors.New("unexpected signing method")
	errSessionNotFound               = errors.New("session not found")
	errInvalidSessionData            = errors.New("invalid session data")
	errSessionExpired                = errors.New("session expired")
)

const (
	// jwtSecretSize is the size of the random JWT signing secret in bytes.
	jwtSecretSize = 32
	// cookieName is the name of the session cookie.
	cookieName = "novaedge_session"
	// defaultSessionTTL is the default session duration.
	defaultSessionTTL = 8 * time.Hour
)

// Config holds authentication configuration.
type Config struct {
	BasicUser    string
	BasicPass    string
	OIDCIssuer   string
	OIDCClientID string
	OIDCSecret   string
	SessionTTL   time.Duration
}

// Enabled returns true if authentication credentials are configured.
func (c Config) Enabled() bool {
	return c.BasicUser != "" && c.BasicPass != ""
}

// OIDCEnabled returns true if OIDC configuration is provided.
func (c Config) OIDCEnabled() bool {
	return c.OIDCIssuer != "" && c.OIDCClientID != ""
}

// Manager handles authentication and session lifecycle.
type Manager struct {
	config    Config
	jwtSecret []byte
	sessions  sync.Map
}

// NewManager creates a new authentication manager with a random JWT signing secret.
func NewManager(config Config) (*Manager, error) {
	if config.SessionTTL == 0 {
		config.SessionTTL = defaultSessionTTL
	}

	secret := make([]byte, jwtSecretSize)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("failed to generate JWT secret: %w", err)
	}

	return &Manager{
		config:    config,
		jwtSecret: secret,
	}, nil
}

// Login validates the provided credentials and returns a JWT token on success.
func (m *Manager) Login(user, pass string) (string, error) {
	if !m.config.Enabled() {
		return "", errAuthenticationIsNotConfigured
	}

	if user != m.config.BasicUser || pass != m.config.BasicPass {
		return "", errInvalidCredentials
	}

	token, err := m.createToken(user)
	if err != nil {
		return "", fmt.Errorf("failed to create token: %w", err)
	}

	m.sessions.Store(token, time.Now().Add(m.config.SessionTTL))

	return token, nil
}

// createToken generates an HS256-signed JWT for the given username.
func (m *Manager) createToken(user string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   user,
		ExpiresAt: jwt.NewNumericDate(now.Add(m.config.SessionTTL)),
		IssuedAt:  jwt.NewNumericDate(now),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(m.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return signed, nil
}

// ValidateToken verifies the JWT signature and checks that the session has not expired or been revoked.
func (m *Manager) ValidateToken(tokenStr string) error {
	_, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: %v", errUnexpectedSigningMethod, t.Header["alg"])
		}
		return m.jwtSecret, nil
	})
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	expiry, ok := m.sessions.Load(tokenStr)
	if !ok {
		return errSessionNotFound
	}

	expiryTime, ok := expiry.(time.Time)
	if !ok {
		return errInvalidSessionData
	}

	if time.Now().After(expiryTime) {
		m.sessions.Delete(tokenStr)
		return errSessionExpired
	}

	return nil
}

// Logout removes the session associated with the given token.
func (m *Manager) Logout(tokenStr string) {
	m.sessions.Delete(tokenStr)
}

// Enabled returns whether the underlying auth config is enabled.
func (m *Manager) Enabled() bool {
	return m.config.Enabled()
}

// OIDCEnabled returns whether OIDC is configured.
func (m *Manager) OIDCEnabled() bool {
	return m.config.OIDCEnabled()
}

// Middleware returns an HTTP middleware that enforces authentication on protected paths.
// When auth is disabled, all requests pass through.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If auth is not enabled, pass through
		if !m.config.Enabled() {
			next.ServeHTTP(w, r)
			return
		}

		// Allow auth paths and static files through without authentication
		if isAuthPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Check for session cookie
		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value == "" {
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		if err := m.ValidateToken(cookie.Value); err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isAuthPath returns true for paths that do not require authentication.
func isAuthPath(path string) bool {
	// Static files (non-API paths) are always allowed so the login page can load
	if !strings.HasPrefix(path, "/api/") {
		return true
	}

	// Allow auth endpoints
	if strings.HasPrefix(path, "/api/v1/auth/") {
		return true
	}

	// Allow mode and health endpoints
	switch path {
	case "/api/v1/mode", "/api/v1/health":
		return true
	}

	return false
}

// writeJSONError writes a JSON-encoded error response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
