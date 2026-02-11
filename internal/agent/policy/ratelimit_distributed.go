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
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// DistributedRateLimiter implements rate limiting with shared state via Redis
type DistributedRateLimiter struct {
	config       *pb.DistributedRateLimitConfig
	redisClient  *RedisClient
	localLimiter *RateLimiter // Fallback when Redis is unavailable
	logger       *zap.Logger
}

// NewDistributedRateLimiter creates a new distributed rate limiter
func NewDistributedRateLimiter(
	config *pb.DistributedRateLimitConfig,
	redisClient *RedisClient,
	logger *zap.Logger,
) *DistributedRateLimiter {
	// Create a local fallback limiter
	localConfig := &pb.RateLimitConfig{
		RequestsPerSecond: config.RequestsPerSecond,
		Burst:             config.Burst,
		Key:               config.Key,
	}

	return &DistributedRateLimiter{
		config:       config,
		redisClient:  redisClient,
		localLimiter: NewRateLimiter(localConfig),
		logger:       logger,
	}
}

// Allow checks if a request should be allowed using distributed state
func (d *DistributedRateLimiter) Allow(r *http.Request) (bool, int32, int32, time.Time) {
	// Extract rate limit key
	key := d.extractKey(r)
	redisKey := fmt.Sprintf("novaedge:ratelimit:%s", key)

	limit := d.config.RequestsPerSecond
	burst := d.config.Burst
	if burst <= 0 {
		burst = limit
	}

	// Check if Redis is available
	if d.redisClient == nil || !d.redisClient.IsHealthy() {
		d.logger.Debug("Redis unavailable, using local rate limiter")
		allowed := d.localLimiter.Allow(r)
		remaining := int32(0)
		if allowed {
			remaining = burst - 1
		}
		resetTime := time.Now().Add(time.Second)
		return allowed, limit, remaining, resetTime
	}

	// Use configured algorithm
	switch d.config.Algorithm {
	case "fixed-window":
		return d.fixedWindow(r.Context(), redisKey, limit, burst, r)
	case "token-bucket":
		return d.tokenBucket(r.Context(), redisKey, limit, burst, r)
	default: // "sliding-window" or unspecified
		return d.slidingWindow(r.Context(), redisKey, limit, burst, r)
	}
}

// fixedWindow implements fixed window rate limiting
func (d *DistributedRateLimiter) fixedWindow(ctx context.Context, key string, limit, burst int32, r *http.Request) (bool, int32, int32, time.Time) {
	now := time.Now()
	windowKey := fmt.Sprintf("%s:%d", key, now.Unix())
	windowDuration := time.Second

	pipe := d.redisClient.Client().Pipeline()
	incrCmd := pipe.Incr(ctx, windowKey)
	pipe.Expire(ctx, windowKey, windowDuration*2) // TTL slightly longer than window

	_, err := pipe.Exec(ctx)
	if err != nil {
		d.logger.Warn("Redis pipeline failed, falling back to local", zap.Error(err))
		metrics.RedisErrors.Inc()
		allowed := d.localLimiter.Allow(r)
		return allowed, limit, 0, now.Add(windowDuration)
	}

	count := int32(incrCmd.Val())
	effectiveLimit := burst
	if effectiveLimit < limit {
		effectiveLimit = limit
	}

	remaining := effectiveLimit - count
	if remaining < 0 {
		remaining = 0
	}
	resetTime := now.Truncate(windowDuration).Add(windowDuration)

	allowed := count <= effectiveLimit
	if allowed {
		metrics.DistributedRateLimitAllowed.Inc()
	} else {
		metrics.DistributedRateLimitDenied.Inc()
	}

	return allowed, limit, remaining, resetTime
}

