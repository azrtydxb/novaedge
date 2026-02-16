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
	"testing"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func newTestLogger(t *testing.T) *zap.Logger {
	t.Helper()
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("failed to create test logger: %v", err)
	}
	return logger
}

func TestNewWAFEngineCache(t *testing.T) {
	logger := newTestLogger(t)
	cache := NewWAFEngineCache(logger)

	if cache == nil {
		t.Fatal("NewWAFEngineCache returned nil")
	}
	if cache.Size() != 0 {
		t.Errorf("new cache size = %d, want 0", cache.Size())
	}
}

func TestWAFEngineCacheGetOrCreate(t *testing.T) {
	logger := newTestLogger(t)
	cache := NewWAFEngineCache(logger)

	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "detection",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}

	// First call should create a new engine (cache miss)
	engine1, err := cache.GetOrCreate(config)
	if err != nil {
		t.Fatalf("GetOrCreate (first call) failed: %v", err)
	}
	if engine1 == nil {
		t.Fatal("GetOrCreate returned nil engine")
	}
	if cache.Size() != 1 {
		t.Errorf("cache size after first insert = %d, want 1", cache.Size())
	}

	// Second call with same config should return cached engine (cache hit)
	engine2, err := cache.GetOrCreate(config)
	if err != nil {
		t.Fatalf("GetOrCreate (second call) failed: %v", err)
	}
	if engine1 != engine2 {
		t.Error("expected same engine instance on cache hit, got different pointers")
	}
	if cache.Size() != 1 {
		t.Errorf("cache size after cache hit = %d, want 1", cache.Size())
	}
}

func TestWAFEngineCacheDifferentConfigs(t *testing.T) {
	logger := newTestLogger(t)
	cache := NewWAFEngineCache(logger)

	config1 := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "detection",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}
	config2 := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    2,
		AnomalyThreshold: 10,
	}

	engine1, err := cache.GetOrCreate(config1)
	if err != nil {
		t.Fatalf("GetOrCreate config1 failed: %v", err)
	}

	engine2, err := cache.GetOrCreate(config2)
	if err != nil {
		t.Fatalf("GetOrCreate config2 failed: %v", err)
	}

	if engine1 == engine2 {
		t.Error("different configs should produce different engine instances")
	}
	if cache.Size() != 2 {
		t.Errorf("cache size = %d, want 2", cache.Size())
	}
}

func TestWAFEngineCachePurge(t *testing.T) {
	logger := newTestLogger(t)
	cache := NewWAFEngineCache(logger)

	config := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "detection",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}

	_, err := cache.GetOrCreate(config)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	if cache.Size() != 1 {
		t.Fatalf("cache size before purge = %d, want 1", cache.Size())
	}

	cache.Purge()

	if cache.Size() != 0 {
		t.Errorf("cache size after purge = %d, want 0", cache.Size())
	}

	// After purge, same config should create a new engine
	_, err = cache.GetOrCreate(config)
	if err != nil {
		t.Fatalf("GetOrCreate after purge failed: %v", err)
	}
	if cache.Size() != 1 {
		t.Errorf("cache size after re-insert = %d, want 1", cache.Size())
	}
}

func TestConfigHash(t *testing.T) {
	config1 := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "detection",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}
	config2 := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "detection",
		ParanoiaLevel:    1,
		AnomalyThreshold: 5,
	}
	config3 := &pb.WAFConfig{
		Enabled:          true,
		Mode:             "prevention",
		ParanoiaLevel:    2,
		AnomalyThreshold: 10,
	}

	hash1, err := configHash(config1)
	if err != nil {
		t.Fatalf("configHash(config1) failed: %v", err)
	}
	hash2, err := configHash(config2)
	if err != nil {
		t.Fatalf("configHash(config2) failed: %v", err)
	}
	hash3, err := configHash(config3)
	if err != nil {
		t.Fatalf("configHash(config3) failed: %v", err)
	}

	// Identical configs must produce the same hash
	if hash1 != hash2 {
		t.Errorf("identical configs produced different hashes: %s vs %s", hash1, hash2)
	}

	// Different configs must produce different hashes
	if hash1 == hash3 {
		t.Error("different configs produced the same hash")
	}

	// Hash should be a valid hex string of SHA-256 length (64 chars)
	if len(hash1) != 64 {
		t.Errorf("hash length = %d, want 64 (SHA-256 hex)", len(hash1))
	}
}

func TestConfigHashNilConfig(t *testing.T) {
	// nil protobuf message should still produce a valid hash (empty message)
	hash, err := configHash(nil)
	if err != nil {
		t.Fatalf("configHash(nil) failed: %v", err)
	}
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash))
	}
}
