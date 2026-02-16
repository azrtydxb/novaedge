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
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// forwardAuthCacheEntry stores a cached auth decision.
type forwardAuthCacheEntry struct {
	allowed   bool
	headers   http.Header
	expiresAt time.Time
}

// ForwardAuthHandler delegates authentication to an external service.
type ForwardAuthHandler struct {
	config     *pb.ForwardAuthConfig
	logger     *zap.Logger
	httpClient *http.Client
	mu         sync.RWMutex
	cache      map[string]forwardAuthCacheEntry
}

// ForwardAuthOption configures a ForwardAuthHandler.
type ForwardAuthOption func(*ForwardAuthHandler)

// WithForwardAuthHTTPClient sets the HTTP client used for forward auth requests.
func WithForwardAuthHTTPClient(c *http.Client) ForwardAuthOption {
	return func(h *ForwardAuthHandler) {
		h.httpClient = c
	}
}

// NewForwardAuthHandler creates a new forward auth handler.
func NewForwardAuthHandler(ctx context.Context, config *pb.ForwardAuthConfig, logger *zap.Logger, opts ...ForwardAuthOption) *ForwardAuthHandler {
	timeout := 5 * time.Second
	if config.TimeoutMs > 0 {
		timeout = time.Duration(config.TimeoutMs) * time.Millisecond
	}

	h := &ForwardAuthHandler{
		config: config,
		logger: logger,
		httpClient: &http.Client{
			Transport: NewSSRFProtectedTransport(),
			Timeout:   timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects
			},
		},
		cache: make(map[string]forwardAuthCacheEntry),
	}

	for _, opt := range opts {
		opt(h)
	}

	// Start cache cleanup goroutine if caching is enabled
	if config.CacheTtlSeconds > 0 {
		go h.cleanupCache(ctx)
	}

	return h
}

// cacheKey computes a cache key based on the relevant request attributes.
func (h *ForwardAuthHandler) cacheKey(r *http.Request) string {
	hasher := sha256.New()
	hasher.Write([]byte(r.Method))
	hasher.Write([]byte(r.URL.String()))
	hasher.Write([]byte(r.Header.Get("Authorization")))
	hasher.Write([]byte(r.Header.Get("Cookie")))
	for _, header := range h.config.AuthHeaders {
		hasher.Write([]byte(header))
		hasher.Write([]byte(r.Header.Get(header)))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// getCachedDecision checks the cache for a previous auth decision.
func (h *ForwardAuthHandler) getCachedDecision(key string) (forwardAuthCacheEntry, bool) {
	if h.config.CacheTtlSeconds <= 0 {
		return forwardAuthCacheEntry{}, false
	}

	h.mu.RLock()
	entry, exists := h.cache[key]
	h.mu.RUnlock()

	if !exists {
		return forwardAuthCacheEntry{}, false
	}

	if time.Now().After(entry.expiresAt) {
		h.mu.Lock()
		delete(h.cache, key)
		h.mu.Unlock()
		return forwardAuthCacheEntry{}, false
	}

	return entry, true
}

// setCachedDecision stores an auth decision in the cache.
func (h *ForwardAuthHandler) setCachedDecision(key string, entry forwardAuthCacheEntry) {
	if h.config.CacheTtlSeconds <= 0 {
		return
	}

	h.mu.Lock()
	h.cache[key] = entry
	h.mu.Unlock()
}

// cleanupCache periodically removes expired cache entries.
func (h *ForwardAuthHandler) cleanupCache(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Debug("forward auth cache cleanup goroutine stopped")
			return
		case <-ticker.C:
			now := time.Now()
			h.mu.Lock()
			for key, entry := range h.cache {
				if now.After(entry.expiresAt) {
					delete(h.cache, key)
				}
			}
			h.mu.Unlock()
		}
	}
}

