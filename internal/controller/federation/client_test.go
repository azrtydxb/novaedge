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

package federation

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewPeerClient(t *testing.T) {
	peer := &PeerInfo{
		Name:       "test-peer",
		Endpoint:   "localhost:50051",
		TLSEnabled: false,
	}

	config := DefaultConfig()
	logger := zap.NewNop()

	client := NewPeerClient(peer, config, logger)
	if client == nil {
		t.Fatal("NewPeerClient returned nil")
	}

	if client.peer != peer {
		t.Error("peer not set correctly")
	}
	if client.config != config {
		t.Error("config not set correctly")
	}
	if client.logger == nil {
		t.Error("logger should not be nil")
	}
}

func TestPeerClient_IsConnected(t *testing.T) {
	peer := &PeerInfo{
		Name:     "test-peer",
		Endpoint: "localhost:50051",
	}

	client := NewPeerClient(peer, DefaultConfig(), zap.NewNop())

	// Initially not connected
	if client.IsConnected() {
		t.Error("expected IsConnected to be false initially")
	}

	// Set connected state
	client.stateMu.Lock()
	client.connected = true
	client.stateMu.Unlock()

	if !client.IsConnected() {
		t.Error("expected IsConnected to be true")
	}
}

func TestPeerClient_IsHealthy(t *testing.T) {
	peer := &PeerInfo{
		Name:     "test-peer",
		Endpoint: "localhost:50051",
	}

	client := NewPeerClient(peer, DefaultConfig(), zap.NewNop())

	// Initially not healthy
	if client.IsHealthy() {
		t.Error("expected IsHealthy to be false initially")
	}

	// Set healthy state
	client.stateMu.Lock()
	client.healthy = true
	client.stateMu.Unlock()

	if !client.IsHealthy() {
		t.Error("expected IsHealthy to be true")
	}
}

func TestPeerClient_GetLatency(t *testing.T) {
	peer := &PeerInfo{
		Name:     "test-peer",
		Endpoint: "localhost:50051",
	}

	client := NewPeerClient(peer, DefaultConfig(), zap.NewNop())

	// Initially zero
	latency := client.GetLatency()
	if latency != 0 {
		t.Errorf("expected initial latency to be 0, got %v", latency)
	}
}

func TestPeerInfo_Fields(t *testing.T) {
	peer := &PeerInfo{
		Name:               "test-peer",
		Endpoint:           "localhost:50051",
		TLSEnabled:         true,
		Region:             "us-west-2",
		Zone:               "zone-a",
		Priority:           10,
		TLSServerName:      "peer.example.com",
		InsecureSkipVerify: true,
		Labels: map[string]string{
			"environment": "production",
		},
	}

	if peer.Name != "test-peer" {
		t.Errorf("expected Name %q, got %q", "test-peer", peer.Name)
	}
	if peer.Endpoint != "localhost:50051" {
		t.Errorf("expected Endpoint %q, got %q", "localhost:50051", peer.Endpoint)
	}
	if !peer.TLSEnabled {
		t.Error("expected TLSEnabled to be true")
	}
	if peer.Region != "us-west-2" {
		t.Errorf("expected Region %q, got %q", "us-west-2", peer.Region)
	}
	if peer.Zone != "zone-a" {
		t.Errorf("expected Zone %q, got %q", "zone-a", peer.Zone)
	}
	if peer.Priority != 10 {
		t.Errorf("expected Priority %d, got %d", 10, peer.Priority)
	}
	if peer.Labels["environment"] != "production" {
		t.Errorf("expected Labels[environment] = %q", "production")
	}
}

func TestPeerState_Fields(t *testing.T) {
	now := time.Now()
	vc := NewVectorClock()

	state := &PeerState{
		Info: &PeerInfo{
			Name:     "test-peer",
			Endpoint: "localhost:50051",
		},
		VectorClock:  vc,
		LastSeen:     now,
		LastSyncTime: now,
		Healthy:      true,
	}

	if state.Info.Name != "test-peer" {
		t.Errorf("expected Info.Name %q, got %q", "test-peer", state.Info.Name)
	}
	if state.VectorClock == nil {
		t.Error("expected VectorClock to be set")
	}
	if !state.Healthy {
		t.Error("expected Healthy to be true")
	}
}
