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
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// OIDCSession represents an authenticated OIDC user session.
type OIDCSession struct {
	IDToken      string                 `json:"id_token"`
	AccessToken  string                 `json:"access_token"`
	RefreshToken string                 `json:"refresh_token"`
	Expiry       time.Time              `json:"expiry"`
	Claims       map[string]interface{} `json:"claims"`
}

const (
	// maxOIDCSessions is the maximum number of pending OIDC auth flow sessions.
	// Sessions exceeding this limit trigger eviction of the oldest entries.
	maxOIDCSessions = 10000
)

// OIDCHandler implements the OAuth2/OIDC authentication flow.
type OIDCHandler struct {
	config       *pb.OIDCConfig
	logger       *zap.Logger
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config oauth2.Config
	sessionKey   []byte // 32-byte AES key for session cookie encryption

	mu       sync.RWMutex
	sessions map[string]*OIDCSession // state -> session (for auth flow)

	// SessionCookieName is the name of the session cookie
	SessionCookieName string
}

// NewOIDCHandler creates a new OIDC handler.
func NewOIDCHandler(ctx context.Context, config *pb.OIDCConfig, logger *zap.Logger) (*OIDCHandler, error) {
	issuerURL := config.IssuerUrl
	if issuerURL == "" && config.Keycloak != nil {
		// Auto-construct issuer URL from Keycloak config
		issuerURL = fmt.Sprintf("%s/realms/%s", strings.TrimRight(config.Keycloak.ServerUrl, "/"), config.Keycloak.Realm)
	}

	if issuerURL == "" {
		return nil, fmt.Errorf("issuer URL is required (set issuerURL or keycloak config)")
	}

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider for %s: %w", issuerURL, err)
	}

	scopes := config.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	sessionKey := config.SessionSecret
	if len(sessionKey) == 0 {
		return nil, fmt.Errorf("session secret is required (32 bytes)")
	}
	if len(sessionKey) != 32 {
		// Hash to 32 bytes if not exact length
		h := sha256.Sum256(sessionKey)
		sessionKey = h[:]
	}

	oauth2Config := oauth2.Config{
		ClientID:     config.ClientId,
		ClientSecret: config.ClientSecret,
		RedirectURL:  config.RedirectUrl,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: config.ClientId})

	h := &OIDCHandler{
		config:            config,
		logger:            logger,
		provider:          provider,
		verifier:          verifier,
		oauth2Config:      oauth2Config,
		sessionKey:        sessionKey,
		sessions:          make(map[string]*OIDCSession),
		SessionCookieName: "novaedge_session",
	}

	// Start session cleanup goroutine to evict expired auth flow sessions
	go h.cleanupSessions(ctx)

	return h, nil
}

// storeSession stores a pending auth flow session with size-bounded eviction.
func (h *OIDCHandler) storeSession(state string, session *OIDCSession) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Evict oldest entries if at capacity
	if len(h.sessions) >= maxOIDCSessions {
		// Find and remove the oldest entry (expired first, then arbitrary)
		var oldestKey string
		var oldestTime time.Time
		first := true
		for key, sess := range h.sessions {
			// Prefer to evict sessions that look expired (zero Expiry means pending auth)
			if !sess.Expiry.IsZero() && time.Now().After(sess.Expiry) {
				delete(h.sessions, key)
				continue
			}
			if first || (sess.Expiry.Before(oldestTime) && !sess.Expiry.IsZero()) {
				oldestKey = key
				oldestTime = sess.Expiry
				first = false
			}
		}
		// If still at capacity after evicting expired, remove the oldest
		if len(h.sessions) >= maxOIDCSessions && oldestKey != "" {
			delete(h.sessions, oldestKey)
			h.logger.Warn("OIDC sessions map at capacity, evicted oldest entry",
				zap.Int("max_sessions", maxOIDCSessions),
			)
		}
	}

	h.sessions[state] = session
}

