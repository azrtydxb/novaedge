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

package policy

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/piwi3910/novaedge/internal/agent/metrics"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// RedisClient wraps a Redis client with health checking and metrics
type RedisClient struct {
	client  redis.UniversalClient
	logger  *zap.Logger
	mu      sync.RWMutex
	healthy bool
	config  *pb.RedisConfig
}

// NewRedisClient creates a new Redis client from protobuf configuration
func NewRedisClient(cfg *pb.RedisConfig, password string, logger *zap.Logger) (*RedisClient, error) {
	var client redis.UniversalClient

	opts := &redis.UniversalOptions{
		Addrs:        []string{cfg.Address},
		Password:     password,
		DB:           int(cfg.Database),
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 2,
	}

	if cfg.Tls {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	if cfg.ClusterMode {
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        opts.Addrs,
			Password:     opts.Password,
			DialTimeout:  opts.DialTimeout,
			ReadTimeout:  opts.ReadTimeout,
			WriteTimeout: opts.WriteTimeout,
			PoolSize:     opts.PoolSize,
			MinIdleConns: opts.MinIdleConns,
			TLSConfig:    opts.TLSConfig,
		})
	} else {
		client = redis.NewClient(&redis.Options{
			Addr:         cfg.Address,
			Password:     password,
			DB:           int(cfg.Database),
			DialTimeout:  opts.DialTimeout,
			ReadTimeout:  opts.ReadTimeout,
			WriteTimeout: opts.WriteTimeout,
			PoolSize:     opts.PoolSize,
			MinIdleConns: opts.MinIdleConns,
			TLSConfig:    opts.TLSConfig,
		})
	}

	rc := &RedisClient{
		client:  client,
		logger:  logger,
		healthy: true,
		config:  cfg,
	}

	// Initial health check
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		logger.Warn("Redis initial health check failed, falling back to local rate limiting",
			zap.String("address", cfg.Address),
			zap.Error(err),
		)
		rc.healthy = false
		// Return both client (for potential reconnection) and error so caller knows about the failure
		return rc, fmt.Errorf("redis ping failed: %w", err)
	}

	return rc, nil
}

// IsHealthy returns whether the Redis connection is healthy
func (rc *RedisClient) IsHealthy() bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.healthy
}

// Client returns the underlying Redis client
func (rc *RedisClient) Client() redis.UniversalClient {
	return rc.client
}

// HealthCheck performs a health check and updates the healthy status
func (rc *RedisClient) HealthCheck(ctx context.Context) bool {
	start := time.Now()
	err := rc.client.Ping(ctx).Err()
	duration := time.Since(start).Seconds()

	metrics.RedisLatency.Observe(duration)

	rc.mu.Lock()
	defer rc.mu.Unlock()

	if err != nil {
		rc.healthy = false
		metrics.RedisErrors.Inc()
		rc.logger.Warn("Redis health check failed",
			zap.String("address", rc.config.Address),
			zap.Error(err),
		)
		return false
	}

	rc.healthy = true
	return true
}

// Close closes the Redis client connection
func (rc *RedisClient) Close() error {
	return rc.client.Close()
}