// slidingWindow implements sliding window counter rate limiting
func (d *DistributedRateLimiter) slidingWindow(ctx context.Context, key string, limit, burst int32, r *http.Request) (bool, int32, int32, time.Time) {
	now := time.Now()
	windowDuration := time.Second

	currentWindow := now.Truncate(windowDuration)
	previousWindow := currentWindow.Add(-windowDuration)

	currentKey := fmt.Sprintf("%s:%d", key, currentWindow.Unix())
	previousKey := fmt.Sprintf("%s:%d", key, previousWindow.Unix())

	pipe := d.redisClient.Client().Pipeline()
	currentCmd := pipe.Get(ctx, currentKey)
	previousCmd := pipe.Get(ctx, previousKey)

	_, err := pipe.Exec(ctx)
	// Ignore redis.Nil errors (keys not found)
	if err != nil && err != redis.Nil {
		d.logger.Warn("Redis get failed, falling back to local", zap.Error(err))
		metrics.RedisErrors.Inc()
		allowed := d.localLimiter.Allow(r)
		return allowed, limit, 0, currentWindow.Add(windowDuration)
	}

	var currentCount, previousCount int64
	if val, parseErr := currentCmd.Int64(); parseErr == nil {
		currentCount = val
	}
	if val, parseErr := previousCmd.Int64(); parseErr == nil {
		previousCount = val
	}

	// Calculate sliding window weight
	elapsed := now.Sub(currentWindow)
	weight := 1.0 - (float64(elapsed) / float64(windowDuration))

	effectiveLimit := int64(burst)
	if effectiveLimit < int64(limit) {
		effectiveLimit = int64(limit)
	}

	weightedCount := int64(float64(previousCount)*weight) + currentCount

	if weightedCount >= effectiveLimit {
		metrics.DistributedRateLimitDenied.Inc()
		remaining := effectiveLimit - weightedCount
		if remaining < 0 {
			remaining = 0
		}
		return false, limit, int32(remaining), currentWindow.Add(windowDuration)
	}

	// Increment current window
	incrPipe := d.redisClient.Client().Pipeline()
	incrPipe.Incr(ctx, currentKey)
	incrPipe.Expire(ctx, currentKey, windowDuration*3)

	if _, execErr := incrPipe.Exec(ctx); execErr != nil {
		d.logger.Warn("Redis incr failed", zap.Error(execErr))
		metrics.RedisErrors.Inc()
	}

	remaining := effectiveLimit - weightedCount - 1
	if remaining < 0 {
		remaining = 0
	}

	metrics.DistributedRateLimitAllowed.Inc()
	return true, limit, int32(remaining), currentWindow.Add(windowDuration)
}

// tokenBucket implements token bucket rate limiting via Redis
func (d *DistributedRateLimiter) tokenBucket(ctx context.Context, key string, limit, burst int32, r *http.Request) (bool, int32, int32, time.Time) {
	now := time.Now()
	tokensKey := fmt.Sprintf("%s:tokens", key)
	tsKey := fmt.Sprintf("%s:ts", key)

	// Lua script for atomic token bucket operation (uses millisecond timestamps for sub-second precision)
	script := redis.NewScript(`
		local tokens_key = KEYS[1]
		local ts_key = KEYS[2]
		local rate = tonumber(ARGV[1])
		local burst = tonumber(ARGV[2])
		local now = tonumber(ARGV[3])
		local ttl = tonumber(ARGV[4])

		local last_ts = tonumber(redis.call("GET", ts_key) or now)
		local tokens = tonumber(redis.call("GET", tokens_key) or burst)

		local elapsed = (now - last_ts) / 1000.0
		tokens = math.min(burst, tokens + elapsed * rate)

		if tokens >= 1 then
			tokens = tokens - 1
			redis.call("SET", tokens_key, tostring(tokens), "EX", ttl)
			redis.call("SET", ts_key, tostring(now), "EX", ttl)
			return {1, math.floor(tokens)}
		else
			redis.call("SET", tokens_key, tostring(tokens), "EX", ttl)
			redis.call("SET", ts_key, tostring(now), "EX", ttl)
			return {0, 0}
		end
	`)

	ttl := int(float64(burst) / float64(limit) * 2)
	if ttl < 2 {
		ttl = 2
	}

	result, err := script.Run(ctx, d.redisClient.Client(),
		[]string{tokensKey, tsKey},
		limit, burst, now.UnixMilli(), ttl,
	).Int64Slice()

	if err != nil {
		d.logger.Warn("Redis token bucket script failed, falling back to local", zap.Error(err))
		metrics.RedisErrors.Inc()
		allowed := d.localLimiter.Allow(r)
		return allowed, limit, 0, now.Add(time.Second)
	}

	allowed := result[0] == 1
	remaining := int32(result[1])

	if allowed {
		metrics.DistributedRateLimitAllowed.Inc()
	} else {
		metrics.DistributedRateLimitDenied.Inc()
	}

	return allowed, limit, remaining, now.Add(time.Second)
}

// extractKey extracts the rate limiting key from the request
func (d *DistributedRateLimiter) extractKey(r *http.Request) string {
	keyConfig := d.config.Key
	if keyConfig == "" {
		keyConfig = "source-ip"
	}

	switch {
	case keyConfig == "source-ip":
		return extractClientIP(r)
	case strings.HasPrefix(keyConfig, "header:"):
		headerName := strings.TrimPrefix(keyConfig, "header:")
		value := r.Header.Get(headerName)
		if value == "" {
			return "default"
		}
		return value
	case strings.HasPrefix(keyConfig, "jwt:"):
		// JWT claim extraction would require decoding the token
		// For now, fall back to source IP if JWT is not available
		return extractClientIP(r)
	default:
		return extractClientIP(r)
	}
}

// HandleDistributedRateLimit is HTTP middleware for distributed rate limiting
func HandleDistributedRateLimit(limiter *DistributedRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, limit, remaining, resetTime := limiter.Allow(r)

			// Always set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(int(limit)))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(int(remaining)))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))

			if !allowed {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
