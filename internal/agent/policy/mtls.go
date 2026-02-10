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
	"net/http"
	"regexp"
	"strings"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// MTLSValidator validates client certificate attributes against policy requirements.
type MTLSValidator struct {
	config     *pb.ClientAuthConfig
	cnPatterns []*regexp.Regexp
	logger     *zap.Logger
}

// NewMTLSValidator creates a new mTLS policy validator.
func NewMTLSValidator(config *pb.ClientAuthConfig, logger *zap.Logger) (*MTLSValidator, error) {
	v := &MTLSValidator{
		config: config,
		logger: logger,
	}

	// Pre-compile CN patterns
	for _, pattern := range config.RequiredCNPatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, &MTLSPolicyError{
				Message: "invalid CN pattern: " + pattern,
				Err:     err,
			}
		}
		v.cnPatterns = append(v.cnPatterns, re)
	}

	return v, nil
}

// MTLSPolicyError is returned when mTLS policy validation fails.
type MTLSPolicyError struct {
	Message string
	Err     error
}

func (e *MTLSPolicyError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *MTLSPolicyError) Unwrap() error {
	return e.Err
}

// Middleware returns an HTTP middleware that validates client certificates.
// If the client certificate does not match the policy, it returns 403 Forbidden.
func (v *MTLSValidator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := v.Validate(r); err != nil {
			v.logger.Warn("mTLS policy validation failed",
				zap.String("remote_addr", r.RemoteAddr),
				zap.Error(err),
			)
			http.Error(w, "Forbidden: client certificate does not meet policy requirements", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Validate checks whether the request's client certificate meets the policy requirements.
func (v *MTLSValidator) Validate(r *http.Request) error {
	// If mode is not require or optional, skip validation
	mode := strings.ToLower(v.config.Mode)
	if mode != "require" && mode != "optional" {
		return nil
	}

	// Check if client certificate is present
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		if mode == "require" {
			return &MTLSPolicyError{Message: "client certificate required but not provided"}
		}
		// Optional mode: no cert is acceptable
		return nil
	}

	clientCert := r.TLS.PeerCertificates[0]

	// Validate CN patterns
	if len(v.cnPatterns) > 0 {
		cn := clientCert.Subject.CommonName
		matched := false
		for _, pattern := range v.cnPatterns {
			if pattern.MatchString(cn) {
				matched = true
				break
			}
		}
		if !matched {
			return &MTLSPolicyError{
				Message: "client certificate CN '" + cn + "' does not match any required pattern",
			}
		}
	}

	// Validate required SANs
	requiredSANs := v.config.RequiredSANs
	if len(requiredSANs) > 0 {
		// Collect all SANs from the certificate
		certSANs := make(map[string]bool)
		for _, dns := range clientCert.DNSNames {
			certSANs[dns] = true
		}
		for _, email := range clientCert.EmailAddresses {
			certSANs[email] = true
		}
		for _, uri := range clientCert.URIs {
			certSANs[uri.String()] = true
		}
		for _, ip := range clientCert.IPAddresses {
			certSANs[ip.String()] = true
		}

		for _, required := range requiredSANs {
			if !certSANs[required] {
				return &MTLSPolicyError{
					Message: "client certificate missing required SAN: " + required,
				}
			}
		}
	}

	return nil
}
