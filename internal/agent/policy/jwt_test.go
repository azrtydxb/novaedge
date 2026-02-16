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
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// generateTestRSAKeyPair generates an RSA key pair for testing
func generateTestRSAKeyPair() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
}

// generateTestECKeyPair generates an ECDSA key pair for testing
func generateTestECKeyPair(curve elliptic.Curve) (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(curve, rand.Reader)
}

// generateTestEdDSAKeyPair generates an Ed25519 key pair for testing
func generateTestEdDSAKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
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
func createTestJWKS(cert string) *JWKS {
	return &JWKS{
		Keys: []JWK{
			{
				Kid: "test-key",
				Kty: "RSA",
				Alg: "RS256",
				Use: "sig",
				X5c: []string{cert},
			},
		},
	}
}

// createTestECJWKS creates a test JWKS with an EC key
func createTestECJWKS(kid string, pubKey *ecdsa.PublicKey, crv string) *JWKS {
	x := base64.RawURLEncoding.EncodeToString(pubKey.X.Bytes())
	y := base64.RawURLEncoding.EncodeToString(pubKey.Y.Bytes())

	return &JWKS{
		Keys: []JWK{
			{
				Kid: kid,
				Kty: "EC",
				Alg: crvToAlg(crv),
				Use: "sig",
				Crv: crv,
				X:   x,
				Y:   y,
			},
		},
	}
}

// createTestEdDSAJWKS creates a test JWKS with an OKP (Ed25519) key
func createTestEdDSAJWKS(kid string, pubKey ed25519.PublicKey) *JWKS {
	x := base64.RawURLEncoding.EncodeToString(pubKey)

	return &JWKS{
		Keys: []JWK{
			{
				Kid: kid,
				Kty: "OKP",
				Alg: "EdDSA",
				Use: "sig",
				Crv: "Ed25519",
				X:   x,
			},
		},
	}
}

// createTestMixedJWKS creates a JWKS with RSA, EC, and EdDSA keys
func createTestMixedJWKS(rsaCert string, ecPub *ecdsa.PublicKey, edPub ed25519.PublicKey) *JWKS {
	ecX := base64.RawURLEncoding.EncodeToString(ecPub.X.Bytes())
	ecY := base64.RawURLEncoding.EncodeToString(ecPub.Y.Bytes())
	edX := base64.RawURLEncoding.EncodeToString(edPub)

	return &JWKS{
		Keys: []JWK{
			{
				Kid: "rsa-key",
				Kty: "RSA",
				Alg: "RS256",
				Use: "sig",
				X5c: []string{rsaCert},
			},
			{
				Kid: "ec-key",
				Kty: "EC",
				Alg: "ES256",
				Use: "sig",
				Crv: "P-256",
				X:   ecX,
				Y:   ecY,
			},
			{
				Kid: "ed-key",
				Kty: "OKP",
				Alg: "EdDSA",
				Use: "sig",
				Crv: "Ed25519",
				X:   edX,
			},
		},
	}
}

