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
	"fmt"
	"sync"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// RemoteEndpointCache stores ServiceEndpoints received from federated clusters.
// It is keyed by cluster name and then by "namespace/service" to allow fast
// lookups when building ConfigSnapshots that include remote endpoints.
type RemoteEndpointCache struct {
	mu        sync.RWMutex
	endpoints map[string]map[string]*pb.ServiceEndpoints // cluster -> "namespace/service" -> endpoints
}

// NewRemoteEndpointCache creates an empty RemoteEndpointCache.
func NewRemoteEndpointCache() *RemoteEndpointCache {
	return &RemoteEndpointCache{
		endpoints: make(map[string]map[string]*pb.ServiceEndpoints),
	}
}

// serviceKey builds the map key for a given namespace and service name.
func serviceKey(namespace, serviceName string) string {
	return fmt.Sprintf("%s/%s", namespace, serviceName)
}

// Update inserts or replaces the endpoints for a service in a remote cluster.
func (c *RemoteEndpointCache) Update(cluster string, endpoints *pb.ServiceEndpoints) {
	if endpoints == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	clusterMap, ok := c.endpoints[cluster]
	if !ok {
		clusterMap = make(map[string]*pb.ServiceEndpoints)
		c.endpoints[cluster] = clusterMap
	}

	key := serviceKey(endpoints.Namespace, endpoints.ServiceName)
	clusterMap[key] = endpoints
}

// Delete removes endpoint data for a specific service from a specific cluster.
func (c *RemoteEndpointCache) Delete(cluster, namespace, serviceName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	clusterMap, ok := c.endpoints[cluster]
	if !ok {
		return
	}

	key := serviceKey(namespace, serviceName)
	delete(clusterMap, key)

	// Clean up empty cluster maps to avoid unbounded growth.
	if len(clusterMap) == 0 {
		delete(c.endpoints, cluster)
	}
}

// DeleteCluster removes all endpoint data for a cluster.
func (c *RemoteEndpointCache) DeleteCluster(cluster string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.endpoints, cluster)
}

// GetForService returns all remote ServiceEndpoints for a given service across
// all federated clusters. The returned slice may be empty but is never nil.
func (c *RemoteEndpointCache) GetForService(namespace, serviceName string) []*pb.ServiceEndpoints {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := serviceKey(namespace, serviceName)
	var result []*pb.ServiceEndpoints

	for _, clusterMap := range c.endpoints {
		if ep, ok := clusterMap[key]; ok {
			result = append(result, ep)
		}
	}

	if result == nil {
		return []*pb.ServiceEndpoints{}
	}
	return result
}

// GetAll returns a copy of all remote ServiceEndpoints grouped by service key
// ("namespace/service"). Each service key maps to the list of
// ServiceEndpoints from every cluster that has data for that service.
func (c *RemoteEndpointCache) GetAll() map[string][]*pb.ServiceEndpoints {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string][]*pb.ServiceEndpoints)
	for _, clusterMap := range c.endpoints {
		for key, ep := range clusterMap {
			result[key] = append(result[key], ep)
		}
	}
	return result
}
