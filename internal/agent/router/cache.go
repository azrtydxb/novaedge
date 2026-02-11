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
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// CacheConfig holds configuration for the response cache.
type CacheConfig struct {
	// Enabled controls whether caching is active.
	Enabled bool
	// MaxSize is the maximum cache memory in bytes.
	MaxSize int64
	// DefaultTTL is the default time-to-live for cached responses.
	DefaultTTL time.Duration
	// MaxTTL is the maximum TTL for cached responses.
	MaxTTL time.Duration
	// MaxEntrySize is the maximum size of a single cache entry in bytes.
	MaxEntrySize int64
}

// DefaultCacheConfig returns a sensible default cache configuration.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		Enabled:      false,
		MaxSize:      256 * 1024 * 1024, // 256 MiB
		DefaultTTL:   5 * time.Minute,
		MaxTTL:       1 * time.Hour,
		MaxEntrySize: 1 * 1024 * 1024, // 1 MiB
	}
}

// ResponseCache provides HTTP response caching with LRU eviction and
// Cache-Control header support.
type ResponseCache struct {
	store  *CacheStore
	config CacheConfig
	logger *zap.Logger
}

// NewResponseCache creates a new response cache.
func NewResponseCache(config CacheConfig, logger *zap.Logger) *ResponseCache {
	storeConfig := CacheStoreConfig{
		MaxEntries:      10000,
		MaxMemoryBytes:  config.MaxSize,
		DefaultTTL:      config.DefaultTTL,
		MaxTTL:          config.MaxTTL,
		CleanupInterval: 1 * time.Minute,
	}
	return &ResponseCache{
		store:  NewCacheStore(storeConfig),
		config: config,
		logger: logger,
	}
}

// Stop stops the cache background goroutines.
func (rc *ResponseCache) Stop() {
	if rc.store != nil {
		rc.store.Stop()
	}
}

// Stats returns cache statistics.
func (rc *ResponseCache) Stats() CacheStats {
	return rc.store.Stats()
}

// Purge removes entries matching the pattern from the cache.
func (rc *ResponseCache) Purge(pattern string) int {
	if pattern == "" || pattern == "*" {
		return rc.store.PurgeAll()
	}
	return rc.store.Purge(pattern)
}

// Middleware returns an HTTP middleware that serves cached responses when available
// and caches new responses.
func (rc *ResponseCache) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rc.config.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Only cache GET and HEAD requests
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		// Check request Cache-Control
		reqCC := parseCacheControl(r.Header.Get("Cache-Control"))
		if reqCC.noCache || reqCC.noStore {
			next.ServeHTTP(w, r)
			return
		}

		// Build cache key
		cacheKey := buildCacheKey(r)

		// Try to serve from cache
		entry := rc.store.Get(cacheKey)
		if entry != nil {
			// Handle conditional requests
			if rc.handleConditionalRequest(w, r, entry) {
				return
			}

			// Serve cached response
			rc.serveCached(w, r, entry)
			return
		}

		// Cache miss: capture the response
		recorder := newCacheRecorder(w)
		next.ServeHTTP(recorder, r)

		// Determine if we should cache this response
		if rc.shouldCache(r, recorder) {
			ttl := rc.determineTTL(recorder.Header())
			rc.storeResponse(cacheKey, recorder, ttl)
		}
	})
}

// handleConditionalRequest handles If-None-Match / If-Modified-Since.
// Returns true if the conditional matched and a 304 was sent.
func (rc *ResponseCache) handleConditionalRequest(w http.ResponseWriter, r *http.Request, entry *CacheEntry) bool {
	// If-None-Match (ETag)
	ifNoneMatch := r.Header.Get("If-None-Match")
	if ifNoneMatch != "" && entry.ETag != "" {
		if ifNoneMatch == entry.ETag || ifNoneMatch == "*" {
			for k, vs := range entry.Headers {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusNotModified)
			return true
		}
	}

	// If-Modified-Since
	ifModifiedSince := r.Header.Get("If-Modified-Since")
	if ifModifiedSince != "" {
		t, err := http.ParseTime(ifModifiedSince)
		if err == nil && !entry.CreatedAt.After(t) {
			for k, vs := range entry.Headers {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusNotModified)
			return true
		}
	}

	return false
}

