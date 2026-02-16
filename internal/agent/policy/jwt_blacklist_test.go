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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewTokenBlacklist(t *testing.T) {
	bl := NewTokenBlacklist()
	if bl == nil {
		t.Fatal("Expected non-nil blacklist")
	}
	if bl.Size() != 0 {
		t.Errorf("Expected empty blacklist, got size %d", bl.Size())
	}
}

func TestTokenBlacklistAdd(t *testing.T) {
	bl := NewTokenBlacklist()

	expiry := time.Now().Add(1 * time.Hour)
	bl.Add("token-1", expiry)

	if bl.Size() != 1 {
		t.Errorf("Expected blacklist size 1, got %d", bl.Size())
	}

	bl.Add("token-2", expiry)
	if bl.Size() != 2 {
		t.Errorf("Expected blacklist size 2, got %d", bl.Size())
	}

	// Adding the same jti again should overwrite (not duplicate)
	bl.Add("token-1", expiry.Add(1*time.Hour))
	if bl.Size() != 2 {
		t.Errorf("Expected blacklist size 2 after re-add, got %d", bl.Size())
	}
}

func TestTokenBlacklistIsBlacklisted(t *testing.T) {
	bl := NewTokenBlacklist()

	t.Run("not in blacklist", func(t *testing.T) {
		if bl.IsBlacklisted("nonexistent") {
			t.Error("Expected false for token not in blacklist")
		}
	})

	t.Run("active entry", func(t *testing.T) {
		expiry := time.Now().Add(1 * time.Hour)
		bl.Add("active-token", expiry)

		if !bl.IsBlacklisted("active-token") {
			t.Error("Expected true for active blacklisted token")
		}
	})

	t.Run("expired entry", func(t *testing.T) {
		expiry := time.Now().Add(-1 * time.Second)
		bl.Add("expired-token", expiry)

		if bl.IsBlacklisted("expired-token") {
			t.Error("Expected false for expired blacklisted token")
		}
	})
}

func TestTokenBlacklistCleanup(t *testing.T) {
	bl := NewTokenBlacklist()

	// Add a mix of expired and active entries
	bl.Add("expired-1", time.Now().Add(-1*time.Hour))
	bl.Add("expired-2", time.Now().Add(-30*time.Minute))
	bl.Add("active-1", time.Now().Add(1*time.Hour))
	bl.Add("active-2", time.Now().Add(2*time.Hour))

	if bl.Size() != 4 {
		t.Fatalf("Expected blacklist size 4 before cleanup, got %d", bl.Size())
	}

	bl.Cleanup()

	if bl.Size() != 2 {
		t.Errorf("Expected blacklist size 2 after cleanup, got %d", bl.Size())
	}

	if !bl.IsBlacklisted("active-1") {
		t.Error("Expected active-1 to still be blacklisted after cleanup")
	}
	if !bl.IsBlacklisted("active-2") {
		t.Error("Expected active-2 to still be blacklisted after cleanup")
	}
}