func crvToAlg(crv string) string {
	switch crv {
	case "P-256":
		return "ES256"
	case "P-384":
		return "ES384"
	case "P-521":
		return "ES512"
	default:
		return ""
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

// createTestECToken creates a test JWT token signed with an ECDSA key
func createTestECToken(privateKey *ecdsa.PrivateKey, method jwt.SigningMethod, kid string, audience []string, expired bool) (string, error) {
	now := time.Now()
	exp := now.Add(1 * time.Hour)
	if expired {
		exp = now.Add(-1 * time.Hour)
	}

	claims := jwt.MapClaims{
		"iss": "test-issuer",
		"exp": exp.Unix(),
		"iat": now.Unix(),
	}

	if len(audience) > 0 {
		claims["aud"] = audience[0]
	}

	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid

	return token.SignedString(privateKey)
}

// createTestEdDSAToken creates a test JWT token signed with an Ed25519 key
func createTestEdDSAToken(privateKey ed25519.PrivateKey, kid string, audience []string, expired bool) (string, error) {
	now := time.Now()
	exp := now.Add(1 * time.Hour)
	if expired {
		exp = now.Add(-1 * time.Hour)
	}

	claims := jwt.MapClaims{
		"iss": "test-issuer",
		"exp": exp.Unix(),
		"iat": now.Unix(),
	}

	if len(audience) > 0 {
		claims["aud"] = audience[0]
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
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

	t.Run("valid n and e parameters", func(t *testing.T) {
		privateKey, err := generateTestRSAKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate key pair: %v", err)
		}

		n := base64.RawURLEncoding.EncodeToString(privateKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.E)).Bytes())

		jwk := JWK{
			Kid: "test-key",
			Kty: "RSA",
			N:   n,
			E:   e,
		}

		pubKey, err := parseRSAPublicKey(jwk)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if pubKey == nil {
			t.Fatal("Expected public key, got nil")
		}

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

	t.Run("missing x5c and n/e", func(t *testing.T) {
		jwk := JWK{
			Kid: "test-key",
			Kty: "RSA",
			X5c: []string{},
		}

		_, err := parseRSAPublicKey(jwk)
		if err == nil {
			t.Error("Expected error for missing x5c and n/e")
		}
	})
}

func TestParseECPublicKey(t *testing.T) {
	t.Run("valid P-256 key", func(t *testing.T) {
		privateKey, err := generateTestECKeyPair(elliptic.P256())
		if err != nil {
			t.Fatalf("Failed to generate EC key pair: %v", err)
		}

		x := base64.RawURLEncoding.EncodeToString(privateKey.X.Bytes())
		y := base64.RawURLEncoding.EncodeToString(privateKey.Y.Bytes())

		jwk := JWK{
			Kid: "ec-key",
			Kty: "EC",
			Crv: "P-256",
			X:   x,
			Y:   y,
		}

		pubKey, err := parseECPublicKey(jwk)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if pubKey == nil {
			t.Fatal("Expected public key, got nil")
		}

		if !pubKey.Equal(&privateKey.PublicKey) {
			t.Error("Parsed public key does not match original")
		}
	})

	t.Run("valid P-384 key", func(t *testing.T) {
		privateKey, err := generateTestECKeyPair(elliptic.P384())
		if err != nil {
			t.Fatalf("Failed to generate EC key pair: %v", err)
		}

		x := base64.RawURLEncoding.EncodeToString(privateKey.X.Bytes())
		y := base64.RawURLEncoding.EncodeToString(privateKey.Y.Bytes())

		jwk := JWK{
			Kid: "ec-key",
			Kty: "EC",
			Crv: "P-384",
			X:   x,
			Y:   y,
		}

		pubKey, err := parseECPublicKey(jwk)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if pubKey == nil {
			t.Fatal("Expected public key, got nil")
		}

		if pubKey.Curve != elliptic.P384() {
			t.Error("Expected P-384 curve")
		}
	})

	t.Run("valid P-521 key", func(t *testing.T) {
		privateKey, err := generateTestECKeyPair(elliptic.P521())
		if err != nil {
			t.Fatalf("Failed to generate EC key pair: %v", err)
		}

		x := base64.RawURLEncoding.EncodeToString(privateKey.X.Bytes())
		y := base64.RawURLEncoding.EncodeToString(privateKey.Y.Bytes())

		jwk := JWK{
			Kid: "ec-key",
			Kty: "EC",
			Crv: "P-521",
			X:   x,
			Y:   y,
		}

		pubKey, err := parseECPublicKey(jwk)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if pubKey == nil {
			t.Fatal("Expected public key, got nil")
		}

		if pubKey.Curve != elliptic.P521() {
			t.Error("Expected P-521 curve")
		}
	})

	t.Run("unsupported curve", func(t *testing.T) {
		jwk := JWK{
			Kid: "ec-key",
			Kty: "EC",
			Crv: "P-192",
			X:   "AAAA",
			Y:   "BBBB",
		}

		_, err := parseECPublicKey(jwk)
		if err == nil {
			t.Error("Expected error for unsupported curve")
		}
	})

	t.Run("missing parameters", func(t *testing.T) {
		jwk := JWK{
			Kid: "ec-key",
			Kty: "EC",
		}

		_, err := parseECPublicKey(jwk)
		if err == nil {
			t.Error("Expected error for missing EC parameters")
		}
	})
}

func TestParseOKPPublicKey(t *testing.T) {
	t.Run("valid Ed25519 key", func(t *testing.T) {
		pubKey, _, err := generateTestEdDSAKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
		}

		x := base64.RawURLEncoding.EncodeToString(pubKey)

		jwk := JWK{
			Kid: "ed-key",
			Kty: "OKP",
			Crv: "Ed25519",
			X:   x,
		}

		parsed, err := parseOKPPublicKey(jwk)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if !pubKey.Equal(parsed) {
			t.Error("Parsed public key does not match original")
		}
	})

	t.Run("unsupported curve", func(t *testing.T) {
		jwk := JWK{
			Kid: "ed-key",
			Kty: "OKP",
			Crv: "Ed448",
			X:   "AAAA",
		}

		_, err := parseOKPPublicKey(jwk)
		if err == nil {
			t.Error("Expected error for unsupported OKP curve")
		}
	})

	t.Run("missing x parameter", func(t *testing.T) {
		jwk := JWK{
			Kid: "ed-key",
			Kty: "OKP",
			Crv: "Ed25519",
		}

		_, err := parseOKPPublicKey(jwk)
		if err == nil {
			t.Error("Expected error for missing x parameter")
		}
	})

	t.Run("invalid key size", func(t *testing.T) {
		jwk := JWK{
			Kid: "ed-key",
			Kty: "OKP",
			Crv: "Ed25519",
			X:   base64.RawURLEncoding.EncodeToString([]byte("too-short")),
		}

		_, err := parseOKPPublicKey(jwk)
		if err == nil {
			t.Error("Expected error for invalid key size")
		}
	})
}

func TestParseJWKPublicKey(t *testing.T) {
	t.Run("unsupported key type", func(t *testing.T) {
		jwk := JWK{
			Kid: "test-key",
			Kty: "oct",
		}

		_, err := parseJWKPublicKey(jwk)
		if err == nil {
			t.Error("Expected error for unsupported key type")
		}
	})
}

func TestBuildAllowedAlgorithms(t *testing.T) {
	t.Run("empty list returns empty set", func(t *testing.T) {
		result := buildAllowedAlgorithms(nil)
		if len(result) != 0 {
			t.Errorf("Expected empty set for nil input, got %d entries", len(result))
		}
	})

	t.Run("specific algorithms", func(t *testing.T) {
		result := buildAllowedAlgorithms([]string{"RS256", "ES256"})
		if !result["RS256"] {
			t.Error("Expected RS256 to be allowed")
		}
		if !result["ES256"] {
			t.Error("Expected ES256 to be allowed")
		}
		if result["ES384"] {
			t.Error("Expected ES384 to NOT be allowed")
		}
		if result["EdDSA"] {
			t.Error("Expected EdDSA to NOT be allowed")
		}
	})

	t.Run("unsupported algorithms are ignored", func(t *testing.T) {
		result := buildAllowedAlgorithms([]string{"RS256", "HS256", "invalid"})
		if !result["RS256"] {
			t.Error("Expected RS256 to be allowed")
		}
		if result["HS256"] {
			t.Error("Expected HS256 to NOT be allowed (not a supported algorithm)")
		}
		if result["invalid"] {
			t.Error("Expected invalid to NOT be allowed")
		}
	})
}

func TestNewJWTValidator(t *testing.T) {
	t.Run("without JWKS URI", func(t *testing.T) {
		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
		}

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if validator == nil {
			t.Fatal("Expected validator, got nil")
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

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if validator == nil {
			t.Fatal("Expected validator, got nil")
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
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           "http://invalid-host-that-does-not-exist.local/jwks",
		}

		_, err := NewJWTValidator(context.Background(), config)
		if err == nil {
			t.Error("Expected error for invalid JWKS URI")
		}
	})

	t.Run("with allowed algorithms configured", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:            "test-issuer",
			AllowedAlgorithms: []string{"RS256", "ES256"},
		}

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		if !validator.isAlgorithmAllowed("RS256") {
			t.Error("Expected RS256 to be allowed")
		}
		if !validator.isAlgorithmAllowed("ES256") {
			t.Error("Expected ES256 to be allowed")
		}
		if validator.isAlgorithmAllowed("ES384") {
			t.Error("Expected ES384 to NOT be allowed")
		}
	})

	t.Run("empty allowed algorithms returns error", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:   "test-issuer",
			Audience: []string{"test-audience"},
		}

		_, err := NewJWTValidator(context.Background(), config)
		if err == nil {
			t.Fatal("Expected error when AllowedAlgorithms is empty")
		}

		expectedMsg := "AllowedAlgorithms must be explicitly configured"
		if !strings.Contains(err.Error(), expectedMsg) {
			t.Errorf("Expected error containing %q, got: %v", expectedMsg, err)
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

	jwks := createTestJWKS(cert)

	// Create mock JWKS server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	t.Run("valid token", func(t *testing.T) {
		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
		}

		validator, err := NewJWTValidator(context.Background(), config)
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
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
		}

		validator, err := NewJWTValidator(context.Background(), config)
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
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "expected-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
		}

		validator, err := NewJWTValidator(context.Background(), config)
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
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"expected-audience"},
			JwksUri:           server.URL,
		}

		validator, err := NewJWTValidator(context.Background(), config)
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
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
		}

		validator, err := NewJWTValidator(context.Background(), config)
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
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
		}

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		_, err = validator.Validate("not.a.valid.jwt")
		if err == nil {
			t.Error("Expected error for malformed token")
		}
	})
}

