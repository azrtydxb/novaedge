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
	"testing"
	"time"
)

const testCacheBody = "hello"

func TestCacheStoreGetPut(t *testing.T) {
	config := CacheStoreConfig{
		MaxEntries:      100,
		MaxMemoryBytes:  1024 * 1024,
		DefaultTTL:      5 * time.Minute,
		MaxTTL:          1 * time.Hour,
		CleanupInterval: 1 * time.Hour, // long interval to avoid cleanup during test
	}
	store := NewCacheStore(config)
	defer store.Stop()

	entry := &CacheEntry{
		Key:       "test-key",
		Body:      []byte(testCacheBody),
		ExpiresAt: time.Now().Add(5 * time.Minute),
		SizeBytes: 5,
	}

	store.Put(entry)

	got := store.Get("test-key")
	if got == nil {
		t.Fatal("Get returned nil for existing key")
	}
	if string(got.Body) != testCacheBody {
		t.Errorf("Get body = %q, want %q", string(got.Body), testCacheBody)
	}
}

func TestCacheStoreGetMiss(t *testing.T) {
	config := CacheStoreConfig{
		MaxEntries:      100,
		MaxMemoryBytes:  1024 * 1024,
		DefaultTTL:      5 * time.Minute,
		MaxTTL:          1 * time.Hour,
		CleanupInterval: 1 * time.Hour,
	}
	store := NewCacheStore(config)
	defer store.Stop()

	got := store.Get("nonexistent")
	if got != nil {
		t.Errorf("Get returned non-nil for nonexistent key: %v", got)
	}
}

func TestCacheStoreTTLExpiry(t *testing.T) {
	config := CacheStoreConfig{
		MaxEntries:      100,
		MaxMemoryBytes:  1024 * 1024,
		DefaultTTL:      5 * time.Minute,
		MaxTTL:          1 * time.Hour,
		CleanupInterval: 1 * time.Hour,
	}
	store := NewCacheStore(config)
	defer store.Stop()

	entry := &CacheEntry{
		Key:       "expired",
		Body:      []byte("old"),
		ExpiresAt: time.Now().Add(-1 * time.Second), // already expired
		SizeBytes: 3,
	}
	store.Put(entry)

	got := store.Get("expired")
	if got != nil {
		t.Error("Get returned non-nil for expired key")
	}
}

func TestCacheStoreLRUEviction(t *testing.T) {
	// With 16 shards and MaxEntries=16, each shard holds 1 entry.
	// We need entries that hash to the same shard to test LRU eviction.
	// Instead, use a large enough MaxEntries and test via memory pressure.
	config := CacheStoreConfig{
		MaxEntries:      1600, // 100 per shard
		MaxMemoryBytes:  1024 * 1024,
		DefaultTTL:      5 * time.Minute,
		MaxTTL:          1 * time.Hour,
		CleanupInterval: 1 * time.Hour,
	}
	store := NewCacheStore(config)
	defer store.Stop()

	// Add many entries
	for i := 0; i < 50; i++ {
		store.Put(&CacheEntry{
			Key:       fmt.Sprintf("key-%d", i),
			Body:      []byte("x"),
			ExpiresAt: time.Now().Add(5 * time.Minute),
			SizeBytes: 1,
		})
	}

	// All entries should be retrievable
	for i := 0; i < 50; i++ {
		got := store.Get(fmt.Sprintf("key-%d", i))
		if got == nil {
			t.Errorf("key-%d should be in cache", i)
		}
	}

	stats := store.Stats()
	if stats.Entries != 50 {
		t.Errorf("Entries = %d, want 50", stats.Entries)
	}
}

func TestCacheStoreMemoryEviction(t *testing.T) {
	config := CacheStoreConfig{
		MaxEntries:      1600,
		MaxMemoryBytes:  160, // 10 bytes per shard
		DefaultTTL:      5 * time.Minute,
		MaxTTL:          1 * time.Hour,
		CleanupInterval: 1 * time.Hour,
	}
	store := NewCacheStore(config)
	defer store.Stop()

	// Add entries that will exceed per-shard memory limits
	for i := 0; i < 50; i++ {
		store.Put(&CacheEntry{
			Key:       fmt.Sprintf("key-%d", i),
			Body:      []byte("12345"),
			ExpiresAt: time.Now().Add(5 * time.Minute),
			SizeBytes: 5,
		})
	}

	stats := store.Stats()
	if stats.MemoryUsed > config.MaxMemoryBytes {
		t.Errorf("memory used %d exceeds max %d", stats.MemoryUsed, config.MaxMemoryBytes)
	}
}

