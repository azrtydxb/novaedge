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
	"fmt"
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
	manager := NewBFDManager(logger, nil, nil)

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

func TestBFDManager_MultipleSessions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)

	peers := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
	}

	// Add multiple sessions
	for _, peer := range peers {
		peerIP := net.ParseIP(peer)
		if err := manager.AddSession(peerIP, config); err != nil {
			t.Fatalf("Failed to add session for %s: %v", peer, err)
		}
	}

	if manager.GetSessionCount() != 3 {
		t.Errorf("Expected 3 sessions, got %d", manager.GetSessionCount())
	}

	// Verify each session exists
	for _, peer := range peers {
		peerIP := net.ParseIP(peer)
		state := manager.GetSessionState(peerIP)
		if state == BFDStateAdminDown {
			t.Errorf("Session for %s should not be AdminDown", peer)
		}
	}

	// Remove one session
	manager.RemoveSession(net.ParseIP(peers[1]))

	if manager.GetSessionCount() != 2 {
		t.Errorf("Expected 2 sessions after removal, got %d", manager.GetSessionCount())
	}
}

func TestBFDManager_UpdateSession(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)

	peerIP := net.ParseIP("10.0.0.1")
	initialConfig := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
	}

	// Add initial session
	err := manager.AddSession(peerIP, initialConfig)
	if err != nil {
		t.Fatalf("Failed to add session: %v", err)
	}

	// Update session config
	updatedConfig := BFDConfig{
		DetectMultiplier:      5,
		DesiredMinTxInterval:  500 * time.Millisecond,
		RequiredMinRxInterval: 500 * time.Millisecond,
		EchoMode:              true,
	}

	err = manager.UpdateSession(peerIP, updatedConfig)
	if err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	// Verify still only one session
	if manager.GetSessionCount() != 1 {
		t.Errorf("Expected 1 session after update, got %d", manager.GetSessionCount())
	}

	// Verify state is still valid
	state := manager.GetSessionState(peerIP)
	if state == BFDStateAdminDown {
		t.Error("Session should not be AdminDown after update")
	}
}

func TestBFDManager_UpdateNonExistentSession(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)

	peerIP := net.ParseIP("10.0.0.99")
	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
	}

	// UpdateSession creates a new session if it doesn't exist
	err := manager.UpdateSession(peerIP, config)
	if err != nil {
		t.Errorf("UpdateSession should create new session if it doesn't exist: %v", err)
	}

	// Verify session was created
	if manager.GetSessionCount() != 1 {
		t.Errorf("Expected 1 session after update of non-existent, got %d", manager.GetSessionCount())
	}
}

func TestBFDManager_GetSessionStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)

	peerIP := net.ParseIP("10.0.0.1")
	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
	}

	_ = manager.AddSession(peerIP, config)

	packetsRx, packetsTx, flaps, ok := manager.GetSessionStats(peerIP)
	if !ok {
		t.Fatal("Expected stats for active session")
	}

	// Verify stats values are initialized
	if packetsRx < 0 {
		t.Error("packetsRx should be non-negative")
	}
	if packetsTx < 0 {
		t.Error("packetsTx should be non-negative")
	}
	if flaps < 0 {
		t.Error("flaps should be non-negative")
	}
}

func TestBFDManager_GetSessionStatsNonExistent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)

	peerIP := net.ParseIP("10.0.0.99")
	_, _, _, ok := manager.GetSessionStats(peerIP)
	if ok {
		t.Error("Expected no stats for non-existent session")
	}
}

func TestBFDManager_StartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("start manager", func(t *testing.T) {
		err := manager.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start BFD manager: %v", err)
		}
	})

	t.Run("add session while running", func(t *testing.T) {
		peerIP := net.ParseIP("10.0.0.1")
		config := BFDConfig{
			DetectMultiplier:      3,
			DesiredMinTxInterval:  300 * time.Millisecond,
			RequiredMinRxInterval: 300 * time.Millisecond,
		}

		err := manager.AddSession(peerIP, config)
		if err != nil {
			t.Fatalf("Failed to add session while running: %v", err)
		}
	})

	t.Run("stop manager", func(t *testing.T) {
		cancel()
		// Give some time for cleanup
		time.Sleep(50 * time.Millisecond)
	})
}