func TestValidateECDSA(t *testing.T) {
	tests := []struct {
		name   string
		curve  elliptic.Curve
		crv    string
		method jwt.SigningMethod
	}{
		{"ES256 with P-256", elliptic.P256(), "P-256", jwt.SigningMethodES256},
		{"ES384 with P-384", elliptic.P384(), "P-384", jwt.SigningMethodES384},
		{"ES512 with P-521", elliptic.P521(), "P-521", jwt.SigningMethodES512},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			privateKey, err := generateTestECKeyPair(tt.curve)
			if err != nil {
				t.Fatalf("Failed to generate EC key pair: %v", err)
			}

			jwks := createTestECJWKS("ec-test-key", &privateKey.PublicKey, tt.crv)

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

			validator, err := NewJWTValidator(context.Background(), config)
			if err != nil {
				t.Fatalf("Failed to create validator: %v", err)
			}

			tokenString, err := createTestECToken(privateKey, tt.method, "ec-test-key", []string{"test-audience"}, false)
			if err != nil {
				t.Fatalf("Failed to create EC token: %v", err)
			}

			token, err := validator.Validate(tokenString)
			if err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			if token == nil || !token.Valid {
				t.Error("Expected valid token")
			}
		})
	}

	t.Run("expired ECDSA token", func(t *testing.T) {
		privateKey, err := generateTestECKeyPair(elliptic.P256())
		if err != nil {
			t.Fatalf("Failed to generate EC key pair: %v", err)
		}

		jwks := createTestECJWKS("ec-test-key", &privateKey.PublicKey, "P-256")

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

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		tokenString, err := createTestECToken(privateKey, jwt.SigningMethodES256, "ec-test-key", []string{"test-audience"}, true)
		if err != nil {
			t.Fatalf("Failed to create EC token: %v", err)
		}

		_, err = validator.Validate(tokenString)
		if err == nil {
			t.Error("Expected error for expired ECDSA token")
		}
	})
}

