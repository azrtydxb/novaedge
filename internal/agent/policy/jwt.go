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
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// JWTValidator implements JWT validation
type JWTValidator struct {
	config *pb.JWTConfig
	mu     sync.RWMutex
	keys   map[string]interface{} // kid -> key
	jwks   *JWKS
}

// JWKS represents a JSON Web Key Set
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key
type JWK struct {
	Kid string   `json:"kid"`
	Kty string   `json:"kty"`
	Alg string   `json:"alg"`
	Use string   `json:"use"`
	N   string   `json:"n"`
	E   string   `json:"e"`
	X5c []string `json:"x5c"`
}

// NewJWTValidator creates a new JWT validator
func NewJWTValidator(config *pb.JWTConfig) (*JWTValidator, error) {
	v := &JWTValidator{
		config: config,
		keys:   make(map[string]interface{}),
	}

	// If JWKS URI is provided, fetch keys
	if config.JwksUri != "" {
		if err := v.fetchJWKS(); err != nil {
			return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
		}

		// Start periodic refresh
		go v.refreshJWKS()
	}

	return v, nil
}

// fetchJWKS fetches and parses JWKS from the configured URL
func (v *JWTValidator) fetchJWKS() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.config.JwksUri, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var jwks JWKS
	if err := json.Unmarshal(body, &jwks); err != nil {
		return err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	v.jwks = &jwks

	// Parse and store keys
	for _, key := range jwks.Keys {
		if key.Kty == "RSA" {
			pubKey, err := parseRSAPublicKey(key)
			if err != nil {
				continue
			}
			v.keys[key.Kid] = pubKey
		}
	}

	return nil
}

// refreshJWKS periodically refreshes the JWKS
func (v *JWTValidator) refreshJWKS() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if err := v.fetchJWKS(); err != nil {
			// Log error but continue
			continue
		}
	}
}

// Validate validates a JWT token
func (v *JWTValidator) Validate(tokenString string) (*jwt.Token, error) {
	// Parse token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Get key ID from token header
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("token missing kid header")
		}

		// Get key from cache
		v.mu.RLock()
		key, exists := v.keys[kid]
		v.mu.RUnlock()

		if !exists {
			return nil, fmt.Errorf("unknown key ID: %s", kid)
		}

		return key, nil
	})

	if err != nil {
		return nil, err
	}

	// Validate claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}

	// Validate issuer
	if v.config.Issuer != "" {
		iss, ok := claims["iss"].(string)
		if !ok || iss != v.config.Issuer {
			return nil, fmt.Errorf("invalid issuer")
		}
	}

	// Validate audience
	if len(v.config.Audience) > 0 {
		aud, ok := claims["aud"].(string)
		if !ok {
			return nil, fmt.Errorf("missing audience claim")
		}

		validAudience := false
		for _, validAud := range v.config.Audience {
			if aud == validAud {
				validAudience = true
				break
			}
		}

		if !validAudience {
			return nil, fmt.Errorf("invalid audience")
		}
	}

	return token, nil
}

// HandleJWT is HTTP middleware for JWT validation
func HandleJWT(validator *JWTValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				metrics.JWTValidationTotal.WithLabelValues("failure").Inc()
				http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
				return
			}

			// Remove "Bearer " prefix
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == authHeader {
				metrics.JWTValidationTotal.WithLabelValues("failure").Inc()
				http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
				return
			}

			// Validate token
			token, err := validator.Validate(tokenString)
			if err != nil {
				metrics.JWTValidationTotal.WithLabelValues("failure").Inc()
				http.Error(w, fmt.Sprintf("Invalid token: %v", err), http.StatusUnauthorized)
				return
			}

			if !token.Valid {
				metrics.JWTValidationTotal.WithLabelValues("failure").Inc()
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			metrics.JWTValidationTotal.WithLabelValues("success").Inc()

			// Store claims in request context for downstream use
			ctx := context.WithValue(r.Context(), "jwt_claims", token.Claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// parseRSAPublicKey parses an RSA public key from a JWK
func parseRSAPublicKey(key JWK) (*rsa.PublicKey, error) {
	// If x5c (certificate chain) is present, use it
	if len(key.X5c) > 0 {
		certPEM := "-----BEGIN CERTIFICATE-----\n" + key.X5c[0] + "\n-----END CERTIFICATE-----"
		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			return nil, fmt.Errorf("failed to decode certificate")
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}

		rsaKey, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("certificate does not contain RSA public key")
		}

		return rsaKey, nil
	}

	// Otherwise, construct from n and e (not implemented in this basic version)
	return nil, fmt.Errorf("JWK without x5c not supported")
}
