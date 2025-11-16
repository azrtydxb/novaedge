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

package upstream

import (
	"testing"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// BenchmarkPoolCreation tests the performance of pool creation
func BenchmarkPoolCreation(b *testing.B) {
	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:             "test-cluster",
		Namespace:        "default",
		LbPolicy:         pb.LoadBalancingPolicy_ROUND_ROBIN,
		ConnectTimeoutMs: 5000,
		IdleTimeoutMs:    90000,
		ConnectionPool: &pb.ConnectionPool{
			MaxIdleConns:            100,
			MaxIdleConnsPerHost:     10,
			IdleConnTimeoutMs:       90000,
			ResponseHeaderTimeoutMs: 10000,
		},
	}

	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		pool := NewPool(cluster, endpoints, logger)
		pool.Close()
	}
}

// BenchmarkUpdateEndpoints tests the performance of endpoint updates
func BenchmarkUpdateEndpoints(b *testing.B) {
	logger := zap.NewNop()
	cluster := &pb.Cluster{
		Name:             "test-cluster",
		Namespace:        "default",
		LbPolicy:         pb.LoadBalancingPolicy_ROUND_ROBIN,
		ConnectTimeoutMs: 5000,
		IdleTimeoutMs:    90000,
		ConnectionPool: &pb.ConnectionPool{
			MaxIdleConns:            100,
			MaxIdleConnsPerHost:     10,
			IdleConnTimeoutMs:       90000,
			ResponseHeaderTimeoutMs: 10000,
		},
	}

	initialEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	updatedEndpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
		{Address: "10.0.0.3", Port: 8080, Ready: true},
	}

	pool := NewPool(cluster, initialEndpoints, logger)
	defer pool.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			pool.UpdateEndpoints(updatedEndpoints)
		} else {
			pool.UpdateEndpoints(initialEndpoints)
		}
	}
}

// BenchmarkConnectionPoolConfig tests different connection pool configurations
func BenchmarkConnectionPoolConfig(b *testing.B) {
	logger := zap.NewNop()
	endpoints := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Ready: true},
		{Address: "10.0.0.2", Port: 8080, Ready: true},
	}

	b.Run("SmallPool", func(b *testing.B) {
		cluster := &pb.Cluster{
			Name:             "test-cluster",
			Namespace:        "default",
			ConnectTimeoutMs: 5000,
			IdleTimeoutMs:    90000,
			ConnectionPool: &pb.ConnectionPool{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 2,
			},
		}

		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			pool := NewPool(cluster, endpoints, logger)
			pool.Close()
		}
	})

	b.Run("LargePool", func(b *testing.B) {
		cluster := &pb.Cluster{
			Name:             "test-cluster",
			Namespace:        "default",
			ConnectTimeoutMs: 5000,
			IdleTimeoutMs:    90000,
			ConnectionPool: &pb.ConnectionPool{
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 50,
			},
		}

		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			pool := NewPool(cluster, endpoints, logger)
			pool.Close()
		}
	})

	b.Run("DefaultConfig", func(b *testing.B) {
		cluster := &pb.Cluster{
			Name:             "test-cluster",
			Namespace:        "default",
			ConnectTimeoutMs: 5000,
			IdleTimeoutMs:    90000,
			// ConnectionPool is nil - will use defaults
		}

		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			pool := NewPool(cluster, endpoints, logger)
			pool.Close()
		}
	})
}