// serveCached writes a cached response to the client.
func (rc *ResponseCache) serveCached(w http.ResponseWriter, _ *http.Request, entry *CacheEntry) {
	for k, vs := range entry.Headers {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Cache", "HIT")
	w.WriteHeader(entry.StatusCode)
	_, _ = w.Write(entry.Body)
}

// shouldCache determines if the response should be cached.
func (rc *ResponseCache) shouldCache(_ *http.Request, rec *cacheRecorder) bool {
	// Only cache successful responses
	if rec.statusCode < 200 || rec.statusCode >= 400 {
		return false
	}

	// Don't cache responses with Set-Cookie
	if rec.Header().Get("Set-Cookie") != "" {
		return false
	}

	// Check response Cache-Control
	respCC := parseCacheControl(rec.Header().Get("Cache-Control"))
	if respCC.noStore || respCC.private {
		return false
	}

	// Don't cache if response exceeds max entry size
	if int64(rec.body.Len()) > rc.config.MaxEntrySize {
		return false
	}

	return true
}

// determineTTL determines the TTL from response headers.
func (rc *ResponseCache) determineTTL(header http.Header) time.Duration {
	cc := parseCacheControl(header.Get("Cache-Control"))

	// s-maxage takes precedence (shared cache)
	if cc.sMaxAge > 0 {
		ttl := time.Duration(cc.sMaxAge) * time.Second
		if ttl > rc.config.MaxTTL {
			return rc.config.MaxTTL
		}
		return ttl
	}

	// max-age
	if cc.maxAge > 0 {
		ttl := time.Duration(cc.maxAge) * time.Second
		if ttl > rc.config.MaxTTL {
			return rc.config.MaxTTL
		}
		return ttl
	}

	// Expires header
	if expires := header.Get("Expires"); expires != "" {
		t, err := http.ParseTime(expires)
		if err == nil {
			ttl := time.Until(t)
			if ttl > 0 {
				if ttl > rc.config.MaxTTL {
					return rc.config.MaxTTL
				}
				return ttl
			}
		}
	}

	return rc.config.DefaultTTL
}

// storeResponse stores a response in the cache.
func (rc *ResponseCache) storeResponse(key string, rec *cacheRecorder, ttl time.Duration) {
	now := time.Now()
	entry := &CacheEntry{
		Key:        key,
		StatusCode: rec.statusCode,
		Headers:    make(map[string][]string),
		Body:       rec.body.Bytes(),
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
		ETag:       rec.Header().Get("ETag"),
		SizeBytes:  rec.body.Len(),
	}

	// Copy response headers
	for k, vs := range rec.Header() {
		entry.Headers[k] = make([]string, len(vs))
		copy(entry.Headers[k], vs)
	}

	// Generate ETag if not present
	if entry.ETag == "" && len(entry.Body) > 0 {
		h := sha256.Sum256(entry.Body)
		entry.ETag = fmt.Sprintf(`"%s"`, hex.EncodeToString(h[:8]))
		entry.Headers["ETag"] = []string{entry.ETag}
	}

	// Account for headers in size
	for k, vs := range entry.Headers {
		entry.SizeBytes += len(k)
		for _, v := range vs {
			entry.SizeBytes += len(v)
		}
	}

	rc.store.Put(entry)
}

// buildCacheKey builds a cache key from the request.
// Key: method + host + path + sorted Vary header values
func buildCacheKey(r *http.Request) string {
	var b strings.Builder
	b.WriteString(r.Method)
	b.WriteByte('|')
	b.WriteString(r.Host)
	b.WriteByte('|')
	b.WriteString(r.URL.Path)
	if r.URL.RawQuery != "" {
		b.WriteByte('?')
		b.WriteString(r.URL.RawQuery)
	}

	// Include Vary header values in the key
	vary := r.Header.Get("Vary")
	if vary != "" {
		varyHeaders := strings.Split(vary, ",")
		sort.Strings(varyHeaders)
		for _, h := range varyHeaders {
			h = strings.TrimSpace(h)
			b.WriteByte('|')
			b.WriteString(h)
			b.WriteByte('=')
			b.WriteString(r.Header.Get(h))
		}
	}

	return b.String()
}

// cacheControl holds parsed Cache-Control directives.
type cacheControl struct {
	noCache bool
	noStore bool
	private bool
	public  bool
	maxAge  int64
	sMaxAge int64
}

// parseCacheControl parses a Cache-Control header value.
func parseCacheControl(header string) cacheControl {
	var cc cacheControl
	if header == "" {
		return cc
	}

	directives := strings.Split(header, ",")
	for _, d := range directives {
		d = strings.TrimSpace(d)
		d = strings.ToLower(d)

		switch {
		case d == "no-cache":
			cc.noCache = true
		case d == "no-store":
			cc.noStore = true
		case d == "private":
			cc.private = true
		case d == "public":
			cc.public = true
		case strings.HasPrefix(d, "max-age="):
			if val, err := strconv.ParseInt(d[8:], 10, 64); err == nil {
				cc.maxAge = val
			}
		case strings.HasPrefix(d, "s-maxage="):
			if val, err := strconv.ParseInt(d[9:], 10, 64); err == nil {
				cc.sMaxAge = val
			}
		}
	}

	return cc
}

// cacheRecorder captures response data so it can be cached.
type cacheRecorder struct {
	http.ResponseWriter
	statusCode    int
	body          bytes.Buffer
	headerWritten bool
}

func newCacheRecorder(w http.ResponseWriter) *cacheRecorder {
	return &cacheRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

func (cr *cacheRecorder) WriteHeader(code int) {
	if !cr.headerWritten {
		cr.statusCode = code
		cr.headerWritten = true
		cr.ResponseWriter.WriteHeader(code)
	}
}

func (cr *cacheRecorder) Write(b []byte) (int, error) {
	if !cr.headerWritten {
		cr.headerWritten = true
		cr.statusCode = http.StatusOK
	}
	cr.body.Write(b)
	return cr.ResponseWriter.Write(b)
}

// Flush implements http.Flusher.
func (cr *cacheRecorder) Flush() {
	if f, ok := cr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker for WebSocket support.
func (cr *cacheRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := cr.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Unwrap returns the underlying ResponseWriter.
func (cr *cacheRecorder) Unwrap() http.ResponseWriter {
	return cr.ResponseWriter
}
