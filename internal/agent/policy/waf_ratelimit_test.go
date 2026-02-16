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
	"time"
)

func TestNewWAFMatchCounter(t *testing.T) {
	counter := NewWAFMatchCounter(5 * time.Minute)
	if counter == nil {
		t.Fatal("NewWAFMatchCounter returned nil")
	}
	if counter.ttl != 5*time.Minute {
		t.Errorf("TTL = %v, want 5m", counter.ttl)
	}
}

func TestWAFMatchCounterIncrement(t *testing.T) {
	counter := NewWAFMatchCounter(5 * time.Minute)

	// Initial count should be zero
	if got := counter.Get("192.0.2.1"); got != 0 {
		t.Errorf("initial count = %d, want 0", got)
	}

	// Increment and verify
	counter.Increment("192.0.2.1")
	if got := counter.Get("192.0.2.1"); got != 1 {
		t.Errorf("count after 1 increment = %d, want 1", got)
	}

	counter.Increment("192.0.2.1")
	counter.Increment("192.0.2.1")
	if got := counter.Get("192.0.2.1"); got != 3 {
		t.Errorf("count after 3 increments = %d, want 3", got)
	}
}

func TestWAFMatchCounterMultipleIPs(t *testing.T) {
	counter := NewWAFMatchCounter(5 * time.Minute)

	counter.Increment("192.0.2.1")
	counter.Increment("192.0.2.1")
	counter.Increment("198.51.100.1")

	if got := counter.Get("192.0.2.1"); got != 2 {
		t.Errorf("count for 192.0.2.1 = %d, want 2", got)
	}
	if got := counter.Get("198.51.100.1"); got != 1 {
		t.Errorf("count for 198.51.100.1 = %d, want 1", got)
	}
	if got := counter.Get("203.0.113.1"); got != 0 {
		t.Errorf("count for unknown IP = %d, want 0", got)
	}
}

func TestWAFMatchCounterExpiry(t *testing.T) {
	ttl := 10 * time.Minute
	counter := NewWAFMatchCounter(ttl)

	// Use a controllable time function
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	counter.nowFunc = func() time.Time { return now }

	counter.Increment("192.0.2.1")
	counter.Increment("192.0.2.1")
	counter.Increment("192.0.2.1")

	if got := counter.Get("192.0.2.1"); got != 3 {
		t.Fatalf("count before expiry = %d, want 3", got)
	}

	// Advance time past TTL
	now = now.Add(ttl + 1*time.Second)

	// Get should return 0 for expired entry
	if got := counter.Get("192.0.2.1"); got != 0 {
		t.Errorf("count after expiry = %d, want 0", got)
	}
}

func TestWAFMatchCounterExpiryResetsCount(t *testing.T) {
	ttl := 5 * time.Minute
	counter := NewWAFMatchCounter(ttl)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	counter.nowFunc = func() time.Time { return now }

	counter.Increment("192.0.2.1")
	counter.Increment("192.0.2.1")
	counter.Increment("192.0.2.1")

	if got := counter.Get("192.0.2.1"); got != 3 {
		t.Fatalf("count before expiry = %d, want 3", got)
	}

	// Advance past TTL
	now = now.Add(ttl + 1*time.Second)

	// Increment after expiry should reset count to 1
	counter.Increment("192.0.2.1")
	if got := counter.Get("192.0.2.1"); got != 1 {
		t.Errorf("count after expiry + increment = %d, want 1 (should have reset)", got)
	}
}

func TestWAFMatchCounterCleanup(t *testing.T) {
	ttl := 5 * time.Minute
	counter := NewWAFMatchCounter(ttl)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	counter.nowFunc = func() time.Time { return now }

	counter.Increment("192.0.2.1")
	counter.Increment("198.51.100.1")
	counter.Increment("203.0.113.1")

	// All three should be present
	if got := len(counter.counts); got != 3 {
		t.Fatalf("entries before cleanup = %d, want 3", got)
	}

	// Advance time to expire all entries
	now = now.Add(ttl + 1*time.Second)

	counter.Cleanup()

	if got := len(counter.counts); got != 0 {
		t.Errorf("entries after cleanup = %d, want 0", got)
	}
}

func TestWAFMatchCounterCleanupPartial(t *testing.T) {
	ttl := 5 * time.Minute
	counter := NewWAFMatchCounter(ttl)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	counter.nowFunc = func() time.Time { return now }

	counter.Increment("192.0.2.1") // old entry

	// Advance 3 minutes
	now = now.Add(3 * time.Minute)
	counter.Increment("198.51.100.1") // newer entry

	// Advance another 3 minutes (total 6 from start, 3 from second)
	now = now.Add(3 * time.Minute)

	// First entry is now 6min old (expired), second is 3min old (still valid)
	counter.Cleanup()

	if got := len(counter.counts); got != 1 {
		t.Errorf("entries after partial cleanup = %d, want 1", got)
	}
	if got := counter.Get("192.0.2.1"); got != 0 {
		t.Errorf("expired entry count = %d, want 0", got)
	}
	if got := counter.Get("198.51.100.1"); got != 1 {
		t.Errorf("valid entry count = %d, want 1", got)
	}
}

func TestWAFMatchCounterGetNonExistent(t *testing.T) {
	counter := NewWAFMatchCounter(5 * time.Minute)

	if got := counter.Get("nonexistent"); got != 0 {
		t.Errorf("count for non-existent key = %d, want 0", got)
	}
}