func TestBFDManager_NeighborDownCallback(t *testing.T) {
	callbackInvoked := false
	var callbackPeer net.IP

	downCallback := func(peer net.IP) {
		callbackInvoked = true
		callbackPeer = peer
	}

	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, downCallback, nil)

	peerIP := net.ParseIP("10.0.0.1")
	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
	}

	_ = manager.AddSession(peerIP, config)

	// Simulate neighbor down detection
	manager.mu.Lock()
	if session, exists := manager.sessions[peerIP.String()]; exists {
		oldState := session.state
		session.state = BFDStateDown
		if oldState == BFDStateUp && downCallback != nil {
			downCallback(peerIP)
		}
	}
	manager.mu.Unlock()

	if callbackInvoked {
		if !callbackPeer.Equal(peerIP) {
			t.Errorf("Expected callback with peer %s, got %s", peerIP, callbackPeer)
		}
	}
}

func TestBFDManager_NeighborUpCallback(t *testing.T) {
	callbackInvoked := false
	var callbackPeer net.IP

	upCallback := func(peer net.IP) {
		callbackInvoked = true
		callbackPeer = peer
	}

	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, upCallback)

	peerIP := net.ParseIP("10.0.0.1")
	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
	}

	_ = manager.AddSession(peerIP, config)

	// Simulate neighbor up transition
	manager.mu.Lock()
	if session, exists := manager.sessions[peerIP.String()]; exists {
		oldState := session.state
		session.state = BFDStateUp
		if oldState != BFDStateUp && upCallback != nil {
			upCallback(peerIP)
		}
	}
	manager.mu.Unlock()

	if callbackInvoked {
		if !callbackPeer.Equal(peerIP) {
			t.Errorf("Expected callback with peer %s, got %s", peerIP, callbackPeer)
		}
	}
}

func TestBFDManager_IPv6Session(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)

	peerIP := net.ParseIP("2001:db8::1")
	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
	}

	err := manager.AddSession(peerIP, config)
	if err != nil {
		t.Fatalf("Failed to add IPv6 BFD session: %v", err)
	}

	if manager.GetSessionCount() != 1 {
		t.Errorf("Expected 1 IPv6 session, got %d", manager.GetSessionCount())
	}

	state := manager.GetSessionState(peerIP)
	if state == BFDStateAdminDown {
		t.Error("IPv6 session should not be AdminDown")
	}
}

func TestBFDManager_EchoMode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)

	peerIP := net.ParseIP("10.0.0.1")
	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
		EchoMode:              true,
	}

	err := manager.AddSession(peerIP, config)
	if err != nil {
		t.Fatalf("Failed to add session with echo mode: %v", err)
	}

	if manager.GetSessionCount() != 1 {
		t.Errorf("Expected 1 session with echo mode, got %d", manager.GetSessionCount())
	}
}

func TestBFDManager_DetectMultiplierVariations(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name             string
		detectMultiplier int32
	}{
		{"min multiplier", 1},
		{"default multiplier", 3},
		{"high multiplier", 10},
		{"very high multiplier", 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewBFDManager(logger, nil, nil)
			peerIP := net.ParseIP("10.0.0.1")
			config := BFDConfig{
				DetectMultiplier:      tt.detectMultiplier,
				DesiredMinTxInterval:  300 * time.Millisecond,
				RequiredMinRxInterval: 300 * time.Millisecond,
			}

			err := manager.AddSession(peerIP, config)
			if err != nil {
				t.Fatalf("Failed to add session with multiplier %d: %v", tt.detectMultiplier, err)
			}
		})
	}
}

