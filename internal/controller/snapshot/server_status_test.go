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
	"context"
	"testing"
	"time"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestAgentStatusStorage(t *testing.T) {
	// Create a test server
	server := &Server{
		shutdownCh: make(chan struct{}),
	}
	defer server.Shutdown()

	// Test ReportStatus updates agent status
	req := &pb.AgentStatus{
		NodeName:             "test-node-1",
		AppliedConfigVersion: "v1.0.0",
		Timestamp:            time.Now().Unix(),
		Healthy:              true,
		Metrics: map[string]int64{
			"active_connections": 42,
			"request_count":      1000,
		},
		Errors: []string{},
	}

	_, err := server.ReportStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("ReportStatus failed: %v", err)
	}

	// Verify status was stored
	status, ok := server.GetAgentStatus("test-node-1")
	if !ok {
		t.Fatal("Expected agent status to be stored")
	}

	if status.NodeName != "test-node-1" {
		t.Errorf("Expected NodeName to be 'test-node-1', got %s", status.NodeName)
	}

	if status.AppliedConfigVersion != "v1.0.0" {
		t.Errorf("Expected AppliedConfigVersion to be 'v1.0.0', got %s", status.AppliedConfigVersion)
	}

	if !status.Healthy {
		t.Error("Expected Healthy to be true")
	}

	if status.ActiveConnections != 42 {
		t.Errorf("Expected ActiveConnections to be 42, got %d", status.ActiveConnections)
	}

	if status.Metrics["request_count"] != 1000 {
		t.Errorf("Expected request_count metric to be 1000, got %d", status.Metrics["request_count"])
	}
}

func TestAgentConnectionTracking(t *testing.T) {
	// Create a test server
	server := &Server{
		shutdownCh: make(chan struct{}),
	}
	defer server.Shutdown()

	// Test updateAgentConnection
	server.updateAgentConnection("test-node-2", "v1.2.3", true)

	status, ok := server.GetAgentStatus("test-node-2")
	if !ok {
		t.Fatal("Expected agent status to be stored")
	}

	if !status.Connected {
		t.Error("Expected Connected to be true")
	}

	if status.AgentVersion != "v1.2.3" {
		t.Errorf("Expected AgentVersion to be 'v1.2.3', got %s", status.AgentVersion)
	}

	// Update to disconnected
	server.updateAgentConnection("test-node-2", "v1.2.3", false)

	status, ok = server.GetAgentStatus("test-node-2")
	if !ok {
		t.Fatal("Expected agent status to be stored")
	}

	if status.Connected {
		t.Error("Expected Connected to be false")
	}
}

func TestGetAllAgentStatuses(t *testing.T) {
	// Create a test server
	server := &Server{
		shutdownCh: make(chan struct{}),
	}
	defer server.Shutdown()

	// Add multiple agents
	server.updateAgentConnection("node-1", "v1.0.0", true)
	server.updateAgentConnection("node-2", "v1.0.1", true)
	server.updateAgentConnection("node-3", "v1.0.2", false)

	// Get all statuses
	statuses := server.GetAllAgentStatuses()

	if len(statuses) != 3 {
		t.Errorf("Expected 3 agent statuses, got %d", len(statuses))
	}

	// Verify we have all three nodes
	nodeNames := make(map[string]bool)
	for _, status := range statuses {
		nodeNames[status.NodeName] = true
	}

	expectedNodes := []string{"node-1", "node-2", "node-3"}
	for _, nodeName := range expectedNodes {
		if !nodeNames[nodeName] {
			t.Errorf("Expected to find node %s in statuses", nodeName)
		}
	}
}

func TestAgentStatusNotFound(t *testing.T) {
	// Create a test server
	server := &Server{
		shutdownCh: make(chan struct{}),
	}
	defer server.Shutdown()

	// Try to get status for non-existent agent
	_, ok := server.GetAgentStatus("non-existent-node")
	if ok {
		t.Error("Expected GetAgentStatus to return false for non-existent node")
	}
}

func TestAgentStatusCopy(t *testing.T) {
	// Create a test server
	server := &Server{
		shutdownCh: make(chan struct{}),
	}
	defer server.Shutdown()

	// Store an agent status with errors and metrics
	req := &pb.AgentStatus{
		NodeName:             "test-node",
		AppliedConfigVersion: "v1.0.0",
		Timestamp:            time.Now().Unix(),
		Healthy:              false,
		Metrics: map[string]int64{
			"active_connections": 10,
		},
		Errors: []string{"error1", "error2"},
	}

	server.storeAgentStatus(req)

	// Get the status
	status1, ok := server.GetAgentStatus("test-node")
	if !ok {
		t.Fatal("Expected agent status to be stored")
	}

	// Modify the returned copy
	status1.Errors[0] = "modified"
	status1.Metrics["active_connections"] = 999

	// Get the status again
	status2, ok := server.GetAgentStatus("test-node")
	if !ok {
		t.Fatal("Expected agent status to be stored")
	}

	// Verify original data wasn't modified
	if status2.Errors[0] != "error1" {
		t.Errorf("Expected first error to be 'error1', got %s (copy was not deep)", status2.Errors[0])
	}

	if status2.Metrics["active_connections"] != 10 {
		t.Errorf("Expected active_connections to be 10, got %d (copy was not deep)", status2.Metrics["active_connections"])
	}
}
