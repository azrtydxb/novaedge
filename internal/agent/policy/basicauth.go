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
	"crypto/md5" //nolint:gosec // G501: MD5 is required by Apache apr1 password hash format (RFC-compatible htpasswd)
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// htpasswdEntry represents a parsed htpasswd credential line.
type htpasswdEntry struct {
	username string
	hash     string
	hashType string // "bcrypt", "sha256", "md5"
}

// BasicAuthValidator validates HTTP Basic Authentication credentials.
type BasicAuthValidator struct {
	config  *pb.BasicAuthConfig
	mu      sync.RWMutex
	entries map[string]htpasswdEntry // username -> entry
}

// NewBasicAuthValidator creates a new basic auth validator from config.
func NewBasicAuthValidator(config *pb.BasicAuthConfig) (*BasicAuthValidator, error) {
	v := &BasicAuthValidator{
		config:  config,
		entries: make(map[string]htpasswdEntry),
	}

	if err := v.parseHtpasswd(config.Htpasswd); err != nil {
		return nil, fmt.Errorf("failed to parse htpasswd: %w", err)
	}

	return v, nil
}

// parseHtpasswd parses htpasswd-formatted credential lines.
func (v *BasicAuthValidator) parseHtpasswd(htpasswd string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	lines := strings.Split(htpasswd, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		username := parts[0]
		hash := parts[1]

		entry := htpasswdEntry{
			username: username,
			hash:     hash,
			hashType: detectHashType(hash),
		}

		v.entries[username] = entry
	}

	if len(v.entries) == 0 {
		return fmt.Errorf("no valid credential entries found")
	}

	return nil
}

// detectHashType identifies the password hash algorithm from the hash string.
func detectHashType(hash string) string {
	switch {
	case strings.HasPrefix(hash, "$2y$") || strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$"):
		return "bcrypt"
	case strings.HasPrefix(hash, "{SHA256}"):
		return "sha256"
	case strings.HasPrefix(hash, "$apr1$"):
		return "md5"
	default:
		return "bcrypt" // default fallback
	}
}

// Validate checks if the username/password combination is valid.
func (v *BasicAuthValidator) Validate(username, password string) bool {
	v.mu.RLock()
	entry, exists := v.entries[username]
	v.mu.RUnlock()

	if !exists {
		return false
	}

	switch entry.hashType {
	case "bcrypt":
		return bcrypt.CompareHashAndPassword([]byte(entry.hash), []byte(password)) == nil
	case "sha256":
		return verifySHA256(password, entry.hash)
	case "md5":
		return verifyAPR1MD5(password, entry.hash)
	default:
		return false
	}
}

// verifySHA256 verifies a password against a {SHA256} hash.
func verifySHA256(password, hash string) bool {
	// Format: {SHA256}base64encodedHash
	if !strings.HasPrefix(hash, "{SHA256}") {
		return false
	}
	encodedHash := strings.TrimPrefix(hash, "{SHA256}")

	storedHash, err := base64.StdEncoding.DecodeString(encodedHash)
	if err != nil {
		return false
	}

	computed := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare(computed[:], storedHash) == 1
}

// verifyAPR1MD5 verifies a password against an Apache apr1 MD5 hash.
// Format: $apr1$salt$hash
func verifyAPR1MD5(password, hash string) bool {
	if !strings.HasPrefix(hash, "$apr1$") {
		return false
	}

	parts := strings.Split(hash, "$")
	if len(parts) < 4 {
		return false
	}
	salt := parts[2]

	computed := computeAPR1MD5(password, salt)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(hash)) == 1
}

