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

// CORS implements Cross-Origin Resource Sharing policy
type CORS struct {
	config *pb.CORSConfig
}

// NewCORS creates a new CORS policy
func NewCORS(config *pb.CORSConfig) *CORS {
	return &CORS{
		config: config,
	}
}

// HandleCORS is HTTP middleware for CORS
func HandleCORS(cors *CORS) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			if origin != "" && cors.isOriginAllowed(origin) {
				// Set CORS headers
				w.Header().Set("Access-Control-Allow-Origin", origin)

				// Set allowed credentials
				if cors.config.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}

				// Set exposed headers
				if len(cors.config.ExposeHeaders) > 0 {
					w.Header().Set("Access-Control-Expose-Headers", strings.Join(cors.config.ExposeHeaders, ", "))
				}

				// Set max age
				if cors.config.MaxAgeSeconds > 0 {
					w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", cors.config.MaxAgeSeconds))
				}

				// Handle preflight request
				if r.Method == http.MethodOptions {
					metrics.CORSRequestsTotal.WithLabelValues("preflight").Inc()

					// Set allowed methods
					if len(cors.config.AllowMethods) > 0 {
						w.Header().Set("Access-Control-Allow-Methods", strings.Join(cors.config.AllowMethods, ", "))
					}

					// Set allowed headers
					if len(cors.config.AllowHeaders) > 0 {
						w.Header().Set("Access-Control-Allow-Headers", strings.Join(cors.config.AllowHeaders, ", "))
					}

					w.WriteHeader(http.StatusNoContent)
					return
				}

				metrics.CORSRequestsTotal.WithLabelValues("simple").Inc()
			}

			// Continue with request
			next.ServeHTTP(w, r)
		})
	}
}

// isOriginAllowed checks if an origin is allowed
func (c *CORS) isOriginAllowed(origin string) bool {
	// If no origins specified, allow all
	if len(c.config.AllowOrigins) == 0 {
		return true
	}

	// Check for wildcard
	for _, allowed := range c.config.AllowOrigins {
		if allowed == "*" {
			return true
		}
		if allowed == origin {
			return true
		}
		// Support simple wildcard patterns like https://*.example.com
		if strings.Contains(allowed, "*") {
			if matchWildcard(allowed, origin) {
				return true
			}
		}
	}

	return false
}

// matchWildcard performs simple wildcard matching
func matchWildcard(pattern, value string) bool {
	// Simple implementation: only support * as prefix wildcard
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(value, suffix)
	}
	return pattern == value
}