// cleanupSessions periodically removes expired pending auth flow sessions.
func (h *OIDCHandler) cleanupSessions(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Debug("OIDC session cleanup goroutine stopped")
			return
		case <-ticker.C:
			h.mu.Lock()
			before := len(h.sessions)
			for key, sess := range h.sessions {
				// Pending auth flow sessions (state tokens) should expire within 10 minutes
				if sess.IDToken == "" && sess.AccessToken == "" {
					// This is a pending auth flow entry (only has code verifier)
					// These should be short-lived; evict any older than 10 minutes
					continue
				}
				if !sess.Expiry.IsZero() && time.Now().After(sess.Expiry) {
					delete(h.sessions, key)
				}
			}
			after := len(h.sessions)
			if before != after {
				h.logger.Debug("cleaned up expired OIDC sessions",
					zap.Int("removed", before-after),
					zap.Int("remaining", after),
				)
			}
			h.mu.Unlock()
		}
	}
}

// encryptSession encrypts session data for the cookie.
func (h *OIDCHandler) encryptSession(session *OIDCSession) (string, error) {
	data, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("failed to marshal session: %w", err)
	}

	block, err := aes.NewCipher(h.sessionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	encrypted := aesGCM.Seal(nonce, nonce, data, nil)
	return base64.URLEncoding.EncodeToString(encrypted), nil
}

// decryptSession decrypts session data from the cookie.
func (h *OIDCHandler) decryptSession(encrypted string) (*OIDCSession, error) {
	data, err := base64.URLEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decode session: %w", err)
	}

	block, err := aes.NewCipher(h.sessionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt session: %w", err)
	}

	var session OIDCSession
	if err := json.Unmarshal(plaintext, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	return &session, nil
}

// generateState generates a random state parameter for PKCE.
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// generateCodeVerifier generates a PKCE code verifier.
func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// refreshToken attempts to refresh the access token using the refresh token.
func (h *OIDCHandler) refreshToken(ctx context.Context, session *OIDCSession) (*OIDCSession, error) {
	if session.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	token := &oauth2.Token{
		RefreshToken: session.RefreshToken,
	}

	tokenSource := h.oauth2Config.TokenSource(ctx, token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	// Extract ID token from the refreshed token
	rawIDToken, ok := newToken.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in refreshed token response")
	}

	// Verify the refreshed ID token
	idToken, err := h.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify refreshed id_token: %w", err)
	}

	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse refreshed claims: %w", err)
	}

	return &OIDCSession{
		IDToken:      rawIDToken,
		AccessToken:  newToken.AccessToken,
		RefreshToken: newToken.RefreshToken,
		Expiry:       newToken.Expiry,
		Claims:       claims,
	}, nil
}

// forwardUserInfo sets upstream headers with user info from claims.
func (h *OIDCHandler) forwardUserInfo(r *http.Request, claims map[string]interface{}) {
	for _, header := range h.config.ForwardHeaders {
		claimName := strings.TrimPrefix(header, "X-Auth-")
		claimName = strings.ToLower(claimName)

		// Look up the claim value
		if val, ok := claims[claimName]; ok {
			switch v := val.(type) {
			case string:
				r.Header.Set(header, v)
			case []interface{}:
				parts := make([]string, 0, len(v))
				for _, item := range v {
					parts = append(parts, fmt.Sprintf("%v", item))
				}
				r.Header.Set(header, strings.Join(parts, ","))
			default:
				r.Header.Set(header, fmt.Sprintf("%v", v))
			}
		}
	}

	// Always forward sub and email if available
	if sub, ok := claims["sub"].(string); ok {
		r.Header.Set("X-Auth-Subject", sub)
	}
	if email, ok := claims["email"].(string); ok {
		r.Header.Set("X-Auth-Email", email)
	}
	if name, ok := claims["name"].(string); ok {
		r.Header.Set("X-Auth-Name", name)
	}
}