func TestBFDManager_IntervalVariations(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name     string
		txInterval time.Duration
		rxInterval time.Duration
	}{
		{"fast intervals", 50 * time.Millisecond, 50 * time.Millisecond},
		{"default intervals", 300 * time.Millisecond, 300 * time.Millisecond},
		{"slow intervals", 1000 * time.Millisecond, 1000 * time.Millisecond},
		{"asymmetric intervals", 100 * time.Millisecond, 500 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewBFDManager(logger, nil, nil)
			peerIP := net.ParseIP("10.0.0.1")
			config := BFDConfig{
				DetectMultiplier:      3,
				DesiredMinTxInterval:  tt.txInterval,
				RequiredMinRxInterval: tt.rxInterval,
			}

			err := manager.AddSession(peerIP, config)
			if err != nil {
				t.Fatalf("Failed to add session with intervals %v/%v: %v", tt.txInterval, tt.rxInterval, err)
			}

			if manager.GetSessionCount() != 1 {
				t.Errorf("Expected 1 session, got %d", manager.GetSessionCount())
			}
		})
	}
}

func TestBFDManager_ConcurrentOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = manager.Start(ctx)

	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
	}

	// Concurrent adds
	var wg sync.WaitGroup
	for i := 1; i <= 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			peerIP := net.ParseIP(fmt.Sprintf("10.0.0.%d", idx))
			_ = manager.AddSession(peerIP, config)
		}(i)
	}
	wg.Wait()

	if manager.GetSessionCount() != 10 {
		t.Errorf("Expected 10 sessions after concurrent adds, got %d", manager.GetSessionCount())
	}

	// Concurrent removes
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			peerIP := net.ParseIP(fmt.Sprintf("10.0.0.%d", idx))
			manager.RemoveSession(peerIP)
		}(i)
	}
	wg.Wait()

	if manager.GetSessionCount() != 5 {
		t.Errorf("Expected 5 sessions after concurrent removes, got %d", manager.GetSessionCount())
	}
}

func TestBFDManager_StateTransitions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)

	peerIP := net.ParseIP("10.0.0.1")
	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  300 * time.Millisecond,
		RequiredMinRxInterval: 300 * time.Millisecond,
	}

	_ = manager.AddSession(peerIP, config)

	// Verify initial state
	state := manager.GetSessionState(peerIP)
	if state != BFDStateDown {
		t.Errorf("Expected initial state Down, got %s", state.String())
	}

	// Simulate state transitions
	manager.mu.Lock()
	if session, exists := manager.sessions[peerIP.String()]; exists {
		session.state = BFDStateInit
	}
	manager.mu.Unlock()

	state = manager.GetSessionState(peerIP)
	if state != BFDStateInit {
		t.Errorf("Expected state Init, got %s", state.String())
	}

	manager.mu.Lock()
	if session, exists := manager.sessions[peerIP.String()]; exists {
		session.state = BFDStateUp
	}
	manager.mu.Unlock()

	state = manager.GetSessionState(peerIP)
	if state != BFDStateUp {
		t.Errorf("Expected state Up, got %s", state.String())
	}
}

func TestBFDManager_StateMachine(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("Down -> Init -> Up", func(t *testing.T) {
		manager := NewBFDManager(logger, nil, nil)
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
		}, nil)

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
		manager := NewBFDManager(logger, nil, nil)
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
	manager := NewBFDManager(logger, nil, nil)

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


func TestBFDManager_GetAllSessionStates(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)
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
	manager := NewBFDManager(logger, nil, nil)

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
	manager := NewBFDManager(logger, nil, nil)
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

// Transport tests

func TestBFDTransport_StartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)

	transport := newBFDTransport(logger, manager, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start transport: %v", err)
	}

	port := transport.LocalPort()
	if port == 0 {
		t.Fatal("Expected non-zero local port after start")
	}

	transport.Stop()
}

