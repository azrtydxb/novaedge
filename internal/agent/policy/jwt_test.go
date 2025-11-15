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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// generateTestRSAKeyPair generates an RSA key pair for testing
func generateTestRSAKeyPair() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
}

// generateTestCertificate generates a test X.509 certificate
func generateTestCertificate(privateKey *rsa.PrivateKey) (string, error) {
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"NovaEdge Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", err
	}

	// Encode to base64 (without PEM headers for x5c)
	return base64.StdEncoding.EncodeToString(certDER), nil
}

// createTestJWKS creates a test JWKS with the given key
func createTestJWKS(kid string, cert string) *JWKS {
	return &JWKS{
		Keys: []JWK{
			{
				Kid: kid,
				Kty: "RSA",
				Alg: "RS256",
				Use: "sig",
				X5c: []string{cert},
			},
		},
	}
}

// createTestToken creates a test JWT token
func createTestToken(privateKey *rsa.PrivateKey, kid, issuer string, audience []string, expired bool) (string, error) {
	now := time.Now()
	exp := now.Add(1 * time.Hour)
	if expired {
		exp = now.Add(-1 * time.Hour)
	}

	claims := jwt.MapClaims{
		"iss": issuer,
		"exp": exp.Unix(),
		"iat": now.Unix(),
	}

	if len(audience) > 0 {
		claims["aud"] = audience[0]
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	return token.SignedString(privateKey)
}

func TestParseRSAPublicKey(t *testing.T) {
	t.Run("valid certificate", func(t *testing.T) {
		privateKey, err := generateTestRSAKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate key pair: %v", err)
		}

		cert, err := generateTestCertificate(privateKey)
		if err != nil {
			t.Fatalf("Failed to generate certificate: %v", err)
		}

		jwk := JWK{
			Kid: "test-key",
			Kty: "RSA",
			X5c: []string{cert},
		}

		pubKey, err := parseRSAPublicKey(jwk)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if pubKey == nil {
			t.Error("Expected public key, got nil")
		}

		// Verify the public key matches
		if !pubKey.Equal(&privateKey.PublicKey) {
			t.Error("Parsed public key does not match original")
		}
	})

	t.Run("invalid certificate", func(t *testing.T) {
		jwk := JWK{
			Kid: "test-key",
			Kty: "RSA",
			X5c: []string{"invalid-base64"},
		}

		_, err := parseRSAPublicKey(jwk)
		if err == nil {
			t.Error("Expected error for invalid certificate")
		}
	})

	t.Run("missing x5c", func(t *testing.T) {
		jwk := JWK{
			Kid: "test-key",
			Kty: "RSA",
			X5c: []string{},
		}

		_, err := parseRSAPublicKey(jwk)
		if err == nil {
			t.Error("Expected error for missing x5c")
		}
	})
}

func TestNewJWTValidator(t *testing.T) {
	t.Run("without JWKS URI", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:   "test-issuer",
			Audience: []string{"test-audience"},
		}

		validator, err := NewJWTValidator(config)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if validator == nil {
			t.Error("Expected validator, got nil")
		}

		if validator.config != config {
			t.Error("Validator config does not match")
		}
	})

	t.Run("with valid JWKS URI", func(t *testing.T) {
		// Create test key and certificate
		privateKey, err := generateTestRSAKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate key pair: %v", err)
		}

		cert, err := generateTestCertificate(privateKey)
		if err != nil {
			t.Fatalf("Failed to generate certificate: %v", err)
		}

		jwks := createTestJWKS("test-key", cert)

		// Create mock JWKS server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jwks)
		}))
		defer server.Close()

		config := &pb.JWTConfig{
			Issuer:   "test-issuer",
			Audience: []string{"test-audience"},
			JwksUri:  server.URL,
		}

		validator, err := NewJWTValidator(config)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if validator == nil {
			t.Error("Expected validator, got nil")
		}

		// Verify key was loaded
		validator.mu.RLock()
		_, exists := validator.keys["test-key"]
		validator.mu.RUnlock()

		if !exists {
			t.Error("Expected key to be loaded")
		}
	})

	t.Run("with invalid JWKS URI", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:   "test-issuer",
			Audience: []string{"test-audience"},
			JwksUri:  "http://invalid-host-that-does-not-exist.local/jwks",
		}

		_, err := NewJWTValidator(config)
		if err == nil {
			t.Error("Expected error for invalid JWKS URI")
		}
	})
}

