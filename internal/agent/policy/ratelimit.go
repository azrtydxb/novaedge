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
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	config   *pb.RateLimitConfig
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config *pb.RateLimitConfig) *RateLimiter {
	return &RateLimiter{
		config:   config,
		limiters: make(map[string]*rate.Limiter),
	}
}

// Allow checks if a request should be allowed
func (rl *RateLimiter) Allow(r *http.Request) (bool, error) {
	// Extract key for rate limiting
	key, err := rl.extractKey(r)
	if err != nil {
		return false, err
	}

	// Get or create limiter for this key
	limiter := rl.getLimiter(key)

	// Check if request is allowed
	allowed := limiter.Allow()

	// Record metrics
	if allowed {
		metrics.RateLimitAllowed.Inc()
	} else {
		metrics.RateLimitDenied.Inc()
	}

	return allowed, nil
}

// getLimiter gets or creates a rate limiter for a specific key
func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	rl.mu.RLock()
	limiter, exists := rl.limiters[key]
	rl.mu.RUnlock()

	if exists {
		return limiter
	}

	// Create new limiter
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists := rl.limiters[key]; exists {
		return limiter
	}

	// Create limiter with configured rate and burst
	rps := rate.Limit(rl.config.RequestsPerSecond)
	burst := int(rl.config.Burst)
	if burst == 0 {
		burst = int(rl.config.RequestsPerSecond) // Default burst = RPS
	}

	limiter = rate.NewLimiter(rps, burst)
	rl.limiters[key] = limiter

	return limiter
}

// extractKey extracts the rate limiting key from the request
func (rl *RateLimiter) extractKey(r *http.Request) (string, error) {
	switch rl.config.Key {
	case "source-ip", "":
		// Extract source IP
		ip := extractClientIP(r)
		return ip, nil

	default:
		// Try to extract from header
		value := r.Header.Get(rl.config.Key)
		if value == "" {
			return "default", nil
		}
		return value, nil
	}
}

// HandleRateLimit is HTTP middleware for rate limiting
func HandleRateLimit(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, err := limiter.Allow(r)
			if err != nil {
				http.Error(w, "Rate limit error", http.StatusInternalServerError)
				return
			}

			if !allowed {
				// Set rate limit headers
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limiter.config.RequestsPerSecond))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("Retry-After", "1")

				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			// Request allowed, continue
			next.ServeHTTP(w, r)
		})
	}
}

// Cleanup removes inactive limiters (called periodically)
func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// In a production system, you'd track last access time
	// and remove limiters that haven't been used recently
	// For simplicity, we keep all limiters for now
}