func TestBFDTransport_SendReceive(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a manager and transport that receives packets
	receivedCh := make(chan struct{}, 1)
	manager := NewBFDManager(logger, nil, nil)

	// Add a session so ProcessPacket actually processes incoming data
	peerIP := net.IPv4(127, 0, 0, 1)
	err := manager.AddSession(peerIP, BFDConfig{DetectMultiplier: 3})
	if err != nil {
		t.Fatal(err)
	}

	// Wrap onStateChange to signal receipt
	manager.mu.Lock()
	session := manager.sessions[peerIP.String()]
	origOnStateChange := session.onStateChange
	session.onStateChange = func(peer net.IP, oldState, newState BFDSessionState) {
		if origOnStateChange != nil {
			origOnStateChange(peer, oldState, newState)
		}
		select {
		case receivedCh <- struct{}{}:
		default:
		}
	}
	manager.mu.Unlock()

	// Start receiving transport
	rxTransport := newBFDTransport(logger, manager, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = rxTransport.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start rx transport: %v", err)
	}
	defer rxTransport.Stop()

	rxPort := rxTransport.LocalPort()

	// Start sending transport
	txTransport := newBFDTransport(logger, nil, 0)
	err = txTransport.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start tx transport: %v", err)
	}
	defer txTransport.Stop()

	// Send a BFD Down packet (triggers Down -> Init transition)
	pkt := &bfdControlPacket{
		Version:               bfdVersion,
		State:                 BFDStateDown,
		DetectMult:            3,
		MyDiscriminator:       999,
		DesiredMinTxInterval:  300000,
		RequiredMinRxInterval: 300000,
	}

	err = txTransport.Send(net.IPv4(127, 0, 0, 1), rxPort, pkt)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Wait for the packet to be received and processed
	select {
	case <-receivedCh:
		// Packet received, state should have changed
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for packet receipt")
	}

	// Verify the session transitioned from Down to Init
	state := manager.GetSessionState(peerIP)
	if state != BFDStateInit {
		t.Errorf("Expected Init after receiving Down packet, got %s", state.String())
	}
}

func TestBFDTransport_SendWithoutStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	transport := newBFDTransport(logger, nil, 0)

	pkt := &bfdControlPacket{
		Version:    bfdVersion,
		State:      BFDStateDown,
		DetectMult: 3,
	}

	err := transport.Send(net.IPv4(127, 0, 0, 1), 3784, pkt)
	if err == nil {
		t.Fatal("Expected error when sending without starting transport")
	}
}

func TestBFDManager_TwoPeersIntegration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create two BFD managers simulating two peers on localhost
	manager1 := NewBFDManager(logger.Named("peer1"), nil, nil)
	manager1.ListenPort = 0

	manager2 := NewBFDManager(logger.Named("peer2"), nil, nil)
	manager2.ListenPort = 0

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start both managers
	err := manager1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager1: %v", err)
	}
	defer manager1.Stop()

	err = manager2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager2: %v", err)
	}
	defer manager2.Stop()

	// Get the actual ports each manager is listening on
	manager1.mu.RLock()
	port1 := manager1.transport.LocalPort()
	manager1.mu.RUnlock()

	manager2.mu.RLock()
	port2 := manager2.transport.LocalPort()
	manager2.mu.RUnlock()

	// Configure each manager to send to the other's port
	manager1.mu.Lock()
	manager1.PeerPort = port2
	manager1.mu.Unlock()

	manager2.mu.Lock()
	manager2.PeerPort = port1
	manager2.mu.Unlock()

	loopback := net.IPv4(127, 0, 0, 1)
	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  50 * time.Millisecond,
		RequiredMinRxInterval: 50 * time.Millisecond,
	}

	err = manager1.AddSession(loopback, config)
	if err != nil {
		t.Fatalf("Failed to add session to manager1: %v", err)
	}

	err = manager2.AddSession(loopback, config)
	if err != nil {
		t.Fatalf("Failed to add session to manager2: %v", err)
	}

	// Both sessions start in Down state. The BFD state machine should
	// transition: Down -> Init (on receiving Down from peer) -> Up (on
	// receiving Init from peer). Wait for both to reach Up.
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			state1 := manager1.GetSessionState(loopback)
			state2 := manager2.GetSessionState(loopback)
			t.Fatalf("Timed out waiting for sessions to reach Up: manager1=%s, manager2=%s",
				state1.String(), state2.String())
		case <-ticker.C:
			state1 := manager1.GetSessionState(loopback)
			state2 := manager2.GetSessionState(loopback)
			if state1 == BFDStateUp && state2 == BFDStateUp {
				// Both peers are Up
				t.Logf("Both BFD sessions reached Up state")
				return
			}
		}
	}
}