// HandleForwardAuth returns HTTP middleware that delegates auth to an external service.
func HandleForwardAuth(handler *ForwardAuthHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cacheKey := handler.cacheKey(r)

			// Check cache
			if cached, ok := handler.getCachedDecision(cacheKey); ok {
				if cached.allowed {
					// Copy cached headers to request
					copyHeaders(r, cached.headers, handler.config.ResponseHeaders)
					metrics.ForwardAuthTotal.WithLabelValues("success", "cached").Inc()
					next.ServeHTTP(w, r)
					return
				}
				metrics.ForwardAuthTotal.WithLabelValues("failure", "cached").Inc()
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Build auth subrequest
			authReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, handler.config.Address, nil)
			if err != nil {
				metrics.ForwardAuthTotal.WithLabelValues("error", "live").Inc()
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			// Forward specified headers from original request
			if len(handler.config.AuthHeaders) > 0 {
				for _, header := range handler.config.AuthHeaders {
					if val := r.Header.Get(header); val != "" {
						authReq.Header.Set(header, val)
					}
				}
			} else {
				// Default: forward all headers
				for key, values := range r.Header {
					for _, value := range values {
						authReq.Header.Add(key, value)
					}
				}
			}

			// Set standard forwarded headers
			authReq.Header.Set("X-Forwarded-Method", r.Method)
			authReq.Header.Set("X-Forwarded-Proto", requestProto(r))
			authReq.Header.Set("X-Forwarded-Host", r.Host)
			authReq.Header.Set("X-Forwarded-Uri", r.RequestURI)

			// Execute auth subrequest
			authResp, err := handler.httpClient.Do(authReq)
			if err != nil {
				metrics.ForwardAuthTotal.WithLabelValues("error", "live").Inc()
				http.Error(w, "Auth Service Unavailable", http.StatusServiceUnavailable)
				return
			}
			defer func() { _ = authResp.Body.Close() }()

			ttl := time.Duration(handler.config.CacheTtlSeconds) * time.Second

			// Check auth response status
			if authResp.StatusCode >= 200 && authResp.StatusCode < 300 {
				// Auth succeeded - copy response headers to upstream request
				copyHeaders(r, authResp.Header, handler.config.ResponseHeaders)

				// Cache success
				handler.setCachedDecision(cacheKey, forwardAuthCacheEntry{
					allowed:   true,
					headers:   cloneHeaders(authResp.Header, handler.config.ResponseHeaders),
					expiresAt: time.Now().Add(ttl),
				})

				metrics.ForwardAuthTotal.WithLabelValues("success", "live").Inc()
				next.ServeHTTP(w, r)
				return
			}

			// Auth failed
			handler.setCachedDecision(cacheKey, forwardAuthCacheEntry{
				allowed:   false,
				expiresAt: time.Now().Add(ttl),
			})

			metrics.ForwardAuthTotal.WithLabelValues("failure", "live").Inc()

			// Return the auth service's status code (401 or 403)
			statusCode := authResp.StatusCode
			if statusCode != http.StatusUnauthorized && statusCode != http.StatusForbidden {
				statusCode = http.StatusForbidden
			}

			// Copy WWW-Authenticate header if present
			if wwwAuth := authResp.Header.Get("WWW-Authenticate"); wwwAuth != "" {
				w.Header().Set("WWW-Authenticate", wwwAuth)
			}

			http.Error(w, http.StatusText(statusCode), statusCode)
		})
	}
}

// copyHeaders copies specified headers from src to the request.
func copyHeaders(r *http.Request, src http.Header, headerNames []string) {
	if len(headerNames) > 0 {
		for _, name := range headerNames {
			if val := src.Get(name); val != "" {
				r.Header.Set(name, val)
			}
		}
	}
}

// cloneHeaders creates a copy of the specified headers.
func cloneHeaders(src http.Header, headerNames []string) http.Header {
	result := make(http.Header)
	if len(headerNames) > 0 {
		for _, name := range headerNames {
			if val := src.Get(name); val != "" {
				result.Set(name, val)
			}
		}
	}
	return result
}

// requestProto returns the protocol scheme of the request.
func requestProto(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}