func TestValidate(t *testing.T) {
	// Setup: Create test key, certificate, and JWKS
	privateKey, err := generateTestRSAKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	cert, err := generateTestCertificate(privateKey)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	jwks := createTestJWKS("test-key", cert)

	// Create mock JWKS server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	t.Run("valid token", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:   "test-issuer",
			Audience: []string{"test-audience"},
			JwksUri:  server.URL,
		}

		validator, err := NewJWTValidator(config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		tokenString, err := createTestToken(privateKey, "test-key", "test-issuer", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		token, err := validator.Validate(tokenString)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if token == nil || !token.Valid {
			t.Error("Expected valid token")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:   "test-issuer",
			Audience: []string{"test-audience"},
			JwksUri:  server.URL,
		}

		validator, err := NewJWTValidator(config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		tokenString, err := createTestToken(privateKey, "test-key", "test-issuer", []string{"test-audience"}, true)
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		_, err = validator.Validate(tokenString)
		if err == nil {
			t.Error("Expected error for expired token")
		}
	})

	t.Run("invalid issuer", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:   "expected-issuer",
			Audience: []string{"test-audience"},
			JwksUri:  server.URL,
		}

		validator, err := NewJWTValidator(config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		tokenString, err := createTestToken(privateKey, "test-key", "wrong-issuer", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		_, err = validator.Validate(tokenString)
		if err == nil {
			t.Error("Expected error for invalid issuer")
		}
	})

	t.Run("invalid audience", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:   "test-issuer",
			Audience: []string{"expected-audience"},
			JwksUri:  server.URL,
		}

		validator, err := NewJWTValidator(config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		tokenString, err := createTestToken(privateKey, "test-key", "test-issuer", []string{"wrong-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		_, err = validator.Validate(tokenString)
		if err == nil {
			t.Error("Expected error for invalid audience")
		}
	})

	t.Run("unknown key ID", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:   "test-issuer",
			Audience: []string{"test-audience"},
			JwksUri:  server.URL,
		}

		validator, err := NewJWTValidator(config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		tokenString, err := createTestToken(privateKey, "unknown-key", "test-issuer", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		_, err = validator.Validate(tokenString)
		if err == nil {
			t.Error("Expected error for unknown key ID")
		}
	})

	t.Run("malformed token", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:   "test-issuer",
			Audience: []string{"test-audience"},
			JwksUri:  server.URL,
		}

		validator, err := NewJWTValidator(config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		_, err = validator.Validate("not.a.valid.jwt")
		if err == nil {
			t.Error("Expected error for malformed token")
		}
	})
}

func TestHandleJWT(t *testing.T) {
	// Setup: Create test key, certificate, and validator
	privateKey, err := generateTestRSAKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	cert, err := generateTestCertificate(privateKey)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	jwks := createTestJWKS("test-key", cert)

	// Create mock JWKS server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	config := &pb.JWTConfig{
		Issuer:   "test-issuer",
		Audience: []string{"test-audience"},
		JwksUri:  server.URL,
	}

	validator, err := NewJWTValidator(config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	// Create test handler
	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	middleware := HandleJWT(validator)
	handler := middleware(nextHandler)

	t.Run("missing Authorization header", func(t *testing.T) {
		nextCalled = false
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		if nextCalled {
			t.Error("Next handler should not be called for missing Authorization header")
		}
	})

	t.Run("invalid Authorization header format", func(t *testing.T) {
		nextCalled = false
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "InvalidFormat token123")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		if nextCalled {
			t.Error("Next handler should not be called for invalid Authorization format")
		}
	})

	t.Run("valid token", func(t *testing.T) {
		nextCalled = false
		tokenString, err := createTestToken(privateKey, "test-key", "test-issuer", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}

		if !nextCalled {
			t.Error("Next handler should be called for valid token")
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		nextCalled = false
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid.jwt.token")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		if nextCalled {
			t.Error("Next handler should not be called for invalid token")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		nextCalled = false
		tokenString, err := createTestToken(privateKey, "test-key", "test-issuer", []string{"test-audience"}, true)
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		if nextCalled {
			t.Error("Next handler should not be called for expired token")
		}
	})
}

func TestFetchJWKS(t *testing.T) {
	t.Run("successful fetch", func(t *testing.T) {
		privateKey, err := generateTestRSAKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate key pair: %v", err)
		}

		cert, err := generateTestCertificate(privateKey)
		if err != nil {
			t.Fatalf("Failed to generate certificate: %v", err)
		}

		jwks := createTestJWKS("test-key", cert)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jwks)
		}))
		defer server.Close()

		config := &pb.JWTConfig{
			JwksUri: server.URL,
		}

		validator := &JWTValidator{
			config: config,
			keys:   make(map[string]interface{}),
		}

		err = validator.fetchJWKS()
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		validator.mu.RLock()
		_, exists := validator.keys["test-key"]
		validator.mu.RUnlock()

		if !exists {
			t.Error("Expected key to be loaded")
		}
	})

	t.Run("server returns error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		config := &pb.JWTConfig{
			JwksUri: server.URL,
		}

		validator := &JWTValidator{
			config: config,
			keys:   make(map[string]interface{}),
		}

		err := validator.fetchJWKS()
		if err == nil {
			t.Error("Expected error for server error status")
		}
	})

	t.Run("invalid JSON response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("invalid json"))
		}))
		defer server.Close()

		config := &pb.JWTConfig{
			JwksUri: server.URL,
		}

		validator := &JWTValidator{
			config: config,
			keys:   make(map[string]interface{}),
		}

		err := validator.fetchJWKS()
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})
}