func TestValidateEdDSA(t *testing.T) {
	t.Run("valid EdDSA token", func(t *testing.T) {
		pubKey, privKey, err := generateTestEdDSAKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
		}

		jwks := createTestEdDSAJWKS("ed-test-key", pubKey)

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

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		tokenString, err := createTestEdDSAToken(privKey, "ed-test-key", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create EdDSA token: %v", err)
		}

		token, err := validator.Validate(tokenString)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if token == nil || !token.Valid {
			t.Error("Expected valid token")
		}
	})

	t.Run("expired EdDSA token", func(t *testing.T) {
		pubKey, privKey, err := generateTestEdDSAKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
		}

		jwks := createTestEdDSAJWKS("ed-test-key", pubKey)

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

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		tokenString, err := createTestEdDSAToken(privKey, "ed-test-key", []string{"test-audience"}, true)
		if err != nil {
			t.Fatalf("Failed to create EdDSA token: %v", err)
		}

		_, err = validator.Validate(tokenString)
		if err == nil {
			t.Error("Expected error for expired EdDSA token")
		}
	})
}

func TestValidateWithAllowedAlgorithms(t *testing.T) {
	// Setup: Create keys for each algorithm type
	rsaKey, err := generateTestRSAKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate RSA key pair: %v", err)
	}
	rsaCert, err := generateTestCertificate(rsaKey)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	ecKey, err := generateTestECKeyPair(elliptic.P256())
	if err != nil {
		t.Fatalf("Failed to generate EC key pair: %v", err)
	}

	edPub, edPriv, err := generateTestEdDSAKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
	}

	mixedJWKS := createTestMixedJWKS(rsaCert, &ecKey.PublicKey, edPub)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mixedJWKS)
	}))
	defer server.Close()

	t.Run("RSA allowed, ECDSA rejected", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
			AllowedAlgorithms: []string{"RS256"},
		}

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		// RSA token should succeed
		rsaToken, err := createTestToken(rsaKey, "rsa-key", "test-issuer", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create RSA token: %v", err)
		}
		token, err := validator.Validate(rsaToken)
		if err != nil {
			t.Errorf("Expected RSA token to be valid, got error: %v", err)
		}
		if token == nil || !token.Valid {
			t.Error("Expected valid RSA token")
		}

		// ECDSA token should be rejected
		ecToken, err := createTestECToken(ecKey, jwt.SigningMethodES256, "ec-key", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create EC token: %v", err)
		}
		_, err = validator.Validate(ecToken)
		if err == nil {
			t.Error("Expected error for ECDSA token when only RS256 is allowed")
		}
	})

	t.Run("ECDSA allowed, RSA rejected", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
			AllowedAlgorithms: []string{"ES256"},
		}

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		// ECDSA token should succeed
		ecToken, err := createTestECToken(ecKey, jwt.SigningMethodES256, "ec-key", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create EC token: %v", err)
		}
		token, err := validator.Validate(ecToken)
		if err != nil {
			t.Errorf("Expected EC token to be valid, got error: %v", err)
		}
		if token == nil || !token.Valid {
			t.Error("Expected valid EC token")
		}

		// RSA token should be rejected
		rsaToken, err := createTestToken(rsaKey, "rsa-key", "test-issuer", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create RSA token: %v", err)
		}
		_, err = validator.Validate(rsaToken)
		if err == nil {
			t.Error("Expected error for RSA token when only ES256 is allowed")
		}
	})

	t.Run("EdDSA allowed, others rejected", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
			AllowedAlgorithms: []string{"EdDSA"},
		}

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		// EdDSA token should succeed
		edToken, err := createTestEdDSAToken(edPriv, "ed-key", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create EdDSA token: %v", err)
		}
		token, err := validator.Validate(edToken)
		if err != nil {
			t.Errorf("Expected EdDSA token to be valid, got error: %v", err)
		}
		if token == nil || !token.Valid {
			t.Error("Expected valid EdDSA token")
		}

		// RSA token should be rejected
		rsaToken, err := createTestToken(rsaKey, "rsa-key", "test-issuer", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create RSA token: %v", err)
		}
		_, err = validator.Validate(rsaToken)
		if err == nil {
			t.Error("Expected error for RSA token when only EdDSA is allowed")
		}
	})

	t.Run("multiple algorithms allowed", func(t *testing.T) {
		config := &pb.JWTConfig{
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
		}

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		// All three should succeed
		rsaToken, err := createTestToken(rsaKey, "rsa-key", "test-issuer", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create RSA token: %v", err)
		}
		if _, err := validator.Validate(rsaToken); err != nil {
			t.Errorf("Expected RSA token to be valid, got error: %v", err)
		}

		ecToken, err := createTestECToken(ecKey, jwt.SigningMethodES256, "ec-key", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create EC token: %v", err)
		}
		if _, err := validator.Validate(ecToken); err != nil {
			t.Errorf("Expected EC token to be valid, got error: %v", err)
		}

		edToken, err := createTestEdDSAToken(edPriv, "ed-key", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create EdDSA token: %v", err)
		}
		if _, err := validator.Validate(edToken); err != nil {
			t.Errorf("Expected EdDSA token to be valid, got error: %v", err)
		}
	})

	t.Run("no algorithms configured allows all", func(t *testing.T) {
		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			Issuer:            "test-issuer",
			Audience:          []string{"test-audience"},
			JwksUri:           server.URL,
		}

		validator, err := NewJWTValidator(context.Background(), config)
		if err != nil {
			t.Fatalf("Failed to create validator: %v", err)
		}

		// All three should succeed when no algorithms are configured
		rsaToken, err := createTestToken(rsaKey, "rsa-key", "test-issuer", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create RSA token: %v", err)
		}
		if _, err := validator.Validate(rsaToken); err != nil {
			t.Errorf("Expected RSA token to be valid, got error: %v", err)
		}

		ecToken, err := createTestECToken(ecKey, jwt.SigningMethodES256, "ec-key", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create EC token: %v", err)
		}
		if _, err := validator.Validate(ecToken); err != nil {
			t.Errorf("Expected EC token to be valid, got error: %v", err)
		}

		edToken, err := createTestEdDSAToken(edPriv, "ed-key", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create EdDSA token: %v", err)
		}
		if _, err := validator.Validate(edToken); err != nil {
			t.Errorf("Expected EdDSA token to be valid, got error: %v", err)
		}
	})
}

