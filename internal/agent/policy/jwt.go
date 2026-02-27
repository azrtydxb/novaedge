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
	"errors"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)
var (
	errAllowedAlgorithmsMustBeExplicitlyConfiguredRefusingToAllow = errors.New("AllowedAlgorithms must be explicitly configured; refusing to allow all algorithms by default")
	errJWKSEndpointReturnedStatus = errors.New("JWKS endpoint returned status")
	errAlgorithmNoneNotAllowed = errors.New(`algorithm "none" is not allowed`)
	errUnexpectedSigningMethod = errors.New("unexpected signing method")
	errAlgorithm2 = errors.New("algorithm")
	errTokenMissingKidHeader = errors.New("token missing kid header")
	errUnknownKeyID = errors.New("unknown key ID")
	errInvalidClaimsType = errors.New("invalid claims type")
	errTokenHasBeenRevoked = errors.New("token has been revoked")
	errInvalidIssuer = errors.New("invalid issuer")
	errMissingAudienceClaim = errors.New("missing audience claim")
	errInvalidAudience = errors.New("invalid audience")
	errUnsupportedKeyType = errors.New("unsupported key type")
	errFailedToDecodeCertificate = errors.New("failed to decode certificate")
	errCertificateDoesNotContainRSAPublicKey = errors.New("certificate does not contain RSA public key")
	errRSAJWKMissingNOrEParameter = errors.New("RSA JWK missing n or e parameter")
	errECJWKMissingCrvXOrYParameter = errors.New("EC JWK missing crv, x, or y parameter")
	errUnsupportedECCurve = errors.New("unsupported EC curve")
	errUnsupportedOKPCurve = errors.New("unsupported OKP curve")
	errOKPJWKMissingXParameter = errors.New("OKP JWK missing x parameter")
	errInvalidEd25519PublicKeySizeGot = errors.New("invalid Ed25519 public key size: got")
)


// jwtClaimsKey is a typed context key for storing JWT claims, avoiding SA1029.
type jwtClaimsKey struct{}

// maxJWKSResponseSize is the maximum size (1 MB) for a JWKS HTTP response.
// This prevents OOM if a malicious or misconfigured endpoint returns a huge payload.
const maxJWKSResponseSize = 1 << 20

// supportedAlgorithms lists all JWT signing algorithms the validator supports.
var supportedAlgorithms = map[string]bool{
	"RS256": true,
	"RS384": true,
	"RS512": true,
	"ES256": true,
	"ES384": true,
	"ES512": true,
	"EdDSA": true,
}

// JWTValidator implements JWT validation
type JWTValidator struct {
	config            *pb.JWTConfig
	mu                sync.RWMutex
	keys              map[string]interface{} // kid -> key
	jwks              *JWKS
	blacklist         *TokenBlacklist
	allowedAlgorithms map[string]bool // set of allowed algorithm names
	httpClient        *http.Client
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
	// EC key fields
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	// OKP (Ed25519) key fields - X is reused from EC fields above
}

// JWTValidatorOption configures a JWTValidator.
type JWTValidatorOption func(*JWTValidator)

// WithHTTPClient sets the HTTP client used for JWKS fetching.
func WithHTTPClient(c *http.Client) JWTValidatorOption {
	return func(v *JWTValidator) {
		v.httpClient = c
	}
}

// NewJWTValidator creates a new JWT validator
func NewJWTValidator(ctx context.Context, config *pb.JWTConfig, opts ...JWTValidatorOption) (*JWTValidator, error) {
	v := &JWTValidator{
		config:     config,
		keys:       make(map[string]interface{}),
		blacklist:  NewTokenBlacklist(),
		httpClient: NewSSRFProtectedClient(10 * time.Second),
	}

	for _, opt := range opts {
		opt(v)
	}

	// Require explicit algorithm configuration to prevent algorithm confusion attacks
	if len(config.GetAllowedAlgorithms()) == 0 {
		return nil, errAllowedAlgorithmsMustBeExplicitlyConfiguredRefusingToAllow
	}

	// Build allowed algorithms set
	v.allowedAlgorithms = buildAllowedAlgorithms(config.GetAllowedAlgorithms())

	// If JWKS URI is provided, fetch keys
	if config.JwksUri != "" {
		if err := v.fetchJWKS(ctx); err != nil {
			return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
		}

		// Start periodic refresh
		go v.refreshJWKS(ctx)
	}

	return v, nil
}

// buildAllowedAlgorithms constructs a set of allowed algorithm names from the
// provided list. Only algorithms present in supportedAlgorithms are included.
func buildAllowedAlgorithms(algorithms []string) map[string]bool {
	result := make(map[string]bool, len(algorithms))
	for _, alg := range algorithms {
		if supportedAlgorithms[alg] {
			result[alg] = true
		}
	}
	return result
}

// fetchJWKS fetches and parses JWKS from the configured URL
func (v *JWTValidator) fetchJWKS(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.config.JwksUri, nil)
	if err != nil {
		return err
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %d", errJWKSEndpointReturnedStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSResponseSize))
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

	// Parse and store keys based on key type
	for _, key := range jwks.Keys {
		parsedKey, parseErr := parseJWKPublicKey(key)
		if parseErr != nil {
			continue
		}
		v.keys[key.Kid] = parsedKey
	}

	return nil
}

// refreshJWKS periodically refreshes the JWKS
func (v *JWTValidator) refreshJWKS(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if err := v.fetchJWKS(ctx); err != nil {
			// Log error but continue
			continue
		}
	}
}

// isAlgorithmAllowed checks whether the given algorithm is in the allowed set.
func (v *JWTValidator) isAlgorithmAllowed(alg string) bool {
	return v.allowedAlgorithms[alg]
}

