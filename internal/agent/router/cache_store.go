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
	"hash/fnv"
	"sync"
	"time"
)

// cacheShardCount is the number of lock shards used to reduce contention.
const cacheShardCount = 16

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

// cacheShard is a single shard of the sharded cache, containing its own
// mutex, entry map, and LRU eviction list.
type cacheShard struct {
	mu       sync.Mutex
	entries  map[string]*list.Element
	eviction *list.List // Front = most recently used
	memUsed  int64

	// Per-shard metrics.
	hitCount      int64
	missCount     int64
	evictionCount int64
}

// CacheStore is a thread-safe sharded LRU cache with TTL-based expiry.
// Entries are distributed across 16 shards by key hash to reduce lock
// contention under concurrent access.
type CacheStore struct {
	shards         [cacheShardCount]*cacheShard
	config         CacheStoreConfig
	maxPerShard    int
	maxMemPerShard int64
	stopCh         chan struct{}
}

// NewCacheStore creates a new sharded LRU cache store and starts the
// background cleanup goroutine.
func NewCacheStore(config CacheStoreConfig) *CacheStore {
	maxPerShard := config.MaxEntries / cacheShardCount
	if maxPerShard < 1 {
		maxPerShard = 1
	}
	maxMemPerShard := config.MaxMemoryBytes / cacheShardCount
	if maxMemPerShard < 1 {
		maxMemPerShard = 1
	}

	cs := &CacheStore{
		config:         config,
		maxPerShard:    maxPerShard,
		maxMemPerShard: maxMemPerShard,
		stopCh:         make(chan struct{}),
	}
	for i := range cs.shards {
		cs.shards[i] = &cacheShard{
			entries:  make(map[string]*list.Element, maxPerShard),
			eviction: list.New(),
		}
	}
	go cs.cleanupLoop()
	return cs
}

// getShard returns the shard responsible for the given key.
func (cs *CacheStore) getShard(key string) *cacheShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return cs.shards[h.Sum32()%cacheShardCount]
}

// Get retrieves a cache entry by key. Returns nil if not found or expired.
func (cs *CacheStore) Get(key string) *CacheEntry {
	shard := cs.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	elem, ok := shard.entries[key]
	if !ok {
		shard.missCount++
		return nil
	}

	entry, ok := elem.Value.(*CacheEntry)
	if !ok {
		shard.missCount++
		return nil
	}

	if entry.IsExpired() {
		shard.removeElement(elem)
		shard.missCount++
		return nil
	}

	// Move to front (most recently used)
	shard.eviction.MoveToFront(elem)
	shard.hitCount++
	return entry
}

// Put stores a cache entry. Evicts least-recently-used entries if necessary.
func (cs *CacheStore) Put(entry *CacheEntry) {
	shard := cs.getShard(entry.Key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	entrySize := int64(entry.SizeBytes)

	// If this single entry exceeds the per-shard memory limit, don't cache it
	if entrySize > cs.maxMemPerShard {
		return
	}

	// If key already exists, remove old entry first
	if existing, ok := shard.entries[entry.Key]; ok {
		shard.removeElement(existing)
	}

	// Evict entries until we have room
	for shard.eviction.Len() >= cs.maxPerShard || (shard.memUsed+entrySize > cs.maxMemPerShard && shard.eviction.Len() > 0) {
		shard.evictLRU()
	}

	elem := shard.eviction.PushFront(entry)
	shard.entries[entry.Key] = elem
	shard.memUsed += entrySize
}

// Delete removes a specific cache entry by key.
func (cs *CacheStore) Delete(key string) bool {
	shard := cs.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	elem, ok := shard.entries[key]
	if !ok {
		return false
	}
	shard.removeElement(elem)
	return true
}

// Purge removes all entries matching the given key prefix pattern.
// Returns the number of entries purged.
func (cs *CacheStore) Purge(pattern string) int {
	total := 0
	for _, shard := range cs.shards {
		shard.mu.Lock()
		for key, elem := range shard.entries {
			if matchPattern(pattern, key) {
				shard.removeElement(elem)
				total++
			}
		}
		shard.mu.Unlock()
	}
	return total
}

// PurgeAll removes all cache entries.
func (cs *CacheStore) PurgeAll() int {
	total := 0
	for _, shard := range cs.shards {
		shard.mu.Lock()
		total += shard.eviction.Len()
		shard.entries = make(map[string]*list.Element, cs.maxPerShard)
		shard.eviction.Init()
		shard.memUsed = 0
		shard.mu.Unlock()
	}
	return total
}

// Stats returns aggregated cache statistics across all shards.
func (cs *CacheStore) Stats() CacheStats {
	var stats CacheStats
	stats.MaxMemory = cs.config.MaxMemoryBytes
	for _, shard := range cs.shards {
		shard.mu.Lock()
		stats.Entries += shard.eviction.Len()
		stats.MemoryUsed += shard.memUsed
		stats.HitCount += shard.hitCount
		stats.MissCount += shard.missCount
		stats.EvictionCount += shard.evictionCount
		shard.mu.Unlock()
	}
	return stats
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

// removeElement removes a list element from the shard (caller must hold lock).
func (s *cacheShard) removeElement(elem *list.Element) {
	entry, ok := elem.Value.(*CacheEntry)
	if !ok {
		return
	}
	s.eviction.Remove(elem)
	delete(s.entries, entry.Key)
	s.memUsed -= int64(entry.SizeBytes)
	if s.memUsed < 0 {
		s.memUsed = 0
	}
}

// evictLRU removes the least recently used entry (caller must hold lock).
func (s *cacheShard) evictLRU() {
	back := s.eviction.Back()
	if back == nil {
		return
	}
	s.removeElement(back)
	s.evictionCount++
}

// cleanupLoop periodically removes expired entries across all shards.
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

// cleanupExpired removes all expired entries from every shard.
func (cs *CacheStore) cleanupExpired() {
	for _, shard := range cs.shards {
		shard.mu.Lock()
		var toRemove []*list.Element
		for elem := shard.eviction.Back(); elem != nil; elem = elem.Prev() {
			entry, ok := elem.Value.(*CacheEntry)
			if !ok {
				continue
			}
			if entry.IsExpired() {
				toRemove = append(toRemove, elem)
			}
		}
		for _, elem := range toRemove {
			shard.removeElement(elem)
			shard.evictionCount++
		}
		shard.mu.Unlock()
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
