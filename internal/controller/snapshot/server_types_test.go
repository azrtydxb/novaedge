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

package snapshot

import (
	"testing"
	"time"
)

func TestConnectionStatus_Constants(t *testing.T) {
	if ConnectionStatusConnected != "connected" {
		t.Errorf("ConnectionStatusConnected = %q, want %q", ConnectionStatusConnected, "connected")
	}
	if ConnectionStatusDisconnected != "disconnected" {
		t.Errorf("ConnectionStatusDisconnected = %q, want %q", ConnectionStatusDisconnected, "disconnected")
	}
}

func TestAgentStatusInfo_Fields(t *testing.T) {
	now := time.Now()
	info := &AgentStatusInfo{
		NodeName:             "test-node",
		AgentVersion:         "v1.0.0",
		AppliedConfigVersion: "v2.0.0",
		Healthy:              true,
		LastSeen:             now,
		Connected:            true,
		ActiveConnections:    42,
		Errors:               []string{"error1", "error2"},
		Metrics: map[string]int64{
			"requests": 1000,
			"errors":   5,
		},
		ClusterName:   "test-cluster",
		ClusterRegion: "us-west-2",
		ClusterZone:   "zone-a",
	}

	if info.NodeName != "test-node" {
		t.Errorf("NodeName = %q, want %q", info.NodeName, "test-node")
	}
	if info.AgentVersion != "v1.0.0" {
		t.Errorf("AgentVersion = %q, want %q", info.AgentVersion, "v1.0.0")
	}
	if info.AppliedConfigVersion != "v2.0.0" {
		t.Errorf("AppliedConfigVersion = %q, want %q", info.AppliedConfigVersion, "v2.0.0")
	}
	if !info.Healthy {
		t.Error("Healthy should be true")
	}
	if info.LastSeen != now {
		t.Errorf("LastSeen mismatch")
	}
	if !info.Connected {
		t.Error("Connected should be true")
	}
	if info.ActiveConnections != 42 {
		t.Errorf("ActiveConnections = %d, want 42", info.ActiveConnections)
	}
	if len(info.Errors) != 2 {
		t.Errorf("Errors length = %d, want 2", len(info.Errors))
	}
	if info.Metrics["requests"] != 1000 {
		t.Errorf("Metrics[requests] = %d, want 1000", info.Metrics["requests"])
	}
	if info.ClusterName != "test-cluster" {
		t.Errorf("ClusterName = %q, want %q", info.ClusterName, "test-cluster")
	}
	if info.ClusterRegion != "us-west-2" {
		t.Errorf("ClusterRegion = %q, want %q", info.ClusterRegion, "us-west-2")
	}
	if info.ClusterZone != "zone-a" {
		t.Errorf("ClusterZone = %q, want %q", info.ClusterZone, "zone-a")
	}
}

func TestAgentExpiryDuration(t *testing.T) {
	expected := 30 * time.Second
	if AgentExpiryDuration != expected {
		t.Errorf("AgentExpiryDuration = %v, want %v", AgentExpiryDuration, expected)
	}
}

func TestStatusCleanupInterval(t *testing.T) {
	expected := 10 * time.Second
	if StatusCleanupInterval != expected {
		t.Errorf("StatusCleanupInterval = %v, want %v", StatusCleanupInterval, expected)
	}
}
