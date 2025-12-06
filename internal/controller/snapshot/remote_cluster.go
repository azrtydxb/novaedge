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
	"sync"
	"time"
)

// RemoteClusterHandler is the interface that remote cluster registries must implement
// to receive updates from the gRPC server about remote agent connections
type RemoteClusterHandler interface {
	// UpdateConnection updates the connection status of agents from a remote cluster
	UpdateConnection(clusterName string, connected bool, agentCount, readyAgents int32)
}

// RemoteAgentInfo tracks information about an agent from a remote cluster
type RemoteAgentInfo struct {
	NodeName      string
	ClusterName   string
	ClusterRegion string
	ClusterZone   string
	AgentVersion  string
	Connected     bool
	Healthy       bool
	LastSeen      time.Time
	Labels        map[string]string
}

// RemoteAgentTracker tracks agents from remote clusters
type RemoteAgentTracker struct {
	mu      sync.RWMutex
	agents  map[string]*RemoteAgentInfo // key: clusterName/nodeName
	handler RemoteClusterHandler
}

// NewRemoteAgentTracker creates a new remote agent tracker
func NewRemoteAgentTracker() *RemoteAgentTracker {
	return &RemoteAgentTracker{
		agents: make(map[string]*RemoteAgentInfo),
	}
}

// SetHandler sets the handler for remote cluster updates
func (t *RemoteAgentTracker) SetHandler(handler RemoteClusterHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handler = handler
}

// key generates a unique key for an agent
func (t *RemoteAgentTracker) key(clusterName, nodeName string) string {
	return clusterName + "/" + nodeName
}

// RegisterAgent registers or updates a remote agent
func (t *RemoteAgentTracker) RegisterAgent(info *RemoteAgentInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := t.key(info.ClusterName, info.NodeName)
	t.agents[key] = info

	// Notify handler of connection update
	if t.handler != nil {
		agentCount, readyAgents := t.countAgentsForCluster(info.ClusterName)
		t.handler.UpdateConnection(info.ClusterName, true, agentCount, readyAgents)
	}
}

// UnregisterAgent removes an agent from tracking
func (t *RemoteAgentTracker) UnregisterAgent(clusterName, nodeName string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := t.key(clusterName, nodeName)
	delete(t.agents, key)

	// Notify handler of connection update
	if t.handler != nil {
		agentCount, readyAgents := t.countAgentsForCluster(clusterName)
		connected := agentCount > 0
		t.handler.UpdateConnection(clusterName, connected, agentCount, readyAgents)
	}
}

// UpdateAgentStatus updates the health status of an agent
func (t *RemoteAgentTracker) UpdateAgentStatus(clusterName, nodeName string, healthy bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := t.key(clusterName, nodeName)
	if agent, ok := t.agents[key]; ok {
		agent.Healthy = healthy
		agent.LastSeen = time.Now()

		// Notify handler of status update
		if t.handler != nil {
			agentCount, readyAgents := t.countAgentsForCluster(clusterName)
			t.handler.UpdateConnection(clusterName, true, agentCount, readyAgents)
		}
	}
}

// countAgentsForCluster counts total and ready agents for a cluster (must be called with lock held)
func (t *RemoteAgentTracker) countAgentsForCluster(clusterName string) (int32, int32) {
	var total, ready int32
	for _, agent := range t.agents {
		if agent.ClusterName == clusterName {
			total++
			if agent.Connected && agent.Healthy {
				ready++
			}
		}
	}
	return total, ready
}

// GetAgentsForCluster returns all agents for a given cluster
func (t *RemoteAgentTracker) GetAgentsForCluster(clusterName string) []*RemoteAgentInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []*RemoteAgentInfo
	for _, agent := range t.agents {
		if agent.ClusterName == clusterName {
			// Return a copy to avoid concurrent modification
			agentCopy := *agent
			if agent.Labels != nil {
				agentCopy.Labels = make(map[string]string)
				for k, v := range agent.Labels {
					agentCopy.Labels[k] = v
				}
			}
			result = append(result, &agentCopy)
		}
	}
	return result
}

// GetAllRemoteClusters returns a list of all remote cluster names with agents
func (t *RemoteAgentTracker) GetAllRemoteClusters() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	clusterSet := make(map[string]struct{})
	for _, agent := range t.agents {
		if agent.ClusterName != "" {
			clusterSet[agent.ClusterName] = struct{}{}
		}
	}

	result := make([]string, 0, len(clusterSet))
	for cluster := range clusterSet {
		result = append(result, cluster)
	}
	return result
}

// GetClusterStats returns agent counts for a cluster
func (t *RemoteAgentTracker) GetClusterStats(clusterName string) (total, ready int32) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.countAgentsForCluster(clusterName)
}

// CleanupStaleAgents marks agents as disconnected if they haven't been seen recently
func (t *RemoteAgentTracker) CleanupStaleAgents(maxAge time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	clustersToUpdate := make(map[string]struct{})

	for key, agent := range t.agents {
		if now.Sub(agent.LastSeen) > maxAge {
			if agent.Connected {
				agent.Connected = false
				clustersToUpdate[agent.ClusterName] = struct{}{}
			}
			// Optionally remove completely stale agents
			if now.Sub(agent.LastSeen) > maxAge*3 {
				delete(t.agents, key)
				clustersToUpdate[agent.ClusterName] = struct{}{}
			}
		}
	}

	// Notify handler of updates
	if t.handler != nil {
		for clusterName := range clustersToUpdate {
			agentCount, readyAgents := t.countAgentsForCluster(clusterName)
			connected := agentCount > 0
			t.handler.UpdateConnection(clusterName, connected, agentCount, readyAgents)
		}
	}
}
