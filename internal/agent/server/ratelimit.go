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

package server

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiterConfig defines rate limiting configuration
type RateLimiterConfig struct {
	// RequestsPerMinute is the maximum number of requests allowed per minute
	RequestsPerMinute int
	// Burst is the maximum burst size allowed
	Burst int
}

// DefaultObservabilityRateLimitConfig returns default rate limiting configuration
// for observability endpoints (healthz, ready, metrics)
func DefaultObservabilityRateLimitConfig() RateLimiterConfig {
	return RateLimiterConfig{
		RequestsPerMinute: 100, // 100 requests per minute
		Burst:             10,  // Allow bursts of up to 10 requests
	}
}

// IPRateLimiter manages per-IP rate limiters
type IPRateLimiter struct {
	mu        sync.RWMutex
	limiters  map[string]*rate.Limiter
	config    RateLimiterConfig
	cleanupCh chan struct{}
}

// NewIPRateLimiter creates a new IP-based rate limiter
func NewIPRateLimiter(config RateLimiterConfig) *IPRateLimiter {
	rl := &IPRateLimiter{
		limiters:  make(map[string]*rate.Limiter),
		config:    config,
		cleanupCh: make(chan struct{}),
	}

	// Start cleanup goroutine to remove stale limiters
	go rl.cleanupRoutine()

	return rl
}

// getLimiter returns the rate limiter for a given IP address
func (rl *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.limiters[ip]
	if !exists {
		// Create limiter with rate per second = requests per minute / 60
		ratePerSecond := rate.Limit(float64(rl.config.RequestsPerMinute) / 60.0)
		limiter = rate.NewLimiter(ratePerSecond, rl.config.Burst)
		rl.limiters[ip] = limiter
	}

	return limiter
}

// cleanupRoutine periodically removes stale limiters to prevent memory leaks
func (rl *IPRateLimiter) cleanupRoutine() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.cleanupCh:
			return
		}
	}
}

// cleanup removes limiters that haven't been used recently
func (rl *IPRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Simple cleanup: clear all limiters periodically
	// In production, you might want to track last access time
	if len(rl.limiters) > 10000 {
		rl.limiters = make(map[string]*rate.Limiter)
	}
}

// Stop stops the cleanup routine
func (rl *IPRateLimiter) Stop() {
	close(rl.cleanupCh)
}

// RateLimitMiddleware returns a middleware that rate limits requests by IP
func RateLimitMiddleware(limiter *IPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract client IP
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				// If we can't parse the IP, use the whole RemoteAddr
				ip = r.RemoteAddr
			}

			// Check X-Forwarded-For header for proxied requests
			if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
				// Use the first IP in the X-Forwarded-For chain
				ip = forwardedFor
				if commaIdx := len(ip); commaIdx > 0 {
					for i, c := range ip {
						if c == ',' {
							commaIdx = i
							break
						}
					}
					ip = ip[:commaIdx]
				}
			}

			// Get rate limiter for this IP
			rl := limiter.getLimiter(ip)

			// Check if request is allowed
			if !rl.Allow() {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			// Request allowed, continue
			next.ServeHTTP(w, r)
		})
	}
}
