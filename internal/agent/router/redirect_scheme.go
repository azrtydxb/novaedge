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

package router

import (
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// RedirectSchemeMiddleware redirects HTTP requests to HTTPS (or another scheme)
type RedirectSchemeMiddleware struct {
	enabled    bool
	scheme     string
	port       int
	statusCode int
	exclusions map[string]bool // paths to skip
	logger     *zap.Logger
}

// NewRedirectSchemeMiddleware creates a new redirect scheme middleware from proto config
func NewRedirectSchemeMiddleware(config *pb.RedirectSchemeConfig, logger *zap.Logger) *RedirectSchemeMiddleware {
	rsm := &RedirectSchemeMiddleware{
		exclusions: make(map[string]bool),
		logger:     logger,
	}

	if config == nil || !config.Enabled {
		rsm.enabled = false
		return rsm
	}

	rsm.enabled = true

	// Set scheme, defaulting to "https"
	rsm.scheme = config.Scheme
	if rsm.scheme == "" {
		rsm.scheme = "https"
	}

	// Set port, defaulting to 443
	rsm.port = int(config.Port)
	if rsm.port == 0 {
		rsm.port = 443
	}

	// Set status code, defaulting to 301 (permanent redirect)
	rsm.statusCode = int(config.StatusCode)
	if rsm.statusCode != http.StatusMovedPermanently && rsm.statusCode != http.StatusFound {
		rsm.statusCode = http.StatusMovedPermanently
	}

	// Build exclusion set
	for _, path := range config.Exclusions {
		rsm.exclusions[path] = true
	}

	return rsm
}

// IsEnabled returns whether the redirect middleware is enabled
func (rsm *RedirectSchemeMiddleware) IsEnabled() bool {
	return rsm.enabled
}

// Wrap returns an http.Handler that redirects matching requests
func (rsm *RedirectSchemeMiddleware) Wrap(next http.Handler) http.Handler {
	if !rsm.enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip if already using the target scheme
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == rsm.scheme {
			next.ServeHTTP(w, r)
			return
		}

		// Check if this path is excluded
		if rsm.isExcluded(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Build redirect URL preserving path and query string
		host := r.Host
		// Strip any existing port from the host
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			host = host[:idx]
		}

		// Only include port if it's non-standard for the scheme
		redirectURL := fmt.Sprintf("%s://%s", rsm.scheme, host)
		if !rsm.isDefaultPort() {
			redirectURL = fmt.Sprintf("%s:%d", redirectURL, rsm.port)
		}
		redirectURL += r.URL.RequestURI()

		http.Redirect(w, r, redirectURL, rsm.statusCode)
	})
}

// isExcluded checks if the given path matches any exclusion pattern
func (rsm *RedirectSchemeMiddleware) isExcluded(path string) bool {
	// Check exact match first
	if rsm.exclusions[path] {
		return true
	}

	// Check prefix match for exclusions ending with /
	for exclusion := range rsm.exclusions {
		if strings.HasSuffix(exclusion, "/") && strings.HasPrefix(path, exclusion) {
			return true
		}
	}

	return false
}

// isDefaultPort returns true if the configured port is the default for the scheme
func (rsm *RedirectSchemeMiddleware) isDefaultPort() bool {
	switch rsm.scheme {
	case "https":
		return rsm.port == 443
	case "http":
		return rsm.port == 80
	default:
		return false
	}
}
