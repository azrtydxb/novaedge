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
	"sync"
	"time"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
)

// TokenBlacklist maintains an in-memory set of revoked JWT token IDs (jti claims)
// with their expiry times. Expired entries are automatically cleaned up.
type TokenBlacklist struct {
	mu      sync.RWMutex
	entries map[string]time.Time // jti -> expiry time
}

// NewTokenBlacklist creates a new empty TokenBlacklist.
func NewTokenBlacklist() *TokenBlacklist {
	return &TokenBlacklist{
		entries: make(map[string]time.Time),
	}
}

// Add adds a token ID to the blacklist with the given expiry time.
// Once the expiry time passes, the entry will be removed during cleanup.
func (b *TokenBlacklist) Add(jti string, expiry time.Time) {
	b.mu.Lock()
	b.entries[jti] = expiry
	b.mu.Unlock()

	metrics.JWTRevocationsTotal.Inc()
	metrics.JWTBlacklistSize.Set(float64(b.Size()))
}

// IsBlacklisted checks whether the given token ID is present in the blacklist
// and has not yet expired.
func (b *TokenBlacklist) IsBlacklisted(jti string) bool {
	b.mu.RLock()
	expiry, exists := b.entries[jti]
	b.mu.RUnlock()

	if !exists {
		return false
	}

	// If the entry has expired, it is no longer considered blacklisted.
	// The Cleanup goroutine will eventually remove it.
	if time.Now().After(expiry) {
		return false
	}

	return true
}

// Cleanup removes all entries whose expiry time has passed.
func (b *TokenBlacklist) Cleanup() {
	now := time.Now()
	b.mu.Lock()
	for jti, expiry := range b.entries {
		if now.After(expiry) {
			delete(b.entries, jti)
		}
	}
	b.mu.Unlock()

	metrics.JWTBlacklistSize.Set(float64(b.Size()))
}

// StartCleanup launches a goroutine that periodically removes expired entries
// from the blacklist. It stops when the context is cancelled.
func (b *TokenBlacklist) StartCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.Cleanup()
			}
		}
	}()
}

// Size returns the current number of entries in the blacklist.
func (b *TokenBlacklist) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.entries)
}