// HandleOIDC returns HTTP middleware that implements the OIDC auth flow.
func HandleOIDC(handler *OIDCHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handle callback path
			if strings.HasSuffix(r.URL.Path, "/oauth2/callback") {
				handleOIDCCallback(handler, w, r)
				return
			}

			// Handle logout path
			if strings.HasSuffix(r.URL.Path, "/oauth2/logout") {
				handleOIDCLogout(handler, w, r)
				return
			}

			// Check for existing session cookie
			cookie, err := r.Cookie(handler.SessionCookieName)
			if err != nil || cookie.Value == "" {
				// No session - redirect to auth
				redirectToAuth(handler, w, r)
				return
			}

			// Decrypt and validate session
			session, err := handler.decryptSession(cookie.Value)
			if err != nil {
				// Invalid session - redirect to auth
				redirectToAuth(handler, w, r)
				return
			}

			// Check if token is expired
			if time.Now().After(session.Expiry) {
				// Try to refresh
				newSession, refreshErr := handler.refreshToken(r.Context(), session)
				if refreshErr != nil {
					// Refresh failed - redirect to auth
					redirectToAuth(handler, w, r)
					return
				}

				// Save refreshed session
				encrypted, encErr := handler.encryptSession(newSession)
				if encErr != nil {
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}

				http.SetCookie(w, &http.Cookie{
					Name:     handler.SessionCookieName,
					Value:    encrypted,
					Path:     "/",
					HttpOnly: true,
					Secure:   r.TLS != nil,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   86400, // 24 hours
				})
				session = newSession
			}

			// Check authorization (roles/groups) if configured
			if handler.config.Authorization != nil {
				if !checkAuthorization(handler.config, session.Claims) {
					metrics.OIDCAuthTotal.WithLabelValues("forbidden").Inc()
					http.Error(w, "Forbidden: insufficient roles or groups", http.StatusForbidden)
					return
				}
			}

			// Forward user info to upstream
			handler.forwardUserInfo(r, session.Claims)

			// Forward Keycloak roles/groups if configured
			if handler.config.Keycloak != nil {
				forwardKeycloakInfo(r, handler.config, session.Claims)
			}

			metrics.OIDCAuthTotal.WithLabelValues("success").Inc()
			next.ServeHTTP(w, r)
		})
	}
}

// redirectToAuth initiates the OIDC auth flow.
func redirectToAuth(handler *OIDCHandler, w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Store code verifier in state (PKCE) with bounded session map
	handler.storeSession(state, &OIDCSession{
		// Store the code verifier in the RefreshToken field temporarily
		RefreshToken: codeVerifier,
	})

	// Generate PKCE challenge
	codeChallenge := oauth2.S256ChallengeOption(codeVerifier)
	authURL := handler.oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline, codeChallenge)

	// Store the original URL in a cookie so we can redirect back after auth
	http.SetCookie(w, &http.Cookie{
		Name:     "novaedge_redirect",
		Value:    r.URL.String(),
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600, // 10 minutes
	})

	metrics.OIDCAuthTotal.WithLabelValues("redirect").Inc()
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleOIDCCallback handles the OAuth2 callback after IdP authentication.
func handleOIDCCallback(handler *OIDCHandler, w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get state and code from callback
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if state == "" || code == "" {
		http.Error(w, "Missing state or code parameter", http.StatusBadRequest)
		return
	}

	// Retrieve stored state (PKCE verifier)
	handler.mu.Lock()
	storedSession, exists := handler.sessions[state]
	if exists {
		delete(handler.sessions, state)
	}
	handler.mu.Unlock()

	if !exists {
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}

	codeVerifier := storedSession.RefreshToken

	// Exchange authorization code for tokens with PKCE
	token, err := handler.oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		metrics.OIDCAuthTotal.WithLabelValues("exchange_error").Inc()
		http.Error(w, fmt.Sprintf("Token exchange failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Extract and verify ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "No id_token in token response", http.StatusInternalServerError)
		return
	}

	idToken, err := handler.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to verify id_token: %v", err), http.StatusInternalServerError)
		return
	}

	// Extract claims
	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse claims: %v", err), http.StatusInternalServerError)
		return
	}

	// Create session
	session := &OIDCSession{
		IDToken:      rawIDToken,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
		Claims:       claims,
	}

	// Encrypt and store in cookie
	encrypted, err := handler.encryptSession(session)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     handler.SessionCookieName,
		Value:    encrypted,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400, // 24 hours
	})

	// Redirect back to original URL
	redirectURL := "/"
	if redirectCookie, err := r.Cookie("novaedge_redirect"); err == nil {
		redirectURL = redirectCookie.Value
	}

	// Clear the redirect cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "novaedge_redirect",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	metrics.OIDCAuthTotal.WithLabelValues("callback_success").Inc()
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// handleOIDCLogout handles the logout flow.
func handleOIDCLogout(handler *OIDCHandler, w http.ResponseWriter, r *http.Request) {
	// Clear session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     handler.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	metrics.OIDCAuthTotal.WithLabelValues("logout").Inc()

	// If Keycloak, redirect to Keycloak logout endpoint
	if handler.config.Keycloak != nil {
		logoutURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/logout",
			strings.TrimRight(handler.config.Keycloak.ServerUrl, "/"),
			handler.config.Keycloak.Realm)

		redirectURI := handler.config.RedirectUrl
		if redirectURI != "" {
			// Strip the callback path to get the base URL
			redirectURI = strings.TrimSuffix(redirectURI, "/oauth2/callback")
		}
		logoutURL += fmt.Sprintf("?post_logout_redirect_uri=%s&client_id=%s", redirectURI, handler.config.ClientId)
		http.Redirect(w, r, logoutURL, http.StatusFound)
		return
	}

	// Generic: just redirect to root
	http.Redirect(w, r, "/", http.StatusFound)
}