// computeAPR1MD5 computes an Apache apr1 MD5 hash.
func computeAPR1MD5(password, salt string) string {
	// Apache APR1 MD5 algorithm
	const itoa64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	pass := []byte(password)
	saltBytes := []byte(salt)

	// Start digest A
	ctx := md5.New() //nolint:gosec // G401: MD5 is required by Apache apr1 password hash format (RFC-compatible htpasswd)
	ctx.Write(pass)
	ctx.Write([]byte("$apr1$"))
	ctx.Write(saltBytes)

	// Digest B
	ctx1 := md5.New() //nolint:gosec // G401: MD5 is required by Apache apr1 password hash format (RFC-compatible htpasswd)
	ctx1.Write(pass)
	ctx1.Write(saltBytes)
	ctx1.Write(pass)
	final := ctx1.Sum(nil)

	for ii := len(pass); ii > 0; ii -= 16 {
		if ii > 16 {
			ctx.Write(final)
		} else {
			ctx.Write(final[:ii])
		}
	}

	for ii := len(pass); ii > 0; ii >>= 1 {
		if ii&1 != 0 {
			ctx.Write([]byte{0})
		} else {
			ctx.Write(pass[:1])
		}
	}

	final = ctx.Sum(nil)

	// 1000 rounds
	for i := 0; i < 1000; i++ {
		ctx1 = md5.New() //nolint:gosec // G401: MD5 is required by Apache apr1 password hash format (RFC-compatible htpasswd)
		if i&1 != 0 {
			ctx1.Write(pass)
		} else {
			ctx1.Write(final)
		}
		if i%3 != 0 {
			ctx1.Write(saltBytes)
		}
		if i%7 != 0 {
			ctx1.Write(pass)
		}
		if i&1 != 0 {
			ctx1.Write(final)
		} else {
			ctx1.Write(pass)
		}
		final = ctx1.Sum(nil)
	}

	// Encode
	encode := func(a, b, c byte, n int) string {
		v := uint(a)<<16 | uint(b)<<8 | uint(c)
		result := make([]byte, 0, n)
		for ; n > 0; n-- {
			result = append(result, itoa64[v&0x3f])
			v >>= 6
		}
		return string(result)
	}

	result := "$apr1$" + salt + "$"
	result += encode(final[0], final[6], final[12], 4)
	result += encode(final[1], final[7], final[13], 4)
	result += encode(final[2], final[8], final[14], 4)
	result += encode(final[3], final[9], final[15], 4)
	result += encode(final[4], final[10], final[5], 4)
	result += encode(0, 0, final[11], 2)

	return result
}

// GenerateBcryptHash generates a bcrypt hash for a password (utility function).
func GenerateBcryptHash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to generate bcrypt hash: %w", err)
	}
	return string(hash), nil
}

// GenerateSHA256Hash generates a {SHA256} hash for a password (utility function).
func GenerateSHA256Hash(password string) string {
	hash := sha256.Sum256([]byte(password))
	return "{SHA256}" + base64.StdEncoding.EncodeToString(hash[:])
}

// GenerateAPR1MD5Hash generates an $apr1$ hash for a password (utility function).
func GenerateAPR1MD5Hash(password string) string {
	salt := make([]byte, 8)
	_, _ = rand.Read(salt)
	const itoa64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	saltStr := ""
	for _, b := range salt {
		saltStr += string(itoa64[int(b)%len(itoa64)])
	}
	return computeAPR1MD5(password, saltStr)
}

// HandleBasicAuth returns HTTP middleware that enforces Basic Auth.
func HandleBasicAuth(validator *BasicAuthValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username, password, ok := r.BasicAuth()
			if !ok {
				metrics.BasicAuthTotal.WithLabelValues("failure").Inc()
				realm := validator.config.Realm
				if realm == "" {
					realm = "Restricted"
				}
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm=%q`, realm))
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			if !validator.Validate(username, password) {
				metrics.BasicAuthTotal.WithLabelValues("failure").Inc()
				realm := validator.config.Realm
				if realm == "" {
					realm = "Restricted"
				}
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm=%q`, realm))
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			metrics.BasicAuthTotal.WithLabelValues("success").Inc()

			// Strip Authorization header if configured
			if validator.config.StripAuth {
				r.Header.Del("Authorization")
			}

			// Add authenticated user info header
			r.Header.Set("X-Auth-User", username)

			next.ServeHTTP(w, r)
		})
	}
}
