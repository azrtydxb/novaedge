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
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	// DefaultCleanupInterval is the default interval between cleanup cycles.
	DefaultCleanupInterval = 5 * time.Minute

	// DefaultCleanupMaxAge is the default maximum age for inactive limiters
	// before they are eligible for removal.
	DefaultCleanupMaxAge = 1 * time.Hour
)

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	config   *pb.RateLimitConfig
	limiters map[string]*rate.Limiter
	lastUsed map[string]time.Time
	mu       sync.RWMutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config *pb.RateLimitConfig) *RateLimiter {
	return &RateLimiter{
		config:   config,
		limiters: make(map[string]*rate.Limiter),
		lastUsed: make(map[string]time.Time),
	}
}

// Allow checks if a request should be allowed
func (rl *RateLimiter) Allow(r *http.Request) bool {
	// Extract key for rate limiting
	key := rl.extractKey(r)

	// Get or create limiter for this key
	limiter := rl.getLimiter(key)

	// Check if request is allowed
	allowed := limiter.Allow()

	// Update last used timestamp on every access
	now := time.Now()
	rl.mu.Lock()
	rl.lastUsed[key] = now
	rl.mu.Unlock()

	// Record metrics
	if allowed {
		metrics.RateLimitAllowed.Inc()
	} else {
		metrics.RateLimitDenied.Inc()
	}

	return allowed
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
	rl.lastUsed[key] = time.Now()

	return limiter
}

// extractKey extracts the rate limiting key from the request
func (rl *RateLimiter) extractKey(r *http.Request) string {
	switch rl.config.Key {
	case "source-ip", "":
		// Extract source IP
		return extractClientIP(r)

	default:
		// Try to extract from header
		value := r.Header.Get(rl.config.Key)
		if value == "" {
			return "default"
		}
		return value
	}
}

// HandleRateLimit is HTTP middleware for rate limiting
func HandleRateLimit(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(r) {
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

// Cleanup removes inactive limiters that haven't been accessed within maxAge.
func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for key, lastAccess := range rl.lastUsed {
		if lastAccess.Before(cutoff) {
			delete(rl.limiters, key)
			delete(rl.lastUsed, key)
			removed++
		}
	}

	if removed > 0 {
		metrics.RateLimiterCleanupsTotal.Add(float64(removed))
	}
	metrics.RateLimiterActiveCount.Set(float64(len(rl.limiters)))
}

// StartCleanup runs periodic cleanup in a background goroutine. It removes
// inactive limiters every interval that have not been used within maxAge.
// The goroutine exits when the provided context is cancelled.
func (rl *RateLimiter) StartCleanup(ctx context.Context, interval, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rl.Cleanup(maxAge)
			}
		}
	}()
}

// ActiveCount returns the current number of active rate limiters.
func (rl *RateLimiter) ActiveCount() int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return len(rl.limiters)
}
