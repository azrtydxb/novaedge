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
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	errNoDataFoundAtPath                     = errors.New("no data found at path")
	errUnexpectedKVV2ResponseStructureAtPath = errors.New("unexpected KV v2 response structure at path")
	errKey                                   = errors.New("key")
)

// KVEngine defines the KV engine version.
type KVEngine string

const (
	// KVEngineV1 is KV secrets engine version 1.
	KVEngineV1 KVEngine = "kv-v1"
	// KVEngineV2 is KV secrets engine version 2.
	KVEngineV2 KVEngine = "kv-v2"
)

// KVManager handles operations with the Vault KV secrets engine.
type KVManager struct {
	client *Client
	logger *zap.Logger

	// Cache for secrets with configurable refresh
	mu    sync.RWMutex
	cache map[string]*cachedSecret
}

// cachedSecret stores a cached secret with expiry time.
type cachedSecret struct {
	data      map[string]interface{}
	fetchedAt time.Time
	ttl       time.Duration
}

// isExpired returns true if the cached secret has expired.
func (c *cachedSecret) isExpired() bool {
	return time.Since(c.fetchedAt) > c.ttl
}

// NewKVManager creates a new KV secrets manager.
func NewKVManager(client *Client, logger *zap.Logger) *KVManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &KVManager{
		client: client,
		logger: logger,
		cache:  make(map[string]*cachedSecret),
	}
}

// ReadSecret reads a secret from Vault KV engine.
// For KV v2, the path is automatically adjusted to include "data/".
func (k *KVManager) ReadSecret(ctx context.Context, engine KVEngine, path string) (map[string]interface{}, error) {
	apiPath := k.buildKVPath(engine, path)

	resp, err := k.client.Read(ctx, apiPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read secret at %s: %w", path, err)
	}

	if resp == nil || resp.Data == nil {
		return nil, fmt.Errorf("%w: %s", errNoDataFoundAtPath, path)
	}

	// KV v2 wraps data in a nested "data" key
	if engine == KVEngineV2 {
		if nestedData, ok := resp.Data["data"].(map[string]interface{}); ok {
			return nestedData, nil
		}
		return nil, fmt.Errorf("%w: %s", errUnexpectedKVV2ResponseStructureAtPath, path)
	}

	return resp.Data, nil
}

// ReadSecretKey reads a specific key from a Vault KV secret.
func (k *KVManager) ReadSecretKey(ctx context.Context, engine KVEngine, path, key string) (string, error) {
	data, err := k.ReadSecret(ctx, engine, path)
	if err != nil {
		return "", err
	}

	value, ok := data[key]
	if !ok {
		return "", fmt.Errorf("%w: %q not found in secret at %s", errKey, key, path)
	}

	strValue, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%w: %q at %s is not a string", errKey, key, path)
	}

	return strValue, nil
}

// ReadSecretCached reads a secret with caching support.
// If the secret is in cache and not expired, the cached version is returned.
func (k *KVManager) ReadSecretCached(ctx context.Context, engine KVEngine, path string, cacheTTL time.Duration) (map[string]interface{}, error) {
	cacheKey := fmt.Sprintf("%s:%s", engine, path)

	// Check cache
	k.mu.RLock()
	if cached, ok := k.cache[cacheKey]; ok && !cached.isExpired() {
		k.mu.RUnlock()
		return cached.data, nil
	}
	k.mu.RUnlock()

	// Fetch from Vault
	data, err := k.ReadSecret(ctx, engine, path)
	if err != nil {
		return nil, err
	}

	// Update cache
	k.mu.Lock()
	k.cache[cacheKey] = &cachedSecret{
		data:      data,
		fetchedAt: time.Now(),
		ttl:       cacheTTL,
	}
	k.mu.Unlock()

	return data, nil
}

// InvalidateCache removes a secret from the cache.
func (k *KVManager) InvalidateCache(engine KVEngine, path string) {
	cacheKey := fmt.Sprintf("%s:%s", engine, path)
	k.mu.Lock()
	delete(k.cache, cacheKey)
	k.mu.Unlock()
}

// InvalidateAllCache clears the entire cache.
func (k *KVManager) InvalidateAllCache() {
	k.mu.Lock()
	k.cache = make(map[string]*cachedSecret)
	k.mu.Unlock()
}

// WriteSecret writes a secret to Vault KV engine.
func (k *KVManager) WriteSecret(ctx context.Context, engine KVEngine, path string, data map[string]interface{}) error {
	apiPath := k.buildKVPath(engine, path)

	var payload map[string]interface{}
	if engine == KVEngineV2 {
		payload = map[string]interface{}{
			"data": data,
		}
	} else {
		payload = data
	}

	_, err := k.client.Write(ctx, apiPath, payload)
	if err != nil {
		return fmt.Errorf("failed to write secret at %s: %w", path, err)
	}

	// Invalidate cache for this path
	k.InvalidateCache(engine, path)

	k.logger.Info("Secret written to Vault",
		zap.String("path", path),
		zap.String("engine", string(engine)))

	return nil
}

// buildKVPath constructs the API path for a KV operation.
func (k *KVManager) buildKVPath(engine KVEngine, path string) string {
	// For KV v2, insert "data/" after the mount path
	if engine == KVEngineV2 {
		// Split path into mount and secret path
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("%s/data/%s", parts[0], parts[1])
		}
		return path + "/data"
	}
	return path
}
