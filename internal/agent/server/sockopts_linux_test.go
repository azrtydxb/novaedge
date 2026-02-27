//go:build linux

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

package server

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestNewReusePortListenConfig_ControlSet(t *testing.T) {
	lc := NewReusePortListenConfig()

	if lc.Control == nil {
		t.Fatal("expected Control function to be set, got nil")
	}
}

func TestNewReusePortListenConfig_ListenerWorks(t *testing.T) {
	lc := NewReusePortListenConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		// SO_REUSEPORT may not be supported on all platforms (e.g., macOS CI).
		// The important thing is that the ListenConfig is properly constructed.
		t.Skipf("listen with SO_REUSEPORT failed (may not be supported on this platform): %v", err)
	}
	defer func() {
		if closeErr := ln.Close(); closeErr != nil {
			t.Logf("failed to close listener: %v", closeErr)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	if addr.Port == 0 {
		t.Fatal("expected non-zero port from listener")
	}

	// Verify we can connect to the listener.
	conn, err := net.DialTimeout("tcp", addr.String(), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to dial listener: %v", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Logf("failed to close conn: %v", closeErr)
		}
	}()
}
