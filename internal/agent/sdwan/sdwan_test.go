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

package sdwan

import (
	"context"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestSDWANManager_Lifecycle(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewSDWANManager(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("unexpected error starting manager: %v", err)
	}

	// Add a link
	err := mgr.AddLink(LinkConfig{
		Name:      "wan-1",
		Site:      "dc-east",
		Provider:  "ISP-A",
		Role:      RolePrimary,
		Bandwidth: "1Gbps",
		Cost:      100,
	})
	if err != nil {
		t.Fatalf("unexpected error adding link: %v", err)
	}

	// Add duplicate should fail
	err = mgr.AddLink(LinkConfig{
		Name: "wan-1",
		Site: "dc-west",
	})
	if err == nil {
		t.Error("expected error adding duplicate link")
	}

	// Remove link
	err = mgr.RemoveLink("wan-1")
	if err != nil {
		t.Fatalf("unexpected error removing link: %v", err)
	}

	// Remove nonexistent
	err = mgr.RemoveLink("nonexistent")
	if err == nil {
		t.Error("expected error removing nonexistent link")
	}

	mgr.Stop()
}

func TestSDWANManager_GetLinkQualities(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewSDWANManager(logger)

	// No links - should return empty map
	qualities := mgr.GetLinkQualities()
	if len(qualities) != 0 {
		t.Errorf("expected empty qualities, got %d", len(qualities))
	}
}

func TestSDWANManager_WithProbing(t *testing.T) {
	// Start a local TCP listener for probing
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	logger := zap.NewNop()
	mgr := NewSDWANManager(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("unexpected error starting manager: %v", err)
	}

	err = mgr.AddLink(LinkConfig{
		Name:       "wan-1",
		Site:       "dc-east",
		Provider:   "ISP-A",
		Role:       RolePrimary,
		Bandwidth:  "1Gbps",
		Cost:       50,
		ProbeAddr:  ln.Addr().String(),
		RemoteSite: "dc-west",
	})
	if err != nil {
		t.Fatalf("unexpected error adding link: %v", err)
	}

	// Wait for probe data
	time.Sleep(4 * time.Second)

	qualities := mgr.GetLinkQualities()
	if len(qualities) == 0 {
		t.Error("expected quality data after probing")
	}

	q, ok := qualities["wan-1"]
	if !ok {
		t.Fatal("expected quality for wan-1")
	}
	if q.LatencyMs <= 0 {
		t.Errorf("expected positive latency, got %f", q.LatencyMs)
	}

	mgr.Stop()
}

func TestSDWANManager_SelectPath_NoData(t *testing.T) {
	logger := zap.NewNop()
	mgr := NewSDWANManager(logger)

	_, err := mgr.SelectPath("policy1", StrategyLowestLatency)
	if err == nil {
		t.Error("expected error when no link data available")
	}
}
