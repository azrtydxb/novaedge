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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// WAFEngineCache caches WAF engine instances by configuration hash to avoid
// recreating engines for identical configurations on config snapshot updates.
type WAFEngineCache struct {
	mu      sync.RWMutex
	engines map[string]*WAFEngine
	logger  *zap.Logger
}

// NewWAFEngineCache creates a new WAF engine cache
func NewWAFEngineCache(logger *zap.Logger) *WAFEngineCache {
	return &WAFEngineCache{
		engines: make(map[string]*WAFEngine),
		logger:  logger,
	}
}

// GetOrCreate returns a cached WAF engine for the given config, or creates one if not cached
func (c *WAFEngineCache) GetOrCreate(config *pb.WAFConfig) (*WAFEngine, error) {
	hash, err := configHash(config)
	if err != nil {
		return nil, fmt.Errorf("failed to hash WAF config: %w", err)
	}

	c.mu.RLock()
	if engine, ok := c.engines[hash]; ok {
		c.mu.RUnlock()
		c.logger.Debug("WAF engine cache hit", zap.String("hash", hash[:12]))
		return engine, nil
	}
	c.mu.RUnlock()

	// Cache miss — create new engine
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if engine, ok := c.engines[hash]; ok {
		return engine, nil
	}

	engine, err := NewWAFEngine(config, c.logger)
	if err != nil {
		return nil, err
	}

	c.engines[hash] = engine
	c.logger.Info("WAF engine cached", zap.String("hash", hash[:12]), zap.Int("cache_size", len(c.engines)))
	return engine, nil
}

// Purge removes all cached engines
func (c *WAFEngineCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.engines = make(map[string]*WAFEngine)
	c.logger.Info("WAF engine cache purged")
}

// Size returns the number of cached engines
func (c *WAFEngineCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.engines)
}

// configHash produces a SHA-256 hash of the protobuf WAF config
func configHash(config *pb.WAFConfig) (string, error) {
	data, err := proto.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal WAF config: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
