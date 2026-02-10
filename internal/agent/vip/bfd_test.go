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

package vip

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestBFDSessionState_String(t *testing.T) {
	tests := []struct {
		state    BFDSessionState
		expected string
	}{
		{BFDStateAdminDown, "AdminDown"},
		{BFDStateDown, "Down"},
		{BFDStateInit, "Init"},
		{BFDStateUp, "Up"},
		{BFDSessionState(99), "Unknown(99)"},
	}

	for _, tt := range tests {
		result := tt.state.String()
		if result != tt.expected {
			t.Errorf("BFDSessionState(%d).String() = %s, want %s", tt.state, result, tt.expected)
		}
	}
}

func TestBFDManager_AddRemoveSession(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil)

	peerIP := net.ParseIP("10.0.0.1")
	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
	}

	t.Run("add session", func(t *testing.T) {
		err := manager.AddSession(peerIP, config)
		if err != nil {
			t.Fatalf("Failed to add BFD session: %v", err)
		}

		if manager.GetSessionCount() != 1 {
			t.Errorf("Expected 1 session, got %d", manager.GetSessionCount())
		}

		state := manager.GetSessionState(peerIP)
		if state != BFDStateDown {
			t.Errorf("Expected initial state Down, got %s", state.String())
		}
	})

	t.Run("add duplicate session", func(t *testing.T) {
		err := manager.AddSession(peerIP, config)
		if err != nil {
			t.Fatalf("Duplicate add should not error: %v", err)
		}

		if manager.GetSessionCount() != 1 {
			t.Errorf("Expected still 1 session, got %d", manager.GetSessionCount())
		}
	})

	t.Run("remove session", func(t *testing.T) {
		manager.RemoveSession(peerIP)

		if manager.GetSessionCount() != 0 {
			t.Errorf("Expected 0 sessions, got %d", manager.GetSessionCount())
		}
	})

	t.Run("get state of non-existent session", func(t *testing.T) {
		state := manager.GetSessionState(net.ParseIP("10.0.0.99"))
		if state != BFDStateDown {
			t.Errorf("Expected Down for non-existent session, got %s", state.String())
		}
	})
}

func TestBFDManager_StateMachine(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("Down -> Init -> Up", func(t *testing.T) {
		manager := NewBFDManager(logger, nil)
		peerIP := net.ParseIP("10.0.0.1")
		config := BFDConfig{DetectMultiplier: 3}

		err := manager.AddSession(peerIP, config)
		if err != nil {
			t.Fatal(err)
		}

		// Remote sends Down -> local goes to Init
		manager.ProcessPacket(peerIP, BFDStateDown, 100)
		state := manager.GetSessionState(peerIP)
		if state != BFDStateInit {
			t.Errorf("Expected Init after receiving Down, got %s", state.String())
		}

		// Remote sends Init -> local goes to Up
		manager.ProcessPacket(peerIP, BFDStateInit, 100)
		state = manager.GetSessionState(peerIP)
		if state != BFDStateUp {
			t.Errorf("Expected Up after receiving Init, got %s", state.String())
		}
	})

	t.Run("Up -> Down on remote AdminDown", func(t *testing.T) {
		var mu sync.Mutex
		neighborDownCalled := false
		manager := NewBFDManager(logger, func(peerIP net.IP) {
			mu.Lock()
			neighborDownCalled = true
			mu.Unlock()
		})

		peerIP := net.ParseIP("10.0.0.2")
		config := BFDConfig{DetectMultiplier: 3}

		err := manager.AddSession(peerIP, config)
		if err != nil {
			t.Fatal(err)
		}

		// Bring session to Up
		manager.ProcessPacket(peerIP, BFDStateDown, 200)
		manager.ProcessPacket(peerIP, BFDStateInit, 200)

		state := manager.GetSessionState(peerIP)
		if state != BFDStateUp {
			t.Fatalf("Expected Up, got %s", state.String())
		}

		// Remote goes AdminDown
		manager.ProcessPacket(peerIP, BFDStateAdminDown, 200)
		state = manager.GetSessionState(peerIP)
		if state != BFDStateDown {
			t.Errorf("Expected Down after AdminDown, got %s", state.String())
		}

		// Wait for async callback
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		if !neighborDownCalled {
			t.Error("Expected onNeighborDown callback to be called")
		}
		mu.Unlock()
	})

	t.Run("session flap counting", func(t *testing.T) {
		manager := NewBFDManager(logger, nil)
		peerIP := net.ParseIP("10.0.0.3")
		config := BFDConfig{DetectMultiplier: 3}

		err := manager.AddSession(peerIP, config)
		if err != nil {
			t.Fatal(err)
		}

		// Bring to Up
		manager.ProcessPacket(peerIP, BFDStateDown, 300)
		manager.ProcessPacket(peerIP, BFDStateInit, 300)

		// Flap down
		manager.ProcessPacket(peerIP, BFDStateDown, 300)

		_, _, flaps, ok := manager.GetSessionStats(peerIP)
		if !ok {
			t.Fatal("Session should exist")
		}
		if flaps != 1 {
			t.Errorf("Expected 1 flap, got %d", flaps)
		}
	})
}

