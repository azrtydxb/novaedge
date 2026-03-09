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

package novanet

import (
	"context"
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestNewClient(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	c := NewClient("/tmp/test.sock", logger)
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.socketPath != "/tmp/test.sock" {
		t.Fatalf("socketPath = %q, want %q", c.socketPath, "/tmp/test.sock")
	}
	if c.IsConnected() {
		t.Fatal("new client should not be connected")
	}
}

func TestDegradedMode(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	c := NewClient("/tmp/nonexistent.sock", logger)

	ctx := context.Background()

	// All operations should return nil in degraded mode (not connected).
	if err := c.EnableSockmap(ctx, "ns", "pod"); err != nil {
		t.Fatalf("EnableSockmap in degraded mode: %v", err)
	}
	if err := c.DisableSockmap(ctx, "ns", "pod"); err != nil {
		t.Fatalf("DisableSockmap in degraded mode: %v", err)
	}
	if err := c.AddMeshRedirect(ctx, "10.0.0.1", 80, 15001); err != nil {
		t.Fatalf("AddMeshRedirect in degraded mode: %v", err)
	}
	if err := c.RemoveMeshRedirect(ctx, "10.0.0.1", 80); err != nil {
		t.Fatalf("RemoveMeshRedirect in degraded mode: %v", err)
	}
	if err := c.ConfigureRateLimit(ctx, "10.0.0.0/24", 100, 200); err != nil {
		t.Fatalf("ConfigureRateLimit in degraded mode: %v", err)
	}
	if err := c.RemoveRateLimit(ctx, "10.0.0.0/24"); err != nil {
		t.Fatalf("RemoveRateLimit in degraded mode: %v", err)
	}

	allowed, denied, err := c.GetRateLimitStats(ctx, "10.0.0.0/24")
	if err != nil {
		t.Fatalf("GetRateLimitStats in degraded mode: %v", err)
	}
	if allowed != 0 || denied != 0 {
		t.Fatalf("GetRateLimitStats = (%d, %d), want (0, 0)", allowed, denied)
	}

	info, err := c.GetBackendHealth(ctx, "10.0.0.1", 8080)
	if err != nil {
		t.Fatalf("GetBackendHealth in degraded mode: %v", err)
	}
	if info != nil {
		t.Fatal("GetBackendHealth should return nil in degraded mode")
	}
}

func TestClose(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	c := NewClient("/tmp/test.sock", logger)
	// Close on a never-connected client should be safe.
	if err := c.Close(); err != nil {
		t.Fatalf("Close on unconnected client: %v", err)
	}
}