func TestTokenBlacklistStartCleanup(t *testing.T) {
	bl := NewTokenBlacklist()

	// Add an entry that expires very quickly
	bl.Add("soon-expired", time.Now().Add(50*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bl.StartCleanup(ctx, 100*time.Millisecond)

	// Wait for the entry to expire and cleanup to run
	time.Sleep(250 * time.Millisecond)

	if bl.Size() != 0 {
		t.Errorf("Expected blacklist to be empty after cleanup, got size %d", bl.Size())
	}

	// Test that cancelling context stops cleanup
	cancel()
	time.Sleep(150 * time.Millisecond) // Let goroutine exit
}

func TestTokenBlacklistStartCleanupContextCancel(t *testing.T) {
	bl := NewTokenBlacklist()

	ctx, cancel := context.WithCancel(context.Background())

	bl.StartCleanup(ctx, 1*time.Hour)

	// Cancel immediately; the goroutine should exit cleanly
	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestJWTValidatorRevoke(t *testing.T) {
	config := &pb.JWTConfig{
		AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
		Issuer:            "test-issuer",
		Audience:          []string{"test-audience"},
	}

	validator, err := NewJWTValidator(context.Background(), config, WithHTTPClient(&http.Client{}))
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	expiry := time.Now().Add(1 * time.Hour)
	validator.Revoke("revoked-token-id", expiry)

	bl := validator.Blacklist()
	if bl.Size() != 1 {
		t.Errorf("Expected blacklist size 1, got %d", bl.Size())
	}

	if !bl.IsBlacklisted("revoked-token-id") {
		t.Error("Expected token to be blacklisted after Revoke")
	}
}

func TestJWTValidatorBlacklistInitialized(t *testing.T) {
	config := &pb.JWTConfig{
		AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
		Issuer:            "test-issuer",
	}

	validator, err := NewJWTValidator(context.Background(), config, WithHTTPClient(&http.Client{}))
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	if validator.Blacklist() == nil {
		t.Error("Expected blacklist to be initialized")
	}
}

// createTestTokenWithJTI creates a test JWT token with a jti claim
func createTestTokenWithJTI(privateKey interface{}, jti string, audience []string) (string, error) {
	now := time.Now()
	exp := now.Add(1 * time.Hour)

	claims := jwt.MapClaims{
		"iss": "test-issuer",
		"exp": exp.Unix(),
		"iat": now.Unix(),
	}

	if jti != "" {
		claims["jti"] = jti
	}

	if len(audience) > 0 {
		claims["aud"] = audience[0]
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"

	return token.SignedString(privateKey)
}

func TestValidateBlacklistedToken(t *testing.T) {
	// Setup: Create test key, certificate, and JWKS
	privateKey, err := generateTestRSAKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	cert, err := generateTestCertificate(privateKey)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	jwks := createTestJWKS(cert)

	// Create mock JWKS server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	t.Run("blacklisted token is rejected", func(t *testing.T) {
		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
		}

		validator, err := NewJWTValidator(context.Background(), config, WithHTTPClient(&http.Client{}))
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		jti := "unique-token-id-123"
		tokenString, err := createTestTokenWithJTI(privateKey, jti, []string{"test-audience"})
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		// Token should be valid before revocation
		token, err := validator.Validate(tokenString)
		if err != nil {
			t.Fatalf("Expected valid token before revocation, got error: %v", err)
		}
		if !token.Valid {
			t.Fatal("Expected token to be valid before revocation")
		}

		// Revoke the token
		validator.Revoke(jti, time.Now().Add(1*time.Hour))

		// Token should now be rejected
		_, err = validator.Validate(tokenString)
		if err == nil {
			t.Error("Expected error for blacklisted token")
		}
		if err != nil && err.Error() != "token has been revoked" {
			t.Errorf("Expected 'token has been revoked' error, got: %v", err)
		}
	})

	t.Run("token without jti is not affected by blacklist", func(t *testing.T) {
		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
		}

		validator, err := NewJWTValidator(context.Background(), config, WithHTTPClient(&http.Client{}))
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		// Create token without jti
		tokenString, err := createTestTokenWithJTI(privateKey, "", []string{"test-audience"})
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		// Revoke some arbitrary jti
		validator.Revoke("some-other-jti", time.Now().Add(1*time.Hour))

		// Token without jti should still be valid
		token, err := validator.Validate(tokenString)
		if err != nil {
			t.Errorf("Expected no error for token without jti, got: %v", err)
		}
		if token == nil || !token.Valid {
			t.Error("Expected valid token for token without jti")
		}
	})

	t.Run("token with expired blacklist entry is allowed", func(t *testing.T) {
		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
		}

		validator, err := NewJWTValidator(context.Background(), config, WithHTTPClient(&http.Client{}))
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		jti := "token-with-expired-blacklist"
		tokenString, err := createTestTokenWithJTI(privateKey, jti, []string{"test-audience"})
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		// Add to blacklist with already-expired time
		validator.Revoke(jti, time.Now().Add(-1*time.Second))

		// Token should be accepted because the blacklist entry has expired
		token, err := validator.Validate(tokenString)
		if err != nil {
			t.Errorf("Expected no error for token with expired blacklist entry, got: %v", err)
		}
		if token == nil || !token.Valid {
			t.Error("Expected valid token when blacklist entry has expired")
		}
	})

	t.Run("different jti not affected", func(t *testing.T) {
		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
		}

		validator, err := NewJWTValidator(context.Background(), config, WithHTTPClient(&http.Client{}))
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		jti := "different-token-id"
		tokenString, err := createTestTokenWithJTI(privateKey, jti, []string{"test-audience"})
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		// Revoke a different token
		validator.Revoke("other-token-id", time.Now().Add(1*time.Hour))

		// This token should still be valid
		token, err := validator.Validate(tokenString)
		if err != nil {
			t.Errorf("Expected no error for non-revoked token, got: %v", err)
		}
		if token == nil || !token.Valid {
			t.Error("Expected valid token for non-revoked token")
		}
	})
}

func TestHandleJWTWithBlacklist(t *testing.T) {
	// Setup: Create test key, certificate, and validator
	privateKey, err := generateTestRSAKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	cert, err := generateTestCertificate(privateKey)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	jwks := createTestJWKS(cert)

	// Create mock JWKS server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	config := &pb.JWTConfig{
		AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
		Issuer:            "test-issuer",
		Audience:          []string{"test-audience"},
		JwksUri:           server.URL,
	}

	validator, err := NewJWTValidator(context.Background(), config, WithHTTPClient(&http.Client{}))
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	middleware := HandleJWT(validator)
	handler := middleware(nextHandler)

	t.Run("blacklisted token rejected by middleware", func(t *testing.T) {
		nextCalled = false
		jti := "middleware-revoked-token"
		tokenString, err := createTestTokenWithJTI(privateKey, jti, []string{"test-audience"})
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		validator.Revoke(jti, time.Now().Add(1*time.Hour))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		if nextCalled {
			t.Error("Next handler should not be called for blacklisted token")
		}
	})
}