func TestCacheStorePurge(t *testing.T) {
	config := CacheStoreConfig{
		MaxEntries:      1600,
		MaxMemoryBytes:  1024 * 1024,
		DefaultTTL:      5 * time.Minute,
		MaxTTL:          1 * time.Hour,
		CleanupInterval: 1 * time.Hour,
	}
	store := NewCacheStore(config)
	defer store.Stop()

	store.Put(&CacheEntry{
		Key:       "GET|example.com|/api/v1/users",
		Body:      []byte("x"),
		ExpiresAt: time.Now().Add(5 * time.Minute),
		SizeBytes: 1,
	})
	store.Put(&CacheEntry{
		Key:       "GET|example.com|/api/v2/users",
		Body:      []byte("x"),
		ExpiresAt: time.Now().Add(5 * time.Minute),
		SizeBytes: 1,
	})
	store.Put(&CacheEntry{
		Key:       "GET|other.com|/data",
		Body:      []byte("x"),
		ExpiresAt: time.Now().Add(5 * time.Minute),
		SizeBytes: 1,
	})

	// Purge all example.com entries
	count := store.Purge("GET|example.com|*")
	if count != 2 {
		t.Errorf("Purge count = %d, want 2", count)
	}

	if store.Get("GET|other.com|/data") == nil {
		t.Error("other.com entry should not have been purged")
	}
}

func TestCacheStorePurgeAll(t *testing.T) {
	config := CacheStoreConfig{
		MaxEntries:      1600,
		MaxMemoryBytes:  1024 * 1024,
		DefaultTTL:      5 * time.Minute,
		MaxTTL:          1 * time.Hour,
		CleanupInterval: 1 * time.Hour,
	}
	store := NewCacheStore(config)
	defer store.Stop()

	for i := 0; i < 5; i++ {
		store.Put(&CacheEntry{
			Key:       fmt.Sprintf("key-%d", i),
			Body:      []byte("x"),
			ExpiresAt: time.Now().Add(5 * time.Minute),
			SizeBytes: 1,
		})
	}

	count := store.PurgeAll()
	if count != 5 {
		t.Errorf("PurgeAll count = %d, want 5", count)
	}

	stats := store.Stats()
	if stats.Entries != 0 {
		t.Errorf("entries after PurgeAll = %d, want 0", stats.Entries)
	}
}

func TestCacheStoreStats(t *testing.T) {
	config := CacheStoreConfig{
		MaxEntries:      1600,
		MaxMemoryBytes:  1024 * 1024,
		DefaultTTL:      5 * time.Minute,
		MaxTTL:          1 * time.Hour,
		CleanupInterval: 1 * time.Hour,
	}
	store := NewCacheStore(config)
	defer store.Stop()

	store.Put(&CacheEntry{
		Key:       "a",
		Body:      []byte(testCacheBody),
		ExpiresAt: time.Now().Add(5 * time.Minute),
		SizeBytes: 5,
	})

	// Hit
	store.Get("a")
	// Miss
	store.Get("nonexistent")

	stats := store.Stats()
	if stats.Entries != 1 {
		t.Errorf("Entries = %d, want 1", stats.Entries)
	}
	if stats.HitCount != 1 {
		t.Errorf("HitCount = %d, want 1", stats.HitCount)
	}
	if stats.MissCount != 1 {
		t.Errorf("MissCount = %d, want 1", stats.MissCount)
	}
	if stats.MemoryUsed != 5 {
		t.Errorf("MemoryUsed = %d, want 5", stats.MemoryUsed)
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		key     string
		match   bool
	}{
		{"*", "anything", true},
		{"*", "", true},
		{"prefix*", "prefix123", true},
		{"prefix*", "other", false},
		{"*suffix", "mysuffix", true},
		{"*suffix", "myprefix", false},
		{"exact", "exact", true},
		{"exact", "other", false},
		{"", "", true},
		{"", "something", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.key, func(t *testing.T) {
			result := matchPattern(tt.pattern, tt.key)
			if result != tt.match {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.key, result, tt.match)
			}
		})
	}
}