// checkAuthorization verifies the user has required roles/groups.
func checkAuthorization(config *pb.OIDCConfig, claims map[string]interface{}) bool {
	authz := config.Authorization
	if authz == nil {
		return true
	}

	hasRoles := len(authz.RequiredRoles) == 0
	hasGroups := len(authz.RequiredGroups) == 0

	// Check roles
	if len(authz.RequiredRoles) > 0 {
		userRoles := extractRoles(config, claims)
		if authz.Mode == "all" {
			hasRoles = containsAll(userRoles, authz.RequiredRoles)
		} else {
			hasRoles = containsAny(userRoles, authz.RequiredRoles)
		}
	}

	// Check groups
	if len(authz.RequiredGroups) > 0 {
		userGroups := extractGroups(config, claims)
		if authz.Mode == "all" {
			hasGroups = containsAll(userGroups, authz.RequiredGroups)
		} else {
			hasGroups = containsAny(userGroups, authz.RequiredGroups)
		}
	}

	// In "all" mode, both roles AND groups must be satisfied
	// In "any" mode, either roles OR groups satisfies the check
	if authz.Mode == "all" {
		return hasRoles && hasGroups
	}

	// "any" mode: if we have no required roles, check groups; vice versa
	if len(authz.RequiredRoles) == 0 {
		return hasGroups
	}
	if len(authz.RequiredGroups) == 0 {
		return hasRoles
	}
	return hasRoles || hasGroups
}

// extractRoles extracts user roles from JWT claims.
func extractRoles(config *pb.OIDCConfig, claims map[string]interface{}) []string {
	if config.Keycloak != nil {
		return extractKeycloakRoles(config, claims)
	}

	// Generic: try "roles" claim
	return extractStringSlice(claims, "roles")
}

// extractGroups extracts user groups from JWT claims.
func extractGroups(config *pb.OIDCConfig, claims map[string]interface{}) []string {
	groupClaim := "groups"
	if config.Keycloak != nil && config.Keycloak.GroupClaim != "" {
		groupClaim = config.Keycloak.GroupClaim
	}
	return extractStringSlice(claims, groupClaim)
}

// extractStringSlice extracts a string slice from a nested claim path.
func extractStringSlice(claims map[string]interface{}, path string) []string {
	parts := strings.Split(path, ".")
	var current interface{} = claims

	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current, ok = m[part]
		if !ok {
			return nil
		}
	}

	// Handle string slice
	if arr, ok := current.([]interface{}); ok {
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}

	return nil
}

// containsAll checks if all required items are in the user's list.
func containsAll(userItems, required []string) bool {
	userSet := make(map[string]struct{}, len(userItems))
	for _, item := range userItems {
		userSet[item] = struct{}{}
	}
	for _, req := range required {
		if _, exists := userSet[req]; !exists {
			return false
		}
	}
	return true
}

// containsAny checks if any required item is in the user's list.
func containsAny(userItems, required []string) bool {
	userSet := make(map[string]struct{}, len(userItems))
	for _, item := range userItems {
		userSet[item] = struct{}{}
	}
	for _, req := range required {
		if _, exists := userSet[req]; exists {
			return true
		}
	}
	return false
}
