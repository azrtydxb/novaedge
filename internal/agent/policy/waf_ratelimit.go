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
	"sync"
	"time"
)

// WAFMatchCounter tracks per-IP WAF match counts with TTL expiry.
// This allows the rate limiter to query how many WAF violations an IP
// has accumulated and dynamically throttle repeat offenders.
type WAFMatchCounter struct {
	mu      sync.RWMutex
	counts  map[string]*matchEntry
	ttl     time.Duration
	nowFunc func() time.Time // for testing
}

type matchEntry struct {
	count    int
	lastSeen time.Time
}

// NewWAFMatchCounter creates a match counter with the given TTL for entries.
// Entries older than ttl are considered expired.
func NewWAFMatchCounter(ttl time.Duration) *WAFMatchCounter {
	return &WAFMatchCounter{
		counts:  make(map[string]*matchEntry),
		ttl:     ttl,
		nowFunc: time.Now,
	}
}

// Increment adds a WAF match for the given IP
func (c *WAFMatchCounter) Increment(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.nowFunc()
	entry, ok := c.counts[ip]
	if !ok || now.Sub(entry.lastSeen) > c.ttl {
		c.counts[ip] = &matchEntry{count: 1, lastSeen: now}
		return
	}
	entry.count++
	entry.lastSeen = now
}

// Get returns the WAF match count for the given IP.
// Returns 0 if the entry has expired or doesn't exist.
func (c *WAFMatchCounter) Get(ip string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.counts[ip]
	if !ok {
		return 0
	}
	if c.nowFunc().Sub(entry.lastSeen) > c.ttl {
		return 0
	}
	return entry.count
}

// Cleanup removes expired entries to prevent unbounded memory growth
func (c *WAFMatchCounter) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.nowFunc()
	for ip, entry := range c.counts {
		if now.Sub(entry.lastSeen) > c.ttl {
			delete(c.counts, ip)
		}
	}
}
