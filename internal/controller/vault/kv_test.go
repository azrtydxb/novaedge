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

package vault

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestKVManager_BuildKVPath_V1(t *testing.T) {
	kv := NewKVManager(nil, zap.NewNop())

	path := kv.buildKVPath(KVEngineV1, "secret/myapp/config")
	if path != "secret/myapp/config" {
		t.Errorf("expected 'secret/myapp/config', got '%s'", path)
	}
}

func TestKVManager_BuildKVPath_V2(t *testing.T) {
	kv := NewKVManager(nil, zap.NewNop())

	path := kv.buildKVPath(KVEngineV2, "secret/myapp/config")
	if path != "secret/data/myapp/config" {
		t.Errorf("expected 'secret/data/myapp/config', got '%s'", path)
	}
}

func TestKVManager_CacheExpiry(t *testing.T) {
	cached := &cachedSecret{
		data:      map[string]any{"key": "value"},
		fetchedAt: time.Now().Add(-10 * time.Minute),
		ttl:       5 * time.Minute,
	}

	if !cached.isExpired() {
		t.Error("expected cached secret to be expired")
	}

	cached.fetchedAt = time.Now()
	if cached.isExpired() {
		t.Error("expected cached secret to NOT be expired")
	}
}

func TestKVManager_InvalidateCache(t *testing.T) {
	kv := NewKVManager(nil, zap.NewNop())

	// Add to cache
	kv.mu.Lock()
	kv.cache["kv-v2:secret/test"] = &cachedSecret{
		data:      map[string]any{"key": "value"},
		fetchedAt: time.Now(),
		ttl:       5 * time.Minute,
	}
	kv.mu.Unlock()

	// Verify in cache
	kv.mu.RLock()
	_, ok := kv.cache["kv-v2:secret/test"]
	kv.mu.RUnlock()
	if !ok {
		t.Error("expected secret in cache")
	}

	// Invalidate
	kv.InvalidateCache(KVEngineV2, "secret/test")

	// Verify removed
	kv.mu.RLock()
	_, ok = kv.cache["kv-v2:secret/test"]
	kv.mu.RUnlock()
	if ok {
		t.Error("expected secret removed from cache")
	}
}

func TestKVManager_InvalidateAllCache(t *testing.T) {
	kv := NewKVManager(nil, zap.NewNop())

	kv.mu.Lock()
	kv.cache["key1"] = &cachedSecret{data: map[string]any{"a": "1"}}
	kv.cache["key2"] = &cachedSecret{data: map[string]any{"b": "2"}}
	kv.mu.Unlock()

	kv.InvalidateAllCache()

	kv.mu.RLock()
	length := len(kv.cache)
	kv.mu.RUnlock()

	if length != 0 {
		t.Errorf("expected empty cache, got %d entries", length)
	}
}
