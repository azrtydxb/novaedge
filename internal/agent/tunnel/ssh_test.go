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

package tunnel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	v1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

func TestNewSSHTunnel(t *testing.T) {
	logger := zap.NewNop()

	t.Run("valid config", func(t *testing.T) {
		config := v1alpha1.TunnelConfig{
			Type:          v1alpha1.TunnelTypeSSH,
			RelayEndpoint: "ssh.example.com:22",
		}

		tunnel, err := newSSHTunnel("test-cluster", config, logger)
		require.NoError(t, err)
		require.NotNil(t, tunnel)

		assert.Equal(t, "test-cluster", tunnel.clusterName)
		assert.Equal(t, config, tunnel.config)
	})

	t.Run("missing relay endpoint", func(t *testing.T) {
		config := v1alpha1.TunnelConfig{
			Type: v1alpha1.TunnelTypeSSH,
		}

		tunnel, err := newSSHTunnel("test-cluster", config, logger)
		require.Error(t, err)
		require.Nil(t, tunnel)
		assert.Contains(t, err.Error(), "relay endpoint is required")
	})
}

func TestSSHTunnel_StartStop(t *testing.T) {
	logger := zap.NewNop()
	config := v1alpha1.TunnelConfig{
		Type:          v1alpha1.TunnelTypeSSH,
		RelayEndpoint: "ssh.example.com:22",
	}

	tunnel, err := newSSHTunnel("test-cluster", config, logger)
	require.NoError(t, err)

	t.Run("start and stop", func(t *testing.T) {
		ctx := context.Background()
		err := tunnel.Start(ctx)
		assert.NoError(t, err)

		// Verify context was set
		tunnel.mu.Lock()
		ctxSet := tunnel.ctx != nil
		tunnel.mu.Unlock()
		assert.True(t, ctxSet)

		// Stop should not block indefinitely
		err = tunnel.Stop()
		assert.NoError(t, err)

		// After stop, healthy should be false
		assert.False(t, tunnel.IsHealthy())
	})

	t.Run("stop without start", func(t *testing.T) {
		tunnel2, err := newSSHTunnel("test-cluster2", config, logger)
		require.NoError(t, err)

		// Stop without start should not panic
		// Note: This will block on <-t.done, so we need to close done channel
		close(tunnel2.done)
		err = tunnel2.Stop()
		assert.NoError(t, err)
	})
}

func TestSSHTunnel_IsHealthy(t *testing.T) {
	logger := zap.NewNop()
	config := v1alpha1.TunnelConfig{
		Type:          v1alpha1.TunnelTypeSSH,
		RelayEndpoint: "ssh.example.com:22",
	}

	tunnel, err := newSSHTunnel("test-cluster", config, logger)
	require.NoError(t, err)

	// Initially not healthy
	assert.False(t, tunnel.IsHealthy())

	// Set healthy
	tunnel.healthy.Store(true)
	assert.True(t, tunnel.IsHealthy())

	// Set unhealthy
	tunnel.healthy.Store(false)
	assert.False(t, tunnel.IsHealthy())
}

func TestSSHTunnel_LocalAddr(t *testing.T) {
	logger := zap.NewNop()
	config := v1alpha1.TunnelConfig{
		Type:          v1alpha1.TunnelTypeSSH,
		RelayEndpoint: "ssh.example.com:22",
	}

	tunnel, err := newSSHTunnel("test-cluster", config, logger)
	require.NoError(t, err)

	// Initially empty
	assert.Equal(t, "", tunnel.LocalAddr())

	// Set local address
	tunnel.mu.Lock()
	tunnel.localAddr = "127.0.0.1:15002"
	tunnel.mu.Unlock()

	assert.Equal(t, "127.0.0.1:15002", tunnel.LocalAddr())
}

func TestSSHTunnel_OverlayAddr(t *testing.T) {
	logger := zap.NewNop()
	config := v1alpha1.TunnelConfig{
		Type:          v1alpha1.TunnelTypeSSH,
		RelayEndpoint: "ssh.example.com:22",
	}

	tunnel, err := newSSHTunnel("test-cluster", config, logger)
	require.NoError(t, err)

	// SSH tunnel doesn't participate in overlay, should return empty
	assert.Equal(t, "", tunnel.OverlayAddr())
}

func TestSSHTunnel_Type(t *testing.T) {
	logger := zap.NewNop()
	config := v1alpha1.TunnelConfig{
		Type:          v1alpha1.TunnelTypeSSH,
		RelayEndpoint: "ssh.example.com:22",
	}

	tunnel, err := newSSHTunnel("test-cluster", config, logger)
	require.NoError(t, err)

	assert.Equal(t, "ssh", tunnel.Type())
}

func TestSSHTunnel_closeConnections(t *testing.T) {
	logger := zap.NewNop()
	config := v1alpha1.TunnelConfig{
		Type:          v1alpha1.TunnelTypeSSH,
		RelayEndpoint: "ssh.example.com:22",
	}

	t.Run("nil connections", func(t *testing.T) {
		tunnel, err := newSSHTunnel("test-cluster", config, logger)
		require.NoError(t, err)

		// Should not panic with nil connections
		tunnel.closeConnections()
	})
}

func TestSSHTunnelConstants(t *testing.T) {
	// Verify constants are set correctly
	assert.Equal(t, "15002", sshRemoteForwardPort)
	assert.Equal(t, sshDialTimeout, 10*time.Second)
	assert.Equal(t, sshKeepaliveInterval, 15*time.Second)
}