func TestBFDManager_DetectionTimeout(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil)

	peerIP := net.ParseIP("10.0.0.1")
	config := BFDConfig{
		DetectMultiplier:      2,
		RequiredMinRxInterval: 50 * time.Millisecond,
	}

	err := manager.AddSession(peerIP, config)
	if err != nil {
		t.Fatal(err)
	}

	// Bring to Up
	manager.ProcessPacket(peerIP, BFDStateDown, 100)
	manager.ProcessPacket(peerIP, BFDStateInit, 100)

	state := manager.GetSessionState(peerIP)
	if state != BFDStateUp {
		t.Fatalf("Expected Up, got %s", state.String())
	}

	// Wait for detection timeout (2 * 50ms = 100ms)
	// Run detection check manually
	time.Sleep(150 * time.Millisecond)
	manager.checkDetectionTimeouts()

	state = manager.GetSessionState(peerIP)
	if state != BFDStateDown {
		t.Errorf("Expected Down after timeout, got %s", state.String())
	}
}

func TestBFDManager_StartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start BFD manager: %v", err)
	}

	// Wait for context to expire
	<-ctx.Done()

	manager.Stop()
	// Should not panic or deadlock
}

func TestBFDManager_GetAllSessionStates(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil)
	config := BFDConfig{DetectMultiplier: 3}

	peers := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	for _, peer := range peers {
		err := manager.AddSession(net.ParseIP(peer), config)
		if err != nil {
			t.Fatal(err)
		}
	}

	states := manager.GetAllSessionStates()
	if len(states) != 3 {
		t.Errorf("Expected 3 states, got %d", len(states))
	}

	for peer, state := range states {
		if state != BFDStateDown {
			t.Errorf("Expected Down for %s, got %s", peer, state.String())
		}
	}
}

func TestBFDManager_DefaultConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil)

	peerIP := net.ParseIP("10.0.0.1")
	// Pass zero-value config to verify defaults are applied
	config := BFDConfig{}

	err := manager.AddSession(peerIP, config)
	if err != nil {
		t.Fatal(err)
	}

	manager.mu.RLock()
	session := manager.sessions["10.0.0.1"]
	manager.mu.RUnlock()

	session.mu.RLock()
	defer session.mu.RUnlock()

	if session.detectMultiplier != bfdDefaultDetectMult {
		t.Errorf("Expected default detect mult %d, got %d", bfdDefaultDetectMult, session.detectMultiplier)
	}
	if session.desiredMinTxInterval != bfdDefaultDesiredMinTx {
		t.Errorf("Expected default desired min TX %v, got %v", bfdDefaultDesiredMinTx, session.desiredMinTxInterval)
	}
}

func TestBFDManager_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil)
	config := BFDConfig{DetectMultiplier: 3}

	var wg sync.WaitGroup

	// Concurrent add/remove/get operations
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			peerIP := net.ParseIP(net.IPv4(10, 0, 0, byte(id)).String())
			_ = manager.AddSession(peerIP, config)
			_ = manager.GetSessionState(peerIP)
			_ = manager.GetSessionCount()
			manager.GetAllSessionStates()
		}(i)
	}

	wg.Wait()
}