func TestValidateMixedKeyTypes(t *testing.T) {
	// Create all key types
	rsaKey, err := generateTestRSAKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate RSA key pair: %v", err)
	}
	rsaCert, err := generateTestCertificate(rsaKey)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	ecKey, err := generateTestECKeyPair(elliptic.P256())
	if err != nil {
		t.Fatalf("Failed to generate EC key pair: %v", err)
	}

	edPub, edPriv, err := generateTestEdDSAKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
	}

	mixedJWKS := createTestMixedJWKS(rsaCert, &ecKey.PublicKey, edPub)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mixedJWKS)
	}))
	defer server.Close()

	config := &pb.JWTConfig{
		AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
		Issuer:            "test-issuer",
		Audience:          []string{"test-audience"},
		JwksUri:           server.URL,
	}

	validator, err := NewJWTValidator(context.Background(), config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	t.Run("RSA token with mixed JWKS", func(t *testing.T) {
		tokenString, err := createTestToken(rsaKey, "rsa-key", "test-issuer", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create RSA token: %v", err)
		}

		token, err := validator.Validate(tokenString)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if token == nil || !token.Valid {
			t.Error("Expected valid token")
		}
	})

	t.Run("ECDSA token with mixed JWKS", func(t *testing.T) {
		tokenString, err := createTestECToken(ecKey, jwt.SigningMethodES256, "ec-key", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create EC token: %v", err)
		}

		token, err := validator.Validate(tokenString)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if token == nil || !token.Valid {
			t.Error("Expected valid token")
		}
	})

	t.Run("EdDSA token with mixed JWKS", func(t *testing.T) {
		tokenString, err := createTestEdDSAToken(edPriv, "ed-key", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create EdDSA token: %v", err)
		}

		token, err := validator.Validate(tokenString)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if token == nil || !token.Valid {
			t.Error("Expected valid token")
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

	validator, err := NewJWTValidator(context.Background(), config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	// Create test handler
	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
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

func TestHandleJWTWithECDSA(t *testing.T) {
	// Setup: Create test ECDSA key and JWKS
	ecKey, err := generateTestECKeyPair(elliptic.P256())
	if err != nil {
		t.Fatalf("Failed to generate EC key pair: %v", err)
	}

	jwks := createTestECJWKS("ec-test-key", &ecKey.PublicKey, "P-256")

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

	validator, err := NewJWTValidator(context.Background(), config)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := HandleJWT(validator)
	handler := middleware(nextHandler)

	t.Run("valid ECDSA token via middleware", func(t *testing.T) {
		nextCalled = false
		tokenString, err := createTestECToken(ecKey, jwt.SigningMethodES256, "ec-test-key", []string{"test-audience"}, false)
		if err != nil {
			t.Fatalf("Failed to create EC token: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}

		if !nextCalled {
			t.Error("Next handler should be called for valid ECDSA token")
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

		jwks := createTestJWKS(cert)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(jwks)
		}))
		defer server.Close()

		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			JwksUri:           server.URL,
		}

		validator := &JWTValidator{
			config:            config,
			keys:              make(map[string]interface{}),
			allowedAlgorithms: buildAllowedAlgorithms([]string{"RS256", "ES256", "EdDSA"}),
		}

		err = validator.fetchJWKS(context.Background())
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

	t.Run("fetch with EC keys", func(t *testing.T) {
		ecKey, err := generateTestECKeyPair(elliptic.P256())
		if err != nil {
			t.Fatalf("Failed to generate EC key pair: %v", err)
		}

		jwks := createTestECJWKS("ec-key", &ecKey.PublicKey, "P-256")

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(jwks)
		}))
		defer server.Close()

		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			JwksUri:           server.URL,
		}

		validator := &JWTValidator{
			config:            config,
			keys:              make(map[string]interface{}),
			allowedAlgorithms: buildAllowedAlgorithms([]string{"RS256", "ES256", "EdDSA"}),
		}

		err = validator.fetchJWKS(context.Background())
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		validator.mu.RLock()
		_, exists := validator.keys["ec-key"]
		validator.mu.RUnlock()

		if !exists {
			t.Error("Expected EC key to be loaded")
		}
	})

	t.Run("fetch with EdDSA keys", func(t *testing.T) {
		pubKey, _, err := generateTestEdDSAKeyPair()
		if err != nil {
			t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
		}

		jwks := createTestEdDSAJWKS("ed-key", pubKey)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(jwks)
		}))
		defer server.Close()

		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			JwksUri:           server.URL,
		}

		validator := &JWTValidator{
			config:            config,
			keys:              make(map[string]interface{}),
			allowedAlgorithms: buildAllowedAlgorithms([]string{"RS256", "ES256", "EdDSA"}),
		}

		err = validator.fetchJWKS(context.Background())
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		validator.mu.RLock()
		_, exists := validator.keys["ed-key"]
		validator.mu.RUnlock()

		if !exists {
			t.Error("Expected EdDSA key to be loaded")
		}
	})

	t.Run("server returns error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			JwksUri:           server.URL,
		}

		validator := &JWTValidator{
			config:            config,
			keys:              make(map[string]interface{}),
			allowedAlgorithms: buildAllowedAlgorithms([]string{"RS256", "ES256", "EdDSA"}),
		}

		err := validator.fetchJWKS(context.Background())
		if err == nil {
			t.Error("Expected error for server error status")
		}
	})

	t.Run("invalid JSON response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("invalid json"))
		}))
		defer server.Close()

		config := &pb.JWTConfig{
			AllowedAlgorithms: []string{"RS256", "ES256", "EdDSA"},
			JwksUri:           server.URL,
		}

		validator := &JWTValidator{
			config:            config,
			keys:              make(map[string]interface{}),
			allowedAlgorithms: buildAllowedAlgorithms([]string{"RS256", "ES256", "EdDSA"}),
		}

		err := validator.fetchJWKS(context.Background())
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})
}

func TestIsSupportedSigningMethod(t *testing.T) {
	tests := []struct {
		name     string
		method   jwt.SigningMethod
		expected bool
	}{
		{"RSA RS256", jwt.SigningMethodRS256, true},
		{"RSA RS384", jwt.SigningMethodRS384, true},
		{"RSA RS512", jwt.SigningMethodRS512, true},
		{"ECDSA ES256", jwt.SigningMethodES256, true},
		{"ECDSA ES384", jwt.SigningMethodES384, true},
		{"ECDSA ES512", jwt.SigningMethodES512, true},
		{"EdDSA", jwt.SigningMethodEdDSA, true},
		{"HMAC HS256", jwt.SigningMethodHS256, false},
		{"HMAC HS384", jwt.SigningMethodHS384, false},
		{"HMAC HS512", jwt.SigningMethodHS512, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSupportedSigningMethod(tt.method)
			if result != tt.expected {
				t.Errorf("Expected %v for %s, got %v", tt.expected, tt.name, result)
			}
		})
	}
}