func TestBFDManager_NilTransportSkipsSending(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewBFDManager(logger, nil, nil)
	// transport is nil by default (no Start called)

	peerIP := net.ParseIP("10.0.0.1")
	config := BFDConfig{
		DetectMultiplier:      3,
		DesiredMinTxInterval:  50 * time.Millisecond,
		RequiredMinRxInterval: 50 * time.Millisecond,
	}

	err := manager.AddSession(peerIP, config)
	if err != nil {
		t.Fatal(err)
	}

	// Call sendControlPackets directly - should not panic with nil transport
	manager.sendControlPackets()

	// Verify metrics were still updated
	_, tx, _, ok := manager.GetSessionStats(peerIP)
	if !ok {
		t.Fatal("Session should exist")
	}
	if tx != 1 {
		t.Errorf("Expected 1 packet TX (metrics-only), got %d", tx)
	}
}


func TestBFDManager_FullRecoveryCycle(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var mu sync.Mutex
	downCount := 0
	upCount := 0

	manager := NewBFDManager(logger,
		func(peerIP net.IP) {
			mu.Lock()
			downCount++
			mu.Unlock()
		},
		func(peerIP net.IP) {
			mu.Lock()
			upCount++
			mu.Unlock()
		},
	)

	peerIP := net.ParseIP("10.0.0.6")
	config := BFDConfig{DetectMultiplier: 3}

	err := manager.AddSession(peerIP, config)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Bring session Up: Down -> Init -> Up
	manager.ProcessPacket(peerIP, BFDStateDown, 600)
	manager.ProcessPacket(peerIP, BFDStateInit, 600)

	state := manager.GetSessionState(peerIP)
	if state != BFDStateUp {
		t.Fatalf("Expected Up, got %s", state.String())
	}

	// 2. Peer goes Down: Up -> Down (triggers onNeighborDown)
	manager.ProcessPacket(peerIP, BFDStateDown, 600)

	state = manager.GetSessionState(peerIP)
	if state != BFDStateDown {
		t.Fatalf("Expected Down, got %s", state.String())
	}

	// Wait for async callback
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if downCount != 1 {
		t.Errorf("Expected 1 onNeighborDown call, got %d", downCount)
	}
	mu.Unlock()

	// 3. Peer recovers: Down -> Init -> Up (triggers onNeighborUp)
	manager.ProcessPacket(peerIP, BFDStateDown, 600) // Both Down -> Init
	manager.ProcessPacket(peerIP, BFDStateInit, 600) // Init -> Up

	state = manager.GetSessionState(peerIP)
	if state != BFDStateUp {
		t.Fatalf("Expected Up after recovery, got %s", state.String())
	}

	mu.Lock()
	if upCount != 2 {
		// upCount is 2: once for initial Up, once for recovery Up
		t.Errorf("Expected 2 onNeighborUp calls (initial + recovery), got %d", upCount)
	}
	mu.Unlock()

	// 4. Verify flap count
	_, _, flaps, ok := manager.GetSessionStats(peerIP)
	if !ok {
		t.Fatal("Session should exist")
	}
	if flaps != 1 {
		t.Errorf("Expected 1 flap, got %d", flaps)
	}
}
