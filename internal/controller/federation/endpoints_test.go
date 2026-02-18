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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestNewRemoteEndpointCache(t *testing.T) {
	cache := NewRemoteEndpointCache()
	assert.NotNil(t, cache)
	assert.NotNil(t, cache.endpoints)
}

func TestServiceKey(t *testing.T) {
	key := serviceKey("default", "my-service")
	assert.Equal(t, "default/my-service", key)
}

func TestRemoteEndpointCache_Update(t *testing.T) {
	cache := NewRemoteEndpointCache()

	endpoints := &pb.ServiceEndpoints{
		Namespace:   "default",
		ServiceName: "test-service",
		Endpoints: []*pb.Endpoint{
			{
				Address: "10.0.0.1",
				Port:    8080,
			},
		},
	}

	cache.Update("cluster-1", endpoints)

	// Verify the endpoint was added
	result := cache.GetForService("default", "test-service")
	require.Len(t, result, 1)
	assert.Equal(t, "default", result[0].Namespace)
	assert.Equal(t, "test-service", result[0].ServiceName)
}

func TestRemoteEndpointCache_UpdateNil(t *testing.T) {
	cache := NewRemoteEndpointCache()

	// Update with nil should not panic
	cache.Update("cluster-1", nil)

	result := cache.GetForService("default", "test-service")
	assert.Empty(t, result)
}

func TestRemoteEndpointCache_UpdateMultipleClusters(t *testing.T) {
	cache := NewRemoteEndpointCache()

	endpoints1 := &pb.ServiceEndpoints{
		Namespace:   "default",
		ServiceName: "test-service",
		Endpoints: []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080},
		},
	}

	endpoints2 := &pb.ServiceEndpoints{
		Namespace:   "default",
		ServiceName: "test-service",
		Endpoints: []*pb.Endpoint{
			{Address: "10.0.1.1", Port: 8080},
		},
	}

	cache.Update("cluster-1", endpoints1)
	cache.Update("cluster-2", endpoints2)

	result := cache.GetForService("default", "test-service")
	require.Len(t, result, 2)
}

func TestRemoteEndpointCache_Delete(t *testing.T) {
	cache := NewRemoteEndpointCache()

	endpoints := &pb.ServiceEndpoints{
		Namespace:   "default",
		ServiceName: "test-service",
		Endpoints: []*pb.Endpoint{
			{Address: "10.0.0.1", Port: 8080},
		},
	}

	cache.Update("cluster-1", endpoints)
	require.Len(t, cache.GetForService("default", "test-service"), 1)

	// Delete the endpoint
	cache.Delete("cluster-1", "default", "test-service")

	result := cache.GetForService("default", "test-service")
	assert.Empty(t, result)
}

func TestRemoteEndpointCache_DeleteNonExistent(_ *testing.T) {
	cache := NewRemoteEndpointCache()

	// Delete on empty cache should not panic
	cache.Delete("cluster-1", "default", "test-service")

	// Delete non-existent cluster should not panic
	endpoints := &pb.ServiceEndpoints{
		Namespace:   "default",
		ServiceName: "test-service",
	}
	cache.Update("cluster-1", endpoints)
	cache.Delete("cluster-2", "default", "test-service")
}

func TestRemoteEndpointCache_DeleteCluster(t *testing.T) {
	cache := NewRemoteEndpointCache()

	endpoints1 := &pb.ServiceEndpoints{
		Namespace:   "default",
		ServiceName: "service-1",
	}
	endpoints2 := &pb.ServiceEndpoints{
		Namespace:   "default",
		ServiceName: "service-2",
	}

	cache.Update("cluster-1", endpoints1)
	cache.Update("cluster-1", endpoints2)

	// Delete entire cluster
	cache.DeleteCluster("cluster-1")

	result := cache.GetForService("default", "service-1")
	assert.Empty(t, result)

	result = cache.GetForService("default", "service-2")
	assert.Empty(t, result)
}

func TestRemoteEndpointCache_GetForService_Empty(t *testing.T) {
	cache := NewRemoteEndpointCache()

	result := cache.GetForService("default", "non-existent")
	assert.Empty(t, result)
	assert.NotNil(t, result)
}

func TestRemoteEndpointCache_ConcurrentAccess(_ *testing.T) {
	cache := NewRemoteEndpointCache()

	done := make(chan bool)

	// Concurrent updates
	go func() {
		for i := int32(0); i < 100; i++ {
			// Use int32 loop variable to avoid conversion issues
			portNum := 8080 + i
			endpoints := &pb.ServiceEndpoints{
				Namespace:   "default",
				ServiceName: "test-service",
				Endpoints: []*pb.Endpoint{
					{Address: "10.0.0.1", Port: portNum},
				},
			}
			cache.Update("cluster-1", endpoints)
		}
		done <- true
	}()

	// Concurrent reads
	go func() {
		for i := 0; i < 100; i++ {
			_ = cache.GetForService("default", "test-service")
		}
		done <- true
	}()

	// Concurrent deletes
	go func() {
		for i := 0; i < 100; i++ {
			cache.Delete("cluster-1", "default", "test-service")
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}
}

func TestRemoteEndpointCache_DeleteCleansEmptyClusterMaps(t *testing.T) {
	cache := NewRemoteEndpointCache()

	endpoints := &pb.ServiceEndpoints{
		Namespace:   "default",
		ServiceName: "test-service",
	}

	cache.Update("cluster-1", endpoints)
	cache.Delete("cluster-1", "default", "test-service")

	// Verify cluster map was cleaned up
	cache.mu.RLock()
	_, exists := cache.endpoints["cluster-1"]
	cache.mu.RUnlock()

	assert.False(t, exists, "cluster map should be deleted when empty")
}
