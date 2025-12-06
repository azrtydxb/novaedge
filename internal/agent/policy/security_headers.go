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
	"fmt"
	"net/http"
	"strings"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// SecurityHeaders implements security headers policy
type SecurityHeaders struct {
	config *pb.SecurityHeadersConfig
}

// NewSecurityHeaders creates a new SecurityHeaders policy
func NewSecurityHeaders(config *pb.SecurityHeadersConfig) *SecurityHeaders {
	return &SecurityHeaders{
		config: config,
	}
}

// HandleSecurityHeaders is HTTP middleware for applying security headers
func HandleSecurityHeaders(sh *SecurityHeaders) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Apply security headers to response
			sh.applyHeaders(w)

			// Track security headers applied
			metrics.SecurityHeadersAppliedTotal.Inc()

			// Continue with request
			next.ServeHTTP(w, r)
		})
	}
}

// applyHeaders applies all configured security headers to the response
func (sh *SecurityHeaders) applyHeaders(w http.ResponseWriter) {
	if sh.config == nil {
		return
	}

	// HSTS (HTTP Strict Transport Security)
	if sh.config.Hsts != nil && sh.config.Hsts.Enabled {
		w.Header().Set("Strict-Transport-Security", sh.buildHSTSValue())
	}

	// Content-Security-Policy
	if sh.config.ContentSecurityPolicy != "" {
		w.Header().Set("Content-Security-Policy", sh.config.ContentSecurityPolicy)
	}

	// X-Frame-Options
	if sh.config.XFrameOptions != "" {
		w.Header().Set("X-Frame-Options", sh.config.XFrameOptions)
	}

	// X-Content-Type-Options
	if sh.config.XContentTypeOptions {
		w.Header().Set("X-Content-Type-Options", "nosniff")
	}

	// X-XSS-Protection
	if sh.config.XXssProtection != "" {
		w.Header().Set("X-XSS-Protection", sh.config.XXssProtection)
	}

	// Referrer-Policy
	if sh.config.ReferrerPolicy != "" {
		w.Header().Set("Referrer-Policy", sh.config.ReferrerPolicy)
	}

	// Permissions-Policy
	if sh.config.PermissionsPolicy != "" {
		w.Header().Set("Permissions-Policy", sh.config.PermissionsPolicy)
	}

	// Cross-Origin-Embedder-Policy
	if sh.config.CrossOriginEmbedderPolicy != "" {
		w.Header().Set("Cross-Origin-Embedder-Policy", sh.config.CrossOriginEmbedderPolicy)
	}

	// Cross-Origin-Opener-Policy
	if sh.config.CrossOriginOpenerPolicy != "" {
		w.Header().Set("Cross-Origin-Opener-Policy", sh.config.CrossOriginOpenerPolicy)
	}

	// Cross-Origin-Resource-Policy
	if sh.config.CrossOriginResourcePolicy != "" {
		w.Header().Set("Cross-Origin-Resource-Policy", sh.config.CrossOriginResourcePolicy)
	}
}

// buildHSTSValue builds the HSTS header value from config
func (sh *SecurityHeaders) buildHSTSValue() string {
	if sh.config.Hsts == nil {
		return ""
	}

	var parts []string

	// max-age is required
	maxAge := sh.config.Hsts.MaxAgeSeconds
	if maxAge <= 0 {
		maxAge = 31536000 // Default 1 year
	}
	parts = append(parts, fmt.Sprintf("max-age=%d", maxAge))

	// includeSubDomains
	if sh.config.Hsts.IncludeSubdomains {
		parts = append(parts, "includeSubDomains")
	}

	// preload
	if sh.config.Hsts.Preload {
		parts = append(parts, "preload")
	}

	return strings.Join(parts, "; ")
}

// DefaultSecurityHeadersConfig returns a secure default configuration
func DefaultSecurityHeadersConfig() *pb.SecurityHeadersConfig {
	return &pb.SecurityHeadersConfig{
		Hsts: &pb.HSTSConfig{
			Enabled:           true,
			MaxAgeSeconds:     31536000, // 1 year
			IncludeSubdomains: true,
			Preload:           false,
		},
		XFrameOptions:       "DENY",
		XContentTypeOptions: true,
		XXssProtection:      "1; mode=block",
		ReferrerPolicy:      "strict-origin-when-cross-origin",
	}
}

// StrictSecurityHeadersConfig returns a strict security configuration
// suitable for high-security applications
func StrictSecurityHeadersConfig() *pb.SecurityHeadersConfig {
	return &pb.SecurityHeadersConfig{
		Hsts: &pb.HSTSConfig{
			Enabled:           true,
			MaxAgeSeconds:     63072000, // 2 years
			IncludeSubdomains: true,
			Preload:           true,
		},
		ContentSecurityPolicy:     "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
		XFrameOptions:             "DENY",
		XContentTypeOptions:       true,
		XXssProtection:            "1; mode=block",
		ReferrerPolicy:            "no-referrer",
		PermissionsPolicy:         "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()",
		CrossOriginEmbedderPolicy: "require-corp",
		CrossOriginOpenerPolicy:   "same-origin",
		CrossOriginResourcePolicy: "same-origin",
	}
}
