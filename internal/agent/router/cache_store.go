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
	"container/list"
	"sync"
	"time"
)

// CacheEntry represents a single cached HTTP response.
type CacheEntry struct {
	Key        string
	StatusCode int
	Headers    map[string][]string
	Body       []byte
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ETag       string
	SizeBytes  int
}

// IsExpired returns true if the cache entry has exceeded its TTL.
func (e *CacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// CacheStoreConfig configures the LRU cache store.
type CacheStoreConfig struct {
	// MaxEntries is the maximum number of cache entries.
	MaxEntries int
	// MaxMemoryBytes is the maximum total memory for cached responses.
	MaxMemoryBytes int64
	// DefaultTTL is the default time-to-live for cache entries.
	DefaultTTL time.Duration
	// MaxTTL is the maximum allowed TTL for cache entries.
	MaxTTL time.Duration
	// CleanupInterval is how often expired entries are purged.
	CleanupInterval time.Duration
}

// DefaultCacheStoreConfig returns a sensible default configuration.
func DefaultCacheStoreConfig() CacheStoreConfig {
	return CacheStoreConfig{
		MaxEntries:      10000,
		MaxMemoryBytes:  256 * 1024 * 1024, // 256 MiB
		DefaultTTL:      5 * time.Minute,
		MaxTTL:          1 * time.Hour,
		CleanupInterval: 1 * time.Minute,
	}
}

// CacheStore is a thread-safe LRU cache with TTL-based expiry.
type CacheStore struct {
	mu       sync.RWMutex
	config   CacheStoreConfig
	items    map[string]*list.Element
	eviction *list.List // Front = most recently used
	memUsed  int64
	stopCh   chan struct{}

	// Metrics
	hitCount      int64
	missCount     int64
	evictionCount int64
}

// NewCacheStore creates a new LRU cache store and starts the background cleanup goroutine.
func NewCacheStore(config CacheStoreConfig) *CacheStore {
	cs := &CacheStore{
		config:   config,
		items:    make(map[string]*list.Element, config.MaxEntries),
		eviction: list.New(),
		stopCh:   make(chan struct{}),
	}
	go cs.cleanupLoop()
	return cs
}

// Get retrieves a cache entry by key. Returns nil if not found or expired.
func (cs *CacheStore) Get(key string) *CacheEntry {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	elem, ok := cs.items[key]
	if !ok {
		cs.missCount++
		return nil
	}

	entry, ok := elem.Value.(*CacheEntry)
	if !ok {
		cs.missCount++
		return nil
	}

	if entry.IsExpired() {
		cs.removeElement(elem)
		cs.missCount++
		return nil
	}

	// Move to front (most recently used)
	cs.eviction.MoveToFront(elem)
	cs.hitCount++
	return entry
}

// Put stores a cache entry. Evicts least-recently-used entries if necessary.
func (cs *CacheStore) Put(entry *CacheEntry) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	entrySize := int64(entry.SizeBytes)

	// If this single entry exceeds max memory, don't cache it
	if entrySize > cs.config.MaxMemoryBytes {
		return
	}

	// If key already exists, remove old entry first
	if existing, ok := cs.items[entry.Key]; ok {
		cs.removeElement(existing)
	}

	// Evict entries until we have room
	for cs.eviction.Len() >= cs.config.MaxEntries || (cs.memUsed+entrySize > cs.config.MaxMemoryBytes && cs.eviction.Len() > 0) {
		cs.evictLRU()
	}

	elem := cs.eviction.PushFront(entry)
	cs.items[entry.Key] = elem
	cs.memUsed += entrySize
}

// Delete removes a specific cache entry by key.
func (cs *CacheStore) Delete(key string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	elem, ok := cs.items[key]
	if !ok {
		return false
	}
	cs.removeElement(elem)
	return true
}

// Purge removes all entries matching the given key prefix pattern.
// Returns the number of entries purged.
func (cs *CacheStore) Purge(pattern string) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	count := 0
	for key, elem := range cs.items {
		if matchPattern(pattern, key) {
			cs.removeElement(elem)
			count++
		}
	}
	return count
}

// PurgeAll removes all cache entries.
func (cs *CacheStore) PurgeAll() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	count := cs.eviction.Len()
	cs.items = make(map[string]*list.Element, cs.config.MaxEntries)
	cs.eviction.Init()
	cs.memUsed = 0
	return count
}

// Stats returns cache statistics.
func (cs *CacheStore) Stats() CacheStats {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return CacheStats{
		Entries:       cs.eviction.Len(),
		MemoryUsed:    cs.memUsed,
		MaxMemory:     cs.config.MaxMemoryBytes,
		HitCount:      cs.hitCount,
		MissCount:     cs.missCount,
		EvictionCount: cs.evictionCount,
	}
}

// CacheStats holds cache statistics.
type CacheStats struct {
	Entries       int   `json:"entries"`
	MemoryUsed    int64 `json:"memoryUsed"`
	MaxMemory     int64 `json:"maxMemory"`
	HitCount      int64 `json:"hitCount"`
	MissCount     int64 `json:"missCount"`
	EvictionCount int64 `json:"evictionCount"`
}

// Stop stops the background cleanup goroutine.
func (cs *CacheStore) Stop() {
	close(cs.stopCh)
}

// removeElement removes a list element from the cache (caller must hold lock).
func (cs *CacheStore) removeElement(elem *list.Element) {
	entry, ok := elem.Value.(*CacheEntry)
	if !ok {
		return
	}
	cs.eviction.Remove(elem)
	delete(cs.items, entry.Key)
	cs.memUsed -= int64(entry.SizeBytes)
	if cs.memUsed < 0 {
		cs.memUsed = 0
	}
}

// evictLRU removes the least recently used entry (caller must hold lock).
func (cs *CacheStore) evictLRU() {
	back := cs.eviction.Back()
	if back == nil {
		return
	}
	cs.removeElement(back)
	cs.evictionCount++
}

// cleanupLoop periodically removes expired entries.
func (cs *CacheStore) cleanupLoop() {
	ticker := time.NewTicker(cs.config.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-cs.stopCh:
			return
		case <-ticker.C:
			cs.cleanupExpired()
		}
	}
}

// cleanupExpired removes all expired entries.
func (cs *CacheStore) cleanupExpired() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	var toRemove []*list.Element
	for elem := cs.eviction.Back(); elem != nil; elem = elem.Prev() {
		entry, ok := elem.Value.(*CacheEntry)
		if !ok {
			continue
		}
		if entry.IsExpired() {
			toRemove = append(toRemove, elem)
		}
	}
	for _, elem := range toRemove {
		cs.removeElement(elem)
		cs.evictionCount++
	}
}

// matchPattern performs simple prefix/suffix/exact matching for cache purge patterns.
// Supports:
//   - "*" matches everything
//   - "prefix*" matches keys starting with prefix
//   - "*suffix" matches keys ending with suffix
//   - exact match otherwise
func matchPattern(pattern, key string) bool {
	if pattern == "*" {
		return true
	}
	n := len(pattern)
	if n == 0 {
		return key == ""
	}
	if pattern[n-1] == '*' {
		prefix := pattern[:n-1]
		return len(key) >= len(prefix) && key[:len(prefix)] == prefix
	}
	if pattern[0] == '*' {
		suffix := pattern[1:]
		return len(key) >= len(suffix) && key[len(key)-len(suffix):] == suffix
	}
	return key == pattern
}