// isSupportedSigningMethod checks whether the token's signing method is one of
// the supported types (RSA, ECDSA, or EdDSA).
func isSupportedSigningMethod(method jwt.SigningMethod) bool {
	switch method.(type) {
	case *jwt.SigningMethodRSA:
		return true
	case *jwt.SigningMethodECDSA:
		return true
	case *jwt.SigningMethodEd25519:
		return true
	default:
		return false
	}
}

// Validate validates a JWT token
func (v *JWTValidator) Validate(tokenString string) (*jwt.Token, error) {
	// Parse token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Explicitly reject the "none" algorithm to prevent algorithm confusion attacks
		if algHeader, ok := token.Header["alg"].(string); ok && strings.EqualFold(algHeader, "none") {
			return nil, errAlgorithmNoneNotAllowed
		}

		// Verify the signing method is a supported type
		if !isSupportedSigningMethod(token.Method) {
			return nil, fmt.Errorf("%w: %v", errUnexpectedSigningMethod, token.Header["alg"])
		}

		// Check against the configured allowed algorithms
		alg := token.Method.Alg()
		if !v.isAlgorithmAllowed(alg) {
			return nil, fmt.Errorf("%w: %s is not allowed", errAlgorithm2, alg)
		}

		// Get key ID from token header
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, errTokenMissingKidHeader
		}

		// Get key from cache
		v.mu.RLock()
		key, exists := v.keys[kid]
		v.mu.RUnlock()

		if !exists {
			return nil, fmt.Errorf("%w: %s", errUnknownKeyID, kid)
		}

		return key, nil
	})

	if err != nil {
		return nil, err
	}

	// Validate claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errInvalidClaimsType
	}

	// Check blacklist using the jti (JWT ID) claim
	if jti, jtiOK := claims["jti"].(string); jtiOK && jti != "" {
		if v.blacklist.IsBlacklisted(jti) {
			metrics.JWTBlockedTotal.Inc()
			return nil, errTokenHasBeenRevoked
		}
	}

	// Validate issuer
	if v.config.Issuer != "" {
		iss, ok := claims["iss"].(string)
		if !ok || iss != v.config.Issuer {
			return nil, errInvalidIssuer
		}
	}

	// Validate audience
	if len(v.config.Audience) > 0 {
		aud, ok := claims["aud"].(string)
		if !ok {
			return nil, errMissingAudienceClaim
		}

		validAudience := false
		for _, validAud := range v.config.Audience {
			if aud == validAud {
				validAudience = true
				break
			}
		}

		if !validAudience {
			return nil, errInvalidAudience
		}
	}

	return token, nil
}

// Revoke adds a token to the blacklist by its jti claim. The token will be
// rejected by Validate until the given expiry time has passed.
func (v *JWTValidator) Revoke(jti string, expiry time.Time) {
	v.blacklist.Add(jti, expiry)
}

// Blacklist returns the token blacklist associated with this validator.
func (v *JWTValidator) Blacklist() *TokenBlacklist {
	return v.blacklist
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
			ctx := context.WithValue(r.Context(), jwtClaimsKey{}, token.Claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// parseJWKPublicKey parses a public key from a JWK, supporting RSA, EC, and OKP key types.
func parseJWKPublicKey(key JWK) (interface{}, error) {
	switch key.Kty {
	case "RSA":
		return parseRSAPublicKey(key)
	case "EC":
		return parseECPublicKey(key)
	case "OKP":
		return parseOKPPublicKey(key)
	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedKeyType, key.Kty)
	}
}

// parseRSAPublicKey parses an RSA public key from a JWK
func parseRSAPublicKey(key JWK) (*rsa.PublicKey, error) {
	// If x5c (certificate chain) is present, use it
	if len(key.X5c) > 0 {
		certPEM := "-----BEGIN CERTIFICATE-----\n" + key.X5c[0] + "\n-----END CERTIFICATE-----"
		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			return nil, errFailedToDecodeCertificate
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}

		rsaKey, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, errCertificateDoesNotContainRSAPublicKey
		}

		return rsaKey, nil
	}

	// Construct from n and e parameters
	if key.N == "" || key.E == "" {
		return nil, errRSAJWKMissingNOrEParameter
	}

	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode RSA n parameter: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode RSA e parameter: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}

// parseECPublicKey parses an ECDSA public key from a JWK
func parseECPublicKey(key JWK) (*ecdsa.PublicKey, error) {
	if key.Crv == "" || key.X == "" || key.Y == "" {
		return nil, errECJWKMissingCrvXOrYParameter
	}

	var curve elliptic.Curve
	switch key.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedECCurve, key.Crv)
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(key.X)
	if err != nil {
		return nil, fmt.Errorf("failed to decode EC x parameter: %w", err)
	}

	yBytes, err := base64.RawURLEncoding.DecodeString(key.Y)
	if err != nil {
		return nil, fmt.Errorf("failed to decode EC y parameter: %w", err)
	}

	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)

	return &ecdsa.PublicKey{
		Curve: curve,
		X:     x,
		Y:     y,
	}, nil
}

// parseOKPPublicKey parses an Ed25519 public key from a JWK with key type "OKP"
func parseOKPPublicKey(key JWK) (ed25519.PublicKey, error) {
	if key.Crv != "Ed25519" {
		return nil, fmt.Errorf("%w: %s (only Ed25519 is supported)", errUnsupportedOKPCurve, key.Crv)
	}

	if key.X == "" {
		return nil, errOKPJWKMissingXParameter
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(key.X)
	if err != nil {
		return nil, fmt.Errorf("failed to decode OKP x parameter: %w", err)
	}

	if len(xBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: %d, want %d", errInvalidEd25519PublicKeySizeGot, len(xBytes), ed25519.PublicKeySize)
	}

	return ed25519.PublicKey(xBytes), nil
}
