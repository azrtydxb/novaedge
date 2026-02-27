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

package ratelimit

import (
	"net"
	"runtime"
	"strings"
	"testing"

	"go.uber.org/zap/zaptest"
)

// skipIfBPFUnavailable skips the test if the error indicates that eBPF map
// creation failed due to insufficient privileges (MEMLOCK limit or missing
// CAP_BPF/CAP_SYS_ADMIN). This allows tests to pass in unprivileged CI.
func skipIfBPFUnavailable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "operation not permitted") ||
		strings.Contains(msg, "MEMLOCK") {
		t.Skipf("Skipping: eBPF unavailable (insufficient privileges): %v", err)
	}
}

func TestNewRateLimiter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	rl, err := NewRateLimiter(logger, 1000)

	if runtime.GOOS != "linux" {
		// On non-Linux, creation should fail.
		if err == nil {
			t.Fatal("expected error on non-Linux platform")
		}
		return
	}

	skipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewRateLimiter() returned error: %v", err)
	}
	defer rl.Close()

	if !rl.IsActive() {
		t.Error("expected IsActive() == true after creation")
	}
}

func TestRateLimiterConfigure(t *testing.T) {
	logger := zaptest.NewLogger(t)
	rl, err := NewRateLimiter(logger, 1000)

	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected error on non-Linux platform")
		}
		return
	}

	skipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewRateLimiter() returned error: %v", err)
	}
	defer rl.Close()

	if err := rl.Configure(100, 200); err != nil {
		t.Errorf("Configure() returned error: %v", err)
	}
}

func TestRateLimiterGetStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	rl, err := NewRateLimiter(logger, 1000)

	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected error on non-Linux platform")
		}
		return
	}

	skipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewRateLimiter() returned error: %v", err)
	}
	defer rl.Close()

	stats, err := rl.GetStats()
	if err != nil {
		t.Errorf("GetStats() returned error: %v", err)
	}

	// Fresh limiter should have zero stats.
	if stats.Allowed != 0 || stats.Denied != 0 {
		t.Errorf("expected zero stats, got allowed=%d denied=%d",
			stats.Allowed, stats.Denied)
	}
}

func TestRateLimiterCheckAllowed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	rl, err := NewRateLimiter(logger, 1000)

	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected error on non-Linux platform")
		}
		return
	}

	skipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewRateLimiter() returned error: %v", err)
	}
	defer rl.Close()

	// Unknown IP should be allowed (not in map yet).
	allowed, err := rl.CheckAllowed(net.ParseIP("10.0.0.1"))
	if err != nil {
		t.Errorf("CheckAllowed() returned error: %v", err)
	}
	if !allowed {
		t.Error("expected unknown IP to be allowed")
	}
}

func TestRateLimiterCloseIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	rl, err := NewRateLimiter(logger, 1000)

	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected error on non-Linux platform")
		}
		// Closing a nil limiter stub should be safe.
		return
	}

	skipIfBPFUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewRateLimiter() returned error: %v", err)
	}

	if err := rl.Close(); err != nil {
		t.Errorf("first Close() returned error: %v", err)
	}
	if err := rl.Close(); err != nil {
		t.Errorf("second Close() returned error: %v", err)
	}

	if rl.IsActive() {
		t.Error("expected IsActive() == false after Close")
	}
}

func TestRateLimitKeyIPv4(t *testing.T) {
	key := RateLimitKey{}
	ip := net.ParseIP("192.168.1.1").To16()
	copy(key.IP[:], ip)

	// Verify the key is non-zero.
	allZero := true
	for _, b := range key.IP {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("expected non-zero key for valid IPv4")
	}
}

func TestRateLimitKeyIPv6(t *testing.T) {
	key := RateLimitKey{}
	ip := net.ParseIP("2001:db8::1").To16()
	copy(key.IP[:], ip)

	if key.IP[0] != 0x20 || key.IP[1] != 0x01 {
		t.Errorf("unexpected IPv6 address bytes: %v", key.IP)
	}
}

func TestRateLimitConfig(t *testing.T) {
	config := RateLimitConfig{
		Rate:     100,
		Burst:    200,
		WindowNS: 1_000_000_000,
	}
	if config.Rate != 100 {
		t.Errorf("unexpected rate: %d", config.Rate)
	}
	if config.Burst != 200 {
		t.Errorf("unexpected burst: %d", config.Burst)
	}
	if config.WindowNS != 1_000_000_000 {
		t.Errorf("unexpected window: %d", config.WindowNS)
	}
}

func TestRateLimitStats(t *testing.T) {
	stats := RateLimitStats{
		Allowed: 1000,
		Denied:  50,
	}
	if stats.Allowed != 1000 || stats.Denied != 50 {
		t.Errorf("unexpected stats: %+v", stats)
	}
}
