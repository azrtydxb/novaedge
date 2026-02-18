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

package dns

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProviderConfig_ApplyDefaults(t *testing.T) {
	t.Run("applies all defaults when empty", func(t *testing.T) {
		config := &ProviderConfig{}
		config.ApplyDefaults()

		assert.Equal(t, DefaultPropagationTimeout, config.PropagationTimeout)
		assert.Equal(t, DefaultPollingInterval, config.PollingInterval)
		assert.NotNil(t, config.Logger)
	})

	t.Run("preserves existing values", func(t *testing.T) {
		customTimeout := 60 * time.Second
		customInterval := 10 * time.Second
		logger := zap.NewNop()

		config := &ProviderConfig{
			PropagationTimeout: customTimeout,
			PollingInterval:    customInterval,
			Logger:             logger,
		}
		config.ApplyDefaults()

		assert.Equal(t, customTimeout, config.PropagationTimeout)
		assert.Equal(t, customInterval, config.PollingInterval)
		assert.Equal(t, logger, config.Logger)
	})

	t.Run("applies partial defaults", func(t *testing.T) {
		config := &ProviderConfig{
			PropagationTimeout: 60 * time.Second,
		}
		config.ApplyDefaults()

		assert.Equal(t, 60*time.Second, config.PropagationTimeout)
		assert.Equal(t, DefaultPollingInterval, config.PollingInterval)
		assert.NotNil(t, config.Logger)
	})
}

func TestNewProvider_Unsupported(t *testing.T) {
	_, err := NewProvider("unsupported", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported DNS provider")
}

func TestNewProvider_NilConfig(t *testing.T) {
	// Test that nil config is handled and defaults are applied
	_, err := NewProvider("cloudflare", map[string]string{}, nil)
	// Should fail due to missing credentials, not nil config
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api_token")
}

func TestDefaultConstants(t *testing.T) {
	assert.Equal(t, 120*time.Second, DefaultPropagationTimeout)
	assert.Equal(t, 5*time.Second, DefaultPollingInterval)
}

// mockProvider implements Provider for testing
type mockProvider struct {
	createErr error
	deleteErr error
	waitErr   error
}

func (m *mockProvider) CreateTXTRecord(ctx context.Context, fqdn, value string) error {
	return m.createErr
}

func (m *mockProvider) DeleteTXTRecord(ctx context.Context, fqdn, value string) error {
	return m.deleteErr
}

func (m *mockProvider) WaitForPropagation(ctx context.Context, fqdn, value string) error {
	return m.waitErr
}

func TestDNS01ChallengeProvider_New(t *testing.T) {
	mock := &mockProvider{}
	logger := zap.NewNop()

	provider := NewDNS01ChallengeProvider(mock, logger)
	assert.NotNil(t, provider)
	assert.Equal(t, mock, provider.provider)
	assert.Equal(t, logger, provider.logger)
}

func TestDNS01ChallengeProvider_New_NilLogger(t *testing.T) {
	mock := &mockProvider{}

	provider := NewDNS01ChallengeProvider(mock, nil)
	assert.NotNil(t, provider)
	assert.NotNil(t, provider.logger)
}

func TestDNS01ChallengeProvider_Present(t *testing.T) {
	t.Run("successful present", func(t *testing.T) {
		mock := &mockProvider{}
		provider := NewDNS01ChallengeProvider(mock, zap.NewNop())

		err := provider.Present("example.com", "token", "keyAuth")
		require.NoError(t, err)
	})

	t.Run("create record error", func(t *testing.T) {
		mock := &mockProvider{
			createErr: errors.New("create failed"),
		}
		provider := NewDNS01ChallengeProvider(mock, zap.NewNop())

		err := provider.Present("example.com", "token", "keyAuth")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create TXT record")
	})

	t.Run("propagation wait error is logged but not returned", func(t *testing.T) {
		mock := &mockProvider{
			waitErr: errors.New("propagation timeout"),
		}
		provider := NewDNS01ChallengeProvider(mock, zap.NewNop())

		err := provider.Present("example.com", "token", "keyAuth")
		// Should not return error for propagation wait failure
		require.NoError(t, err)
	})
}

func TestDNS01ChallengeProvider_CleanUp(t *testing.T) {
	t.Run("successful cleanup", func(t *testing.T) {
		mock := &mockProvider{}
		provider := NewDNS01ChallengeProvider(mock, zap.NewNop())

		err := provider.CleanUp("example.com", "token", "keyAuth")
		require.NoError(t, err)
	})

	t.Run("delete record error", func(t *testing.T) {
		mock := &mockProvider{
			deleteErr: errors.New("delete failed"),
		}
		provider := NewDNS01ChallengeProvider(mock, zap.NewNop())

		err := provider.CleanUp("example.com", "token", "keyAuth")
		require.Error(t, err)
		assert.Equal(t, "delete failed", err.Error())
	})
}
